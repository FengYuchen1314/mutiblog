package projection

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	_ "modernc.org/sqlite"
)

type Stats struct {
	Status               string     `json:"status"`
	Documents            int        `json:"documents"`
	Path                 string     `json:"path"`
	LastIndexedAt        *time.Time `json:"lastIndexedAt,omitempty"`
	LastExternalChangeAt *time.Time `json:"lastExternalChangeAt,omitempty"`
	LastError            string     `json:"lastError,omitempty"`
}

type SearchResult struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Locale  string `json:"locale"`
	Status  string `json:"status"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

type Service struct {
	repository  *fsrepo.Repository
	content     *content.Service
	logger      *slog.Logger
	db          *sql.DB
	path        string
	mu          sync.RWMutex
	lastHash    string
	stats       Stats
	cancelWatch context.CancelFunc
	watchWG     sync.WaitGroup
}

func Open(repository *fsrepo.Repository, contentService *content.Service, logger *slog.Logger) (*Service, error) {
	path := filepath.Join(repository.Root(), "state", "index.sqlite")
	service := &Service{repository: repository, content: contentService, logger: logger, path: path, stats: Stats{Status: "building", Path: path}}
	if err := service.openDatabase(); err != nil {
		corrupt := fmt.Sprintf("%s.corrupt-%d", path, time.Now().UTC().UnixMilli())
		_ = os.Rename(path, corrupt)
		if err := service.openDatabase(); err != nil {
			return nil, err
		}
	}
	if err := service.Rebuild(); err != nil {
		service.db.Close()
		return nil, err
	}
	return service, nil
}

func (s *Service) openDatabase() error {
	database, err := sql.Open("sqlite", s.path)
	if err != nil {
		return err
	}
	database.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		`CREATE TABLE IF NOT EXISTS documents (
			kind TEXT NOT NULL,
			id TEXT NOT NULL,
			locale TEXT NOT NULL,
			status TEXT NOT NULL,
			title TEXT NOT NULL,
			summary TEXT NOT NULL,
			markdown TEXT NOT NULL,
			source_path TEXT NOT NULL,
			mtime_ns INTEGER NOT NULL,
			content_hash TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (kind, id, locale)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(kind UNINDEXED, id UNINDEXED, locale UNINDEXED, title, summary, markdown, tokenize='unicode61')`,
		`CREATE TABLE IF NOT EXISTS projection_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			database.Close()
			return err
		}
	}
	s.db = database
	return nil
}

