// Package i18n normalizes configured locales and resolves their URL forms.
package i18n

import (
	"fmt"
	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/model"
	"golang.org/x/text/language"
	"strings"
)

type Registry struct {
	locales                     []model.Locale
	prefix                      map[string]model.Locale
	canonical                   map[string]model.Locale
	aliases                     map[string]model.Locale
	countries                   map[string]model.Locale
	DefaultLocale, SourceLocale model.Locale
}

func Canonical(s string) (model.Locale, bool) {
	tag, err := language.Parse(s)
	if err != nil {
		return "", false
	}
	v := tag.String()
	if v == "und" {
		return "", false
	}
	return model.Locale(v), true
}
func New(cfg config.I18nConfig) (*Registry, error) {
	r := &Registry{prefix: map[string]model.Locale{}, canonical: map[string]model.Locale{}, aliases: map[string]model.Locale{}, countries: map[string]model.Locale{}, DefaultLocale: model.Locale(cfg.DefaultLocale), SourceLocale: model.Locale(cfg.SourceLocale)}
	for _, item := range cfg.Locales {
		if !item.Enabled {
			continue
		}
		loc, ok := Canonical(item.Code)
		if !ok {
			return nil, fmt.Errorf("invalid enabled locale %q", item.Code)
		}
		r.locales = append(r.locales, loc)
		r.canonical[strings.ToLower(string(loc))] = loc
		p := strings.ToLower(item.URLPrefix)
		if p == "" {
			p = strings.ToLower(string(loc))
		}
		if _, exists := r.prefix[p]; exists {
			return nil, fmt.Errorf("duplicate locale URL prefix %q", p)
		}
		r.prefix[p] = loc
	}
	for alias, target := range cfg.LocaleAliases {
		if a, ok := Canonical(alias); ok {
			if t, ok := Canonical(target); ok {
				r.aliases[strings.ToLower(string(a))] = t
			}
		}
	}
	for cc, target := range cfg.CountryLocaleMap {
		if t, ok := Canonical(target); ok {
			r.countries[strings.ToUpper(cc)] = t
		}
	}
	return r, nil
}
func (r *Registry) Enabled(loc model.Locale) bool {
	for _, v := range r.locales {
		if v == loc {
			return true
		}
	}
	return false
}
func (r *Registry) URLPrefix(loc model.Locale) string {
	for p, v := range r.prefix {
		if v == loc {
			return p
		}
	}
	return strings.ToLower(string(loc))
}
func (r *Registry) FromPrefix(p string) (model.Locale, bool) {
	v, ok := r.prefix[strings.ToLower(p)]
	return v, ok
}
func (r *Registry) Canonical(s string) (model.Locale, bool) {
	v, ok := r.canonical[strings.ToLower(s)]
	return v, ok
}
func (r *Registry) Alias(s string) (model.Locale, bool) {
	v, ok := r.aliases[strings.ToLower(s)]
	return v, ok
}
func (r *Registry) CountryLocale(c string) (model.Locale, bool) {
	v, ok := r.countries[strings.ToUpper(c)]
	return v, ok
}
func (r *Registry) Locales() []model.Locale { return append([]model.Locale(nil), r.locales...) }
