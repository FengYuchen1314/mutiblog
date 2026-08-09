// Package app assembles process-lifetime infrastructure without exposing HTTP policy.
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/fengyuchen/mutiblog/internal/ai"
	"github.com/fengyuchen/mutiblog/internal/auth"
	"github.com/fengyuchen/mutiblog/internal/backup"
	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/events"
	"github.com/fengyuchen/mutiblog/internal/feed"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"github.com/fengyuchen/mutiblog/internal/httpserver"
	blogi18n "github.com/fengyuchen/mutiblog/internal/i18n"
	"github.com/fengyuchen/mutiblog/internal/index"
	"github.com/fengyuchen/mutiblog/internal/jobs"
	"github.com/fengyuchen/mutiblog/internal/media"
	"github.com/fengyuchen/mutiblog/internal/migrate"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/render"
	"github.com/fengyuchen/mutiblog/internal/state"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
	"github.com/fengyuchen/mutiblog/internal/theme"
	"github.com/fengyuchen/mutiblog/internal/watcher"
)

type Options struct {
	Root, ConfigFile string
	Dev              bool
	Logger           *slog.Logger
}
type App struct {
	Root, ConfigFile string
	Config           *config.Config
	State            *state.DB
	Content          *content.Store
	Taxonomy         *taxonomy.Store
	Index            *index.Index
	Events           *events.Bus
	Watcher          *watcher.Watcher
	Users            *auth.Users
	Media            *media.LocalStorage
	Render           *render.Service
	Jobs             *jobs.Queue
	AI               *ai.Service
	Theme            *theme.Store
	Backup           backup.Options
	logger           *slog.Logger
	server           *http.Server
}

// releaseFingerprint is deliberately small and fully derived from source
// configuration. It lets startup distinguish a valid static release from one
// that was rendered for another public URL, theme, or locale set.
type releaseFingerprint struct {
	BaseURL           string   `json:"baseURL"`
	Theme             string   `json:"theme"`
	ThemeSettingsHash string   `json:"themeSettingsHash"`
	Locales           []string `json:"locales"`
}

