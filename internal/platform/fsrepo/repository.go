package fsrepo

import (
	"bytes"
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
	"upvotes/posts",
	"upvotes/pages",
	"visits/posts",
	"visits/pages",
	"media/originals",
	"media/metadata",
	"themes/installed",
	"themes/settings",
	"revisions",
	"releases/posts",
	"releases/pages",
	"state/tasks",
	"state/audit",
	"state/previews",
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
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	rootInfo, err := os.Lstat(absolute)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("repository root must resolve to a real directory")
	}
	repository := &Repository{root: absolute}
	for _, relative := range directoryLayout {
		mode := fs.FileMode(0o750)
		if relative == "config" {
			mode = 0o700
		}
		directory, err := repository.path(relative)
		if err != nil {
			return nil, fmt.Errorf("validate %s: %w", relative, err)
		}
		if err := os.MkdirAll(directory, mode); err != nil {
			return nil, fmt.Errorf("create %s: %w", relative, err)
		}
		if _, err := repository.path(relative); err != nil {
			return nil, fmt.Errorf("validate created %s: %w", relative, err)
		}
		info, err := os.Lstat(directory)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("repository layout path %s must be a real directory", relative)
		}
		// MkdirAll preserves the mode of an existing directory. The config
		// directory contains plaintext provider keys and authentication data,
		// so every open must reassert its owner-only boundary.
		if relative == "config" {
			if err := os.Chmod(directory, mode); err != nil {
				return nil, fmt.Errorf("secure config directory: %w", err)
			}
		}
	}
	return repository, nil
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

func (r *Repository) ReadFile(relative string) ([]byte, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	path, err := r.path(relative)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (r *Repository) ReadDir(relative string) ([]fs.DirEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	path, err := r.path(relative)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(path)
}

func (r *Repository) WriteYAML(relative string, value any, secret bool) error {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	err := encoder.Encode(value)
	if err != nil {
		return fmt.Errorf("encode %s: %w", relative, err)
	}
	data := buffer.Bytes()
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

func (r *Repository) MakeDir(relative string, mode fs.FileMode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	path, err := r.path(relative)
	if err != nil {
		return err
	}
	return os.MkdirAll(path, mode)
}

func (r *Repository) RemoveFile(relative string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	path, err := r.path(relative)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("repository removal target must be a regular file")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// RemoveTree removes one repository-owned directory without following a
// symbolic link in either the target or any existing parent component.
func (r *Repository) RemoveTree(relative string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	target, err := r.path(relative)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("repository tree removal target must be a real directory")
	}
	if err := os.RemoveAll(target); err != nil {
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
	current := r.root
	parts := strings.Split(clean, string(filepath.Separator))
	for index, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("repository path contains a symbolic link")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errors.New("repository path parent is not a directory")
		}
	}
	return path, nil
}
