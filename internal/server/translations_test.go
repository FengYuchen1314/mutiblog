package server

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManualTranslationEndpointsStayRemoved(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate server source directory")
	}
	root := filepath.Dir(filename)
	for _, name := range []string{"server.go", "translations.go"} {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			`POST /api/v1/admin/posts/{id}/translate`,
			`POST /api/v1/admin/pages/{id}/translate`,
			"handleStartTranslation",
			"handleStartPageTranslation",
			"handleStartEntityTranslation",
		} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("manual translation entry point %q remains in %s", forbidden, name)
			}
		}
	}
}
