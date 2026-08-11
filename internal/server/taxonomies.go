package server

import (
	"errors"
	"net/http"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/taxonomy"
)

type createTaxonomyRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    string `json:"parentId"`
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
	item, err := s.taxonomies.Create(r.PathValue("kind"), taxonomy.CreateInput{ID: request.ID, Name: request.Name, Description: request.Description, ParentID: request.ParentID})
	if err != nil {
		s.writeTaxonomyError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusCreated, item, item.Kind, item.ID)
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
	s.writePublishedResource(w, r, http.StatusOK, item, item.Kind, item.ID)
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
	item, err := s.taxonomies.UpdateStructure(r.PathValue("kind"), r.PathValue("id"), taxonomy.UpdateStructureInput{ExpectedRevision: request.Revision, ParentID: request.ParentID})
	if err != nil {
		s.writeTaxonomyError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, item.Kind, item.ID)
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
	case errors.Is(err, taxonomy.ErrInvalidKind), errors.Is(err, taxonomy.ErrInvalidParent):
		s.writeError(w, http.StatusUnprocessableEntity, "taxonomy_invalid", "The taxonomy relationship is invalid.", nil)
	case errors.Is(err, taxonomy.ErrInUse):
		s.writeError(w, http.StatusConflict, "taxonomy_in_use", "Remove child categories and content references before deleting this taxonomy.", nil)
	default:
		s.logger.Error("taxonomy operation failed", "error", err)
		s.writeError(w, http.StatusUnprocessableEntity, "taxonomy_invalid", "The taxonomy is invalid.", nil)
	}
}
