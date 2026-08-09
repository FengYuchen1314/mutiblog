package media

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateImageRejectsMismatchedExtension(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}
	if _, err := ValidateImage(jpeg, "payload.png", []string{"image/jpeg", "image/png"}); err == nil {
		t.Fatal("expected MIME/extension mismatch to be rejected")
	}
}

func TestSVGUploadIsSanitized(t *testing.T) {
	root := t.TempDir()
	storage := NewLocal(root, "/media")
	raw := []byte(`<svg onclick="alert(1)"><script>alert(1)</script><foreignObject>bad</foreignObject><a href="javascript:alert(1)">x</a><path d="M0 0"/></svg>`)
	if err := storage.Put(context.Background(), "unsafe.svg", bytes.NewReader(raw), int64(len(raw)), "image/svg+xml"); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(root, "unsafe.svg"))
	if err != nil {
		t.Fatal(err)
	}
	value := strings.ToLower(string(stored))
	for _, unsafe := range []string{"<script", "foreignobject", "onclick", "javascript:"} {
		if strings.Contains(value, unsafe) {
			t.Fatalf("unsafe SVG content %q remains: %s", unsafe, value)
		}
	}
}

func TestParseByteSize(t *testing.T) {
	for input, want := range map[string]int64{"20MB": 20 << 20, "1KB": 1 << 10, "2GB": 2 << 30} {
		got, err := ParseByteSize(input)
		if err != nil || got != want {
			t.Fatalf("ParseByteSize(%q) = %d, %v", input, got, err)
		}
	}
	if _, err := ParseByteSize("20"); err == nil {
		t.Fatal("missing unit should be rejected")
	}
}
