package media

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
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

func TestCreateCleansAnOriginalWhoseWriteReturnedAfterCommit(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	writeErr := errors.New("original directory sync failed")
	originalPath := ""
	service.writeFile = func(relative string, data []byte, mode fs.FileMode) error {
		originalPath = relative
		if err := repository.WriteFile(relative, data, mode); err != nil {
			return err
		}
		return writeErr
	}

	if _, err := service.Create("uncertain-original.png", bytes.NewReader(testMediaPNG(t))); !errors.Is(err, writeErr) {
		t.Fatalf("Create() error = %v, want %v", err, writeErr)
	}
	if originalPath == "" {
		t.Fatal("Create did not attempt to write the original")
	}
	if exists, err := repository.Exists(originalPath); err != nil || exists {
		t.Fatalf("uncertain original exists = %v, %v", exists, err)
	}
	entries, err := repository.ReadDir(filepath.Join("media", "metadata"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("metadata entries = %v, %v", entries, err)
	}
}

func TestCreateReportsOriginalWriteAndCleanupFailures(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	writeErr := errors.New("original directory sync failed")
	cleanupErr := errors.New("original cleanup failed")
	originalPath := ""
	service.writeFile = func(relative string, data []byte, mode fs.FileMode) error {
		originalPath = relative
		if err := repository.WriteFile(relative, data, mode); err != nil {
			return err
		}
		return writeErr
	}
	service.removeFile = func(string) error { return cleanupErr }

	_, err = service.Create("uncertain-original.png", bytes.NewReader(testMediaPNG(t)))
	if !errors.Is(err, writeErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Create() error = %v, want joined write and cleanup failures", err)
	}
	if exists, statErr := repository.Exists(originalPath); statErr != nil || !exists {
		t.Fatalf("orphan original exists = %v, %v", exists, statErr)
	}

	service.removeFile = repository.RemoveFile
	if count, err := service.Recover(); err != nil || count != 1 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	if exists, statErr := repository.Exists(originalPath); statErr != nil || exists {
		t.Fatalf("recovered original exists = %v, %v", exists, statErr)
	}
}

func TestCreateRollsBackMetadataBeforeOriginalAfterUncertainMetadataWrite(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	writeErr := errors.New("metadata directory sync failed")
	originalPath := ""
	metadataPath := ""
	service.writeFile = func(relative string, data []byte, mode fs.FileMode) error {
		originalPath = relative
		return repository.WriteFile(relative, data, mode)
	}
	service.writeYAML = func(relative string, value any, secret bool) error {
		metadataPath = relative
		if err := repository.WriteYAML(relative, value, secret); err != nil {
			return err
		}
		return writeErr
	}

	if _, err := service.Create("uncertain-metadata.png", bytes.NewReader(testMediaPNG(t))); !errors.Is(err, writeErr) {
		t.Fatalf("Create() error = %v, want %v", err, writeErr)
	}
	for _, relative := range []string{metadataPath, originalPath} {
		if exists, err := repository.Exists(relative); err != nil || exists {
			t.Fatalf("rolled-back path %s exists = %v, %v", relative, exists, err)
		}
	}
}

func TestCreatePreservesOriginalWhenMetadataRollbackIsUncertain(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	writeErr := errors.New("metadata directory sync failed")
	cleanupErr := errors.New("metadata cleanup sync failed")
	originalPath := ""
	metadataPath := ""
	var writtenAsset domain.MediaAsset
	service.writeFile = func(relative string, data []byte, mode fs.FileMode) error {
		originalPath = relative
		return repository.WriteFile(relative, data, mode)
	}
	service.writeYAML = func(relative string, value any, secret bool) error {
		metadataPath = relative
		writtenAsset = value.(domain.MediaAsset)
		if err := repository.WriteYAML(relative, value, secret); err != nil {
			return err
		}
		return writeErr
	}
	originalCleanupCalled := false
	service.removeFile = func(relative string) error {
		if relative == metadataPath {
			return cleanupErr
		}
		if relative == originalPath {
			originalCleanupCalled = true
		}
		return repository.RemoveFile(relative)
	}

	_, err = service.Create("preserved-pair.png", bytes.NewReader(testMediaPNG(t)))
	if !errors.Is(err, writeErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Create() error = %v, want joined metadata write and cleanup failures", err)
	}
	if originalCleanupCalled {
		t.Fatal("Create removed the original while metadata rollback was uncertain")
	}
	for _, relative := range []string{metadataPath, originalPath} {
		if exists, statErr := repository.Exists(relative); statErr != nil || !exists {
			t.Fatalf("preserved path %s exists = %v, %v", relative, exists, statErr)
		}
	}

	service.removeFile = repository.RemoveFile
	if count, err := service.Recover(); err != nil || count != 0 {
		t.Fatalf("Recover() preserved pair = %d, %v", count, err)
	}
	if _, err := service.Get(writtenAsset.ID); err != nil {
		t.Fatalf("preserved asset unavailable: %v", err)
	}
}

func TestCreateReportsMetadataFailureAndRetriesOrphanCleanup(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	metadataErr := errors.New("metadata write failed")
	cleanupErr := errors.New("orphan cleanup failed")
	originalPath := ""
	removeCalls := make([]string, 0, 2)
	service.writeFile = func(relative string, data []byte, mode fs.FileMode) error {
		originalPath = relative
		return repository.WriteFile(relative, data, mode)
	}
	service.writeYAML = func(string, any, bool) error { return metadataErr }
	service.removeFile = func(relative string) error {
		removeCalls = append(removeCalls, filepath.ToSlash(relative))
		if strings.HasPrefix(filepath.ToSlash(relative), "media/originals/") {
			return cleanupErr
		}
		return repository.RemoveFile(relative)
	}

	_, err = service.Create("orphan.png", bytes.NewReader(testMediaPNG(t)))
	if !errors.Is(err, metadataErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Create() error = %v, want joined metadata and cleanup failures", err)
	}
	if len(removeCalls) != 2 || !strings.HasPrefix(removeCalls[0], "media/metadata/") || !strings.HasPrefix(removeCalls[1], "media/originals/") {
		t.Fatalf("rollback order = %v", removeCalls)
	}
	if exists, statErr := repository.Exists(originalPath); statErr != nil || !exists {
		t.Fatalf("orphan original exists = %v, %v", exists, statErr)
	}
	if count, err := service.Recover(); count != 0 || !errors.Is(err, cleanupErr) {
		t.Fatalf("Recover() with cleanup failure = %d, %v", count, err)
	}

	service.removeFile = repository.RemoveFile
	if count, err := service.Recover(); err != nil || count != 1 {
		t.Fatalf("Recover() retry = %d, %v", count, err)
	}
	if count, err := service.Recover(); err != nil || count != 0 {
		t.Fatalf("second Recover() = %d, %v", count, err)
	}
}

func TestRecoverRemovesOnlyCanonicalUntrackedOriginals(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	asset, err := service.Create("tracked.png", bytes.NewReader(testMediaPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	orphanID := strings.Repeat("a", 32)
	if orphanID == asset.ID {
		orphanID = strings.Repeat("b", 32)
	}
	referencedID := strings.Repeat("c", 32)
	invalidMonthID := strings.Repeat("d", 32)
	orphanPath := filepath.Join("media", "originals", "1999", "12", orphanID+".png")
	duplicatePath := filepath.Join("media", "originals", "2000", "01", asset.Filename)
	referencedPath := filepath.Join("media", "originals", "2001", "02", referencedID+".png")
	invalidMonthPath := filepath.Join("media", "originals", "2026", "99", invalidMonthID+".png")
	unknownPath := filepath.Join("media", "originals", "operator-note.bin")
	for _, relative := range []string{orphanPath, duplicatePath, referencedPath, invalidMonthPath, unknownPath} {
		if err := repository.WriteFile(relative, []byte("untracked"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	if err := repository.WriteFile(
		filepath.Join("content", "posts", "orphan-reference.md"),
		[]byte("![preserved](/media/2001/02/"+referencedID+".png)"),
		0o640,
	); err != nil {
		t.Fatal(err)
	}

	if count, err := service.Recover(); err != nil || count != 2 {
		t.Fatalf("Recover() = %d, %v", count, err)
	}
	if _, err := service.Get(asset.ID); err != nil {
		t.Fatalf("tracked asset unavailable: %v", err)
	}
	for _, relative := range []string{orphanPath, duplicatePath} {
		if exists, err := repository.Exists(relative); err != nil || exists {
			t.Fatalf("orphan %s exists = %v, %v", relative, exists, err)
		}
	}
	for _, relative := range []string{invalidMonthPath, unknownPath} {
		if exists, err := repository.Exists(relative); err != nil || !exists {
			t.Fatalf("unknown file %s preserved = %v, %v", relative, exists, err)
		}
	}
	if exists, err := repository.Exists(referencedPath); err != nil || !exists {
		t.Fatalf("referenced orphan preserved = %v, %v", exists, err)
	}
	if count, err := service.Recover(); err != nil || count != 0 {
		t.Fatalf("second Recover() = %d, %v", count, err)
	}
}

func testMediaPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
