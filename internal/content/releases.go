package content

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

type releasePointer struct {
	SchemaVersion int       `yaml:"schemaVersion"`
	Snapshot      string    `yaml:"snapshot"`
	Revision      int       `yaml:"revision"`
	UpdatedAt     time.Time `yaml:"updatedAt"`
}

const retainedContentReleases = 20

func (s *Service) decoratePublicationState(kind string, item *domain.Post) {
	normalizeRevisionPointers(&item.Meta)
	item.Meta.HasUnpublishedChanges = false
	root, err := releaseRoot(kind, item.Meta.ID)
	if err != nil {
		return
	}
	var pointer releasePointer
	if err := s.repository.ReadYAML(filepath.Join(root, "current.yaml"), &pointer); err == nil {
		item.Meta.ReleaseRevision = pointer.Revision
		item.Meta.HasUnpublishedChanges = item.Meta.Status == domain.ContentStatusPublished && pointer.Revision != item.Meta.HeadRevision
	}
}

func normalizeRevisionPointers(meta *domain.PostMeta) {
	if meta.Visibility == "" {
		meta.Visibility = domain.ContentVisibilityPublic
	}
	if meta.BaseRevision <= 0 {
		meta.BaseRevision = 1
	}
	if meta.HeadRevision <= 0 || meta.HeadRevision != meta.Revision {
		meta.HeadRevision = meta.Revision
	}
}

func advanceHead(meta *domain.PostMeta) {
	meta.Revision++
	meta.HeadRevision = meta.Revision
	if meta.BaseRevision <= 0 {
		meta.BaseRevision = 1
	}
}

// InitializePublishedReleases migrates pre-pointer installations before file
// watchers or unrelated resource builds can observe later head edits. It also
// persists explicit base/head/release revision pointers for legacy content.
func (s *Service) InitializePublishedReleases() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	initialized := 0
	for _, itemKind := range []string{"Post", "Page"} {
		var items []domain.Post
		var err error
		if itemKind == "Post" {
			items, err = s.ListPosts()
		} else {
			items, err = s.ListPages()
		}
		if err != nil {
			return initialized, err
		}
		for _, item := range items {
			normalizeRevisionPointers(&item.Meta)
			if item.Meta.Status == domain.ContentStatusPublished {
				released, err := s.readRelease(itemKind, item.Meta.ID)
				if errors.Is(err, os.ErrNotExist) {
					if err := s.writeRelease(itemKind, item); err != nil {
						return initialized, err
					}
					item.Meta.ReleaseRevision = item.Meta.Revision
					initialized++
				} else if err != nil {
					return initialized, err
				} else {
					item.Meta.ReleaseRevision = released.Meta.Revision
				}
			}
			paths, err := lifecyclePaths(itemKind, item.Meta.ID)
			if err != nil {
				return initialized, err
			}
			var stored domain.PostMeta
			metaPath := filepath.Join(paths.content, "meta.yaml")
			if err := s.repository.ReadYAML(metaPath, &stored); err != nil {
				return initialized, err
			}
			if stored.BaseRevision != item.Meta.BaseRevision || stored.HeadRevision != item.Meta.HeadRevision || stored.ReleaseRevision != item.Meta.ReleaseRevision {
				if err := s.repository.WriteYAML(metaPath, item.Meta, false); err != nil {
					return initialized, err
				}
			}
		}
	}
	return initialized, nil
}

// ListPostsForBuild and ListPagesForBuild substitute the immutable public
// release for every published head. Private releases are omitted entirely;
// draft, unpublished, and recycled heads are retained for the renderer's
// normal status filter.
func (s *Service) ListPostsForBuild() ([]domain.Post, error) {
	posts, err := s.ListPosts()
	if err != nil {
		return nil, err
	}
	return s.publicReleasesForBuild("Post", posts)
}

func (s *Service) ListPagesForBuild() ([]domain.Post, error) {
	pages, err := s.ListPages()
	if err != nil {
		return nil, err
	}
	return s.publicReleasesForBuild("Page", pages)
}

func (s *Service) publicReleasesForBuild(kind string, heads []domain.Post) ([]domain.Post, error) {
	items, err := s.releasesForBuild(kind, heads)
	if err != nil {
		return nil, err
	}
	public := make([]domain.Post, 0, len(items))
	for _, item := range items {
		if item.Meta.Visibility == domain.ContentVisibilityPrivate {
			continue
		}
		public = append(public, item)
	}
	return public, nil
}

