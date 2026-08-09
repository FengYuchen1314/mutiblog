// Package backup creates portable, checksummed archives of the file-backed blog.
package backup

import (
	"archive/zip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type Options struct {
	Root, Content, Data, Config, Media, OutputDir string
	IncludeMedia, ExcludeSecrets                  bool
	// Scopes optionally selects content, data, config, and media. A nil map
	// retains the complete-backup default for existing callers.
	Scopes map[string]bool
	Name   string
	Keep   int
}
type Manifest struct {
	Version   string            `json:"version"`
	CreatedAt time.Time         `json:"createdAt"`
	Checksums map[string]string `json:"checksums"`
}
type Archive struct {
	Path      string
	Size      int64
	CreatedAt time.Time
}
type RestoreOptions struct {
	Root, Archive string
	Confirm       bool
}

func Create(opts Options) (Archive, error) {
	if opts.OutputDir == "" {
		return Archive{}, errors.New("backup output directory is required")
	}
	if opts.Keep <= 0 {
		opts.Keep = 10
	}
	include := func(scope string) bool { return opts.Scopes == nil || opts.Scopes[scope] }
	if opts.Scopes != nil && !include("content") && !include("data") && !include("config") && !include("media") {
		return Archive{}, errors.New("backup requires at least one export scope")
	}
	if err := fsutil.EnsureDir(opts.OutputDir, 0o755); err != nil {
		return Archive{}, err
	}
	now := time.Now().UTC()
	explicitName := opts.Name != ""
	name := opts.Name
	if !explicitName {
		name = "backup-" + now.Format("20060102-150405") + ".zip"
	}
	if filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return Archive{}, errors.New("backup name must be a simple .zip filename")
	}
	finalPath := filepath.Join(opts.OutputDir, name)
	for suffix := 2; ; suffix++ {
		if _, err := os.Stat(finalPath); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return Archive{}, err
		}
		if explicitName {
			return Archive{}, fmt.Errorf("backup already exists: %s", finalPath)
		}
		name = "backup-" + now.Format("20060102-150405") + fmt.Sprintf("-%d.zip", suffix)
		finalPath = filepath.Join(opts.OutputDir, name)
	}
	temporary, err := os.CreateTemp(opts.OutputDir, "."+name+".tmp-")
	if err != nil {
		return Archive{}, err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	zipWriter := zip.NewWriter(temporary)
	checksums := map[string]string{}
	stateSnapshot := ""
	if include("data") {
		stateSnapshot, err = snapshotState(filepath.Join(opts.Data, "state.db"), opts.OutputDir)
	}
	if err != nil {
		_ = zipWriter.Close()
		_ = temporary.Close()
		return Archive{}, err
	}
	if stateSnapshot != "" {
		defer os.Remove(stateSnapshot)
	}
	entries := []struct {
		prefix, root string
		redact       bool
	}{}
	if include("content") {
		entries = append(entries, struct {
			prefix, root string
			redact       bool
		}{"content", opts.Content, false})
	}
	if include("data") {
		entries = append(entries, struct {
			prefix, root string
			redact       bool
		}{"data", opts.Data, false})
	}
	if include("config") {
		entries = append(entries, struct {
			prefix, root string
			redact       bool
		}{"config", opts.Config, opts.ExcludeSecrets})
	}
	if include("media") && opts.IncludeMedia {
		entries = append(entries, struct {
			prefix, root string
			redact       bool
		}{"media", opts.Media, false})
	}
	for _, entry := range entries {
		if err := addTree(zipWriter, entry.prefix, entry.root, entry.redact, checksums); err != nil {
			_ = zipWriter.Close()
			_ = temporary.Close()
			return Archive{}, err
		}
	}
	if stateSnapshot != "" {
		if err := addFile(zipWriter, "data/state.db", stateSnapshot, checksums); err != nil {
			_ = zipWriter.Close()
			_ = temporary.Close()
			return Archive{}, err
		}
	}
	manifest, err := json.MarshalIndent(Manifest{Version: "1", CreatedAt: now, Checksums: checksums}, "", "  ")
	if err != nil {
		return Archive{}, err
	}
	w, err := zipWriter.Create("manifest.json")
	if err != nil {
		return Archive{}, err
	}
	if _, err = w.Write(manifest); err != nil {
		return Archive{}, err
	}
	if err = zipWriter.Close(); err != nil {
		return Archive{}, err
	}
	if err = temporary.Sync(); err != nil {
		return Archive{}, err
	}
	if err = temporary.Close(); err != nil {
		return Archive{}, err
	}
	if err = os.Rename(temporaryPath, finalPath); err != nil {
		return Archive{}, err
	}
	info, err := os.Stat(finalPath)
	if err != nil {
		return Archive{}, err
	}
	if opts.Name == "" {
		if err = prune(opts.OutputDir, opts.Keep); err != nil {
			return Archive{}, err
		}
	}
	return Archive{Path: finalPath, Size: info.Size(), CreatedAt: now}, nil
}
func addTree(zipWriter *zip.Writer, prefix, root string, redact bool, checksums map[string]string) error {
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup refuses symlink %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(filepath.Join(prefix, rel))
		if redact && (filepath.Base(path) == ".secrets.yaml" || strings.HasSuffix(filepath.Base(path), ".local.yaml")) {
			return nil
		}
		if name == "data/state.db" || name == "data/state.db-wal" || name == "data/state.db-shm" {
			return nil
		}
		var content []byte
		if redact && filepath.Base(path) == "config.yaml" {
			content, err = redactedConfig(path)
			if err != nil {
				return err
			}
		}
		writer, err := zipWriter.Create(name)
		if err != nil {
			return err
		}
		hash := sha256.New()
		if content != nil {
			_, err = io.MultiWriter(writer, hash).Write(content)
		} else {
			file, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			_, err = io.Copy(io.MultiWriter(writer, hash), file)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil {
			return err
		}
		checksums[name] = hex.EncodeToString(hash.Sum(nil))
		return nil
	})
}
func snapshotState(source, outputDir string) (string, error) {
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(outputDir, ".state-backup-")
	if err != nil {
		return "", err
	}
	path := temporary.Name()
	if err = temporary.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err = os.Remove(path); err != nil {
		return "", err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(source)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return "", err
	}
	defer db.Close()
	if _, err = db.Exec("VACUUM INTO ?", path); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("snapshot state database: %w", err)
	}
	return path, nil
}
func addFile(zipWriter *zip.Writer, name, path string, checksums map[string]string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer, err := zipWriter.Create(name)
	if err != nil {
		return err
	}
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(writer, hash), file); err != nil {
		return err
	}
	checksums[name] = hex.EncodeToString(hash.Sum(nil))
	return nil
}
func redactedConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err = yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	redactSecrets(raw)
	return yaml.Marshal(raw)
}
func redactSecrets(value map[string]any) {
	for key, item := range value {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "secret") || strings.Contains(lower, "password") ||
			strings.Contains(lower, "apikey") ||
			strings.Contains(lower, "api_key") ||
			strings.Contains(lower, "token") {
			value[key] = ""
			continue
		}
		switch nested := item.(type) {
		case map[string]any:
			redactSecrets(nested)
		case []any:
			for _, child := range nested {
				if mapped, ok := child.(map[string]any); ok {
					redactSecrets(mapped)
				}
			}
		}
	}
}
func List(dir string) ([]Archive, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var archives []Archive
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "backup-") || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		archives = append(
			archives,
			Archive{Path: filepath.Join(dir, entry.Name()), Size: info.Size(), CreatedAt: info.ModTime()},
		)
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].Path > archives[j].Path })
	return archives, nil
}
func Validate(path string) (Manifest, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{}, err
	}
	defer reader.Close()
	var manifest Manifest
	files := map[string]*zip.File{}
	for _, file := range reader.File {
		files[file.Name] = file
		if file.Name == "manifest.json" {
			r, err := file.Open()
			if err != nil {
				return manifest, err
			}
			err = json.NewDecoder(r).Decode(&manifest)
			r.Close()
			if err != nil {
				return manifest, err
			}
		}
	}
	if manifest.Version == "" {
		return manifest, errors.New("backup manifest is missing")
	}
	for name, want := range manifest.Checksums {
		file := files[name]
		if file == nil {
			return manifest, fmt.Errorf("backup is missing %s", name)
		}
		reader, err := file.Open()
		if err != nil {
			return manifest, err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, reader)
		reader.Close()
		if err != nil {
			return manifest, err
		}
		if hex.EncodeToString(hash.Sum(nil)) != want {
			return manifest, fmt.Errorf("checksum mismatch for %s", name)
		}
	}
	return manifest, nil
}

