package visits

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"golang.org/x/text/language"
)

var (
	ErrInvalid      = errors.New("invalid visit request")
	ErrNotFound     = errors.New("visit subject not found")
	ErrRateLimit    = errors.New("visit rate limit exceeded")
	ErrInvalidState = errors.New("invalid visit state")
)

const (
	visitWindow    = 30 * time.Minute
	maxVisitKeys   = 4096
	defaultPopular = 5
	maxPopular     = 20
	// JSON consumers render these counts with JavaScript Number; persist only
	// values that remain exact across the public API boundary.
	maxStoredVisits = int64(1<<53 - 1)
)

type Result struct {
	Count   int64 `json:"count"`
	Counted bool  `json:"counted"`
}

type PopularPost struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Visits      int64  `json:"visits"`
	PublishedAt string `json:"-"`
}

type Profile struct {
	Posts      int   `json:"posts"`
	Categories int   `json:"categories"`
	Visits     int64 `json:"visits"`
}

type Snapshot struct {
	Locale         string              `json:"locale"`
	PopularPosts   []PopularPost       `json:"popularPosts"`
	Profile        Profile             `json:"profile"`
	PublicSubjects map[string]struct{} `json:"-"`
}

type Service struct {
	repository *fsrepo.Repository
	content    *content.Service
	mu         sync.Mutex
	recent     map[string]time.Time
	now        func() time.Time
}

type storedCounter struct {
	SchemaVersion   int       `yaml:"schemaVersion"`
	Kind            string    `yaml:"kind"`
	SubjectKind     string    `yaml:"subjectKind"`
	SubjectID       string    `yaml:"subjectId"`
	SubjectIdentity string    `yaml:"subjectIdentity"`
	Count           int64     `yaml:"count"`
	UpdatedAt       time.Time `yaml:"updatedAt"`
}

func NewService(repository *fsrepo.Repository, contentService *content.Service) *Service {
	return &Service{repository: repository, content: contentService, recent: make(map[string]time.Time), now: time.Now}
}

// Record counts at most one visit per anonymous address and subject during a
// bounded in-memory window. Address-derived data never reaches persistent
// storage: only the aggregate count and immutable content identity are saved.
func (s *Service) Record(kind, id, ipAddress string) (Result, error) {
	kind, subject, err := s.publishedSubject(kind, id)
	if err != nil {
		return Result{}, err
	}
	address := net.ParseIP(strings.TrimSpace(ipAddress))
	if address == nil {
		return Result{}, ErrInvalid
	}
	secret, err := s.secret()
	if err != nil {
		return Result{}, err
	}
	rateKey := keyedHash(secret, "visit\x00"+address.String()+"\x00"+kind+"\x00"+id)
	identity := subjectIdentity(subject)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if !s.allowLocked(rateKey, now) {
		return Result{}, ErrRateLimit
	}
	counter, err := s.readCounter(kind, id, identity)
	if err != nil {
		delete(s.recent, rateKey)
		return Result{}, err
	}
	if counter.Count >= maxStoredVisits {
		delete(s.recent, rateKey)
		return Result{}, ErrRateLimit
	}
	counter.Count++
	counter.UpdatedAt = now.UTC()
	if err := s.repository.WriteYAML(counterPath(kind, id), counter, false); err != nil {
		// A failed durable write must remain retryable instead of consuming the
		// visitor's entire de-duplication window.
		delete(s.recent, rateKey)
		return Result{}, err
	}
	return Result{Count: counter.Count, Counted: true}, nil
}

