package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestBackupWhitelistsFileTruthAndExcludesSecrets(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/site.yaml", []byte("title: Test\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/locales.yaml", []byte("sourceLocale: en\n"), 0640); err != nil {
		t.Fatal(err)
	}
	writeValidBackupAdmin(t, repository)
	if err := repository.WriteFile("config/secrets.yaml", []byte("apiKey: test-provider-secret-never-archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/secrets.yaml.bak", []byte("apiKey: backup-copy-secret-never-archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/admin.yaml.bak-login-reset", []byte("passwordHash: old-admin-hash-never-archive\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("content/posts/hello/meta.yaml", []byte("id: hello\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("releases/posts/hello/current.yaml", []byte("snapshot: 000001-1000000000000\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("visits/posts/hello.yaml", []byte("kind: VisitCounter\ncount: 3\n"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("generated/releases/build/index.html", []byte("generated"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("media/.trash/deleted/original/deleted.png", []byte("deleted-private-stage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("themes/installed/.previous-demo/theme.yaml", []byte("private-theme-stage"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("themes/settings/.delete-demo", []byte("pending"), 0600); err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	record, err := service.Create()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(repository.Root(), "backups", record.Filename))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(gz)
	names := []string{}
	contents := ""
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, header.Name)
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		contents += string(data)
	}
	joined := strings.Join(names, "\n")
	if !strings.Contains(joined, "config/site.yaml") || !strings.Contains(joined, "content/posts/hello/meta.yaml") || !strings.Contains(joined, "releases/posts/hello/current.yaml") || !strings.Contains(joined, "visits/posts/hello.yaml") {
		t.Fatalf("archive names:\n%s", joined)
	}
	for _, forbidden := range []string{"config/secrets.yaml", "config/admin.yaml.bak-login-reset", "media/.trash/", "deleted-private-stage", "themes/installed/.previous-", "themes/settings/.delete-", "private-theme-stage", "generated/", "state/", "test-provider-secret-never-archive", "backup-copy-secret-never-archive", "old-admin-hash-never-archive"} {
		if strings.Contains(joined+contents, forbidden) {
			t.Fatalf("backup contains %q", forbidden)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	importRepository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	imported, err := NewService(importRepository).Import(file)
	if err != nil || !strings.HasPrefix(imported.ID, "backup-import-") || imported.Size == 0 {
		t.Fatalf("imported backup = %#v, %v", imported, err)
	}
	if _, _, err := NewService(importRepository).Path(imported.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(importRepository).Import(strings.NewReader("not a backup")); err != ErrInvalidArchive {
		t.Fatalf("invalid import error = %v", err)
	}
}

func TestBackupRejectsSymlinkInPermanentData(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repository.Root(), "content", "posts", "linked.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(repository).Create(); err == nil {
		t.Fatal("expected a permanent-data symlink to reject backup creation")
	}
}

func TestBackupPathRejectsTamperedRecordFilename(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	id := "backup-tampered"
	record := Record{SchemaVersion: domain.SchemaVersion, ID: id, Filename: "../config/site.yaml", Size: 1, CreatedAt: time.Now().UTC()}
	if err := repository.WriteYAML(filepath.Join("backups", id+".yaml"), record, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Path(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("tampered record path error = %v", err)
	}
}

func TestBackupListRejectsRecordWhoseIdentityDoesNotMatchItsPath(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id := "backup-original"
	record := Record{SchemaVersion: domain.SchemaVersion, ID: "backup-forged", Filename: "backup-forged.tar.gz", Size: 1, CreatedAt: time.Now().UTC()}
	if err := repository.WriteYAML(filepath.Join("backups", id+".yaml"), record, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile(filepath.Join("backups", record.Filename), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService(repository).List(); err == nil {
		t.Fatal("List accepted a backup record with a forged cross-file identity")
	}
}
