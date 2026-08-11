package media

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"gopkg.in/yaml.v3"
)

const MaxUploadSize int64 = 20 << 20

var (
	ErrEmptyFile       = errors.New("media file is empty")
	ErrFileTooLarge    = errors.New("media file is too large")
	ErrUnsupportedType = errors.New("unsupported media type")
	ErrNotFound        = errors.New("media asset not found")
	ErrInUse           = errors.New("media asset is in use")
)

var mediaIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
var storedMediaPathPattern = regexp.MustCompile(`^[0-9]{4}/[0-9]{2}/[0-9a-f]{32}\.(?:jpg|png|gif|webp)$`)

var imageExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
}

func NewService(repository *fsrepo.Repository) *Service {
	return &Service{repository: repository}
}

// Recover resolves deletions interrupted between the two atomic renames. A
// staged metadata file without its original means the delete had not yet
// committed and is restored. Once both files are staged, the delete is
// committed and only private cleanup remains.
func (s *Service) Recover() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recoverLocked()
}

func (s *Service) Create(originalName string, reader io.Reader) (domain.MediaAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := io.ReadAll(io.LimitReader(reader, MaxUploadSize+1))
	if err != nil {
		return domain.MediaAsset{}, err
	}
	if len(data) == 0 {
		return domain.MediaAsset{}, ErrEmptyFile
	}
	if int64(len(data)) > MaxUploadSize {
		return domain.MediaAsset{}, ErrFileTooLarge
	}
	mimeType := http.DetectContentType(data)
	extension, ok := imageExtensions[mimeType]
	if !ok {
		return domain.MediaAsset{}, ErrUnsupportedType
	}
	id, err := newCompactUUID()
	if err != nil {
		return domain.MediaAsset{}, err
	}
	now := time.Now().UTC()
	filename := id + extension
	relative := filepath.Join(now.Format("2006"), now.Format("01"), filename)
	asset := domain.MediaAsset{
		SchemaVersion: domain.SchemaVersion,
		Kind:          "MediaAsset",
		ID:            id,
		Filename:      filename,
		OriginalName:  safeOriginalName(originalName, extension),
		MIMEType:      mimeType,
		Size:          int64(len(data)),
		URL:           "/media/" + filepath.ToSlash(relative),
		CreatedAt:     now,
	}
	originalPath := filepath.Join("media", "originals", relative)
	if err := s.repository.WriteFile(originalPath, data, 0o640); err != nil {
		return domain.MediaAsset{}, err
	}
	if err := s.repository.WriteYAML(filepath.Join("media", "metadata", id+".yaml"), asset, false); err != nil {
		_ = s.repository.RemoveFile(originalPath)
		return domain.MediaAsset{}, err
	}
	return asset, nil
}

func (s *Service) List() ([]domain.MediaAsset, error) {
	entries, err := s.repository.ReadDir(filepath.Join("media", "metadata"))
	if err != nil {
		return nil, err
	}
	assets := make([]domain.MediaAsset, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		asset, err := s.Get(id)
		if err != nil {
			return nil, fmt.Errorf("read media metadata %s: %w", entry.Name(), err)
		}
		assets = append(assets, asset)
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].CreatedAt.After(assets[j].CreatedAt) })
	return assets, nil
}

func (s *Service) Get(id string) (domain.MediaAsset, error) {
	if !mediaIDPattern.MatchString(id) {
		return domain.MediaAsset{}, ErrNotFound
	}
	var asset domain.MediaAsset
	if err := s.repository.ReadYAML(filepath.Join("media", "metadata", id+".yaml"), &asset); err != nil || asset.SchemaVersion != domain.SchemaVersion || asset.Kind != "MediaAsset" || asset.ID != id {
		return domain.MediaAsset{}, ErrNotFound
	}
	originalRelative, err := originalRelativePath(asset)
	if err != nil {
		return domain.MediaAsset{}, ErrNotFound
	}
	info, err := os.Lstat(filepath.Join(s.repository.Root(), "media", "originals", originalRelative))
	if err != nil || !info.Mode().IsRegular() || info.Size() != asset.Size {
		return domain.MediaAsset{}, ErrNotFound
	}
	return asset, nil
}

