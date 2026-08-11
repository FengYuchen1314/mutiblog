package content

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"golang.org/x/text/language"
)

var (
	ErrNotFound        = errors.New("content not found")
	ErrAlreadyExists   = errors.New("content id already exists")
	ErrConflict        = errors.New("content revision conflict")
	ErrInvalidID       = errors.New("invalid content id")
	ErrLocaleDisabled  = errors.New("locale is not enabled")
	ErrManualProtected = errors.New("manual translation is protected")
	ErrSourceChanged   = errors.New("source content changed during translation")
	ErrTargetChanged   = errors.New("translation target changed during translation")
	customSlugPattern  = regexp.MustCompile(`^[a-z]+(?:-[a-z]+)*$`)
	compactUUIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	timestampIDPattern = regexp.MustCompile(`^[0-9]{13}$`)
	lastTimestampID    atomic.Int64
)

const (
	IDStrategyUUID      = "uuid"
	IDStrategyTimestamp = "timestamp"
)

type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
}

type CreatePostInput struct {
	ID             string
	Title          string
	Summary        string
	SEOTitle       string
	SEODescription string
	Markdown       string
}

type UpdateLocaleInput struct {
	ExpectedRevision int
	Title            string
	Summary          string
	SEOTitle         string
	SEODescription   string
	Markdown         string
}

type ApplyAITranslationInput struct {
	ExpectedSourceRevision int
	ExpectedTargetRevision *int
	OverwriteManual        bool
	Content                domain.LocalizedMarkdown
}

func NewService(repository *fsrepo.Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListPosts() ([]domain.Post, error) {
	entries, err := s.repository.ReadDir("content/posts")
	if err != nil {
		return nil, err
	}
	posts := make([]domain.Post, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		post, err := s.GetPost(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read post %s: %w", entry.Name(), err)
		}
		posts = append(posts, post)
	}
	sort.Slice(posts, func(i, j int) bool { return posts[i].Meta.UpdatedAt.After(posts[j].Meta.UpdatedAt) })
	return posts, nil
}

func (s *Service) GetPost(id string) (domain.Post, error) {
	if !validID(id) {
		return domain.Post{}, ErrInvalidID
	}
	var meta domain.PostMeta
	if err := s.repository.ReadYAML(postPath(id, "meta.yaml"), &meta); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Post{}, ErrNotFound
		}
		return domain.Post{}, err
	}
	if !validStoredContentMeta(meta, id, "Post") {
		return domain.Post{}, errors.New("post metadata identity is invalid")
	}
	contents := make(map[string]domain.LocalizedMarkdown, len(meta.Locales))
	for locale := range meta.Locales {
		data, err := s.repository.ReadFile(postPath(id, locale+".md"))
		if err != nil {
			return domain.Post{}, err
		}
		localized, err := decodeMarkdown(data)
		if err != nil {
			return domain.Post{}, fmt.Errorf("decode %s locale %s: %w", id, locale, err)
		}
		contents[locale] = localized
	}
	post := domain.Post{Meta: meta, Content: contents}
	s.decoratePublicationState("Post", &post)
	return post, nil
}

