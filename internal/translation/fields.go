package translation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"golang.org/x/text/language"
)

var (
	ErrInvalidFields = errors.New("translation fields are invalid")
	fieldKeyPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)
)

// TranslateContent translates one article-like source snapshot with the
// configured default provider without creating a standalone Translation task
// or triggering a public rebuild. Site-wide localization uses this primitive
// so it can validate and publish the complete locale exactly once.
func (s *Service) TranslateContent(ctx context.Context, sourceLocale, targetLocale string, source domain.LocalizedMarkdown) (domain.LocalizedMarkdown, error) {
	sourceLocale, targetLocale, err := canonicalLocalePair(sourceLocale, targetLocale)
	if err != nil {
		return domain.LocalizedMarkdown{}, err
	}
	provider, _, err := s.ai.DefaultCredentials()
	if err != nil {
		return domain.LocalizedMarkdown{}, err
	}
	return s.translate(ctx, provider.ID, sourceLocale, targetLocale, source, nil)
}

// TranslateFields translates a bounded set of short, human-visible values
// while preserving its exact key set. Empty source values stay empty. The
// caller remains responsible for entity-specific validation and optimistic
// concurrency when applying the returned snapshot.
func (s *Service) TranslateFields(ctx context.Context, sourceLocale, targetLocale string, fields map[string]string) (map[string]string, error) {
	sourceLocale, targetLocale, err := canonicalLocalePair(sourceLocale, targetLocale)
	if err != nil {
		return nil, err
	}
	keys, runes, err := validateTranslationFields(fields)
	if err != nil {
		return nil, err
	}
	provider, _, err := s.ai.DefaultCredentials()
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(fields))
	switch provider.Kind {
	case ai.ProviderKindGoogleFree:
		for _, key := range keys {
			value := fields[key]
			if strings.TrimSpace(value) == "" {
				result[key] = value
				continue
			}
			translated, translateErr := s.translateGoogleText(ctx, provider.ID, sourceLocale, targetLocale, value, googleTranslationRuneLimit(provider))
			if translateErr != nil {
				return nil, fmt.Errorf("translate field %s: %w", key, translateErr)
			}
			if strings.TrimSpace(translated) == "" {
				return nil, fmt.Errorf("translate field %s: %w", key, ErrUnsafeOutput)
			}
			result[key] = translated
		}
	case ai.ProviderKindOpenAICompatible:
		request, marshalErr := json.Marshal(fields)
		if marshalErr != nil {
			return nil, marshalErr
		}
		response, translateErr := s.chatProviderWithRetry(ctx, provider.ID, []ai.ChatMessage{
			{Role: "system", Content: translationSystemPrompt(sourceLocale, targetLocale) + " Translate every JSON string value. Return one strict JSON object with exactly the same keys. Preserve empty values, placeholders, and product names. Do not use a Markdown fence."},
			{Role: "user", Content: string(request)},
		}, translationChunkTokenBudget(runes, provider.MaxOutputTokens))
		if translateErr != nil {
			return nil, translateErr
		}
		if decodeErr := decodeJSONObject(response, &result); decodeErr != nil {
			return nil, fmt.Errorf("invalid translated fields: %w", decodeErr)
		}
	default:
		return nil, ai.ErrInvalidProvider
	}
	if err := validateTranslatedFieldSet(fields, result, keys); err != nil {
		return nil, err
	}
	return result, nil
}

func canonicalLocalePair(rawSource, rawTarget string) (string, string, error) {
	sourceTag, sourceErr := language.Parse(strings.TrimSpace(rawSource))
	targetTag, targetErr := language.Parse(strings.TrimSpace(rawTarget))
	if sourceErr != nil || targetErr != nil || strings.TrimSpace(rawSource) == "" || strings.TrimSpace(rawTarget) == "" || sourceTag.String() == targetTag.String() {
		return "", "", ErrInvalidFields
	}
	return sourceTag.String(), targetTag.String(), nil
}

func validateTranslationFields(fields map[string]string) ([]string, int, error) {
	if len(fields) == 0 || len(fields) > 256 {
		return nil, 0, ErrInvalidFields
	}
	keys := make([]string, 0, len(fields))
	totalRunes := 0
	for key, value := range fields {
		if !fieldKeyPattern.MatchString(key) || !utf8.ValidString(value) || len([]rune(value)) > 20_000 {
			return nil, 0, ErrInvalidFields
		}
		keys = append(keys, key)
		totalRunes += utf8.RuneCountInString(value)
		if totalRunes > 100_000 {
			return nil, 0, ErrInvalidFields
		}
	}
	sort.Strings(keys)
	return keys, totalRunes, nil
}

func validateTranslatedFieldSet(source, translated map[string]string, keys []string) error {
	if len(translated) != len(source) {
		return ErrUnsafeOutput
	}
	for _, key := range keys {
		value, exists := translated[key]
		if !exists || !utf8.ValidString(value) {
			return ErrUnsafeOutput
		}
		if strings.TrimSpace(source[key]) == "" {
			if value != source[key] {
				return ErrUnsafeOutput
			}
			continue
		}
		if strings.TrimSpace(value) == "" {
			return ErrUnsafeOutput
		}
	}
	return nil
}
