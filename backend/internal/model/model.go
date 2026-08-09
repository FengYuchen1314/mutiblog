// Package model defines dependency-free values shared across the application.
package model

import (
	"fmt"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

type Locale string
type ArticleID string
type ContentType string

const (
	ContentPost ContentType = "post"
	ContentPage ContentType = "page"
)

type Status string

const (
	StatusDraft       Status = "draft"
	StatusPublished   Status = "published"
	StatusUnpublished Status = "unpublished"
	StatusTrashed     Status = "trashed"
)

type TranslationStatus string

const (
	TSOriginal    TranslationStatus = "original"
	TSPending     TranslationStatus = "pending"
	TSTranslating TranslationStatus = "translating"
	TSCompleted   TranslationStatus = "completed"
	TSFailed      TranslationStatus = "failed"
	TSOutdated    TranslationStatus = "outdated"
	TSManual      TranslationStatus = "manual"
)

// LocalizedString accepts either a scalar YAML value or a locale-keyed map.
type LocalizedString map[Locale]string

func (l LocalizedString) Get(locale, fallback Locale) string {
	if v, ok := l[locale]; ok {
		return v
	}
	return l[fallback]
}

func (l *LocalizedString) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*l = LocalizedString{"": n.Value}
		return nil
	}
	var raw map[string]string
	if err := n.Decode(&raw); err != nil {
		return err
	}
	*l = make(LocalizedString, len(raw))
	for k, v := range raw {
		(*l)[Locale(k)] = v
	}
	return nil
}

func (l LocalizedString) MarshalYAML() (any, error) {
	if len(l) == 1 {
		if v, ok := l[""]; ok {
			return v, nil
		}
	}
	keys := make([]string, 0, len(l))
	for key := range l {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, key := range keys {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: l[Locale(key)]})
	}
	return n, nil
}

type Article struct {
	ID        ArticleID
	Type      ContentType
	BundleDir string
	Source    Locale
	SourceRev int
	CreatedAt time.Time
	Versions  map[Locale]*ArticleVersion
	Trans     map[Locale]*TranslationState
	Broken    bool
	BrokenErr string
}

type ArticleVersion struct {
	Locale      Locale
	FilePath    string
	Front       FrontMatter
	Body        string
	BodyHash    string
	FileModTime time.Time
	Rev         int
}

type FrontMatter struct {
	ID           ArticleID      `yaml:"id" json:"id"`
	Title        string         `yaml:"title" json:"title"`
	Slug         string         `yaml:"slug" json:"slug"`
	Description  string         `yaml:"description,omitempty" json:"description,omitempty"`
	Date         time.Time      `yaml:"date" json:"date"`
	Updated      *time.Time     `yaml:"updated,omitempty" json:"updated,omitempty"`
	Status       Status         `yaml:"status" json:"status"`
	Categories   []string       `yaml:"categories,omitempty" json:"categories,omitempty"`
	Tags         []string       `yaml:"tags,omitempty" json:"tags,omitempty"`
	Cover        string         `yaml:"cover,omitempty" json:"cover,omitempty"`
	Author       string         `yaml:"author" json:"author"`
	SourceLocale Locale         `yaml:"sourceLocale" json:"sourceLocale"`
	Locale       Locale         `yaml:"locale" json:"locale"`
	Pinned       bool           `yaml:"pinned,omitempty" json:"pinned,omitempty"`
	TOC          *bool          `yaml:"toc,omitempty" json:"toc,omitempty"`
	Comments     *bool          `yaml:"comments,omitempty" json:"comments,omitempty"`
	SEO          *SEO           `yaml:"seo,omitempty" json:"seo,omitempty"`
	Template     string         `yaml:"template,omitempty" json:"template,omitempty"`
	Order        int            `yaml:"order,omitempty" json:"order,omitempty"`
	ShowInMenu   bool           `yaml:"showInMenu,omitempty" json:"showInMenu,omitempty"`
	Extra        map[string]any `yaml:",inline" json:"-"`
}

