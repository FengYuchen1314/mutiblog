package content

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

// SetScheduledPublish persists the user's publication intent with the content
// itself. Durable task history deliberately lives outside backups; this marker
// lets startup and backup restore recreate the pending task without treating a
// mere future display timestamp as an instruction to publish.
func (s *Service) SetScheduledPublish(kind, id string, expectedRevision int, dueAt time.Time) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, path, err := s.scheduledContentLocked(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	if item.Meta.Revision != expectedRevision {
		return domain.Post{}, ErrConflict
	}
	dueAt = dueAt.UTC()
	if item.Meta.PublishedAt == nil || !item.Meta.PublishedAt.UTC().Equal(dueAt) {
		return domain.Post{}, ErrConflict
	}
	item.Meta.ScheduledRevision = expectedRevision
	if err := s.repository.WriteYAML(path, item.Meta, false); err != nil {
		return domain.Post{}, err
	}
	return item, nil
}

// ClearScheduledPublish only clears the intent that belongs to the supplied
// revision. An older task can therefore never erase a newer reschedule.
func (s *Service) ClearScheduledPublish(kind, id string, scheduledRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, path, err := s.scheduledContentLocked(kind, id)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if item.Meta.ScheduledRevision == 0 || item.Meta.ScheduledRevision != scheduledRevision {
		return nil
	}
	item.Meta.ScheduledRevision = 0
	return s.repository.WriteYAML(path, item.Meta, false)
}

// CompleteScheduledPublish repairs the narrow crash window after the scheduled
// head revision was committed but before its immutable release pointer was
// committed. It never advances the head again: the durable scheduled task and
// the exact revision/time transition are required before the existing head is
// promoted into the public release.
func (s *Service) CompleteScheduledPublish(kind, id string, scheduledRevision int, dueAt time.Time) (domain.Post, error) {
	return s.completeScheduledPublish(kind, id, scheduledRevision, dueAt, 0)
}

// CompleteScheduledPublishForPublication repairs a scheduled publication only
// when the committed head still belongs to the caller's pre-reserved explicit
// publication generation. It prevents a completed older schedule from
// promoting a later same-source republish during recovery.
func (s *Service) CompleteScheduledPublishForPublication(kind, id string, scheduledRevision int, dueAt time.Time, expectedPublicationGeneration int) (domain.Post, error) {
	if expectedPublicationGeneration <= 0 {
		return domain.Post{}, ErrConflict
	}
	return s.completeScheduledPublish(kind, id, scheduledRevision, dueAt, expectedPublicationGeneration)
}

func (s *Service) completeScheduledPublish(kind, id string, scheduledRevision int, dueAt time.Time, expectedPublicationGeneration int) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, _, err := s.scheduledContentLocked(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	dueAt = dueAt.UTC()
	if item.Meta.Status != domain.ContentStatusPublished || item.Meta.Revision != scheduledRevision+1 || item.Meta.HeadRevision != item.Meta.Revision || item.Meta.ScheduledRevision != 0 || item.Meta.PublishedAt == nil || !item.Meta.PublishedAt.UTC().Equal(dueAt) || item.Meta.ReleaseRevision > item.Meta.Revision {
		return domain.Post{}, ErrConflict
	}
	if expectedPublicationGeneration > 0 && item.Meta.PublicationGeneration != expectedPublicationGeneration {
		return domain.Post{}, ErrConflict
	}
	released, releaseErr := s.readRelease(item.Meta.Kind, item.Meta.ID)
	if releaseErr != nil && !errors.Is(releaseErr, os.ErrNotExist) {
		return domain.Post{}, releaseErr
	}
	if releaseErr == nil && released.Meta.Revision > item.Meta.Revision {
		return domain.Post{}, ErrConflict
	}
	// Legacy interrupted publications predate the stable generation marker.
	// The recovered head itself is the durable identity, so bind that one
	// explicit publication to its committed revision before deriving a release.
	// Never replace a nonzero generation: it may already be owned by a later
	// recovery checkpoint for this same publication.
	if item.Meta.PublicationGeneration == 0 {
		item.Meta.PublicationGeneration = item.Meta.Revision
	}
	// A normal publish records a public snapshot whose derived locales are all
	// pending fresh AI output. A process can stop after the head metadata commit
	// but before that snapshot is written. Startup migration may then have
	// synthesized a same-revision release directly from the head, which still
	// marks old AI targets current. Treat that raw release as incomplete too:
	// otherwise a recovered translation task will SkipCurrentAI and leave the
	// scheduled publication with an old target visible.
	expectedRelease := publicationReleaseWithAutomaticTranslationPending(item)
	if errors.Is(releaseErr, os.ErrNotExist) || released.Meta.Revision != item.Meta.Revision || !sameContentSnapshot(released, expectedRelease) {
		if err := s.writeRelease(item.Meta.Kind, expectedRelease); err != nil {
			return domain.Post{}, err
		}
	}
	item.Meta.ReleaseRevision = item.Meta.Revision
	paths, err := lifecyclePaths(item.Meta.Kind, item.Meta.ID)
	if err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(filepath.Join(paths.content, "meta.yaml"), item.Meta, false); err != nil {
		return domain.Post{}, err
	}
	item.Meta.HasUnpublishedChanges = false
	return item, nil
}

// scheduledContentLocked expects the caller to hold s.mu for writing.
func (s *Service) scheduledContentLocked(kind, id string) (domain.Post, string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "post", "posts":
		item, err := s.getPostLocked(id)
		return item, filepath.Join("content", "posts", id, "meta.yaml"), err
	case "page", "pages":
		item, err := s.getPageLocked(id)
		return item, filepath.Join("content", "pages", id, "meta.yaml"), err
	default:
		return domain.Post{}, "", ErrNotFound
	}
}
