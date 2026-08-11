package server

import (
	"errors"
	"net/http"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/links"
)

type createLinkGroupRequest struct {
	ID, Name, Description string
	Order                 int
}
type createLinkRequest struct {
	ID, GroupID, URL, Logo, Name, Description string
	Order                                     int
}
type updateLinkLocaleRequest struct {
	Revision          int
	Name, Description string
}
type updateLinkGroupRequest struct {
	Revision int
	Order    int
}
type updateLinkRequest struct {
	Revision           int
	GroupID, URL, Logo string
	Order              int
}

func (s *Server) handleListLinks(w http.ResponseWriter, _ *http.Request) {
	groups, err := s.links.ListGroups()
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	items, err := s.links.ListLinks()
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"groups": groups, "items": items})
}
func (s *Server) handleCreateLinkGroup(w http.ResponseWriter, r *http.Request) {
	var request createLinkGroupRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.links.CreateGroup(links.CreateGroupInput{ID: request.ID, Name: request.Name, Description: request.Description, Order: request.Order})
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusCreated, item, "LinkGroup", item.ID)
}
func (s *Server) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	var request createLinkRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.links.CreateLink(links.CreateLinkInput{ID: request.ID, GroupID: request.GroupID, URL: request.URL, Logo: request.Logo, Name: request.Name, Description: request.Description, Order: request.Order})
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusCreated, item, "Link", item.ID)
}
func (s *Server) handleUpdateLinkGroup(w http.ResponseWriter, r *http.Request) {
	var request updateLinkGroupRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.links.UpdateGroup(r.PathValue("id"), links.UpdateGroupInput{ExpectedRevision: request.Revision, Order: request.Order})
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, "LinkGroup", item.ID)
}
func (s *Server) handleUpdateLink(w http.ResponseWriter, r *http.Request) {
	var request updateLinkRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.links.UpdateLink(r.PathValue("id"), links.UpdateLinkInput{ExpectedRevision: request.Revision, GroupID: request.GroupID, URL: request.URL, Logo: request.Logo, Order: request.Order})
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, "Link", item.ID)
}
func (s *Server) handleUpdateLinkGroupLocale(w http.ResponseWriter, r *http.Request) {
	var request updateLinkLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.links.UpdateGroupLocale(r.PathValue("id"), r.PathValue("locale"), links.UpdateLocaleInput{ExpectedRevision: request.Revision, Name: request.Name, Description: request.Description})
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, "LinkGroup", item.ID)
}
func (s *Server) handleUpdateLinkLocale(w http.ResponseWriter, r *http.Request) {
	var request updateLinkLocaleRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, 400, "invalid_json", "The request body is invalid.", nil)
		return
	}
	item, err := s.links.UpdateLinkLocale(r.PathValue("id"), r.PathValue("locale"), links.UpdateLocaleInput{ExpectedRevision: request.Revision, Name: request.Name, Description: request.Description})
	if err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedResource(w, r, http.StatusOK, item, "Link", item.ID)
}
func (s *Server) handleDeleteLinkGroup(w http.ResponseWriter, r *http.Request) {
	var request deleteResourceRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if err := s.links.DeleteGroup(r.PathValue("id"), request.Revision); err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedDeletion(w, r, "LinkGroup", r.PathValue("id"))
}
func (s *Server) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	var request deleteResourceRequest
	if !decodeJSON(w, r, &request) {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "The request body is invalid.", nil)
		return
	}
	if err := s.links.DeleteLink(r.PathValue("id"), request.Revision); err != nil {
		s.writeLinkError(w, err)
		return
	}
	s.writePublishedDeletion(w, r, "Link", r.PathValue("id"))
}
func (s *Server) writeLinkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, links.ErrNotFound):
		s.writeError(w, 404, "link_not_found", "The link resource does not exist.", nil)
	case errors.Is(err, links.ErrConflict):
		s.writeError(w, 409, "revision_conflict", "The link resource changed after it was loaded.", nil)
	case errors.Is(err, content.ErrAlreadyExists):
		s.writeError(w, 409, "link_id_exists", "This ID already exists.", nil)
	case errors.Is(err, content.ErrInvalidID), errors.Is(err, content.ErrLocaleDisabled), errors.Is(err, links.ErrInvalid):
		s.writeError(w, 422, "link_invalid", "The link resource is invalid.", nil)
	case errors.Is(err, links.ErrInUse):
		s.writeError(w, 409, "link_group_in_use", "Delete or move every link in this group first.", nil)
	default:
		s.logger.Error("link operation failed", "error", err)
		s.writeError(w, 500, "links_unavailable", "Links are unavailable.", nil)
	}
}
