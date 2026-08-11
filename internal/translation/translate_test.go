package translation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestMarkdownProtectionRoundTrip(t *testing.T) {
	source := "# 标题\n\n```go\nfmt.Println(\"hello\")\n```\n\n访问 [站点](https://example.com/a?q=1) 和 `inline()`，公式 $x+y$。"
	protected, values, err := protectMarkdown(source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(protected, "fmt.Println") || strings.Contains(protected, "https://example.com") || len(values) != 4 {
		t.Fatalf("protected = %q, values = %#v", protected, values)
	}
	restored, err := restoreMarkdown(protected, values)
	if err != nil || restored != source {
		t.Fatalf("restored = %q, err = %v", restored, err)
	}
	if _, err := restoreMarkdown(strings.Replace(protected, values[0].Token, "removed", 1), values); !errors.Is(err, ErrUnsafeOutput) {
		t.Fatalf("unsafe output error = %v", err)
	}
}

func TestMarkdownProtectionPreservesMeaningfulOuterWhitespace(t *testing.T) {
	source := "    indented code\nline with hard break  "
	protected, values, err := protectMarkdown(source)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreMarkdown(protected, values)
	if err != nil || restored != source {
		t.Fatalf("restored = %q, err = %v", restored, err)
	}
}

func TestMarkdownProtectionPreservesFencedBlockWithoutFinalNewline(t *testing.T) {
	source := "before\n\n```go\nfmt.Println(\"hello\")\n```"
	protected, values, err := protectMarkdown(source)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreMarkdown(protected, values)
	if err != nil || restored != source {
		t.Fatalf("restored = %q, err = %v", restored, err)
	}
}

func TestSegmentMarkdownRespectsLimit(t *testing.T) {
	chunks := segmentMarkdown(strings.Repeat("段落内容", 120)+"\n\n"+strings.Repeat("second ", 80), 100)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > 100 {
			t.Fatalf("chunk exceeds limit: %d", len([]rune(chunk)))
		}
	}
}

func TestSegmentMarkdownPreservesParagraphBoundariesAndProtectedTokens(t *testing.T) {
	token := "MUTIBLOG_PROTECTED_0123456789ab_000001"
	source := strings.Repeat("alpha ", 12) + token + strings.Repeat(" beta", 12) + "\n\nsecond paragraph"
	parts := segmentMarkdownParts(source, 64)
	if len(parts) < 2 {
		t.Fatalf("parts = %#v", parts)
	}
	var rebuilt strings.Builder
	for _, part := range parts {
		if strings.Contains(part.Text, "MUTIBLOG_PROTECTED") && !strings.Contains(part.Text, token) {
			t.Fatalf("protected token was split: %#v", parts)
		}
		rebuilt.WriteString(part.Text)
		rebuilt.WriteString(part.Separator)
	}
	if rebuilt.String() != source || strings.Count(rebuilt.String(), token) != 1 {
		t.Fatalf("rebuilt Markdown = %q", rebuilt.String())
	}
}

func TestSegmentMarkdownPreservesEveryBoundaryByte(t *testing.T) {
	tests := []string{
		"alpha beta gamma delta",
		"alpha  \nbeta\tgamma",
		"alpha   beta",
		"\t\tindented content followed by words",
		"first\n\nsecond\n\nthird",
	}
	for _, source := range tests {
		parts := segmentMarkdownParts(source, 9)
		var rebuilt strings.Builder
		for _, part := range parts {
			rebuilt.WriteString(part.Text)
			rebuilt.WriteString(part.Separator)
		}
		if rebuilt.String() != source {
			t.Fatalf("source = %q, rebuilt = %q, parts = %#v", source, rebuilt.String(), parts)
		}
	}
}

func TestDecodeJSONObjectRejectsTrailingObjects(t *testing.T) {
	var value translatedMetadata
	if err := decodeJSONObject(`{"title":"ok","summary":"","seoTitle":"","seoDescription":""}{"title":"second"}`, &value); err == nil {
		t.Fatal("multiple JSON objects were accepted")
	}
	if err := decodeJSONObject("Here is the result: {\"title\":\"ok\",\"summary\":\"\",\"seoTitle\":\"\",\"seoDescription\":\"\"}", &value); err == nil {
		t.Fatal("JSON wrapped in provider commentary was accepted")
	}
}

