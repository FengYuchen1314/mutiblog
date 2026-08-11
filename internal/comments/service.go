package comments

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/mail"
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
	ErrInvalid   = errors.New("invalid comment")
	ErrNotFound  = errors.New("comment not found")
	ErrRateLimit = errors.New("comment rate limit exceeded")
)

type CreateInput struct {
	SubjectKind string
	SubjectID   string
	ParentID    string
	Name        string
	Email       string
	Website     string
	Content     string
	Locale      string
	IPAddress   string
	UserAgent   string
}

type AdminQuery struct {
	Status string
	Kind   string
	Query  string
	Page   int
	Size   int
}

type Service struct {
	repository *fsrepo.Repository
	mu         sync.Mutex
	recent     map[string][]time.Time
}

const recentClientLimit = 4096

func NewService(repository *fsrepo.Repository) *Service {
	return &Service{repository: repository, recent: make(map[string][]time.Time)}
}

func (s *Service) Create(input CreateInput) (domain.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kind, err := normalizeSubjectKind(input.SubjectKind)
	if err != nil || !content.ValidPublicID(input.SubjectID, false) {
		return domain.Comment{}, ErrInvalid
	}
	if err := s.requirePublishedSubject(kind, input.SubjectID); err != nil {
		return domain.Comment{}, ErrInvalid
	}
	locale, err := s.normalizeEnabledLocale(input.Locale)
	if err != nil {
		return domain.Comment{}, ErrInvalid
	}
	settings, err := s.settings()
	if err != nil {
		return domain.Comment{}, err
	}
	name := strings.TrimSpace(input.Name)
	body := strings.TrimSpace(input.Content)
	if name == "" || len([]rune(name)) > 80 || body == "" || len([]rune(body)) > settings.MaxLength || strings.ContainsRune(body, '\x00') {
		return domain.Comment{}, ErrInvalid
	}
	website, err := normalizeWebsite(input.Website)
	if err != nil {
		return domain.Comment{}, ErrInvalid
	}
	emailHash := ""
	if strings.TrimSpace(input.Email) != "" {
		address, err := mail.ParseAddress(strings.TrimSpace(input.Email))
		if err != nil || !strings.Contains(address.Address, "@") {
			return domain.Comment{}, ErrInvalid
		}
		digest := sha256.Sum256([]byte(strings.ToLower(address.Address)))
		emailHash = "sha256:" + hex.EncodeToString(digest[:])
	}
	secret, err := s.commentSecret()
	if err != nil {
		return domain.Comment{}, err
	}
	ipHash := hashIP(secret, input.IPAddress)
	if !s.allow(ipHash) {
		return domain.Comment{}, ErrRateLimit
	}
	id, err := content.NewPublicID()
	if err != nil {
		return domain.Comment{}, err
	}
	status := "pending"
	if settings.Moderation == "none" {
		status = "approved"
	}
	now := time.Now().UTC()
	comment := domain.Comment{SchemaVersion: domain.SchemaVersion, Kind: "Comment", ID: id, Subject: domain.CommentSubject{Kind: kind, ID: input.SubjectID}, ParentID: strings.TrimSpace(input.ParentID), Status: status, Author: domain.CommentAuthor{Name: name, EmailHash: emailHash, Website: website}, Content: body, Locale: locale, CreatedAt: now, IPHash: ipHash, UserAgentFamily: userAgentFamily(input.UserAgent)}
	if comment.ParentID != "" {
		parent, err := s.Get(kind, input.SubjectID, comment.ParentID)
		if err != nil || parent.ParentID != "" {
			return domain.Comment{}, ErrInvalid
		}
	}
	if err := s.repository.WriteYAML(commentPath(kind, input.SubjectID, id), comment, false); err != nil {
		return domain.Comment{}, err
	}
	return comment, nil
}

func (s *Service) ListPublic(kind, id string, page, size int) ([]domain.Comment, int, error) {
	if err := s.requirePublishedSubjectExists(kind, id); err != nil {
		return nil, 0, err
	}
	items, err := s.listSubject(kind, id)
	if err != nil {
		return nil, 0, err
	}
	approved := items[:0]
	for _, item := range items {
		if item.Status == "approved" {
			approved = append(approved, item)
		}
	}
	total := len(approved)
	return paginate(approved, page, size), total, nil
}

