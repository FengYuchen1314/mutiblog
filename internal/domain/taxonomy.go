package domain

import "time"

type LocalizedTaxonomy struct {
	Name           string       `yaml:"name" json:"name"`
	Description    string       `yaml:"description,omitempty" json:"description,omitempty"`
	SEOTitle       string       `yaml:"seoTitle,omitempty" json:"seoTitle,omitempty"`
	SEODescription string       `yaml:"seoDescription,omitempty" json:"seoDescription,omitempty"`
	State          string       `yaml:"state" json:"state"`
	Origin         LocaleOrigin `yaml:"origin" json:"origin"`
	Revision       int          `yaml:"revision" json:"revision"`
	SourceRevision int          `yaml:"sourceRevision" json:"sourceRevision"`
}

type Taxonomy struct {
	SchemaVersion int                          `yaml:"schemaVersion" json:"schemaVersion"`
	Kind          string                       `yaml:"kind" json:"kind"`
	ID            string                       `yaml:"id" json:"id"`
	SourceLocale  string                       `yaml:"sourceLocale" json:"sourceLocale"`
	ParentID      string                       `yaml:"parentId,omitempty" json:"parentId,omitempty"`
	Cover         string                       `yaml:"cover,omitempty" json:"cover,omitempty"`
	Template      string                       `yaml:"template" json:"template"`
	Revision      int                          `yaml:"revision" json:"revision"`
	CreatedAt     time.Time                    `yaml:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time                    `yaml:"updatedAt" json:"updatedAt"`
	Locales       map[string]LocalizedTaxonomy `yaml:"locales" json:"locales"`
}
