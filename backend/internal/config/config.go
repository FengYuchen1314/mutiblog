// Package config loads the versioned, file-backed site configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	SchemaVersion int            `yaml:"schemaVersion"`
	Server        ServerConfig   `yaml:"server"`
	Paths         PathsConfig    `yaml:"paths"`
	Site          SiteConfig     `yaml:"site"`
	I18n          I18nConfig     `yaml:"i18n"`
	Render        RenderConfig   `yaml:"render"`
	Markdown      MarkdownConfig `yaml:"markdown"`
	Storage       StorageConfig  `yaml:"storage"`
	AI            AIConfig       `yaml:"ai"`
	Theme         ThemeConfig    `yaml:"theme"`
	Search        SearchConfig   `yaml:"search"`
	Index         IndexConfig    `yaml:"index"`
	Comments      CommentsConfig `yaml:"comments"`
	SEO           SEOConfig      `yaml:"seo"`
	Cache         CacheConfig    `yaml:"cache"`
	Security      SecurityConfig `yaml:"security"`
	Backup        BackupConfig   `yaml:"backup"`
	Log           LogConfig      `yaml:"log"`
}
type ServerConfig struct {
	Host            string   `yaml:"host"`
	Port            int      `yaml:"port"`
	BaseURL         string   `yaml:"baseURL"`
	TrustedProxies  []string `yaml:"trustedProxies"`
	ServeStatic     bool     `yaml:"serveStatic"`
	GracefulTimeout Duration `yaml:"gracefulTimeout"`
}
type PathsConfig struct {
	Content   string `yaml:"content"`
	Data      string `yaml:"data"`
	Media     string `yaml:"media"`
	Themes    string `yaml:"themes"`
	Generated string `yaml:"generated"`
	Cache     string `yaml:"cache"`
}
type SiteConfig struct {
	Title         string   `yaml:"title"`
	Description   string   `yaml:"description"`
	Keywords      []string `yaml:"keywords"`
	Logo          string   `yaml:"logo"`
	Favicon       string   `yaml:"favicon"`
	Author        string   `yaml:"author"`
	Copyright     string   `yaml:"copyright"`
	PostsPerPage  int      `yaml:"postsPerPage"`
	ExcerptLength int      `yaml:"excerptLength"`
	Timezone      string   `yaml:"timezone"`
}
type LocaleConfig struct {
	Code      string `yaml:"code"`
	Name      string `yaml:"name"`
	URLPrefix string `yaml:"urlPrefix"`
	Enabled   bool   `yaml:"enabled"`
}
type I18nConfig struct {
	DefaultLocale          string            `yaml:"defaultLocale"`
	SourceLocale           string            `yaml:"sourceLocale"`
	Locales                []LocaleConfig    `yaml:"locales"`
	LocaleAliases          map[string]string `yaml:"localeAliases"`
	CountryLocaleMap       map[string]string `yaml:"countryLocaleMap"`
	CookieName             string            `yaml:"cookieName"`
	CookieMaxAge           int               `yaml:"cookieMaxAge"`
	RedirectRoot           bool              `yaml:"redirectRoot"`
	AutoTranslateOnPublish bool              `yaml:"autoTranslateOnPublish"`
	TranslateTargets       []string          `yaml:"translateTargets"`
}
type RenderConfig struct {
	WorkerEnabled bool     `yaml:"workerEnabled"`
	WorkerCommand []string `yaml:"workerCommand"`
	WorkerSocket  string   `yaml:"workerSocket"`
	WorkerCount   int      `yaml:"workerCount"`
	Concurrency   int      `yaml:"concurrency"`
	Timeout       Duration `yaml:"timeout"`
	Output        string   `yaml:"output"`
	KeepReleases  int      `yaml:"keepReleases"`
	PrettyURLs    bool     `yaml:"prettyURLs"`
	MinifyHTML    bool     `yaml:"minifyHTML"`
}
type MarkdownConfig struct {
	Katex               bool `yaml:"katex"`
	Mermaid             bool `yaml:"mermaid"`
	ExternalLinksNewTab bool `yaml:"externalLinksNewTab"`
	HeadingAnchors      bool `yaml:"headingAnchors"`
	Sanitize            bool `yaml:"sanitize"`
	TOCMinDepth         int  `yaml:"tocMinDepth"`
	TOCMaxDepth         int  `yaml:"tocMaxDepth"`
}
type StorageConfig struct {
	Driver string `yaml:"driver"`
	Local  struct {
		Root         string `yaml:"root"`
		PublicPrefix string `yaml:"publicPrefix"`
	} `yaml:"local"`
	Image struct {
		MaxUploadSize string   `yaml:"maxUploadSize"`
		AllowedTypes  []string `yaml:"allowedTypes"`
		StripEXIF     bool     `yaml:"stripEXIF"`
	} `yaml:"image"`
}
type AIConfig struct {
	Enabled              bool     `yaml:"enabled"`
	Provider             string   `yaml:"provider"`
	BaseURL              string   `yaml:"baseURL"`
	APIKey               string   `yaml:"apiKey"`
	Model                string   `yaml:"model"`
	Temperature          float64  `yaml:"temperature"`
	MaxTokensPerRequest  int      `yaml:"maxTokensPerRequest"`
	Timeout              Duration `yaml:"timeout"`
	Concurrency          int      `yaml:"concurrency"`
	MaxRetries           int      `yaml:"maxRetries"`
	RateLimitRPM         int      `yaml:"rateLimitRPM"`
	SegmentBudget        int      `yaml:"segmentBudget"`
	SystemPromptOverride string   `yaml:"systemPromptOverride"`
}
type ThemeConfig struct {
	Active string `yaml:"active"`
}
type SearchConfig struct {
	Enabled         bool `yaml:"enabled"`
	MaxIndexSizeMB  int  `yaml:"maxIndexSizeMB"`
	BodyCharsPerDoc int  `yaml:"bodyCharsPerDoc"`
}
type IndexConfig struct {
	BodyResidentLimitMB int    `yaml:"bodyResidentLimitMB"`
	BodyCacheMB         int    `yaml:"bodyCacheMB"`
	ForceMode           string `yaml:"forceMode"`
}
type CommentsConfig struct {
	Enabled  bool           `yaml:"enabled"`
	Provider string         `yaml:"provider"`
	Options  map[string]any `yaml:"options"`
}
type SEOConfig struct {
	GenerateSitemap bool   `yaml:"generateSitemap"`
	GenerateRSS     bool   `yaml:"generateRSS"`
	RSSItemCount    int    `yaml:"rssItemCount"`
	RobotsTxt       string `yaml:"robotsTxt"`
}
type CacheConfig struct {
	HTMLMaxAge               int    `yaml:"htmlMaxAge"`
	HTMLSMaxAge              int    `yaml:"htmlSMaxAge"`
	HTMLStaleWhileRevalidate int    `yaml:"htmlStaleWhileRevalidate"`
	AssetMaxAge              int    `yaml:"assetMaxAge"`
	MediaMaxAge              int    `yaml:"mediaMaxAge"`
	MediaSMaxAge             int    `yaml:"mediaSMaxAge"`
	Purger                   string `yaml:"purger"`
}
type SecurityConfig struct {
	SessionSecret  string `yaml:"sessionSecret"`
	SessionMaxAge  int    `yaml:"sessionMaxAge"`
	CookieSecure   string `yaml:"cookieSecure"`
	LoginRateLimit struct {
		Attempts int      `yaml:"attempts"`
		Window   Duration `yaml:"window"`
		Lockout  Duration `yaml:"lockout"`
	} `yaml:"loginRateLimit"`
	APIRateLimit struct {
		RPS   int `yaml:"rps"`
		Burst int `yaml:"burst"`
	} `yaml:"apiRateLimit"`
}
type BackupConfig struct {
	Dir          string `yaml:"dir"`
	Keep         int    `yaml:"keep"`
	IncludeMedia bool   `yaml:"includeMedia"`
}
type LogConfig struct {
	Level              string `yaml:"level"`
	Format             string `yaml:"format"`
	AuditRetentionDays int    `yaml:"auditRetentionDays"`
}
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}
func (d Duration) Duration() time.Duration { return time.Duration(d) }

