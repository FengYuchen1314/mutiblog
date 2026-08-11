package publisher

import (
	"context"
	"os"
	"os/exec"
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
	for _, expected := range []string{"--max-old-space-size=256", "--permission", "--allow-fs-read=" + inputPath, "--allow-fs-read=" + filepath.Join(themeRoot, "server.mjs"), "--allow-fs-read=" + filepath.Join(themeRoot, "assets"), "--allow-fs-read=" + outputPath + string(filepath.Separator) + "*", "--allow-fs-write=" + outputPath, "--allow-fs-write=" + outputPath + string(filepath.Separator) + "*"} {
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

func TestCommandRendererCanCreateMissingOutputTreeUnderNodePermissionModel(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed")
	}
	if err := ValidateNodeBinary(context.Background(), node); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	staging := filepath.Join(root, "generated", "staging")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(staging, "build.input.json")
	if err := os.WriteFile(inputPath, []byte(`{"theme":{"id":"earth"}}`), 0o640); err != nil {
		t.Fatal(err)
	}
	rendererCLI := filepath.Join(root, "renderer.mjs")
	program := `
import { mkdir, readdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
if (process.env.MUTIBLOG_RENDERER_TEST_SECRET) throw new Error("renderer inherited parent secret");
const output = process.argv[process.argv.indexOf("--output") + 1];
await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
await writeFile(join(output, "index.html"), "ok", "utf8");
await mkdir(join(output, "assets"), { recursive: true });
await writeFile(join(output, "assets", "theme.css"), "body{}", "utf8");
if (!(await readdir(join(output, "assets"))).includes("theme.css")) throw new Error("cannot inspect generated assets");
process.stdout.write("{}\n");
`
	if err := os.WriteFile(rendererCLI, []byte(program), 0o640); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(staging, "missing-output")
	t.Setenv("MUTIBLOG_RENDERER_TEST_SECRET", "must-not-cross-process-boundary")
	if _, err := (CommandRenderer{NodeBinary: node, RendererCLI: rendererCLI}).Render(context.Background(), inputPath, outputPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outputPath, "index.html"))
	if err != nil || string(data) != "ok" {
		t.Fatalf("rendered output = %q, %v", data, err)
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