type SEO struct {
	Title       string   `yaml:"title,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Keywords    []string `yaml:"keywords,omitempty"`
	NoIndex     bool     `yaml:"noindex,omitempty"`
	OGImage     string   `yaml:"ogImage,omitempty"`
}
type TranslationState struct {
	Status                 TranslationStatus `yaml:"status"`
	TranslatedFromRevision int               `yaml:"translatedFromRevision"`
	Revision               int               `yaml:"revision"`
	ManualEdited           bool              `yaml:"manualEdited"`
	ManualEditedAt         *time.Time        `yaml:"manualEditedAt,omitempty"`
	Provider               string            `yaml:"provider,omitempty"`
	Model                  string            `yaml:"model,omitempty"`
	UpdatedAt              *time.Time        `yaml:"updatedAt,omitempty"`
	TokensUsed             int               `yaml:"tokensUsed,omitempty"`
	Error                  string            `yaml:"error,omitempty"`
	FailedAt               *time.Time        `yaml:"failedAt,omitempty"`
	Attempts               int               `yaml:"attempts,omitempty"`
	SourceDrift            int               `yaml:"-"`
}
type BundleMetadata struct {
	ID             ArticleID                    `yaml:"id"`
	Type           ContentType                  `yaml:"type"`
	SourceLocale   Locale                       `yaml:"sourceLocale"`
	SourceRevision int                          `yaml:"sourceRevision"`
	CreatedAt      time.Time                    `yaml:"createdAt"`
	Translations   map[Locale]*TranslationState `yaml:"translations"`
}
type Category struct {
	ID, Slug, Parent, Color, Cover string
	Order                          int
	Name                           LocalizedString
	Description                    LocalizedString
	SEO                            *SEO
}
type Tag struct {
	ID, Slug, Color string
	Name            LocalizedString
	Description     LocalizedString
}
type LinkGroup struct {
	ID    string          `yaml:"id"`
	Order int             `yaml:"order"`
	Name  LocalizedString `yaml:"name"`
}
type Link struct {
	ID, Name, URL, Logo, Group string
	Description                LocalizedString
	Order                      int
	Status                     string
	CreatedAt                  time.Time  `yaml:"createdAt"`
	LastCheckedAt              *time.Time `yaml:"lastCheckedAt,omitempty"`
}
type Menu struct {
	ID    string          `yaml:"id"`
	Name  LocalizedString `yaml:"name"`
	Items []MenuItem      `yaml:"items"`
}
type MenuItem struct {
	ID, Type, Ref, URL, Icon, Target string
	Label                            LocalizedString
	Order                            int
	Children                         []MenuItem
}
type User struct {
	ID           string            `yaml:"id" json:"id"`
	Username     string            `yaml:"username" json:"username"`
	Email        string            `yaml:"email" json:"email"`
	DisplayName  string            `yaml:"displayName" json:"displayName"`
	Avatar       string            `yaml:"avatar,omitempty" json:"avatar,omitempty"`
	Role         string            `yaml:"role" json:"role"`
	PasswordHash string            `yaml:"passwordHash" json:"-"`
	TokenVersion int               `yaml:"tokenVersion" json:"tokenVersion"`
	Locale       Locale            `yaml:"locale" json:"locale"`
	CreatedAt    time.Time         `yaml:"createdAt" json:"createdAt"`
	LastLoginAt  *time.Time        `yaml:"lastLoginAt,omitempty" json:"lastLoginAt,omitempty"`
	Disabled     bool              `yaml:"disabled" json:"disabled"`
	Bio          LocalizedString   `yaml:"bio,omitempty" json:"bio,omitempty"`
	Social       map[string]string `yaml:"social,omitempty" json:"social,omitempty"`
}
type MediaMeta struct {
	Path, MIME    string
	Size          int64
	Width, Height int
	CreatedAt     time.Time
	Variants      map[string]string
}

func (f FrontMatter) Validate() error {
	if f.ID == "" || f.Title == "" || f.Slug == "" || f.Author == "" || f.SourceLocale == "" || f.Locale == "" {
		return fmt.Errorf("front matter is missing required fields")
	}
	return nil
}
