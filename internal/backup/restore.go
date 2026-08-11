package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxRestoreFiles = 10000
	maxRestoreBytes = 2 << 30
)

var (
	ErrInvalidArchive          = errors.New("invalid backup archive")
	ErrRestoreCommitRolledBack = errors.New("restore commit marker failed and restore was rolled back")
)

var restoreRoots = []string{"config", "content", "comments", "media", "themes/installed", "themes/settings", "revisions", "releases"}

type Restoration struct {
	service               *Service
	staging               string
	rollback              string
	previousPublicRelease string
	publicReleaseCaptured bool
	finished              bool
}

type restoreJournal struct {
	Staging               string `json:"staging"`
	Rollback              string `json:"rollback"`
	PreviousPublicRelease string `json:"previousPublicRelease,omitempty"`
	PublicReleaseCaptured bool   `json:"publicReleaseCaptured,omitempty"`
	Status                string `json:"status"`
}

func (s *Service) BeginRestore(id string) (*Restoration, error) {
	return s.beginRestore(id, "", false)
}

func (s *Service) BeginRestoreWithPublicRelease(id, previousPublicRelease string) (*Restoration, error) {
	if previousPublicRelease != "" && !validPublicReleaseTarget(previousPublicRelease) {
		return nil, ErrInvalidArchive
	}
	return s.beginRestore(id, previousPublicRelease, true)
}

func (s *Service) beginRestore(id, previousPublicRelease string, publicReleaseCaptured bool) (*Restoration, error) {
	s.mu.Lock()
	path, _, err := s.Path(id)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	staging, err := os.MkdirTemp(s.repository.Root(), ".restore-staging-")
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	restoration := &Restoration{
		service:               s,
		staging:               staging,
		previousPublicRelease: previousPublicRelease,
		publicReleaseCaptured: publicReleaseCaptured,
	}
	if err := extractBackup(path, staging); err != nil {
		os.RemoveAll(staging)
		s.mu.Unlock()
		return nil, err
	}
	// Backups created before public release pointers existed remain restorable;
	// their published heads are migrated lazily on first edit or publish.
	for _, relative := range []string{"releases/posts", "releases/pages"} {
		if err := os.MkdirAll(filepath.Join(staging, filepath.FromSlash(relative)), 0o750); err != nil {
			os.RemoveAll(staging)
			s.mu.Unlock()
			return nil, err
		}
	}
	if err := validateRestoredTree(staging); err != nil {
		os.RemoveAll(staging)
		s.mu.Unlock()
		return nil, err
	}
	rollback, err := os.MkdirTemp(s.repository.Root(), ".restore-rollback-")
	if err != nil {
		os.RemoveAll(staging)
		s.mu.Unlock()
		return nil, err
	}
	restoration.rollback = rollback
	if err := restoration.writeJournal("prepared"); err != nil {
		os.RemoveAll(staging)
		os.RemoveAll(rollback)
		s.mu.Unlock()
		return nil, err
	}
	if err := restoration.swapIn(); err != nil {
		_ = restoration.Rollback()
		return nil, err
	}
	return restoration, nil
}

func (r *Restoration) Commit() error {
	if r.finished {
		return nil
	}
	if err := r.writeJournal("committed"); err != nil {
		_ = r.Rollback()
		return fmt.Errorf("%w: %v", ErrRestoreCommitRolledBack, err)
	}
	r.finished = true
	defer r.service.mu.Unlock()
	if err := os.RemoveAll(r.staging); err != nil {
		return err
	}
	if err := os.RemoveAll(r.rollback); err != nil {
		return err
	}
	return os.Remove(filepath.Join(r.service.repository.Root(), ".restore-transaction.json"))
}

func (r *Restoration) Rollback() error {
	if r.finished {
		return nil
	}
	r.finished = true
	defer r.service.mu.Unlock()
	err := r.rollbackSwap()
	if err != nil {
		return err
	}
	if r.publicReleaseCaptured {
		if err := restorePublicRelease(r.service.repository.Root(), r.previousPublicRelease); err != nil {
			return err
		}
	}
	removeStagingErr := os.RemoveAll(r.staging)
	removeRollbackErr := os.RemoveAll(r.rollback)
	if removeStagingErr != nil {
		return removeStagingErr
	}
	if removeRollbackErr != nil {
		return removeRollbackErr
	}
	removeJournalErr := os.Remove(filepath.Join(r.service.repository.Root(), ".restore-transaction.json"))
	if errors.Is(removeJournalErr, os.ErrNotExist) {
		return nil
	}
	return removeJournalErr
}

