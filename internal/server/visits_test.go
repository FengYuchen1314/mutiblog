package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/comments"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/visits"
)

func TestPublicVisitAndStatsAPIsExposeOnlyPublicAggregates(t *testing.T) {
	app, repository, postID, draftID := testVisitServer(t)
	body, _ := json.Marshal(createVisitRequest{Kind: "Post", ID: postID})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/public/visits", bytes.NewReader(body))
	request.Host = "blog.example.com"
	request.RemoteAddr = "203.0.113.40:4100"
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("visit POST status = %d, body = %s", response.Code, response.Body.String())
	}
	var visit visits.Result
	if err := json.NewDecoder(response.Body).Decode(&visit); err != nil || visit.Count != 1 || !visit.Counted {
		t.Fatalf("visit response = %#v, %v", visit, err)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/public/visits", bytes.NewReader(body))
	request.Host = "blog.example.com"
	request.RemoteAddr = "203.0.113.40:4101"
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "1800" {
		t.Fatalf("duplicate visit status = %d, headers = %#v", response.Code, response.Header())
	}

	now := time.Now().UTC()
	for _, comment := range []domain.Comment{
		{SchemaVersion: 1, Kind: "Comment", ID: "approved-comment", Subject: domain.CommentSubject{Kind: "Post", ID: postID}, Status: "approved", Author: domain.CommentAuthor{Name: "Reader"}, Content: "Public", Locale: "en", CreatedAt: now},
		{SchemaVersion: 1, Kind: "Comment", ID: "pending-comment", Subject: domain.CommentSubject{Kind: "Post", ID: postID}, Status: "pending", Author: domain.CommentAuthor{Name: "Reader"}, Content: "Pending", Locale: "en", CreatedAt: now},
		{SchemaVersion: 1, Kind: "Comment", ID: "draft-comment", Subject: domain.CommentSubject{Kind: "Post", ID: draftID}, Status: "approved", Author: domain.CommentAuthor{Name: "Reader"}, Content: "Must not leak", Locale: "en", CreatedAt: now},
	} {
		if err := repository.WriteYAML("comments/"+comment.Subject.Kind+"/"+comment.Subject.ID+"/"+comment.ID+".yaml", comment, false); err != nil {
			t.Fatal(err)
		}
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/public/stats?locale=en", nil)
	request.Host = "blog.example.com"
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stats GET status = %d, body = %s", response.Code, response.Body.String())
	}
	var stats publicStatsResponse
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.Profile.Posts != 1 || stats.Profile.Visits != 1 || stats.Profile.Comments == nil || *stats.Profile.Comments != 1 || !stats.Profile.CommentsAvailable {
		t.Fatalf("profile stats = %#v", stats.Profile)
	}
	if len(stats.PopularPosts) != 1 || stats.PopularPosts[0].ID != postID || stats.PopularPosts[0].Title != "Public post" {
		t.Fatalf("popular posts = %#v", stats.PopularPosts)
	}
	encoded, _ := json.Marshal(stats)
	if strings.Contains(string(encoded), draftID) || strings.Contains(string(encoded), "Must not leak") {
		t.Fatalf("stats leaked draft content: %s", encoded)
	}
	late := domain.Comment{SchemaVersion: 1, Kind: "Comment", ID: "late-approved-comment", Subject: domain.CommentSubject{Kind: "Post", ID: postID}, Status: "approved", Author: domain.CommentAuthor{Name: "Reader"}, Content: "Late", Locale: "en", CreatedAt: now}
	if err := repository.WriteYAML("comments/Post/"+postID+"/"+late.ID+".yaml", late, false); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/public/stats?locale=en", nil)
	request.Host = "blog.example.com"
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	var cached publicStatsResponse
	if response.Code != http.StatusOK || json.NewDecoder(response.Body).Decode(&cached) != nil || cached.Profile.Comments == nil || *cached.Profile.Comments != 1 {
		t.Fatalf("short-TTL cached stats = %#v, status %d", cached, response.Code)
	}
}

func TestPublicStatsGracefullyDegradesWhenCommentsAreInvalid(t *testing.T) {
	app, repository, _, _ := testVisitServer(t)
	if err := repository.WriteFile("comments/Post/broken-subject/broken-comment.yaml", []byte("schemaVersion: [broken\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/stats?locale=en", nil)
	request.Host = "blog.example.com"
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stats GET status = %d, body = %s", response.Code, response.Body.String())
	}
	var stats publicStatsResponse
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if stats.Profile.Comments != nil || stats.Profile.CommentsAvailable || stats.Profile.Posts != 1 {
		t.Fatalf("degraded profile = %#v", stats.Profile)
	}
}

func TestPublicStatsDoesNotCacheVisitStateFailures(t *testing.T) {
	app, repository, postID, _ := testVisitServer(t)
	if _, err := app.visits.Record("Post", postID, "203.0.113.60"); err != nil {
		t.Fatal(err)
	}
	path := "visits/posts/" + postID + ".yaml"
	valid, err := repository.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile(path, []byte("schemaVersion: [broken\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/stats?locale=en", nil)
	request.Host = "blog.example.com"
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt stats status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := repository.WriteFile(path, valid, 0o640); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/public/stats?locale=en", nil)
	request.Host = "blog.example.com"
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("repaired stats status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestPublicVisitRejectsPreviewAndDraft(t *testing.T) {
	app, _, _, draftID := testVisitServer(t)
	body, _ := json.Marshal(createVisitRequest{Kind: "Post", ID: draftID})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/public/visits", bytes.NewReader(body))
	request.Host = "blog.example.com"
	request.RemoteAddr = "203.0.113.41:4100"
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("draft POST status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/public/visits", bytes.NewReader(body))
	request.Host = "preview.example.com"
	request.RemoteAddr = "203.0.113.41:4101"
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("preview POST status = %d, body = %s", response.Code, response.Body.String())
	}
}

func testVisitServer(t *testing.T) (*Server, *fsrepo.Repository, string, string) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{SchemaVersion: 1, SourceLocale: "zh-CN", BaseURL: "https://blog.example.com", Locales: map[string]domain.LocalizedSite{"zh-CN": {Title: "统计"}, "en": {Title: "Stats"}}}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: 1, SourceLocale: "zh-CN", Enabled: []domain.LocaleDefinition{{Code: "zh-CN", Label: "简体中文", Enabled: true, Status: domain.LocaleStatusReady}, {Code: "en", Label: "English", Enabled: true, Status: domain.LocaleStatusReady}}, Fallback: []string{"zh-CN"}}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: 1, Providers: map[string]string{}, CommentHMACKey: strings.Repeat("2", 64)}, true); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "public-stats-post", Title: "公开文章"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.ApplyAITranslation(post.Meta.ID, "en", content.ApplyAITranslationInput{
		ExpectedSourceRevision: post.Meta.Locales[post.Meta.SourceLocale].Revision,
		Content:                domain.LocalizedMarkdown{Title: "Public post"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := contentService.PromoteAITranslation("Post", post.Meta.ID, "en", post.Meta.Locales[post.Meta.SourceLocale].Revision); err != nil {
		t.Fatal(err)
	}
	draft, err := contentService.CreatePost(content.CreatePostInput{ID: "draft-stats-post", Title: "Draft post"})
	if err != nil {
		t.Fatal(err)
	}
	app := &Server{
		repository: repository,
		content:    contentService,
		comments:   comments.NewService(repository),
		visits:     visits.NewService(repository, contentService),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		mux:        http.NewServeMux(),
	}
	app.routes()
	return app, repository, post.Meta.ID, draft.Meta.ID
}
