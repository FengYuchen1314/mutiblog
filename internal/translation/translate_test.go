package translation

import (
	"errors"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
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
