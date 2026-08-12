package menus

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
	ErrNotFound = errors.New("menu not found")
	ErrConflict = errors.New("menu revision conflict")
	ErrInvalid  = errors.New("invalid menu")
	ErrInUse    = errors.New("menu item has children")
)

type CreateInput struct{ ID, Label string }
type AddItemInput struct {
	ID, ParentID, TargetKind, URL, Label string
	OpenInNew                            bool
	Order                                int
	ExpectedRevision                     int
}
type UpdateLocaleInput struct {
	ExpectedRevision int
	Label            string
}
type ApplyAILocaleInput struct {
	ExpectedSourceRevision int
	ExpectedTargetRevision int
	Label                  string
}
type UpdateItemInput struct {
	ExpectedRevision          int
	ParentID, TargetKind, URL string
	OpenInNew                 bool
	Order                     int
}
type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
}

func NewService(repository *fsrepo.Repository) *Service { return &Service{repository: repository} }

func (s *Service) List() ([]domain.Menu, error) {
	entries, err := s.repository.ReadDir("content/menus")
	if err != nil {
		return nil, err
	}
	result := []domain.Menu{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		menu, err := s.Get(strings.TrimSuffix(entry.Name(), ".yaml"))
		if err != nil {
			return nil, err
		}
		sort.Slice(menu.Items, func(i, j int) bool { return menu.Items[i].Order < menu.Items[j].Order })
		result = append(result, menu)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}
func (s *Service) Get(id string) (domain.Menu, error) {
	if !content.ValidPublicID(id, false) {
		return domain.Menu{}, ErrNotFound
	}
	var menu domain.Menu
	if err := s.repository.ReadYAML(filepath.Join("content/menus", id+".yaml"), &menu); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Menu{}, ErrNotFound
		}
		return domain.Menu{}, err
	}
	if menu.SchemaVersion != domain.SchemaVersion || menu.Kind != "Menu" || menu.ID != id {
		return domain.Menu{}, ErrNotFound
	}
	if !validStoredMenu(menu) {
		return domain.Menu{}, ErrInvalid
	}
	return menu, nil
}

func validStoredMenu(menu domain.Menu) bool {
	sourceTag, err := language.Parse(menu.SourceLocale)
	if err != nil || sourceTag.String() != menu.SourceLocale || menu.Revision < 1 {
		return false
	}
	if source, ok := menu.Locales[menu.SourceLocale]; !ok || strings.TrimSpace(source.Label) == "" {
		return false
	}
	for locale, value := range menu.Locales {
		tag, err := language.Parse(locale)
		if err != nil || tag.String() != locale || strings.TrimSpace(value.Label) == "" {
			return false
		}
	}
	items := make(map[string]domain.MenuItem, len(menu.Items))
	for _, item := range menu.Items {
		if !content.ValidPublicID(item.ID, false) {
			return false
		}
		if _, exists := items[item.ID]; exists {
			return false
		}
		target, kind, err := normalizeTarget(item.TargetKind, item.URL)
		if err != nil || target != item.URL || kind != item.TargetKind {
			return false
		}
		if source, ok := item.Locales[menu.SourceLocale]; !ok || strings.TrimSpace(source.Label) == "" {
			return false
		}
		for locale, value := range item.Locales {
			tag, err := language.Parse(locale)
			if err != nil || tag.String() != locale || strings.TrimSpace(value.Label) == "" {
				return false
			}
		}
		items[item.ID] = item
	}
	for _, item := range menu.Items {
		seen := map[string]bool{item.ID: true}
		parentID := item.ParentID
		for parentID != "" {
			parent, ok := items[parentID]
			if !ok || seen[parentID] {
				return false
			}
			seen[parentID] = true
			parentID = parent.ParentID
		}
	}
	return true
}
func (s *Service) Create(input CreateInput) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strings.TrimSpace(input.ID)
	var err error
	if id == "" {
		id, err = content.ConfiguredPublicID(s.repository)
	} else if !content.ValidPublicID(id, true) {
		return domain.Menu{}, content.ErrInvalidID
	}
	if err != nil {
		return domain.Menu{}, err
	}
	path := filepath.Join("content/menus", id+".yaml")
	if exists, err := s.repository.Exists(path); err != nil {
		return domain.Menu{}, err
	} else if exists {
		return domain.Menu{}, content.ErrAlreadyExists
	}
	locale, err := s.sourceLocale()
	if err != nil {
		return domain.Menu{}, err
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return domain.Menu{}, ErrInvalid
	}
	now := time.Now().UTC()
	menu := domain.Menu{SchemaVersion: 1, Kind: "Menu", ID: id, SourceLocale: locale, Revision: 1, CreatedAt: now, UpdatedAt: now, Locales: map[string]domain.LocalizedMenu{locale: {Label: label, State: "current", Origin: domain.LocaleOriginSource, Revision: 1, SourceRevision: 1}}, Items: []domain.MenuItem{}}
	if err := s.repository.WriteYAML(path, menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}
