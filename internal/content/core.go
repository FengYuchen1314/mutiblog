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

type contentDescriptor struct {
	kind            string
	plural          string
	defaultTemplate string
}

var (
	postContent = contentDescriptor{kind: "Post", plural: "posts", defaultTemplate: "post"}
	pageContent = contentDescriptor{kind: "Page", plural: "pages", defaultTemplate: "page"}
)

func (descriptor contentDescriptor) singular() string {
	return strings.ToLower(descriptor.kind)
}

func (descriptor contentDescriptor) path(id string, parts ...string) string {
	items := make([]string, 0, len(parts)+3)
	items = append(items, "content", descriptor.plural, id)
	items = append(items, parts...)
	return filepath.Join(items...)
}

func (descriptor contentDescriptor) revisionRoot(id string) string {
	return filepath.Join("revisions", descriptor.plural, id)
}

// The shared helpers in this file never acquire s.mu. Callers must hold the
// read or write lock appropriate for the operation.
func (s *Service) listContentLocked(descriptor contentDescriptor) ([]domain.Post, error) {
	entries, err := s.repository.ReadDir("content/" + descriptor.plural)
	if err != nil {
		return nil, err
	}
	items := make([]domain.Post, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		item, err := s.getContentLocked(descriptor, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s %s: %w", descriptor.singular(), entry.Name(), err)
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Meta.UpdatedAt.After(items[j].Meta.UpdatedAt) })
	return items, nil
}

func (s *Service) getContentLocked(descriptor contentDescriptor, id string) (domain.Post, error) {
	if !validID(id) {
		return domain.Post{}, ErrInvalidID
	}
	var meta domain.PostMeta
	if err := s.repository.ReadYAML(descriptor.path(id, "meta.yaml"), &meta); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Post{}, ErrNotFound
		}
		return domain.Post{}, err
	}
	if !validStoredContentMeta(meta, id, descriptor.kind) {
		return domain.Post{}, errors.New(descriptor.singular() + " metadata identity is invalid")
	}
	contents := make(map[string]domain.LocalizedMarkdown, len(meta.Locales))
	for locale := range meta.Locales {
		data, err := s.repository.ReadFile(descriptor.path(id, locale+".md"))
		if err != nil {
			return domain.Post{}, err
		}
		localized, err := decodeMarkdown(data)
		if err != nil {
			return domain.Post{}, descriptor.decodeError(id, locale, err)
		}
		contents[locale] = localized
	}
	item := domain.Post{Meta: meta, Content: contents}
	s.decoratePublicationState(descriptor.kind, &item)
	return item, nil
}

func (descriptor contentDescriptor) decodeError(id, locale string, err error) error {
	// Keep the historic Post and Page error strings stable for callers.
	if descriptor.kind == pageContent.kind {
		return fmt.Errorf("decode page %s locale %s: %w", id, locale, err)
	}
	return fmt.Errorf("decode %s locale %s: %w", id, locale, err)
}

