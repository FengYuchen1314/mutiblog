package server

import (
	"errors"
	"net/http"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/menus"
)

type createMenuRequest struct{ ID, Label string }
type addMenuItemRequest struct {
	ID, ParentID, TargetKind, URL, Label string
	OpenInNew                            bool
	Order, Revision                      int
}
type updateMenuLocaleRequest struct {
	Revision int
	Label    string
}
type updateMenuItemRequest struct {
	Revision                  int
	ParentID, TargetKind, URL string
	OpenInNew                 bool
	Order                     int
}

func (s *Server) handleListMenus(w http.ResponseWriter, _ *http.Request) {
	items, err := s.menus.List()
	if err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writeJSON(w, 200, map[string]any{"items": items})
}
func (s *Server) handleCreateMenu(w http.ResponseWriter, r *http.Request) {
	var request createMenuRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.menus.Create(menus.CreateInput{ID: request.ID, Label: request.Label})
	if err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusCreated, item, "Menu", item.ID)
}
func (s *Server) handleAddMenuItem(w http.ResponseWriter, r *http.Request) {
	var request addMenuItemRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.menus.AddItem(r.PathValue("id"), menus.AddItemInput{ID: request.ID, ParentID: request.ParentID, TargetKind: request.TargetKind, URL: request.URL, Label: request.Label, OpenInNew: request.OpenInNew, Order: request.Order, ExpectedRevision: request.Revision})
	if err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusCreated, item, "Menu", item.ID)
}
func (s *Server) handleUpdateMenuLocale(w http.ResponseWriter, r *http.Request) {
	var request updateMenuLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.menus.UpdateMenuLocale(r.PathValue("id"), r.PathValue("locale"), menus.UpdateLocaleInput{ExpectedRevision: request.Revision, Label: request.Label})
	if err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, "Menu", item.ID)
}
func (s *Server) handleUpdateMenuItem(w http.ResponseWriter, r *http.Request) {
	var request updateMenuItemRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	menu, err := s.menus.UpdateItem(r.PathValue("id"), r.PathValue("item"), menus.UpdateItemInput{ExpectedRevision: request.Revision, ParentID: request.ParentID, TargetKind: request.TargetKind, URL: request.URL, OpenInNew: request.OpenInNew, Order: request.Order})
	if err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, menu, "Menu", menu.ID)
}
func (s *Server) handleUpdateMenuItemLocale(w http.ResponseWriter, r *http.Request) {
	var request updateMenuLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.menus.UpdateItemLocale(r.PathValue("id"), r.PathValue("item"), r.PathValue("locale"), menus.UpdateLocaleInput{ExpectedRevision: request.Revision, Label: request.Label})
	if err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, "Menu", item.ID)
}
func (s *Server) handleDeleteMenu(w http.ResponseWriter, r *http.Request) {
	var request deleteResourceRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		s.writeMenuError(w, err)
		return
	}
	if site.PrimaryMenu == r.PathValue("id") {
		s.writeError(w, http.StatusConflict, "menu_primary", "Choose another primary menu before deleting this one.", nil)
		return
	}
	if err := s.menus.Delete(r.PathValue("id"), request.Revision); err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writePublishedDeletion(w, r, "Menu", r.PathValue("id"))
}
func (s *Server) handleDeleteMenuItem(w http.ResponseWriter, r *http.Request) {
	var request deleteResourceRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	menu, err := s.menus.DeleteItem(r.PathValue("id"), r.PathValue("item"), request.Revision)
	if err != nil {
		s.writeMenuError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, menu, "Menu", menu.ID)
}
func (s *Server) writeMenuError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, menus.ErrNotFound):
		s.writeError(w, 404, "menu_not_found", "The menu does not exist.", nil)
	case errors.Is(err, menus.ErrConflict):
		s.writeError(w, 409, "revision_conflict", "The menu changed after it was loaded.", nil)
	case errors.Is(err, content.ErrAlreadyExists):
		s.writeError(w, 409, "menu_id_exists", "This ID already exists.", nil)
	case errors.Is(err, content.ErrInvalidID), errors.Is(err, content.ErrLocaleDisabled), errors.Is(err, menus.ErrInvalid):
		s.writeError(w, 422, "menu_invalid", "The menu is invalid.", nil)
	case errors.Is(err, menus.ErrInUse):
		s.writeError(w, 409, "menu_item_in_use", "Delete child menu items before deleting their parent.", nil)
	default:
		s.logger.Error("menu operation failed", "error", err)
		s.writeError(w, 500, "menus_unavailable", "Menus are unavailable.", nil)
	}
}