// GetPublishedRelease returns the same immutable snapshot that the public
// renderer uses. Dynamic surfaces such as comments must not observe an
// unpublished head while visitors are still reading the previous release.
func (s *Service) GetPublishedRelease(kind, id string) (domain.Post, error) {
	head, err := s.getLifecycleContent(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	if head.Meta.Status != domain.ContentStatusPublished {
		return domain.Post{}, ErrNotFound
	}
	released, err := s.readRelease(kind, id)
	if errors.Is(err, os.ErrNotExist) {
		if head.Meta.Visibility == domain.ContentVisibilityPrivate {
			return domain.Post{}, ErrNotFound
		}
		return head, nil
	}
	if err == nil && released.Meta.Visibility == domain.ContentVisibilityPrivate {
		return domain.Post{}, ErrNotFound
	}
	return released, err
}

func (s *Service) releasesForBuild(kind string, heads []domain.Post) ([]domain.Post, error) {
	result := make([]domain.Post, 0, len(heads))
	for _, head := range heads {
		if head.Meta.Status != domain.ContentStatusPublished {
			result = append(result, head)
			continue
		}
		released, err := s.readRelease(kind, head.Meta.ID)
		if errors.Is(err, os.ErrNotExist) {
			// Backward-compatible migration path: the pre-release-pointer head is
			// the only public snapshot until its first edit or explicit publish.
			result = append(result, head)
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, released)
	}
	return result, nil
}

func (s *Service) ensureRelease(kind string, item domain.Post) error {
	if item.Meta.Status != domain.ContentStatusPublished {
		return nil
	}
	if _, err := s.readRelease(kind, item.Meta.ID); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.writeRelease(kind, item)
}

func (s *Service) writeRelease(kind string, item domain.Post) error {
	normalizeRevisionPointers(&item.Meta)
	item.Meta.ReleaseRevision = item.Meta.Revision
	root, err := releaseRoot(kind, item.Meta.ID)
	if err != nil {
		return err
	}
	snapshot := fmt.Sprintf("%06d-%d", item.Meta.Revision, time.Now().UTC().UnixMilli())
	base := filepath.Join(root, "snapshots", snapshot)
	if err := s.repository.WriteYAML(filepath.Join(base, "meta.yaml"), item.Meta, false); err != nil {
		return err
	}
	for locale, localized := range item.Content {
		data, err := encodeMarkdown(localized)
		if err != nil {
			return err
		}
		if err := s.repository.WriteFile(filepath.Join(base, locale+".md"), data, 0o640); err != nil {
			return err
		}
	}
	pointer := releasePointer{SchemaVersion: domain.SchemaVersion, Snapshot: snapshot, Revision: item.Meta.Revision, UpdatedAt: time.Now().UTC()}
	if err := s.repository.WriteYAML(filepath.Join(root, "current.yaml"), pointer, false); err != nil {
		return err
	}
	s.pruneReleaseSnapshots(root, snapshot)
	return nil
}

func (s *Service) readRelease(kind, id string) (domain.Post, error) {
	root, err := releaseRoot(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	var pointer releasePointer
	if err := s.repository.ReadYAML(filepath.Join(root, "current.yaml"), &pointer); err != nil {
		return domain.Post{}, err
	}
	if pointer.SchemaVersion != domain.SchemaVersion || !validRevisionID(pointer.Snapshot) {
		return domain.Post{}, errors.New("invalid content release pointer")
	}
	base := filepath.Join(root, "snapshots", pointer.Snapshot)
	var meta domain.PostMeta
	if err := s.repository.ReadYAML(filepath.Join(base, "meta.yaml"), &meta); err != nil {
		return domain.Post{}, err
	}
	if meta.ID != id || !strings.EqualFold(meta.Kind, kind) || meta.Status != domain.ContentStatusPublished {
		return domain.Post{}, errors.New("invalid content release snapshot")
	}
	normalizeRevisionPointers(&meta)
	meta.ReleaseRevision = pointer.Revision
	contents := make(map[string]domain.LocalizedMarkdown, len(meta.Locales))
	for locale := range meta.Locales {
		data, err := s.repository.ReadFile(filepath.Join(base, locale+".md"))
		if err != nil {
			return domain.Post{}, err
		}
		localized, err := decodeMarkdown(data)
		if err != nil {
			return domain.Post{}, err
		}
		contents[locale] = localized
	}
	return domain.Post{Meta: meta, Content: contents}, nil
}

// PromoteAITranslation merges only the verified AI locale into the current
// public snapshot. It never promotes unrelated head edits made while the task
// was running.
func (s *Service) PromoteAITranslation(kind, id, locale string, sourceRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	head, err := s.getLifecycleContent(kind, id)
	if err != nil {
		return err
	}
	state, ok := head.Meta.Locales[locale]
	if !ok || state.Origin != domain.LocaleOriginAI || state.State != "current" || state.SourceRevision != sourceRevision {
		return ErrSourceChanged
	}
	if head.Meta.Status != domain.ContentStatusPublished {
		return nil
	}
	if err := s.ensureRelease(kind, head); err != nil {
		return err
	}
	released, err := s.readRelease(kind, id)
	if err != nil {
		return err
	}
	if released.Meta.Locales[released.Meta.SourceLocale].Revision != sourceRevision {
		// The AI result belongs to an unpublished source head. Keep it on the
		// head so an explicit publish can release the whole coherent snapshot.
		return nil
	}
	released.Meta.Locales[locale] = state
	advanceHead(&released.Meta)
	released.Meta.ReleaseRevision = released.Meta.Revision
	released.Meta.UpdatedAt = time.Now().UTC()
	released.Content[locale] = head.Content[locale]
	if err := s.writeRelease(kind, released); err != nil {
		return err
	}
	head.Meta.ReleaseRevision = released.Meta.Revision
	paths, err := lifecyclePaths(kind, id)
	if err != nil {
		return err
	}
	return s.repository.WriteYAML(filepath.Join(paths.content, "meta.yaml"), head.Meta, false)
}

func releaseRoot(kind, id string) (string, error) {
	if !validID(id) {
		return "", ErrInvalidID
	}
	switch strings.ToLower(kind) {
	case "post", "posts":
		return filepath.Join("releases", "posts", id), nil
	case "page", "pages":
		return filepath.Join("releases", "pages", id), nil
	default:
		return "", ErrNotFound
	}
}

func (s *Service) pruneReleaseSnapshots(root, current string) {
	snapshots := filepath.Join(root, "snapshots")
	entries, err := s.repository.ReadDir(snapshots)
	if err != nil {
		return
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && validRevisionID(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	kept := 0
	for _, id := range ids {
		if id == current || kept < retainedContentReleases-1 {
			kept++
			continue
		}
		_ = s.repository.RemoveTree(filepath.Join(snapshots, id))
	}
}