// PublicSnapshot returns only immutable public releases. It intentionally
// excludes markdown and summaries, and its aggregate categories are derived
// solely from public posts so private/draft-only taxonomy use is not leaked.
func (s *Service) PublicSnapshot(rawLocale string, limit int) (Snapshot, error) {
	locale, locales, err := s.enabledLocale(rawLocale)
	if err != nil {
		return Snapshot{}, ErrInvalid
	}
	if limit <= 0 {
		limit = defaultPopular
	}
	if limit > maxPopular {
		limit = maxPopular
	}
	posts, err := s.content.ListPostsForBuild()
	if err != nil {
		return Snapshot{}, err
	}
	pages, err := s.content.ListPagesForBuild()
	if err != nil {
		return Snapshot{}, err
	}

	result := Snapshot{
		Locale:         locale,
		PopularPosts:   []PopularPost{},
		PublicSubjects: make(map[string]struct{}),
	}
	categories := make(map[string]struct{})
	for _, post := range posts {
		if post.Meta.Status != domain.ContentStatusPublished || post.Meta.Visibility == domain.ContentVisibilityPrivate {
			continue
		}
		localized, ok := selectLocalized(post, locale, locales)
		if !ok {
			continue
		}
		count, err := s.publicCount("Post", post)
		if err != nil {
			return Snapshot{}, err
		}
		result.Profile.Posts++
		result.Profile.Visits += count
		result.PublicSubjects[subjectKey("Post", post.Meta.ID)] = struct{}{}
		for _, category := range post.Meta.Categories {
			if content.ValidPublicID(category, false) {
				categories[category] = struct{}{}
			}
		}
		publishedAt := ""
		if post.Meta.PublishedAt != nil {
			publishedAt = post.Meta.PublishedAt.UTC().Format(time.RFC3339Nano)
		}
		result.PopularPosts = append(result.PopularPosts, PopularPost{
			ID:          post.Meta.ID,
			Title:       localized.Title,
			URL:         "/" + locale + "/posts/" + post.Meta.ID + "/",
			Visits:      count,
			PublishedAt: publishedAt,
		})
	}
	for _, page := range pages {
		if page.Meta.Status != domain.ContentStatusPublished || page.Meta.Visibility == domain.ContentVisibilityPrivate {
			continue
		}
		if _, ok := selectLocalized(page, locale, locales); !ok {
			continue
		}
		count, err := s.publicCount("Page", page)
		if err != nil {
			return Snapshot{}, err
		}
		result.Profile.Visits += count
		result.PublicSubjects[subjectKey("Page", page.Meta.ID)] = struct{}{}
	}
	result.Profile.Categories = len(categories)
	sort.SliceStable(result.PopularPosts, func(i, j int) bool {
		if result.PopularPosts[i].Visits != result.PopularPosts[j].Visits {
			return result.PopularPosts[i].Visits > result.PopularPosts[j].Visits
		}
		if result.PopularPosts[i].PublishedAt != result.PopularPosts[j].PublishedAt {
			return result.PopularPosts[i].PublishedAt > result.PopularPosts[j].PublishedAt
		}
		return result.PopularPosts[i].ID < result.PopularPosts[j].ID
	})
	if len(result.PopularPosts) > limit {
		result.PopularPosts = result.PopularPosts[:limit]
	}
	return result, nil
}

func (s *Service) EnabledLocale(raw string) (string, error) {
	locale, _, err := s.enabledLocale(raw)
	return locale, err
}

// ValidateAll verifies every persisted aggregate against its path and current
// content identity. Orphans are tolerated only for the narrow crash window
// between permanent content deletion and ancillary cleanup.
func (s *Service) ValidateAll() error {
	root := filepath.Join(s.repository.Root(), "visits")
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrInvalidState
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(relative), "/")
		if len(parts) != 2 || filepath.Ext(parts[1]) != ".yaml" {
			return ErrInvalidState
		}
		kind, ok := normalizeSubjectKind(parts[0])
		id := strings.TrimSuffix(parts[1], ".yaml")
		if !ok || !content.ValidPublicID(id, false) {
			return ErrInvalidState
		}
		var counter storedCounter
		if err := s.repository.ReadYAML(filepath.ToSlash(filepath.Join("visits", relative)), &counter); err != nil {
			return err
		}
		if !validCounter(counter, kind, id) {
			return ErrInvalidState
		}
		subject, err := s.storedSubject(kind, id)
		if errors.Is(err, content.ErrNotFound) {
			return nil
		}
		if err != nil || !hmac.Equal([]byte(counter.SubjectIdentity), []byte(subjectIdentity(subject))) {
			return ErrInvalidState
		}
		return nil
	})
}

func (s *Service) publicCount(kind string, subject domain.Post) (int64, error) {
	identity := subjectIdentity(subject)
	s.mu.Lock()
	defer s.mu.Unlock()
	counter, err := s.readCounter(kind, subject.Meta.ID, identity)
	if err != nil {
		return 0, err
	}
	return counter.Count, nil
}

func (s *Service) publishedSubject(kind, id string) (string, domain.Post, error) {
	kind, ok := normalizeSubjectKind(kind)
	if !ok || !content.ValidPublicID(id, false) || s.repository == nil || s.content == nil {
		return "", domain.Post{}, ErrInvalid
	}
	subject, err := s.content.GetPublishedRelease(kind, id)
	if errors.Is(err, content.ErrNotFound) || errors.Is(err, content.ErrInvalidID) {
		return "", domain.Post{}, ErrNotFound
	}
	if err != nil {
		return "", domain.Post{}, err
	}
	return kind, subject, nil
}

