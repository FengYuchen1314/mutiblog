// Package render orchestrates the Node worker and atomically publishes static HTML.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"github.com/fengyuchen/mutiblog/internal/model"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Service struct {
	root, socket, output string
	command              []string
	cmd                  *exec.Cmd
	runningSocket        string
	mu                   sync.Mutex
	socketMu             sync.RWMutex
	client               *http.Client
	theme                map[string]any
	themeName, themeDir  string
	markdown             map[string]any
	baseURL              string
	defaultLocale        model.Locale
	localePrefixes       map[model.Locale]string
}

func New(root, socket, output string, command []string) *Service {
	s := &Service{root: root, socket: socket, output: output, command: command}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.Socket())
	}}
	s.client = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	return s
}
func (s *Service) Socket() string {
	s.socketMu.RLock()
	defer s.socketMu.RUnlock()
	return s.socket
}
func (s *Service) SetSocket(socket string) {
	s.socketMu.Lock()
	defer s.socketMu.Unlock()
	s.socket = socket
}
func (s *Service) Output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.output
}
func (s *Service) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cmd != nil
}
func (s *Service) SetOutput(output string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output = output
}
func (s *Service) Health(ctx context.Context) error {
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://unix/health", nil)
	if err != nil {
		return err
	}
	res, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("renderer health returned %s", res.Status)
	}
	return nil
}
func (s *Service) SetThemeSettings(values map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.theme = make(map[string]any, len(values))
	for key, value := range values {
		s.theme[key] = value
	}
}
func (s *Service) SetTheme(name, dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.themeName, s.themeDir = name, dir
}
func (s *Service) themeIdentity() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.themeName, s.themeDir
}

// SetMarkdownOptions sends the site-level Markdown switches to the only HTML
// renderer. Go deliberately never interprets Markdown itself.
func (s *Service) SetMarkdownOptions(katex, externalLinksNewTab, headingAnchors bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markdown = map[string]any{
		"katex":               katex,
		"externalLinksNewTab": externalLinksNewTab,
		"headingAnchors":      headingAnchors,
	}
}
func (s *Service) SetSiteOptions(baseURL string, defaultLocale model.Locale, prefixes map[model.Locale]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baseURL = strings.TrimRight(baseURL, "/")
	s.defaultLocale = defaultLocale
	s.localePrefixes = make(map[model.Locale]string, len(prefixes))
	for locale, prefix := range prefixes {
		s.localePrefixes[locale] = strings.Trim(prefix, "/")
	}
}
func (s *Service) themeSettings() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make(map[string]any, len(s.theme))
	for key, value := range s.theme {
		values[key] = value
	}
	return values
}
func (s *Service) markdownOptions() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	values := make(map[string]any, len(s.markdown))
	for key, value := range s.markdown {
		values[key] = value
	}
	return values
}
func (s *Service) localePrefix(locale model.Locale) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prefix := s.localePrefixes[locale]; prefix != "" {
		return prefix
	}
	return strings.ToLower(string(locale))
}
func (s *Service) siteURL(locale model.Locale, path ...string) string {
	s.mu.Lock()
	baseURL := s.baseURL
	s.mu.Unlock()
	parts := []string{"", s.localePrefix(locale)}
	parts = append(parts, path...)
	return strings.TrimRight(baseURL, "/") + strings.Join(parts, "/") + "/"
}
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		return nil
	}
	if len(s.command) == 0 {
		return fmt.Errorf("renderer command is empty")
	}
	args := append([]string(nil), s.command[1:]...)
	if len(args) > 0 && !filepath.IsAbs(args[0]) {
		candidate := filepath.Join(s.root, args[0])
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			candidate = filepath.Join(s.root, "frontend", "renderer", "server.js")
		}
		args[0] = candidate
	}
	socket := s.Socket()
	_ = os.Remove(socket)
	s.cmd = exec.CommandContext(ctx, s.command[0], args...)
	s.cmd.Dir = s.root
	s.cmd.Env = append(os.Environ(), "BLOG_RENDER_SOCKET="+socket)
	if err := s.cmd.Start(); err != nil {
		s.cmd = nil
		return err
	}
	s.runningSocket = socket
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/health", nil)
		res, err := s.client.Do(req)
		if err == nil && res.StatusCode == 200 {
			res.Body.Close()
			return nil
		}
		if res != nil {
			res.Body.Close()
		}
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
	_ = os.Remove(s.runningSocket)
	s.cmd = nil
	s.runningSocket = ""
	return fmt.Errorf("renderer worker did not become healthy")
}
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked()
}

