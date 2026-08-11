package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

var (
	ErrInvalidProvider = errors.New("invalid AI provider configuration")
	ErrKeyMissing      = errors.New("AI provider key is missing")
	ErrProviderFailed  = errors.New("AI provider request failed")
)

const (
	maxProviderErrorBodyBytes    = 16 << 10
	maxProviderErrorSummaryRunes = 240
)

var (
	bearerCredentialPattern    = regexp.MustCompile(`(?i)\bbearer[ \t]+[^,;\r\n]+`)
	labeledCredentialPattern   = regexp.MustCompile(`(?i)\b(api[ _-]*key|access[ _-]*token|refresh[ _-]*token|token|secret|client[ _-]*secret|credentials?|password|authorization)\b( +(is|was|provided|value))? *[:=] *("[^"]*"|'[^']*'|[^,;\r\n]+)`)
	describedCredentialPattern = regexp.MustCompile(`(?i)\b(api[ _-]*key|access[ _-]*token|refresh[ _-]*token|token|secret|client[ _-]*secret|credentials?|password|authorization)\b +(is|was|provided|value) +("[^"]*"|'[^']*'|[^,;\r\n]+)`)
	genericAIKeyPattern        = regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9._-]{8,}\b`)
)

type providerRequestError struct {
	status    int
	reason    string
	retryable bool
}

func (e *providerRequestError) Error() string {
	if e.status > 0 {
		if e.reason != "" {
			return fmt.Sprintf("%s: HTTP %d: %s", ErrProviderFailed, e.status, e.reason)
		}
		return fmt.Sprintf("%s: HTTP %d", ErrProviderFailed, e.status)
	}
	return fmt.Sprintf("%s: %s", ErrProviderFailed, e.reason)
}

func (e *providerRequestError) Unwrap() error { return ErrProviderFailed }

func RetryableProviderError(err error) bool {
	var requestError *providerRequestError
	return errors.As(err, &requestError) && requestError.retryable
}

// SafeProviderErrorMessage exposes details only from the private error type
// whose upstream summary has passed the bounded sanitizer. Arbitrary wrappers
// around ErrProviderFailed receive the generic message instead.
func SafeProviderErrorMessage(err error) string {
	var requestError *providerRequestError
	if !errors.As(err, &requestError) {
		return ErrProviderFailed.Error()
	}
	return requestError.Error()
}

type Client struct {
	HTTPClient *http.Client
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Thinking    *thinkingMode `json:"thinking,omitempty"`
}

// thinkingMode is intentionally narrow. DeepSeek V4 enables thinking by
// default, which can exhaust a small connectivity-check budget before it
// emits any visible assistant content. Translation does not benefit from the
// hidden reasoning channel, so disable it for the official V4 endpoint while
// leaving every other OpenAI-compatible provider request untouched.
type thinkingMode struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message      ChatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
}

func (c Client) Chat(ctx context.Context, provider domain.AIProviderConfig, apiKey string, messages []ChatMessage, maxTokens int) (string, error) {
	endpoint, err := chatEndpoint(provider)
	if err != nil {
		return "", ErrInvalidProvider
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", ErrKeyMissing
	}
	requestBody, err := json.Marshal(chatRequest{
		Model:       provider.Model,
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   maxTokens,
		Thinking:    disabledThinkingMode(provider),
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTPClient
	if httpClient == nil {
		timeout := time.Duration(provider.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 45 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		return "", &providerRequestError{reason: "connection error", retryable: ctx.Err() == nil}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		summary := providerErrorSummary(response.Body, response.Header.Get("Content-Type"), apiKey)
		retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return "", &providerRequestError{status: response.StatusCode, reason: summary, retryable: retryable}
	}
	const maxResponseBytes = 8 << 20
	responseData, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(responseData) > maxResponseBytes {
		return "", &providerRequestError{reason: "invalid response", retryable: true}
	}
	var result chatResponse
	decoder := json.NewDecoder(bytes.NewReader(responseData))
	if err := decoder.Decode(&result); err != nil {
		return "", &providerRequestError{reason: "invalid response", retryable: true}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", &providerRequestError{reason: "invalid response", retryable: true}
	}
	if len(result.Choices) == 0 {
		return "", &providerRequestError{reason: "empty response", retryable: true}
	}
	// A syntactically valid 2xx response is not necessarily complete. In
	// particular, OpenAI-compatible APIs return finish_reason=length when an
	// output cap truncates a response. Persisting that partial Markdown would
	// silently corrupt a translation, so accept only an explicit normal stop.
	// Do not retry a completed-but-truncated response at the same budget: the
	// caller must reduce its chunk size before making another request.
	if finishReason := strings.TrimSpace(strings.ToLower(result.Choices[0].FinishReason)); finishReason != "stop" && finishReason != "end_turn" {
		return "", &providerRequestError{reason: "incomplete response", retryable: false}
	}
	if strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", &providerRequestError{reason: "empty response", retryable: true}
	}
	return result.Choices[0].Message.Content, nil
}

func providerErrorSummary(body io.Reader, contentType, apiKey string) string {
	data, err := io.ReadAll(io.LimitReader(body, maxProviderErrorBodyBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxProviderErrorBodyBytes || !utf8.Valid(data) {
		return ""
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" || !safeProviderErrorText(raw) || looksLikeHTML(raw) {
		return ""
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)
	var summary string
	switch {
	case isJSONMediaType(mediaType):
		summary = providerJSONErrorSummary(data)
	case mediaType == "text/plain":
		if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
			summary = providerJSONErrorSummary(data)
		} else {
			summary = raw
		}
	case mediaType == "":
		if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
			summary = providerJSONErrorSummary(data)
		} else {
			summary = raw
		}
	default:
		return ""
	}
	return sanitizeProviderErrorSummary(summary, apiKey)
}

func isJSONMediaType(mediaType string) bool {
	return mediaType == "application/json" || mediaType == "text/json" || strings.HasSuffix(mediaType, "+json")
}

func providerJSONErrorSummary(data []byte) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return ""
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ""
	}
	return providerErrorEnvelopeText(value)
}

func providerErrorEnvelopeText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		if nested, exists := typed["error"]; exists {
			if text := providerErrorValueText(nested); text != "" {
				return text
			}
		}
		for _, key := range []string{"message", "detail", "error_description", "description", "title"} {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
		if nested, exists := typed["errors"]; exists {
			return providerErrorValueText(nested)
		}
	case []any:
		for _, item := range typed {
			if text := providerErrorValueText(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func providerErrorValueText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"message", "detail", "error_description", "description", "title", "code", "type"} {
			if text, ok := typed[key].(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	case []any:
		for _, item := range typed {
			if text := providerErrorValueText(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func sanitizeProviderErrorSummary(summary, apiKey string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" || !safeProviderErrorText(summary) || looksLikeHTML(summary) {
		return ""
	}
	if apiKey = strings.TrimSpace(apiKey); apiKey != "" {
		summary = strings.ReplaceAll(summary, apiKey, "[REDACTED]")
	}
	summary = strings.Join(strings.Fields(summary), " ")
	summary = bearerCredentialPattern.ReplaceAllString(summary, "Bearer [REDACTED]")
	summary = labeledCredentialPattern.ReplaceAllString(summary, "$1=[REDACTED]")
	summary = describedCredentialPattern.ReplaceAllString(summary, "$1 [REDACTED]")
	summary = genericAIKeyPattern.ReplaceAllString(summary, "[REDACTED]")
	runes := []rune(summary)
	if len(runes) > maxProviderErrorSummaryRunes {
		summary = string(runes[:maxProviderErrorSummaryRunes-1]) + "…"
	}
	return summary
}

func safeProviderErrorText(value string) bool {
	for _, character := range value {
		if character == '\n' || character == '\r' || character == '\t' {
			continue
		}
		// Reject non-printing Unicode format characters too (for example bidi
		// overrides), not only ASCII/C1 controls. Those characters can disguise
		// credential text in an otherwise printable upstream diagnostic.
		if !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func looksLikeHTML(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "\uFEFF")))
	for offset := 0; offset < len(lower); {
		index := strings.IndexByte(lower[offset:], '<')
		if index < 0 {
			return false
		}
		index += offset
		if index+1 < len(lower) {
			next := lower[index+1]
			if next == '!' || next == '/' || next >= 'a' && next <= 'z' {
				return true
			}
		}
		offset = index + 1
	}
	return false
}

func disabledThinkingMode(provider domain.AIProviderConfig) *thinkingMode {
	base, err := url.Parse(strings.TrimSpace(provider.BaseURL))
	if err != nil || !strings.EqualFold(base.Hostname(), "api.deepseek.com") {
		return nil
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(provider.Model)), "deepseek-v4-") {
		return nil
	}
	return &thinkingMode{Type: "disabled"}
}

func chatEndpoint(provider domain.AIProviderConfig) (string, error) {
	base, err := url.Parse(strings.TrimSpace(provider.BaseURL))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil || strings.TrimSpace(provider.Model) == "" {
		return "", ErrInvalidProvider
	}
	base.RawQuery = ""
	base.Fragment = ""
	base.Path = strings.TrimRight(base.Path, "/") + "/chat/completions"
	return base.String(), nil
}
