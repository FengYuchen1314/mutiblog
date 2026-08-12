package content

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
)

type releasePointer struct {
	SchemaVersion   int       `yaml:"schemaVersion"`
	Snapshot        string    `yaml:"snapshot"`
	HistorySnapshot string    `yaml:"historySnapshot,omitempty"`
	Revision        int       `yaml:"revision"`
	UpdatedAt       time.Time `yaml:"updatedAt"`
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
			items, err = s.listPostsLocked()
		} else {
			items, err = s.listPagesLocked()
		}
		if err != nil {
			return initialized, err
		}
		for _, item := range items {
			normalizeRevisionPointers(&item.Meta)
			released, releaseErr := s.readRelease(itemKind, item.Meta.ID)
			switch {
			case releaseErr == nil:
				migrated, err := s.ensureReleaseHistory(itemKind, released)
				if err != nil {
					return initialized, err
				}
				if migrated {
					initialized++
				}
				if item.Meta.Status == domain.ContentStatusPublished {
					// The immutable release, not a mutable head that may already
					// belong to a later interrupted publication, owns the legacy
					// generation migration. Otherwise P2's committed head marker
					// could be rewritten back to P1 before scheduled recovery can
					// promote P2's pending release.
					releaseGeneration := released.Meta.PublicationGeneration
					if releaseGeneration <= 0 {
						// Legacy releases predate the stable publication marker. Their
						// current immutable release is authoritative; future explicit
						// publishes will allocate a newer generation normally.
						releaseGeneration = released.Meta.Revision
					}
					if released.Meta.PublicationGeneration != releaseGeneration {
						released.Meta.PublicationGeneration = releaseGeneration
						if err := s.writeReleasePublicationGeneration(itemKind, released); err != nil {
							return initialized, err
						}
					}
					// A larger head marker is a durable P2 metadata commit whose
					// release pointer did not make it to disk. Preserve it and
					// materialize the same pending release that Publish would have
					// written; do not let P1's pointer rebind P2 to generation P1.
					if item.Meta.PublicationGeneration > releaseGeneration {
						pending := publicationReleaseWithAutomaticTranslationPending(item)
						if err := s.writeRelease(itemKind, pending); err != nil {
							return initialized, err
						}
						item.Meta.ReleaseRevision = item.Meta.Revision
						initialized++
					} else {
						item.Meta.ReleaseRevision = released.Meta.Revision
						item.Meta.PublicationGeneration = releaseGeneration
					}
				} else {
					item.Meta.ReleaseRevision = released.Meta.Revision
				}
			case errors.Is(releaseErr, os.ErrNotExist):
				if item.Meta.Status == domain.ContentStatusPublished {
					hasPublicationGeneration := item.Meta.PublicationGeneration > 0
					if !hasPublicationGeneration {
						item.Meta.PublicationGeneration = item.Meta.Revision
					}
					release := item
					if hasPublicationGeneration {
						// A marker written before its release pointer is evidence of a
						// modern explicit publish. Recreate its pending release rather
						// than exposing old AI targets as current after restart.
						release = publicationReleaseWithAutomaticTranslationPending(item)
					}
					if err := s.writeRelease(itemKind, release); err != nil {
						return initialized, err
					}
					item.Meta.ReleaseRevision = item.Meta.Revision
					initialized++
				}
			default:
				return initialized, releaseErr
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
			if stored.BaseRevision != item.Meta.BaseRevision || stored.HeadRevision != item.Meta.HeadRevision || stored.ReleaseRevision != item.Meta.ReleaseRevision || stored.PublicationGeneration != item.Meta.PublicationGeneration {
				if err := s.repository.WriteYAML(metaPath, item.Meta, false); err != nil {
					return initialized, err
				}
			}
		}
	}
	return initialized, nil
}

// writeReleasePublicationGeneration updates only the immutable snapshot and
// its exact history mirror. Rewriting through writeRelease would allocate a
// fresh snapshot ID during startup migration, which would make an otherwise
// no-op legacy normalization look like a new publication.
func (s *Service) writeReleasePublicationGeneration(kind string, released domain.Post) error {
	root, err := releaseRoot(kind, released.Meta.ID)
	if err != nil {
		return err
	}
	var pointer releasePointer
	if err := s.repository.ReadYAML(filepath.Join(root, "current.yaml"), &pointer); err != nil {
		return err
	}
	if pointer.SchemaVersion != domain.SchemaVersion || !validRevisionID(pointer.Snapshot) {
		return errors.New("invalid content release pointer")
	}
	if err := s.writeContentSnapshot(filepath.Join(root, "snapshots", pointer.Snapshot), released); err != nil {
		return err
	}
	if !validRevisionID(pointer.HistorySnapshot) {
		return nil
	}
	paths, err := lifecyclePaths(kind, released.Meta.ID)
	if err != nil {
		return err
	}
	return s.writeContentSnapshot(filepath.Join(paths.revisions, pointer.HistorySnapshot), released)
}

