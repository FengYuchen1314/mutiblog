package content

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

var ErrInvalidStatus = errors.New("invalid content status transition")

type RevisionSummary struct {
	ID        string               `json:"id"`
	Revision  int                  `json:"revision"`
	CreatedAt time.Time            `json:"createdAt"`
	Status    domain.ContentStatus `json:"status"`
	Title     string               `json:"title"`
	IsBase    bool                 `json:"isBase"`
	IsHead    bool                 `json:"isHead"`
	IsRelease bool                 `json:"isRelease"`
}

func (s *Service) ListRevisions(kind, id string) ([]RevisionSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	paths, err := lifecyclePaths(kind, id)
	if err != nil {
		return nil, err
	}
	current, err := s.getLifecycleContentLocked(kind, id)
	if err != nil {
		return nil, err
	}
	entries, err := s.repository.ReadDir(paths.revisions)
	if errors.Is(err, os.ErrNotExist) {
		return []RevisionSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	activeReleaseSnapshot := ""
	if root, rootErr := releaseRoot(kind, id); rootErr == nil {
		var pointer releasePointer
		if pointerErr := s.repository.ReadYAML(filepath.Join(root, "current.yaml"), &pointer); pointerErr == nil && pointer.SchemaVersion == domain.SchemaVersion && validRevisionID(pointer.HistorySnapshot) {
			activeReleaseSnapshot = pointer.HistorySnapshot
		}
	}
	hasExactReleaseSnapshot := activeReleaseSnapshot != ""
	items := make([]RevisionSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validRevisionID(entry.Name()) {
			continue
		}
		var meta domain.PostMeta
		if err := s.repository.ReadYAML(filepath.Join(paths.revisions, entry.Name(), "meta.yaml"), &meta); err != nil || meta.ID != id {
			continue
		}
		createdAt := meta.UpdatedAt
		if parts := strings.Split(entry.Name(), "-"); len(parts) == 2 {
			if milliseconds, parseErr := strconv.ParseInt(parts[1], 10, 64); parseErr == nil {
				createdAt = time.UnixMilli(milliseconds).UTC()
			}
		}
		title := id
		if data, readErr := s.repository.ReadFile(filepath.Join(paths.revisions, entry.Name(), meta.SourceLocale+".md")); readErr == nil {
			if localized, decodeErr := decodeMarkdown(data); decodeErr == nil && localized.Title != "" {
				title = localized.Title
			}
		}
		isRelease := meta.Revision == current.Meta.ReleaseRevision
		if hasExactReleaseSnapshot {
			isRelease = entry.Name() == activeReleaseSnapshot
		}
		items = append(items, RevisionSummary{ID: entry.Name(), Revision: meta.Revision, CreatedAt: createdAt, Status: meta.Status, Title: title, IsBase: meta.Revision == current.Meta.BaseRevision, IsHead: meta.Revision == current.Meta.HeadRevision, IsRelease: isRelease})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) GetRevision(kind, id, revisionID string) (domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	paths, err := lifecyclePaths(kind, id)
	if err != nil || !validRevisionID(revisionID) {
		return domain.Post{}, ErrNotFound
	}
	if _, err := s.getLifecycleContentLocked(kind, id); err != nil {
		return domain.Post{}, err
	}
	return s.readRevision(paths, id, revisionID)
}

func (s *Service) RestoreRevision(kind, id, revisionID string, expectedRevision int) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := lifecyclePaths(kind, id)
	if err != nil || !validRevisionID(revisionID) {
		return domain.Post{}, ErrNotFound
	}
	current, err := s.getLifecycleContentLocked(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	if current.Meta.Revision != expectedRevision {
		return domain.Post{}, ErrConflict
	}
	restored, err := s.readRevision(paths, id, revisionID)
	if err != nil {
		return domain.Post{}, err
	}
	if err := s.snapshotLifecycle(kind, current); err != nil {
		return domain.Post{}, err
	}
	restored.Meta.SchemaVersion = domain.SchemaVersion
	restored.Meta.Kind = current.Meta.Kind
	restored.Meta.ID = current.Meta.ID
	restored.Meta.Status = current.Meta.Status
	restored.Meta.CreatedAt = current.Meta.CreatedAt
	restored.Meta.UpdatedAt = time.Now().UTC()
	restored.Meta.PublishedAt = current.Meta.PublishedAt
	// A revision snapshot may contain an obsolete scheduling marker. Keep the
	// current head's intent long enough for the server to invalidate its exact
	// queued task after this restore; never resurrect the snapshot's old intent.
	restored.Meta.ScheduledRevision = current.Meta.ScheduledRevision
	restored.Meta.Revision = current.Meta.Revision + 1
	restored.Meta.BaseRevision = current.Meta.BaseRevision
	restored.Meta.HeadRevision = restored.Meta.Revision
	restored.Meta.ReleaseRevision = current.Meta.ReleaseRevision
	if err := s.writeLifecycleContent(paths, current, restored); err != nil {
		return domain.Post{}, err
	}
	restored.Meta.HasUnpublishedChanges = restored.Meta.Status == domain.ContentStatusPublished
	return restored, nil
}

func (s *Service) ChangeStatus(kind, id, action string, expectedRevision int) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := lifecyclePaths(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	item, err := s.getLifecycleContentLocked(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	if item.Meta.Revision != expectedRevision {
		return domain.Post{}, ErrConflict
	}
	target := item.Meta.Status
	switch action {
	case "unpublish":
		if item.Meta.Status != domain.ContentStatusPublished {
			return domain.Post{}, ErrInvalidStatus
		}
		target = domain.ContentStatusUnpublished
	case "recycle":
		if item.Meta.Status == domain.ContentStatusRecycled {
			return item, nil
		}
		target = domain.ContentStatusRecycled
	case "restore":
		if item.Meta.Status != domain.ContentStatusRecycled {
			return domain.Post{}, ErrInvalidStatus
		}
		target = domain.ContentStatusDraft
		if item.Meta.PublishedAt != nil {
			target = domain.ContentStatusUnpublished
		}
	default:
		return domain.Post{}, ErrInvalidStatus
	}
	if err := s.snapshotLifecycle(kind, item); err != nil {
		return domain.Post{}, err
	}
	item.Meta.Status = target
	advanceHead(&item.Meta)
	item.Meta.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join(paths.content, "meta.yaml"), item.Meta, false); err != nil {
		return domain.Post{}, err
	}
	item.Meta.HasUnpublishedChanges = false
	return item, nil
}

func (s *Service) DeleteRecycled(kind, id string, expectedRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	paths, err := lifecyclePaths(kind, id)
	if err != nil {
		return err
	}
	item, err := s.getLifecycleContentLocked(kind, id)
	if err != nil {
		return err
	}
	if item.Meta.Revision != expectedRevision {
		return ErrConflict
	}
	if item.Meta.Status != domain.ContentStatusRecycled {
		return ErrInvalidStatus
	}
	for _, target := range []string{
		paths.content,
		paths.revisions,
		paths.releases,
		filepath.Join("comments", item.Meta.Kind, id),
	} {
		if err := s.repository.RemoveTree(target); err != nil {
			return err
		}
	}
	for _, ancillaryPath := range []string{
		filepath.Join("upvotes", strings.ToLower(item.Meta.Kind)+"s", id+".yaml"),
		filepath.Join("visits", strings.ToLower(item.Meta.Kind)+"s", id+".yaml"),
	} {
		exists, err := s.repository.Exists(ancillaryPath)
		if err != nil {
			return err
		}
		if exists {
			if err := s.repository.RemoveFile(ancillaryPath); err != nil {
				return err
			}
		}
	}
	return nil
}

type lifecycleLocation struct {
	content   string
	revisions string
	releases  string
}

func lifecyclePaths(kind, id string) (lifecycleLocation, error) {
	if !validID(id) {
		return lifecycleLocation{}, ErrInvalidID
	}
	switch strings.ToLower(kind) {
	case "post", "posts":
		return lifecycleLocation{content: filepath.Join("content", "posts", id), revisions: filepath.Join("revisions", "posts", id), releases: filepath.Join("releases", "posts", id)}, nil
	case "page", "pages":
		return lifecycleLocation{content: filepath.Join("content", "pages", id), revisions: filepath.Join("revisions", "pages", id), releases: filepath.Join("releases", "pages", id)}, nil
	default:
		return lifecycleLocation{}, ErrNotFound
	}
}

// getLifecycleContentLocked expects the caller to hold s.mu for reading or
// writing. Keeping this dispatch lock-free prevents writer-to-reader lock
// recursion in lifecycle mutations.
func (s *Service) getLifecycleContentLocked(kind, id string) (domain.Post, error) {
	if strings.HasPrefix(strings.ToLower(kind), "page") {
		return s.getPageLocked(id)
	}
	return s.getPostLocked(id)
}

func (s *Service) snapshotLifecycle(kind string, item domain.Post) error {
	if strings.HasPrefix(strings.ToLower(kind), "page") {
		return s.snapshotPage(item)
	}
	return s.snapshot(item)
}

func (s *Service) readRevision(paths lifecycleLocation, id, revisionID string) (domain.Post, error) {
	base := filepath.Join(paths.revisions, revisionID)
	var meta domain.PostMeta
	if err := s.repository.ReadYAML(filepath.Join(base, "meta.yaml"), &meta); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Post{}, ErrNotFound
		}
		return domain.Post{}, err
	}
	if meta.ID != id || meta.Kind == "" {
		return domain.Post{}, ErrNotFound
	}
	normalizeRevisionPointers(&meta)
	contents := make(map[string]domain.LocalizedMarkdown, len(meta.Locales))
	for locale := range meta.Locales {
		data, err := s.repository.ReadFile(filepath.Join(base, locale+".md"))
		if err != nil {
			return domain.Post{}, err
		}
		localized, err := decodeMarkdown(data)
		if err != nil {
			return domain.Post{}, fmt.Errorf("decode revision locale %s: %w", locale, err)
		}
		contents[locale] = localized
	}
	return domain.Post{Meta: meta, Content: contents}, nil
}

func (s *Service) writeLifecycleContent(paths lifecycleLocation, current, restored domain.Post) error {
	for locale, localized := range restored.Content {
		data, err := encodeMarkdown(localized)
		if err != nil {
			return err
		}
		if err := s.repository.WriteFile(filepath.Join(paths.content, locale+".md"), data, 0o640); err != nil {
			return err
		}
	}
	if err := s.repository.WriteYAML(filepath.Join(paths.content, "meta.yaml"), restored.Meta, false); err != nil {
		return err
	}
	for locale := range current.Meta.Locales {
		if _, retained := restored.Meta.Locales[locale]; retained {
			continue
		}
		if err := s.repository.RemoveFile(filepath.Join(paths.content, locale+".md")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove locale %s after revision restore: %w", locale, err)
		}
	}
	return nil
}

func validRevisionID(id string) bool {
	parts := strings.Split(id, "-")
	if len(parts) != 2 || len(parts[0]) != 6 || len(parts[1]) < 13 {
		return false
	}
	_, firstErr := strconv.ParseUint(parts[0], 10, 64)
	_, secondErr := strconv.ParseInt(parts[1], 10, 64)
	return firstErr == nil && secondErr == nil && filepath.Base(id) == id
}
