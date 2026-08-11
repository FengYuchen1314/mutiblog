package upvotes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestUpvotesPersistCountDeduplicateAndDoNotStoreVisitorData(t *testing.T) {
	service, repository, postID := testUpvoteService(t)
	firstToken := strings.Repeat("a", 64)
	secondToken := strings.Repeat("b", 64)

	initial, err := service.Get("Post", postID, firstToken)
	if err != nil || initial.Count != 0 || initial.Upvoted {
		t.Fatalf("initial result = %#v, %v", initial, err)
	}
	created, err := service.Add("Post", postID, firstToken, "203.0.113.10")
	if err != nil || !created.Created || !created.Upvoted || created.Count != 1 {
		t.Fatalf("created result = %#v, %v", created, err)
	}
	duplicate, err := service.Add("posts", postID, firstToken, "203.0.113.10")
	if err != nil || duplicate.Created || !duplicate.Upvoted || duplicate.Count != 1 {
		t.Fatalf("duplicate result = %#v, %v", duplicate, err)
	}
	second, err := service.Add("Post", postID, secondToken, "203.0.113.11")
	if err != nil || !second.Created || second.Count != 2 {
		t.Fatalf("second result = %#v, %v", second, err)
	}

	reopened := NewService(repository, content.NewService(repository))
	result, err := reopened.Get("Post", postID, firstToken)
	if err != nil || result.Count != 2 || !result.Upvoted {
		t.Fatalf("reopened result = %#v, %v", result, err)
	}
	data, err := repository.ReadFile(counterPath("Post", postID))
	if err != nil {
		t.Fatal(err)
	}
	for _, privateValue := range []string{"203.0.113.10", firstToken, secondToken} {
		if strings.Contains(string(data), privateValue) {
			t.Fatalf("counter file leaked %q: %s", privateValue, data)
		}
	}
	if err := reopened.ValidateAll(); err != nil {
		t.Fatalf("ValidateAll() = %v", err)
	}
}

func TestUpvotesRejectInvalidOrUnpublishedSubjectsAndRateLimitByHashedAddress(t *testing.T) {
	service, repository, postID := testUpvoteService(t)
	contentService := content.NewService(repository)
	draft, err := contentService.CreatePost(content.CreatePostInput{ID: "draft-upvote", Title: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	validToken := strings.Repeat("c", 64)
	if _, err := service.Add("Post", draft.Meta.ID, validToken, "203.0.113.20"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft Add() error = %v", err)
	}
	if _, err := service.Get("Unknown", postID, validToken); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown kind error = %v", err)
	}
	if _, err := service.Add("Post", postID, "not-a-token", "203.0.113.20"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid token error = %v", err)
	}
	if _, err := service.Add("Post", postID, validToken, "not-an-ip"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid address error = %v", err)
	}

	for index := 0; index < rateLimitCount; index++ {
		token := fmt.Sprintf("%064x", index+1)
		if _, err := service.Add("Post", postID, token, "203.0.113.99"); err != nil {
			t.Fatalf("rate-limited before attempt %d: %v", index+1, err)
		}
	}
	if _, err := service.Add("Post", postID, strings.Repeat("d", 64), "203.0.113.99"); !errors.Is(err, ErrRateLimit) {
		t.Fatalf("rate limit error = %v", err)
	}
}

func TestUpvotesResetRuntimeCountWhenStoredContentIdentityChanges(t *testing.T) {
	service, repository, postID := testUpvoteService(t)
	token := strings.Repeat("e", 64)
	if _, err := service.Add("Post", postID, token, "203.0.113.30"); err != nil {
		t.Fatal(err)
	}
	var counter storedCounter
	if err := repository.ReadYAML(counterPath("Post", postID), &counter); err != nil {
		t.Fatal(err)
	}
	counter.SubjectIdentity = strings.Repeat("f", 64)
	if err := repository.WriteYAML(counterPath("Post", postID), counter, false); err != nil {
		t.Fatal(err)
	}
	result, err := service.Get("Post", postID, token)
	if err != nil || result.Count != 0 || result.Upvoted {
		t.Fatalf("identity-mismatched result = %#v, %v", result, err)
	}
	if err := service.ValidateAll(); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("ValidateAll() error = %v", err)
	}
}

