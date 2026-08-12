package server

import (
	"net/http"
	"strings"

	"github.com/FengYuchen1314/mutiblog/internal/localeconfig"
	"golang.org/x/text/language"
)

func (s *Server) requireEditableContentLocale(w http.ResponseWriter, r *http.Request) bool {
	tag, err := language.Parse(strings.TrimSpace(r.PathValue("locale")))
	if err == nil && tag.String() == localeconfig.FixedSourceLocale {
		return true
	}
	s.writeError(w, http.StatusForbidden, "content_locale_ai_managed", "Only Simplified Chinese source content can be edited manually. Other locales are managed by AI translation.", nil)
	return false
}