func (s *Service) AddItem(menuID string, input AddItemInput) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(menuID)
	if err != nil {
		return domain.Menu{}, err
	}
	if menu.Revision != input.ExpectedRevision {
		return domain.Menu{}, ErrConflict
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id, err = content.ConfiguredPublicID(s.repository)
	} else if !content.ValidPublicID(id, true) {
		return domain.Menu{}, content.ErrInvalidID
	}
	if err != nil {
		return domain.Menu{}, err
	}
	for _, item := range menu.Items {
		if item.ID == id {
			return domain.Menu{}, content.ErrAlreadyExists
		}
	}
	if input.ParentID != "" {
		found := false
		for _, item := range menu.Items {
			if item.ID == input.ParentID {
				found = true
				break
			}
		}
		if !found {
			return domain.Menu{}, ErrInvalid
		}
	}
	target, kind, err := normalizeTarget(input.TargetKind, input.URL)
	if err != nil {
		return domain.Menu{}, err
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return domain.Menu{}, ErrInvalid
	}
	menu.Items = append(menu.Items, domain.MenuItem{ID: id, ParentID: input.ParentID, TargetKind: kind, URL: target, OpenInNew: input.OpenInNew, Order: input.Order, Locales: map[string]domain.LocalizedMenu{menu.SourceLocale: {Label: label, State: "current", Origin: domain.LocaleOriginSource, Revision: 1, SourceRevision: 1}}})
	menu.Revision++
	menu.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/menus", menu.ID+".yaml"), menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}
func (s *Service) UpdateMenuLocale(id, rawLocale string, input UpdateLocaleInput) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(id)
	if err != nil {
		return domain.Menu{}, err
	}
	locale, err := s.enabledLocale(rawLocale)
	if err != nil {
		return domain.Menu{}, err
	}
	if menu.Revision != input.ExpectedRevision {
		return domain.Menu{}, ErrConflict
	}
	value, err := updatedValue(menu.SourceLocale, locale, menu.Locales, input.Label)
	if err != nil {
		return domain.Menu{}, err
	}
	menu.Locales[locale] = value
	if locale == menu.SourceLocale {
		for code, derived := range menu.Locales {
			if code != locale {
				derived.State = "stale"
				menu.Locales[code] = derived
			}
		}
	}
	menu.Revision++
	menu.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/menus", id+".yaml"), menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}
func (s *Service) UpdateItemLocale(menuID, itemID, rawLocale string, input UpdateLocaleInput) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(menuID)
	if err != nil {
		return domain.Menu{}, err
	}
	locale, err := s.enabledLocale(rawLocale)
	if err != nil {
		return domain.Menu{}, err
	}
	if menu.Revision != input.ExpectedRevision {
		return domain.Menu{}, ErrConflict
	}
	found := false
	for index := range menu.Items {
		if menu.Items[index].ID != itemID {
			continue
		}
		value, err := updatedValue(menu.SourceLocale, locale, menu.Items[index].Locales, input.Label)
		if err != nil {
			return domain.Menu{}, err
		}
		menu.Items[index].Locales[locale] = value
		if locale == menu.SourceLocale {
			for code, derived := range menu.Items[index].Locales {
				if code != locale {
					derived.State = "stale"
					menu.Items[index].Locales[code] = derived
				}
			}
		}
		found = true
		break
	}
	if !found {
		return domain.Menu{}, ErrNotFound
	}
	menu.Revision++
	menu.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/menus", menuID+".yaml"), menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}

func (s *Service) ApplyAIMenuLocale(id, rawLocale string, input ApplyAILocaleInput) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(id)
	if err != nil {
		return domain.Menu{}, err
	}
	locale, err := s.enabledLocale(rawLocale)
	if err != nil {
		return domain.Menu{}, err
	}
	value, err := aiLocalizedValue(menu.SourceLocale, locale, menu.Locales, input)
	if err != nil {
		return domain.Menu{}, err
	}
	menu.Locales[locale] = value
	menu.Revision++
	menu.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/menus", id+".yaml"), menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}

func (s *Service) ApplyAIItemLocale(menuID, itemID, rawLocale string, input ApplyAILocaleInput) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(menuID)
	if err != nil {
		return domain.Menu{}, err
	}
	locale, err := s.enabledLocale(rawLocale)
	if err != nil {
		return domain.Menu{}, err
	}
	index := -1
	for itemIndex := range menu.Items {
		if menu.Items[itemIndex].ID == itemID {
			index = itemIndex
			break
		}
	}
	if index < 0 {
		return domain.Menu{}, ErrNotFound
	}
	value, err := aiLocalizedValue(menu.SourceLocale, locale, menu.Items[index].Locales, input)
	if err != nil {
		return domain.Menu{}, err
	}
	menu.Items[index].Locales[locale] = value
	menu.Revision++
	menu.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/menus", menuID+".yaml"), menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}