func TestUpvoteRateLimiterHasStrictHighCardinalityBound(t *testing.T) {
	service := &Service{attempts: make(map[string][]time.Time)}
	now := time.Date(2026, time.August, 11, 0, 0, 0, 0, time.UTC)
	for index := 0; index < maxRateLimitKeys; index++ {
		if !service.allowLocked(fmt.Sprintf("address-%d", index), now) {
			t.Fatalf("key %d rejected before hard limit", index)
		}
	}
	if service.allowLocked("overflow-address", now) {
		t.Fatal("new high-cardinality key was accepted past the hard limit")
	}
	if len(service.attempts) != maxRateLimitKeys {
		t.Fatalf("attempt keys = %d, want %d", len(service.attempts), maxRateLimitKeys)
	}
	if !service.allowLocked("replacement-address", now.Add(rateLimitWindow+time.Second)) {
		t.Fatal("expired keys were not reclaimed")
	}
	if len(service.attempts) > maxRateLimitKeys {
		t.Fatalf("attempt keys grew past hard limit: %d", len(service.attempts))
	}
}

func TestUpvoteWriteFailureDoesNotCommitVoteInMemory(t *testing.T) {
	service, repository, postID := testUpvoteService(t)
	token := strings.Repeat("9", 64)
	postsDirectory := filepath.Join(repository.Root(), "upvotes", "posts")
	if err := os.RemoveAll(postsDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(postsDirectory, []byte("blocks counter directory"), 0o640); err != nil {
		t.Fatal(err)
	}
	if result, err := service.Add("Post", postID, token, "203.0.113.80"); err == nil || result.Created {
		t.Fatalf("failed write result = %#v, %v", result, err)
	}
	if err := os.Remove(postsDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(postsDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	result, err := service.Add("Post", postID, token, "203.0.113.80")
	if err != nil || !result.Created || result.Count != 1 {
		t.Fatalf("retry result = %#v, %v", result, err)
	}
}

func TestUpvoteCounterRejectsPerSubjectVoterOverflow(t *testing.T) {
	counter := storedCounter{Count: maxVotersPerSubject}
	if err := appendVoter(&counter, strings.Repeat("a", 64)); !errors.Is(err, ErrRateLimit) {
		t.Fatalf("appendVoter() error = %v", err)
	}
	overflow := newCounter("Post", "upvote-post", strings.Repeat("b", 64))
	overflow.UpdatedAt = time.Now().UTC()
	overflow.Count = maxVotersPerSubject + 1
	if validCounter(overflow, "Post", "upvote-post") {
		t.Fatal("oversized persisted counter was accepted")
	}
}

func TestPermanentContentDeletionRemovesUpvoteCounter(t *testing.T) {
	service, repository, postID := testUpvoteService(t)
	if _, err := service.Add("Post", postID, strings.Repeat("8", 64), "203.0.113.81"); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	head, err := contentService.GetPost(postID)
	if err != nil {
		t.Fatal(err)
	}
	recycled, err := contentService.ChangeStatus("Post", postID, "recycle", head.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := contentService.DeleteRecycled("Post", postID, recycled.Meta.Revision); err != nil {
		t.Fatal(err)
	}
	if exists, err := repository.Exists(counterPath("Post", postID)); err != nil || exists {
		t.Fatalf("deleted counter exists = %v, %v", exists, err)
	}
}

func testUpvoteService(t *testing.T) (*Service, *fsrepo.Repository, string) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  "en",
		Enabled:       []domain.LocaleDefinition{{Code: "en", Label: "English", Enabled: true}},
		Fallback:      []string{"en"},
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}, CommentHMACKey: strings.Repeat("1", 64)}, true); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "upvote-post", Title: "Upvote post", Markdown: "Body"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(repository, contentService), repository, post.Meta.ID
}
