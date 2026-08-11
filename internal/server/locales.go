package server

import (
	"net/http"
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
	current = next
	if err := s.repository.WriteYAML("config/locales.yaml", current, false); err != nil {
		s.writeError(w, http.StatusInternalServerError, "locales_write_failed", "Cannot save locale settings.", nil)
		return
	}
	site.SourceLocale = localeconfig.FixedSourceLocale
	site.UpdatedAt = time.Now().UTC()
	if err := s.repository.WriteYAML("config/site.yaml", site, false); err != nil {
		if rollbackErr := s.repository.WriteYAML("config/locales.yaml", previous, false); rollbackErr != nil {
			s.logger.Error("rollback locale settings failed", "error", rollbackErr)
		}
		s.writeError(w, http.StatusInternalServerError, "site_write_failed", "Cannot save the new source locale.", nil)
		return
	}
	localizationReports := make([]localization.Report, 0, len(targets))
	localizationFailures := make([]string, 0)
	readyTargets := make([]string, 0, len(targets))
	for _, target := range targets {
		if s.localeProvisioner == nil {
			localizationFailures = append(localizationFailures, target)
			setLocaleStatus(&current, target, domain.LocaleStatusFailed)
			continue
		}
		result, provisionErr := s.localeProvisioner.Provision(r.Context(), target)
		if provisionErr != nil {
			s.logger.Error("site-wide locale provisioning failed", "locale", target, "error", provisionErr)
			localizationFailures = append(localizationFailures, target)
			setLocaleStatus(&current, target, domain.LocaleStatusFailed)
			continue
		}
		localizationReports = append(localizationReports, result)
		readyTargets = append(readyTargets, target)
		setLocaleStatus(&current, target, domain.LocaleStatusBuilding)
	}
	if len(targets) > 0 {
		if err := s.repository.WriteYAML("config/locales.yaml", current, false); err != nil {
			s.writeError(w, http.StatusInternalServerError, "locales_write_failed", "Cannot save locale translation status.", nil)
			return
		}
	}
	s.clearPublicStatsCache()
	if len(targets) > 0 && len(readyTargets) == 0 {
		w.Header().Set("X-MutiBlog-Static-Build", "skipped")
		s.writeJSON(w, http.StatusAccepted, map[string]any{
			"locales":      current,
			"localization": map[string]any{"status": "failed", "reports": localizationReports, "failedLocales": localizationFailures},
			"build":        map[string]any{"status": "failed"},
		})
		return
	}
	report, err := s.publisher.Build(r.Context())
	if err != nil {
		s.logger.Error("static build after locale settings update failed", "error", err)
		for _, target := range readyTargets {
			setLocaleStatus(&current, target, domain.LocaleStatusFailed)
		}
		if statusErr := s.repository.WriteYAML("config/locales.yaml", current, false); statusErr != nil {
			s.logger.Error("mark locales failed after static build failure", "error", statusErr)
			s.writeError(w, http.StatusInternalServerError, "locale_status_write_failed", "The static build failed and the locale status could not be saved safely.", nil)
			return
		}
		w.Header().Set("X-MutiBlog-Static-Build", "failed")
		s.writeJSON(w, http.StatusAccepted, map[string]any{
			"locales":      current,
			"localization": map[string]any{"status": "failed", "reports": localizationReports, "failedLocales": append(localizationFailures, readyTargets...)},
			"build":        map[string]any{"status": "failed"},
		})
		return
	}
	for _, target := range readyTargets {
		setLocaleStatus(&current, target, domain.LocaleStatusReady)
	}
	if len(readyTargets) > 0 {
		if err := s.repository.WriteYAML("config/locales.yaml", current, false); err != nil {
			s.logger.Error("mark locales ready after static build", "error", err)
			w.Header().Set("X-MutiBlog-Static-Build", "succeeded")
			s.writeError(w, http.StatusInternalServerError, "locale_status_write_failed", "The static site was built, but the locale status could not be finalized safely.", nil)
			return
		}
	}
	w.Header().Set("X-MutiBlog-Static-Build", "succeeded")
	status := http.StatusOK
	localizationStatus := "succeeded"
	if len(localizationFailures) > 0 {
		status = http.StatusAccepted
		localizationStatus = "partial"
	}
	s.writeJSON(w, status, map[string]any{
		"locales":      current,
		"localization": map[string]any{"status": localizationStatus, "reports": localizationReports, "failedLocales": localizationFailures},
		"build":        map[string]any{"status": "succeeded", "report": report},
	})
}

func setLocaleStatus(config *domain.LocalesConfig, locale, status string) {
	for index := range config.Enabled {
		if config.Enabled[index].Code == locale && locale != localeconfig.FixedSourceLocale {
			config.Enabled[index].Status = status
			config.Enabled[index].Enabled = true
			return
		}
	}
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