func (s *Service) Delete(id string, expectedRevision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(id)
	if err != nil {
		return err
	}
	if menu.Revision != expectedRevision {
		return ErrConflict
	}
	return s.repository.RemoveFile(filepath.Join("content/menus", menu.ID+".yaml"))
}

func (s *Service) DeleteItem(menuID, itemID string, expectedRevision int) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(menuID)
	if err != nil {
		return domain.Menu{}, err
	}
	if menu.Revision != expectedRevision {
		return domain.Menu{}, ErrConflict
	}
	index := -1
	for itemIndex, item := range menu.Items {
		if item.ParentID == itemID {
			return domain.Menu{}, ErrInUse
		}
		if item.ID == itemID {
			index = itemIndex
		}
	}
	if index < 0 {
		return domain.Menu{}, ErrNotFound
	}
	menu.Items = append(menu.Items[:index], menu.Items[index+1:]...)
	menu.Revision++
	menu.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/menus", menu.ID+".yaml"), menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}

func (s *Service) UpdateItem(menuID, itemID string, input UpdateItemInput) (domain.Menu, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	menu, err := s.Get(menuID)
	if err != nil {
		return domain.Menu{}, err
	}
	if menu.Revision != input.ExpectedRevision {
		return domain.Menu{}, ErrConflict
	}
	index := -1
	byID := make(map[string]domain.MenuItem, len(menu.Items))
	for itemIndex, item := range menu.Items {
		byID[item.ID] = item
		if item.ID == itemID {
			index = itemIndex
		}
	}
	if index < 0 {
		return domain.Menu{}, ErrNotFound
	}
	parentID := strings.TrimSpace(input.ParentID)
	if parentID == itemID {
		return domain.Menu{}, ErrInvalid
	}
	if parentID != "" {
		parent, exists := byID[parentID]
		if !exists {
			return domain.Menu{}, ErrInvalid
		}
		seen := map[string]bool{itemID: true}
		for {
			if seen[parent.ID] {
				return domain.Menu{}, ErrInvalid
			}
			seen[parent.ID] = true
			if parent.ParentID == "" {
				break
			}
			var exists bool
			parent, exists = byID[parent.ParentID]
			if !exists {
				return domain.Menu{}, ErrInvalid
			}
		}
	}
	target, kind, err := normalizeTarget(input.TargetKind, input.URL)
	if err != nil {
		return domain.Menu{}, err
	}
	item := &menu.Items[index]
	if item.ParentID == parentID && item.TargetKind == kind && item.URL == target && item.OpenInNew == input.OpenInNew && item.Order == input.Order {
		return menu, nil
	}
	item.ParentID = parentID
	item.TargetKind = kind
	item.URL = target
	item.OpenInNew = input.OpenInNew
	item.Order = input.Order
	menu.Revision++
	menu.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML(filepath.Join("content/menus", menu.ID+".yaml"), menu, false); err != nil {
		return domain.Menu{}, err
	}
	return menu, nil
}
func updatedValue(source, locale string, values map[string]domain.LocalizedMenu, label string) (domain.LocalizedMenu, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return domain.LocalizedMenu{}, ErrInvalid
	}
	value := values[locale]
	value.Label = label
	value.Revision++
	value.State = "current"
	if locale == source {
		value.Origin = domain.LocaleOriginSource
		value.SourceRevision = value.Revision
	} else {
		value.Origin = domain.LocaleOriginManual
		value.SourceRevision = values[source].Revision
	}
	return value, nil
}
func aiLocalizedValue(source, locale string, values map[string]domain.LocalizedMenu, input ApplyAILocaleInput) (domain.LocalizedMenu, error) {
	if locale == source {
		return domain.LocalizedMenu{}, content.ErrLocaleDisabled
	}
	sourceValue, exists := values[source]
	if !exists || sourceValue.Revision != input.ExpectedSourceRevision {
		return domain.LocalizedMenu{}, content.ErrSourceChanged
	}
	value := values[locale]
	if value.Revision != input.ExpectedTargetRevision {
		return domain.LocalizedMenu{}, content.ErrTargetChanged
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		return domain.LocalizedMenu{}, ErrInvalid
	}
	value.Label = label
	value.Revision++
	value.State = "current"
	value.Origin = domain.LocaleOriginAI
	value.SourceRevision = sourceValue.Revision
	return value, nil
}
func normalizeTarget(kind, raw string) (string, string, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	raw = strings.TrimSpace(raw)
	if kind == "internal" {
		if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.Contains(raw, "..") || strings.ContainsAny(raw, "?#") {
			return "", "", ErrInvalid
		}
		return raw, "internal", nil
	}
	if kind == "external" {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return "", "", ErrInvalid
		}
		return parsed.String(), "external", nil
	}
	return "", "", ErrInvalid
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
