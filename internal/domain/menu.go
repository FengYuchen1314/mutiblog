package domain

import "time"

type LocalizedMenu struct {
	Label          string       `yaml:"label" json:"label"`
	State          string       `yaml:"state" json:"state"`
	Origin         LocaleOrigin `yaml:"origin" json:"origin"`
	Revision       int          `yaml:"revision" json:"revision"`
	SourceRevision int          `yaml:"sourceRevision" json:"sourceRevision"`
}

type MenuItem struct {
	ID         string                   `yaml:"id" json:"id"`
	ParentID   string                   `yaml:"parentId,omitempty" json:"parentId,omitempty"`
	TargetKind string                   `yaml:"targetKind" json:"targetKind"`
	URL        string                   `yaml:"url" json:"url"`
	OpenInNew  bool                     `yaml:"openInNew" json:"openInNew"`
	Order      int                      `yaml:"order" json:"order"`
	Locales    map[string]LocalizedMenu `yaml:"locales" json:"locales"`
}

type Menu struct {
	SchemaVersion int                      `yaml:"schemaVersion" json:"schemaVersion"`
	Kind          string                   `yaml:"kind" json:"kind"`
	ID            string                   `yaml:"id" json:"id"`
	SourceLocale  string                   `yaml:"sourceLocale" json:"sourceLocale"`
	Revision      int                      `yaml:"revision" json:"revision"`
	CreatedAt     time.Time                `yaml:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time                `yaml:"updatedAt" json:"updatedAt"`
	Locales       map[string]LocalizedMenu `yaml:"locales" json:"locales"`
	Items         []MenuItem               `yaml:"items" json:"items"`
}
