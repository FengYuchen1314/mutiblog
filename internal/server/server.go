package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/audit"
	"github.com/FengYuchen1314/mutiblog/internal/auth"
	"github.com/FengYuchen1314/mutiblog/internal/backup"
	"github.com/FengYuchen1314/mutiblog/internal/comments"
	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/dictionary"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/links"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	"github.com/FengYuchen1314/mutiblog/internal/localization"
	"github.com/FengYuchen1314/mutiblog/internal/media"
	"github.com/FengYuchen1314/mutiblog/internal/menus"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/projection"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/scheduled"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
	"github.com/FengYuchen1314/mutiblog/internal/themes"
	"github.com/FengYuchen1314/mutiblog/internal/translation"
	"github.com/FengYuchen1314/mutiblog/internal/upvotes"
	"github.com/FengYuchen1314/mutiblog/internal/visits"
	"golang.org/x/text/language"
)

const (
	sessionCookieName = "mutiblog_session"
	previewCookieName = "mutiblog_theme_preview"
)

type Options struct {
	Repository  *fsrepo.Repository
	ConsoleDir  string
	Version     string
	Logger      *slog.Logger
	Publisher   SitePublisher
	NodeBinary  string
	RendererCLI string
}

type requestIDContextKey struct{}

type SitePublisher interface {
	Build(context.Context) (publisher.BuildReport, error)
}

type localeProvisioner interface {
	Provision(context.Context, string) (localization.Report, error)
}

// backupTaskStarter narrows the backup task-start checkpoint used by the
// asynchronous runners. The concrete backup service remains responsible for
// all other task lifecycle operations; keeping this seam small lets callers
// exercise an unavailable checkpoint without coupling it to filesystem
// permission semantics.
type backupTaskStarter interface {
	StartTask(string) (backup.Task, error)
}

type Server struct {
	repository         *fsrepo.Repository
	consoleDir         string
	version            string
	logger             *slog.Logger
	sessions           *auth.SessionStore
	logins             *loginLimiter
	content            *content.Service
	media              *media.Service
	ai                 *ai.Service
	audit              *audit.Service
	publisher          SitePublisher
	translator         *translation.Service
	localeProvisioner  localeProvisioner
	scheduler          *scheduled.Service
	taxonomies         *taxonomy.Service
	comments           *comments.Service
	upvotes            *upvotes.Service
	visits             *visits.Service
	links              *links.Service
	menus              *menus.Service
	backups            *backup.Service
	backupTaskStarter  backupTaskStarter
	themes             *themes.Service
	projection         *projection.Service
	mux                *http.ServeMux
	lifecycle          context.Context
	cancel             context.CancelFunc
	startupWG          sync.WaitGroup
	setupMu            sync.Mutex
	initialBuildQueued bool
	themeGate          sync.RWMutex
	mutationGate       sync.RWMutex
	statsMu            sync.Mutex
	statsCache         map[string]cachedPublicStats
	shareQR            *shareQRService
	backgroundMu       sync.Mutex
}

