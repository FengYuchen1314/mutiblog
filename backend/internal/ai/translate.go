package ai

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/fengyuchen/mutiblog/internal/model"
)

var placeholderTokenRE = regexp.MustCompile(`⟦P\d+⟧`)

type TranslationResult struct {
	Markdown string
	Warnings []string
	Usage    Usage
}

// TranslateMarkdown coordinates extraction, bounded requests, validation, and
// atomic-in-memory reconstruction. Callers write the resulting Markdown only
// after this function succeeds, so a failed provider can never leave a partial
// locale file on disk.
func TranslateMarkdown(ctx context.Context, provider Provider, markdown string, src, dst model.Locale, title string, budget int) (TranslationResult, error) {
	segs, err := Extract(markdown, ExtractOpts{})
	if err != nil {
		return TranslationResult{}, err
	}
	if len(segs) == 0 {
		return TranslationResult{Markdown: markdown}, nil
	}
	translated := make([]string, len(segs))
	var warnings []string
	for _, batch := range Batch(segs, budget) {
		values, failed, err := translateBatch(ctx, provider, batch, src, dst, title)
		if err != nil {
			return TranslationResult{}, err
		}
		for i, segment := range batch {
			translated[segment.Index] = values[i]
		}
		warnings = append(warnings, failed...)
	}
	if len(warnings)*5 > len(segs) {
		return TranslationResult{}, fmt.Errorf("translation validation failed for %d of %d segments", len(warnings), len(segs))
	}
	output, err := Apply(markdown, segs, translated)
	if err != nil {
		return TranslationResult{}, err
	}
	if err := CompareFingerprint(markdown, output); err != nil {
		return TranslationResult{}, err
	}
	result := TranslationResult{Markdown: output, Warnings: warnings}
	if p, ok := provider.(usageProvider); ok {
		result.Usage = p.LastUsage()
	}
	return result, nil
}

func translateBatch(ctx context.Context, provider Provider, batch []Segment, src, dst model.Locale, title string) ([]string, []string, error) {
	out, err := provider.Translate(ctx, batch, src, dst, title)
	if err == nil {
		err = ValidateBatch(batch, out)
	}
	if err == nil {
		return out, nil, nil
	}
	// One clean retry deals with transient malformed JSON or model drift.
	out, retryErr := provider.Translate(ctx, batch, src, dst, title)
	if retryErr == nil {
		retryErr = ValidateBatch(batch, out)
	}
	if retryErr == nil {
		return out, nil, nil
	}
	if len(batch) == 1 {
		return []string{batch[0].Text}, []string{fmt.Sprintf("segment %d retained original: %v", batch[0].Index, retryErr)}, nil
	}
	mid := len(batch) / 2
	left, leftWarnings, leftErr := translateBatch(ctx, provider, batch[:mid], src, dst, title)
	if leftErr != nil {
		return nil, nil, leftErr
	}
	right, rightWarnings, rightErr := translateBatch(ctx, provider, batch[mid:], src, dst, title)
	if rightErr != nil {
		return nil, nil, rightErr
	}
	return append(left, right...), append(leftWarnings, rightWarnings...), nil
}

func ValidateBatch(in []Segment, out []string) error {
	if len(in) != len(out) {
		return fmt.Errorf("segment count mismatch: sent %d, got %d", len(in), len(out))
	}
	for i, segment := range in {
		got := out[i]
		if !samePlaceholderSet(placeholderSet(segment.Text), placeholderSet(got)) {
			return fmt.Errorf("segment %d: placeholder mismatch", i)
		}
		if strings.TrimSpace(segment.Text) != "" && strings.TrimSpace(got) == "" {
			return fmt.Errorf("segment %d: empty translation", i)
		}
		originalLen, translatedLen := len([]rune(segment.Text)), len([]rune(got))
		if originalLen > 0 {
			ratio := float64(translatedLen) / float64(originalLen)
			if ratio > 5 || ratio < 0.125 {
				return fmt.Errorf("segment %d: suspicious length ratio %.2f", i, ratio)
			}
		}
		if containsRefusal(got) {
			return fmt.Errorf("segment %d: model refusal detected", i)
		}
	}
	return nil
}
func placeholderSet(value string) map[string]int {
	out := map[string]int{}
	for _, token := range placeholderTokenRE.FindAllString(value, -1) {
		out[token]++
	}
	return out
}
func samePlaceholderSet(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}
func containsRefusal(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "as an ai language model") || strings.Contains(lower, "i cannot translate") || strings.Contains(value, "无法翻译")
}

type Fingerprint struct {
	Headings, CodeBlocks                                       []string
	Links, Images, ListItems, TableRows, Footnotes, MathBlocks int
}

func MarkdownFingerprint(markdown string) Fingerprint {
	var f Fingerprint
	inFence := false
	var code strings.Builder
	for _, line := range strings.SplitAfter(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if fenceRE.MatchString(line) {
			if inFence {
				sum := sha256.Sum256([]byte(code.String()))
				f.CodeBlocks = append(f.CodeBlocks, fmt.Sprintf("%x", sum))
				code.Reset()
			}
			inFence = !inFence
			continue
		}
		if inFence {
			code.WriteString(line)
			continue
		}
		if m := headingRE.FindStringSubmatch(trimmed); len(m) > 1 {
			f.Headings = append(f.Headings, fmt.Sprint(len(strings.TrimRight(m[1], " \t"))))
		}
		if listRE.MatchString(line) {
			f.ListItems++
		}
		if isTableRow(line) && !isTableSeparator(line) {
			f.TableRows++
		}
		if footnoteRE.MatchString(line) {
			f.Footnotes++
		}
		f.MathBlocks += strings.Count(line, "$$") / 2
	}
	f.Images = strings.Count(markdown, "![")
	f.Links = strings.Count(markdown, "](") - f.Images
	return f
}
func CompareFingerprint(source, translated string) error {
	a, b := MarkdownFingerprint(source), MarkdownFingerprint(translated)
	if !equalStrings(a.Headings, b.Headings) {
		return errors.New("translated Markdown changed heading structure")
	}
	if !equalStrings(a.CodeBlocks, b.CodeBlocks) {
		return errors.New("translated Markdown changed a code block")
	}
	return nil
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
