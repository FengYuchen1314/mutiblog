// Package theme discovers render themes and owns their constrained settings schema.
package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"gopkg.in/yaml.v3"
)

var requiredTemplates = []string{
	"home",
	"post",
	"page",
	"category",
	"category_list",
	"tag",
	"tag_list",
	"archive",
	"links",
	"search",
	"not_found",
}

// EngineVersion is the theme API version implemented by this release. It is
// intentionally independent from the binary build label, so prerelease build
// metadata cannot accidentally change the theme compatibility contract.
const EngineVersion = "1.0.0"

type Manifest struct {
	Name          string            `yaml:"name"          json:"name"`
	DisplayName   map[string]string `yaml:"displayName"   json:"displayName"`
	Version       string            `yaml:"version"       json:"version"`
	Description   map[string]string `yaml:"description"   json:"description"`
	Author        string            `yaml:"author"        json:"author"`
	Templates     []string          `yaml:"templates"     json:"templates"`
	Islands       []string          `yaml:"islands"       json:"islands"`
	Engine        Engine            `yaml:"engine"        json:"engine"`
	PageTemplates []PageTemplate    `yaml:"pageTemplates" json:"pageTemplates"`
}
type Engine struct {
	MinVersion string `yaml:"minVersion" json:"minVersion"`
}
type PageTemplate struct {
	Name  string            `yaml:"name"  json:"name"`
	Label map[string]string `yaml:"label" json:"label"`
}
type Schema struct {
	Groups []Group `json:"groups"`
	Fields []Field `json:"fields"`
}
type Group struct {
	ID    string            `json:"id"`
	Label map[string]string `json:"label"`
}
type Field struct {
	Key         string            `json:"key"`
	Type        string            `json:"type"`
	Group       string            `json:"group"`
	Help        map[string]string `json:"help"`
	Language    string            `json:"language"`
	ItemLabel   string            `json:"itemLabel"`
	Label       map[string]string `json:"label"`
	Default     any               `json:"default"`
	Required    bool              `json:"required"`
	Advanced    bool              `json:"advanced"`
	ShowIf      *ShowIf           `json:"showIf,omitempty"`
	Placeholder string            `json:"placeholder,omitempty"`
	Pattern     string            `json:"pattern,omitempty"`
	MaxLength   *int              `json:"maxLength,omitempty"`
	Rows        *int              `json:"rows,omitempty"`
	Min         *float64          `json:"min,omitempty"`
	Max         *float64          `json:"max,omitempty"`
	Step        *float64          `json:"step,omitempty"`
	Options     []Option          `json:"options,omitempty"`
	Items       []Field           `json:"items,omitempty"`
}
type ShowIf struct {
	Key    string `json:"key"`
	Equals any    `json:"equals"`
}
type Option struct {
	Value string            `json:"value"`
	Label map[string]string `json:"label"`
}
type Theme struct {
	Manifest Manifest `json:"manifest"`
	Schema   Schema   `json:"schema"`
	Dir      string   `json:"-"`
}
type ValidationError struct {
	Key     string `json:"key"`
	Message string `json:"message"`
}

func Discover(root string) ([]Theme, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var themes []Theme
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		theme, err := Load(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		themes = append(themes, theme)
	}
	sort.Slice(themes, func(i, j int) bool { return themes[i].Manifest.Name < themes[j].Manifest.Name })
	return themes, nil
}
func Load(dir string) (Theme, error) {
	var manifest Manifest
	b, err := os.ReadFile(filepath.Join(dir, "theme.yaml"))
	if err != nil {
		return Theme{}, err
	}
	if err := yaml.Unmarshal(b, &manifest); err != nil {
		return Theme{}, fmt.Errorf("theme manifest: %w", err)
	}
	if manifest.Name == "" {
		return Theme{}, errors.New("theme name is required")
	}
	var schema Schema
	if b, err = os.ReadFile(filepath.Join(dir, "settings.schema.json")); err == nil {
		if err := json.Unmarshal(b, &schema); err != nil {
			return Theme{}, fmt.Errorf("settings schema: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Theme{}, err
	}
	if errs := ValidateManifest(manifest); len(errs) > 0 {
		return Theme{}, errors.New(strings.Join(errs, "; "))
	}
	return Theme{Manifest: manifest, Schema: schema, Dir: dir}, nil
}
func ValidateManifest(m Manifest) []string {
	if m.Name == "" {
		return []string{"theme name is required"}
	}
	have := map[string]bool{}
	for _, name := range m.Templates {
		have[name] = true
	}
	var errs []string
	for _, name := range requiredTemplates {
		if !have[name] {
			errs = append(errs, "missing template "+name)
		}
	}
	return errs
}

// Compatible reports whether an application engine version satisfies a
// theme's optional minimum version. Versions use the documented vMAJOR.MINOR.PATCH
// form; an invalid declared requirement is rejected rather than guessed.
func Compatible(m Manifest, current string) error {
	if m.Engine.MinVersion == "" || current == "" || current == "dev" {
		return nil
	}
	required, err := semanticVersion(m.Engine.MinVersion)
	if err != nil {
		return fmt.Errorf("theme %q has invalid engine.minVersion %q", m.Name, m.Engine.MinVersion)
	}
	actual, err := semanticVersion(current)
	if err != nil {
		return fmt.Errorf("invalid application engine version %q", current)
	}
	for i := range required {
		if actual[i] > required[i] {
			return nil
		}
		if actual[i] < required[i] {
			return fmt.Errorf(
				"theme %q requires engine v%s or later; current version is v%s",
				m.Name,
				m.Engine.MinVersion,
				current,
			)
		}
	}
	return nil
}

func semanticVersion(value string) ([3]int, error) {
	var version [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "+", 2)[0]
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return version, errors.New("expected MAJOR.MINOR.PATCH")
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version, errors.New("expected non-negative numeric components")
		}
		version[i] = n
	}
	return version, nil
}