func (s *Service) ListAll() ([]domain.Comment, error) {
	items := make([]domain.Comment, 0)
	root := filepath.Join(s.repository.Root(), "comments")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		location, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(location), "/")
		if len(parts) != 3 || filepath.Ext(parts[2]) != ".yaml" {
			return ErrInvalid
		}
		kind, err := normalizeSubjectKind(parts[0])
		if err != nil {
			return ErrInvalid
		}
		subjectID := parts[1]
		commentID := strings.TrimSuffix(parts[2], ".yaml")
		relative, err := filepath.Rel(s.repository.Root(), path)
		if err != nil {
			return err
		}
		var comment domain.Comment
		if err := s.repository.ReadYAML(relative, &comment); err != nil {
			return err
		}
		if !validStoredComment(comment, kind, subjectID, commentID) {
			return ErrInvalid
		}
		items = append(items, comment)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) ListAdmin(query AdminQuery) ([]domain.Comment, int, error) {
	items, err := s.ListAll()
	if err != nil {
		return nil, 0, err
	}
	status := strings.ToLower(strings.TrimSpace(query.Status))
	kind := strings.ToLower(strings.TrimSpace(query.Kind))
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	filtered := make([]domain.Comment, 0, len(items))
	for _, item := range items {
		if status != "" && status != "all" && item.Status != status {
			continue
		}
		if kind != "" && kind != "all" && strings.ToLower(item.Subject.Kind) != kind {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(item.Author.Name+"\n"+item.Content+"\n"+item.Subject.ID), needle) {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	return paginate(filtered, query.Page, query.Size), total, nil
}

func (s *Service) Moderate(kind, subjectID, id, status string) (domain.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status != "approved" && status != "pending" && status != "spam" {
		return domain.Comment{}, ErrInvalid
	}
	comment, err := s.Get(kind, subjectID, id)
	if err != nil {
		return domain.Comment{}, err
	}
	comment.Status = status
	if err := s.repository.WriteYAML(commentPath(comment.Subject.Kind, subjectID, id), comment, false); err != nil {
		return domain.Comment{}, err
	}
	return comment, nil
}

func (s *Service) Delete(kind, subjectID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	comment, err := s.Get(kind, subjectID, id)
	if err != nil {
		return err
	}
	return s.repository.RemoveFile(commentPath(comment.Subject.Kind, comment.Subject.ID, comment.ID))
}

func (s *Service) Get(kind, subjectID, id string) (domain.Comment, error) {
	kind, err := normalizeSubjectKind(kind)
	if err != nil || !content.ValidPublicID(subjectID, false) || !content.ValidPublicID(id, false) {
		return domain.Comment{}, ErrNotFound
	}
	var comment domain.Comment
	if err := s.repository.ReadYAML(commentPath(kind, subjectID, id), &comment); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.Comment{}, ErrNotFound
		}
		return domain.Comment{}, err
	}
	if !validStoredComment(comment, kind, subjectID, id) {
		return domain.Comment{}, ErrInvalid
	}
	return comment, nil
}

func (s *Service) listSubject(kind, id string) ([]domain.Comment, error) {
	kind, err := normalizeSubjectKind(kind)
	if err != nil || !content.ValidPublicID(id, false) {
		return nil, ErrInvalid
	}
	entries, err := s.repository.ReadDir(filepath.Join("comments", kind, id))
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Comment{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]domain.Comment, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var comment domain.Comment
		commentID := strings.TrimSuffix(entry.Name(), ".yaml")
		if err := s.repository.ReadYAML(commentPath(kind, id, commentID), &comment); err != nil {
			return nil, err
		}
		if !validStoredComment(comment, kind, id, commentID) {
			return nil, ErrInvalid
		}
		items = append(items, comment)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) settings() (domain.CommentsConfig, error) {
	var settings domain.CommentsConfig
	if err := s.repository.ReadYAML("config/comments.yaml", &settings); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return domain.CommentsConfig{}, err
		}
		settings = domain.CommentsConfig{SchemaVersion: 1, Moderation: "pending", PageSize: 20, MaxLength: 2000}
		if err := s.repository.WriteYAML("config/comments.yaml", settings, false); err != nil {
			return domain.CommentsConfig{}, err
		}
	}
	if settings.PageSize < 1 {
		settings.PageSize = 20
	}
	if settings.MaxLength < 1 {
		settings.MaxLength = 2000
	}
	return settings, nil
}

