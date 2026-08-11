package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
	"github.com/FengYuchen1314/mutiblog/internal/auth"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
	"golang.org/x/text/language"
)

var usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{2,31}$`)

type setupRequest struct {
	SiteTitle    string `json:"siteTitle"`
	BaseURL      string `json:"baseUrl"`
	SourceLocale string `json:"sourceLocale"`
	AdminLocale  string `json:"adminLocale"`
	Timezone     string `json:"timezone"`
	Username     string `json:"username"`
	Password     string `json:"password"`
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, _ *http.Request) {
	initialized, err := s.repository.Exists("config/initialized")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "repository_error", "Cannot read setup state.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"initialized": initialized})
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	initialized, err := s.repository.Exists("config/initialized")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "repository_error", "Cannot read setup state.", nil)
		return
	}
	if initialized {
		s.writeError(w, http.StatusConflict, "already_initialized", "MutiBlog is already initialized.", nil)
		return
	}

	var request setupRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	fields := validateSetup(&request)
	if len(fields) > 0 {
		s.writeError(w, http.StatusUnprocessableEntity, "validation_failed", "Some setup fields are invalid.", fields)
		return
	}
	buildRequest, err := s.initialSetupBuildRequest(r.Context())
	if err != nil {
		s.writeError(w, http.StatusUnprocessableEntity, "validation_failed", "The initial static build task ID is invalid.", map[string]string{"taskId": "Use a valid generated task ID."})
		return
	}

	sourceTag, _ := language.Parse(request.SourceLocale)
	adminTag, _ := language.Parse(request.AdminLocale)
	baseURL, _ := normalizeBaseURL(request.BaseURL)
	request.SourceLocale = sourceTag.String()
	request.AdminLocale = adminTag.String()
	now := time.Now().UTC()
	passwordHash, err := auth.HashPassword(request.Password)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "password_hash_failed", "Cannot secure the administrator password.", nil)
		return
	}

	site := domain.SiteConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  request.SourceLocale,
		AdminLocale:   request.AdminLocale,
		Timezone:      request.Timezone,
		BaseURL:       baseURL,
		ActiveTheme:   "earth",
		IDStrategy:    "uuid",
		Locales: map[string]domain.LocalizedSite{
			request.SourceLocale: {Title: strings.TrimSpace(request.SiteTitle)},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	locales := domain.LocalesConfig{
		SchemaVersion: domain.SchemaVersion,
		SourceLocale:  request.SourceLocale,
		Enabled: []domain.LocaleDefinition{{
			Code: request.SourceLocale, Label: defaultLocaleLabel(request.SourceLocale), Enabled: true,
		}},
		Fallback: localeconfig.FixedFallbackOrder(),
	}
	localeconfig.Normalize(&locales)
	admin := domain.AdminConfig{
		SchemaVersion: domain.SchemaVersion,
		Username:      request.Username,
		PasswordHash:  passwordHash,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	commentKey := make([]byte, 32)
	if _, err := rand.Read(commentKey); err != nil {
		s.writeError(w, http.StatusInternalServerError, "secret_generation_failed", "Cannot initialize comment privacy protection.", nil)
		return
	}
	secrets := domain.SecretsConfig{SchemaVersion: domain.SchemaVersion, Providers: map[string]string{}, CommentHMACKey: hex.EncodeToString(commentKey)}
	providers := domain.AIProvidersConfig{
		SchemaVersion:   domain.SchemaVersion,
		DefaultProvider: "google-free",
		Providers: []domain.AIProviderConfig{{
			ID:              "google-free",
			Name:            "Google Free Translate",
			Kind:            ai.ProviderKindGoogleFree,
			BaseURL:         ai.GoogleFreeDefaultEndpoint,
			Model:           ai.GoogleFreeDefaultModel,
			Enabled:         true,
			TimeoutSeconds:  45,
			MaxOutputTokens: 8192,
		}},
	}
	comments := domain.CommentsConfig{SchemaVersion: domain.SchemaVersion, Moderation: "pending", PageSize: 20, MaxLength: 2000}

	for _, write := range []struct {
		path   string
		value  any
		secret bool
	}{
		{"config/secrets.yaml", secrets, true},
		{"config/providers.yaml", providers, false},
		{"config/comments.yaml", comments, false},
		{"config/locales.yaml", locales, false},
		{"config/site.yaml", site, false},
		{"config/admin.yaml", admin, true},
	} {
		if err := s.repository.WriteYAML(write.path, write.value, write.secret); err != nil {
			s.logger.Error("setup write failed", "path", write.path, "error", err)
			s.writeError(w, http.StatusInternalServerError, "setup_write_failed", "Cannot save the initial configuration.", nil)
			return
		}
	}
	if err := s.repository.WriteFile("config/initialized", []byte(now.Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		s.writeError(w, http.StatusInternalServerError, "setup_write_failed", "Cannot finalize the initial configuration.", nil)
		return
	}
	token, session, err := s.sessions.Create(admin.Username)
	if err != nil {
		s.logger.Error("create initial administrator session failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "session_failed", "Cannot create the initial administrator session.", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
	})
	s.recordSecurityEvent(r, "setup", "succeeded", request.Username)
	build := map[string]any{"status": "failed"}
	preparedBuildContext, durableTask, prepareErr := s.prepareInitialStaticBuild(buildRequest)
	switch {
	case prepareErr != nil:
		s.logger.Error("prepare initial static build task failed; administrator can retry from tools", "task", buildRequest.TaskID, "error", prepareErr)
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
	case durableTask:
		if !s.launchInitialStaticBuild(preparedBuildContext, buildRequest.TaskID) {
			s.logger.Error("initial static build task could not start because the server is stopping", "task", buildRequest.TaskID)
			w.Header().Set("X-MutiBlog-Static-Build", "failed")
			break
		}
		s.initialBuildQueued = true
		build = map[string]any{"status": "queued", "taskId": buildRequest.TaskID}
	default:
		// Test and extension publishers are allowed to expose only the compact
		// synchronous SitePublisher contract. Preserve a usable setup path for
		// them while the production publisher always takes the durable branch.
		report, buildErr := s.publisher.Build(publisher.WithBuildRequest(r.Context(), buildRequest))
		if buildErr != nil {
			s.logger.Error("initial static build after setup failed; administrator can retry from tools", "error", buildErr)
			w.Header().Set("X-MutiBlog-Static-Build", "failed")
			break
		}
		build = map[string]any{"status": "succeeded", "report": report}
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"initialized":  true,
		"sourceLocale": request.SourceLocale,
		"adminLocale":  request.AdminLocale,
		"session": map[string]any{
			"username": admin.Username, "csrfToken": session.CSRFToken, "expiresAt": session.ExpiresAt, "adminLocale": request.AdminLocale,
		},
		"build": build,
	})
}

func (s *Server) initialSetupBuildRequest(ctx context.Context) (publisher.BuildRequest, error) {
	request := publisher.BuildRequestFromContext(ctx)
	if request.TaskID == "" {
		var err error
		request.TaskID, err = publisher.NewBuildTaskID()
		if err != nil {
			return publisher.BuildRequest{}, err
		}
	}
	if !taskstore.ValidStaticBuildID(request.TaskID) {
		return publisher.BuildRequest{}, errors.New("initial static build task ID is invalid")
	}
	exists, err := s.repository.Exists("state/tasks/" + request.TaskID + ".yaml")
	if err != nil {
		return publisher.BuildRequest{}, err
	}
	if exists {
		return publisher.BuildRequest{}, errors.New("initial static build task ID already exists")
	}
	request.Operation = "initial-setup"
	request.SubjectKind = ""
	request.SubjectID = ""
	return request, nil
}

func (s *Server) prepareInitialStaticBuild(request publisher.BuildRequest) (context.Context, bool, error) {
	contextWithRequest := publisher.WithBuildRequest(s.lifecycle, request)
	tracked, ok := s.publisher.(*trackedSitePublisher)
	if !ok {
		return contextWithRequest, false, nil
	}
	return tracked.prepareBuild(contextWithRequest)
}

func (s *Server) launchInitialStaticBuild(buildContext context.Context, taskID string) bool {
	return s.launchBackground(func(_ context.Context) {
		s.mutationGate.RLock()
		defer s.mutationGate.RUnlock()
		s.themeGate.RLock()
		defer s.themeGate.RUnlock()
		if _, err := s.publisher.Build(buildContext); err != nil {
			s.logger.Error("initial static build failed; previous public release remains active", "task", taskID, "error", err)
		}
	})
}

func defaultLocaleLabel(locale string) string {
	if locale == "en" {
		return "English"
	}
	if locale == localeconfig.DefaultFallback {
		return localeconfig.DefaultFallbackLabel
	}
	return locale
}

func validateSetup(request *setupRequest) map[string]string {
	fields := make(map[string]string)
	if title := strings.TrimSpace(request.SiteTitle); title == "" || len([]rune(title)) > 80 {
		fields["siteTitle"] = "Site title must contain 1 to 80 characters."
	}
	if baseURL, err := normalizeBaseURL(request.BaseURL); err != nil || baseURL == "" {
		fields["baseUrl"] = "Public base URL must be an absolute HTTP or HTTPS origin."
	}
	if _, err := language.Parse(request.SourceLocale); err != nil || strings.TrimSpace(request.SourceLocale) == "" {
		fields["sourceLocale"] = "Source locale must be a valid BCP 47 language tag."
	}
	adminTag, err := language.Parse(request.AdminLocale)
	if err != nil || strings.TrimSpace(request.AdminLocale) == "" || (adminTag.String() != "en" && adminTag.String() != "zh-CN") {
		fields["adminLocale"] = "Admin locale must be one of the installed console languages."
	}
	if _, err := time.LoadLocation(request.Timezone); err != nil || strings.TrimSpace(request.Timezone) == "" {
		fields["timezone"] = "Timezone must be a valid IANA timezone."
	}
	if !usernamePattern.MatchString(request.Username) {
		fields["username"] = "Username must start with a letter and contain 3 to 32 lowercase letters, numbers, or hyphens."
	}
	if len(request.Password) < 12 || len(request.Password) > 256 {
		fields["password"] = "Password must contain 12 to 256 characters."
	}
	return fields
}
