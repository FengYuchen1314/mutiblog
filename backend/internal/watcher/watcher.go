// Package watcher turns filesystem notifications into debounced domain events.
package watcher

import (
	"context"
	"github.com/fengyuchen/mutiblog/internal/events"
	"github.com/fsnotify/fsnotify"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Watcher struct {
	roots       []string
	contentRoot string
	bus         *events.Bus
	fs          *fsnotify.Watcher
	mu          sync.Mutex
	recent      map[string]time.Time
	pending     map[string]events.Event
	count       int
}

func New(paths []string, bus *events.Bus) (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(paths))
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			fs.Close()
			return nil, err
		}
		roots = append(roots, abs)
	}
	w := &Watcher{roots: roots, bus: bus, fs: fs, recent: map[string]time.Time{}, pending: map[string]events.Event{}}
	for _, root := range roots {
		if filepath.Base(root) == "content" {
			w.contentRoot = root
		}
	}
	return w, nil
}
func (w *Watcher) Close() error { return w.fs.Close() }
func (w *Watcher) Suppress(path string, d time.Duration) {
	abs, _ := filepath.Abs(path)
	w.mu.Lock()
	w.recent[filepath.Clean(abs)] = time.Now().Add(d)
	w.mu.Unlock()
}
func (w *Watcher) Start(ctx context.Context) error {
	for _, root := range w.roots {
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err == nil && d.IsDir() && !ignore(path) {
				return w.fs.Add(path)
			}
			return nil
		}); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	var tick <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-w.fs.Errors:
			if !ok {
				return nil
			}
			if err != nil {
			}
		case event, ok := <-w.fs.Events:
			if !ok {
				return nil
			}
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() && !ignore(event.Name) {
					_ = w.fs.Add(event.Name)
				}
			}
			if ignore(event.Name) || w.suppressed(event.Name) {
				continue
			}
			w.queue(event.Name, event.Op)
			if tick == nil {
				timer.Reset(300 * time.Millisecond)
				tick = timer.C
			}
		case <-tick:
			w.flush(ctx)
			tick = nil
		}
	}
}
func (w *Watcher) suppressed(path string) bool {
	abs, _ := filepath.Abs(path)
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	for key, until := range w.recent {
		if now.After(until) {
			delete(w.recent, key)
		}
	}
	_, ok := w.recent[filepath.Clean(abs)]
	return ok
}
func ignore(path string) bool {
	base := filepath.Base(path)
	if strings.HasPrefix(base, ".") || base == ".DS_Store" || strings.HasSuffix(base, ".swp") ||
		strings.HasSuffix(base, "~") {
		return true
	}
	return strings.Contains(filepath.ToSlash(path), "/.drafts/") ||
		strings.Contains(filepath.ToSlash(path), "/.revisions/")
}
func (w *Watcher) queue(path string, op fsnotify.Op) {
	event, key := w.classify(path, op)
	if event == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending[key] = event
	w.count++
}
func (w *Watcher) classify(path string, op fsnotify.Op) (events.Event, string) {
	slash := filepath.ToSlash(path)
	if w.contentRoot != "" {
		if rel, err := filepath.Rel(w.contentRoot, path); err == nil && rel != ".." &&
			!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			parts := strings.Split(filepath.ToSlash(rel), "/")
			var bundle string
			if len(parts) >= 3 && parts[0] == "posts" {
				bundle = strings.Join(parts[:3], "/")
			}
			if len(parts) >= 2 && parts[0] == "pages" {
				bundle = strings.Join(parts[:2], "/")
			}
			if bundle == "" {
				return nil, ""
			}
			if op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				if _, err := os.Stat(filepath.Join(w.contentRoot, filepath.FromSlash(bundle))); os.IsNotExist(err) {
					return events.BundleRemoved{Dir: bundle}, "bundle:" + bundle
				}
			}
			return events.BundleChanged{Dir: bundle}, "bundle:" + bundle
		}
	}
	if strings.Contains(slash, "/data/categories/") {
		return events.TaxonomyChanged{Kind: "categories"}, "taxonomy:categories"
	}
	if strings.Contains(slash, "/data/tags/") {
		return events.TaxonomyChanged{Kind: "tags"}, "taxonomy:tags"
	}
	if strings.Contains(slash, "/data/links/") {
		return events.TaxonomyChanged{Kind: "links"}, "taxonomy:links"
	}
	if strings.Contains(slash, "/data/menus/") {
		return events.TaxonomyChanged{Kind: "menus"}, "taxonomy:menus"
	}
	if strings.Contains(slash, "/data/users/") {
		return events.TaxonomyChanged{Kind: "users"}, "taxonomy:users"
	}
	if strings.HasSuffix(slash, "/config.yaml") {
		return events.ConfigChanged{}, "config"
	}
	if strings.Contains(slash, "/themes/") && strings.Contains(slash, "/dist/") {
		return events.ThemeDistChanged{}, "theme"
	}
	return nil, ""
}
func (w *Watcher) flush(ctx context.Context) {
	w.mu.Lock()
	pending := w.pending
	count := w.count
	w.pending = map[string]events.Event{}
	w.count = 0
	w.mu.Unlock()
	if count > 200 {
		w.bus.Publish(ctx, events.SettingsChanged{Keys: []string{"external-bulk-change"}})
		return
	}
	for _, event := range pending {
		w.bus.Publish(ctx, event)
	}
}
