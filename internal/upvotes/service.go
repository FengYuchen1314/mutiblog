package upvotes

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

var (
	ErrInvalid      = errors.New("invalid upvote request")
	ErrNotFound     = errors.New("upvote subject not found")
	ErrRateLimit    = errors.New("upvote rate limit exceeded")
	ErrInvalidState = errors.New("invalid upvote state")
)

const (
	rateLimitCount      = 30
	rateLimitWindow     = 10 * time.Minute
	maxRateLimitKeys    = 4096
	maxVotersPerSubject = 100000
)

var visitorTokenPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Result struct {
	Count   int64 `json:"count"`
	Upvoted bool  `json:"upvoted"`
	Created bool  `json:"created"`
}

type Service struct {
	repository   *fsrepo.Repository
	content      *content.Service
	mu           sync.Mutex
	attempts     map[string][]time.Time
	writeCounter func(string, storedCounter) error
}

type storedCounter struct {
	SchemaVersion   int       `yaml:"schemaVersion"`
	Kind            string    `yaml:"kind"`
	SubjectKind     string    `yaml:"subjectKind"`
	SubjectID       string    `yaml:"subjectId"`
	SubjectIdentity string    `yaml:"subjectIdentity"`
	Count           int64     `yaml:"count"`
	VoterHashes     []string  `yaml:"voterHashes"`
	UpdatedAt       time.Time `yaml:"updatedAt"`
}

func NewService(repository *fsrepo.Repository, contentService *content.Service) *Service {
	return &Service{
		repository: repository,
		content:    contentService,
		attempts:   make(map[string][]time.Time),
		writeCounter: func(path string, counter storedCounter) error {
			return repository.WriteYAML(path, counter, false)
		},
	}
}

func NewVisitorToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func ValidVisitorToken(token string) bool {
	return visitorTokenPattern.MatchString(token)
}

func (s *Service) Get(kind, id, visitorToken string) (Result, error) {
	kind, subject, err := s.publishedSubject(kind, id)
	if err != nil {
		return Result{}, err
	}
	if !ValidVisitorToken(visitorToken) {
		return Result{}, ErrInvalid
	}
	secret, err := s.secret()
	if err != nil {
		return Result{}, err
	}
	voterHash := keyedHash(secret, "visitor\x00"+visitorToken)
	identity := subjectIdentity(subject)

	s.mu.Lock()
	defer s.mu.Unlock()
	counter, err := s.readCounter(kind, id, identity)
	if err != nil {
		return Result{}, err
	}
	return Result{Count: counter.Count, Upvoted: containsHash(counter.VoterHashes, voterHash)}, nil
}

func (s *Service) Add(kind, id, visitorToken, ipAddress string) (Result, error) {
	kind, subject, err := s.publishedSubject(kind, id)
	if err != nil {
		return Result{}, err
	}
	if !ValidVisitorToken(visitorToken) {
		return Result{}, ErrInvalid
	}
	address := net.ParseIP(strings.TrimSpace(ipAddress))
	if address == nil {
		return Result{}, ErrInvalid
	}
	secret, err := s.secret()
	if err != nil {
		return Result{}, err
	}
	voterHash := keyedHash(secret, "visitor\x00"+visitorToken)
	rateKey := keyedHash(secret, "address\x00"+address.String())
	identity := subjectIdentity(subject)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.allowLocked(rateKey, time.Now()) {
		return Result{}, ErrRateLimit
	}
	counter, err := s.readCounter(kind, id, identity)
	if err != nil {
		if !errors.Is(err, ErrInvalidState) {
			refundLatestAttempt(s.attempts, rateKey)
		}
		return Result{}, err
	}
	if containsHash(counter.VoterHashes, voterHash) {
		return Result{Count: counter.Count, Upvoted: true}, nil
	}
	if err := appendVoter(&counter, voterHash); err != nil {
		return Result{}, err
	}
	counter.UpdatedAt = time.Now().UTC()
	writeCounter := s.writeCounter
	if writeCounter == nil {
		writeCounter = func(path string, counter storedCounter) error {
			return s.repository.WriteYAML(path, counter, false)
		}
	}
	if err := writeCounter(counterPath(kind, id), counter); err != nil {
		refundLatestAttempt(s.attempts, rateKey)
		return Result{}, err
	}
	return Result{Count: counter.Count, Upvoted: true, Created: true}, nil
}