type Store struct{ root, data string }

func NewStore(themesRoot, dataRoot string) *Store {
	return &Store{root: themesRoot, data: filepath.Join(dataRoot, "themes")}
}
func (s *Store) List() ([]Theme, error) { return Discover(s.root) }
func (s *Store) Get(name string) (Theme, error) {
	if name == "" || filepath.Base(name) != name {
		return Theme{}, errors.New("invalid theme name")
	}
	return Load(filepath.Join(s.root, name))
}
func (s *Store) Settings(name string) (map[string]any, error) {
	theme, err := s.Get(name)
	if err != nil {
		return nil, err
	}
	values := defaults(theme.Schema)
	b, err := os.ReadFile(filepath.Join(s.data, name+".settings.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	var saved map[string]any
	if err := yaml.Unmarshal(b, &saved); err != nil {
		return nil, err
	}
	for key, value := range saved {
		values[key] = value
	}
	_, warnings := ValidateSettings(theme.Schema, values)
	return values, warningsError(warnings)
}
func (s *Store) SaveSettings(name string, input map[string]any) (map[string]any, []ValidationError, error) {
	theme, err := s.Get(name)
	if err != nil {
		return nil, nil, err
	}
	values, warnings := ValidateSettings(theme.Schema, input)
	diff := map[string]any{}
	for key, value := range values {
		if field, ok := fieldByKey(theme.Schema, key); ok && !reflect.DeepEqual(value, field.Default) {
			diff[key] = value
		}
	}
	b, err := yaml.Marshal(diff)
	if err != nil {
		return nil, warnings, err
	}
	if err = fsutil.AtomicWrite(filepath.Join(s.data, name+".settings.yaml"), b, 0o644); err != nil {
		return nil, warnings, err
	}
	return values, warnings, nil
}
func defaults(schema Schema) map[string]any {
	out := map[string]any{}
	for _, field := range schema.Fields {
		if field.Type != "group-divider" {
			out[field.Key] = field.Default
		}
	}
	return out
}
func fieldByKey(schema Schema, key string) (Field, bool) {
	for _, field := range schema.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return Field{}, false
}
func ValidateSettings(schema Schema, input map[string]any) (map[string]any, []ValidationError) {
	out := defaults(schema)
	var warnings []ValidationError
	for _, field := range schema.Fields {
		if field.Type == "group-divider" {
			continue
		}
		value, exists := input[field.Key]
		if !exists {
			continue
		}
		validated, ok := validateField(field, value)
		if !ok {
			warnings = append(warnings, ValidationError{Key: field.Key, Message: "invalid value; using default"})
			continue
		}
		out[field.Key] = validated
	}
	return out, warnings
}
func validateField(field Field, value any) (any, bool) {
	switch field.Type {
	case "text", "textarea", "color", "image", "url", "code":
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		if field.Type == "color" && text != "" && !isColor(text) {
			return nil, false
		}
		if field.Type == "code" {
			text = strings.ReplaceAll(strings.ReplaceAll(text, "</style>", ""), "<script", "")
		}
		return text, true
	case "number":
		number, ok := asNumber(value)
		if !ok {
			return nil, false
		}
		if field.Min != nil && number < *field.Min {
			number = *field.Min
		}
		if field.Max != nil && number > *field.Max {
			number = *field.Max
		}
		return number, true
	case "boolean":
		_, ok := value.(bool)
		return value, ok
	case "select", "radio":
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		for _, option := range field.Options {
			if text == option.Value {
				return text, true
			}
		}
		return nil, false
	case "multiselect":
		values, ok := value.([]any)
		if !ok {
			return nil, false
		}
		valid := map[string]bool{}
		for _, option := range field.Options {
			valid[option.Value] = true
		}
		out := make([]string, 0, len(values))
		for _, item := range values {
			text, ok := item.(string)
			if !ok || !valid[text] {
				return nil, false
			}
			out = append(out, text)
		}
		return out, true
	case "i18n-text", "i18n-textarea":
		values, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return nil, false
			}
		}
		return values, true
	case "array":
		values, ok := value.([]any)
		if !ok {
			return nil, false
		}
		out := make([]map[string]any, 0, len(values))
		for _, item := range values {
			object, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			validated := make(map[string]any, len(field.Items))
			for _, child := range field.Items {
				candidate, exists := object[child.Key]
				if !exists {
					validated[child.Key] = child.Default
					continue
				}
				clean, ok := validateField(child, candidate)
				if !ok {
					return nil, false
				}
				validated[child.Key] = clean
			}
			out = append(out, validated)
		}
		return out, true
	default:
		return nil, false
	}
}
func isColor(value string) bool {
	if len(value) != 4 && len(value) != 7 {
		return false
	}
	if !strings.HasPrefix(value, "#") {
		return false
	}
	for _, r := range value[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}
func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}
func warningsError(warnings []ValidationError) error {
	if len(warnings) == 0 {
		return nil
	}
	return fmt.Errorf("%d invalid theme settings", len(warnings))
}
