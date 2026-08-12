package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
)

type createTaxonomyRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    string `json:"parentId"`
	Cover       string `json:"cover"`
	Template    string `json:"template"`
}

type updateTaxonomyLocaleRequest struct {
	Revision       int    `json:"revision"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	SEOTitle       string `json:"seoTitle"`
	SEODescription string `json:"seoDescription"`
}

type deleteResourceRequest struct {
	Revision int `json:"revision"`
}

type updateTaxonomyStructureRequest struct {
	Revision int    `json:"revision"`
	ParentID string `json:"parentId"`
	Cover    string `json:"cover"`
	Template string `json:"template"`
}

func (s *Server) handleListTaxonomies(w http.ResponseWriter, r *http.Request) {
	items, err := s.taxonomies.List(r.PathValue("kind"))
	if err != nil {
		s.writeTaxonomyError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleCreateTaxonomy(w http.ResponseWriter, r *http.Request) {
	var request createTaxonomyRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if request.Template == "" {
		request.Template = "category"
	}
	if taxonomyKindIsCategory(r.PathValue("kind")) {
		s.themeGate.RLock()
		defer s.themeGate.RUnlock()
		if supported, err := s.themes.SupportsActiveTemplate("category", request.Template); err != nil {
			s.writeThemeError(w, err)
			return
		} else if !supported {
			s.writeError(w, http.StatusUnprocessableEntity, "theme_template_invalid", "The active theme does not provide this category template.", map[string]string{"template": "Choose a template provided by the active theme."})
			return
		}
	}
	item, err := s.taxonomies.Create(r.PathValue("kind"), taxonomy.CreateInput{ID: request.ID, Name: request.Name, Description: request.Description, ParentID: request.ParentID, Cover: request.Cover, Template: request.Template})
	if err != nil {
		s.writeTaxonomyError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusCreated, item, item.Kind, item.ID, true)
}

func (s *Server) handleUpdateTaxonomyLocale(w http.ResponseWriter, r *http.Request) {
	var request updateTaxonomyLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.taxonomies.UpdateLocale(r.PathValue("kind"), r.PathValue("id"), r.PathValue("locale"), taxonomy.UpdateLocaleInput{
		ExpectedRevision: request.Revision, Name: request.Name, Description: request.Description,
		SEOTitle: request.SEOTitle, SEODescription: request.SEODescription,
	})
	if err != nil {
		s.writeTaxonomyError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, item.Kind, item.ID, sourceLocaleWasUpdated(item.SourceLocale, r.PathValue("locale")))
}

func (s *Server) handleDeleteTaxonomy(w http.ResponseWriter, r *http.Request) {
	var request deleteResourceRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if err := s.taxonomies.Delete(r.PathValue("kind"), r.PathValue("id"), request.Revision); err != nil {
		s.writeTaxonomyError(w, err)
		return
	}
	s.writePublishedDeletion(w, r, "Taxonomy", r.PathValue("id"))
}

func (s *Server) handleUpdateTaxonomyStructure(w http.ResponseWriter, r *http.Request) {
	var request updateTaxonomyStructureRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if taxonomyKindIsCategory(r.PathValue("kind")) {
		if request.Template == "" {
			request.Template = "category"
		}
		s.themeGate.RLock()
		defer s.themeGate.RUnlock()
		if supported, err := s.themes.SupportsActiveTemplate("category", request.Template); err != nil {
			s.writeThemeError(w, err)
			return
		} else if !supported {
			current, currentErr := s.taxonomies.Get("Category", r.PathValue("id"))
			if currentErr != nil {
				s.writeTaxonomyError(w, currentErr)
				return
			}
			if request.Template != current.Template {
				s.writeError(w, http.StatusUnprocessableEntity, "theme_template_invalid", "The active theme does not provide this category template.", map[string]string{"template": "Choose a template provided by the active theme."})
				return
			}
		}
	}
	item, err := s.taxonomies.UpdateStructure(r.PathValue("kind"), r.PathValue("id"), taxonomy.UpdateStructureInput{ExpectedRevision: request.Revision, ParentID: request.ParentID, Cover: request.Cover, Template: request.Template})
	if err != nil {
		s.writeTaxonomyError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, item.Kind, item.ID, false)
}

func taxonomyKindIsCategory(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "category", "categories":
		return true
	default:
		return false
	}
}

func (s *Server) writeTaxonomyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, taxonomy.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "taxonomy_not_found", "The taxonomy does not exist.", nil)
	case errors.Is(err, taxonomy.ErrConflict):
		s.writeError(w, http.StatusConflict, "revision_conflict", "The taxonomy changed after it was loaded.", nil)
	case errors.Is(err, content.ErrAlreadyExists):
		s.writeError(w, http.StatusConflict, "taxonomy_id_exists", "This ID already exists.", nil)
	case errors.Is(err, content.ErrInvalidID):
		s.writeError(w, http.StatusUnprocessableEntity, "invalid_taxonomy_id", "The custom ID must contain lowercase letters and hyphens only.", nil)
	case errors.Is(err, content.ErrLocaleDisabled):
		s.writeError(w, http.StatusUnprocessableEntity, "locale_disabled", "The locale is not enabled.", nil)
	case errors.Is(err, taxonomy.ErrInvalidSettings):
		s.writeError(w, http.StatusUnprocessableEntity, "taxonomy_invalid", "The taxonomy cover or template is invalid.", nil)
	case errors.Is(err, taxonomy.ErrInvalidKind), errors.Is(err, taxonomy.ErrInvalidParent):
		s.writeError(w, http.StatusUnprocessableEntity, "taxonomy_invalid", "The taxonomy relationship is invalid.", nil)
	case errors.Is(err, taxonomy.ErrInUse):
		s.writeError(w, http.StatusConflict, "taxonomy_in_use", "Remove child categories and content references before deleting this taxonomy.", nil)
	default:
		s.logger.Error("taxonomy operation failed", "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, "taxonomy_invalid", "The taxonomy is invalid.", nil)
	}
}
