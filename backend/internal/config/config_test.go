package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppliesEnvironmentAndInterpolation(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(
		"server: { baseURL: https://example.test, port: 8080 }\n" +
			"i18n: { defaultLocale: en, sourceLocale: en, " +
			"locales: [{code: en, name: English, urlPrefix: en, enabled: true}] }\n" +
			"security: { sessionSecret: '${BLOG_AI_API_KEY}' }\n",
	)
	if err := os.WriteFile(filepath.Join(root, "config", "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BLOG_SERVER_PORT", "9000")
	t.Setenv("BLOG_AI_API_KEY", "01234567890123456789012345678901")
	cfg, _, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Port != 9000 {
		t.Fatalf("port=%d", cfg.Server.Port)
	}
}

func TestUpdateSectionPreservesCommentsAndRejectsInvalidValues(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`# retained root comment
server:
  # retained field comment
  baseURL: https://example.test
  port: 8080
i18n:
  defaultLocale: en
  sourceLocale: en
  locales: [{code: en, name: English, urlPrefix: en, enabled: true}]
security:
  sessionSecret: "01234567890123456789012345678901"
`)
	path := filepath.Join(root, "config", "config.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := UpdateSection(root, "", "server", map[string]any{"port": 9000})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Server.Port != 9000 {
		t.Fatalf("port=%d", updated.Server.Port)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(written) == string(data) || !strings.Contains(string(written), "# retained field comment") {
		t.Fatalf("comments were not preserved:\n%s", written)
	}
	before := string(written)
	if _, err := UpdateSection(root, "", "server", map[string]any{"port": 70000}); err == nil {
		t.Fatal("invalid port unexpectedly accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != before {
		t.Fatal("invalid update changed live config")
	}
}

func TestReadSettingsMasksInterpolatedSecrets(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(
		"ai: { apiKey: '${BLOG_TEST_KEY}' }\nsecurity: { sessionSecret: '${BLOG_TEST_SECRET}' }\nsite: { title: Example }\n",
	)
	if err := os.WriteFile(filepath.Join(root, "config", "config.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BLOG_TEST_KEY", "key-value")
	t.Setenv("BLOG_TEST_SECRET", "secret-value")
	settings, _, err := ReadSettings(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if settings["ai"].(map[string]any)["apiKey"] != "********" ||
		settings["security"].(map[string]any)["sessionSecret"] != "********" {
		t.Fatalf("settings leaked a secret: %#v", settings)
	}
	if settings["site"].(map[string]any)["title"] != "Example" {
		t.Fatalf("unexpected non-secret settings: %#v", settings)
	}
}

func TestUpdateSchemaVersionAddsAndValidatesMarker(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config", "config.yaml")
	data := []byte(
		"# retained\nserver: { baseURL: https://example.test, port: 8080 }\n" +
			"i18n: { defaultLocale: en, sourceLocale: en, " +
			"locales: [{code: en, name: English, urlPrefix: en, enabled: true}] }\n" +
			"security: { sessionSecret: '01234567890123456789012345678901' }\n",
	)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	updated, err := UpdateSchemaVersion(root, "", 2)
	if err != nil || updated.SchemaVersion != 2 {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "schemaVersion: 2") || !strings.Contains(string(written), "# retained") {
		t.Fatalf("config=%s", written)
	}
	if _, err := UpdateSchemaVersion(root, "", 0); err == nil {
		t.Fatal("invalid schema version was accepted")
	}
}
