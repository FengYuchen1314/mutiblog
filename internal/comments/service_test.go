package comments

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

func TestCommentRateLimiterHasHardClientBound(t *testing.T) {
	service := &Service{recent: make(map[string][]time.Time)}
	now := time.Now()
	for index := 0; index < recentClientLimit; index++ {
		service.recent[fmt.Sprintf("client-%d", index)] = []time.Time{now}
	}
	if service.allow("one-client-too-many") {
		t.Fatal("high-cardinality client was accepted beyond the hard bound")
	}
	if len(service.recent) != recentClientLimit {
		t.Fatalf("recent clients = %d, want %d", len(service.recent), recentClientLimit)
	}
	service.recent["client-0"] = []time.Time{now.Add(-11 * time.Minute)}
	if !service.allow("replacement-client") {
		t.Fatal("expired client slot was not reclaimed")
	}
	if len(service.recent) != recentClientLimit {
		t.Fatalf("recent clients after reclaim = %d, want %d", len(service.recent), recentClientLimit)
	}
}

func TestCommentPrivacyModerationAndFileTruth(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locales := domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Enabled: true}}}
	if err := repository.WriteYAML("config/locales.yaml", locales, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/comments.yaml", domain.CommentsConfig{SchemaVersion: 1, Moderation: "pending", PageSize: 20, MaxLength: 2000}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: 1, Providers: map[string]string{}, CommentHMACKey: "test-comment-secret"}, true); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "comment-post", Title: "评论测试"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	head, err := contentService.UpdatePostSettings(post.Meta.ID, content.UpdatePostSettingsInput{ExpectedRevision: post.Meta.Revision, CommentPolicy: "closed", Template: "post"})
	if err != nil {
		t.Fatal(err)
	}
	comment, err := service.Create(CreateInput{SubjectKind: "Post", SubjectID: post.Meta.ID, Name: "访客", Email: "visitor@example.com", Website: "https://example.com", Content: "很好。", Locale: "zh-CN", IPAddress: "203.0.113.10", UserAgent: "Mozilla/5.0 Chrome/120"})
	if err != nil {
		t.Fatalf("unpublished closed policy changed the public release: %v", err)
	}
	post, err = contentService.PublishPost(head.Meta.ID, head.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(CreateInput{SubjectKind: "Post", SubjectID: post.Meta.ID, Name: "访客二", Content: "第二条。", Locale: "zh-CN", IPAddress: "203.0.113.11"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("published closed policy accepted a comment: %v", err)
	}
	if comment.Status != "pending" || comment.Author.EmailHash == "" || comment.IPHash == "" || comment.UserAgentFamily != "Chrome" {
		t.Fatalf("comment = %#v", comment)
	}
	data, err := repository.ReadFile(commentPath("Post", post.Meta.ID, comment.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"visitor@example.com", "203.0.113.10", "Mozilla/5.0"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("comment file leaked %q: %s", secret, data)
		}
	}
	if items, total, err := service.ListPublic("Post", post.Meta.ID, 1, 20); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("pending public list = %#v, %d, %v", items, total, err)
	}
	comment, err = service.Moderate("Post", post.Meta.ID, comment.ID, "approved")
	if err != nil {
		t.Fatal(err)
	}
	if items, total, err := service.ListPublic("Post", post.Meta.ID, 1, 20); err != nil || total != 1 || len(items) != 1 || items[0].Content != "很好。" {
		t.Fatalf("approved public list = %#v, %d, %v", items, total, err)
	}
	post, err = contentService.ChangeStatus("Post", post.Meta.ID, "unpublish", post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ListPublic("Post", post.Meta.ID, 1, 20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unpublished subject exposed comments: %v", err)
	}
	if all, err := service.ListAll(); err != nil || len(all) != 1 {
		t.Fatalf("all comments = %#v, %v", all, err)
	}
	if items, total, err := service.ListAdmin(AdminQuery{Status: "approved", Query: "访客", Page: 1, Size: 1}); err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("filtered admin comments = %#v, %d, %v", items, total, err)
	}
	if items, total, err := service.ListAdmin(AdminQuery{Status: "spam", Page: 1, Size: 20}); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("empty admin filter = %#v, %d, %v", items, total, err)
	}
	if err := service.Delete("Post", post.Meta.ID, comment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get("Post", post.Meta.ID, comment.ID); err != ErrNotFound {
		t.Fatalf("deleted comment error = %v", err)
	}
}

func TestCommentIdentityMismatchCannotDeleteAnotherSubject(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	commentID := "comment-id"
	victim := domain.Comment{
		SchemaVersion: domain.SchemaVersion,
		Kind:          "Comment",
		ID:            commentID,
		Subject:       domain.CommentSubject{Kind: "Post", ID: "second-post"},
		Status:        "approved",
	}
	if err := repository.WriteYAML(commentPath("Post", "second-post", commentID), victim, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML(commentPath("Post", "first-post", commentID), victim, false); err != nil {
		t.Fatal(err)
	}

	service := NewService(repository)
	if _, err := service.ListAll(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched list error = %v, want ErrInvalid", err)
	}
	if err := service.Delete("Post", "first-post", commentID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("mismatched deletion error = %v, want ErrInvalid", err)
	}
	if _, err := os.Stat(filepath.Join(repository.Root(), commentPath("Post", "second-post", commentID))); err != nil {
		t.Fatalf("victim comment was removed: %v", err)
	}
}

func TestPaginationRejectsOverflowingPageWithoutPanicking(t *testing.T) {
	items := []domain.Comment{{ID: "first"}}
	page := int(^uint(0) >> 1)
	if got := paginate(items, page, 100); len(got) != 0 {
		t.Fatalf("overflowing page returned %#v", got)
	}
}