func New(opts Options) (*App, []string, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if err := ensureSessionSecret(opts.Root); err != nil {
		return nil, nil, err
	}
	cfg, warnings, err := config.Load(opts.Root, opts.ConfigFile)
	if err != nil {
		return nil, warnings, err
	}
	if cfg.SchemaVersion > migrate.CurrentContentSchema {
		return nil, warnings, fmt.Errorf("content schema %d is newer than this binary (supports %d); upgrade the application or restore a backup", cfg.SchemaVersion, migrate.CurrentContentSchema)
	}
	if cfg.SchemaVersion < migrate.CurrentContentSchema {
		warnings = append(warnings, fmt.Sprintf("content schema %d is older than supported schema %d; run blog migrate before editing content", cfg.SchemaVersion, migrate.CurrentContentSchema))
	}
	for _, path := range []string{cfg.Paths.Content, cfg.Paths.Data, cfg.Paths.Media, cfg.Paths.Generated, cfg.Paths.Cache, cfg.Paths.Themes} {
		if err := fsutil.EnsureDir(path, 0o755); err != nil {
			return nil, warnings, err
		}
	}
	db, err := state.Open(filepath.Join(cfg.Paths.Data, "state.db"))
	if err != nil && !os.IsNotExist(err) {
		if db == nil {
			return nil, warnings, err
		}
		warnings = append(warnings, err.Error())
	}
	contentStore := content.NewStore(cfg.Paths.Content)
	users, err := auth.OpenUsers(cfg.Paths.Data)
	if err != nil {
		_ = db.Close()
		return nil, warnings, err
	}
	taxonomyStore := taxonomy.NewStore(cfg.Paths.Data)
	ix := index.New(index.Options{ContentRoot: cfg.Paths.Content, BodyResidentLimitBytes: int64(cfg.Index.BodyResidentLimitMB) << 20, ForceMode: cfg.Index.ForceMode})
	if err := ix.RebuildAll(context.Background(), contentStore, taxonomyStore); err != nil {
		_ = db.Close()
		return nil, warnings, err
	}
	bus := events.New(opts.Logger)
	w, err := watcher.New([]string{cfg.Paths.Content, cfg.Paths.Data, filepath.Join(opts.Root, "config"), cfg.Paths.Themes}, bus)
	if err != nil {
		_ = db.Close()
		return nil, warnings, err
	}
	mediaStore := media.NewLocal(cfg.Paths.Media, cfg.Storage.Local.PublicPrefix)
	themeStore := theme.NewStore(cfg.Paths.Themes, cfg.Paths.Data)
	if active, themeErr := themeStore.Get(cfg.Theme.Active); themeErr != nil || theme.Compatible(active.Manifest, theme.EngineVersion) != nil {
		reason := "unknown error"
		if themeErr != nil {
			reason = themeErr.Error()
		} else if compatibilityErr := theme.Compatible(active.Manifest, theme.EngineVersion); compatibilityErr != nil {
			reason = compatibilityErr.Error()
		}
		if cfg.Theme.Active != "default" {
			warnings = append(warnings, "active theme unavailable; falling back to default: "+reason)
			cfg.Theme.Active = "default"
		}
	}
	output := cfg.Render.Output
	if !filepath.IsAbs(output) {
		output = filepath.Join(opts.Root, output)
	}
	renderer := render.New(opts.Root, cfg.Render.WorkerSocket, output, cfg.Render.WorkerCommand)
	renderer.SetMarkdownOptions(cfg.Markdown.Katex, cfg.Markdown.ExternalLinksNewTab, cfg.Markdown.HeadingAnchors)
	prefixes := make(map[model.Locale]string, len(cfg.I18n.Locales))
	for _, locale := range cfg.I18n.Locales {
		if locale.Enabled {
			prefixes[model.Locale(locale.Code)] = locale.URLPrefix
		}
	}
	renderer.SetSiteOptions(cfg.Server.BaseURL, model.Locale(cfg.I18n.DefaultLocale), prefixes)
	renderer.SetTheme(cfg.Theme.Active, filepath.Join(cfg.Paths.Themes, cfg.Theme.Active))
	if values, err := themeStore.Settings(cfg.Theme.Active); err == nil {
		renderer.SetThemeSettings(values)
	} else {
		warnings = append(warnings, "theme settings: "+err.Error())
	}
	queue := jobs.New(db)
	enqueueRender := func(articleID model.ArticleID, loc model.Locale) {
		payload, _ := json.Marshal(map[string]string{"articleID": string(articleID), "locale": string(loc)})
		if _, err := queue.Enqueue(context.Background(), jobs.Job{Kind: "render", DedupeKey: string(articleID) + ":" + string(loc), Payload: payload, Priority: 10}); err != nil {
			opts.Logger.Warn("external change render was not queued", "article", articleID, "locale", loc, "err", err)
		}
	}
	enqueueLocaleRefresh := func(loc model.Locale) {
		payload, _ := json.Marshal(map[string]string{"locale": string(loc)})
		if _, err := queue.Enqueue(context.Background(), jobs.Job{Kind: "render", DedupeKey: "locale:" + string(loc), Payload: payload, Priority: 30}); err != nil {
			opts.Logger.Warn("locale refresh was not queued", "locale", loc, "err", err)
		}
	}
	// External edits (vim, git pull) refresh the index and the published HTML,
	// but must never auto-trigger AI translation (P29).
	bus.Subscribe("BundleChanged", func(_ context.Context, e events.Event) error {
		change := e.(events.BundleChanged)
		before, _ := ix.ArticleByDir(change.Dir)
		article, err := contentStore.LoadBundle(change.Dir)
		if err != nil {
			if before != nil {
				ix.RemoveBundle(change.Dir)
				for _, loc := range publishedLocales(before) {
					if removeErr := renderer.Remove(before, loc); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
						opts.Logger.Warn("removing externally deleted article failed", "article", before.ID, "locale", loc, "err", removeErr)
					}
					enqueueLocaleRefresh(loc)
				}
			}
			return nil
		}
		ix.UpsertArticle(article)
		for _, loc := range publishedLocales(article) {
			enqueueRender(article.ID, loc)
		}
		return nil
	})
	bus.Subscribe("BundleRemoved", func(_ context.Context, e events.Event) error {
		change := e.(events.BundleRemoved)
		before, _ := ix.ArticleByDir(change.Dir)
		ix.RemoveBundle(change.Dir)
		if before != nil {
			for _, loc := range publishedLocales(before) {
				if removeErr := renderer.Remove(before, loc); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					opts.Logger.Warn("removing externally deleted article failed", "article", before.ID, "locale", loc, "err", removeErr)
				}
				enqueueLocaleRefresh(loc)
			}
		} else {
			for _, locale := range cfg.I18n.Locales {
				if locale.Enabled {
					enqueueLocaleRefresh(model.Locale(locale.Code))
				}
			}
		}
		return nil
	})
	bus.Subscribe("TaxonomyChanged", func(_ context.Context, e events.Event) error {
		if err := ix.ReloadTaxonomy(taxonomyStore); err != nil {
			return err
		}
		for _, locale := range cfg.I18n.Locales {
			if locale.Enabled {
				enqueueLocaleRefresh(model.Locale(locale.Code))
			}
		}
		return nil
	})
	var translator *ai.Service
	if cfg.AI.Enabled {
		provider, err := ai.NewOpenAICompatible(cfg.AI)
		if err != nil {
			_ = db.Close()
			return nil, warnings, err
		}
		translator = &ai.Service{Provider: provider, Store: contentStore, Index: ix, Jobs: queue, DB: db, Budget: cfg.AI.SegmentBudget}
	}
	backupOptions := backup.Options{Root: opts.Root, Content: cfg.Paths.Content, Data: cfg.Paths.Data, Config: filepath.Join(opts.Root, "config"), Media: cfg.Paths.Media, OutputDir: cfg.Backup.Dir, IncludeMedia: cfg.Backup.IncludeMedia, Keep: cfg.Backup.Keep}
	return &App{Root: opts.Root, ConfigFile: opts.ConfigFile, Config: cfg, State: db, Content: contentStore, Taxonomy: taxonomyStore, Index: ix, Events: bus, Watcher: w, Users: users, Media: mediaStore, Render: renderer, Jobs: queue, AI: translator, Theme: themeStore, Backup: backupOptions, logger: opts.Logger}, warnings, nil
}
func (a *App) Serve(ctx context.Context) error {
	if a.Config.Render.WorkerEnabled {
		if err := a.Render.Start(ctx); err != nil {
			return err
		}
		if reason, needed := a.releaseNeedsRebuild(); needed {
			a.logger.Info("static release needs rebuilding", "reason", reason)
			if err := a.Rebuild(ctx); err != nil {
				_ = a.Render.Close()
				return fmt.Errorf("initial static rebuild: %w", err)
			}
		}
	}
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	go a.consumeRenders(watchCtx)
	go a.watchJobs(watchCtx)
	go a.consumeScheduledPublishes(watchCtx)
	if a.AI != nil {
		go a.consumeTranslations(watchCtx)
	}
	a.server = &http.Server{Addr: fmt.Sprintf("%s:%d", a.Config.Server.Host, a.Config.Server.Port), Handler: httpserver.New(&httpserver.Server{Root: a.Root, ConfigFile: a.ConfigFile, Config: a.Config, State: a.State, Users: a.Users, Index: a.Index, Content: a.Content, Events: a.Events, Media: a.Media, Taxonomy: a.Taxonomy, Render: a.Render, Jobs: a.Jobs, AI: a.AI, Theme: a.Theme, AppVersion: theme.EngineVersion, Backup: a.Backup}), ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		if err := a.Watcher.Start(watchCtx); err != nil {
			a.logger.Error("watcher stopped", "err", err)
		}
	}()
	go func() { errCh <- a.server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), a.Config.Server.GracefulTimeout.Duration())
		defer cancel()
		err := a.server.Shutdown(shutCtx)
		_ = a.Watcher.Close()
		_ = a.Render.Close()
		_ = a.State.Close()
		return err
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
func (a *App) rebuildPublished(ctx context.Context) error {
	locales := map[model.Locale]bool{}
	// A first-run site has no published article yet, but it must still publish
	// locale homes, feeds, sitemap, and robots.txt. Enabled configuration is
	// therefore the source of truth for the site-level render set.
	for _, configured := range a.Config.I18n.Locales {
		if configured.Enabled {
			locales[model.Locale(configured.Code)] = true
		}
	}
	for _, article := range a.Index.Articles() {
		for locale, version := range article.Versions {
			if version.Front.Status != model.StatusPublished {
				continue
			}
			if _, err := a.Render.Render(ctx, article, locale); err != nil {
				return err
			}
			locales[locale] = true
		}
	}
	for locale := range locales {
		if err := a.renderLocale(ctx, locale); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) currentReleaseFingerprint() releaseFingerprint {
	locales := make([]string, 0, len(a.Config.I18n.Locales))
	for _, locale := range a.Config.I18n.Locales {
		if locale.Enabled {
			locales = append(locales, locale.Code)
		}
	}
	sort.Strings(locales)
	settings := map[string]any{}
	if a.Theme != nil {
		if loaded, err := a.Theme.Settings(a.Config.Theme.Active); err == nil {
			settings = loaded
		}
	}
	encoded, _ := json.Marshal(settings)
	return releaseFingerprint{
		BaseURL:           strings.TrimRight(a.Config.Server.BaseURL, "/"),
		Theme:             a.Config.Theme.Active,
		ThemeSettingsHash: fmt.Sprintf("%x", sha256.Sum256(encoded)),
		Locales:           locales,
	}
}

func (a *App) releaseNeedsRebuild() (string, bool) {
	path := filepath.Join(a.Render.Output(), ".release.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "no published release", true
	}
	if err != nil {
		return "release fingerprint could not be read", true
	}
	var current releaseFingerprint
	if err := json.Unmarshal(data, &current); err != nil {
		return "release fingerprint is invalid", true
	}
	if !reflect.DeepEqual(current, a.currentReleaseFingerprint()) {
		return "site configuration changed", true
	}
	return "", false
}

// Rebuild is the offline-safe CLI entry point. It starts the local renderer
// only for the duration of rebuilding and writes no runtime HTTP state.
func (a *App) Rebuild(ctx context.Context) error {
	if !a.Config.Render.WorkerEnabled {
		return errors.New("render.workerEnabled must be true for rebuild")
	}
	if !a.Render.Running() {
		previousSocket := a.Render.Socket()
		isolatedSocket := filepath.Join(os.TempDir(), fmt.Sprintf("mutiblog-rebuild-%d.sock", os.Getpid()))
		a.Render.SetSocket(isolatedSocket)
		defer a.Render.SetSocket(previousSocket)
		if err := a.Render.Start(ctx); err != nil {
			return err
		}
	}
	output := a.Render.Output()
	releases := releaseRoot(output)
	if err := os.MkdirAll(releases, 0o755); err != nil {
		return err
	}
	stamp := fmt.Sprint(time.Now().UnixNano())
	staging := filepath.Join(releases, "release-"+stamp)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	a.Render.SetOutput(staging)
	err := a.rebuildPublished(ctx)
	a.Render.SetOutput(output)
	if err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	meta, _ := json.MarshalIndent(a.currentReleaseFingerprint(), "", "  ")
	if err := fsutil.AtomicWrite(filepath.Join(staging, ".release.json"), append(meta, '\n'), 0o644); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := activateRelease(output, staging, releases, stamp); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	pruneReleases(releases, a.Config.Render.KeepReleases)
	return nil
}

func releaseRoot(output string) string {
	if filepath.Base(output) == "public" {
		return filepath.Join(filepath.Dir(output), "releases")
	}
	return output + ".releases"
}

func activateRelease(output, staging, releases, stamp string) error {
	if info, err := os.Lstat(output); err == nil && info.Mode()&os.ModeSymlink == 0 {
		legacy := filepath.Join(releases, "legacy-"+stamp)
		if err := os.Rename(output, legacy); err != nil {
			return fmt.Errorf("preserve previous static output: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	relative, err := filepath.Rel(filepath.Dir(output), staging)
	if err != nil {
		return err
	}
	temporary := output + ".next-" + stamp
	if err := os.Symlink(relative, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, output); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("activate static release: %w", err)
	}
	return nil
}

func pruneReleases(root string, keep int) {
	if keep < 1 {
		keep = 1
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "release-") {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) <= keep {
		return
	}
	for _, name := range names[keep:] {
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}

// RebuildTo is used by verify to produce a complete, isolated candidate tree
// without touching the currently served output.
func (a *App) RebuildTo(ctx context.Context, output string) error {
	previous := a.Render.Output()
	a.Render.SetOutput(output)
	defer a.Render.SetOutput(previous)
	return a.Rebuild(ctx)
}
func (a *App) consumeTranslations(ctx context.Context) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		job, err := a.Jobs.Claim(ctx, "translate", "translate-1")
		if err != nil || job == nil {
			continue
		}
		stopHeartbeat := a.jobHeartbeat(ctx, job.ID, "translate-1")
		if err := a.AI.Run(ctx, job.Payload); err != nil {
			stopHeartbeat()
			_ = a.Jobs.Fail(ctx, job.ID, err)
			continue
		}
		stopHeartbeat()
		_ = a.Jobs.Complete(ctx, job.ID)
	}
}
func (a *App) Close() error {
	if a.server != nil {
		_ = a.server.Close()
	}
	if a.Watcher != nil {
		_ = a.Watcher.Close()
	}
	if a.Render != nil {
		_ = a.Render.Close()
	}
	return a.State.Close()
}
func (a *App) consumeRenders(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		job, err := a.Jobs.Claim(ctx, "render", "render-1")
		if err != nil || job == nil {
			continue
		}
		stopHeartbeat := a.jobHeartbeat(ctx, job.ID, "render-1")
		var payload struct {
			ArticleID string `json:"articleID"`
			Locale    string `json:"locale"`
		}
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			stopHeartbeat()
			_ = a.Jobs.Fail(ctx, job.ID, err)
			continue
		}
		if payload.ArticleID != "" {
			article, ok := a.Index.Article(model.ArticleID(payload.ArticleID))
			if !ok {
				stopHeartbeat()
				_ = a.Jobs.Fail(ctx, job.ID, fmt.Errorf("article %s not found", payload.ArticleID))
				continue
			}
			version := article.Versions[model.Locale(payload.Locale)]
			// A refresh queued after unpublish/delete must rebuild the list and
			// feed pages, but it must never recreate the removed content unit.
			if version != nil && version.Front.Status == model.StatusPublished {
				if _, err := a.Render.Render(ctx, article, model.Locale(payload.Locale)); err != nil {
					stopHeartbeat()
					_ = a.Jobs.Fail(ctx, job.ID, err)
					continue
				}
			} else if version == nil || version.Front.Status != model.StatusPublished {
				if err := a.Render.Remove(article, model.Locale(payload.Locale)); err != nil && !errors.Is(err, os.ErrNotExist) {
					stopHeartbeat()
					_ = a.Jobs.Fail(ctx, job.ID, err)
					continue
				}
			}
		}
		if err := a.renderLocale(ctx, model.Locale(payload.Locale)); err != nil {
			stopHeartbeat()
			_ = a.Jobs.Fail(ctx, job.ID, err)
			continue
		}
		stopHeartbeat()
		_ = a.Jobs.Complete(ctx, job.ID)
	}
}

