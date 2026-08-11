package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

var (
	ErrInvalidProvider = errors.New("invalid AI provider configuration")
	ErrProviderFailed  = errors.New("AI provider request failed")
)

type providerRequestError struct {
	status    int
	reason    string
	retryable bool
}

func (e *providerRequestError) Error() string {
	if e.status > 0 {
		return fmt.Sprintf("%s: HTTP %d", ErrProviderFailed, e.status)
	}
	return fmt.Sprintf("%s: %s", ErrProviderFailed, e.reason)
}

func (e *providerRequestError) Unwrap() error { return ErrProviderFailed }

func RetryableProviderError(err error) bool {
	var requestError *providerRequestError
	return errors.As(err, &requestError) && requestError.retryable
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
}

type chatResponse struct {
	Choices []struct {
		Message ChatMessage `json:"message"`
	} `json:"choices"`
}

func (c Client) Chat(ctx context.Context, provider domain.AIProviderConfig, apiKey string, messages []ChatMessage, maxTokens int) (string, error) {
	endpoint, err := chatEndpoint(provider)
	if err != nil || strings.TrimSpace(apiKey) == "" {
		return "", ErrInvalidProvider
	}
	requestBody, err := json.Marshal(chatRequest{Model: provider.Model, Messages: messages, Temperature: 0.1, MaxTokens: maxTokens})
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
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return "", &providerRequestError{status: response.StatusCode, retryable: retryable}
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
	if len(result.Choices) == 0 || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", &providerRequestError{reason: "empty response", retryable: true}
	}
	return result.Choices[0].Message.Content, nil
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
