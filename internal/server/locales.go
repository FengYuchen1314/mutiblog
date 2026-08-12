package server

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	"github.com/FengYuchen1314/mutiblog/internal/localization"
	"golang.org/x/text/language"
)

type updateLocalesRequest struct {
	SourceLocale string                    `json:"sourceLocale"`
	Enabled      []domain.LocaleDefinition `json:"enabled"`
}

func (s *Server) handleLocales(w http.ResponseWriter, _ *http.Request) {
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		s.logger.Error("read locales failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "locales_unavailable", "Cannot read locale configuration.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, config)
}

func (s *Server) handleUpdateLocales(w http.ResponseWriter, r *http.Request) {
	var request updateLocalesRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var current domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &current); err != nil {
		s.writeError(w, http.StatusInternalServerError, "locales_unavailable", "Cannot read locale settings.", nil)
		return
	}
	previous := current
	previousSource := current.SourceLocale
	localeconfig.Normalize(&current)
	requestedSource := strings.TrimSpace(request.SourceLocale)
	if requestedSource != "" {
		sourceTag, err := language.Parse(requestedSource)
		if err != nil {
			s.writeError(w, http.StatusUnprocessableEntity, "source_locale_invalid", "The source locale must be a valid BCP 47 language tag.", nil)
			return
		}
		if sourceTag.String() != localeconfig.FixedSourceLocale {
			s.writeError(w, http.StatusUnprocessableEntity, "source_locale_fixed", "The source locale is fixed to Simplified Chinese (zh-CN).", map[string]string{"sourceLocale": localeconfig.FixedSourceLocale})
			return
		}
	}
	definitions := append([]domain.LocaleDefinition(nil), current.Enabled...)
	existing := make(map[string]int, len(definitions))
	for index := range definitions {
		tag, err := language.Parse(strings.TrimSpace(definitions[index].Code))
		if err != nil || strings.TrimSpace(definitions[index].Code) == "" {
			s.writeError(w, http.StatusInternalServerError, "locales_unavailable", "The stored locale configuration is invalid.", nil)
			return
		}
		code := tag.String()
		if _, duplicate := existing[code]; duplicate {
			s.writeError(w, http.StatusInternalServerError, "locales_unavailable", "The stored locale configuration contains duplicate locale codes.", nil)
			return
		}
		definitions[index].Code = code
		existing[code] = index
	}
	seen := make(map[string]bool, len(request.Enabled))
	targets := make([]string, 0)
	for index, definition := range request.Enabled {
		tag, err := language.Parse(strings.TrimSpace(definition.Code))
		if err != nil || strings.TrimSpace(definition.Code) == "" {
			s.writeError(w, http.StatusUnprocessableEntity, "locale_invalid", "A locale code is not valid BCP 47.", map[string]string{"enabled": "Invalid locale at index " + strconv.Itoa(index) + "."})
			return
		}
		code := tag.String()
		label := strings.TrimSpace(definition.Label)
		if label == "" || len([]rune(label)) > 80 {
			s.writeError(w, http.StatusUnprocessableEntity, "locale_label_invalid", "Locale labels must contain 1 to 80 characters.", nil)
			return
		}
		if seen[code] {
			continue
		}
		seen[code] = true
		if currentIndex, found := existing[code]; found {
			if definitions[currentIndex].Enabled && !definition.Enabled {
				s.writeError(w, http.StatusUnprocessableEntity, "locale_disable_forbidden", "An added locale cannot be disabled or removed.", map[string]string{"enabled": code})
				return
			}
			// Locale membership is append-only. A stale client may send an old
			// label or lifecycle status; retain the authoritative stored entry.
			continue
		}
		if !definition.Enabled {
			s.writeError(w, http.StatusUnprocessableEntity, "locale_enable_required", "A newly added locale must be enabled.", map[string]string{"enabled": code})
			return
		}
		definition.Code = code
		definition.Label = label
		definition.Enabled = true
		definition.Status = domain.LocaleStatusProvisioning
		definitions = append(definitions, definition)
		existing[code] = len(definitions) - 1
		targets = append(targets, code)
	}
	for code := range seen {
		if code == localeconfig.FixedSourceLocale {
			continue
		}
		index, exists := existing[code]
		if exists && definitions[index].Status != domain.LocaleStatusReady && !containsString(targets, code) {
			targets = append(targets, code)
		}
	}
	next := current
	next.SourceLocale = localeconfig.FixedSourceLocale
	next.Enabled = definitions
	localeconfig.Normalize(&next)
	// Hide every requested target before any translation work begins. This is
	// also required for statusless legacy and failed targets: their previous
	// static files may still exist in the active release while a retry runs.
	for _, target := range targets {
		setLocaleStatus(&next, target, domain.LocaleStatusProvisioning)
	}
	taskManager := s.localeTaskManager
	if taskManager == nil && s.localeTasks != nil {
		taskManager = s.localeTasks
	}
	if len(targets) > 0 && taskManager == nil {
		s.writeError(w, http.StatusServiceUnavailable, "locale_tasks_unavailable", "Locale provisioning is temporarily unavailable.", nil)
		return
	}
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		s.writeError(w, http.StatusInternalServerError, "site_unavailable", "Cannot read site settings.", nil)
		return
	}
	if _, exists := site.Locales[localeconfig.FixedSourceLocale]; !exists {
		previousCopy, available := site.Locales[previousSource]
		if !available {
			previousCopy, available = site.Locales[site.SourceLocale]
		}
		if !available || strings.TrimSpace(previousCopy.Title) == "" {
			s.writeError(w, http.StatusInternalServerError, "site_source_copy_missing", "The current source-language site copy is unavailable.", nil)
			return
		}
		if site.Locales == nil {
			site.Locales = make(map[string]domain.LocalizedSite)
		}
		// Legacy repositories may predate the fixed Chinese-source invariant.
		// Preserve their only site copy so the configuration repair remains
		// buildable; managed setup and all future writes use zh-CN directly.
		site.Locales[localeconfig.FixedSourceLocale] = previousCopy
	}
	var localeTask localization.Task
	localeTaskCreated := false
	if len(targets) > 0 {
		var err error
		localeTask, localeTaskCreated, err = taskManager.Prepare(localization.LocaleProvisionStartInput{Locales: targets})
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "locale_task_write_failed", "Locale settings could not be queued safely.", nil)
			return
		}
	}
	failPrepared := func() {
		if !localeTaskCreated {
			return
		}
		if err := taskManager.FailPrepared(localeTask.ID, "locale-config-commit-failed"); err != nil {
			if s.logger != nil {
				s.logger.Error("terminalize locale task after config commit failure failed", "task", localeTask.ID, "error", err)
			}
			// The still-queued receipt can safely prove the config mismatch itself.
			taskManager.LaunchPrepared(localeTask.ID)
		}
	}
	launchPrepared := func() {
		if localeTask.ID == "" {
			return
		}
		if !taskManager.LaunchPrepared(localeTask.ID) && localeTaskCreated && s.logger != nil {
			s.logger.Warn("prepared locale provisioning task remains durable for recovery", "task", localeTask.ID)
		}
	}
	current = next
	if err := s.repository.WriteYAML("config/locales.yaml", current, false); err != nil {
		persisted, readErr := s.readLocaleConfig()
		switch {
		case readErr == nil && reflect.DeepEqual(persisted, current):
			// Atomic rename succeeded and only its durability sync reported an
			// error. The desired config is authoritative; finish the transaction.
			if s.logger != nil {
				s.logger.Warn("locale config write reported an error after the desired state became readable", "error", err)
			}
		case readErr == nil && reflect.DeepEqual(persisted, previous):
			failPrepared()
			s.writeError(w, http.StatusInternalServerError, "locales_write_failed", "Cannot save locale settings.", nil)
			return
		default:
			// The receipt must remain live when the durable config state cannot be
			// proven. Claim any managed write before the watcher can publish it.
			s.syncProjectionAfterManagedMutation()
			launchPrepared()
			s.clearPublicStatsCache()
			s.writeError(w, http.StatusInternalServerError, "locales_write_failed", "Cannot save locale settings.", nil)
			return
		}
	}
	site.SourceLocale = localeconfig.FixedSourceLocale
	site.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML("config/site.yaml", site, false); err != nil {
		persistedSite, readErr := s.readSiteConfig()
		if readErr == nil && reflect.DeepEqual(persistedSite, site) {
			// As with locale config, a post-rename sync error does not roll back an
			// already-readable desired site config.
			if s.logger != nil {
				s.logger.Warn("site config write reported an error after the desired state became readable", "error", err)
			}
		} else {
			rollbackErr := s.repository.WriteYAML("config/locales.yaml", previous, false)
			rolledBack, rollbackReadErr := s.readLocaleConfig()
			rollbackConfirmed := rollbackReadErr == nil && reflect.DeepEqual(rolledBack, previous)
			if rollbackErr != nil && s.logger != nil {
				s.logger.Error("rollback locale settings failed", "error", rollbackErr)
			}
			// The site write or locale rollback may have crossed its atomic rename
			// before reporting failure. Claim the readable final state in every case.
			s.syncProjectionAfterManagedMutation()
			if rollbackConfirmed {
				failPrepared()
			} else {
				launchPrepared()
			}
			s.clearPublicStatsCache()
			s.writeError(w, http.StatusInternalServerError, "site_write_failed", "Cannot save the new source locale.", nil)
			return
		}
	}
	// The route suppresses the generic post-handler projection sync. Claim the
	// committed provisioning config now, while its exclusive mutation gate is
	// still held, so the watcher cannot publish this intermediate state.
	s.syncProjectionAfterManagedMutation()
	if len(targets) > 0 {
		launchPrepared()
		s.clearPublicStatsCache()
		w.Header().Set("X-MutiBlog-Static-Build", "deferred")
		s.writeJSON(w, http.StatusAccepted, map[string]any{
			"locales":      current,
			"task":         adminTaskFromLocaleProvision(localeTask),
			"localization": map[string]any{"status": "queued", "taskId": localeTask.ID},
			"build":        map[string]any{"status": "deferred"},
		})
		return
	}
	s.clearPublicStatsCache()
	w.Header().Set("X-MutiBlog-Static-Build", "skipped")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"locales":      current,
		"localization": map[string]any{"status": "idle"},
		"build":        map[string]any{"status": "skipped"},
	})
}