func New(options Options) (*Server, error) {
	if options.Repository == nil {
		return nil, errors.New("repository is required")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if err := dictionary.Ensure(options.Repository); err != nil {
		return nil, fmt.Errorf("initialize framework dictionaries: %w", err)
	}
	lifecycle, cancel := context.WithCancel(context.Background())
	server := &Server{
		repository: options.Repository,
		consoleDir: options.ConsoleDir,
		version:    options.Version,
		logger:     options.Logger,
		sessions:   auth.NewSessionStore(24 * time.Hour),
		logins:     newLoginLimiter(),
		content:    content.NewService(options.Repository),
		media:      media.NewService(options.Repository),
		ai:         ai.NewService(options.Repository, ai.Client{}),
		audit:      audit.NewService(options.Repository),
		taxonomies: taxonomy.NewService(options.Repository),
		comments:   comments.NewService(options.Repository),
		links:      links.NewService(options.Repository),
		menus:      menus.NewService(options.Repository),
		backups:    backup.NewService(options.Repository),
		themes:     themes.NewService(options.Repository),
		mux:        http.NewServeMux(),
		lifecycle:  lifecycle,
		cancel:     cancel,
		statsCache: make(map[string]cachedPublicStats),
		shareQR:    newShareQRService(),
	}
	server.upvotes = upvotes.NewService(options.Repository, server.content)
	server.visits = visits.NewService(options.Repository, server.content)
	keepBackupService := false
	keepPublisherService := false
	defer func() {
		if !keepBackupService {
			server.backups.Close()
		}
		if !keepPublisherService {
			if publisher, ok := server.publisher.(interface{ Close() }); ok {
				publisher.Close()
			}
		}
	}()
	if err := server.backups.RecoverInterruptedRestores(); err != nil {
		return nil, fmt.Errorf("recover interrupted backup restore: %w", err)
	}
	if _, err := server.ai.MigrateLegacyDefault(); err != nil {
		return nil, fmt.Errorf("migrate default translation provider: %w", err)
	}
	if _, err := localeconfig.MigrateFallback(options.Repository); err != nil {
		return nil, fmt.Errorf("migrate locale fallback: %w", err)
	}
	if _, err := server.media.Recover(); err != nil {
		return nil, fmt.Errorf("reconcile media storage: %w", err)
	}
	if _, err := server.themes.Recover(); err != nil {
		return nil, fmt.Errorf("recover interrupted theme operation: %w", err)
	}
	retryBackupTasks, err := server.backups.RecoverTasks()
	if err != nil {
		return nil, fmt.Errorf("recover backup tasks: %w", err)
	}
	if options.Publisher != nil {
		server.publisher = newTrackedSitePublisher(options.Publisher)
	} else {
		if err := publisher.ValidateNodeBinary(context.Background(), options.NodeBinary); err != nil {
			return nil, err
		}
		publisherService := publisher.NewService(options.Repository, server.content, publisher.CommandRenderer{NodeBinary: options.NodeBinary, RendererCLI: options.RendererCLI})
		if _, err := publisherService.Recover(); err != nil {
			publisherService.Close()
			return nil, fmt.Errorf("recover interrupted static builds: %w", err)
		}
		server.publisher = newTrackedSitePublisher(publisherService)
	}
	if _, err := server.content.InitializePublishedReleases(); err != nil {
		return nil, fmt.Errorf("initialize published content releases: %w", err)
	}
	server.translator = translation.NewService(options.Repository, server.content, server.ai, server.publisher)
	server.localeProvisioner = localization.NewService(options.Repository, server.content, server.translator)
	projectionService, err := projection.Open(options.Repository, server.content, options.Logger)
	if err != nil {
		server.translator.Close()
		return nil, fmt.Errorf("open rebuildable projection: %w", err)
	}
	server.projection = projectionService
	if err := server.projection.RecoverTasks(); err != nil {
		server.translator.Close()
		_ = server.projection.Close()
		return nil, fmt.Errorf("recover index rebuild tasks: %w", err)
	}
	if tracked, ok := server.publisher.(*trackedSitePublisher); ok {
		tracked.setBeforeBuild(func() {
			if _, err := server.projection.RebuildIfChanged(); err != nil {
				server.logger.Error("refresh projection before static build failed", "error", err)
			}
		})
	}
	server.projection.StartWatcher(server.lifecycle, server.reconcileExternalSourceChange)
	server.scheduler = scheduled.NewService(options.Repository, server.content, server.publisher, server.translator, server.acquireBackgroundContentMutation)
	server.translator.SetContentMutationAcquire(server.acquireBackgroundContentMutation)
	server.translator.SetContentChangedCallback(func(entityKind, entityID string, revision int) {
		if _, err := server.scheduler.InvalidateForEntity(entityKind, entityID, revision); err != nil {
			server.logger.Error("invalidate scheduled publication after AI translation failed", "kind", entityKind, "id", entityID, "revision", revision, "error", err)
		}
	})
	// Translation recovery starts only after the projection, its watcher, and the
	// scheduled-publication invalidation hook are ready. Otherwise a recovered AI
	// promotion could mutate the head without immediately retiring a stale
	// publication intent. Persist orphan publication receipts before Recover;
	// Recover then launches all queued work in one pass without racing a runner's
	// first checkpoint.
	server.mutationGate.Lock()
	_, recoverErr := server.translator.ReconcilePublishedPublications()
	if recoverErr != nil {
		recoverErr = fmt.Errorf("recover pending published translations: %w", recoverErr)
	} else {
		_, recoverErr = server.translator.Recover()
	}
	if recoverErr != nil {
		recoverErr = fmt.Errorf("recover translation tasks: %w", recoverErr)
	} else if _, err := server.scheduler.Recover(); err != nil {
		recoverErr = fmt.Errorf("recover scheduled publish tasks: %w", err)
	}
	server.mutationGate.Unlock()
	if recoverErr != nil {
		server.cancel()
		server.scheduler.Close()
		server.translator.Close()
		_ = server.projection.Close()
		return nil, recoverErr
	}
	server.resumeBackupTasks(retryBackupTasks)
	server.routes()
	// An initialized site already has a last-known-good public release. Do not
	// delay the HTTP listener (and Docker health checks) behind a best-effort
	// rebuild that can legitimately take several minutes on a small VPS.
	server.startupWG.Add(1)
	go func() {
		defer server.startupWG.Done()
		server.mutationGate.RLock()
		defer server.mutationGate.RUnlock()
		server.themeGate.RLock()
		defer server.themeGate.RUnlock()
		server.rebuildPublicAtStartup(server.lifecycle)
	}()
	keepBackupService = true
	keepPublisherService = true
	return server, nil
}

func (s *Server) acquireBackgroundContentMutation() func() {
	s.mutationGate.Lock()
	return func() {
		// Claim scheduled-publication and AI content writes in the derived
		// projection before the watcher can observe them as external edits and
		// publish an incomplete translation batch. The owning durable task performs
		// the single intentional public build.
		if s.projection != nil {
			if _, err := s.projection.RebuildManagedChange(); err != nil {
				s.logger.Error("refresh projection after background content mutation failed", "error", err)
			}
		}
		s.mutationGate.Unlock()
	}
}

func (s *Server) reconcileExternalSourceChange(ctx context.Context) (bool, error) {
	// Every managed request holds the shared side of this gate until its
	// deferred projection refresh completes. If the hash is already current
	// after we obtain the exclusive side, the watcher merely observed that
	// internal write and must not enqueue a duplicate public build.
	s.mutationGate.Lock()
	defer s.mutationGate.Unlock()
	// A direct YAML edit cannot remove or disable the required Chinese content
	// fallback. Normalize before claiming the projection fingerprint so a
	// malformed locale file remains retryable after the operator repairs it.
	if _, err := localeconfig.MigrateFallback(s.repository); err != nil {
		return false, fmt.Errorf("normalize externally edited locale fallback: %w", err)
	}
	changed, err := s.projection.RebuildIfChanged()
	if err != nil || !changed {
		return changed, err
	}
	s.clearPublicStatsCache()
	initialized, err := s.repository.Exists("config/site.yaml")
	if err != nil || !initialized {
		return true, err
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	_, err = s.publisher.Build(ctx)
	return true, err
}

func (s *Server) rebuildPublicAtStartup(parent context.Context) {
	if err := parent.Err(); err != nil {
		return
	}
	s.setupMu.Lock()
	if s.initialBuildQueued {
		s.setupMu.Unlock()
		return
	}
	initialized, err := s.repository.Exists("config/initialized")
	s.setupMu.Unlock()
	if err != nil {
		s.logger.Error("check initialization before startup build failed", "error", err)
		return
	}
	if !initialized {
		return
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(parent, 60*time.Second)
		report, err := s.publisher.Build(ctx)
		cancel()
		if err == nil {
			s.logger.Info("startup static build completed", "files", report.Files, "generatedAt", report.GeneratedAt, "attempt", attempt)
			return
		}
		lastErr = err
		if attempt < 3 {
			timer := time.NewTimer(time.Duration(attempt) * time.Second)
			select {
			case <-parent.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
		}
	}
	s.logger.Error("startup static build failed after limited retries; previous release remains active", "error", lastErr)
}

func (s *Server) Close() error {
	s.backgroundMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.backgroundMu.Unlock()
	s.startupWG.Wait()
	if s.backups != nil {
		s.backups.Close()
	}
	if s.scheduler != nil {
		s.scheduler.Close()
	}
	if s.translator != nil {
		s.translator.Close()
	}
	if publisher, ok := s.publisher.(interface{ Close() }); ok {
		publisher.Close()
	}
	if s.projection != nil {
		return s.projection.Close()
	}
	return nil
}

// launchBackground registers post-startup work before checking the lifecycle
// cancellation under the same lock used by Close. That prevents a late HTTP
// request from calling WaitGroup.Add concurrently with shutdown's Wait.
func (s *Server) launchBackground(work func(context.Context)) bool {
	if work == nil {
		return false
	}
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	if s.lifecycle == nil || s.lifecycle.Err() != nil {
		return false
	}
	s.startupWG.Add(1)
	go func() {
		defer s.startupWG.Done()
		work(s.lifecycle)
	}()
	return true
}

func (s *Server) Handler() http.Handler {
	return s.requestID(s.recoverPanic(s.requestLog(s.securityHeaders(s.withBuildTaskRequest(s.previewIsolation(s.mux))))))
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health/live", s.handleLive)
	s.mux.HandleFunc("GET /health/ready", s.handleReady)
	s.mux.HandleFunc("GET /api/v1/setup/status", s.handleSetupStatus)
	s.mux.HandleFunc("POST /api/v1/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.requireSession(s.handleLogout))
	s.mux.HandleFunc("GET /api/v1/auth/session", s.requireSession(s.handleSession))
	s.mux.HandleFunc("PUT /api/v1/admin/security/password", s.requireAdmin(s.handleChangePassword))
	s.mux.HandleFunc("GET /api/v1/admin/security/audit", s.requireAdmin(s.handleSecurityAudit))
	s.mux.HandleFunc("GET /api/v1/admin/system/status", s.requireAdmin(s.handleSystemStatus))
	s.mux.HandleFunc("GET /api/v1/admin/tasks", s.requireAdmin(s.handleListTasks))
	s.mux.HandleFunc("GET /api/v1/admin/tasks/{id}", s.requireAdmin(s.handleGetTask))
	s.mux.HandleFunc("GET /api/v1/admin/index/status", s.requireAdmin(s.handleIndexStatus))
	// Index rebuilds mutate only the derived projection. The durable worker owns
	// that rebuild, so skip the ordinary post-mutation projection refresh here
	// to avoid doing the same expensive work twice before returning 202.
	s.mux.HandleFunc("POST /api/v1/admin/index/rebuild", s.requireAdminWithoutProjectionSync(s.handleRebuildIndex))
	s.mux.HandleFunc("GET /api/v1/admin/index/search", s.requireAdmin(s.handleIndexSearch))
	s.mux.HandleFunc("GET /api/v1/admin/locales", s.requireAdmin(s.handleLocales))
	s.mux.HandleFunc("PUT /api/v1/admin/locales", s.requireExclusiveAdmin(s.handleUpdateLocales))
	s.mux.HandleFunc("GET /api/v1/admin/dictionaries", s.requireAdmin(s.handleFrameworkDictionaries))
	s.mux.HandleFunc("PUT /api/v1/admin/dictionaries/{locale}", s.requireAdmin(s.handleUpdateFrameworkDictionary))
	s.mux.HandleFunc("GET /api/v1/admin/posts", s.requireAdmin(s.handleListPosts))
	s.mux.HandleFunc("POST /api/v1/admin/posts", s.requireAdmin(s.handleCreatePost))
	s.mux.HandleFunc("GET /api/v1/admin/posts/{id}", s.requireAdmin(s.handleGetPost))
	s.mux.HandleFunc("PUT /api/v1/admin/posts/{id}", s.requireAdmin(s.handleUpdatePostSettings))
	s.mux.HandleFunc("PUT /api/v1/admin/posts/{id}/locales/{locale}", s.requireAdmin(s.handleUpdatePostLocale))
	s.mux.HandleFunc("POST /api/v1/admin/posts/{id}/publish", s.requireAdmin(s.handlePublishPost))
	s.mux.HandleFunc("GET /api/v1/admin/posts/{id}/revisions", s.requireAdmin(s.handleListPostRevisions))
	s.mux.HandleFunc("GET /api/v1/admin/posts/{id}/revisions/{revision}", s.requireAdmin(s.handleGetPostRevision))
	s.mux.HandleFunc("POST /api/v1/admin/posts/{id}/revisions/{revision}/restore", s.requireAdmin(s.handleRestorePostRevision))
	s.mux.HandleFunc("POST /api/v1/admin/posts/{id}/{action}", s.requireAdmin(s.handlePostLifecycle))
	s.mux.HandleFunc("DELETE /api/v1/admin/posts/{id}", s.requireAdmin(s.handleDeletePost))
	s.mux.HandleFunc("GET /api/v1/admin/pages", s.requireAdmin(s.handleListPages))
	s.mux.HandleFunc("POST /api/v1/admin/pages", s.requireAdmin(s.handleCreatePage))
	s.mux.HandleFunc("GET /api/v1/admin/pages/{id}", s.requireAdmin(s.handleGetPage))
	s.mux.HandleFunc("PUT /api/v1/admin/pages/{id}", s.requireAdmin(s.handleUpdatePageSettings))
	s.mux.HandleFunc("PUT /api/v1/admin/pages/{id}/locales/{locale}", s.requireAdmin(s.handleUpdatePageLocale))
	s.mux.HandleFunc("POST /api/v1/admin/pages/{id}/publish", s.requireAdmin(s.handlePublishPage))
	s.mux.HandleFunc("GET /api/v1/admin/pages/{id}/revisions", s.requireAdmin(s.handleListPageRevisions))
	s.mux.HandleFunc("GET /api/v1/admin/pages/{id}/revisions/{revision}", s.requireAdmin(s.handleGetPageRevision))
	s.mux.HandleFunc("POST /api/v1/admin/pages/{id}/revisions/{revision}/restore", s.requireAdmin(s.handleRestorePageRevision))
	s.mux.HandleFunc("POST /api/v1/admin/pages/{id}/{action}", s.requireAdmin(s.handlePageLifecycle))
	s.mux.HandleFunc("DELETE /api/v1/admin/pages/{id}", s.requireAdmin(s.handleDeletePage))
	s.mux.HandleFunc("GET /api/v1/admin/taxonomies/{kind}", s.requireAdmin(s.handleListTaxonomies))
	s.mux.HandleFunc("POST /api/v1/admin/taxonomies/{kind}", s.requireAdmin(s.handleCreateTaxonomy))
	s.mux.HandleFunc("PUT /api/v1/admin/taxonomies/{kind}/{id}", s.requireAdmin(s.handleUpdateTaxonomyStructure))
	s.mux.HandleFunc("PUT /api/v1/admin/taxonomies/{kind}/{id}/locales/{locale}", s.requireAdmin(s.handleUpdateTaxonomyLocale))
	s.mux.HandleFunc("DELETE /api/v1/admin/taxonomies/{kind}/{id}", s.requireExclusiveAdmin(s.handleDeleteTaxonomy))
	s.mux.HandleFunc("GET /api/v1/admin/links", s.requireAdmin(s.handleListLinks))
	s.mux.HandleFunc("POST /api/v1/admin/links/groups", s.requireAdmin(s.handleCreateLinkGroup))
	s.mux.HandleFunc("POST /api/v1/admin/links/items", s.requireAdmin(s.handleCreateLink))
	s.mux.HandleFunc("PUT /api/v1/admin/links/groups/{id}", s.requireAdmin(s.handleUpdateLinkGroup))
	s.mux.HandleFunc("PUT /api/v1/admin/links/items/{id}", s.requireAdmin(s.handleUpdateLink))
	s.mux.HandleFunc("PUT /api/v1/admin/links/groups/{id}/locales/{locale}", s.requireAdmin(s.handleUpdateLinkGroupLocale))
	s.mux.HandleFunc("PUT /api/v1/admin/links/items/{id}/locales/{locale}", s.requireAdmin(s.handleUpdateLinkLocale))
	s.mux.HandleFunc("DELETE /api/v1/admin/links/groups/{id}", s.requireAdmin(s.handleDeleteLinkGroup))
	s.mux.HandleFunc("DELETE /api/v1/admin/links/items/{id}", s.requireAdmin(s.handleDeleteLink))
	s.mux.HandleFunc("GET /api/v1/admin/menus", s.requireAdmin(s.handleListMenus))
	s.mux.HandleFunc("POST /api/v1/admin/menus", s.requireAdmin(s.handleCreateMenu))
	s.mux.HandleFunc("POST /api/v1/admin/menus/{id}/items", s.requireAdmin(s.handleAddMenuItem))
	s.mux.HandleFunc("PUT /api/v1/admin/menus/{id}/items/{item}", s.requireAdmin(s.handleUpdateMenuItem))
	s.mux.HandleFunc("PUT /api/v1/admin/menus/{id}/locales/{locale}", s.requireAdmin(s.handleUpdateMenuLocale))
	s.mux.HandleFunc("PUT /api/v1/admin/menus/{id}/items/{item}/locales/{locale}", s.requireAdmin(s.handleUpdateMenuItemLocale))
	s.mux.HandleFunc("DELETE /api/v1/admin/menus/{id}/items/{item}", s.requireAdmin(s.handleDeleteMenuItem))
	s.mux.HandleFunc("DELETE /api/v1/admin/menus/{id}", s.requireAdmin(s.handleDeleteMenu))
	s.mux.HandleFunc("GET /api/v1/admin/backups", s.requireAdmin(s.handleListBackups))
	s.mux.HandleFunc("GET /api/v1/admin/backups/tasks", s.requireAdmin(s.handleListBackupTasks))
	s.mux.HandleFunc("POST /api/v1/admin/backups", s.requireAdmin(s.handleCreateBackup))
	s.mux.HandleFunc("POST /api/v1/admin/backups/import", s.requireAdmin(s.handleImportBackup))
	s.mux.HandleFunc("POST /api/v1/admin/backups/import-url", s.requireAdmin(s.handleImportBackupURL))
	s.mux.HandleFunc("GET /api/v1/admin/backups/{id}/download", s.requireAdmin(s.handleDownloadBackup))
	s.mux.HandleFunc("POST /api/v1/admin/backups/{id}/restore", s.requireExclusiveAdmin(s.handleRestoreBackup))
	s.mux.HandleFunc("DELETE /api/v1/admin/backups/{id}", s.requireAdmin(s.handleDeleteBackup))
	s.mux.HandleFunc("GET /api/v1/admin/themes", s.requireAdmin(s.handleListThemes))
	s.mux.HandleFunc("GET /api/v1/admin/themes/{id}/screenshot", s.requireAdmin(s.handleThemeScreenshot))
	s.mux.HandleFunc("POST /api/v1/admin/themes/install", s.requireAdmin(s.handleInstallTheme))
	s.mux.HandleFunc("POST /api/v1/admin/themes/install-url", s.requireAdmin(s.handleInstallThemeURL))
	s.mux.HandleFunc("POST /api/v1/admin/themes/{id}/activate", s.requireAdmin(s.handleActivateTheme))
	s.mux.HandleFunc("POST /api/v1/admin/themes/{id}/reload", s.requireAdmin(s.handleReloadTheme))
	s.mux.HandleFunc("POST /api/v1/admin/themes/{id}/preview", s.requireAdmin(s.handleCreateThemePreview))
	s.mux.HandleFunc("DELETE /api/v1/admin/themes/{id}", s.requireAdmin(s.handleUninstallTheme))
	s.mux.HandleFunc("GET /api/v1/admin/themes/{id}/settings", s.requireAdmin(s.handleGetThemeSettings))
	s.mux.HandleFunc("PUT /api/v1/admin/themes/{id}/settings", s.requireAdmin(s.handleSaveThemeSettings))
	s.mux.HandleFunc("POST /api/v1/admin/themes/{id}/settings/reset", s.requireAdmin(s.handleResetThemeSettings))
	s.mux.HandleFunc("GET /api/v1/admin/settings", s.requireAdmin(s.handleGetSettings))
	s.mux.HandleFunc("PUT /api/v1/admin/settings/site", s.requireAdmin(s.handleUpdateSiteSettings))
	s.mux.HandleFunc("PUT /api/v1/admin/settings/site/locales/{locale}", s.requireAdmin(s.handleUpdateSiteLocale))
	s.mux.HandleFunc("PUT /api/v1/admin/settings/comments", s.requireAdmin(s.handleUpdateCommentSettings))
	s.mux.HandleFunc("POST /api/v1/admin/publish/build", s.requireAdmin(s.handleBuildSite))
	s.mux.HandleFunc("GET /api/v1/admin/ai/providers", s.requireAdmin(s.handleListProviders))
	s.mux.HandleFunc("PUT /api/v1/admin/ai/providers/{id}", s.requireAdmin(s.handleUpsertProvider))
	s.mux.HandleFunc("DELETE /api/v1/admin/ai/providers/{id}", s.requireAdmin(s.handleDeleteProvider))
	s.mux.HandleFunc("POST /api/v1/admin/ai/providers/{id}/test", s.requireAdmin(s.handleTestProvider))
	s.mux.HandleFunc("GET /api/v1/admin/ai/tasks", s.requireAdmin(s.handleListTranslationTasks))
	s.mux.HandleFunc("GET /api/v1/admin/attachments", s.requireAdmin(s.handleListMedia))
	s.mux.HandleFunc("POST /api/v1/admin/attachments", s.requireAdmin(s.handleCreateMedia))
	s.mux.HandleFunc("DELETE /api/v1/admin/attachments/{id}", s.requireExclusiveAdmin(s.handleDeleteMedia))
	s.mux.HandleFunc("GET /media/{path...}", s.handlePublicMedia)
	s.mux.HandleFunc("GET /api/v1/public/comments", s.withSharedMutation(s.handlePublicComments))
	s.mux.HandleFunc("POST /api/v1/public/comments", s.withSharedMutation(s.handleCreatePublicComment))
	s.mux.HandleFunc("GET /api/v1/public/upvotes", s.withSharedMutation(s.handlePublicUpvotes))
	s.mux.HandleFunc("POST /api/v1/public/upvotes", s.withSharedMutation(s.handleCreatePublicUpvote))
	s.mux.HandleFunc("GET /api/v1/public/stats", s.withSharedMutation(s.handlePublicStats))
	s.mux.HandleFunc("POST /api/v1/public/visits", s.withSharedMutation(s.handleCreatePublicVisit))
	s.mux.HandleFunc("GET /api/v1/public/share-qr", s.withSharedMutation(s.handlePublicShareQR))
	s.mux.HandleFunc("GET /api/v1/admin/comments", s.requireAdmin(s.handleAdminComments))
	s.mux.HandleFunc("PUT /api/v1/admin/comments/{kind}/{subject}/{id}", s.requireAdmin(s.handleModerateComment))
	s.mux.HandleFunc("DELETE /api/v1/admin/comments/{kind}/{subject}/{id}", s.requireAdmin(s.handleDeleteComment))
	s.mux.HandleFunc("GET /console/", s.handleConsole)
	s.mux.HandleFunc("GET /__mutiblog-preview/open/{id}", s.handleOpenThemePreview)
	s.mux.HandleFunc("GET /", s.handlePublic)
}

func (s *Server) handleLive(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	probe := filepath.Join(s.repository.Root(), "state", ".ready")
	if err := os.WriteFile(probe, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
		s.writeError(w, http.StatusServiceUnavailable, "repository_unavailable", "The data repository is not writable.", nil)
		return
	}
	_ = os.Remove(probe)
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *Server) handleSystemStatus(w http.ResponseWriter, _ *http.Request) {
	initialized, _ := s.repository.Exists("config/site.yaml")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"initialized": initialized,
		"version":     s.version,
		"dataDir":     s.repository.Root(),
		"index":       s.projection.Stats(),
		"publisher":   publisherStatus(s.publisher),
	})
}

func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	if s.consoleDir == "" {
		http.NotFound(w, r)
		return
	}
	// Anchor the cleaned URL at a synthetic filesystem root before joining it
	// to the console distribution.
	clean := strings.TrimPrefix(filepath.Clean(string(filepath.Separator)+r.URL.Path), string(filepath.Separator))
	clean = strings.TrimPrefix(clean, "console/")
	if clean == "." || clean == "" {
		clean = "index.html"
	}
	requested := filepath.Join(s.consoleDir, clean)
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		s.serveFile(w, r, requested)
		return
	}
	s.serveFile(w, r, filepath.Join(s.consoleDir, "index.html"))
}

