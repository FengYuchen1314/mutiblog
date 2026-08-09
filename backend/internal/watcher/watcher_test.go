package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fengyuchen/mutiblog/internal/events"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
)

func TestExternalWriteEmitsBundleChangedAndAtomicWriteIsSuppressed(t *testing.T) {
	root := t.TempDir()
	contentRoot := filepath.Join(root, "content")
	bundle := filepath.Join(contentRoot, "posts", "2026", "demo")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "metadata.yaml"), []byte("id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bus := events.New(nil)
	received := make(chan events.Event, 3)
	bus.Subscribe("BundleChanged", func(_ context.Context, e events.Event) error { received <- e; return nil })
	w, err := New([]string{contentRoot}, bus)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Start(ctx) }()
	time.Sleep(50 * time.Millisecond)
	path := filepath.Join(bundle, "index.en.md")
	if err := os.WriteFile(path, []byte("external"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-received:
		changed, ok := event.(events.BundleChanged)
		if !ok || changed.Dir != "posts/2026/demo" {
			t.Fatalf("event=%#v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no external change event")
	}
	fsutil.SetSuppressor(w)
	defer fsutil.SetSuppressor(nil)
	if err := fsutil.AtomicWrite(path, []byte("ours"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-received:
		t.Fatalf("unexpected self-write event: %#v", event)
	case <-time.After(500 * time.Millisecond):
	}
}
