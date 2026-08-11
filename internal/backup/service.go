package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

var ErrNotFound = errors.New("backup not found")

const MaxImportSize int64 = 512 << 20

type Record struct {
	SchemaVersion int       `yaml:"schemaVersion" json:"schemaVersion"`
	ID            string    `yaml:"id" json:"id"`
	Filename      string    `yaml:"filename" json:"filename"`
	Size          int64     `yaml:"size" json:"size"`
	CreatedAt     time.Time `yaml:"createdAt" json:"createdAt"`
}

type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
}

func NewService(repository *fsrepo.Repository) *Service { return &Service{repository: repository} }

func (s *Service) Create() (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := "backup-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	filename := id + ".tar.gz"
	target := filepath.Join(s.repository.Root(), "backups", filename)
	temporary, err := os.CreateTemp(filepath.Dir(target), ".mutiblog-backup-*")
	if err != nil {
		return Record{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	gzipWriter := gzip.NewWriter(temporary)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, root := range []string{"config", "content", "comments", "media/originals", "media/metadata", "themes/installed", "themes/settings", "revisions", "releases"} {
		if err := s.addTree(tarWriter, root); err != nil {
			tarWriter.Close()
			gzipWriter.Close()
			temporary.Close()
			return Record{}, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		gzipWriter.Close()
		temporary.Close()
		return Record{}, err
	}
	if err := gzipWriter.Close(); err != nil {
		temporary.Close()
		return Record{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return Record{}, err
	}
	if err := temporary.Close(); err != nil {
		return Record{}, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Record{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return Record{}, err
	}
	record := Record{SchemaVersion: domain.SchemaVersion, ID: id, Filename: filename, Size: info.Size(), CreatedAt: time.Now().UTC()}
	if err := s.repository.WriteYAML(filepath.Join("backups", id+".yaml"), record, false); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Service) Import(reader io.Reader) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	temporary, err := os.CreateTemp(filepath.Join(s.repository.Root(), "backups"), ".mutiblog-import-*")
	if err != nil {
		return Record{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, copyErr := io.Copy(temporary, io.LimitReader(reader, MaxImportSize+1))
	if closeErr := temporary.Close(); copyErr != nil {
		return Record{}, copyErr
	} else if closeErr != nil {
		return Record{}, closeErr
	}
	if written > MaxImportSize {
		return Record{}, ErrInvalidArchive
	}
	return s.importTemporaryLocked(temporaryPath, written)
}

func (s *Service) ImportStaged(taskID string) (Record, error) {
	if !validTaskID(taskID) {
		return Record{}, ErrTaskNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.stagedImportPath(taskID)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > MaxImportSize {
		return Record{}, ErrInvalidArchive
	}
	defer os.Remove(path)
	return s.importTemporaryLocked(path, info.Size())
}

func (s *Service) importTemporaryLocked(temporaryPath string, size int64) (Record, error) {
	id := "backup-import-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	filename := id + ".tar.gz"
	target := filepath.Join(s.repository.Root(), "backups", filename)
	staging, err := os.MkdirTemp(s.repository.Root(), ".import-validation-")
	if err != nil {
		return Record{}, err
	}
	defer os.RemoveAll(staging)
	if err := extractBackup(temporaryPath, staging); err != nil {
		return Record{}, err
	}
	for _, relative := range []string{"releases/posts", "releases/pages"} {
		if err := os.MkdirAll(filepath.Join(staging, filepath.FromSlash(relative)), 0o750); err != nil {
			return Record{}, err
		}
	}
	if err := validateRestoredTree(staging); err != nil {
		return Record{}, err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return Record{}, err
	}
	record := Record{SchemaVersion: domain.SchemaVersion, ID: id, Filename: filename, Size: size, CreatedAt: time.Now().UTC()}
	if err := s.repository.WriteYAML(filepath.Join("backups", id+".yaml"), record, false); err != nil {
		_ = os.Remove(target)
		return Record{}, err
	}
	return record, nil
}

func (s *Service) List() ([]Record, error) {
	entries, err := s.repository.ReadDir("backups")
	if err != nil {
		return nil, err
	}
	items := []Record{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var record Record
		if err := s.repository.ReadYAML(filepath.Join("backups", entry.Name()), &record); err != nil {
			return nil, fmt.Errorf("read backup record %s: %w", entry.Name(), err)
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		if !validRecord(record) || record.ID != id {
			return nil, fmt.Errorf("invalid backup record %s", entry.Name())
		}
		info, err := os.Stat(filepath.Join(s.repository.Root(), "backups", record.Filename))
		if err != nil || !info.Mode().IsRegular() || info.Size() != record.Size {
			return nil, fmt.Errorf("backup archive for %s is missing or has changed", id)
		}
		items = append(items, record)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}
func (s *Service) Path(id string) (string, Record, error) {
	if !validID(id) {
		return "", Record{}, ErrNotFound
	}
	var record Record
	if err := s.repository.ReadYAML(filepath.Join("backups", id+".yaml"), &record); err != nil {
		return "", Record{}, ErrNotFound
	}
	if !validRecord(record) || record.ID != id {
		return "", Record{}, ErrNotFound
	}
	path := filepath.Join(s.repository.Root(), "backups", record.Filename)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() != record.Size {
		return "", Record{}, ErrNotFound
	}
	return path, record, nil
}
func (s *Service) Delete(id string) error {
	_, record, err := s.Path(id)
	if err != nil {
		return err
	}
	if err := s.repository.RemoveFile(filepath.Join("backups", record.Filename)); err != nil {
		return err
	}
	return s.repository.RemoveFile(filepath.Join("backups", id+".yaml"))
}

func (s *Service) addTree(writer *tar.Writer, relativeRoot string) error {
	absoluteRoot := filepath.Join(s.repository.Root(), relativeRoot)
	return filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(s.repository.Root(), path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "config/secrets.yaml" || relative == "config/initialized" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(relative, "themes/installed/.") || strings.HasPrefix(relative, "themes/settings/.") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup source contains a symbolic link: %s", relative)
		}
		if !entry.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("backup source contains a special file: %s", relative)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relative
		if entry.IsDir() {
			header.Name += "/"
		}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
func validID(id string) bool {
	return strings.HasPrefix(id, "backup-") && !strings.ContainsAny(id, "/\\") && filepath.Base(id) == id && len(id) < 96
}
func validRecord(record Record) bool {
	return record.SchemaVersion == domain.SchemaVersion && validID(record.ID) && record.Filename == record.ID+".tar.gz" && record.Size >= 0 && !record.CreatedAt.IsZero()
}
func (record Record) DownloadName() string {
	return fmt.Sprintf("mutiblog-%s.tar.gz", strings.TrimPrefix(record.ID, "backup-"))
}
