package taxonomy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDetectsCategoryCycle(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "categories")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"a.yaml": "id: a\nparent: b\n", "b.yaml": "id: b\nparent: a\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result := NewStore(root).LoadAll()
	if len(result.Errors) == 0 {
		t.Fatal("expected cycle error")
	}
	if result.Categories["a"].Parent != "" && result.Categories["b"].Parent != "" {
		t.Fatalf("cycle remained: %#v", result.Categories)
	}
}
