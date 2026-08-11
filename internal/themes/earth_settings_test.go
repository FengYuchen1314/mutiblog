package themes

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestEmbeddedEarthSettingsSchemaIsCanonicalAndServed(t *testing.T) {
	canonical, err := os.ReadFile("earth_settings.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if earthSettingsSchema != string(canonical) {
		t.Fatal("embedded Earth settings schema differs from the canonical JSON file")
	}

	var expected map[string]any
	if err := json.Unmarshal(canonical, &expected); err != nil {
		t.Fatalf("canonical Earth settings schema is invalid JSON: %v", err)
	}
	if expected["type"] != "object" {
		t.Fatalf("canonical Earth settings schema type = %#v, want object", expected["type"])
	}

	service, _ := themeFixture(t)
	themes, err := service.List()
	if err != nil {
		t.Fatalf("list themes: %v", err)
	}
	if len(themes) == 0 || themes[0].ID != "earth" || themes[0].SettingsSchema != "" {
		t.Fatalf("built-in Earth manifest exposes a non-existent settings schema path: %#v", themes)
	}
	settings, err := service.Settings("earth")
	if err != nil {
		t.Fatalf("serve embedded Earth settings schema: %v", err)
	}
	if !reflect.DeepEqual(settings.Schema, expected) {
		t.Fatal("served Earth settings schema differs from the canonical JSON file")
	}
}
