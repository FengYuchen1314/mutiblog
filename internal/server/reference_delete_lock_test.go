package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
)

type deleteResult struct {
	response *http.Response
	err      error
}

func TestReferenceCheckingDeletesWaitForContentMutations(t *testing.T) {
	t.Run("taxonomy", func(t *testing.T) {
		app, repository, client, baseURL, csrf := newReferenceDeleteTestServer(t)
		category, err := app.taxonomies.Create("categories", taxonomy.CreateInput{ID: "news", Name: "News"})
		if err != nil {
			t.Fatal(err)
		}
		post, err := app.content.CreatePost(content.CreatePostInput{ID: "taxonomy-reference", Title: "Taxonomy reference"})
		if err != nil {
			t.Fatal(err)
		}
		request := newAdminDeleteRequest(t, baseURL+"/api/v1/admin/taxonomies/categories/news", map[string]int{"revision": category.Revision}, csrf)

		app.mutationGate.RLock()
		result := startDelete(client, request)
		assertDeleteBlocked(t, &app.mutationGate, result, func() {
			visibility := post.Meta.Visibility
			pinned := post.Meta.Pinned
			if _, err := app.content.UpdatePostSettings(post.Meta.ID, content.UpdatePostSettingsInput{
				ExpectedRevision: post.Meta.Revision,
				Categories:       []string{category.ID},
				Tags:             post.Meta.Tags,
				Pinned:           &pinned,
				Visibility:       &visibility,
				CommentPolicy:    post.Meta.CommentPolicy,
				Template:         post.Meta.Template,
			}); err != nil {
				t.Fatal(err)
			}
		}, app.mutationGate.RUnlock)
		response := awaitDelete(t, result)
		defer response.Body.Close()
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("taxonomy delete status = %d, body = %s", response.StatusCode, readBody(t, response))
		}
		if _, err := app.taxonomies.Get("categories", category.ID); err != nil {
			t.Fatalf("referenced taxonomy was deleted: %v", err)
		}
		if exists, err := repository.Exists("content/taxonomies/categories/news.yaml"); err != nil || !exists {
			t.Fatalf("taxonomy file exists = %t, err = %v", exists, err)
		}
	})

	t.Run("media", func(t *testing.T) {
		app, _, client, baseURL, csrf := newReferenceDeleteTestServer(t)
		post, err := app.content.CreatePost(content.CreatePostInput{ID: "media-reference", Title: "Media reference"})
		if err != nil {
			t.Fatal(err)
		}
		var imageData bytes.Buffer
		pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
		pixel.Set(0, 0, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
		if err := png.Encode(&imageData, pixel); err != nil {
			t.Fatal(err)
		}
		asset, err := app.media.Create("reference.png", bytes.NewReader(imageData.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		request := newAdminDeleteRequest(t, baseURL+"/api/v1/admin/attachments/"+asset.ID, nil, csrf)

		app.mutationGate.RLock()
		result := startDelete(client, request)
		assertDeleteBlocked(t, &app.mutationGate, result, func() {
			if _, err := app.content.UpdateLocale(post.Meta.ID, post.Meta.SourceLocale, content.UpdateLocaleInput{
				ExpectedRevision: post.Meta.Revision,
				Title:            "Media reference",
				Markdown:         "![reference](" + asset.URL + ")",
			}); err != nil {
				t.Fatal(err)
			}
		}, app.mutationGate.RUnlock)
		response := awaitDelete(t, result)
		defer response.Body.Close()
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("media delete status = %d, body = %s", response.StatusCode, readBody(t, response))
		}
		if _, err := app.media.Get(asset.ID); err != nil {
			t.Fatalf("referenced media was deleted: %v", err)
		}
	})
}

func newReferenceDeleteTestServer(t *testing.T) (*Server, *fsrepo.Repository, *http.Client, string, string) {
	t.Helper()
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, err := New(Options{Repository: repository, Version: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Publisher: fakeSitePublisher{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })
	testServer := httptest.NewServer(app.Handler())
	t.Cleanup(testServer.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response := requestJSON(t, client, http.MethodPost, testServer.URL+"/api/v1/setup", map[string]string{
		"siteTitle": "Reference locking", "baseUrl": testServer.URL, "sourceLocale": "zh-CN", "adminLocale": "zh-CN",
		"timezone": "UTC", "username": "admin", "password": "correct horse battery staple",
	}, nil)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("setup status = %d, body = %s", response.StatusCode, readBody(t, response))
	}
	response.Body.Close()
	response, err = client.Get(testServer.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var session struct {
		CSRFToken string `json:"csrfToken"`
	}
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	if session.CSRFToken == "" {
		t.Fatal("setup session did not provide a CSRF token")
	}
	// This test needs the DELETE request to be the only possible waiter for the
	// exclusive mutation gate. Stop the projection watcher and startup rebuild
	// so their background writers cannot satisfy the writer-pending handshake.
	app.cancel()
	app.startupWG.Wait()
	if tracked, ok := app.publisher.(*trackedSitePublisher); ok {
		tracked.setBeforeBuild(nil)
	}
	projectionService := app.projection
	app.projection = nil
	if err := projectionService.Close(); err != nil {
		t.Fatal(err)
	}
	return app, repository, client, testServer.URL, session.CSRFToken
}

func newAdminDeleteRequest(t *testing.T, url string, body any, csrf string) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(http.MethodDelete, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-CSRF-Token", csrf)
	return request
}

func startDelete(client *http.Client, request *http.Request) <-chan deleteResult {
	result := make(chan deleteResult, 1)
	go func() {
		response, err := client.Do(request)
		result <- deleteResult{response: response, err: err}
	}()
	return result
}

func assertDeleteBlocked(t *testing.T, gate *sync.RWMutex, result <-chan deleteResult, mutate func(), unlock func()) {
	t.Helper()
	defer unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case early := <-result:
			if early.response != nil {
				early.response.Body.Close()
			}
			t.Fatalf("reference-checking delete did not request the exclusive mutation gate: %v", early.err)
		default:
		}
		// The test already holds one read lock, so the writer cannot own the
		// gate. TryRLock fails only after the DELETE has queued for its write
		// lock; immediately release successful probe locks while it has not.
		if !gate.TryRLock() {
			break
		}
		gate.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("reference-checking delete did not reach the exclusive mutation gate")
		}
		runtime.Gosched()
	}
	mutate()
}

func awaitDelete(t *testing.T, result <-chan deleteResult) *http.Response {
	t.Helper()
	select {
	case completed := <-result:
		if completed.err != nil {
			t.Fatal(completed.err)
		}
		return completed.response
	case <-time.After(10 * time.Second):
		t.Fatal("reference-checking delete did not complete after the content mutation released the gate")
		return nil
	}
}
