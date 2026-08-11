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

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/upvotes"
)

func TestPublicUpvoteAPIUsesAnonymousCookieAndPersistsOneVote(t *testing.T) {
	app, repository, postID := testUpvoteServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/upvotes?kind=Post&id="+postID, nil)
	request.Host = "blog.example.com"
	request.RemoteAddr = "203.0.113.40:4100"
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != upvoteVisitorCookieName || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("visitor cookies = %#v", cookies)
	}
	var initial upvotes.Result
	if err := json.NewDecoder(response.Body).Decode(&initial); err != nil || initial.Count != 0 || initial.Upvoted {
		t.Fatalf("initial result = %#v, %v", initial, err)
	}

	body, _ := json.Marshal(createUpvoteRequest{Kind: "Post", ID: postID})
	request = httptest.NewRequest(http.MethodPost, "/api/v1/public/upvotes", bytes.NewReader(body))
	request.Host = "blog.example.com"
	request.RemoteAddr = "203.0.113.40:4101"
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, body = %s", response.Code, response.Body.String())
	}
	var created upvotes.Result
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil || created.Count != 1 || !created.Upvoted || !created.Created {
		t.Fatalf("created result = %#v, %v", created, err)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/public/upvotes", bytes.NewReader(body))
	request.Host = "blog.example.com"
	request.RemoteAddr = "203.0.113.40:4102"
	request.AddCookie(cookies[0])
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("duplicate POST status = %d, body = %s", response.Code, response.Body.String())
	}
	var duplicate upvotes.Result
	if err := json.NewDecoder(response.Body).Decode(&duplicate); err != nil || duplicate.Count != 1 || duplicate.Created || !duplicate.Upvoted {
		t.Fatalf("duplicate result = %#v, %v", duplicate, err)
	}
	data, err := repository.ReadFile("upvotes/posts/" + postID + ".yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "203.0.113.40") || strings.Contains(string(data), cookies[0].Value) {
		t.Fatalf("persisted upvote leaked visitor data: %s", data)
	}
}

func TestPublicUpvoteAPIRejectsPreviewAndInvalidInput(t *testing.T) {
	app, _, postID := testUpvoteServer(t)
	body, _ := json.Marshal(createUpvoteRequest{Kind: "Post", ID: postID})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/public/upvotes", bytes.NewReader(body))
	request.Host = "preview.example.com"
	request.RemoteAddr = "203.0.113.41:4100"
	response := httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("preview POST status = %d, body = %s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/public/upvotes", strings.NewReader(`{"kind":"Post","id":"Bad_ID"}`))
	request.Host = "blog.example.com"
	request.RemoteAddr = "203.0.113.41:4101"
	response = httptest.NewRecorder()
	app.mux.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid POST status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestUpvoteVisitorCookieTrustsForwardedHTTPSOnlyFromTrustedProxy(t *testing.T) {
	app, _, postID := testUpvoteServer(t)
	for _, test := range []struct {
		name       string
		remoteAddr string
		wantSecure bool
	}{
		{name: "trusted-proxy", remoteAddr: "127.0.0.1:4100", wantSecure: true},
		{name: "untrusted-direct-client", remoteAddr: "203.0.113.42:4100", wantSecure: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/public/upvotes?kind=Post&id="+postID, nil)
			request.Host = "blog.example.com"
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-Proto", "https")
			response := httptest.NewRecorder()
			app.mux.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("GET status = %d, body = %s", response.Code, response.Body.String())
			}
			cookies := response.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Secure != test.wantSecure {
				t.Fatalf("cookies = %#v, want secure %v", cookies, test.wantSecure)
			}
		})
	}
}

func testUpvoteServer(t *testing.T) (*Server, *fsrepo.Repository, string) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/site.yaml", domain.SiteConfig{SchemaVersion: domain.SchemaVersion, SourceLocale: "en", BaseURL: "https://blog.example.com", Locales: map[string]domain.LocalizedSite{"en": {Title: "Upvotes"}}}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/locales.yaml", domain.LocalesConfig{SchemaVersion: domain.SchemaVersion, SourceLocale: "en", Enabled: []domain.LocaleDefinition{{Code: "en", Label: "English", Enabled: true}}, Fallback: []string{"en"}}, false); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("config/secrets.yaml", domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}, CommentHMACKey: strings.Repeat("2", 64)}, true); err != nil {
		t.Fatal(err)
	}
	contentService := content.NewService(repository)
	post, err := contentService.CreatePost(content.CreatePostInput{ID: "api-upvote-post", Title: "API upvote"})
	if err != nil {
		t.Fatal(err)
	}
	post, err = contentService.PublishPost(post.Meta.ID, post.Meta.Revision)
	if err != nil {
		t.Fatal(err)
	}
	app := &Server{
		repository: repository,
		content:    contentService,
		upvotes:    upvotes.NewService(repository, contentService),
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		mux:        http.NewServeMux(),
	}
	app.routes()
	return app, repository, post.Meta.ID
}