// Delete removes both the metadata and original only when no permanent source
// or historical revision refers to the public URL. Moving both files into a
// private trash directory first makes the visible deletion all-or-nothing;
// cleanup failure leaves no broken half-asset and is retried next time.
func (s *Service) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.recoverLocked(); err != nil {
		return err
	}
	asset, err := s.Get(id)
	if err != nil {
		return err
	}
	inUse, err := s.referenced(asset.URL)
	if err != nil {
		return err
	}
	if inUse {
		return ErrInUse
	}
	originalRelative, _ := originalRelativePath(asset)
	metadataPath := filepath.Join(s.repository.Root(), "media", "metadata", id+".yaml")
	originalPath := filepath.Join(s.repository.Root(), "media", "originals", originalRelative)
	for _, path := range []string{metadataPath, originalPath} {
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			return ErrNotFound
		}
	}
	trash := filepath.Join(s.repository.Root(), "media", ".trash", id)
	if err := os.MkdirAll(filepath.Join(trash, "original"), 0o700); err != nil {
		return err
	}
	trashedMetadata := filepath.Join(trash, "metadata.yaml")
	trashedOriginal := filepath.Join(trash, "original", asset.Filename)
	if err := os.Rename(metadataPath, trashedMetadata); err != nil {
		return err
	}
	if err := os.Rename(originalPath, trashedOriginal); err != nil {
		_ = os.Rename(trashedMetadata, metadataPath)
		return err
	}
	// Both source files are now invisible and the deletion is committed. A
	// cleanup error is safe to defer because Recover recognizes this state.
	_ = os.RemoveAll(trash)
	return nil
}

func (s *Service) recoverLocked() (int, error) {
	trashRoot := filepath.Join(s.repository.Root(), "media", ".trash")
	entries, err := os.ReadDir(trashRoot)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, entry := range entries {
		id := entry.Name()
		stage := filepath.Join(trashRoot, id)
		if !mediaIDPattern.MatchString(id) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return recovered, fmt.Errorf("invalid media deletion staging entry %q", id)
		}
		if err := s.recoverDeletion(id, stage); err != nil {
			return recovered, err
		}
		recovered++
	}
	if err := os.Remove(trashRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
		return recovered, err
	}
	return recovered, nil
}

func (s *Service) recoverDeletion(id, stage string) error {
	metadataStage := filepath.Join(stage, "metadata.yaml")
	metadataInfo, metadataErr := os.Lstat(metadataStage)
	metadataExists := metadataErr == nil
	if metadataErr != nil && !errors.Is(metadataErr, os.ErrNotExist) {
		return metadataErr
	}
	if metadataExists && !metadataInfo.Mode().IsRegular() {
		return fmt.Errorf("invalid staged media metadata for %s", id)
	}

	var asset domain.MediaAsset
	metadataActive := filepath.Join(s.repository.Root(), "media", "metadata", id+".yaml")
	metadataSource := metadataActive
	if metadataExists {
		metadataSource = metadataStage
	}
	if err := readAssetFile(metadataSource, id, &asset); err != nil {
		if metadataExists || !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// With neither staged nor active metadata, the delete either stopped
		// before its first rename (empty stage) or after the commit point while
		// private cleanup was removing the staged files. Validate the remaining
		// layout before completing that cleanup.
		return removeMetadataFreeMediaStage(id, stage)
	}
	originalRelative, err := originalRelativePath(asset)
	if err != nil {
		return err
	}
	originalActive := filepath.Join(s.repository.Root(), "media", "originals", originalRelative)
	originalStage := filepath.Join(stage, "original", asset.Filename)
	originalInfo, originalErr := os.Lstat(originalStage)
	originalExists := originalErr == nil
	if originalErr != nil && !errors.Is(originalErr, os.ErrNotExist) {
		return originalErr
	}
	if originalExists && (!originalInfo.Mode().IsRegular() || originalInfo.Size() != asset.Size) {
		return fmt.Errorf("invalid staged media original for %s", id)
	}

	switch {
	case metadataExists && originalExists:
		// The second rename is the commit point.
		return os.RemoveAll(stage)
	case metadataExists:
		if err := requireMediaFile(originalActive, asset.Size); err != nil {
			return err
		}
		if _, err := os.Lstat(metadataActive); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return fmt.Errorf("active media metadata already exists for %s", id)
			}
			return err
		}
		if err := os.Rename(metadataStage, metadataActive); err != nil {
			return err
		}
		return os.RemoveAll(stage)
	case originalExists:
		if err := requireMediaFile(metadataActive, -1); err != nil {
			return err
		}
		if _, err := os.Lstat(originalActive); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return fmt.Errorf("active media original already exists for %s", id)
			}
			return err
		}
		if err := os.MkdirAll(filepath.Dir(originalActive), 0o750); err != nil {
			return err
		}
		if err := os.Rename(originalStage, originalActive); err != nil {
			return err
		}
		return os.RemoveAll(stage)
	default:
		return removeEmptyMediaStage(stage)
	}
}