func TestTranslationBudgetsNeverRelyOnProviderClamping(t *testing.T) {
	for _, test := range []struct {
		name          string
		providerLimit int
	}{
		{name: "small", providerLimit: 256},
		{name: "qwen-default", providerLimit: 8192},
		{name: "large", providerLimit: 65536},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			chunkRunes := translationChunkRuneLimit(test.providerLimit)
			if chunkRunes < 1 || chunkRunes > translationChunkRunes {
				t.Fatalf("chunk rune limit = %d for provider limit %d", chunkRunes, test.providerLimit)
			}
			if budget := translationChunkTokenBudget(chunkRunes, test.providerLimit); budget > test.providerLimit {
				t.Fatalf("chunk budget = %d, provider limit = %d", budget, test.providerLimit)
			}
			if budget := translationMetadataTokenBudget(test.providerLimit); budget > test.providerLimit {
				t.Fatalf("metadata budget = %d, provider limit = %d", budget, test.providerLimit)
			}
		})
	}
	if got := translationChunkRuneLimit(8192); got >= translationChunkRunes {
		t.Fatalf("8192-token provider must reduce the default chunk: %d", got)
	}
	source := domain.LocalizedMarkdown{Markdown: strings.Repeat("x", 5000)}
	if got, want := translationWorkUnits(source, 8192), 4; got != want {
		t.Fatalf("Qwen work units = %d, want %d", got, want)
	}
}

