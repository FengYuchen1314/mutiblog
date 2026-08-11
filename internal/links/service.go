package links

import (
	"errors"
	"net/url"
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
	ErrNotFound = errors.New("link resource not found")
	ErrConflict = errors.New("link resource revision conflict")
	ErrInvalid  = errors.New("invalid link resource")
	ErrInUse    = errors.New("link group is in use")
)

type CreateGroupInput struct {
	ID, Name, Description string
	Order                 int
}
type CreateLinkInput struct {
	ID, GroupID, URL, Logo, Name, Description string
	Order                                     int
}
type UpdateLocaleInput struct {
	ExpectedRevision  int
	Name, Description string
}
type UpdateGroupInput struct {
	ExpectedRevision int
	Order            int
}
type UpdateLinkInput struct {
	ExpectedRevision int
	GroupID, URL     string
	Logo             string
	Order            int
}

type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
}

func NewService(repository *fsrepo.Repository) *Service { return &Service{repository: repository} }

func (s *Service) ListGroups() ([]domain.LinkGroup, error) {
	entries, err := s.repository.ReadDir("content/links/groups")
	if errors.Is(err, os.ErrNotExist) {
		return []domain.LinkGroup{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]domain.LinkGroup, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			item, err := s.getGroup(strings.TrimSuffix(entry.Name(), ".yaml"))
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Order == items[j].Order {
			return items[i].ID < items[j].ID
		}
		return items[i].Order < items[j].Order
	})
	return items, nil
}

func (s *Service) ListLinks() ([]domain.Link, error) {
	entries, err := s.repository.ReadDir("content/links/items")
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Link{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]domain.Link, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".yaml" {
			item, err := s.getLink(strings.TrimSuffix(entry.Name(), ".yaml"))
			if err != nil {
				return nil, err
			}
			if _, err := s.getGroup(item.GroupID); err != nil {
				return nil, ErrInvalid
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Order == items[j].Order {
			return items[i].ID < items[j].ID
		}
		return items[i].Order < items[j].Order
	})
	return items, nil
}