// Restart replaces an unhealthy worker without changing its configured socket
// or output directory. It is used by the application watchdog; publishing
// requests will remain queued while the replacement warms up.
func (s *Service) Restart(ctx context.Context) error {
	if err := s.Close(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return s.Start(ctx)
}

// closeLocked gives a cooperative Node process five seconds to leave before
// forcing it down. A blocked event loop may not handle SIGINT, so waiting
// indefinitely here would also stall graceful application shutdown.
func (s *Service) closeLocked() error {
	if s.cmd == nil {
		return nil
	}
	cmd := s.cmd
	err := cmd.Process.Signal(os.Interrupt)
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case <-wait:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-wait
	}
	s.cmd = nil
	if s.runningSocket != "" {
		_ = os.Remove(s.runningSocket)
	}
	s.runningSocket = ""
	return err
}
func (s *Service) Render(ctx context.Context, article *model.Article, loc model.Locale) (string, error) {
	if article == nil || article.Versions[loc] == nil {
		return "", os.ErrNotExist
	}
	v := article.Versions[loc]
	segment := "posts"
	if article.Type == model.ContentPage {
		segment = ""
	}
	pathFor := func(locale model.Locale, slug string) string {
		if segment == "" {
			return s.siteURL(locale, slug)
		}
		return s.siteURL(locale, segment, slug)
	}
	locales := make([]model.Locale, 0, len(article.Versions))
	for locale := range article.Versions {
		locales = append(locales, locale)
	}
	sort.Slice(locales, func(i, j int) bool { return locales[i] < locales[j] })
	alternates := make([]map[string]string, 0, len(locales)+1)
	for _, locale := range locales {
		alternates = append(alternates, map[string]string{"hrefLang": string(locale), "href": pathFor(locale, article.Versions[locale].Front.Slug)})
	}
	if defaultVersion := article.Versions[s.defaultLocale]; defaultVersion != nil {
		alternates = append(alternates, map[string]string{"hrefLang": "x-default", "href": pathFor(s.defaultLocale, defaultVersion.Front.Slug)})
	}
	themeName, themeDir := s.themeIdentity()
	payload, _ := json.Marshal(map[string]any{"kind": string(article.Type), "title": v.Front.Title, "description": v.Front.Description, "body": v.Body, "locale": loc, "author": v.Front.Author, "publishedAt": v.Front.Date.UTC().Format(time.RFC3339), "modifiedAt": frontUpdated(v.Front), "theme": s.themeSettings(), "themeName": themeName, "themeDir": themeDir, "markdown": s.markdownOptions(), "canonical": pathFor(loc, v.Front.Slug), "alternates": alternates})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/render", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("renderer returned %s: %s", res.Status, string(body))
	}
	var response struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", err
	}
	prefix := s.localePrefix(loc)
	parts := []string{s.Output(), prefix}
	if segment != "" {
		parts = append(parts, segment)
	}
	parts = append(parts, v.Front.Slug, "index.html")
	path := filepath.Join(parts...)
	if err := fsutil.AtomicWrite(path, []byte(response.HTML), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes one previously published content unit. Collection pages are
// refreshed separately by the orchestrator; this method intentionally never
// guesses which taxonomy pages depend on the article.
func (s *Service) Remove(article *model.Article, loc model.Locale) error {
	if article == nil || article.Versions[loc] == nil {
		return os.ErrNotExist
	}
	segment := "posts"
	if article.Type == model.ContentPage {
		segment = ""
	}
	parts := []string{s.Output(), s.localePrefix(loc)}
	if segment != "" {
		parts = append(parts, segment)
	}
	parts = append(parts, article.Versions[loc].Front.Slug, "index.html")
	if err := os.Remove(filepath.Join(parts...)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func frontUpdated(front model.FrontMatter) string {
	if front.Updated == nil {
		return ""
	}
	return front.Updated.UTC().Format(time.RFC3339)
}

// Markdown renders a preview through the same Node pipeline used at publish
// time, so editor preview semantics cannot diverge from static output.
func (s *Service) Markdown(ctx context.Context, body string) (string, error) {
	payload, err := json.Marshal(map[string]any{"body": body, "markdown": s.markdownOptions()})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/markdown", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("renderer returned %s: %s", res.Status, string(body))
	}
	var response struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", err
	}
	return response.HTML, nil
}

type HomeItem struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
}

type PaginationLink struct {
	Page    int    `json:"page"`
	URL     string `json:"url"`
	Current bool   `json:"current"`
}
type Pagination struct {
	Page  int              `json:"page"`
	Total int              `json:"total"`
	Links []PaginationLink `json:"links"`
}

func (s *Service) RenderHome(ctx context.Context, locale model.Locale, title string, items []HomeItem) (string, error) {
	return s.RenderCollection(ctx, locale, title, items, "")
}