func (s *Service) createContentLocked(descriptor contentDescriptor, input CreatePostInput) (domain.Post, error) {
	locales, err := s.locales()
	if err != nil {
		return domain.Post{}, err
	}
	id := input.ID
	if id == "" {
		id, err = ConfiguredPublicID(s.repository)
		if err != nil {
			return domain.Post{}, err
		}
	} else if !customSlugPattern.MatchString(id) {
		return domain.Post{}, ErrInvalidID
	}
	if exists, err := s.repository.Exists(descriptor.path(id, "meta.yaml")); err != nil {
		return domain.Post{}, err
	} else if exists {
		return domain.Post{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	meta := domain.PostMeta{
		SchemaVersion: domain.SchemaVersion, Kind: descriptor.kind, ID: id, Status: domain.ContentStatusDraft,
		SourceLocale: locales.SourceLocale, CreatedAt: now, UpdatedAt: now, Categories: []string{}, Tags: []string{},
		Visibility: domain.ContentVisibilityPublic, CommentPolicy: "open", Template: descriptor.defaultTemplate, Revision: 1, BaseRevision: 1, HeadRevision: 1,
		Locales: map[string]domain.LocaleContentState{locales.SourceLocale: {State: "current", Origin: domain.LocaleOriginSource, Revision: 1, SourceRevision: 1}},
	}
	localized := domain.LocalizedMarkdown{
		Title: strings.TrimSpace(input.Title), Summary: strings.TrimSpace(input.Summary),
		SEOTitle: strings.TrimSpace(input.SEOTitle), SEODescription: strings.TrimSpace(input.SEODescription),
		Markdown: input.Markdown,
	}
	if localized.Title == "" {
		localized.Title = "Untitled"
	}
	if err := s.writeContentLocale(descriptor, id, locales.SourceLocale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(descriptor.path(id, "meta.yaml"), meta, false); err != nil {
		return domain.Post{}, err
	}
	return domain.Post{Meta: meta, Content: map[string]domain.LocalizedMarkdown{locales.SourceLocale: localized}}, nil
}

func (s *Service) updateContentLocaleLocked(descriptor contentDescriptor, id, locale string, input UpdateLocaleInput) (domain.Post, error) {
	locale, err := s.normalizeEnabledLocale(locale)
	if err != nil {
		return domain.Post{}, err
	}
	item, err := s.getContentLocked(descriptor, id)
	if err != nil {
		return domain.Post{}, err
	}
	if item.Meta.Revision != input.ExpectedRevision {
		return domain.Post{}, ErrConflict
	}
	if err := s.snapshotContent(descriptor, item); err != nil {
		return domain.Post{}, err
	}
	localized := domain.LocalizedMarkdown{Title: strings.TrimSpace(input.Title), Summary: strings.TrimSpace(input.Summary), SEOTitle: strings.TrimSpace(input.SEOTitle), SEODescription: strings.TrimSpace(input.SEODescription), Markdown: input.Markdown}
	if localized.Title == "" {
		return domain.Post{}, errors.New("title is required")
	}
	state, exists := item.Meta.Locales[locale]
	if !exists {
		state = domain.LocaleContentState{Revision: 0, SourceRevision: item.Meta.Locales[item.Meta.SourceLocale].Revision}
	}
	state.Revision++
	state.State = "current"
	if locale == item.Meta.SourceLocale {
		state.Origin = domain.LocaleOriginSource
		state.SourceRevision = state.Revision
		for derivedLocale, derived := range item.Meta.Locales {
			if derivedLocale != locale {
				derived.State = "stale"
				item.Meta.Locales[derivedLocale] = derived
			}
		}
	} else {
		state.Origin = domain.LocaleOriginManual
		state.SourceRevision = item.Meta.Locales[item.Meta.SourceLocale].Revision
	}
	item.Meta.Locales[locale] = state
	advanceHead(&item.Meta)
	item.Meta.UpdatedAt = time.Now().UTC()
	if err := s.writeContentLocale(descriptor, id, locale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(descriptor.path(id, "meta.yaml"), item.Meta, false); err != nil {
		return domain.Post{}, err
	}
	item.Content[locale] = localized
	item.Meta.HasUnpublishedChanges = item.Meta.Status == domain.ContentStatusPublished
	return item, nil
}

func (s *Service) applyAIContentTranslationLocked(descriptor contentDescriptor, id, locale string, input ApplyAITranslationInput) (domain.Post, error) {
	locale, err := s.normalizeEnabledLocale(locale)
	if err != nil {
		return domain.Post{}, err
	}
	item, err := s.getContentLocked(descriptor, id)
	if err != nil {
		return domain.Post{}, err
	}
	if locale == item.Meta.SourceLocale {
		return domain.Post{}, ErrLocaleDisabled
	}
	sourceState := item.Meta.Locales[item.Meta.SourceLocale]
	if sourceState.Revision != input.ExpectedSourceRevision {
		return domain.Post{}, ErrSourceChanged
	}
	state, exists := item.Meta.Locales[locale]
	if input.ExpectedTargetRevision != nil {
		currentRevision := 0
		if exists {
			currentRevision = state.Revision
		}
		if currentRevision != *input.ExpectedTargetRevision {
			return domain.Post{}, ErrTargetChanged
		}
	}
	if exists && state.Origin == domain.LocaleOriginManual && !input.OverwriteManual {
		return domain.Post{}, ErrManualProtected
	}
	localized := input.Content
	localized.Title = strings.TrimSpace(localized.Title)
	localized.Summary = strings.TrimSpace(localized.Summary)
	localized.SEOTitle = strings.TrimSpace(localized.SEOTitle)
	localized.SEODescription = strings.TrimSpace(localized.SEODescription)
	if localized.Title == "" {
		return domain.Post{}, errors.New("translated title is required")
	}
	if err := s.snapshotContent(descriptor, item); err != nil {
		return domain.Post{}, err
	}
	state.Revision++
	state.State = "current"
	state.Origin = domain.LocaleOriginAI
	state.SourceRevision = sourceState.Revision
	item.Meta.Locales[locale] = state
	advanceHead(&item.Meta)
	item.Meta.UpdatedAt = time.Now().UTC()
	if err := s.writeContentLocale(descriptor, id, locale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(descriptor.path(id, "meta.yaml"), item.Meta, false); err != nil {
		return domain.Post{}, err
	}
	item.Content[locale] = localized
	item.Meta.HasUnpublishedChanges = item.Meta.Status == domain.ContentStatusPublished
	return item, nil
}

func (s *Service) publishContentLocked(descriptor contentDescriptor, id string, expectedRevision int) (domain.Post, error) {
	item, err := s.getContentLocked(descriptor, id)
	if err != nil {
		return domain.Post{}, err
	}
	if item.Meta.Revision != expectedRevision {
		return domain.Post{}, ErrConflict
	}
	if item.Meta.Status == domain.ContentStatusRecycled {
		return domain.Post{}, ErrInvalidStatus
	}
	if err := s.snapshotContent(descriptor, item); err != nil {
		return domain.Post{}, err
	}
	previousMeta := item.Meta
	now := time.Now().UTC()
	item.Meta.Status = domain.ContentStatusPublished
	item.Meta.ScheduledRevision = 0
	item.Meta.UpdatedAt = now
	if item.Meta.PublishedAt == nil {
		item.Meta.PublishedAt = &now
	}
	advanceHead(&item.Meta)
	item.Meta.ReleaseRevision = item.Meta.Revision
	if err := commitPublication(
		previousMeta,
		item.Meta,
		func(meta domain.PostMeta) error {
			return s.repository.WriteYAML(descriptor.path(id, "meta.yaml"), meta, false)
		},
		func() error { return s.writeRelease(descriptor.kind, item) },
	); err != nil {
		return domain.Post{}, err
	}
	item.Meta.HasUnpublishedChanges = false
	return item, nil
}

func (s *Service) writeContentLocale(descriptor contentDescriptor, id, locale string, value domain.LocalizedMarkdown) error {
	data, err := encodeMarkdown(value)
	if err != nil {
		return err
	}
	return s.repository.WriteFile(descriptor.path(id, locale+".md"), data, 0o640)
}

func (s *Service) snapshotContent(descriptor contentDescriptor, item domain.Post) error {
	if err := s.ensureRelease(descriptor.kind, item); err != nil {
		return err
	}
	revisionRoot := descriptor.revisionRoot(item.Meta.ID)
	revisionID, err := s.nextSnapshotID(item.Meta.Revision, revisionRoot)
	if err != nil {
		return err
	}
	base := filepath.Join(revisionRoot, revisionID)
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