func (r *Restoration) swapIn() error {
	dataRoot := r.service.repository.Root()
	for _, relative := range restoreRoots {
		current := filepath.Join(dataRoot, filepath.FromSlash(relative))
		old := filepath.Join(r.rollback, filepath.FromSlash(relative))
		restored := filepath.Join(r.staging, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(old), 0o750); err != nil {
			return err
		}
		if err := os.Rename(current, old); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(current), 0o750); err != nil {
			return err
		}
		if err := os.Rename(restored, current); err != nil {
			return err
		}
	}
	for _, name := range []string{"secrets.yaml", "initialized"} {
		source := filepath.Join(r.rollback, "config", name)
		target := filepath.Join(dataRoot, "config", name)
		if info, err := os.Stat(source); err == nil && info.Mode().IsRegular() {
			if err := copyRegularFile(source, target, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Restoration) rollbackSwap() error {
	dataRoot := r.service.repository.Root()
	var firstErr error
	for index := len(restoreRoots) - 1; index >= 0; index-- {
		relative := restoreRoots[index]
		current := filepath.Join(dataRoot, filepath.FromSlash(relative))
		old := filepath.Join(r.rollback, filepath.FromSlash(relative))
		if _, err := os.Stat(old); err != nil {
			continue
		}
		if err := os.RemoveAll(current); err != nil && firstErr == nil {
			firstErr = err
			continue
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(current), 0o750); mkdirErr != nil && firstErr == nil {
			firstErr = mkdirErr
		} else if renameErr := os.Rename(old, current); renameErr != nil && firstErr == nil {
			firstErr = renameErr
		}
	}
	return firstErr
}

func (r *Restoration) writeJournal(status string) error {
	journal := restoreJournal{
		Staging:               filepath.Base(r.staging),
		Rollback:              filepath.Base(r.rollback),
		PreviousPublicRelease: r.previousPublicRelease,
		PublicReleaseCaptured: r.publicReleaseCaptured,
		Status:                status,
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	path := filepath.Join(r.service.repository.Root(), ".restore-transaction.json")
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	directory, err := os.Open(r.service.repository.Root())
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (s *Service) RecoverInterruptedRestores() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.repository.Root())
	if err != nil {
		return err
	}
	staging := map[string]string{}
	rollbacks := map[string]string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		switch {
		case strings.HasPrefix(entry.Name(), ".restore-staging-"):
			staging[entry.Name()] = filepath.Join(s.repository.Root(), entry.Name())
		case strings.HasPrefix(entry.Name(), ".restore-rollback-"):
			rollbacks[entry.Name()] = filepath.Join(s.repository.Root(), entry.Name())
		}
	}
	var journal restoreJournal
	journalPath := filepath.Join(s.repository.Root(), ".restore-transaction.json")
	data, readErr := os.ReadFile(journalPath)
	if readErr == nil {
		_ = json.Unmarshal(data, &journal)
	}
	if rollback, exists := rollbacks[journal.Rollback]; exists {
		if journal.Status != "committed" {
			restoration := &Restoration{
				service:               s,
				rollback:              rollback,
				previousPublicRelease: journal.PreviousPublicRelease,
				publicReleaseCaptured: journal.PublicReleaseCaptured,
			}
			if err := restoration.rollbackSwap(); err != nil {
				return err
			}
			if journal.PublicReleaseCaptured {
				if err := restorePublicRelease(s.repository.Root(), journal.PreviousPublicRelease); err != nil {
					return err
				}
			}
		}
		if err := os.RemoveAll(rollback); err != nil {
			return err
		}
		delete(rollbacks, journal.Rollback)
	}
	if path, exists := staging[journal.Staging]; exists {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		delete(staging, journal.Staging)
	}
	// A rollback directory without a durable committed journal is treated as
	// interrupted and conservatively restored.
	for _, rollback := range rollbacks {
		restoration := &Restoration{service: s, rollback: rollback}
		if err := restoration.rollbackSwap(); err != nil {
			return err
		}
		if err := os.RemoveAll(rollback); err != nil {
			return err
		}
	}
	for _, path := range staging {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(journalPath + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func extractBackup(path, destination string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return ErrInvalidArchive
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var files, total int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrInvalidArchive
		}
		files++
		total += header.Size
		if files > maxRestoreFiles || total > maxRestoreBytes || header.Size < 0 {
			return ErrInvalidArchive
		}
		relative, ok := safeRestorePath(header.Name)
		if !ok {
			return ErrInvalidArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode).Perm()
			if mode == 0 || mode&0o022 != 0 {
				mode = 0o640
			}
			output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return ErrInvalidArchive
			}
			_, copyErr := io.CopyN(output, reader, header.Size)
			closeErr := output.Close()
			if copyErr != nil || closeErr != nil {
				return ErrInvalidArchive
			}
		default:
			return ErrInvalidArchive
		}
	}
	return nil
}

func safeRestorePath(raw string) (string, bool) {
	if raw == "" || strings.Contains(raw, "\\") || filepath.IsAbs(raw) {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	if clean == "config/secrets.yaml" || clean == "config/initialized" {
		return "", false
	}
	for _, root := range restoreRoots {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return clean, true
		}
	}
	return "", false
}

func validateRestoredTree(root string) error {
	for _, required := range []string{"config", "config/site.yaml", "config/locales.yaml", "content", "comments", "media", "themes/installed", "themes/settings", "revisions", "releases/posts", "releases/pages"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(required))); err != nil {
			return fmt.Errorf("%w: missing %s", ErrInvalidArchive, required)
		}
	}
	return nil
}

func copyRegularFile(source, target string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	temporary := fmt.Sprintf("%s.restore-%d", target, time.Now().UTC().UnixNano())
	defer os.Remove(temporary)
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(temporary)
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func restorePublicRelease(dataRoot, target string) error {
	generatedRoot := filepath.Join(dataRoot, "generated")
	current := filepath.Join(generatedRoot, "current")
	if target == "" {
		if err := os.Remove(current); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(generatedRoot)
	}
	if !validPublicReleaseTarget(target) {
		return ErrInvalidArchive
	}
	info, err := os.Stat(filepath.Join(generatedRoot, filepath.FromSlash(target)))
	if err != nil || !info.IsDir() {
		return errors.New("previous public release is missing")
	}
	temporary := filepath.Join(generatedRoot, fmt.Sprintf(".current-restore-%d", time.Now().UTC().UnixNano()))
	if err := os.Symlink(filepath.FromSlash(target), temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, current); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return syncDirectory(generatedRoot)
}

func validPublicReleaseTarget(target string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(target)))
	return target == clean && !filepath.IsAbs(target) && strings.HasPrefix(target, "releases/") && clean != "releases" && !strings.Contains(clean, "../")
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