func (s *Service) CreatePost(input CreatePostInput) (domain.Post, error) {
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
	if exists, err := s.repository.Exists(postPath(id, "meta.yaml")); err != nil {
		return domain.Post{}, err
	} else if exists {
		return domain.Post{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	meta := domain.PostMeta{
		SchemaVersion: domain.SchemaVersion, Kind: "Post", ID: id, Status: domain.ContentStatusDraft,
		SourceLocale: locales.SourceLocale, CreatedAt: now, UpdatedAt: now, Categories: []string{}, Tags: []string{},
		Visibility: domain.ContentVisibilityPublic, CommentPolicy: "open", Template: "post", Revision: 1, BaseRevision: 1, HeadRevision: 1,
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
	if err := s.writeLocale(id, locales.SourceLocale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(postPath(id, "meta.yaml"), meta, false); err != nil {
		return domain.Post{}, err
	}
	return domain.Post{Meta: meta, Content: map[string]domain.LocalizedMarkdown{locales.SourceLocale: localized}}, nil
}

func (s *Service) UpdateLocale(id, locale string, input UpdateLocaleInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locale, err := s.normalizeEnabledLocale(locale)
	if err != nil {
		return domain.Post{}, err
	}
	post, err := s.GetPost(id)
	if err != nil {
		return domain.Post{}, err
	}
	if post.Meta.Revision != input.ExpectedRevision {
		return domain.Post{}, ErrConflict
	}
	if err := s.snapshot(post); err != nil {
		return domain.Post{}, err
	}
	localized := domain.LocalizedMarkdown{Title: strings.TrimSpace(input.Title), Summary: strings.TrimSpace(input.Summary), SEOTitle: strings.TrimSpace(input.SEOTitle), SEODescription: strings.TrimSpace(input.SEODescription), Markdown: input.Markdown}
	if localized.Title == "" {
		return domain.Post{}, errors.New("title is required")
	}
	state, exists := post.Meta.Locales[locale]
	if !exists {
		state = domain.LocaleContentState{Revision: 0, SourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision}
	}
	state.Revision++
	state.State = "current"
	if locale == post.Meta.SourceLocale {
		state.Origin = domain.LocaleOriginSource
		state.SourceRevision = state.Revision
		for derivedLocale, derived := range post.Meta.Locales {
			if derivedLocale != locale {
				derived.State = "stale"
				post.Meta.Locales[derivedLocale] = derived
			}
		}
	} else {
		state.Origin = domain.LocaleOriginManual
		state.SourceRevision = post.Meta.Locales[post.Meta.SourceLocale].Revision
	}
	post.Meta.Locales[locale] = state
	advanceHead(&post.Meta)
	post.Meta.UpdatedAt = time.Now().UTC()
	if err := s.writeLocale(id, locale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(postPath(id, "meta.yaml"), post.Meta, false); err != nil {
		return domain.Post{}, err
	}
	post.Content[locale] = localized
	post.Meta.HasUnpublishedChanges = post.Meta.Status == domain.ContentStatusPublished
	return post, nil
}

func (s *Service) ApplyAITranslation(id, locale string, input ApplyAITranslationInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	locale, err := s.normalizeEnabledLocale(locale)
	if err != nil {
		return domain.Post{}, err
	}
	post, err := s.GetPost(id)
	if err != nil {
		return domain.Post{}, err
	}
	if locale == post.Meta.SourceLocale {
		return domain.Post{}, ErrLocaleDisabled
	}
	sourceState := post.Meta.Locales[post.Meta.SourceLocale]
	if sourceState.Revision != input.ExpectedSourceRevision {
		return domain.Post{}, ErrSourceChanged
	}
	state, exists := post.Meta.Locales[locale]
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
	if err := s.snapshot(post); err != nil {
		return domain.Post{}, err
	}
	state.Revision++
	state.State = "current"
	state.Origin = domain.LocaleOriginAI
	state.SourceRevision = sourceState.Revision
	post.Meta.Locales[locale] = state
	advanceHead(&post.Meta)
	post.Meta.UpdatedAt = time.Now().UTC()
	if err := s.writeLocale(id, locale, localized); err != nil {
		return domain.Post{}, err
	}
	if err := s.repository.WriteYAML(postPath(id, "meta.yaml"), post.Meta, false); err != nil {
		return domain.Post{}, err
	}
	post.Content[locale] = localized
	post.Meta.HasUnpublishedChanges = post.Meta.Status == domain.ContentStatusPublished
	return post, nil
}

func (s *Service) PublishPost(id string, expectedRevision int) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	post, err := s.GetPost(id)
	if err != nil {
		return domain.Post{}, err
	}
	if post.Meta.Revision != expectedRevision {
		return domain.Post{}, ErrConflict
	}
	if post.Meta.Status == domain.ContentStatusRecycled {
		return domain.Post{}, ErrInvalidStatus
	}
	if err := s.snapshot(post); err != nil {
		return domain.Post{}, err
	}
	previousMeta := post.Meta
	now := time.Now().UTC()
	post.Meta.Status = domain.ContentStatusPublished
	post.Meta.ScheduledRevision = 0
	post.Meta.UpdatedAt = now
	if post.Meta.PublishedAt == nil {
		post.Meta.PublishedAt = &now
	}
	advanceHead(&post.Meta)
	post.Meta.ReleaseRevision = post.Meta.Revision
	if err := s.repository.WriteYAML(postPath(id, "meta.yaml"), post.Meta, false); err != nil {
		return domain.Post{}, err
	}
	if err := s.writeRelease("Post", post); err != nil {
		_ = s.repository.WriteYAML(postPath(id, "meta.yaml"), previousMeta, false)
		return domain.Post{}, err
	}
	post.Meta.HasUnpublishedChanges = false
	return post, nil
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
		return "", ErrLocaleDisabled
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
	return "", ErrLocaleDisabled
}

func (s *Service) writeLocale(id, locale string, value domain.LocalizedMarkdown) error {
	data, err := encodeMarkdown(value)
	if err != nil {
		return err
	}
	return s.repository.WriteFile(postPath(id, locale+".md"), data, 0o640)
}

func (s *Service) snapshot(post domain.Post) error {
	if err := s.ensureRelease("Post", post); err != nil {
		return err
	}
	revisionID := fmt.Sprintf("%06d-%d", post.Meta.Revision, time.Now().UTC().UnixMilli())
	base := filepath.Join("revisions", "posts", post.Meta.ID, revisionID)
	if err := s.repository.WriteYAML(filepath.Join(base, "meta.yaml"), post.Meta, false); err != nil {
		return err
	}
	for locale, localized := range post.Content {
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

func postPath(id string, parts ...string) string {
	items := append([]string{"content", "posts", id}, parts...)
	return filepath.Join(items...)
}

func validID(id string) bool {
	return customSlugPattern.MatchString(id) || compactUUIDPattern.MatchString(id) || timestampIDPattern.MatchString(id)
}

func validStoredContentMeta(meta domain.PostMeta, id, kind string) bool {
	if meta.SchemaVersion != domain.SchemaVersion || meta.ID != id || meta.Kind != kind || meta.Revision < 1 || meta.ScheduledRevision < 0 || meta.ScheduledRevision > meta.Revision || meta.Locales == nil {
		return false
	}
	if _, exists := meta.Locales[meta.SourceLocale]; !exists {
		return false
	}
	for locale := range meta.Locales {
		tag, err := language.Parse(locale)
		if err != nil || tag.String() != locale {
			return false
		}
	}
	return true
}

func ValidPublicID(id string, customOnly bool) bool {
	if customOnly {
		return customSlugPattern.MatchString(id)
	}
	return validID(id)
}

func NormalizeIDStrategy(strategy string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(strategy)) {
	case "", IDStrategyUUID:
		return IDStrategyUUID, true
	case IDStrategyTimestamp:
		return IDStrategyTimestamp, true
	default:
		return "", false
	}
}

func ConfiguredPublicID(repository *fsrepo.Repository) (string, error) {
	var site domain.SiteConfig
	if err := repository.ReadYAML("config/site.yaml", &site); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewPublicID(IDStrategyUUID)
		}
		return "", err
	}
	strategy, valid := NormalizeIDStrategy(site.IDStrategy)
	if !valid {
		return "", fmt.Errorf("invalid ID strategy %q", site.IDStrategy)
	}
	return NewPublicID(strategy)
}

func NewPublicID(strategy ...string) (string, error) {
	selected := IDStrategyUUID
	if len(strategy) > 0 {
		var valid bool
		selected, valid = NormalizeIDStrategy(strategy[0])
		if !valid {
			return "", fmt.Errorf("invalid ID strategy %q", strategy[0])
		}
	}
	if selected == IDStrategyTimestamp {
		return newTimestampID(), nil
	}
	return newCompactUUID()
}

func newTimestampID() string {
	for {
		now := time.Now().UTC().UnixMilli()
		previous := lastTimestampID.Load()
		if now <= previous {
			now = previous + 1
		}
		if lastTimestampID.CompareAndSwap(previous, now) {
			return strconv.FormatInt(now, 10)
		}
	}
}

func newCompactUUID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	return hex.EncodeToString(buffer), nil
}
