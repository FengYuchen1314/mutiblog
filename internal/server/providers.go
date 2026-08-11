package server

import (
	"errors"
	"net/http"

	"github.com/FengYuchen1314/mutiblog/internal/ai"
)

type upsertProviderRequest struct {
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	BaseURL         string  `json:"baseUrl"`
	Model           string  `json:"model"`
	Enabled         bool    `json:"enabled"`
	TimeoutSeconds  int     `json:"timeoutSeconds"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Default         bool    `json:"default"`
	APIKey          *string `json:"apiKey,omitempty"`
}

func (s *Server) handleListProviders(w http.ResponseWriter, _ *http.Request) {
	providers, err := s.ai.List()
	if err != nil {
		s.logger.Error("list AI providers failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "providers_unavailable", "Cannot list AI providers.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": providers})
}

func (s *Server) handleUpsertProvider(w http.ResponseWriter, r *http.Request) {
	var request upsertProviderRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	provider, err := s.ai.Upsert(r.PathValue("id"), ai.UpsertProviderInput{
		Name: request.Name, Kind: request.Kind, BaseURL: request.BaseURL, Model: request.Model,
		Enabled: request.Enabled, TimeoutSeconds: request.TimeoutSeconds, MaxOutputTokens: request.MaxOutputTokens,
		Default: request.Default, APIKey: request.APIKey,
	})
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	s.recordSecurityEvent(r, "provider-update", "succeeded", adminActor(r))
	s.writeJSON(w, http.StatusOK, provider)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := s.ai.Delete(r.PathValue("id")); err != nil {
		s.writeProviderError(w, err)
		return
	}
	s.recordSecurityEvent(r, "provider-delete", "succeeded", adminActor(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	result, err := s.ai.Test(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeProviderError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) writeProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ai.ErrProviderNotFound):
		s.writeError(w, http.StatusNotFound, "provider_not_found", "The AI provider does not exist.", nil)
	case errors.Is(err, ai.ErrKeyMissing):
		s.writeError(w, http.StatusUnprocessableEntity, "provider_key_missing", "Save an API key before testing this provider.", nil)
	case errors.Is(err, ai.ErrInvalidProvider):
		s.writeError(w, http.StatusUnprocessableEntity, "provider_invalid", "The AI provider configuration is invalid.", nil)
	case errors.Is(err, ai.ErrProviderFailed):
		s.writeError(w, http.StatusBadGateway, "provider_connection_failed", ai.SafeProviderErrorMessage(err), nil)
	default:
		s.logger.Error("AI provider operation failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "provider_operation_failed", "The AI provider operation failed.", nil)
	}
}
