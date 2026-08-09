package feed

import (
	"testing"

	"github.com/fengyuchen/mutiblog/internal/model"
)

func TestArticleAlternatesArePublishedAndStable(t *testing.T) {
	article := &model.Article{Versions: map[model.Locale]*model.ArticleVersion{
		"zh-CN": {Front: model.FrontMatter{Slug: "中文", Status: model.StatusPublished}},
		"en":    {Front: model.FrontMatter{Slug: "english", Status: model.StatusPublished}},
		"ja":    {Front: model.FrontMatter{Slug: "draft", Status: model.StatusDraft}},
	}}
	generator := Generator{BaseURL: "https://blog.example", DefaultLocale: "zh-CN", Prefixes: map[model.Locale]string{"zh-CN": "zh-cn", "en": "en"}}
	got := generator.articleAlternates(article)
	if len(got) != 3 {
		t.Fatalf("alternates=%#v", got)
	}
	if got[0].Lang != "en" || got[1].Lang != "zh-CN" || got[2].Lang != "x-default" {
		t.Fatalf("alternate ordering=%#v", got)
	}
	if got[2].Href != "https://blog.example/zh-cn/posts/中文/" {
		t.Fatalf("x-default URL=%q", got[2].Href)
	}
}