var variable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func Load(root, file string) (*Config, []string, error) {
	if !filepath.IsAbs(root) {
		var err error
		root, err = filepath.Abs(root)
		if err != nil {
			return nil, nil, err
		}
	}
	if file == "" {
		file = filepath.Join(root, "config", "config.yaml")
	} else if !filepath.IsAbs(file) {
		file = filepath.Join(root, file)
	}
	mainData, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, fmt.Errorf("read config: %w", err)
	}
	var raw map[string]any
	if err = yaml.Unmarshal(mainData, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	local := strings.TrimSuffix(file, ".yaml") + ".local.yaml"
	if b, readErr := os.ReadFile(local); readErr == nil {
		var override map[string]any
		if err := yaml.Unmarshal(b, &override); err != nil {
			return nil, nil, fmt.Errorf("parse local config: %w", err)
		}
		raw = merge(raw, override)
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, nil, readErr
	}
	applyEnvironment(raw)
	warnings := make([]string, 0)
	raw = interpolateMap(raw, &warnings).(map[string]any)
	b, err := yaml.Marshal(raw)
	if err != nil {
		return nil, nil, err
	}
	cfg := defaults()
	if err := yaml.NewDecoder(bytes.NewReader(b)).Decode(&cfg); err != nil {
		return nil, nil, fmt.Errorf("decode config: %w", err)
	}
	applyDefaults(&cfg)
	cfg.resolvePaths(root)
	if err := cfg.Validate(); err != nil {
		return nil, warnings, err
	}
	return &cfg, warnings, nil
}