func (s *Service) commentSecret() ([]byte, error) {
	var secrets domain.SecretsConfig
	if err := s.repository.ReadYAML("config/secrets.yaml", &secrets); err != nil {
		return nil, err
	}
	if secrets.CommentHMACKey == "" {
		buffer := make([]byte, 32)
		if _, err := rand.Read(buffer); err != nil {
			return nil, err
		}
		secrets.CommentHMACKey = hex.EncodeToString(buffer)
		if err := s.repository.WriteYAML("config/secrets.yaml", secrets, true); err != nil {
			return nil, err
		}
	}
	return []byte(secrets.CommentHMACKey), nil
}

func (s *Service) requirePublishedSubject(kind, id string) error {
	subject, err := content.NewService(s.repository).GetPublishedRelease(kind, id)
	if err != nil {
		if errors.Is(err, content.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if subject.Meta.CommentPolicy != "open" {
		return ErrInvalid
	}
	return nil
}

func (s *Service) requirePublishedSubjectExists(kind, id string) error {
	if _, err := content.NewService(s.repository).GetPublishedRelease(kind, id); err != nil {
		if errors.Is(err, content.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *Service) normalizeEnabledLocale(raw string) (string, error) {
	tag, err := language.Parse(raw)
	if err != nil {
		return "", err
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
	return "", ErrInvalid
}

func (s *Service) allow(key string) bool {
	now := time.Now()
	cutoff := now.Add(-10 * time.Minute)
	if _, exists := s.recent[key]; !exists && len(s.recent) >= recentClientLimit {
		for candidate, values := range s.recent {
			kept := values[:0]
			for _, value := range values {
				if value.After(cutoff) {
					kept = append(kept, value)
				}
			}
			if len(kept) == 0 {
				delete(s.recent, candidate)
			} else {
				s.recent[candidate] = kept
			}
		}
		// Keep the unauthenticated limiter strictly bounded even during a
		// high-cardinality address rotation attack. Existing clients retain
		// their windows; unseen clients fail closed until an entry expires.
		if len(s.recent) >= recentClientLimit {
			return false
		}
	}
	recent := s.recent[key][:0]
	for _, value := range s.recent[key] {
		if value.After(cutoff) {
			recent = append(recent, value)
		}
	}
	if len(recent) >= 5 {
		s.recent[key] = recent
		return false
	}
	s.recent[key] = append(recent, now)
	return true
}

func normalizeSubjectKind(kind string) (string, error) {
	switch strings.ToLower(kind) {
	case "post":
		return "Post", nil
	case "page":
		return "Page", nil
	default:
		return "", ErrInvalid
	}
}
func commentPath(kind, subjectID, id string) string {
	return filepath.Join("comments", kind, subjectID, id+".yaml")
}

func validStoredComment(comment domain.Comment, kind, subjectID, id string) bool {
	if comment.SchemaVersion != domain.SchemaVersion || comment.Kind != "Comment" || comment.ID != id || comment.Subject.Kind != kind || comment.Subject.ID != subjectID {
		return false
	}
	if !content.ValidPublicID(comment.ID, false) || !content.ValidPublicID(comment.Subject.ID, false) {
		return false
	}
	return comment.Status == "pending" || comment.Status == "approved" || comment.Status == "spam"
}

func hashIP(secret []byte, ip string) string {
	digest := hmac.New(sha256.New, secret)
	_, _ = digest.Write([]byte(strings.TrimSpace(ip)))
	return "hmac-sha256:" + hex.EncodeToString(digest.Sum(nil))
}
func normalizeWebsite(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", ErrInvalid
	}
	return parsed.String(), nil
}
func userAgentFamily(raw string) string {
	lower := strings.ToLower(raw)
	for _, pair := range []struct{ needle, name string }{{"firefox", "Firefox"}, {"edg/", "Edge"}, {"chrome", "Chrome"}, {"safari", "Safari"}} {
		if strings.Contains(lower, pair.needle) {
			return pair.name
		}
	}
	return "Other"
}
func paginate(items []domain.Comment, page, size int) []domain.Comment {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	if len(items) == 0 || page > (len(items)-1)/size+1 {
		return []domain.Comment{}
	}
	start := (page - 1) * size
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
