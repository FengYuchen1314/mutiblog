package ai

import (
	"strings"
	"testing"
)

func TestExtractProtectsMarkdownStructure(t *testing.T) {
	md := "---\n" +
		"title: 原始标题\n---\n\n" +
		"# 安装 Docker\n\n" +
		"访问 [这里](https://example.com/a?b=c) 并执行 `docker compose up -d`。\n\n" +
		"![封面图](/media/cover.png)\n\n" +
		"| 名称 | 地址 |\n| --- | --- |\n| 服务 | 192.168.1.1:8080 |\n\n" +
		"- [x] 完成任务\n> 引用 $E = mc^2$\n\n" +
		"```go\n// 不翻译\nfmt.Println(\"hello\")\n```\n"
	segs, err := Extract(md, ExtractOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 9 {
		t.Fatalf("segments = %d: %#v", len(segs), segs)
	}
	for _, segment := range segs {
		if strings.Contains(segment.Text, "docker compose") || strings.Contains(segment.Text, "https://") || strings.Contains(segment.Text, "192.168") || strings.Contains(segment.Text, "$E =") {
			t.Fatalf("unprotected content in %q", segment.Text)
		}
	}
	translated := make([]string, len(segs))
	for i, segment := range segs {
		translated[i] = "译文 " + segment.Text
	}
	got, err := Apply(md, segs, translated)
	if err != nil {
		t.Fatal(err)
	}
	for _, protected := range []string{"docker compose up -d", "https://example.com/a?b=c", "/media/cover.png", "192.168.1.1:8080", "$E = mc^2$", "// 不翻译"} {
		if !strings.Contains(got, protected) {
			t.Fatalf("lost protected content %q in %s", protected, got)
		}
	}
	if !strings.Contains(got, "# 译文 安装 Docker") || !strings.Contains(got, "- [x] 译文 完成任务") {
		t.Fatalf("structure changed: %s", got)
	}
}

func TestBatchKeepsOversizeSegmentWhole(t *testing.T) {
	segs := []Segment{{Text: "aa"}, {Text: "bbb"}, {Text: strings.Repeat("x", 9)}}
	batches := Batch(segs, 5)
	if len(batches) != 2 || len(batches[0]) != 2 || len(batches[1]) != 1 {
		t.Fatalf("batches = %#v", batches)
	}
}