func (a *App) jobHeartbeat(ctx context.Context, id int64, worker string) func() {
	beatCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-beatCtx.Done():
				return
			case <-ticker.C:
				if err := a.Jobs.Touch(beatCtx, id, worker); err != nil && !errors.Is(err, context.Canceled) {
					a.logger.Warn("job heartbeat failed", "job", id, "err", err)
				}
			}
		}
	}()
	return cancel
}

func (a *App) watchJobs(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	rendererFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := a.Jobs.ReclaimStale(ctx, 120*time.Second)
			if err != nil {
				a.logger.Warn("stale job recovery failed", "err", err)
			} else if count > 0 {
				a.logger.Warn("reclaimed stale jobs", "count", count)
			}
			if a.Render == nil || !a.Config.Render.WorkerEnabled {
				continue
			}
			if err := a.Render.Health(ctx); err == nil {
				rendererFailures = 0
				continue
			}
			rendererFailures++
			a.logger.Warn("renderer health check failed", "consecutiveFailures", rendererFailures)
			if rendererFailures < 3 {
				continue
			}
			a.logger.Warn("restarting unhealthy renderer after three failed health checks")
			if err := a.Render.Restart(ctx); err != nil {
				a.logger.Error("renderer restart failed; will retry on next watchdog cycle", "err", err)
				continue
			}
			rendererFailures = 0
			a.logger.Info("renderer restarted successfully")
		}
	}
}

