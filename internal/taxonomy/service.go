package taxonomy

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"golang.org/x/text/language"
)

var (
	ErrNotFound        = errors.New("taxonomy not found")
	ErrConflict        = errors.New("taxonomy revision conflict")
	ErrInvalidKind     = errors.New("invalid taxonomy kind")
	ErrInvalidParent   = errors.New("invalid category parent")
	ErrInvalidSettings = errors.New("invalid taxonomy settings")
	ErrInUse           = errors.New("taxonomy is in use")
)

type CreateInput struct {
	ID          string
	Name        string
	Description string
	ParentID    string
	Cover       string
	Template    string
}

type UpdateLocaleInput struct {
	ExpectedRevision int
	Name             string
	Description      string
	SEOTitle         string
	SEODescription   string
}

type UpdateStructureInput struct {
	ExpectedRevision int
	ParentID         string
	Cover            string
	Template         string
}

type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
}

func NewService(repository *fsrepo.Repository) *Service { return &Service{repository: repository} }

func (s *Service) List(kind string) ([]domain.Taxonomy, error) {
	directory, normalizedKind, err := kindDirectory(kind)
	if err != nil {
		return nil, err
	}
	entries, err := s.repository.ReadDir(filepath.Join("content", "taxonomies", directory))
	if err != nil {
		return nil, err
	}
	items := make([]domain.Taxonomy, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		item, err := s.Get(normalizedKind, strings.TrimSuffix(entry.Name(), ".yaml"))
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func (s *Service) Get(kind, id string) (domain.Taxonomy, error) {
	_, normalizedKind, err := kindDirectory(kind)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	path, err := taxonomyPath(kind, id)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	var item domain.Taxonomy
	if err := s.repository.ReadYAML(path, &item); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Taxonomy{}, ErrNotFound
		}
		return domain.Taxonomy{}, err
	}
	if item.SchemaVersion != domain.SchemaVersion || item.Kind != normalizedKind || item.ID != id {
		return domain.Taxonomy{}, ErrNotFound
	}
	return item, nil
}

func (s *Service) Create(kind string, input CreateInput) (domain.Taxonomy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, normalizedKind, err := kindDirectory(kind)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id, err = content.ConfiguredPublicID(s.repository)
		if err != nil {
			return domain.Taxonomy{}, err
		}
	} else if !content.ValidPublicID(id, true) {
		return domain.Taxonomy{}, content.ErrInvalidID
	}
	path, _ := taxonomyPath(normalizedKind, id)
	if exists, err := s.repository.Exists(path); err != nil {
		return domain.Taxonomy{}, err
	} else if exists {
		return domain.Taxonomy{}, content.ErrAlreadyExists
	}
	if normalizedKind == "Category" && input.ParentID != "" {
		if !content.ValidPublicID(input.ParentID, false) {
			return domain.Taxonomy{}, ErrInvalidParent
		}
		if _, err := s.Get("Category", input.ParentID); err != nil {
			return domain.Taxonomy{}, ErrInvalidParent
		}
	} else if normalizedKind == "Tag" && input.ParentID != "" {
		return domain.Taxonomy{}, ErrInvalidParent
	}
	locales, err := s.locales()
	if err != nil {
		return domain.Taxonomy{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Taxonomy{}, errors.New("name is required")
	}
	now := time.Now().UTC()
	cover, template, err := normalizeCategorySettings(normalizedKind, input.Cover, input.Template)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	item := domain.Taxonomy{
		SchemaVersion: domain.SchemaVersion, Kind: normalizedKind, ID: id, SourceLocale: locales.SourceLocale,
		ParentID: input.ParentID, Cover: cover, Template: template, Revision: 1, CreatedAt: now, UpdatedAt: now,
		Locales: map[string]domain.LocalizedTaxonomy{locales.SourceLocale: {Name: name, Description: strings.TrimSpace(input.Description), State: "current", Origin: domain.LocaleOriginSource, Revision: 1, SourceRevision: 1}},
	}
	if err := s.repository.WriteYAML(path, item, false); err != nil {
		return domain.Taxonomy{}, err
	}
	return item, nil
}

func (s *Service) UpdateLocale(kind, id, rawLocale string, input UpdateLocaleInput) (domain.Taxonomy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locale, err := s.normalizeEnabledLocale(rawLocale)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	item, err := s.Get(kind, id)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return domain.Taxonomy{}, ErrConflict
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Taxonomy{}, errors.New("name is required")
	}
	localized := item.Locales[locale]
	localized.Name = name
	localized.Description = strings.TrimSpace(input.Description)
	localized.SEOTitle = strings.TrimSpace(input.SEOTitle)
	localized.SEODescription = strings.TrimSpace(input.SEODescription)
	localized.Revision++
	localized.State = "current"
	if locale == item.SourceLocale {
		localized.Origin = domain.LocaleOriginSource
		localized.SourceRevision = localized.Revision
		for code, derived := range item.Locales {
			if code != locale {
				derived.State = "stale"
				item.Locales[code] = derived
			}
		}
	} else {
		localized.Origin = domain.LocaleOriginManual
		localized.SourceRevision = item.Locales[item.SourceLocale].Revision
	}
	item.Locales[locale] = localized
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	path, _ := taxonomyPath(item.Kind, item.ID)
	if err := s.repository.WriteYAML(path, item, false); err != nil {
		return domain.Taxonomy{}, err
	}
	return item, nil
}

