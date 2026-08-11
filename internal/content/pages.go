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

type CreatePageInput = CreatePostInput

func (s *Service) ListPages() ([]domain.Post, error) {
	entries, err := s.repository.ReadDir("content/pages")
	if err != nil {
		return nil, err
	}
	pages := make([]domain.Post, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		page, err := s.GetPage(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read page %s: %w", entry.Name(), err)
		}
		pages = append(pages, page)
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Meta.UpdatedAt.After(pages[j].Meta.UpdatedAt) })
	return pages, nil
}

func (s *Service) GetPage(id string) (domain.Post, error) {
	if !validID(id) {
		return domain.Post{}, ErrInvalidID
	}
	var meta domain.PostMeta
	if err := s.repository.ReadYAML(pagePath(id, "meta.yaml"), &meta); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Post{}, ErrNotFound
		}
		return domain.Post{}, err
	}
	if !validStoredContentMeta(meta, id, "Page") {
		return domain.Post{}, errors.New("page metadata identity is invalid")
	}
	contents := make(map[string]domain.LocalizedMarkdown, len(meta.Locales))
	for locale := range meta.Locales {
		data, err := s.repository.ReadFile(pagePath(id, locale+".md"))
		if err != nil {
			return domain.Post{}, err
		}
		localized, err := decodeMarkdown(data)
		if err != nil {
			return domain.Post{}, fmt.Errorf("decode page %s locale %s: %w", id, locale, err)
		}
		contents[locale] = localized
	}
	page := domain.Post{Meta: meta, Content: contents}
	s.decoratePublicationState("Page", &page)
	return page, nil
}