// ListPostsForBuild and ListPagesForBuild substitute an immutable public
// release for every published head. A newly published release can be pending
// automatic translations, though. That pending snapshot remains the current
// release for its translation task, but it must not make an unrelated static
// build fail exact-locale validation. Build reads therefore fall back to the
// newest complete immutable release, or omit a first publication until it is
// complete. No release pointer or history entry is changed by this selection.
func (s *Service) ListPostsForBuild() ([]domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	posts, err := s.listPostsLocked()
	if err != nil {
		return nil, err
	}
	return s.publicReleasesForBuild("Post", posts)
}

func (s *Service) ListPagesForBuild() ([]domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pages, err := s.listPagesLocked()
	if err != nil {
		return nil, err
	}
	return s.publicReleasesForBuild("Page", pages)
}

// ListPostsForLocalization and ListPagesForLocalization retain the active
// public pointer, including an incomplete pending publication. Localization
// planners must translate that exact release; they must never receive the
// last-known-good fallback that a static build uses.
func (s *Service) ListPostsForLocalization() ([]domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	posts, err := s.listPostsLocked()
	if err != nil {
		return nil, err
	}
	return s.currentPublicReleases("Post", posts)
}

func (s *Service) ListPagesForLocalization() ([]domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pages, err := s.listPagesLocked()
	if err != nil {
		return nil, err
	}
	return s.currentPublicReleases("Page", pages)
}

func (s *Service) publicReleasesForBuild(kind string, heads []domain.Post) ([]domain.Post, error) {
	visibleLocales, err := s.renderableLocalesForBuild()
	if err != nil {
		return nil, err
	}
	return s.releasesForBuild(kind, heads, visibleLocales)
}