func (s *Server) handlePublic(w http.ResponseWriter, r *http.Request) {
	current := filepath.Join(s.repository.Root(), "generated", "current")
	isPreview := s.isThemePreviewHost(r)
	if isPreview {
		cookie, err := r.Cookie(previewCookieName)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		previewer, ok := s.publisher.(interface {
			PreviewRoot(string) (publisher.PreviewRecord, string, error)
		})
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, root, err := previewer.PreviewRoot(cookie.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{Name: previewCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
			http.NotFound(w, r)
			return
		}
		current = root
		w.Header().Set("Cache-Control", "private, no-store")
	}
	s.serveStaticRootWithLocaleVisibility(w, r, current, !isPreview)
}

func (s *Server) serveStaticRoot(w http.ResponseWriter, r *http.Request, current string) {
	s.serveStaticRootWithLocaleVisibility(w, r, current, true)
}

func (s *Server) serveStaticRootWithLocaleVisibility(w http.ResponseWriter, r *http.Request, current string, enforceVisibility bool) {
	// Repeat the anchored clean at the public boundary instead of depending on
	// ServeMux or a reverse proxy to canonicalize encoded dot segments.
	clean := strings.TrimPrefix(filepath.Clean(string(filepath.Separator)+r.URL.Path), string(filepath.Separator))
	if clean == "." || clean == "" {
		clean = "index.html"
	}
	var localeConfig *domain.LocalesConfig
	if enforceVisibility {
		localeConfig = s.readPublicLocaleConfig()
		if pathTargetsExplicitlyHiddenLocale(clean, localeConfig) {
			// The locale can become ready without changing this URL, so prevent an
			// intermediary from retaining the temporary lifecycle 404.
			w.Header().Set("Cache-Control", "no-cache")
			http.NotFound(w, r)
			return
		}
	}
	if strings.Trim(r.URL.Path, "/") == "" {
		w.Header().Add("Vary", "Accept-Language")
		w.Header().Set("Cache-Control", "no-cache")
	}
	if target, ok := s.publicRedirectWithLocaleVisibility(current, r, localeConfig, enforceVisibility); ok {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	requested := filepath.Join(current, clean)
	if info, err := os.Stat(requested); err == nil && !info.IsDir() {
		if w.Header().Get("Cache-Control") == "" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		s.serveFile(w, r, requested)
		return
	}
	if info, err := os.Stat(filepath.Join(requested, "index.html")); err == nil && !info.IsDir() {
		if w.Header().Get("Cache-Control") == "" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		s.serveFile(w, r, filepath.Join(requested, "index.html"))
		return
	}
	if notFound := s.localizedNotFoundWithLocaleVisibility(current, clean, localeConfig, enforceVisibility); notFound != "" {
		if w.Header().Get("Cache-Control") == "" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		s.serveFileStatus(w, r, notFound, http.StatusNotFound)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) previewOrigin(r *http.Request) (string, error) {
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		return "", err
	}
	raw := strings.TrimSpace(site.BaseURL)
	if raw == "" {
		scheme := "https"
		if r.TLS == nil && strings.HasPrefix(r.Host, "localhost") {
			scheme = "http"
		}
		raw = scheme + "://" + r.Host
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return "", errors.New("site base URL is invalid")
	}
	hostname := parsed.Hostname()
	parts := strings.Split(hostname, ".")
	if len(parts) >= 3 {
		parts[0] = "preview"
	} else {
		parts = append([]string{"preview"}, parts...)
	}
	host := strings.Join(parts, ".")
	if parsed.Port() != "" {
		host += ":" + parsed.Port()
	}
	scheme := parsed.Scheme
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	return scheme + "://" + host, nil
}

func (s *Server) isThemePreviewHost(r *http.Request) bool {
	origin, err := s.previewOrigin(r)
	if err != nil {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(r.Host, parsed.Host)
}

func (s *Server) localizedNotFound(current, clean string) string {
	return s.localizedNotFoundWithLocaleVisibility(current, clean, s.readPublicLocaleConfig(), true)
}

func (s *Server) localizedNotFoundWithLocaleVisibility(current, clean string, config *domain.LocalesConfig, enforceVisibility bool) string {
	requestedLocale := strings.SplitN(clean, "/", 2)[0]
	candidates := []string{requestedLocale}
	available, hasReleaseReport := activeReleaseLocales(current)
	if enforceVisibility {
		available = filterExplicitlyHiddenLocales(available, config)
	}
	sourceLocale := releaseRootLocale(current, available)
	if sourceLocale == "" && !hasReleaseReport {
		if config == nil {
			config = s.readPublicLocaleConfig()
		}
		if config != nil {
			sourceLocale = config.SourceLocale
		}
	}
	if requestedLocale != localeconfig.DefaultFallback {
		candidates = append(candidates, localeconfig.DefaultFallback)
	}
	if sourceLocale != requestedLocale && sourceLocale != localeconfig.DefaultFallback {
		candidates = append(candidates, sourceLocale)
	}
	for _, locale := range candidates {
		if locale == "" || locale == "." || strings.ContainsAny(locale, `/\\`) {
			continue
		}
		if enforceVisibility && localeIsExplicitlyHidden(locale, config) {
			continue
		}
		path := filepath.Join(current, locale, "404.html")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func (s *Server) publicRedirect(current string, r *http.Request) (string, bool) {
	return s.publicRedirectWithLocaleVisibility(current, r, s.readPublicLocaleConfig(), true)
}

func (s *Server) publicRedirectWithLocaleVisibility(current string, r *http.Request, config *domain.LocalesConfig, enforceVisibility bool) (string, bool) {
	if strings.Trim(r.URL.Path, "/") == "" {
		if target, ok := s.negotiatedRootTargetWithLocaleVisibility(current, r.Header.Get("Accept-Language"), config, enforceVisibility); ok {
			return target, true
		}
	}
	data, err := os.ReadFile(filepath.Join(current, "redirects.json"))
	if err != nil {
		return "", false
	}
	var redirects []publisher.RedirectRecord
	if err := json.Unmarshal(data, &redirects); err != nil {
		s.logger.Error("decode public redirects failed", "error", err)
		return "", false
	}
	want := strings.TrimSuffix(r.URL.Path, "/")
	if want == "" {
		want = "/"
	}
	for _, redirect := range redirects {
		from := strings.TrimSuffix(redirect.From, "/")
		if from == "" {
			from = "/"
		}
		if from == want && redirect.Status == http.StatusFound && strings.HasPrefix(redirect.To, "/") && !strings.HasPrefix(redirect.To, "//") {
			if enforceVisibility && redirectTargetsExplicitlyHiddenLocale(redirect.To, config) {
				continue
			}
			return redirect.To, true
		}
	}
	return "", false
}

func (s *Server) negotiatedRootTarget(current, acceptLanguage string) (string, bool) {
	return s.negotiatedRootTargetWithLocaleVisibility(current, acceptLanguage, s.readPublicLocaleConfig(), true)
}

func (s *Server) negotiatedRootTargetWithLocaleVisibility(current, acceptLanguage string, config *domain.LocalesConfig, enforceVisibility bool) (string, bool) {
	available, hasReleaseReport := activeReleaseLocales(current)
	if enforceVisibility {
		available = filterExplicitlyHiddenLocales(available, config)
	}
	if len(available) == 0 && !hasReleaseReport {
		if config == nil {
			config = s.readPublicLocaleConfig()
		}
		if config == nil {
			return "", false
		}
		for _, locale := range config.Enabled {
			if localeDefinitionIsPublic(locale) {
				available = append(available, locale.Code)
			}
		}
	}
	defaultLocale := releaseRootLocale(current, available)
	codes := make([]string, 0, len(available))
	tags := make([]language.Tag, 0, len(available))
	appendLocale := func(code string) {
		for _, existing := range codes {
			if existing == code {
				return
			}
		}
		tag, err := language.Parse(code)
		if err == nil && tag.String() == code && publicLocaleExists(current, code) {
			codes = append(codes, code)
			tags = append(tags, tag)
		}
	}
	// The root redirect embedded in the active release is its published source
	// language. Put it first so unpublished locale settings cannot change the
	// public default when a later build fails.
	appendLocale(defaultLocale)
	for _, code := range available {
		appendLocale(code)
	}
	if len(tags) == 0 {
		return "", false
	}
	index := 0
	if strings.TrimSpace(acceptLanguage) != "" {
		accepted, _, err := language.ParseAcceptLanguage(acceptLanguage)
		if err == nil && len(accepted) > 0 {
			_, matched, confidence := language.NewMatcher(tags).Match(accepted...)
			// Do not route an unrelated language to the matcher's merely closest
			// script or family. Only exact/base-region matches override the source
			// locale fallback.
			if confidence >= language.High && matched >= 0 && matched < len(codes) {
				index = matched
			}
		}
	}
	return "/" + codes[index] + "/", true
}

func (s *Server) readPublicLocaleConfig() *domain.LocalesConfig {
	if s.repository == nil {
		return nil
	}
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return nil
	}
	return &config
}

func localeDefinitionIsPublic(definition domain.LocaleDefinition) bool {
	status := strings.TrimSpace(definition.Status)
	return definition.Enabled && (status == "" || status == domain.LocaleStatusReady)
}

func localeIsExplicitlyHidden(locale string, config *domain.LocalesConfig) bool {
	if config == nil {
		return false
	}
	for _, definition := range config.Enabled {
		if definition.Code == locale {
			status := strings.TrimSpace(definition.Status)
			return !definition.Enabled || (status != "" && status != domain.LocaleStatusReady)
		}
	}
	return false
}

func filterExplicitlyHiddenLocales(locales []string, config *domain.LocalesConfig) []string {
	if config == nil {
		return locales
	}
	filtered := make([]string, 0, len(locales))
	for _, locale := range locales {
		if !localeIsExplicitlyHidden(locale, config) {
			filtered = append(filtered, locale)
		}
	}
	return filtered
}

func pathTargetsExplicitlyHiddenLocale(clean string, config *domain.LocalesConfig) bool {
	locale := strings.SplitN(clean, "/", 2)[0]
	return localeIsExplicitlyHidden(locale, config)
}

func redirectTargetsExplicitlyHiddenLocale(target string, config *domain.LocalesConfig) bool {
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	clean := strings.TrimPrefix(filepath.Clean(string(filepath.Separator)+parsed.Path), string(filepath.Separator))
	return pathTargetsExplicitlyHiddenLocale(clean, config)
}

func activeReleaseLocales(current string) ([]string, bool) {
	data, err := os.ReadFile(filepath.Join(current, "build-report.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false
	}
	if err != nil {
		return nil, true
	}
	var report publisher.BuildReport
	if json.Unmarshal(data, &report) != nil || report.SchemaVersion != domain.SchemaVersion {
		return nil, true
	}
	return report.Locales, true
}

func releaseRootLocale(current string, available []string) string {
	data, err := os.ReadFile(filepath.Join(current, "redirects.json"))
	if err == nil {
		var redirects []publisher.RedirectRecord
		if json.Unmarshal(data, &redirects) == nil {
			for _, redirect := range redirects {
				if redirect.From != "/" || redirect.Status != http.StatusFound {
					continue
				}
				candidate := strings.Trim(redirect.To, "/")
				for _, locale := range available {
					if candidate == locale && redirect.To == "/"+locale+"/" {
						return locale
					}
				}
			}
		}
	}
	if len(available) > 0 {
		return available[0]
	}
	return ""
}

func publicLocaleExists(current, locale string) bool {
	if locale == "" || strings.ContainsAny(locale, `/\\`) {
		return false
	}
	info, err := os.Stat(filepath.Join(current, locale, "index.html"))
	return err == nil && info.Mode().IsRegular()
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, path string) {
	s.serveFileStatus(w, r, path, http.StatusOK)
}

func (s *Server) serveFileStatus(w http.ResponseWriter, r *http.Request, path string, status int) {
	file, err := os.Open(path)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}
	if contentType := mime.TypeByExtension(filepath.Ext(path)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	etag := fmt.Sprintf("\"%x-%x\"", info.ModTime().UnixNano(), info.Size())
	w.Header().Set("ETag", etag)
	if status == http.StatusOK && etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func etagMatches(header, current string) bool {
	for value := range strings.SplitSeq(header, ",") {
		value = strings.TrimSpace(value)
		if value == "*" || strings.TrimPrefix(value, "W/") == current {
			return true
		}
	}
	return false
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	s.writeJSON(w, status, map[string]any{"code": code, "message": message, "requestId": w.Header().Get("X-Request-ID"), "fields": fields})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buffer := make([]byte, 12)
		id := ""
		if _, err := rand.Read(buffer); err == nil {
			id = hex.EncodeToString(buffer)
		} else {
			id = fmt.Sprintf("%024x", uint64(time.Now().UnixNano()))
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// previewIsolation keeps untrusted theme output on a sibling host away from
// installation, authentication, administration, and operational endpoints.
// Host-only session cookies already separate the browser state; this server-
// side boundary also prevents a user from logging in on the preview host.
func (s *Server) previewIsolation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isThemePreviewHost(r) {
			for _, prefix := range []string{"/api/v1/admin", "/api/v1/auth", "/api/v1/setup", "/console", "/health/"} {
				if r.URL.Path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(r.URL.Path, prefix) {
					http.NotFound(w, r)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		defer func() {
			requestID, _ := r.Context().Value(requestIDContextKey{}).(string)
			s.logger.Info("request", "requestId", requestID, "method", r.Method, "path", r.URL.Path, "durationMs", time.Since(started).Milliseconds())
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				requestID, _ := r.Context().Value(requestIDContextKey{}).(string)
				s.logger.Error("request panic", "requestId", requestID, "error", fmt.Sprint(recovered), "path", r.URL.Path)
				s.writeError(w, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	// Only the local/private reverse-proxy hop may describe the original
	// scheme. A direct caller can otherwise force a Secure cookie over cleartext
	// HTTP simply by spoofing X-Forwarded-Proto, leaving login/logout state
	// inconsistent. Match the strict first-hop rule used by public QR and
	// upvote endpoints.
	if !trustedProxyIP(remoteIP(r.RemoteAddr)) {
		return false
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")
	return len(forwarded) > 0 && strings.EqualFold(strings.TrimSpace(forwarded[0]), "https")
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
