package importer

import (
	"testing"
	"time"

	"github.com/fengyuchen/mutiblog/internal/model"
)

func TestParseMarkdownSupportsExternalFrontMatterFormats(t *testing.T) {
	when := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	cases := []struct {
		name, raw, title, slug string
		status                 model.Status
	}{
		{"yaml", "---\ntitle: YAML post\nslug: yaml-post\nstatus: published\n---\n\nbody", "YAML post", "yaml-post", model.StatusPublished},
		{"hugo", "+++\ntitle = \"Hugo post\"\ndraft = false\n+++\n\nbody", "Hugo post", "hugo-post", model.StatusPublished},
		{"plain", "body", "plain", "plain", model.StatusDraft},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			front, body, err := ParseMarkdown(File{Name: "plain.md", Data: []byte(tc.raw), ModTime: when}, "en", "")
			if err != nil || front.Title != tc.title || front.Slug != tc.slug || front.Status != tc.status || body != "body" {
				t.Fatalf("front=%#v body=%q err=%v", front, body, err)
			}
		})
	}
}

func TestParseMarkdownForcedStatusWins(t *testing.T) {
	front, _, err := ParseMarkdown(File{Name: "post.md", Data: []byte("---\ntitle: Post\nstatus: published\n---\n\nbody")}, "en", model.StatusDraft)
	if err != nil || front.Status != model.StatusDraft {
		t.Fatalf("front=%#v err=%v", front, err)
	}
}
