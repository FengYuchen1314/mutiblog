package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fengyuchen/mutiblog/internal/model"
)

type fakeProvider struct {
	calls     int
	malformed bool
}

func (f *fakeProvider) Name() string               { return "fake" }
func (f *fakeProvider) Test(context.Context) error { return nil }

func (f *fakeProvider) Translate(
	_ context.Context,
	segs []Segment,
	_ model.Locale,
	_ model.Locale,
	_ string,
) ([]string, error) {
	f.calls++
	out := make([]string, len(segs))
	for i, segment := range segs {
		if f.malformed && strings.Contains(segment.Text, "坏") {
			out[i] = ""
			continue
		}
		out[i] = "译" + segment.Text
	}
	return out, nil
}

func TestTranslateMarkdownKeepsProtectedContent(t *testing.T) {
	provider := &fakeProvider{}
	input := "# 标题\n\n运行 `go test ./...`，参见 [文档](https://example.com/docs)。\n\n" +
		"```go\nfmt.Println(\"unchanged\")\n```\n"
	result, err := TranslateMarkdown(context.Background(), provider, input, "zh-CN", "en", "标题", 20)
	if err != nil {
		t.Fatal(err)
	}
	wants := []string{"# 译标题", "`go test ./...`", "https://example.com/docs", "fmt.Println(\"unchanged\")"}
	for _, want := range wants {
		if !strings.Contains(result.Markdown, want) {
			t.Fatalf("missing %q in %s", want, result.Markdown)
		}
	}
	if provider.calls < 2 {
		t.Fatalf("expected batching, calls=%d", provider.calls)
	}
}

func TestTranslateMarkdownRetainsSingleInvalidSegment(t *testing.T) {
	provider := &fakeProvider{malformed: true}
	input := "# 标题\n\n甲\n\n乙\n\n丙\n\n丁\n\n坏\n"
	result, err := TranslateMarkdown(context.Background(), provider, input, "zh-CN", "en", "标题", 200)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Markdown, "\n坏\n") || len(result.Warnings) != 1 {
		t.Fatalf("result=%s warnings=%v", result.Markdown, result.Warnings)
	}
	if provider.calls < 4 {
		t.Fatalf("calls=%d, expected retries and split (%s)", provider.calls, fmt.Sprint(result.Warnings))
	}
}