// consumeScheduledPublishes makes scheduled_publish durable across process
// restarts. Claiming each row before writing content prevents duplicate work
// if the next ticker overlaps a slow disk operation.
func (a *App) consumeScheduledPublishes(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.publishDue(ctx)
		}
	}
}

func (a *App) publishDue(ctx context.Context) {
	rows, err := a.State.Read().QueryContext(ctx, "SELECT article_id,locale FROM scheduled_publish WHERE status='scheduled' AND publish_at<=?", time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		a.logger.Warn("scheduled publish lookup failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, locale string
		if err := rows.Scan(&id, &locale); err != nil {
			continue
		}
		claim, err := a.State.Write().ExecContext(ctx, "UPDATE scheduled_publish SET status='publishing' WHERE article_id=? AND status='scheduled'", id)
		if err != nil {
			continue
		}
		changed, _ := claim.RowsAffected()
		if changed == 0 {
			continue
		}
		article, ok := a.Index.Article(model.ArticleID(id))
		version := (*model.ArticleVersion)(nil)
		if ok {
			version = article.Versions[model.Locale(locale)]
		}
		if version == nil {
			_, _ = a.State.Write().ExecContext(ctx, "UPDATE scheduled_publish SET status='failed' WHERE article_id=?", id)
			continue
		}
		front := version.Front
		front.Status = model.StatusPublished
		now := time.Now().UTC()
		front.Updated = &now
		if err := a.Content.SaveVersion(article, model.Locale(locale), front, version.Body, content.SaveOpts{BumpSourceRevision: model.Locale(locale) == article.Source, Snapshot: true}); err != nil {
			_, _ = a.State.Write().ExecContext(ctx, "UPDATE scheduled_publish SET status='failed' WHERE article_id=?", id)
			continue
		}
		a.Index.UpsertArticle(article)
		a.Events.Publish(ctx, events.ArticlePublished{ID: article.ID, Locale: model.Locale(locale), FirstPublish: true})
		payload, _ := json.Marshal(map[string]string{"articleID": id, "locale": locale})
		if _, err := a.Jobs.Enqueue(ctx, jobs.Job{Kind: "render", DedupeKey: id + ":" + locale, Payload: payload, Priority: 10}); err != nil {
			_, _ = a.State.Write().ExecContext(ctx, "UPDATE scheduled_publish SET status='scheduled' WHERE article_id=?", id)
			continue
		}
		if a.AI != nil && a.Config.I18n.AutoTranslateOnPublish {
			for _, target := range a.translationTargets(article.Source) {
				if _, err := a.AI.Enqueue(ctx, article.ID, target, false); err != nil {
					a.logger.Warn("scheduled publication translation was not queued", "article", article.ID, "locale", target, "err", err)
				}
			}
		}
		_, _ = a.State.Write().ExecContext(ctx, "UPDATE scheduled_publish SET status='done' WHERE article_id=?", id)
	}
}

// translationTargets is shared by synchronous publish flow expectations and
// the scheduler. Keeping it here prevents a future scheduled path from
// silently diverging from the locale and manual-translation rules.
func (a *App) translationTargets(source model.Locale) []model.Locale {
	registry, err := blogi18n.New(a.Config.I18n)
	if err != nil {
		return nil
	}
	wanted := a.Config.I18n.TranslateTargets
	if len(wanted) == 0 {
		for _, locale := range registry.Locales() {
			wanted = append(wanted, string(locale))
		}
	}
	seen := map[model.Locale]bool{}
	result := make([]model.Locale, 0, len(wanted))
	for _, value := range wanted {
		target, ok := registry.Canonical(value)
		if !ok || target == source || !registry.Enabled(target) || seen[target] {
			continue
		}
		seen[target] = true
		result = append(result, target)
	}
	return result
}
func (a *App) renderLocale(ctx context.Context, loc model.Locale) error {
	posts := a.Index.PublishedPosts(loc)
	prefix := a.localePrefix(loc)
	items := articleItems(posts, loc, prefix)
	perPage := a.Config.Site.PostsPerPage
	if perPage < 1 {
		perPage = 10
	}
	extraPaths := make([]string, 0)
	totalPages := max(1, (len(items)+perPage-1)/perPage)
	for page := 1; page <= totalPages; page++ {
		start := (page - 1) * perPage
		end := start + perPage
		if end > len(items) {
			end = len(items)
		}
		relative := ""
		if page > 1 {
			relative = fmt.Sprintf("page/%d", page)
			extraPaths = append(extraPaths, "/"+prefix+"/"+relative+"/")
		}
		if _, err := a.Render.RenderPaginatedCollection(ctx, loc, a.Config.Site.Title, items[start:end], relative, paginationFor(prefix, page, totalPages)); err != nil {
			return err
		}
	}
	if err := a.renderTaxonomy(ctx, loc, prefix, &extraPaths); err != nil {
		return err
	}
	if _, err := a.Render.RenderNotFound(ctx, loc, notFoundTitle(loc), notFoundMessage(loc), notFoundHomeLabel(loc)); err != nil {
		return err
	}
	if _, err := a.Render.RenderSearch(ctx, loc, searchTitle(loc)); err != nil {
		return err
	}
	extraPaths = append(extraPaths, "/"+prefix+"/search/")
	prefixes := make(map[model.Locale]string, len(a.Config.I18n.Locales))
	for _, configured := range a.Config.I18n.Locales {
		if configured.Enabled {
			prefixes[model.Locale(configured.Code)] = configured.URLPrefix
		}
	}
	return (&feed.Generator{Output: a.Render.Output(), BaseURL: a.Config.Server.BaseURL, SiteTitle: a.Config.Site.Title, Index: a.Index, ExtraURLs: extraPaths, Prefix: prefix, RobotsTxt: a.Config.SEO.RobotsTxt, Prefixes: prefixes, DefaultLocale: model.Locale(a.Config.I18n.DefaultLocale)}).Generate(loc)
}

func paginationFor(prefix string, current, total int) render.Pagination {
	links := make([]render.PaginationLink, 0, total)
	for page := 1; page <= total; page++ {
		url := "/" + prefix + "/"
		if page > 1 {
			url += fmt.Sprintf("page/%d/", page)
		}
		links = append(links, render.PaginationLink{Page: page, URL: url, Current: page == current})
	}
	return render.Pagination{Page: current, Total: total, Links: links}
}

func notFoundTitle(locale model.Locale) string {
	if strings.HasPrefix(strings.ToLower(string(locale)), "zh") {
		return "页面未找到"
	}
	return "Page not found"
}
func notFoundMessage(locale model.Locale) string {
	if strings.HasPrefix(strings.ToLower(string(locale)), "zh") {
		return "你访问的页面不存在或已被移动。"
	}
	return "The page you requested does not exist or has moved."
}
func notFoundHomeLabel(locale model.Locale) string {
	if strings.HasPrefix(strings.ToLower(string(locale)), "zh") {
		return "返回首页"
	}
	return "Back to home"
}
func searchTitle(locale model.Locale) string {
	if strings.HasPrefix(strings.ToLower(string(locale)), "zh") {
		return "搜索"
	}
	return "Search"
}

func articleItems(posts []*model.Article, loc model.Locale, prefix string) []render.HomeItem {
	items := make([]render.HomeItem, 0, len(posts))
	for _, article := range posts {
		v := article.Versions[loc]
		if v == nil {
			continue
		}
		items = append(items, render.HomeItem{Title: v.Front.Title, Description: v.Front.Description, URL: "/" + prefix + "/posts/" + v.Front.Slug + "/"})
	}
	return items
}

func (a *App) localePrefix(locale model.Locale) string {
	for _, configured := range a.Config.I18n.Locales {
		if model.Locale(configured.Code) == locale && configured.URLPrefix != "" {
			return configured.URLPrefix
		}
	}
	return strings.ToLower(string(locale))
}

func (a *App) renderTaxonomy(ctx context.Context, loc model.Locale, prefix string, extraPaths *[]string) error {
	postItems := func(posts []*model.Article) []render.HomeItem { return articleItems(posts, loc, prefix) }
	categories := a.Index.Categories()
	categoryIndex := make([]render.HomeItem, 0, len(categories))
	for _, category := range categories {
		slug := category.Slug
		if slug == "" {
			slug = category.ID
		}
		name := category.Name.Get(loc, "")
		if name == "" {
			name = slug
		}
		relative := "categories/" + slug
		categoryIndex = append(categoryIndex, render.HomeItem{Title: name, Description: category.Description.Get(loc, ""), URL: "/" + prefix + "/" + relative + "/"})
		posts, _ := a.Index.List(index.ListQuery{Type: model.ContentPost, Locale: loc, Category: category.ID, Status: []model.Status{model.StatusPublished}})
		if _, err := a.Render.RenderCollection(ctx, loc, name, postItems(posts), relative); err != nil {
			return err
		}
		*extraPaths = append(*extraPaths, "/"+prefix+"/"+relative+"/")
	}
	if _, err := a.Render.RenderCollection(ctx, loc, "Categories", categoryIndex, "categories"); err != nil {
		return err
	}
	*extraPaths = append(*extraPaths, "/"+prefix+"/categories/")

	tags := a.Index.Tags()
	tagIndex := make([]render.HomeItem, 0, len(tags))
	for _, tag := range tags {
		slug := tag.Slug
		if slug == "" {
			slug = tag.ID
		}
		name := tag.Name.Get(loc, "")
		if name == "" {
			name = slug
		}
		relative := "tags/" + slug
		tagIndex = append(tagIndex, render.HomeItem{Title: name, Description: tag.Description.Get(loc, ""), URL: "/" + prefix + "/" + relative + "/"})
		posts, _ := a.Index.List(index.ListQuery{Type: model.ContentPost, Locale: loc, Tag: tag.ID, Status: []model.Status{model.StatusPublished}})
		if _, err := a.Render.RenderCollection(ctx, loc, name, postItems(posts), relative); err != nil {
			return err
		}
		*extraPaths = append(*extraPaths, "/"+prefix+"/"+relative+"/")
	}
	if _, err := a.Render.RenderCollection(ctx, loc, "Tags", tagIndex, "tags"); err != nil {
		return err
	}
	*extraPaths = append(*extraPaths, "/"+prefix+"/tags/")

	type monthKey struct{ year, month int }
	months := map[monthKey][]*model.Article{}
	for _, article := range a.Index.PublishedPosts(loc) {
		v := article.Versions[loc]
		months[monthKey{v.Front.Date.Year(), int(v.Front.Date.Month())}] = append(months[monthKey{v.Front.Date.Year(), int(v.Front.Date.Month())}], article)
	}
	keys := make([]monthKey, 0, len(months))
	for key := range months {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].year > keys[j].year || (keys[i].year == keys[j].year && keys[i].month > keys[j].month)
	})
	archiveIndex := make([]render.HomeItem, 0, len(keys))
	for _, key := range keys {
		relative := fmt.Sprintf("archives/%04d/%02d", key.year, key.month)
		title := fmt.Sprintf("%04d-%02d", key.year, key.month)
		archiveIndex = append(archiveIndex, render.HomeItem{Title: title, Description: fmt.Sprintf("%d posts", len(months[key])), URL: "/" + prefix + "/" + relative + "/"})
		if _, err := a.Render.RenderCollection(ctx, loc, title, postItems(months[key]), relative); err != nil {
			return err
		}
		*extraPaths = append(*extraPaths, "/"+prefix+"/"+relative+"/")
	}
	if _, err := a.Render.RenderCollection(ctx, loc, "Archive", archiveIndex, "archives"); err != nil {
		return err
	}
	*extraPaths = append(*extraPaths, "/"+prefix+"/archives/")

	links := a.Index.Links()
	linkItems := make([]render.HomeItem, 0, len(links))
	for _, link := range links {
		linkItems = append(linkItems, render.HomeItem{Title: link.Name, Description: link.Description.Get(loc, ""), URL: link.URL})
	}
	if _, err := a.Render.RenderCollection(ctx, loc, "Links", linkItems, "links"); err != nil {
		return err
	}
	*extraPaths = append(*extraPaths, "/"+prefix+"/links/")
	return nil
}
// ensureSessionSecret implements the P5 startup contract: environment wins,
// then config/.secrets.yaml, then a freshly generated 48-byte secret persisted
// atomically with mode 0600. The process must never fail to start because the
// secret file is missing or read-only.
func ensureSessionSecret(root string) error {
	if os.Getenv("BLOG_SESSION_SECRET") != "" || os.Getenv("BLOG_SECURITY_SESSIONSECRET") != "" {
		return nil
	}
	dir := filepath.Join(root, "config")
	path := filepath.Join(dir, ".secrets.yaml")
	if b, err := os.ReadFile(path); err == nil {
		if secret := secretFromYAML(b); secret != "" {
			return os.Setenv("BLOG_SESSION_SECRET", secret)
		}
	}
	secret := ""
	if b, err := os.ReadFile(filepath.Join(root, "cache", "dev-session-secret")); err == nil && len(b) >= 32 {
		secret = strings.TrimSpace(string(b))
	}
	if secret == "" {
		raw := make([]byte, 48)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		secret = base64.RawURLEncoding.EncodeToString(raw)
	}
	if err := fsutil.EnsureDir(dir, 0o755); err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(path, []byte("sessionSecret: "+secret+"\n"), 0o600); err != nil {
		// A read-only config directory must not block startup; the ephemeral
		// secret is still valid for the lifetime of this process.
		slog.Warn("cannot persist session secret; it will change on restart", "path", path, "err", err)
	} else {
		slog.Info("generated a persistent session secret; do not commit config/.secrets.yaml", "path", path)
	}
	return os.Setenv("BLOG_SESSION_SECRET", secret)
}

func secretFromYAML(b []byte) string {
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "sessionSecret:")
		if !ok {
			continue
		}
		return strings.Trim(strings.TrimSpace(rest), `"'`)
	}
	return ""
}

func publishedLocales(article *model.Article) []model.Locale {
	var out []model.Locale
	if article == nil {
		return out
	}
	for loc, version := range article.Versions {
		if version != nil && version.Front.Status == model.StatusPublished {
			out = append(out, loc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
