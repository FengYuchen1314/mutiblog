package domain

import "time"

type ContentStatus string

const (
	ContentStatusDraft       ContentStatus = "draft"
	ContentStatusPublished   ContentStatus = "published"
	ContentStatusUnpublished ContentStatus = "unpublished"
	ContentStatusRecycled    ContentStatus = "recycled"
)

type LocaleOrigin string

const (
	LocaleOriginSource LocaleOrigin = "source"
	LocaleOriginAI     LocaleOrigin = "ai"
	LocaleOriginManual LocaleOrigin = "manual"
)

type LocaleContentState struct {
	State          string       `yaml:"state" json:"state"`
	Origin         LocaleOrigin `yaml:"origin" json:"origin"`
	Revision       int          `yaml:"revision" json:"revision"`
	SourceRevision int          `yaml:"sourceRevision" json:"sourceRevision"`
}

type PostMeta struct {
	SchemaVersion         int                           `yaml:"schemaVersion" json:"schemaVersion"`
	Kind                  string                        `yaml:"kind" json:"kind"`
	ID                    string                        `yaml:"id" json:"id"`
	Status                ContentStatus                 `yaml:"status" json:"status"`
	SourceLocale          string                        `yaml:"sourceLocale" json:"sourceLocale"`
	CreatedAt             time.Time                     `yaml:"createdAt" json:"createdAt"`
	UpdatedAt             time.Time                     `yaml:"updatedAt" json:"updatedAt"`
	PublishedAt           *time.Time                    `yaml:"publishedAt,omitempty" json:"publishedAt,omitempty"`
	Categories            []string                      `yaml:"categories" json:"categories"`
	Tags                  []string                      `yaml:"tags" json:"tags"`
	Cover                 string                        `yaml:"cover,omitempty" json:"cover,omitempty"`
	CommentPolicy         string                        `yaml:"commentPolicy" json:"commentPolicy"`
	Template              string                        `yaml:"template" json:"template"`
	Revision              int                           `yaml:"revision" json:"revision"`
	BaseRevision          int                           `yaml:"baseRevision" json:"baseRevision"`
	HeadRevision          int                           `yaml:"headRevision" json:"headRevision"`
	ReleaseRevision       int                           `yaml:"releaseRevision,omitempty" json:"releaseRevision,omitempty"`
	HasUnpublishedChanges bool                          `yaml:"-" json:"hasUnpublishedChanges"`
	Locales               map[string]LocaleContentState `yaml:"locales" json:"locales"`
}

type LocalizedMarkdown struct {
	Title          string `yaml:"title" json:"title"`
	Summary        string `yaml:"summary,omitempty" json:"summary,omitempty"`
	SEOTitle       string `yaml:"seoTitle,omitempty" json:"seoTitle,omitempty"`
	SEODescription string `yaml:"seoDescription,omitempty" json:"seoDescription,omitempty"`
	Markdown       string `yaml:"-" json:"markdown"`
}

type Post struct {
	Meta    PostMeta                     `json:"meta"`
	Content map[string]LocalizedMarkdown `json:"content"`
}