// ValidateAll verifies persisted counter identity without requiring the local
// HMAC secret. Backup restore uses it before any staged data becomes active.
func (s *Service) ValidateAll() error {
	root := filepath.Join(s.repository.Root(), "upvotes")
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrInvalidState
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
		if err := s.repository.ReadYAML(filepath.ToSlash(filepath.Join("upvotes", relative)), &counter); err != nil {
			return err
		}
		if !validCounter(counter, kind, id) {
			return ErrInvalidState
		}
		subject, err := s.storedSubject(kind, id)
		if errors.Is(err, content.ErrNotFound) {
			// A crash between permanent content deletion and ancillary cleanup
			// must not make an otherwise valid backup unrestorable.
			return nil
		}
		if err != nil || !hmac.Equal([]byte(counter.SubjectIdentity), []byte(subjectIdentity(subject))) {
			return ErrInvalidState
		}
		return nil
	})
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
		return nil, errors.New("upvote privacy secret is unavailable")
	}
	return []byte(secret), nil
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
	cutoff := now.Add(-rateLimitWindow)
	if _, exists := s.attempts[key]; !exists && len(s.attempts) >= maxRateLimitKeys {
		for candidate, values := range s.attempts {
			if len(values) == 0 || !values[len(values)-1].After(cutoff) {
				delete(s.attempts, candidate)
			}
		}
		if len(s.attempts) >= maxRateLimitKeys {
			return false
		}
	}
	recent := s.attempts[key][:0]
	for _, attempt := range s.attempts[key] {
		if attempt.After(cutoff) {
			recent = append(recent, attempt)
		}
	}
	if len(recent) >= rateLimitCount {
		s.attempts[key] = recent
		return false
	}
	s.attempts[key] = append(recent, now)
	return true
}

// refundLatestAttempt removes only the timestamp appended for the failed
// operation. Earlier legitimate attempts in the same window must continue to
// count toward the rate limit. The caller must hold the service mutex.
func refundLatestAttempt(attempts map[string][]time.Time, key string) {
	values := attempts[key]
	if len(values) == 0 {
		return
	}
	if len(values) == 1 {
		delete(attempts, key)
		return
	}
	values[len(values)-1] = time.Time{}
	attempts[key] = values[:len(values)-1]
}

func newCounter(kind, id, identity string) storedCounter {
	return storedCounter{
		SchemaVersion:   domain.SchemaVersion,
		Kind:            "UpvoteCounter",
		SubjectKind:     kind,
		SubjectID:       id,
		SubjectIdentity: identity,
		VoterHashes:     []string{},
	}
}

func validCounter(counter storedCounter, kind, id string) bool {
	if counter.SchemaVersion != domain.SchemaVersion || counter.Kind != "UpvoteCounter" || counter.SubjectKind != kind || counter.SubjectID != id || counter.Count < 0 || counter.Count > maxVotersPerSubject || len(counter.VoterHashes) > maxVotersPerSubject || counter.Count != int64(len(counter.VoterHashes)) || !validDigest(counter.SubjectIdentity) || counter.UpdatedAt.IsZero() {
		return false
	}
	previous := ""
	for _, hash := range counter.VoterHashes {
		if !validDigest(hash) || hash <= previous {
			return false
		}
		previous = hash
	}
	return true
}

func appendVoter(counter *storedCounter, voterHash string) error {
	if counter.Count >= maxVotersPerSubject || len(counter.VoterHashes) >= maxVotersPerSubject {
		return ErrRateLimit
	}
	counter.Count++
	counter.VoterHashes = append(counter.VoterHashes, voterHash)
	sort.Strings(counter.VoterHashes)
	return nil
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
	return filepath.Join("upvotes", strings.ToLower(kind)+"s", id+".yaml")
}

func subjectIdentity(subject domain.Post) string {
	value := subject.Meta.Kind + "\x00" + subject.Meta.ID + "\x00" + subject.Meta.CreatedAt.UTC().Format(time.RFC3339Nano)
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func keyedHash(secret []byte, value string) string {
	hash := hmac.New(sha256.New, secret)
	_, _ = hash.Write([]byte(value))
	return hex.EncodeToString(hash.Sum(nil))
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func containsHash(values []string, target string) bool {
	index := sort.SearchStrings(values, target)
	return index < len(values) && hmac.Equal([]byte(values[index]), []byte(target))
}
