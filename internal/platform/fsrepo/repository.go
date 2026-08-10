package fsrepo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type Repository struct {
	root string
	mu   sync.RWMutex
}

var directoryLayout = []string{
	"config",
	"content/posts",
	"content/pages",
	"content/taxonomies/categories",
	"content/taxonomies/tags",
	"content/menus",
	"content/links",
	"content/dictionaries",
	"comments",
	"media/originals",
	"media/metadata",
	"themes/installed",
	"themes/settings",
	"revisions",
	"state/tasks",
	"state/audit",
	"generated/staging",
	"generated/previews",
	"generated/releases",
	"backups",
}

func Open(root string) (*Repository, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, err
	}
	for _, relative := range directoryLayout {
		mode := fs.FileMode(0o750)
		if relative == "config" {
			mode = 0o700
		}
		if err := os.MkdirAll(filepath.Join(absolute, relative), mode); err != nil {
			return nil, fmt.Errorf("create %s: %w", relative, err)
		}
	}
	return &Repository{root: absolute}, nil
}

func (r *Repository) Root() string { return r.root }

func (r *Repository) Exists(relative string) (bool, error) {
	path, err := r.path(relative)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (r *Repository) ReadYAML(relative string, destination any) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	path, err := r.path(relative)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode %s: %w", relative, err)
	}
	return nil
}

func (r *Repository) WriteYAML(relative string, value any, secret bool) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", relative, err)
	}
	mode := fs.FileMode(0o640)
	if secret {
		mode = 0o600
	}
	return r.WriteFile(relative, data, mode)
}

func (r *Repository) WriteFile(relative string, data []byte, mode fs.FileMode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, err := r.path(relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".mutiblog-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (r *Repository) path(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", errors.New("repository path must be relative")
	}
	clean := filepath.Clean(relative)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("repository path escapes root")
	}
	path := filepath.Join(r.root, clean)
	rel, err := filepath.Rel(r.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("repository path escapes root")
	}
	return path, nil
}