func TestMarkdownProtectionRoundTripsGoogleSensitiveStructures(t *testing.T) {
	source := strings.Join([]string{
		"阅读 [中文文档][]、[快捷文档] 和 ![中文图片][]。  ",
		"下一行含 <https://example.com/a_(b)> 与裸链接 https://example.com/a_(b).",
		"",
		"[中文文档]: https://example.com/docs \"标题\"",
		"[快捷文档]: https://example.com/quick",
		"[中文图片]: https://example.com/image.png",
		"",
		"````go",
		"fmt.Println(`多反引号 $$不翻译$$`)",
		"````",
		"",
		"    indentedCode(\"保持\")",
		"",
		"行内 ``code ` span``、$x+y$ 与：",
		"$$",
		"x + y = z",
		"$$",
	}, "\n")
	protected, values, err := protectMarkdown(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, unsafe := range []string{"https://example.com", "fmt.Println", "indentedCode", "code ` span", "x + y = z"} {
		if strings.Contains(protected, unsafe) {
			t.Fatalf("protected Markdown leaked %q: %q", unsafe, protected)
		}
	}
	restored, err := restoreMarkdown(protected, values)
	if err != nil || restored != source {
		t.Fatalf("restored = %q, err = %v", restored, err)
	}
}

func TestMarkdownProtectionSafelyRestoresNestedTokens(t *testing.T) {
	source := strings.Join([]string{
		"`[id] and $$inside code$$`",
		"$$[id] and https://example.com/math$$",
		`<span title="[id] https://example.com/html">文字</span>`,
		`$\href{https://example.com/math-link}{x}$`,
		"",
		"[id]: https://example.com/reference",
	}, "\n")
	protected, values, err := protectMarkdown(source)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := restoreMarkdown(protected, values)
	if err != nil || restored != source {
		t.Fatalf("nested token restore = %q, err = %v, protected = %q, values = %#v", restored, err, protected, values)
	}
}

func TestTranslationCanonicalizesReferenceLinksWithoutFreezingVisibleLabels(t *testing.T) {
	source := strings.Join([]string{
		"[快捷]、[折叠][]、![图片][]、[可见][快捷]、\\[快捷][] 与 [同名](https://example.com/inline)",
		"",
		"[快捷]: https://example.com/shortcut",
		"[折叠]: https://example.com/collapsed",
		"[图片]: https://example.com/image.png",
		"[同名]: https://example.com/reference",
	}, "\n")
	protected, values, err := protectMarkdownForTranslation(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, visible := range []string{"快捷", "折叠", "图片", "同名"} {
		if !strings.Contains(protected, visible) {
			t.Fatalf("visible reference label %q was protected: %q", visible, protected)
		}
	}
	providerOutput := strings.NewReplacer("快捷", "Shortcut", "折叠", "Collapsed", "图片", "Image", "可见", "Visible", "同名", "Inline").Replace(protected)
	restored, err := restoreMarkdown(providerOutput, values)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"[Shortcut][快捷]", "[Collapsed][折叠]", "![Image][图片]", "[Visible][快捷]", `\[Shortcut][]`, "[Inline](https://example.com/inline)",
		"[快捷]: https://example.com/shortcut", "[折叠]: https://example.com/collapsed",
	} {
		if !strings.Contains(restored, expected) {
			t.Fatalf("canonical reference missing %q: %q", expected, restored)
		}
	}
}

func TestRestoreMarkdownRejectsMissingReorderedAndInjectedTokens(t *testing.T) {
	protected, values, err := protectMarkdown("`one` and `two`")
	if err != nil || len(values) != 2 {
		t.Fatalf("protectMarkdown() values = %#v, err = %v", values, err)
	}
	reordered := strings.NewReplacer(values[0].Token, "MUTIBLOG_SWAP", values[1].Token, values[0].Token, "MUTIBLOG_SWAP", values[1].Token).Replace(protected)
	for _, unsafe := range []string{
		strings.Replace(protected, values[0].Token, "", 1),
		reordered,
		protected + " MUTIBLOG_PROTECTED_deadbeefcafe_999999",
	} {
		if _, err := restoreMarkdown(unsafe, values); !errors.Is(err, ErrUnsafeOutput) {
			t.Fatalf("unsafe tokens were accepted: %q, err = %v", unsafe, err)
		}
	}
}

func TestGoogleFreeStructuredTranslationPreservesMarkdownAndWhitespace(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/translate_a/single" || r.Header.Get("Authorization") != "" {
			t.Fatalf("request = %s, authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		source := r.Form.Get("q")
		if utf8.RuneCountInString(source) > ai.GoogleFreeMaxInputRunes || r.ContentLength > 60000 {
			t.Fatalf("Google request exceeded bounds: runes=%d bytes=%d", utf8.RuneCountInString(source), r.ContentLength)
		}
		for _, protected := range []string{"MUTIBLOG_PROTECTED_", "fmt.Println", "https://example.com", "$x+y$"} {
			if strings.Contains(source, protected) {
				t.Fatalf("protected Markdown reached Google: %q", source)
			}
		}
		if strings.Contains(source, "](") {
			t.Fatalf("inline link delimiters reached Google: %q", source)
		}
		translated := strings.NewReplacer("标题", "Title", "摘要", "Summary", "正文", "Body", "继续", "Continue", "链接", "Link", "中文文档", "Docs").Replace(source)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{translated, source}}})
	}))
	defer server.Close()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	aiService := ai.NewService(repository, ai.Client{HTTPClient: server.Client()})
	if _, err := aiService.Upsert("google-free", ai.UpsertProviderInput{
		Name: "Google", Kind: ai.ProviderKindGoogleFree, BaseURL: server.URL + "/translate_a/single",
		Model: ai.GoogleFreeDefaultModel, Enabled: true, Default: true,
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository, nil, aiService, nil)
	defer service.Close()
	longText := strings.Repeat("长😀", 2100)
	source := domain.LocalizedMarkdown{
		Title: "标题", Summary: "摘要",
		Markdown: longText + "\r\n\r\n# 正文\n\nUse `x` now  \n继续 [链接](https://example.com/a_(b))，阅读 [中文文档][]。\n\n[中文文档]: https://example.com/docs\n\n```go\nfmt.Println(\"保持\")\n```\n\n公式 $x+y$。\r\n" + longText,
	}
	translated, err := service.translate(context.Background(), "google-free", "zh-CN", "en", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if translated.Title != "Title" || translated.Summary != "Summary" || !strings.Contains(translated.Markdown, "# Body") || !strings.Contains(translated.Markdown, "Continue") {
		t.Fatalf("translated = %#v", translated)
	}
	for _, exact := range []string{"Use `x` now  \n", "[Link](https://example.com/a_(b))", "[Docs][中文文档]", "[中文文档]: https://example.com/docs", "fmt.Println(\"保持\")", "$x+y$"} {
		if !strings.Contains(translated.Markdown, exact) {
			t.Fatalf("translated Markdown lost %q: %q", exact, translated.Markdown)
		}
	}
	if strings.Count(translated.Markdown, "长😀") != 4200 || !strings.Contains(translated.Markdown, "长😀\r\n\r\n# Body") {
		t.Fatalf("long Markdown was lost or reordered")
	}
	if requests < 7 {
		t.Fatalf("native Google requests = %d", requests)
	}
}

func TestGoogleCanaryRejectsEmptyOrAlteredTranslation(t *testing.T) {
	for _, test := range []struct {
		name      string
		transform func(string) string
	}{
		{name: "empty-core", transform: func(source string) string {
			lines := strings.Split(source, "\n")
			return lines[0] + "\n\n" + lines[len(lines)-1]
		}},
		{name: "altered-end-canary", transform: func(source string) string { return strings.Replace(source, "_END", "_FIN", 1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				source := r.Form.Get("q")
				_ = json.NewEncoder(w).Encode([]any{[]any{[]any{test.transform(source), source}}})
			}))
			defer server.Close()
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			aiService := ai.NewService(repository, ai.Client{HTTPClient: server.Client()})
			if _, err := aiService.Upsert("google", ai.UpsertProviderInput{Name: "Google", Kind: ai.ProviderKindGoogleFree, BaseURL: server.URL + "/translate_a/single", Enabled: true, Default: true}); err != nil {
				t.Fatal(err)
			}
			service := NewService(repository, nil, aiService, nil)
			defer service.Close()
			if _, err := service.translateGoogleTextPart(context.Background(), "google", "zh-CN", "en", "  正文\t"); !errors.Is(err, ErrUnsafeOutput) {
				t.Fatalf("canary error = %v", err)
			}
		})
	}
}