func (s *Service) Rebuild() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	posts, err := s.content.ListPosts()
	if err != nil {
		return s.fail(err)
	}
	pages, err := s.content.ListPages()
	if err != nil {
		return s.fail(err)
	}
	transaction, err := s.db.Begin()
	if err != nil {
		return s.fail(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec("DELETE FROM documents"); err != nil {
		return s.fail(err)
	}
	if _, err := transaction.Exec("DELETE FROM documents_fts"); err != nil {
		return s.fail(err)
	}
	count := 0
	for _, group := range []struct {
		kind  string
		root  string
		items []contentItem
	}{
		{kind: "Post", root: "posts", items: toContentItems(posts)},
		{kind: "Page", root: "pages", items: toContentItems(pages)},
	} {
		for _, item := range group.items {
			for locale, localized := range item.content {
				relative := filepath.Join("content", group.root, item.id, locale+".md")
				absolute := filepath.Join(s.repository.Root(), relative)
				info, err := os.Stat(absolute)
				if err != nil {
					return s.fail(err)
				}
				data, err := os.ReadFile(absolute)
				if err != nil {
					return s.fail(err)
				}
				digest := sha256.Sum256(data)
				if _, err := transaction.Exec(`INSERT INTO documents(kind,id,locale,status,title,summary,markdown,source_path,mtime_ns,content_hash,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, group.kind, item.id, locale, item.status, localized.title, localized.summary, localized.markdown, filepath.ToSlash(relative), info.ModTime().UnixNano(), hex.EncodeToString(digest[:]), item.updatedAt.Format(time.RFC3339Nano)); err != nil {
					return s.fail(err)
				}
				if _, err := transaction.Exec(`INSERT INTO documents_fts(kind,id,locale,title,summary,markdown) VALUES(?,?,?,?,?,?)`, group.kind, item.id, locale, localized.title, localized.summary, localized.markdown); err != nil {
					return s.fail(err)
				}
				count++
			}
		}
	}
	now := time.Now().UTC()
	if _, err := transaction.Exec(`INSERT INTO projection_meta(key,value) VALUES('lastIndexedAt',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, now.Format(time.RFC3339Nano)); err != nil {
		return s.fail(err)
	}
	if err := transaction.Commit(); err != nil {
		return s.fail(err)
	}
	fingerprint, err := s.fingerprint()
	if err != nil {
		return s.fail(err)
	}
	s.lastHash = fingerprint
	s.stats.Status = "ready"
	s.stats.Documents = count
	s.stats.LastIndexedAt = &now
	s.stats.LastError = ""
	return nil
}

func (s *Service) StartWatcher(parent context.Context, onExternalChange func(context.Context) error) {
	s.mu.Lock()
	if s.cancelWatch != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancelWatch = cancel
	s.watchWG.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.watchWG.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fingerprint, err := s.fingerprint()
				if err != nil {
					s.recordWatchError(err)
					continue
				}
				s.mu.RLock()
				unchanged := fingerprint == s.lastHash && s.stats.Status == "ready"
				s.mu.RUnlock()
				if unchanged {
					continue
				}
				if err := s.Rebuild(); err != nil {
					s.logger.Error("projection rebuild after file change failed", "error", err)
					continue
				}
				now := time.Now().UTC()
				s.mu.Lock()
				s.stats.LastExternalChangeAt = &now
				s.mu.Unlock()
				if onExternalChange != nil {
					changeCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
					err := onExternalChange(changeCtx)
					cancel()
					if err != nil {
						s.recordWatchError(err)
						s.logger.Error("static rebuild after file change failed; previous release retained", "error", err)
					} else {
						s.logger.Info("external file change indexed and published")
					}
				}
			}
		}
	}()
}

func (s *Service) Close() error {
	s.mu.Lock()
	if s.cancelWatch != nil {
		s.cancelWatch()
		s.cancelWatch = nil
	}
	s.mu.Unlock()
	s.watchWG.Wait()
	return s.db.Close()
}

func (s *Service) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *Service) Search(query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT d.kind,d.id,d.locale,d.status,d.title,d.summary FROM documents_fts f JOIN documents d ON d.kind=f.kind AND d.id=f.id AND d.locale=f.locale WHERE documents_fts MATCH ? ORDER BY bm25(documents_fts) LIMIT ?`, ftsQuery(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SearchResult{}
	for rows.Next() {
		var item SearchResult
		if err := rows.Scan(&item.Kind, &item.ID, &item.Locale, &item.Status, &item.Title, &item.Summary); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) fingerprint() (string, error) {
	hash := sha256.New()
	for _, root := range []string{"config/site.yaml", "config/locales.yaml", "content", "themes/settings"} {
		absolute := filepath.Join(s.repository.Root(), filepath.FromSlash(root))
		err := filepath.WalkDir(absolute, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("projection source contains a symlink")
			}
			relative, err := filepath.Rel(s.repository.Root(), path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			io.WriteString(hash, relative)
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Service) fail(err error) error {
	s.stats.Status = "error"
	s.stats.LastError = err.Error()
	return err
}

func (s *Service) recordWatchError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.Status = "error"
	s.stats.LastError = err.Error()
}

type localizedItem struct{ title, summary, markdown string }
type contentItem struct {
	id        string
	status    string
	updatedAt time.Time
	content   map[string]localizedItem
}

func toContentItems(items []domain.Post) []contentItem {
	result := make([]contentItem, 0, len(items))
	for _, item := range items {
		localized := make(map[string]localizedItem, len(item.Content))
		for locale, value := range item.Content {
			localized[locale] = localizedItem{title: value.Title, summary: value.Summary, markdown: value.Markdown}
		}
		result = append(result, contentItem{id: item.Meta.ID, status: string(item.Meta.Status), updatedAt: item.Meta.UpdatedAt, content: localized})
	}
	return result
}

func ftsQuery(query string) string {
	parts := strings.Fields(query)
	for index, part := range parts {
		parts[index] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"*`
	}
	sort.Strings(parts)
	return strings.Join(parts, " AND ")
}