func (s *Server) readLocaleConfig() (domain.LocalesConfig, error) {
	var config domain.LocalesConfig
	err := s.repository.ReadYAML("config/locales.yaml", &config)
	return config, err
}

func (s *Server) readSiteConfig() (domain.SiteConfig, error) {
	var config domain.SiteConfig
	err := s.repository.ReadYAML("config/site.yaml", &config)
	return config, err
}

func (s *Server) updateLocaleTargetLifecycle(ctx context.Context, update localization.TargetLifecycleUpdate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch update.Status {
	case domain.LocaleStatusBuilding, domain.LocaleStatusReady, domain.LocaleStatusFailed:
	default:
		return errors.New("locale lifecycle status is invalid")
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	var config domain.LocalesConfig
	if err := s.repository.ReadYAML("config/locales.yaml", &config); err != nil {
		return err
	}
	for _, locale := range update.Locales {
		if !setLocaleStatus(&config, locale, update.Status) {
			return errors.New("locale lifecycle target is unavailable")
		}
	}
	if err := s.repository.WriteYAML("config/locales.yaml", config, false); err != nil {
		return err
	}
	s.clearPublicStatsCache()
	return nil
}

func setLocaleStatus(config *domain.LocalesConfig, locale, status string) bool {
	for index := range config.Enabled {
		if config.Enabled[index].Code == locale && locale != localeconfig.FixedSourceLocale {
			config.Enabled[index].Status = status
			config.Enabled[index].Enabled = true
			return true
		}
	}
	return false
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func definitionsEnabled(definitions []domain.LocaleDefinition, locale string) bool {
	for _, definition := range definitions {
		if definition.Code == locale {
			return definition.Enabled
		}
	}
	return false
}