func (s *Service) Delete(kind, id string, expectedRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.Get(kind, id)
	if err != nil {
		return err
	}
	if item.Revision != expectedRevision {
		return ErrConflict
	}
	if item.Kind == "Category" {
		categories, err := s.List("Category")
		if err != nil {
			return err
		}
		for _, category := range categories {
			if category.ParentID == item.ID {
				return ErrInUse
			}
		}
	}
	posts, err := content.NewService(s.repository).ListPosts()
	if err != nil {
		return err
	}
	for _, post := range posts {
		references := post.Meta.Tags
		if item.Kind == "Category" {
			references = post.Meta.Categories
		}
		for _, reference := range references {
			if reference == item.ID {
				return ErrInUse
			}
		}
	}
	path, err := taxonomyPath(item.Kind, item.ID)
	if err != nil {
		return err
	}
	return s.repository.RemoveFile(path)
}

func (s *Service) UpdateStructure(kind, id string, input UpdateStructureInput) (domain.Taxonomy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.Get(kind, id)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return domain.Taxonomy{}, ErrConflict
	}
	parentID := strings.TrimSpace(input.ParentID)
	if item.Kind == "Tag" && parentID != "" {
		return domain.Taxonomy{}, ErrInvalidParent
	}
	if item.Kind == "Category" && parentID != "" {
		if parentID == item.ID || !content.ValidPublicID(parentID, false) {
			return domain.Taxonomy{}, ErrInvalidParent
		}
		parent, err := s.Get("Category", parentID)
		if err != nil {
			return domain.Taxonomy{}, ErrInvalidParent
		}
		seen := map[string]bool{item.ID: true}
		for parent.ParentID != "" {
			if seen[parent.ID] {
				return domain.Taxonomy{}, ErrInvalidParent
			}
			seen[parent.ID] = true
			parent, err = s.Get("Category", parent.ParentID)
			if err != nil {
				return domain.Taxonomy{}, ErrInvalidParent
			}
		}
		if seen[parent.ID] {
			return domain.Taxonomy{}, ErrInvalidParent
		}
	}
	cover, template, err := normalizeCategorySettings(item.Kind, input.Cover, input.Template)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	if item.ParentID == parentID && item.Cover == cover && item.Template == template {
		return item, nil
	}
	item.ParentID = parentID
	item.Cover = cover
	item.Template = template
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	path, _ := taxonomyPath(item.Kind, item.ID)
	if err := s.repository.WriteYAML(path, item, false); err != nil {
		return domain.Taxonomy{}, err
	}
	return item, nil
}

func normalizeCategorySettings(kind, rawCover, rawTemplate string) (string, string, error) {
	if kind != "Category" {
		return "", "tag", nil
	}
	cover := strings.TrimSpace(rawCover)
	if cover != "" && (!strings.HasPrefix(cover, "/media/") || strings.Contains(cover, "..")) {
		return "", "", ErrInvalidSettings
	}
	template := strings.TrimSpace(rawTemplate)
	if template == "" {
		template = "category"
	}
	if !content.ValidPublicID(template, true) {
		return "", "", ErrInvalidSettings
	}
	return cover, template, nil
}

func (s *Service) locales() (domain.LocalesConfig, error) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return domain.LocalesConfig{}, err
	}
	return config, nil
}

func (s *Service) normalizeEnabledLocale(raw string) (string, error) {
	tag, err := language.Parse(raw)
	if err != nil {
		return "", content.ErrLocaleDisabled
	}
	normalized := tag.String()
	config, err := s.locales()
	if err != nil {
		return "", err
	}
	for _, locale := range config.Enabled {
		if locale.Enabled && locale.Code == normalized {
			return normalized, nil
		}
	}
	return "", content.ErrLocaleDisabled
}

func kindDirectory(kind string) (string, string, error) {
	switch strings.ToLower(kind) {
	case "category", "categories":
		return "categories", "Category", nil
	case "tag", "tags":
		return "tags", "Tag", nil
	default:
		return "", "", ErrInvalidKind
	}
}

func taxonomyPath(kind, id string) (string, error) {
	directory, _, err := kindDirectory(kind)
	if err != nil {
		return "", err
	}
	if !content.ValidPublicID(id, false) {
		return "", content.ErrInvalidID
	}
	return filepath.Join("content", "taxonomies", directory, id+".yaml"), nil
}