func (s *Service) currentPublicReleases(kind string, heads []domain.Post) ([]domain.Post, error) {
	result := make([]domain.Post, 0, len(heads))
	for _, head := range heads {
		if head.Meta.Status != domain.ContentStatusPublished {
			if head.Meta.Visibility != domain.ContentVisibilityPrivate {
				result = append(result, head)
			}
			continue
		}
		released, err := s.readRelease(kind, head.Meta.ID)
		if errors.Is(err, os.ErrNotExist) {
			if head.Meta.Visibility != domain.ContentVisibilityPrivate {
				result = append(result, head)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if released.Meta.Visibility != domain.ContentVisibilityPrivate {
			result = append(result, released)
		}
	}
	return result, nil
}

// GetPublishedRelease returns the active immutable release pointer. Translation
// and dynamic surfaces use that exact pending publication; static builds may
// instead select an older complete release through List*ForBuild.
func (s *Service) GetPublishedRelease(kind, id string) (domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	head, err := s.getLifecycleContentLocked(kind, id)
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

func (s *Service) releasesForBuild(kind string, heads []domain.Post, visibleLocales map[string]struct{}) ([]domain.Post, error) {
	result := make([]domain.Post, 0, len(heads))
	for _, head := range heads {
		if head.Meta.Status != domain.ContentStatusPublished {
			if head.Meta.Visibility != domain.ContentVisibilityPrivate {
				result = append(result, head)
			}
			continue
		}
		released, err := s.readRelease(kind, head.Meta.ID)
		if errors.Is(err, os.ErrNotExist) {
			// Backward-compatible migration path: the pre-release-pointer head is
			// the only public snapshot until its first edit or explicit publish.
			// It still has to be complete for every currently renderable locale.
			if head.Meta.Visibility != domain.ContentVisibilityPrivate && releaseRenderableForBuild(head, visibleLocales) {
				result = append(result, head)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		// The active release owns visibility. Falling back past an explicit
		// private publication would otherwise leak the older public snapshot.
		if released.Meta.Visibility == domain.ContentVisibilityPrivate {
			continue
		}
		selected, available, selectErr := s.latestRenderableReleaseForBuild(kind, head.Meta.ID, released, visibleLocales)
		if selectErr != nil {
			return nil, selectErr
		}
		if available {
			result = append(result, selected)
		}
	}
	return result, nil
}

// renderableLocalesForBuild mirrors publisher.snapshot's locale visibility:
// ready, legacy statusless, and internally building locales are present in a
// renderer input. Provisioning and failed locales are intentionally hidden.
func (s *Service) renderableLocalesForBuild() (map[string]struct{}, error) {
	config, err := s.locales()
	if err != nil {
		return nil, err
	}
	localeconfig.Normalize(&config)
	locales := make(map[string]struct{}, len(config.Enabled))
	for _, definition := range config.Enabled {
		if !definition.Enabled {
			continue
		}
		switch definition.Status {
		case "", domain.LocaleStatusReady, domain.LocaleStatusBuilding:
			locales[definition.Code] = struct{}{}
		}
	}
	return locales, nil
}

// latestRenderableReleaseForBuild deliberately leaves current.yaml untouched.
// The current snapshot may be incomplete while an automatic translation task
// owns it; scanning immutable release snapshots lets a later, unrelated build
// keep serving the last coherent version instead of failing globally.
func (s *Service) latestRenderableReleaseForBuild(kind, id string, current domain.Post, visibleLocales map[string]struct{}) (domain.Post, bool, error) {
	if releaseRenderableForBuild(current, visibleLocales) {
		return current, true, nil
	}
	root, err := releaseRoot(kind, id)
	if err != nil {
		return domain.Post{}, false, err
	}
	entries, err := s.repository.ReadDir(filepath.Join(root, "snapshots"))
	if errors.Is(err, os.ErrNotExist) {
		return domain.Post{}, false, nil
	}
	if err != nil {
		return domain.Post{}, false, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, entry := range entries {
		if !entry.IsDir() || !validRevisionID(entry.Name()) {
			continue
		}
		candidate, readErr := s.readReleaseSnapshot(kind, id, entry.Name())
		// A partial or corrupt historical snapshot is never a reason to make an
		// otherwise unrelated public build fail. It cannot become a fallback.
		if readErr != nil || candidate.Meta.Revision >= current.Meta.Revision {
			continue
		}
		if candidate.Meta.Visibility == domain.ContentVisibilityPrivate || !releaseRenderableForBuild(candidate, visibleLocales) {
			continue
		}
		return candidate, true, nil
	}
	return domain.Post{}, false, nil
}

// releaseRenderableForBuild matches the renderer's published Post/Page
// exact-locale contract closely enough to choose a safe immutable snapshot
// before handing it to the renderer. The renderer remains the final validator.
func releaseRenderableForBuild(item domain.Post, visibleLocales map[string]struct{}) bool {
	if item.Meta.Status != domain.ContentStatusPublished {
		return true
	}
	sourceLocale := item.Meta.SourceLocale
	sourceState, exists := item.Meta.Locales[sourceLocale]
	if !exists || sourceState.Origin != domain.LocaleOriginSource || sourceState.State != "current" || sourceState.SourceRevision != sourceState.Revision {
		return false
	}
	source, exists := item.Content[sourceLocale]
	if !exists {
		return false
	}
	locales := make(map[string]struct{}, len(visibleLocales)+1)
	for locale := range visibleLocales {
		locales[locale] = struct{}{}
	}
	locales[sourceLocale] = struct{}{}
	for locale := range locales {
		target, exists := item.Content[locale]
		if !exists || !completeLocalizedMarkdownForBuild(source, target) {
			return false
		}
		state, exists := item.Meta.Locales[locale]
		if !exists || state.State != "current" {
			return false
		}
		if locale == sourceLocale {
			if state.Origin != domain.LocaleOriginSource || state.SourceRevision != state.Revision {
				return false
			}
			continue
		}
		if state.SourceRevision != sourceState.Revision {
			return false
		}
	}
	return true
}

func completeLocalizedMarkdownForBuild(source, target domain.LocalizedMarkdown) bool {
	for _, values := range [][2]string{
		{source.Title, target.Title},
		{source.Summary, target.Summary},
		{source.SEOTitle, target.SEOTitle},
		{source.SEODescription, target.SEODescription},
		{source.Markdown, target.Markdown},
	} {
		if strings.TrimSpace(values[0]) != "" && strings.TrimSpace(values[1]) == "" {
			return false
		}
	}
	return true
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

func commitPublication(previous, published domain.PostMeta, writeMeta func(domain.PostMeta) error, writeRelease func() error) error {
	if err := writeMeta(published); err != nil {
		return err
	}
	releaseErr := writeRelease()
	if releaseErr == nil {
		return nil
	}
	if rollbackErr := writeMeta(previous); rollbackErr != nil {
		return errors.Join(releaseErr, fmt.Errorf("roll back publication metadata: %w", rollbackErr))
	}
	return releaseErr
}

func (s *Service) writeRelease(kind string, item domain.Post) error {
	normalizeRevisionPointers(&item.Meta)
	item.Meta.ReleaseRevision = item.Meta.Revision
	root, err := releaseRoot(kind, item.Meta.ID)
	if err != nil {
		return err
	}
	paths, err := lifecyclePaths(kind, item.Meta.ID)
	if err != nil {
		return err
	}
	snapshot, err := s.nextSnapshotID(item.Meta.Revision, filepath.Join(root, "snapshots"), paths.revisions)
	if err != nil {
		return err
	}
	base := filepath.Join(root, "snapshots", snapshot)
	if err := s.writeContentSnapshot(base, item); err != nil {
		return err
	}
	// The release pointer's snapshot ID is also the history identity. Mirroring
	// the exact public snapshot prevents a synthesized AI release from being
	// confused with an unrelated head revision that happens to share its number.
	if err := s.writeContentSnapshot(filepath.Join(paths.revisions, snapshot), item); err != nil {
		return err
	}
	pointer := releasePointer{SchemaVersion: domain.SchemaVersion, Snapshot: snapshot, HistorySnapshot: snapshot, Revision: item.Meta.Revision, UpdatedAt: time.Now().UTC()}
	if err := s.repository.WriteYAML(filepath.Join(root, "current.yaml"), pointer, false); err != nil {
		return err
	}
	s.pruneReleaseSnapshots(root, snapshot)
	return nil
}

func (s *Service) ensureReleaseHistory(kind string, released domain.Post) (bool, error) {
	root, err := releaseRoot(kind, released.Meta.ID)
	if err != nil {
		return false, err
	}
	var pointer releasePointer
	if err := s.repository.ReadYAML(filepath.Join(root, "current.yaml"), &pointer); err != nil {
		return false, err
	}
	if pointer.SchemaVersion != domain.SchemaVersion || !validRevisionID(pointer.Snapshot) {
		return false, errors.New("invalid content release pointer")
	}
	paths, err := lifecyclePaths(kind, released.Meta.ID)
	if err != nil {
		return false, err
	}
	if validRevisionID(pointer.HistorySnapshot) {
		existing, readErr := s.readRevision(paths, released.Meta.ID, pointer.HistorySnapshot)
		if readErr == nil && sameContentSnapshot(existing, released) {
			return false, nil
		}
	}
	historySnapshot := ""
	if pointer.HistorySnapshot == "" {
		candidate := pointer.Snapshot
		historyPath := filepath.Join(paths.revisions, candidate)
		exists, err := s.repository.Exists(historyPath)
		if err != nil {
			return false, err
		}
		if !exists {
			historySnapshot = candidate
		} else {
			existing, readErr := s.readRevision(paths, released.Meta.ID, candidate)
			if readErr == nil && sameContentSnapshot(existing, released) {
				historySnapshot = candidate
			}
		}
	}
	if historySnapshot == "" {
		historySnapshot, err = s.nextSnapshotIDFrom(released.Meta.Revision, snapshotMilliseconds(pointer.Snapshot, pointer.UpdatedAt), paths.revisions, filepath.Join(root, "snapshots"))
		if err != nil {
			return false, err
		}
	}
	historyPath := filepath.Join(paths.revisions, historySnapshot)
	exists, err := s.repository.Exists(historyPath)
	if err != nil {
		return false, err
	}
	if !exists {
		if err := s.writeContentSnapshot(historyPath, released); err != nil {
			return false, err
		}
	}
	pointer.HistorySnapshot = historySnapshot
	if err := s.repository.WriteYAML(filepath.Join(root, "current.yaml"), pointer, false); err != nil {
		return false, err
	}
	return true, nil
}

func sameContentSnapshot(first, second domain.Post) bool {
	first.Meta.HasUnpublishedChanges = false
	second.Meta.HasUnpublishedChanges = false
	return reflect.DeepEqual(first, second)
}

func (s *Service) nextSnapshotID(revision int, roots ...string) (string, error) {
	return s.nextSnapshotIDFrom(revision, time.Now().UTC().UnixMilli(), roots...)
}

func (s *Service) nextSnapshotIDFrom(revision int, timestamp int64, roots ...string) (string, error) {
	for {
		candidate := fmt.Sprintf("%06d-%d", revision, timestamp)
		available := true
		for _, root := range roots {
			exists, err := s.repository.Exists(filepath.Join(root, candidate))
			if err != nil {
				return "", err
			}
			if exists {
				available = false
				break
			}
		}
		if available {
			return candidate, nil
		}
		timestamp++
	}
}

func snapshotMilliseconds(id string, fallback time.Time) int64 {
	parts := strings.Split(id, "-")
	if len(parts) == 2 {
		if milliseconds, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			return milliseconds
		}
	}
	if !fallback.IsZero() {
		return fallback.UTC().UnixMilli()
	}
	return time.Now().UTC().UnixMilli()
}

func (s *Service) writeContentSnapshot(base string, item domain.Post) error {
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
	released, err := s.readReleaseSnapshot(kind, id, pointer.Snapshot)
	if err != nil {
		return domain.Post{}, err
	}
	if pointer.Revision != released.Meta.Revision {
		return domain.Post{}, errors.New("content release revision does not match its snapshot")
	}
	released.Meta.ReleaseRevision = pointer.Revision
	return released, nil
}

func (s *Service) readReleaseSnapshot(kind, id, snapshot string) (domain.Post, error) {
	root, err := releaseRoot(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	if !validRevisionID(snapshot) {
		return domain.Post{}, errors.New("invalid content release snapshot")
	}
	base := filepath.Join(root, "snapshots", snapshot)
	var meta domain.PostMeta
	if err := s.repository.ReadYAML(filepath.Join(base, "meta.yaml"), &meta); err != nil {
		return domain.Post{}, err
	}
	if meta.ID != id || !strings.EqualFold(meta.Kind, kind) || meta.Status != domain.ContentStatusPublished {
		return domain.Post{}, errors.New("invalid content release snapshot")
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
	return s.promoteTranslation(kind, id, locale, sourceRevision, false, 0)
}

// PromoteAITranslationForPublication is the generation-fenced variant used by
// automatic publication tasks. It closes the read-to-promote race with a
// newer explicit publish that kept the same source content.
func (s *Service) PromoteAITranslationForPublication(kind, id, locale string, sourceRevision, publicationGeneration int) error {
	if publicationGeneration <= 0 {
		return ErrSourceChanged
	}
	return s.promoteTranslation(kind, id, locale, sourceRevision, false, publicationGeneration)
}

// PromoteCurrentTranslation is retained only for explicit legacy repository
// recovery: it may preserve a stored manual repair whose source revision still
// matches. Normal provisioning and AI tasks use PromoteAITranslation so a
// manual value cannot be claimed as an AI result.
func (s *Service) PromoteCurrentTranslation(kind, id, locale string, sourceRevision int) error {
	return s.promoteTranslation(kind, id, locale, sourceRevision, true, 0)
}

func (s *Service) promoteTranslation(kind, id, locale string, sourceRevision int, allowManual bool, expectedPublicationGeneration int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	head, err := s.getLifecycleContentLocked(kind, id)
	if err != nil {
		return err
	}
	state, ok := head.Meta.Locales[locale]
	validOrigin := state.Origin == domain.LocaleOriginAI || (allowManual && state.Origin == domain.LocaleOriginManual)
	if !ok || !validOrigin || state.State != "current" || state.SourceRevision != sourceRevision {
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
	if expectedPublicationGeneration > 0 && released.Meta.PublicationGeneration != expectedPublicationGeneration {
		return ErrSourceChanged
	}
	if released.Meta.Locales[released.Meta.SourceLocale].Revision != sourceRevision {
		// The AI result belongs to an unpublished source head. Keep it on the
		// head so an explicit publish can release the whole coherent snapshot.
		return nil
	}
	releasedState, hasReleasedState := released.Meta.Locales[locale]
	releasedContent, hasReleasedContent := released.Content[locale]
	headContent, hasHeadContent := head.Content[locale]
	if hasReleasedState && hasReleasedContent && hasHeadContent && releasedState == state && releasedContent == headContent {
		// A crash can commit current.yaml before persisting ReleaseRevision on the
		// head. Rewriting the decorated head repairs that window without creating
		// another release or advancing its revision on retry.
		paths, err := lifecyclePaths(kind, id)
		if err != nil {
			return err
		}
		head.Meta.ReleaseRevision = released.Meta.Revision
		return s.repository.WriteYAML(filepath.Join(paths.content, "meta.yaml"), head.Meta, false)
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

// ApplyAIReleaseTranslation adds a target locale to the immutable public
// release when its source snapshot is older than the unpublished head. It does
// not copy the old translation onto that newer head; callers translate the
// head separately so a later publish cannot release stale target text.
func (s *Service) ApplyAIReleaseTranslation(kind, id, locale string, input ApplyAIReleaseTranslationInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	descriptor := postContent
	if strings.EqualFold(kind, pageContent.kind) || strings.EqualFold(kind, pageContent.plural) {
		descriptor = pageContent
	} else if !strings.EqualFold(kind, postContent.kind) && !strings.EqualFold(kind, postContent.plural) {
		return ErrNotFound
	}
	locale, err := s.normalizeEnabledLocale(locale)
	if err != nil {
		return err
	}
	head, err := s.getContentLocked(descriptor, id)
	if err != nil {
		return err
	}
	if head.Meta.Status != domain.ContentStatusPublished {
		return ErrInvalidStatus
	}
	released, err := s.readRelease(descriptor.kind, id)
	if err != nil {
		return err
	}
	if released.Meta.Revision != input.ExpectedReleaseRevision {
		return ErrSourceChanged
	}
	if input.ExpectedPublicationGeneration > 0 && released.Meta.PublicationGeneration != input.ExpectedPublicationGeneration {
		return ErrSourceChanged
	}
	if locale == released.Meta.SourceLocale {
		return ErrLocaleDisabled
	}
	sourceState, exists := released.Meta.Locales[released.Meta.SourceLocale]
	if !exists || sourceState.Revision != input.ExpectedSourceRevision {
		return ErrSourceChanged
	}
	headSourceState, headSourceExists := head.Meta.Locales[head.Meta.SourceLocale]
	headSourceContent, headContentExists := head.Content[head.Meta.SourceLocale]
	releaseSourceContent, releaseContentExists := released.Content[released.Meta.SourceLocale]
	if !headSourceExists || !headContentExists || !releaseContentExists {
		return ErrSourceChanged
	}
	if head.Meta.SourceLocale != released.Meta.SourceLocale || (headSourceState.Revision == sourceState.Revision && headSourceContent == releaseSourceContent) {
		// The ordinary Apply + Promote path owns a release that still follows its
		// head source. This method must never synthesize a second competing release.
		return ErrSourceChanged
	}
	target := released.Meta.Locales[locale]
	if target.Revision != input.ExpectedTargetRevision {
		return ErrTargetChanged
	}
	localized := input.Content
	localized.Title = strings.TrimSpace(localized.Title)
	localized.Summary = strings.TrimSpace(localized.Summary)
	localized.SEOTitle = strings.TrimSpace(localized.SEOTitle)
	localized.SEODescription = strings.TrimSpace(localized.SEODescription)
	if localized.Title == "" || (strings.TrimSpace(releaseSourceContent.Markdown) != "" && strings.TrimSpace(localized.Markdown) == "") {
		return errors.New("complete translated release content is required")
	}
	if released.Meta.Revision+1 >= head.Meta.HeadRevision {
		if err := s.snapshotContent(descriptor, head); err != nil {
			return err
		}
		advanceHead(&head.Meta)
		head.Meta.UpdatedAt = time.Now().UTC()
	}
	target.Revision++
	target.State = "current"
	target.Origin = domain.LocaleOriginAI
	target.SourceRevision = sourceState.Revision
	released.Meta.Locales[locale] = target
	released.Content[locale] = localized
	advanceHead(&released.Meta)
	released.Meta.ReleaseRevision = released.Meta.Revision
	released.Meta.UpdatedAt = time.Now().UTC()
	if err := s.writeRelease(descriptor.kind, released); err != nil {
		return err
	}
	head.Meta.ReleaseRevision = released.Meta.Revision
	head.Meta.HasUnpublishedChanges = head.Meta.HeadRevision != head.Meta.ReleaseRevision
	paths, err := lifecyclePaths(descriptor.kind, id)
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
