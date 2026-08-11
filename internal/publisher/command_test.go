package publisher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRendererPermissionArgsConfineCustomTheme(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "generated", "staging")
	themeRoot := filepath.Join(root, "themes", "installed", "minimal")
	if err := os.MkdirAll(filepath.Join(themeRoot, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(staging, "build.input.json")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatal(err)
	}
	input := `{"theme":{"id":"minimal","modulePath":` + quoted(filepath.Join(themeRoot, "server.mjs")) + `,"assetsPath":` + quoted(filepath.Join(themeRoot, "assets")) + `}}`
	if err := os.WriteFile(inputPath, []byte(input), 0o640); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(staging, "build")
	arguments, err := rendererPermissionArgs("/app/renderer/cli.mjs", inputPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(arguments, "\n")
	for _, expected := range []string{"--max-old-space-size=256", "--permission", "--allow-fs-read=" + inputPath, "--allow-fs-read=" + filepath.Join(themeRoot, "server.mjs"), "--allow-fs-read=" + filepath.Join(themeRoot, "assets"), "--allow-fs-write=" + outputPath} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("renderer permissions missing %q:\n%s", expected, joined)
		}
	}
	for _, forbidden := range []string{"--allow-net", "--allow-child-process", "--allow-worker", "--allow-fs-read=*", filepath.Join(root, "secrets")} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("renderer permissions contain %q:\n%s", forbidden, joined)
		}
	}
}

func TestRendererPermissionArgsRejectThemePathEscape(t *testing.T) {
	root := t.TempDir()
	staging := filepath.Join(root, "generated", "staging")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(staging, "build.input.json")
	if err := os.WriteFile(inputPath, []byte(`{"theme":{"id":"escape","modulePath":"/etc/passwd"}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := rendererPermissionArgs("/app/renderer/cli.mjs", inputPath, filepath.Join(staging, "build")); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("path escape error = %v", err)
	}
}

func quoted(value string) string {
	return `"` + strings.ReplaceAll(value, `\`, `\\`) + `"`
}
