package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreValidatesAndPersistsOnlyOverrides(t *testing.T) {
	root := t.TempDir()
	themeDir := filepath.Join(root, "themes", "demo")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name: demo\ntemplates: [home, post, page, category, category_list, tag, tag_list, archive, links, search, not_found]\n"
	if err := os.WriteFile(filepath.Join(themeDir, "theme.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	schema := `{"fields":[{"key":"color","type":"color","default":"#112233"},{"key":"columns","type":"number","min":1,"max":4,"default":2},{"key":"layout","type":"select","options":[{"value":"wide"}],"default":"wide"},{"key":"css","type":"code","default":""}]}`
	if err := os.WriteFile(filepath.Join(themeDir, "settings.schema.json"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(root, "themes"), filepath.Join(root, "data"))
	values, warnings, err := store.SaveSettings("demo", map[string]any{"color": "#abcdef", "columns": 9.0, "layout": "bad", "css": "x</style><script y", "unknown": "discard"})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || values["columns"] != 4.0 || values["layout"] != "wide" || values["css"] != "x y" {
		t.Fatalf("values=%#v warnings=%#v", values, warnings)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "themes", "demo.settings.yaml")); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Settings("demo")
	if err != nil || loaded["color"] != "#abcdef" || loaded["columns"] != 4 {
		t.Fatalf("loaded=%#v err=%v", loaded, err)
	}
}

func TestLoadRejectsMissingTemplate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "theme.yaml"), []byte("name: broken\ntemplates: [home]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatal("expected missing template error")
	}
}

func TestCompatibleChecksMinimumEngineVersion(t *testing.T) {
	manifest := Manifest{Name: "future", Engine: Engine{MinVersion: "1.1.0"}}
	if err := Compatible(manifest, "1.0.0"); err == nil {
		t.Fatal("expected incompatible engine version")
	}
	if err := Compatible(manifest, "1.1.0"); err != nil {
		t.Fatal(err)
	}
	manifest.Engine.MinVersion = "not-a-version"
	if err := Compatible(manifest, "1.1.0"); err == nil {
		t.Fatal("expected invalid version declaration")
	}
}

func TestArraySettingsValidateNestedFieldsAndDiscardUnknownKeys(t *testing.T) {
	schema := Schema{Fields: []Field{{Key: "links", Type: "array", Default: []any{}, Items: []Field{{Key: "platform", Type: "select", Default: "github", Options: []Option{{Value: "github"}, {Value: "x"}}}, {Key: "url", Type: "url", Default: ""}}}}}
	values, warnings := ValidateSettings(schema, map[string]any{"links": []any{map[string]any{"platform": "x", "unknown": "drop"}, map[string]any{"platform": "bad", "url": "https://example.test"}}})
	if len(warnings) != 1 {
		t.Fatalf("warnings=%#v", warnings)
	}
	defaults, ok := values["links"].([]any)
	if !ok || len(defaults) != 0 {
		t.Fatalf("invalid array should use default: %#v", values)
	}
	values, warnings = ValidateSettings(schema, map[string]any{"links": []any{map[string]any{"platform": "x"}}})
	links, ok := values["links"].([]map[string]any)
	if len(warnings) != 0 || !ok || len(links) != 1 || links[0]["platform"] != "x" || links[0]["url"] != "" || links[0]["unknown"] != nil {
		t.Fatalf("values=%#v warnings=%#v", values, warnings)
	}
}
