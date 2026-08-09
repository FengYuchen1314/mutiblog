package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/model"
)

type Provider interface {
	Translate(context.Context, []Segment, model.Locale, model.Locale, string) ([]string, error)
	Test(context.Context) error
	Name() string
}

type Usage struct{ PromptTokens, CompletionTokens int }

type usageProvider interface{ LastUsage() Usage }

// OpenAICompatible implements the small common subset shared by OpenAI-style
// chat-completions providers. It never includes credentials in returned errors.
type OpenAICompatible struct {
	baseURL, apiKey, model, prompt string
	temperature                    float64
	maxTokens, maxRetries, rpm     int
	client                         *http.Client
	noJSONMode                     atomic.Bool
	mu                             sync.Mutex
	nextRequest                    time.Time
	lastUsage                      Usage
}

func NewOpenAICompatible(cfg config.AIConfig) (*OpenAICompatible, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("AI provider requires baseURL, apiKey, and model")
	}
	timeout := time.Duration(cfg.Timeout)
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	baseURL := strings.TrimSpace(cfg.BaseURL)
	// docs/13 P9: users paste all four spellings; only the /v1 form is right.
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/chat/completions")
	baseURL = strings.TrimRight(baseURL, "/")
	return &OpenAICompatible{baseURL: baseURL, apiKey: cfg.APIKey, model: cfg.Model, prompt: cfg.SystemPromptOverride, temperature: cfg.Temperature, maxTokens: cfg.MaxTokensPerRequest, maxRetries: maxRetries, rpm: cfg.RateLimitRPM, client: &http.Client{Timeout: timeout}}, nil
}

func (p *OpenAICompatible) Name() string     { return "openai-compatible" }
func (p *OpenAICompatible) LastUsage() Usage { p.mu.Lock(); defer p.mu.Unlock(); return p.lastUsage }

func (p *OpenAICompatible) Test(ctx context.Context) error {
	// A settings-page connection check must fail promptly; regular translation
	// keeps the configured (normally 120s) request budget.
	testCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := p.Translate(testCtx, []Segment{{Text: "Hello"}}, "en", "zh-CN", "Connection test")
	return err
}

