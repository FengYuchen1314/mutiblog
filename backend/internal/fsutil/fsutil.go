// Package fsutil contains the durability and path-safety primitives used by all writers.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrUnsafePath = errors.New("unsafe path")

type WriteSuppressor interface {
	Suppress(path string, d time.Duration)
}

var suppressor struct {
	sync.RWMutex
	value WriteSuppressor
}

func SetSuppressor(s WriteSuppressor) {
	suppressor.Lock()
	defer suppressor.Unlock()
	suppressor.value = s
}
func notify(path string) {
	suppressor.RLock()
	s := suppressor.value
	suppressor.RUnlock()
	if s != nil {
		s.Suppress(path, 2*time.Second)
	}
}

func EnsureDir(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists but is not a directory", path)
	}
	return nil
}

func AtomicWrite(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err = EnsureDir(dir, 0o755); err != nil {
		return err
	}
	notify(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Chmod(perm); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	if d, e := os.Open(dir); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// AtomicWriteBatch prepares every temporary file before replacing any target.
func AtomicWriteBatch(files map[string][]byte, perm os.FileMode) error {
	type item struct {
		path, tmp string
		file      *os.File
	}
	items := make([]item, 0, len(files))
	cleanup := func() {
		for _, x := range items {
			_ = x.file.Close()
			_ = os.Remove(x.tmp)
		}
	}
	for path, data := range files {
		if err := EnsureDir(filepath.Dir(path), 0o755); err != nil {
			cleanup()
			return err
		}
		notify(path)
		f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
		if err != nil {
			cleanup()
			return err
		}
		x := item{path: path, tmp: f.Name(), file: f}
		items = append(items, x)
		if _, err = f.Write(data); err != nil {
			cleanup()
			return err
		}
		if err = f.Sync(); err != nil {
			cleanup()
			return err
		}
		if err = f.Chmod(perm); err != nil {
			cleanup()
			return err
		}
		if err = f.Close(); err != nil {
			cleanup()
			return err
		}
	}
	for _, x := range items {
		if err := os.Rename(x.tmp, x.path); err != nil {
			cleanup()
			return err
		}
		if d, e := os.Open(filepath.Dir(x.path)); e == nil {
			_ = d.Sync()
			_ = d.Close()
		}
	}
	return nil
}

func SafeJoin(base, rel string) (string, error) {
	if !filepath.IsAbs(base) || rel == "" || filepath.IsAbs(rel) {
		return "", ErrUnsafePath
	}
	if strings.Contains(rel, "\\") {
		return "", ErrUnsafePath
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(baseReal, clean)
	// Existing symlinks in every resolved parent must remain inside base.
	for current := candidate; current != baseReal; current = filepath.Dir(current) {
		if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			if resolved != baseReal && !strings.HasPrefix(resolved, baseReal+string(filepath.Separator)) {
				return "", ErrUnsafePath
			}
		}
	}
	if candidate != baseReal && !strings.HasPrefix(candidate, baseReal+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	return candidate, nil
}

func AtomicSymlink(target, linkPath string) error {
	dir := filepath.Dir(linkPath)
	if err := EnsureDir(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(linkPath)+".tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Remove(name); err != nil {
		return err
	}
	if err = os.Symlink(target, name); err != nil {
		return err
	}
	if err = os.Rename(name, linkPath); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
func HardLinkOrCopy(src, dst string) error {
	if err := EnsureDir(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp")
	if err != nil {
		return err
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

type KeyedMutex struct {
	mu   sync.Mutex
	keys map[string]*keyLock
}
type keyLock struct {
	mu   sync.Mutex
	refs int
}

func (m *KeyedMutex) Lock(key string) func() {
	m.mu.Lock()
	if m.keys == nil {
		m.keys = make(map[string]*keyLock)
	}
	l := m.keys[key]
	if l == nil {
		l = &keyLock{}
		m.keys[key] = l
	}
	l.refs++
	m.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		m.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(m.keys, key)
		}
		m.mu.Unlock()
	}
}