func (s *Service) storedSubject(kind, id string) (domain.Post, error) {
	if s.content == nil {
		return domain.Post{}, ErrInvalid
	}
	if kind == "Post" {
		return s.content.GetPost(id)
	}
	return s.content.GetPage(id)
}

func (s *Service) secret() ([]byte, error) {
	var secrets domain.SecretsConfig
	if err := s.repository.ReadYAML("config/secrets.yaml", &secrets); err != nil {
		return nil, err
	}
	secret := strings.TrimSpace(secrets.CommentHMACKey)
	if secret == "" {
		return nil, errors.New("visit privacy secret is unavailable")
	}
	return []byte(secret), nil
}

func (s *Service) enabledLocale(raw string) (string, domain.LocalesConfig, error) {
	tag, err := language.Parse(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(raw) == "" {
		return "", domain.LocalesConfig{}, ErrInvalid
	}
	locale := tag.String()
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return "", domain.LocalesConfig{}, err
	}
	for _, definition := range config.Enabled {
		if definition.Enabled && definition.Code == locale {
			return locale, config, nil
		}
	}
	return "", domain.LocalesConfig{}, ErrInvalid
}

func (s *Service) readCounter(kind, id, identity string) (storedCounter, error) {
	path := counterPath(kind, id)
	var counter storedCounter
	if err := s.repository.ReadYAML(path, &counter); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newCounter(kind, id, identity), nil
		}
		return storedCounter{}, err
	}
	if !validCounter(counter, kind, id) {
		return storedCounter{}, fmt.Errorf("%w: %s", ErrInvalidState, path)
	}
	if !hmac.Equal([]byte(counter.SubjectIdentity), []byte(identity)) {
		return newCounter(kind, id, identity), nil
	}
	return counter, nil
}

func (s *Service) allowLocked(key string, now time.Time) bool {
	cutoff := now.Add(-visitWindow)
	if previous, exists := s.recent[key]; exists {
		if previous.After(cutoff) {
			return false
		}
		delete(s.recent, key)
	}
	if len(s.recent) >= maxVisitKeys {
		for candidate, value := range s.recent {
			if !value.After(cutoff) {
				delete(s.recent, candidate)
			}
		}
		if len(s.recent) >= maxVisitKeys {
			return false
		}
	}
	s.recent[key] = now
	return true
}

func newCounter(kind, id, identity string) storedCounter {
	return storedCounter{
		SchemaVersion:   domain.SchemaVersion,
		Kind:            "VisitCounter",
		SubjectKind:     kind,
		SubjectID:       id,
		SubjectIdentity: identity,
	}
}

func validCounter(counter storedCounter, kind, id string) bool {
	return counter.SchemaVersion == domain.SchemaVersion &&
		counter.Kind == "VisitCounter" &&
		counter.SubjectKind == kind &&
		counter.SubjectID == id &&
		counter.Count > 0 &&
		counter.Count <= maxStoredVisits &&
		validDigest(counter.SubjectIdentity) &&
		!counter.UpdatedAt.IsZero()
}

func normalizeSubjectKind(kind string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "post", "posts":
		return "Post", true
	case "page", "pages":
		return "Page", true
	default:
		return "", false
	}
}

func counterPath(kind, id string) string {
	return filepath.Join("visits", strings.ToLower(kind)+"s", id+".yaml")
}

func subjectIdentity(subject domain.Post) string {
	value := subject.Meta.Kind + "\x00" + subject.Meta.ID + "\x00" + subject.Meta.CreatedAt.UTC().Format(time.RFC3339Nano)
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func keyedHash(secret []byte, value string) string {
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte(value))
	return hex.EncodeToString(digest.Sum(nil))
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func selectLocalized(subject domain.Post, requested string, config domain.LocalesConfig) (domain.LocalizedMarkdown, bool) {
	enabled := make(map[string]struct{}, len(config.Enabled))
	for _, definition := range config.Enabled {
		if definition.Enabled {
			enabled[definition.Code] = struct{}{}
		}
	}
	chain := []string{requested}
	chain = append(chain, localeconfig.FixedFallbackOrder()...)
	chain = append(chain, subject.Meta.SourceLocale)
	seen := make(map[string]struct{}, len(chain))
	for _, locale := range chain {
		if _, duplicate := seen[locale]; duplicate {
			continue
		}
		seen[locale] = struct{}{}
		if _, ok := enabled[locale]; !ok {
			continue
		}
		if localized, ok := subject.Content[locale]; ok {
			return localized, true
		}
	}
	return domain.LocalizedMarkdown{}, false
}

func SubjectKey(kind, id string) string {
	return subjectKey(kind, id)
}

func subjectKey(kind, id string) string {
	return kind + "\x00" + id
}