// RenderNotFound produces the locale-scoped static fallback consumed by a
// reverse proxy when a visitor requests an absent page. No application process
// is required on that read path.
func (s *Service) RenderNotFound(ctx context.Context, locale model.Locale, title, message, homeLabel string) (string, error) {
	prefix := s.localePrefix(locale)
	themeName, themeDir := s.themeIdentity()
	payload, _ := json.Marshal(map[string]any{"kind": "not_found", "title": title, "message": message, "homeLabel": homeLabel, "locale": locale, "prefix": prefix, "theme": s.themeSettings(), "themeName": themeName, "themeDir": themeDir, "canonical": strings.TrimRight(s.baseURL, "/") + "/" + prefix + "/404.html"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/render", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("renderer returned %s", res.Status)
	}
	var response struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", err
	}
	path := filepath.Join(s.Output(), prefix, "404.html")
	if err := fsutil.AtomicWrite(path, []byte(response.HTML), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RenderSearch writes a static search shell. Its small client-side island only
// fetches the already-published locale JSON index after a visitor types.
func (s *Service) RenderSearch(ctx context.Context, locale model.Locale, title string) (string, error) {
	prefix := s.localePrefix(locale)
	themeName, themeDir := s.themeIdentity()
	payload, _ := json.Marshal(map[string]any{"kind": "search", "title": title, "locale": locale, "prefix": prefix, "searchIndexURL": "/" + prefix + "/search-index.json", "theme": s.themeSettings(), "themeName": themeName, "themeDir": themeDir, "canonical": s.siteURL(locale, "search"), "alternates": s.collectionAlternates("search")})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/render", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("renderer returned %s", res.Status)
	}
	var response struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", err
	}
	path := filepath.Join(s.Output(), prefix, "search", "index.html")
	if err := fsutil.AtomicWrite(path, []byte(response.HTML), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RenderCollection produces any list-like static page (home, taxonomy,
// archive, or links) with the same deterministic Node template.
// relative is a locale-relative directory; an empty value means the locale home.
func (s *Service) RenderCollection(ctx context.Context, locale model.Locale, title string, items []HomeItem, relative string) (string, error) {
	return s.renderCollection(ctx, locale, title, items, relative, nil)
}

// RenderPaginatedCollection uses the same collection template while adding
// fully-static page navigation computed by Go from the canonical list order.
func (s *Service) RenderPaginatedCollection(ctx context.Context, locale model.Locale, title string, items []HomeItem, relative string, pagination Pagination) (string, error) {
	return s.renderCollection(ctx, locale, title, items, relative, &pagination)
}

func (s *Service) renderCollection(ctx context.Context, locale model.Locale, title string, items []HomeItem, relative string, pagination *Pagination) (string, error) {
	canonical := s.siteURL(locale)
	if relative != "" {
		canonical = s.siteURL(locale, strings.Trim(relative, "/"))
	}
	themeName, themeDir := s.themeIdentity()
	payload, _ := json.Marshal(map[string]any{"kind": "collection", "title": title, "locale": locale, "items": items, "pagination": pagination, "theme": s.themeSettings(), "themeName": themeName, "themeDir": themeDir, "canonical": canonical, "alternates": s.collectionAlternates(relative)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/render", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("renderer returned %s", res.Status)
	}
	var response struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", err
	}
	parts := []string{s.Output(), s.localePrefix(locale)}
	if relative != "" {
		parts = append(parts, filepath.FromSlash(relative))
	}
	parts = append(parts, "index.html")
	path := filepath.Join(parts...)
	if err := fsutil.AtomicWrite(path, []byte(response.HTML), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// collectionAlternates yields matching locale routes for pages whose URL is
// stable across locales (home, taxonomy indexes, archives, and search). The
// configured default is also exposed as x-default for crawlers.
func (s *Service) collectionAlternates(relative string) []map[string]string {
	s.mu.Lock()
	locales := make([]model.Locale, 0, len(s.localePrefixes))
	for locale := range s.localePrefixes {
		locales = append(locales, locale)
	}
	defaultLocale := s.defaultLocale
	s.mu.Unlock()
	sort.Slice(locales, func(i, j int) bool { return locales[i] < locales[j] })

	relative = strings.Trim(relative, "/")
	result := make([]map[string]string, 0, len(locales)+1)
	for _, locale := range locales {
		if relative == "" {
			result = append(result, map[string]string{"hrefLang": string(locale), "href": s.siteURL(locale)})
			continue
		}
		result = append(result, map[string]string{"hrefLang": string(locale), "href": s.siteURL(locale, relative)})
	}
	if relative == "" {
		result = append(result, map[string]string{"hrefLang": "x-default", "href": s.siteURL(defaultLocale)})
	} else {
		result = append(result, map[string]string{"hrefLang": "x-default", "href": s.siteURL(defaultLocale, relative)})
	}
	return result
}