// Restore replaces only canonical source directories after the archive has
// passed checksum and path validation. Existing directories are retained as
// timestamped siblings so callers can recover from an interrupted deployment.
func Restore(opts RestoreOptions) error {
	if !opts.Confirm {
		return errors.New("restore requires explicit confirmation")
	}
	if opts.Root == "" || opts.Archive == "" {
		return errors.New("restore root and archive are required")
	}
	if _, err := Validate(opts.Archive); err != nil {
		return err
	}
	reader, err := zip.OpenReader(opts.Archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	stage, err := os.MkdirTemp(filepath.Dir(opts.Root), ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	allowed := map[string]bool{"content": true, "data": true, "config": true, "media": true}
	for _, file := range reader.File {
		if file.Name == "manifest.json" {
			continue
		}
		parts := strings.Split(filepath.ToSlash(file.Name), "/")
		if len(parts) < 2 || !allowed[parts[0]] || strings.Contains(file.Name, "\\") ||
			strings.HasPrefix(file.Name, "/") ||
			strings.Contains(file.Name, "..") {
			return fmt.Errorf("unsafe backup path %q", file.Name)
		}
		target, err := fsutil.SafeJoin(stage, file.Name)
		if err != nil {
			return err
		}
		if err = fsutil.EnsureDir(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(in, int64(file.UncompressedSize64)+1))
		in.Close()
		if err != nil {
			return err
		}
		if uint64(len(data)) != file.UncompressedSize64 {
			return fmt.Errorf("invalid archive size for %s", file.Name)
		}
		if err = fsutil.AtomicWrite(target, data, 0o644); err != nil {
			return err
		}
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	var moved []struct{ live, backup string }
	for _, name := range []string{"content", "data", "config", "media"} {
		incoming := filepath.Join(stage, name)
		if _, err := os.Stat(incoming); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		live := filepath.Join(opts.Root, name)
		backup := live + ".bak." + stamp
		if _, err := os.Stat(live); err == nil {
			if err = os.Rename(live, backup); err != nil {
				rollback(moved)
				return err
			}
			moved = append(moved, struct{ live, backup string }{live, backup})
		}
		if err = os.Rename(incoming, live); err != nil {
			rollback(moved)
			return err
		}
	}
	return nil
}
func rollback(moved []struct{ live, backup string }) {
	for i := len(moved) - 1; i >= 0; i-- {
		_ = os.RemoveAll(moved[i].live)
		_ = os.Rename(moved[i].backup, moved[i].live)
	}
}
func prune(dir string, keep int) error {
	archives, err := List(dir)
	if err != nil {
		return err
	}
	for i := keep; i < len(archives); i++ {
		if err := os.Remove(archives[i].Path); err != nil {
			return err
		}
	}
	return nil
}
