package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/model"
)

func TestOpenAICompatibleFallsBackFromJSONMode(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if calls == 1 {
			if !strings.Contains(string(body), "response_format") {
				t.Fatal("first request omitted JSON mode")
			}
			http.Error(w, "response_format unsupported", http.StatusBadRequest)
			return
		}
		if strings.Contains(string(body), "response_format") {
			t.Fatal("fallback still sent JSON mode")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "```json\n{\"segments\":[\"你好 ⟦P1⟧\"]}\n```"}}},
			"usage":   map[string]int{"prompt_tokens": 3, "completion_tokens": 4},
		})
	}))
	defer server.Close()
	p, err := NewOpenAICompatible(config.AIConfig{BaseURL: server.URL, APIKey: "test-key", Model: "fake", Timeout: config.Duration(time.Second), MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Translate(context.Background(), []Segment{{Text: "Hello ⟦P1⟧"}}, model.Locale("en"), model.Locale("zh-CN"), "title")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0] != "你好 ⟦P1⟧" || calls != 2 {
		t.Fatalf("out=%q calls=%d", out, calls)
	}
	if usage := p.LastUsage(); usage.PromptTokens != 3 || usage.CompletionTokens != 4 {
		t.Fatalf("usage=%+v", usage)
	}
}