func (s *Service) CreatePage(input CreatePageInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if exists, err := s.repository.Exists(pagePath(id, "meta.yaml")); err != nil {
		return domain.Post{}, err
	} else if exists {
		return domain.Post{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	meta := domain.PostMeta{
		SchemaVersion: domain.SchemaVersion, Kind: "Page", ID: id, Status: domain.ContentStatusDraft,
		SourceLocale: locales.SourceLocale, CreatedAt: now, UpdatedAt: now, Categories: []string{}, Tags: []string{},
		CommentPolicy: "open", Template: "page", Revision: 1, BaseRevision: 1, HeadRevision: 1,
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
	if err := s.writePageLocale(id, locales.SourceLocale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(pagePath(id, "meta.yaml"), meta, false); err != nil {
		return domain.Post{}, err
	}
	return domain.Post{Meta: meta, Content: map[string]domain.LocalizedMarkdown{locales.SourceLocale: localized}}, nil
}

func (s *Service) UpdatePageLocale(id, locale string, input UpdateLocaleInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locale, err := s.normalizeEnabledLocale(locale)
	if err != nil {
		return domain.Post{}, err
	}
	page, err := s.GetPage(id)
	if err != nil {
		return domain.Post{}, err
	}
	if page.Meta.Revision != input.ExpectedRevision {
		return domain.Post{}, ErrConflict
	}
	if err := s.snapshotPage(page); err != nil {
		return domain.Post{}, err
	}
	localized := domain.LocalizedMarkdown{Title: strings.TrimSpace(input.Title), Summary: strings.TrimSpace(input.Summary), SEOTitle: strings.TrimSpace(input.SEOTitle), SEODescription: strings.TrimSpace(input.SEODescription), Markdown: input.Markdown}
	if localized.Title == "" {
		return domain.Post{}, errors.New("title is required")
	}
	state, exists := page.Meta.Locales[locale]
	if !exists {
		state = domain.LocaleContentState{SourceRevision: page.Meta.Locales[page.Meta.SourceLocale].Revision}
	}
	state.Revision++
	state.State = "current"
	if locale == page.Meta.SourceLocale {
		state.Origin = domain.LocaleOriginSource
		state.SourceRevision = state.Revision
		for derivedLocale, derived := range page.Meta.Locales {
			if derivedLocale != locale {
				derived.State = "stale"
				page.Meta.Locales[derivedLocale] = derived
			}
		}
	} else {
		state.Origin = domain.LocaleOriginManual
		state.SourceRevision = page.Meta.Locales[page.Meta.SourceLocale].Revision
	}
	page.Meta.Locales[locale] = state
	advanceHead(&page.Meta)
	page.Meta.UpdatedAt = time.Now().UTC()
	if err := s.writePageLocale(id, locale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(pagePath(id, "meta.yaml"), page.Meta, false); err != nil {
		return domain.Post{}, err
	}
	page.Content[locale] = localized
	page.Meta.HasUnpublishedChanges = page.Meta.Status == domain.ContentStatusPublished
	return page, nil
}

func (s *Service) ApplyAIPageTranslation(id, locale string, input ApplyAITranslationInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locale, err := s.normalizeEnabledLocale(locale)
	if err != nil {
		return domain.Post{}, err
	}
	page, err := s.GetPage(id)
	if err != nil {
		return domain.Post{}, err
	}
	if locale == page.Meta.SourceLocale {
		return domain.Post{}, ErrLocaleDisabled
	}
	sourceState := page.Meta.Locales[page.Meta.SourceLocale]
	if sourceState.Revision != input.ExpectedSourceRevision {
		return domain.Post{}, ErrSourceChanged
	}
	state, exists := page.Meta.Locales[locale]
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
	if err := s.snapshotPage(page); err != nil {
		return domain.Post{}, err
	}
	state.Revision++
	state.State = "current"
	state.Origin = domain.LocaleOriginAI
	state.SourceRevision = sourceState.Revision
	page.Meta.Locales[locale] = state
	advanceHead(&page.Meta)
	page.Meta.UpdatedAt = time.Now().UTC()
	if err := s.writePageLocale(id, locale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(pagePath(id, "meta.yaml"), page.Meta, false); err != nil {
		return domain.Post{}, err
	}
	page.Content[locale] = localized
	page.Meta.HasUnpublishedChanges = page.Meta.Status == domain.ContentStatusPublished
	return page, nil
}

func (s *Service) PublishPage(id string, expectedRevision int) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page, err := s.GetPage(id)
	if err != nil {
		return domain.Post{}, err
	}
	if page.Meta.Revision != expectedRevision {
		return domain.Post{}, ErrConflict
	}
	if page.Meta.Status == domain.ContentStatusRecycled {
		return domain.Post{}, ErrInvalidStatus
	}
	if err := s.snapshotPage(page); err != nil {
		return domain.Post{}, err
	}
	previousMeta := page.Meta
	now := time.Now().UTC()
	page.Meta.Status = domain.ContentStatusPublished
	page.Meta.UpdatedAt = now
	if page.Meta.PublishedAt == nil {
		page.Meta.PublishedAt = &now
	}
	advanceHead(&page.Meta)
	page.Meta.ReleaseRevision = page.Meta.Revision
	if err := s.repository.WriteYAML(pagePath(id, "meta.yaml"), page.Meta, false); err != nil {
		return domain.Post{}, err
	}
	if err := s.writeRelease("Page", page); err != nil {
		_ = s.repository.WriteYAML(pagePath(id, "meta.yaml"), previousMeta, false)
		return domain.Post{}, err
	}
	page.Meta.HasUnpublishedChanges = false
	return page, nil
}

func (s *Service) writePageLocale(id, locale string, value domain.LocalizedMarkdown) error {
	data, err := encodeMarkdown(value)
	if err != nil {
		return err
	}
	return s.repository.WriteFile(pagePath(id, locale+".md"), data, 0o640)
}

func (s *Service) snapshotPage(page domain.Post) error {
	if err := s.ensureRelease("Page", page); err != nil {
		return err
	}
	revisionID := fmt.Sprintf("%06d-%d", page.Meta.Revision, time.Now().UTC().UnixMilli())
	base := filepath.Join("revisions", "pages", page.Meta.ID, revisionID)
	if err := s.repository.WriteYAML(filepath.Join(base, "meta.yaml"), page.Meta, false); err != nil {
		return err
	}
	for locale, localized := range page.Content {
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

func pagePath(id string, parts ...string) string {
	items := append([]string{"content", "pages", id}, parts...)
	return filepath.Join(items...)
}