func (s *Service) CreateGroup(input CreateGroupInput) (domain.LinkGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.newID(input.ID)
	if err != nil {
		return domain.LinkGroup{}, err
	}
	path := filepath.Join("content/links/groups", id+".yaml")
	if exists, err := s.repository.Exists(path); err != nil {
		return domain.LinkGroup{}, err
	} else if exists {
		return domain.LinkGroup{}, content.ErrAlreadyExists
	}
	locale, err := s.sourceLocale()
	if err != nil {
		return domain.LinkGroup{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.LinkGroup{}, ErrInvalid
	}
	now := time.Now().UTC()
	item := domain.LinkGroup{SchemaVersion: 1, Kind: "LinkGroup", ID: id, SourceLocale: locale, Order: input.Order, Revision: 1, CreatedAt: now, UpdatedAt: now, Locales: map[string]domain.LocalizedLink{locale: {Name: name, Description: strings.TrimSpace(input.Description), State: "current", Origin: domain.LocaleOriginSource, Revision: 1, SourceRevision: 1}}}
	if err := s.repository.WriteYAML(path, item, false); err != nil {
		return domain.LinkGroup{}, err
	}
	return item, nil
}

func (s *Service) CreateLink(input CreateLinkInput) (domain.Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.newID(input.ID)
	if err != nil {
		return domain.Link{}, err
	}
	path := filepath.Join("content/links/items", id+".yaml")
	if exists, err := s.repository.Exists(path); err != nil {
		return domain.Link{}, err
	} else if exists {
		return domain.Link{}, content.ErrAlreadyExists
	}
	if _, err := s.getGroup(input.GroupID); err != nil {
		return domain.Link{}, ErrInvalid
	}
	target, err := normalizeURL(input.URL)
	if err != nil {
		return domain.Link{}, ErrInvalid
	}
	logo := strings.TrimSpace(input.Logo)
	if logo != "" && (!strings.HasPrefix(logo, "/media/") || strings.Contains(logo, "..")) {
		return domain.Link{}, ErrInvalid
	}
	locale, err := s.sourceLocale()
	if err != nil {
		return domain.Link{}, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domain.Link{}, ErrInvalid
	}
	now := time.Now().UTC()
	item := domain.Link{SchemaVersion: 1, Kind: "Link", ID: id, GroupID: input.GroupID, URL: target, Logo: logo, Order: input.Order, SourceLocale: locale, Revision: 1, CreatedAt: now, UpdatedAt: now, Locales: map[string]domain.LocalizedLink{locale: {Name: name, Description: strings.TrimSpace(input.Description), State: "current", Origin: domain.LocaleOriginSource, Revision: 1, SourceRevision: 1}}}
	if err := s.repository.WriteYAML(path, item, false); err != nil {
		return domain.Link{}, err
	}
	return item, nil
}

func (s *Service) UpdateGroupLocale(id, rawLocale string, input UpdateLocaleInput) (domain.LinkGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.getGroup(id)
	if err != nil {
		return domain.LinkGroup{}, err
	}
	locale, err := s.enabledLocale(rawLocale)
	if err != nil {
		return domain.LinkGroup{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return domain.LinkGroup{}, ErrConflict
	}
	localized, err := updateLocalized(item.SourceLocale, locale, item.Locales, input)
	if err != nil {
		return domain.LinkGroup{}, err
	}
	item.Locales = localized
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/links/groups", id+".yaml"), item, false); err != nil {
		return domain.LinkGroup{}, err
	}
	return item, nil
}

func (s *Service) UpdateLinkLocale(id, rawLocale string, input UpdateLocaleInput) (domain.Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.getLink(id)
	if err != nil {
		return domain.Link{}, err
	}
	locale, err := s.enabledLocale(rawLocale)
	if err != nil {
		return domain.Link{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return domain.Link{}, ErrConflict
	}
	localized, err := updateLocalized(item.SourceLocale, locale, item.Locales, input)
	if err != nil {
		return domain.Link{}, err
	}
	item.Locales = localized
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/links/items", id+".yaml"), item, false); err != nil {
		return domain.Link{}, err
	}
	return item, nil
}

func (s *Service) DeleteGroup(id string, expectedRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	group, err := s.getGroup(id)
	if err != nil {
		return err
	}
	if group.Revision != expectedRevision {
		return ErrConflict
	}
	items, err := s.ListLinks()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.GroupID == group.ID {
			return ErrInUse
		}
	}
	return s.repository.RemoveFile(filepath.Join("content/links/groups", group.ID+".yaml"))
}

func (s *Service) DeleteLink(id string, expectedRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.getLink(id)
	if err != nil {
		return err
	}
	if item.Revision != expectedRevision {
		return ErrConflict
	}
	return s.repository.RemoveFile(filepath.Join("content/links/items", item.ID+".yaml"))
}

func (s *Service) UpdateGroup(id string, input UpdateGroupInput) (domain.LinkGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.getGroup(id)
	if err != nil {
		return domain.LinkGroup{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return domain.LinkGroup{}, ErrConflict
	}
	if item.Order == input.Order {
		return item, nil
	}
	item.Order = input.Order
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/links/groups", item.ID+".yaml"), item, false); err != nil {
		return domain.LinkGroup{}, err
	}
	return item, nil
}

func (s *Service) UpdateLink(id string, input UpdateLinkInput) (domain.Link, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, err := s.getLink(id)
	if err != nil {
		return domain.Link{}, err
	}
	if item.Revision != input.ExpectedRevision {
		return domain.Link{}, ErrConflict
	}
	if _, err := s.getGroup(input.GroupID); err != nil {
		return domain.Link{}, ErrInvalid
	}
	target, err := normalizeURL(input.URL)
	if err != nil {
		return domain.Link{}, ErrInvalid
	}
	logo := strings.TrimSpace(input.Logo)
	if logo != "" && (!strings.HasPrefix(logo, "/media/") || strings.Contains(logo, "..")) {
		return domain.Link{}, ErrInvalid
	}
	if item.GroupID == input.GroupID && item.URL == target && item.Logo == logo && item.Order == input.Order {
		return item, nil
	}
	item.GroupID = input.GroupID
	item.URL = target
	item.Logo = logo
	item.Order = input.Order
	item.Revision++
	item.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/links/items", item.ID+".yaml"), item, false); err != nil {
		return domain.Link{}, err
	}
	return item, nil
}

