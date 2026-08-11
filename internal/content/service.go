package content

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
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
	// mu prevents successful reads on this Service instance from observing the
	// intermediate files of another operation on the same instance. It is not a
	// cross-instance or cross-process repository transaction. Public read methods
	// take RLock; methods suffixed Locked require caller-held RLock or Lock.
	mu sync.RWMutex
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

// ApplyAIReleaseTranslationInput is used only when a published entity has a
// newer unpublished source head. In that case the current public release and
// the future head need separate translations with independent CAS checks.
type ApplyAIReleaseTranslationInput struct {
	ExpectedReleaseRevision int
	ExpectedSourceRevision  int
	ExpectedTargetRevision  int
	Content                 domain.LocalizedMarkdown
}

func NewService(repository *fsrepo.Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) ListPosts() ([]domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listPostsLocked()
}

// listPostsLocked expects the caller to hold s.mu for reading or writing.
func (s *Service) listPostsLocked() ([]domain.Post, error) {
	return s.listContentLocked(postContent)
}

func (s *Service) GetPost(id string) (domain.Post, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getPostLocked(id)
}

// getPostLocked expects the caller to hold s.mu for reading or writing.
func (s *Service) getPostLocked(id string) (domain.Post, error) {
	return s.getContentLocked(postContent, id)
}

func (s *Service) CreatePost(input CreatePostInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createContentLocked(postContent, input)
}

func (s *Service) UpdateLocale(id, locale string, input UpdateLocaleInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateContentLocaleLocked(postContent, id, locale, input)
}

func (s *Service) ApplyAITranslation(id, locale string, input ApplyAITranslationInput) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyAIContentTranslationLocked(postContent, id, locale, input)
}

func (s *Service) PublishPost(id string, expectedRevision int) (domain.Post, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.publishContentLocked(postContent, id, expectedRevision)
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
	return s.writeContentLocale(postContent, id, locale, value)
}

func (s *Service) snapshot(post domain.Post) error {
	return s.snapshotContent(postContent, post)
}

func postPath(id string, parts ...string) string {
	return postContent.path(id, parts...)
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
