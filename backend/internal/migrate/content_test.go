package migrate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/model"
)

func TestRunAddsMissingCreatedAtAndDryRunDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	store := content.NewStore(root)
	article, err := store.CreateBundle(
		model.ContentPost,
		"en",
		model.FrontMatter{
			Title:        "Old",
			Slug:         "old",
			Author:       "admin",
			SourceLocale: "en",
			Status:       model.StatusDraft,
			Date:         time.Now(),
		},
		"body",
	)
	if err != nil {
		t.Fatal(err)
	}
	article.CreatedAt = time.Time{}
	if err := store.SaveMetadata(article); err != nil {
		t.Fatal(err)
	}
	metadata := filepath.Join(root, filepath.FromSlash(article.BundleDir), "metadata.yaml")
	before, err := os.ReadFile(metadata)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), store, 1, 2, true)
	if err != nil || len(report.Changed) != 1 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	after, _ := os.ReadFile(metadata)
	if string(before) != string(after) {
		t.Fatal("dry run modified metadata")
	}
	report, err = Run(context.Background(), store, 1, 2, false)
	if err != nil || len(report.Changed) != 1 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	after, _ = os.ReadFile(metadata)
	if !strings.Contains(string(after), "createdAt:") {
		t.Fatalf("metadata=%s", after)
	}
	report, err = Run(context.Background(), store, 1, 2, false)
	if err != nil || len(report.Changed) != 0 {
		t.Fatalf("migration must be idempotent: %#v %v", report, err)
	}
}