func defaults() Config {
	return Config{
		Server: ServerConfig{
			Host:            "0.0.0.0",
			Port:            8080,
			ServeStatic:     true,
			GracefulTimeout: Duration(15 * time.Second),
		},
		Site: SiteConfig{PostsPerPage: 10, ExcerptLength: 200, Timezone: "Asia/Shanghai"},
		Render: RenderConfig{
			WorkerEnabled: true,
			WorkerCount:   1,
			Concurrency:   1,
			Timeout:       Duration(30 * time.Second),
			KeepReleases:  3,
			PrettyURLs:    true,
			MinifyHTML:    true,
		},
		Markdown: MarkdownConfig{
			Katex:               true,
			Mermaid:             true,
			ExternalLinksNewTab: true,
			HeadingAnchors:      true,
			TOCMinDepth:         2,
			TOCMaxDepth:         3,
		},
		AI: AIConfig{
			Timeout:       Duration(120 * time.Second),
			Concurrency:   2,
			MaxRetries:    3,
			RateLimitRPM:  60,
			SegmentBudget: 2500,
		},
		Search:   SearchConfig{Enabled: true, MaxIndexSizeMB: 3, BodyCharsPerDoc: 2000},
		Index:    IndexConfig{BodyResidentLimitMB: 64, BodyCacheMB: 32},
		SEO:      SEOConfig{GenerateSitemap: true, GenerateRSS: true, RSSItemCount: 20},
		Security: SecurityConfig{SessionMaxAge: 604800, CookieSecure: "auto"},
		Backup:   BackupConfig{Keep: 10, IncludeMedia: true},
		Log:      LogConfig{Level: "info", Format: "json", AuditRetentionDays: 90},
	}
}

// applyDefaults keeps omitted fields stable when users keep a deliberately small config file.
func applyDefaults(c *Config) {
	d := defaults()
	if c.SchemaVersion == 0 {
		c.SchemaVersion = 1
	}
	if c.Server.Host == "" {
		c.Server.Host = d.Server.Host
	}
	if c.Server.GracefulTimeout == 0 {
		c.Server.GracefulTimeout = d.Server.GracefulTimeout
	}
	if c.Site.PostsPerPage == 0 {
		c.Site.PostsPerPage = d.Site.PostsPerPage
	}
	if c.Site.ExcerptLength == 0 {
		c.Site.ExcerptLength = d.Site.ExcerptLength
	}
	if c.Site.Timezone == "" {
		c.Site.Timezone = d.Site.Timezone
	}
	if c.Render.WorkerCount == 0 {
		c.Render.WorkerCount = d.Render.WorkerCount
	}
	if c.Render.Concurrency == 0 {
		c.Render.Concurrency = d.Render.Concurrency
	}
	if c.Render.Timeout == 0 {
		c.Render.Timeout = d.Render.Timeout
	}
	if c.Render.KeepReleases == 0 {
		c.Render.KeepReleases = d.Render.KeepReleases
	}
	if c.AI.Timeout == 0 {
		c.AI.Timeout = d.AI.Timeout
	}
	if c.AI.Concurrency == 0 {
		c.AI.Concurrency = d.AI.Concurrency
	}
	if c.AI.MaxRetries == 0 {
		c.AI.MaxRetries = d.AI.MaxRetries
	}
	if c.AI.RateLimitRPM == 0 {
		c.AI.RateLimitRPM = d.AI.RateLimitRPM
	}
	if c.AI.SegmentBudget == 0 {
		c.AI.SegmentBudget = d.AI.SegmentBudget
	}
	if c.Theme.Active == "" {
		c.Theme.Active = "default"
	}
	if c.Index.BodyResidentLimitMB == 0 {
		c.Index.BodyResidentLimitMB = d.Index.BodyResidentLimitMB
	}
	if c.Index.BodyCacheMB == 0 {
		c.Index.BodyCacheMB = d.Index.BodyCacheMB
	}
	if c.Security.SessionMaxAge == 0 {
		c.Security.SessionMaxAge = d.Security.SessionMaxAge
	}
	if c.Security.CookieSecure == "" {
		c.Security.CookieSecure = d.Security.CookieSecure
	}
	if c.Log.Level == "" {
		c.Log.Level = d.Log.Level
	}
	if c.Log.Format == "" {
		c.Log.Format = d.Log.Format
	}
}
func (c *Config) resolvePaths(root string) {
	for _, p := range []*string{
		&c.Paths.Content, &c.Paths.Data, &c.Paths.Media,
		&c.Paths.Themes, &c.Paths.Generated, &c.Paths.Cache,
	} {
		if *p == "" {
			continue
		}
		if !filepath.IsAbs(*p) {
			*p = filepath.Clean(filepath.Join(root, *p))
		}
	}
	if c.Backup.Dir != "" && !filepath.IsAbs(c.Backup.Dir) {
		c.Backup.Dir = filepath.Join(root, c.Backup.Dir)
	}
}
func (c Config) Validate() error {
	if c.SchemaVersion < 1 {
		return errors.New("schemaVersion must be at least 1")
	}
	if c.Server.BaseURL == "" {
		return errors.New("server.baseURL is required")
	}
	if !(strings.HasPrefix(c.Server.BaseURL, "http://") || strings.HasPrefix(c.Server.BaseURL, "https://")) {
		return errors.New("server.baseURL must be an http(s) URL")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.New("server.port must be between 1 and 65535")
	}
	if len(c.I18n.Locales) == 0 {
		return errors.New("i18n.locales must not be empty")
	}
	enabled := map[string]bool{}
	for _, l := range c.I18n.Locales {
		if l.Enabled {
			enabled[l.Code] = true
		}
	}
	if !enabled[c.I18n.DefaultLocale] || !enabled[c.I18n.SourceLocale] {
		return errors.New("defaultLocale and sourceLocale must be enabled locales")
	}
	if strings.Contains(c.Security.SessionSecret, "${") || len(c.Security.SessionSecret) < 32 {
		return errors.New("security.sessionSecret is required and must be at least 32 characters")
	}
	return ValidateTimeoutOrdering(c)
}
func ValidateTimeoutOrdering(c Config) error {
	if c.Render.Timeout.Duration() <= 25*time.Second {
		return errors.New("render.timeout must exceed Node's 25s render timeout")
	}
	if c.AI.Timeout.Duration() < 120*time.Second {
		return errors.New("ai.timeout must be at least 120s")
	}
	if c.Server.GracefulTimeout.Duration() < 15*time.Second {
		return errors.New("server.gracefulTimeout must be at least 15s")
	}
	return nil
}

