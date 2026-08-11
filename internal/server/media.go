package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/media"
)

var publicMediaPathPattern = regexp.MustCompile(`^[0-9]{4}/(?:0[1-9]|1[0-2])/[0-9a-f]{32}\.(?:jpg|png|gif|webp)$`)

func (s *Server) handleListMedia(w http.ResponseWriter, _ *http.Request) {
	assets, err := s.media.List()
	if err != nil {
		s.logger.Error("list media failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "media_unavailable", "Cannot list media.", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"page": 1, "size": len(assets), "total": len(assets), "items": assets})
}

func (s *Server) handleCreateMedia(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadSize+(1<<20))
	if err := r.ParseMultipartForm(media.MaxUploadSize); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "media_too_large", "The image exceeds the 20 MiB upload limit.", nil)
		} else {
			s.writeError(w, http.StatusBadRequest, "multipart_invalid", "The multipart upload is invalid.", nil)
		}
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "media_missing", "A multipart image field named file is required.", nil)
		return
	}
	defer file.Close()
	if header.Size > media.MaxUploadSize {
		s.writeError(w, http.StatusRequestEntityTooLarge, "media_too_large", "The image exceeds the 20 MiB upload limit.", nil)
		return
	}
	asset, err := s.media.Create(header.Filename, file)
	if err != nil {
		s.writeMediaError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, asset)
}

func (s *Server) handleDeleteMedia(w http.ResponseWriter, r *http.Request) {
	if err := s.media.Delete(r.PathValue("id")); err != nil {
		s.writeMediaError(w, err)
		return
	}
	s.recordSecurityEvent(r, "media-delete", "succeeded", adminActor(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePublicMedia(w http.ResponseWriter, r *http.Request) {
	relative := strings.TrimPrefix(r.PathValue("path"), "/")
	if !publicMediaPathPattern.MatchString(relative) {
		http.NotFound(w, r)
		return
	}
	data, err := s.repository.ReadFile(filepath.Join("media", "originals", filepath.FromSlash(relative)))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mediaTypeFromExtension(filepath.Ext(relative)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrFileTooLarge):
		s.writeError(w, http.StatusRequestEntityTooLarge, "media_too_large", "The image exceeds the 20 MiB upload limit.", nil)
	case errors.Is(err, media.ErrEmptyFile):
		s.writeError(w, http.StatusUnprocessableEntity, "media_empty", "The image is empty.", nil)
	case errors.Is(err, media.ErrUnsupportedType):
		s.writeError(w, http.StatusUnsupportedMediaType, "media_type_unsupported", "Only JPEG, PNG, GIF, and WebP images are supported.", nil)
	case errors.Is(err, media.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "media_not_found", "The media asset does not exist.", nil)
	case errors.Is(err, media.ErrInUse):
		s.writeError(w, http.StatusConflict, "media_in_use", "The media asset is referenced by permanent content or history.", nil)
	default:
		s.logger.Error("media upload failed", "error", err)
		s.writeError(w, http.StatusInternalServerError, "media_upload_failed", "Cannot store the image.", nil)
	}
}

func mediaTypeFromExtension(extension string) string {
	switch strings.ToLower(extension) {
	case ".jpg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