func updateLocalized(source, locale string, locales map[string]domain.LocalizedLink, input UpdateLocaleInput) (map[string]domain.LocalizedLink, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrInvalid
	}
	value := locales[locale]
	value.Name = name
	value.Description = strings.TrimSpace(input.Description)
	value.Revision++
	value.State = "current"
	if locale == source {
		value.Origin = domain.LocaleOriginSource
		value.SourceRevision = value.Revision
		for code, derived := range locales {
			if code != locale {
				derived.State = "stale"
				locales[code] = derived
			}
		}
	} else {
		value.Origin = domain.LocaleOriginManual
		value.SourceRevision = locales[source].Revision
	}
	locales[locale] = value
	return locales, nil
}

func (s *Service) getGroup(id string) (domain.LinkGroup, error) {
	if !content.ValidPublicID(id, false) {
		return domain.LinkGroup{}, ErrNotFound
	}
	var item domain.LinkGroup
	if err := s.repository.ReadYAML(filepath.Join("content/links/groups", id+".yaml"), &item); err != nil {
		return domain.LinkGroup{}, ErrNotFound
	}
	if item.SchemaVersion != domain.SchemaVersion || item.Kind != "LinkGroup" || item.ID != id {
		return domain.LinkGroup{}, ErrNotFound
	}
	if !validLocalizedLinks(item.SourceLocale, item.Revision, item.Locales) {
		return domain.LinkGroup{}, ErrInvalid
	}
	return item, nil
}
func (s *Service) getLink(id string) (domain.Link, error) {
	if !content.ValidPublicID(id, false) {
		return domain.Link{}, ErrNotFound
	}
	var item domain.Link
	if err := s.repository.ReadYAML(filepath.Join("content/links/items", id+".yaml"), &item); err != nil {
		return domain.Link{}, ErrNotFound
	}
	if item.SchemaVersion != domain.SchemaVersion || item.Kind != "Link" || item.ID != id {
		return domain.Link{}, ErrNotFound
	}
	if !content.ValidPublicID(item.GroupID, false) || !validLocalizedLinks(item.SourceLocale, item.Revision, item.Locales) {
		return domain.Link{}, ErrInvalid
	}
	if _, err := normalizeURL(item.URL); err != nil {
		return domain.Link{}, ErrInvalid
	}
	if item.Logo != "" && (!strings.HasPrefix(item.Logo, "/media/") || strings.Contains(item.Logo, "..")) {
		return domain.Link{}, ErrInvalid
	}
	return item, nil
}

func validLocalizedLinks(sourceLocale string, revision int, values map[string]domain.LocalizedLink) bool {
	sourceTag, err := language.Parse(sourceLocale)
	if err != nil || sourceTag.String() != sourceLocale || revision < 1 {
		return false
	}
	if source, ok := values[sourceLocale]; !ok || strings.TrimSpace(source.Name) == "" {
		return false
	}
	for locale, value := range values {
		tag, err := language.Parse(locale)
		if err != nil || tag.String() != locale || strings.TrimSpace(value.Name) == "" {
			return false
		}
	}
	return true
}
func (s *Service) newID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return content.ConfiguredPublicID(s.repository)
	}
	if !content.ValidPublicID(raw, true) {
		return "", content.ErrInvalidID
	}
	return raw, nil
}
func (s *Service) sourceLocale() (string, error) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return "", err
	}
	return config.SourceLocale, nil
}
func (s *Service) enabledLocale(raw string) (string, error) {
	tag, err := language.Parse(raw)
	if err != nil {
		return "", content.ErrLocaleDisabled
	}
	normalized := tag.String()
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return "", err
	}
	for _, locale := range config.Enabled {
		if locale.Enabled && locale.Code == normalized {
			return normalized, nil
		}
	}
	return "", content.ErrLocaleDisabled
}
func normalizeURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalid
	}
	return parsed.String(), nil
}
