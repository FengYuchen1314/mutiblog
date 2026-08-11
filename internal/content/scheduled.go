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
	item, path, err := s.scheduledContent(kind, id)
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
	item, path, err := s.scheduledContent(kind, id)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	item, _, err := s.scheduledContent(kind, id)
	if err != nil {
		return domain.Post{}, err
	}
	dueAt = dueAt.UTC()
	if item.Meta.Status != domain.ContentStatusPublished || item.Meta.Revision != scheduledRevision+1 || item.Meta.HeadRevision != item.Meta.Revision || item.Meta.ScheduledRevision != 0 || item.Meta.PublishedAt == nil || !item.Meta.PublishedAt.UTC().Equal(dueAt) || item.Meta.ReleaseRevision > item.Meta.Revision {
		return domain.Post{}, ErrConflict
	}
	released, releaseErr := s.readRelease(item.Meta.Kind, item.Meta.ID)
	if releaseErr != nil && !errors.Is(releaseErr, os.ErrNotExist) {
		return domain.Post{}, releaseErr
	}
	if releaseErr == nil && released.Meta.Revision > item.Meta.Revision {
		return domain.Post{}, ErrConflict
	}
	if errors.Is(releaseErr, os.ErrNotExist) || released.Meta.Revision != item.Meta.Revision {
		if err := s.writeRelease(item.Meta.Kind, item); err != nil {
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

func (s *Service) scheduledContent(kind, id string) (domain.Post, string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "post", "posts":
		item, err := s.GetPost(id)
		return item, filepath.Join("content", "posts", id, "meta.yaml"), err
	case "page", "pages":
		item, err := s.GetPage(id)
		return item, filepath.Join("content", "pages", id, "meta.yaml"), err
	default:
		return domain.Post{}, "", ErrNotFound
	}
}
