package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestCreateAndListImage(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var imageData bytes.Buffer
	canvas := image.NewRGBA(image.Rect(0, 0, 2, 2))
	canvas.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageData, canvas); err != nil {
		t.Fatal(err)
	}
	uploaded := append([]byte(nil), imageData.Bytes()...)
	service := NewService(repository)
	asset, err := service.Create("pasted image.png", &imageData)
	if err != nil {
		t.Fatal(err)
	}
	if len(asset.ID) != 32 || asset.MIMEType != "image/png" || asset.URL == "" || asset.OriginalName != "pasted image.png" {
		t.Fatalf("unexpected asset: %#v", asset)
	}
	stored, err := repository.ReadFile(filepath.Join("media", "originals", strings.TrimPrefix(asset.URL, "/media/")))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, uploaded) {
		t.Fatal("stored media differs from upload")
	}
	assets, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].ID != asset.ID {
		t.Fatalf("assets = %#v", assets)
	}
}

func TestRejectsInvalidAndOversizedMedia(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	if _, err := service.Create("payload.svg", strings.NewReader("<svg></svg>")); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("SVG error = %v", err)
	}
	if _, err := service.Create("empty.png", bytes.NewReader(nil)); !errors.Is(err, ErrEmptyFile) {
		t.Fatalf("empty error = %v", err)
	}
	if _, err := service.Create("huge.png", io.LimitReader(zeroReader{}, MaxUploadSize+1)); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("large error = %v", err)
	}
}

func TestDeleteMediaAndProtectPermanentReferences(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	newImage := func() bytes.Buffer {
		var data bytes.Buffer
		if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := newImage()
	asset, err := service.Create("referenced.png", &first)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("content/posts/reference.md", []byte("![image]("+asset.URL+")"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(asset.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("referenced delete error = %v", err)
	}
	if err := repository.RemoveFile("content/posts/reference.md"); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(asset.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(asset.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted Get error = %v", err)
	}
	if assets, err := service.List(); err != nil || len(assets) != 0 {
		t.Fatalf("assets after delete = %#v, %v", assets, err)
	}
}

func TestListRejectsMetadataWhoseIdentityDoesNotMatchItsPath(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	asset, err := service.Create("identity.png", &data)
	if err != nil {
		t.Fatal(err)
	}
	asset.ID = strings.Repeat("f", 32)
	if err := repository.WriteYAML(filepath.Join("media", "metadata", strings.TrimSuffix(asset.Filename, ".png")+".yaml"), asset, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.List(); err == nil {
		t.Fatal("List accepted media metadata with a forged cross-file identity")
	}
}

func TestRecoverInterruptedMediaDeletion(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	create := func(name string) domain.MediaAsset {
		var data bytes.Buffer
		if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
			t.Fatal(err)
		}
		asset, err := service.Create(name, &data)
		if err != nil {
			t.Fatal(err)
		}
		return asset
	}

	rolledBack := create("rollback.png")
	rollbackStage := filepath.Join(repository.Root(), "media", ".trash", rolledBack.ID)
	if err := os.MkdirAll(filepath.Join(rollbackStage, "original"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repository.Root(), "media", "metadata", rolledBack.ID+".yaml"), filepath.Join(rollbackStage, "metadata.yaml")); err != nil {
		t.Fatal(err)
	}
	count, err := service.Recover()
	if err != nil || count != 1 {
		t.Fatalf("Recover rollback = %d, %v", count, err)
	}
	if _, err := service.Get(rolledBack.ID); err != nil {
		t.Fatalf("rolled-back asset unavailable: %v", err)
	}

	committed := create("committed.png")
	committedStage := filepath.Join(repository.Root(), "media", ".trash", committed.ID)
	if err := os.MkdirAll(filepath.Join(committedStage, "original"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repository.Root(), "media", "metadata", committed.ID+".yaml"), filepath.Join(committedStage, "metadata.yaml")); err != nil {
		t.Fatal(err)
	}
	originalRelative, err := originalRelativePath(committed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repository.Root(), "media", "originals", originalRelative), filepath.Join(committedStage, "original", committed.Filename)); err != nil {
		t.Fatal(err)
	}
	count, err = service.Recover()
	if err != nil || count != 1 {
		t.Fatalf("Recover commit = %d, %v", count, err)
	}
	if _, err := service.Get(committed.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("committed asset restored unexpectedly: %v", err)
	}

	partialCleanup := create("partial-cleanup.png")
	partialStage := filepath.Join(repository.Root(), "media", ".trash", partialCleanup.ID)
	if err := os.MkdirAll(filepath.Join(partialStage, "original"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repository.Root(), "media", "metadata", partialCleanup.ID+".yaml"), filepath.Join(partialStage, "metadata.yaml")); err != nil {
		t.Fatal(err)
	}
	partialRelative, err := originalRelativePath(partialCleanup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(repository.Root(), "media", "originals", partialRelative), filepath.Join(partialStage, "original", partialCleanup.Filename)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(partialStage, "metadata.yaml")); err != nil {
		t.Fatal(err)
	}
	count, err = service.Recover()
	if err != nil || count != 1 {
		t.Fatalf("Recover partial cleanup = %d, %v", count, err)
	}
	if _, err := os.Stat(partialStage); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial cleanup stage still exists: %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
