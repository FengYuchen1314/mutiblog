package content

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

var templatePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

type UpdatePostSettingsInput struct {
	ExpectedRevision int
	Categories       []string
	Tags             []string
	Cover            string
	Pinned           *bool
	Visibility       *domain.ContentVisibility
	PublishedAt      *time.Time
	PublishTimeSet   bool
	CommentPolicy    string
	Template         string
}

func (s *Service) UpdatePostSettings(id string, input UpdatePostSettingsInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	post, err := s.getPostLocked(id)
	if err != nil {
		return domain.Post{}, err
	}
	if post.Meta.Revision != input.ExpectedRevision {
		return domain.Post{}, ErrConflict
	}
	categories, err := s.validateTaxonomyIDs("categories", input.Categories)
	if err != nil {
		return domain.Post{}, err
	}
	tags, err := s.validateTaxonomyIDs("tags", input.Tags)
	if err != nil {
		return domain.Post{}, err
	}
	cover := strings.TrimSpace(input.Cover)
	if cover != "" && (!strings.HasPrefix(cover, "/media/") || strings.Contains(cover, "..")) {
		return domain.Post{}, errors.New("cover must be a local media URL")
	}
	policy := input.CommentPolicy
	if policy == "" {
		policy = "open"
	}
	if policy != "open" && policy != "closed" {
		return domain.Post{}, errors.New("invalid comment policy")
	}
	template := strings.TrimSpace(input.Template)
	if template == "" {
		template = "post"
	}
	if !templatePattern.MatchString(template) {
		return domain.Post{}, errors.New("invalid template")
	}
	if err := validateAdvancedSettings(input); err != nil {
		return domain.Post{}, err
	}
	if err := s.snapshot(post); err != nil {
		return domain.Post{}, err
	}
	post.Meta.Categories = categories
	post.Meta.Tags = tags
	post.Meta.Cover = cover
	applyAdvancedSettings(&post.Meta, input, true)
	post.Meta.CommentPolicy = policy
	post.Meta.Template = template
	advanceHead(&post.Meta)
	post.Meta.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(postPath(id, "meta.yaml"), post.Meta, false); err != nil {
		return domain.Post{}, err
	}
	post.Meta.HasUnpublishedChanges = post.Meta.Status == domain.ContentStatusPublished
	return post, nil
}

func (s *Service) UpdatePageSettings(id string, input UpdatePostSettingsInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	page, err := s.getPageLocked(id)
	if err != nil {
		return domain.Post{}, err
	}
	if page.Meta.Revision != input.ExpectedRevision {
		return domain.Post{}, ErrConflict
	}
	cover := strings.TrimSpace(input.Cover)
	if cover != "" && (!strings.HasPrefix(cover, "/media/") || strings.Contains(cover, "..")) {
		return domain.Post{}, errors.New("cover must be a local media URL")
	}
	policy := input.CommentPolicy
	if policy == "" {
		policy = "open"
	}
	if policy != "open" && policy != "closed" {
		return domain.Post{}, errors.New("invalid comment policy")
	}
	template := strings.TrimSpace(input.Template)
	if template == "" {
		template = "page"
	}
	if !templatePattern.MatchString(template) {
		return domain.Post{}, errors.New("invalid template")
	}
	if err := validateAdvancedSettings(input); err != nil {
		return domain.Post{}, err
	}
	if err := s.snapshotPage(page); err != nil {
		return domain.Post{}, err
	}
	page.Meta.Cover = cover
	applyAdvancedSettings(&page.Meta, input, false)
	page.Meta.CommentPolicy = policy
	page.Meta.Template = template
	advanceHead(&page.Meta)
	page.Meta.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(pagePath(id, "meta.yaml"), page.Meta, false); err != nil {
		return domain.Post{}, err
	}
	page.Meta.HasUnpublishedChanges = page.Meta.Status == domain.ContentStatusPublished
	return page, nil
}

func validateAdvancedSettings(input UpdatePostSettingsInput) error {
	if input.Visibility != nil && *input.Visibility != domain.ContentVisibilityPublic && *input.Visibility != domain.ContentVisibilityPrivate {
		return errors.New("invalid content visibility")
	}
	if input.PublishTimeSet && input.PublishedAt != nil && input.PublishedAt.IsZero() {
		return errors.New("invalid publish time")
	}
	return nil
}

func applyAdvancedSettings(meta *domain.PostMeta, input UpdatePostSettingsInput, allowPinned bool) {
	if allowPinned && input.Pinned != nil {
		meta.Pinned = *input.Pinned
	}
	if input.Visibility != nil {
		meta.Visibility = *input.Visibility
	}
	if input.PublishTimeSet {
		meta.PublishedAt = nil
		if input.PublishedAt != nil {
			publishedAt := input.PublishedAt.UTC()
			meta.PublishedAt = &publishedAt
		}
	}
}

func (s *Service) validateTaxonomyIDs(directory string, values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		id := strings.TrimSpace(value)
		if seen[id] {
			continue
		}
		if !validID(id) {
			return nil, errors.New("invalid taxonomy ID")
		}
		exists, err := s.repository.Exists(filepath.Join("content", "taxonomies", directory, id+".yaml"))
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errors.New("taxonomy does not exist")
		}
		seen[id] = true
		result = append(result, id)
	}
	return result, nil
}