func merge(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		child, ok := result[k].(map[string]any)
		incoming, incomingOK := v.(map[string]any)
		if ok && incomingOK {
			result[k] = merge(child, incoming)
		} else {
			result[k] = v
		}
	}
	return result
}
func interpolateMap(v any, warnings *[]string) any {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			x[k] = interpolateMap(vv, warnings)
		}
		return x
	case []any:
		for i, vv := range x {
			x[i] = interpolateMap(vv, warnings)
		}
		return x
	case string:
		return variable.ReplaceAllStringFunc(x, func(token string) string {
			name := variable.FindStringSubmatch(token)[1]
			if value, ok := os.LookupEnv(name); ok {
				return value
			}
			*warnings = append(*warnings, "environment variable "+name+" is not set")
			return token
		})
	default:
		return v
	}
}
func applyEnvironment(raw map[string]any) {
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, "BLOG_") {
			continue
		}
		path := strings.TrimPrefix(key, "BLOG_")
		switch path {
		case "SERVER_PORT":
			set(raw, []string{"server", "port"}, value)
		case "AI_APIKEY", "AI_API_KEY":
			set(raw, []string{"ai", "apiKey"}, value)
		case "SECURITY_SESSIONSECRET", "SESSION_SECRET":
			set(raw, []string{"security", "sessionSecret"}, value)
		default:
			parts := strings.Split(strings.ToLower(path), "__")
			if len(parts) > 1 {
				set(raw, parts, value)
			}
		}
	}
}
func set(raw map[string]any, path []string, text string) {
	m := raw
	for _, part := range path[:len(path)-1] {
		next, ok := m[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[part] = next
		}
		m = next
	}
	key := path[len(path)-1]
	if old, ok := m[key]; ok {
		m[key] = coerce(old, text)
	} else {
		m[key] = text
	}
}
func coerce(old any, text string) any {
	switch old.(type) {
	case int:
		v, _ := strconv.Atoi(text)
		return v
	case bool:
		v, _ := strconv.ParseBool(text)
		return v
	case float64:
		v, _ := strconv.ParseFloat(text, 64)
		return v
	default:
		return text
	}
}

var _ = reflect.TypeOf // retained as an explicit guard against accidental reflection-based secret dumps.
