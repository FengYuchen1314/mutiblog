package content

import "github.com/FengYuchen1314/mutiblog/internal/domain"

type CreatePageInput = CreatePostInput

func (s *Service) ListPages() ([]domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listPagesLocked()
}

// listPagesLocked expects the caller to hold s.mu for reading or writing.
func (s *Service) listPagesLocked() ([]domain.Post, error) {
	return s.listContentLocked(pageContent)
}

func (s *Service) GetPage(id string) (domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getPageLocked(id)
}

// getPageLocked expects the caller to hold s.mu for reading or writing.
func (s *Service) getPageLocked(id string) (domain.Post, error) {
	return s.getContentLocked(pageContent, id)
}

func (s *Service) CreatePage(input CreatePageInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createContentLocked(pageContent, input)
}

func (s *Service) UpdatePageLocale(id, locale string, input UpdateLocaleInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateContentLocaleLocked(pageContent, id, locale, input)
}

func (s *Service) ApplyAIPageTranslation(id, locale string, input ApplyAITranslationInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyAIContentTranslationLocked(pageContent, id, locale, input)
}

func (s *Service) PublishPage(id string, expectedRevision int) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publishContentLocked(pageContent, id, expectedRevision)
}

func (s *Service) writePageLocale(id, locale string, value domain.LocalizedMarkdown) error {
	return s.writeContentLocale(pageContent, id, locale, value)
}

func (s *Service) snapshotPage(page domain.Post) error {
	return s.snapshotContent(pageContent, page)
}

func pagePath(id string, parts ...string) string {
	return pageContent.path(id, parts...)
}
