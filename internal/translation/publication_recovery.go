package translation

import (
	"errors"
	"fmt"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

// ReconcilePublishedPublications restores the durable translation receipt for
// an explicit publication that reached its pending immutable release before
// the process stopped. It deliberately looks only at release-local pending
// targets (missing, stale, or invalid AI): draft-head edits and legacy current
// releases are not publication recovery work.
//
// A durable receipt in any terminal or active state is authoritative for its
// exact generation: automatic startup recovery must never re-charge or retry a
// failed/needs-review publication merely because its release remains stale.
// This method only persists missing receipts; the caller follows it with
// Recover, which launches queued work in one race-free recovery pass.
func (s *Service) ReconcilePublishedPublications() (int, error) {
	tasks, err := s.List()
	if err != nil {
		return 0, fmt.Errorf("list publication translation receipts: %w", err)
	}
	receipts := make(map[publicationReceiptKey]struct{}, len(tasks))
	for _, task := range tasks {
		if task.PublicationRevision > 0 && task.PublicationGeneration > 0 {
			receipts[publicationReceiptKey{kind: task.EntityKind, id: task.EntityID, generation: task.PublicationGeneration}] = struct{}{}
		}
	}

	reconciled := 0
	for _, candidate := range []struct {
		kind string
		list func() ([]domain.Post, error)
	}{
		{kind: "Post", list: s.content.ListPosts},
		{kind: "Page", list: s.content.ListPages},
	} {
		head, err := candidate.list()
		if err != nil {
			return reconciled, fmt.Errorf("list published %s content: %w", candidate.kind, err)
		}
		for _, item := range head {
			if item.Meta.Status != domain.ContentStatusPublished {
				continue
			}
			pending, err := s.reconcilePublishedPublication(candidate.kind, item.Meta.ID, receipts)
			if err != nil {
				return reconciled, err
			}
			if pending {
				reconciled++
			}
		}
	}
	return reconciled, nil
}

type publicationReceiptKey struct {
	kind       string
	id         string
	generation int
}

func (s *Service) reconcilePublishedPublication(kind, id string, receipts map[publicationReceiptKey]struct{}) (bool, error) {
	released, err := s.content.GetPublishedRelease(kind, id)
	if errors.Is(err, content.ErrNotFound) {
		// Private published content has no public immutable release to rebuild.
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read published %s %q: %w", kind, id, err)
	}
	pendingTargets, err := s.pendingPublicationTargets(released)
	if err != nil {
		return false, fmt.Errorf("inspect published %s %q translation targets: %w", kind, id, err)
	}
	if len(pendingTargets) == 0 {
		return false, nil
	}
	if released.Meta.PublicationGeneration <= 0 {
		// Do not let a legacy zero-value record acquire a newer same-source
		// publication. InitializePublishedReleases must establish this identity
		// before recovery reaches this point.
		return false, fmt.Errorf("published %s %q: %w", kind, id, ErrPublicationGenerationUnavailable)
	}
	key := publicationReceiptKey{kind: kind, id: id, generation: released.Meta.PublicationGeneration}
	if _, exists := receipts[key]; exists {
		return true, nil
	}
	task, _, err := s.Prepare(StartInput{
		EntityKind:             kind,
		PostID:                 id,
		Locales:                pendingTargets,
		OverwriteManual:        true,
		PublishedRelease:       true,
		PublicationGeneration:  released.Meta.PublicationGeneration,
		SkipCurrentAI:          true,
		RecordPreflightFailure: true,
	})
	if err != nil {
		// Automatic publication deliberately persists provider-configuration
		// failures. The release remains pending and the previous generated site
		// stays active; an explicit retry or newer publication can retry safely.
		if errors.Is(err, ErrNoTargets) || (task.ID != "" && recordableProviderPreflightError(err)) {
			if task.ID != "" {
				receipts[key] = struct{}{}
			}
			return true, nil
		}
		return false, fmt.Errorf("prepare published %s %q translation: %w", kind, id, err)
	}
	if task.PublicationGeneration != released.Meta.PublicationGeneration {
		return false, fmt.Errorf("prepare published %s %q translation: %w", kind, id, content.ErrSourceChanged)
	}
	receipts[key] = struct{}{}
	return true, nil
}

// pendingPublicationTargets uses the same enabled/ready target selection as
// Prepare, but returns only the release-local pending representation. A first
// publication often has no target locale entry at all, while an explicit
// republish marks existing targets stale. It also includes a malformed current
// AI target (wrong source or incomplete fields), but not a current manual
// sibling. Passing this exact list to Prepare preserves those distinctions.
func (s *Service) pendingPublicationTargets(released domain.Post) ([]string, error) {
	targets, _, err := s.targets(released, nil, true)
	if errors.Is(err, ErrNoTargets) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	source, sourceExists := released.Content[released.Meta.SourceLocale]
	sourceState, sourceStateExists := released.Meta.Locales[released.Meta.SourceLocale]
	pending := make([]string, 0, len(targets))
	for _, locale := range targets {
		state, exists := released.Meta.Locales[locale]
		target, targetExists := released.Content[locale]
		invalidCurrentAI := exists && state.Origin == domain.LocaleOriginAI && (state.State != "current" || !sourceExists || !sourceStateExists || state.SourceRevision != sourceState.Revision || !targetExists || !completeLocalizedMarkdown(source, target))
		if !exists || state.State == "stale" || invalidCurrentAI {
			pending = append(pending, locale)
		}
	}
	return pending, nil
}