func TestGoogleProviderRetryIsBoundedAndCancelable(t *testing.T) {
	for _, test := range []struct {
		name       string
		failures   int
		cancelWait bool
		wantCalls  int
		wantError  bool
	}{
		{name: "third-attempt-succeeds", failures: 2, wantCalls: 3},
		{name: "three-failures-stop", failures: 3, wantCalls: 3, wantError: true},
		{name: "cancel-during-backoff", failures: 3, cancelWait: true, wantCalls: 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls <= test.failures {
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				source := r.Form.Get("q")
				_ = json.NewEncoder(w).Encode([]any{[]any{[]any{source, source}}})
			}))
			defer server.Close()
			repository, err := fsrepo.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			aiService := ai.NewService(repository, ai.Client{HTTPClient: server.Client()})
			if _, err := aiService.Upsert("google", ai.UpsertProviderInput{Name: "Google", Kind: ai.ProviderKindGoogleFree, BaseURL: server.URL + "/translate_a/single", Enabled: true, Default: true}); err != nil {
				t.Fatal(err)
			}
			service := NewService(repository, nil, aiService, nil)
			defer service.Close()
			ctx, cancel := context.WithCancel(context.Background())
			service.retryWait = func(ctx context.Context, _ time.Duration) error {
				if test.cancelWait {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			_, err = service.translateProviderWithRetry(ctx, "google", "zh-CN", "en", "正文")
			cancel()
			if calls != test.wantCalls || (err != nil) != test.wantError {
				t.Fatalf("calls = %d, err = %v", calls, err)
			}
		})
	}
}
