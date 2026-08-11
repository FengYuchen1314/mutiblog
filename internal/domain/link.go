package domain

import "time"

type LocalizedLink struct {
	Name           string       `yaml:"name" json:"name"`
	Description    string       `yaml:"description,omitempty" json:"description,omitempty"`
	State          string       `yaml:"state" json:"state"`
	Origin         LocaleOrigin `yaml:"origin" json:"origin"`
	Revision       int          `yaml:"revision" json:"revision"`
	SourceRevision int          `yaml:"sourceRevision" json:"sourceRevision"`
}

type LinkGroup struct {
	SchemaVersion int                      `yaml:"schemaVersion" json:"schemaVersion"`
	Kind          string                   `yaml:"kind" json:"kind"`
	ID            string                   `yaml:"id" json:"id"`
	SourceLocale  string                   `yaml:"sourceLocale" json:"sourceLocale"`
	Order         int                      `yaml:"order" json:"order"`
	Revision      int                      `yaml:"revision" json:"revision"`
	CreatedAt     time.Time                `yaml:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time                `yaml:"updatedAt" json:"updatedAt"`
	Locales       map[string]LocalizedLink `yaml:"locales" json:"locales"`
}

type Link struct {
	SchemaVersion int                      `yaml:"schemaVersion" json:"schemaVersion"`
	Kind          string                   `yaml:"kind" json:"kind"`
	ID            string                   `yaml:"id" json:"id"`
	GroupID       string                   `yaml:"groupId" json:"groupId"`
	URL           string                   `yaml:"url" json:"url"`
	Logo          string                   `yaml:"logo,omitempty" json:"logo,omitempty"`
	Order         int                      `yaml:"order" json:"order"`
	SourceLocale  string                   `yaml:"sourceLocale" json:"sourceLocale"`
	Revision      int                      `yaml:"revision" json:"revision"`
	CreatedAt     time.Time                `yaml:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time                `yaml:"updatedAt" json:"updatedAt"`
	Locales       map[string]LocalizedLink `yaml:"locales" json:"locales"`
}