func (p *OpenAICompatible) Translate(ctx context.Context, segs []Segment, src, dst model.Locale, title string) ([]string, error) {
	values := make([]string, len(segs))
	for i, segment := range segs {
		values[i] = segment.Text
	}
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if err := p.wait(ctx); err != nil {
			return nil, err
		}
		out, usage, status, retryAfter, err := p.request(ctx, values, src, dst, title, p.temperature)
		if status == http.StatusBadRequest && !p.noJSONMode.Load() && unsupportedJSONMode(err) {
			p.noJSONMode.Store(true)
			continue
		}
		if err == nil {
			p.mu.Lock()
			p.lastUsage = usage
			p.mu.Unlock()
			return out, nil
		}
		if status != http.StatusTooManyRequests && (status < 500 || status > 599) {
			return nil, err
		}
		if attempt == p.maxRetries {
			return nil, err
		}
		wait := retryAfter
		if wait <= 0 {
			wait = time.Duration(1<<attempt) * 250 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, errors.New("AI provider exhausted retries")
}

func (p *OpenAICompatible) wait(ctx context.Context) error {
	if p.rpm <= 0 {
		return nil
	}
	p.mu.Lock()
	now := time.Now()
	when := p.nextRequest
	if when.Before(now) {
		when = now
	}
	p.nextRequest = when.Add(time.Minute / time.Duration(p.rpm))
	p.mu.Unlock()
	if delay := time.Until(when); delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil
}

func (p *OpenAICompatible) request(ctx context.Context, segments []string, src, dst model.Locale, title string, temperature float64) ([]string, Usage, int, time.Duration, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	body := map[string]any{
		"model":       p.model,
		"temperature": temperature,
		"messages":    []message{{Role: "system", Content: p.systemPrompt(src, dst)}, {Role: "user", Content: userPrompt(src, dst, title, segments)}},
	}
	if p.maxTokens > 0 {
		body["max_tokens"] = p.maxTokens
	}
	if !p.noJSONMode.Load() {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, Usage{}, 0, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, Usage{}, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	response, err := p.client.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, Usage{}, 0, 0, fmt.Errorf("AI 请求超时：请检查服务器网络或代理设置（%w）", err)
		}
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "tls") || strings.Contains(lower, "x509") || strings.Contains(lower, "certificate") || strings.Contains(lower, "handshake") {
			return nil, Usage{}, 0, 0, fmt.Errorf("TLS 握手失败，可能是中间人代理或证书问题（%w）", err)
		}
		if strings.Contains(lower, "no such host") || strings.Contains(lower, "dns") || strings.Contains(lower, "lookup") {
			return nil, Usage{}, 0, 0, fmt.Errorf("无法解析域名 %s，请检查 Base URL（%w）", p.baseURL, err)
		}
		return nil, Usage{}, 0, 0, fmt.Errorf("AI provider request failed: %w", err)
	}
	defer response.Body.Close()
	retryAfter := parseRetryAfter(response.Header.Get("Retry-After"))
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, Usage{}, response.StatusCode, retryAfter, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(data))
		if len(message) > 512 {
			message = message[:512]
		}
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, Usage{}, response.StatusCode, retryAfter, fmt.Errorf("API Key 无效或无权访问该模型（HTTP %d）：%s", response.StatusCode, message)
		case http.StatusNotFound:
			return nil, Usage{}, response.StatusCode, retryAfter, fmt.Errorf("端点未找到（HTTP 404）：请检查 Base URL 是否需要以 /v1 结尾。原始响应：%s", message)
		case http.StatusTooManyRequests:
			return nil, Usage{}, response.StatusCode, retryAfter, fmt.Errorf("AI 服务限流（HTTP 429），请稍后重试或降低并发：%s", message)
		}
		return nil, Usage{}, response.StatusCode, retryAfter, fmt.Errorf("AI provider returned HTTP %d: %s", response.StatusCode, message)
	}
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil || len(decoded.Choices) == 0 {
		return nil, Usage{}, response.StatusCode, retryAfter, errors.New("AI provider returned an invalid completion")
	}
	stringsOut, err := parseSegments(decoded.Choices[0].Message.Content)
	if err != nil {
		return nil, Usage{}, response.StatusCode, retryAfter, err
	}
	return stringsOut, Usage{PromptTokens: decoded.Usage.PromptTokens, CompletionTokens: decoded.Usage.CompletionTokens}, response.StatusCode, retryAfter, nil
}

func (p *OpenAICompatible) systemPrompt(src, dst model.Locale) string {
	if p.prompt != "" {
		return p.prompt
	}
	return fmt.Sprintf("You are a professional technical translator. Translate each input JSON string from %s to %s. Output ONLY {\"segments\":[...]} with the same number and order. Preserve every placeholder token like ⟦P1⟧ exactly. Preserve Markdown syntax, links, code, URLs, paths, formulas, product names, and technical identifiers; translate only human-readable text. Do not add explanations or summaries.", src, dst)
}
func userPrompt(src, dst model.Locale, title string, segments []string) string {
	b, _ := json.Marshal(map[string]any{"sourceLanguage": src, "targetLanguage": dst, "articleTitle": title, "segments": segments})
	return string(b)
}
func parseSegments(content string) ([]string, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(strings.TrimSpace(content), "```")
	var decoded struct {
		Segments []string `json:"segments"`
	}
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		return nil, errors.New("AI provider returned non-JSON segments")
	}
	return decoded.Segments, nil
}
func unsupportedJSONMode(err error) bool {
	if err == nil {
		return false
	}
	v := strings.ToLower(err.Error())
	return strings.Contains(v, "response_format") || strings.Contains(v, "unsupported")
}
func parseRetryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(time.Until(when), 0)
	}
	return 0
}
