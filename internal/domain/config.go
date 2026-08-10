package domain

import "time"

const SchemaVersion = 1

type LocalizedSite struct {
	Title       string `yaml:"title" json:"title"`
	Subtitle    string `yaml:"subtitle,omitempty" json:"subtitle,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type SiteConfig struct {
	SchemaVersion int                      `yaml:"schemaVersion" json:"schemaVersion"`
	SourceLocale  string                   `yaml:"sourceLocale" json:"sourceLocale"`
	AdminLocale   string                   `yaml:"adminLocale" json:"adminLocale"`
	Timezone      string                   `yaml:"timezone" json:"timezone"`
	BaseURL       string                   `yaml:"baseUrl,omitempty" json:"baseUrl,omitempty"`
	ActiveTheme   string                   `yaml:"activeTheme" json:"activeTheme"`
	Locales       map[string]LocalizedSite `yaml:"locales" json:"locales"`
	CreatedAt     time.Time                `yaml:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time                `yaml:"updatedAt" json:"updatedAt"`
}

type LocaleDefinition struct {
	Code    string `yaml:"code" json:"code"`
	Label   string `yaml:"label" json:"label"`
	Enabled bool   `yaml:"enabled" json:"enabled"`
}

type LocalesConfig struct {
	SchemaVersion int                `yaml:"schemaVersion" json:"schemaVersion"`
	SourceLocale  string             `yaml:"sourceLocale" json:"sourceLocale"`
	Enabled       []LocaleDefinition `yaml:"enabled" json:"enabled"`
	Fallback      []string           `yaml:"fallback" json:"fallback"`
}

type AdminConfig struct {
	SchemaVersion int       `yaml:"schemaVersion"`
	Username      string    `yaml:"username"`
	PasswordHash  string    `yaml:"passwordHash"`
	CreatedAt     time.Time `yaml:"createdAt"`
	UpdatedAt     time.Time `yaml:"updatedAt"`
}

type SecretsConfig struct {
	SchemaVersion int               `yaml:"schemaVersion"`
	Providers     map[string]string `yaml:"providers"`
}