func readAssetFile(path, id string, asset *domain.MediaAsset) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, asset); err != nil || asset.SchemaVersion != domain.SchemaVersion || asset.Kind != "MediaAsset" || asset.ID != id {
		return fmt.Errorf("invalid staged media metadata for %s", id)
	}
	return nil
}

func requireMediaFile(path string, size int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || (size >= 0 && info.Size() != size) {
		return ErrNotFound
	}
	return nil
}

func removeEmptyMediaStage(stage string) error {
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "original" || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid media deletion staging content")
		}
		children, err := os.ReadDir(filepath.Join(stage, entry.Name()))
		if err != nil || len(children) != 0 {
			return fmt.Errorf("invalid media deletion staging content")
		}
	}
	return os.RemoveAll(stage)
}

func removeMetadataFreeMediaStage(id, stage string) error {
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "original" || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("invalid media deletion staging content")
		}
		children, err := os.ReadDir(filepath.Join(stage, entry.Name()))
		if err != nil || len(children) > 1 {
			return fmt.Errorf("invalid media deletion staging content")
		}
		if len(children) == 1 {
			child := children[0]
			extension := filepath.Ext(child.Name())
			_, validExtension := imageExtensions[mediaTypeForExtension(extension)]
			if child.Name() != id+extension || !validExtension || child.IsDir() || child.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("invalid media deletion staging content")
			}
			info, err := child.Info()
			if err != nil || !info.Mode().IsRegular() {
				return fmt.Errorf("invalid media deletion staging content")
			}
		}
	}
	return os.RemoveAll(stage)
}

func mediaTypeForExtension(extension string) string {
	for mediaType, candidate := range imageExtensions {
		if candidate == extension {
			return mediaType
		}
	}
	return ""
}

func (s *Service) referenced(publicURL string) (bool, error) {
	needle := []byte(publicURL)
	for _, relative := range []string{"config", "content", "revisions", "releases", "themes/settings"} {
		root := filepath.Join(s.repository.Root(), filepath.FromSlash(relative))
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(data, needle) {
				return ErrInUse
			}
			return nil
		})
		if errors.Is(err, ErrInUse) {
			return true, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

func originalRelativePath(asset domain.MediaAsset) (string, error) {
	const prefix = "/media/"
	if !strings.HasPrefix(asset.URL, prefix) {
		return "", ErrNotFound
	}
	relative := strings.TrimPrefix(asset.URL, prefix)
	if !storedMediaPathPattern.MatchString(relative) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative || filepath.Base(relative) != asset.Filename || !strings.HasPrefix(asset.Filename, asset.ID+".") {
		return "", ErrNotFound
	}
	return filepath.FromSlash(relative), nil
}

func safeOriginalName(name, extension string) string {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." {
		return "image" + extension
	}
	runes := []rune(name)
	if len(runes) > 160 {
		name = string(runes[:160])
	}
	return name
}

func newCompactUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate media id: %w", err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return hex.EncodeToString(buffer), nil
}
