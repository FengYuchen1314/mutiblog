// Package importer converts external Markdown into canonical article bundles.
package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/index"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
	"gopkg.in/yaml.v3"
)

type File struct {
	Name    string
	Data    []byte
	ModTime time.Time
}
type Report struct {
	Found    int      `json:"found"`
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Failures []string `json:"failures,omitempty"`
}
type Options struct {
	Locale                model.Locale
	Status                model.Status
	DryRun                bool
	CreateMissingTaxonomy bool
}
type Service struct {
	Content  *content.Store
	Index    *index.Index
	Taxonomy *taxonomy.Store
}
type frontMatter struct {
	Title, Slug, Description, Author string
	Date                             time.Time `yaml:"date"`
	Status                           model.Status
	Categories, Tags                 []string
}

func (s Service) Run(ctx context.Context, files []File, opts Options) (Report, error) {
	if s.Content == nil || s.Index == nil || s.Taxonomy == nil {
		return Report{}, errors.New("import service is not configured")
	}
	if opts.Locale == "" {
		return Report{}, errors.New("import locale is required")
	}
	report, used := Report{Found: len(files)}, map[string]bool{}
	data := s.Taxonomy.LoadAll()
	for _, article := range s.Index.Articles() {
		if version := article.Versions[opts.Locale]; version != nil {
			used[version.Front.Slug] = true
		}
	}
	for _, file := range files {
		front, body, err := ParseMarkdown(file, opts.Locale, opts.Status)
		if err != nil {
			report.Skipped++
			report.Failures = append(report.Failures, file.Name+": "+err.Error())
			continue
		}
		front.Slug = uniqueImportSlug(front.Slug, used)
		used[front.Slug] = true
		front.Categories, _, err = resolveImportCategories(
			front.Categories,
			opts.Locale,
			data.Categories,
			s.Taxonomy,
			opts.DryRun,
			opts.CreateMissingTaxonomy,
		)
		if err == nil {
			front.Tags, _, err = resolveImportTags(
				front.Tags,
				opts.Locale,
				data.Tags,
				s.Taxonomy,
				opts.DryRun,
				opts.CreateMissingTaxonomy,
			)
		}
		if err != nil {
			report.Skipped++
			report.Failures = append(report.Failures, file.Name+": "+err.Error())
			continue
		}
		if opts.DryRun {
			report.Imported++
			continue
		}
		if _, err = s.Content.CreateBundle(model.ContentPost, opts.Locale, front, body); err != nil {
			report.Skipped++
			report.Failures = append(report.Failures, file.Name+": "+err.Error())
			continue
		}
		report.Imported++
	}
	if !opts.DryRun && report.Imported > 0 {
		if err := s.Index.RebuildAll(ctx, s.Content, s.Taxonomy); err != nil {
			return report, fmt.Errorf("rebuild import index: %w", err)
		}
	}
	if report.Imported == 0 && report.Skipped > 0 {
		return report, errors.New("import: no files were accepted")
	}
	return report, nil
}

func resolveImportCategories(
	values []string,
	locale model.Locale,
	existing map[string]*model.Category,
	store *taxonomy.Store,
	dryRun, createMissing bool,
) ([]string, []string, error) {
	resolved, created := make([]string, 0, len(values)), []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id := findImportCategory(value, locale, existing)
		if id == "" {
			if !createMissing {
				return nil, created, fmt.Errorf("missing category %q", value)
			}
			id = uniqueTaxonomyID(importSlug(value), existing)
			category := &model.Category{ID: id, Slug: id, Name: model.LocalizedString{"": value}}
			if !dryRun {
				if err := store.SaveCategory(category); err != nil {
					return nil, created, err
				}
			}
			existing[id] = category
			created = append(created, "category "+id)
		}
		if !seen[id] {
			resolved = append(resolved, id)
			seen[id] = true
		}
	}
	return resolved, created, nil
}

func resolveImportTags(
	values []string,
	locale model.Locale,
	existing map[string]*model.Tag,
	store *taxonomy.Store,
	dryRun, createMissing bool,
) ([]string, []string, error) {
	resolved, created := make([]string, 0, len(values)), []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id := findImportTag(value, locale, existing)
		if id == "" {
			if !createMissing {
				return nil, created, fmt.Errorf("missing tag %q", value)
			}
			id = uniqueTagID(importSlug(value), existing)
			tag := &model.Tag{ID: id, Slug: id, Name: model.LocalizedString{"": value}}
			if !dryRun {
				if err := store.SaveTag(tag); err != nil {
					return nil, created, err
				}
			}
			existing[id] = tag
			created = append(created, "tag "+id)
		}
		if !seen[id] {
			resolved = append(resolved, id)
			seen[id] = true
		}
	}
	return resolved, created, nil
}
func findImportCategory(value string, locale model.Locale, existing map[string]*model.Category) string {
	if _, ok := existing[value]; ok {
		return value
	}
	for id, item := range existing {
		if item.Name.Get(locale, "") == value || item.Name.Get("", "") == value {
			return id
		}
	}
	return ""
}
func findImportTag(value string, locale model.Locale, existing map[string]*model.Tag) string {
	if _, ok := existing[value]; ok {
		return value
	}
	for id, item := range existing {
		if item.Name.Get(locale, "") == value || item.Name.Get("", "") == value {
			return id
		}
	}
	return ""
}
func uniqueTaxonomyID(base string, existing map[string]*model.Category) string {
	if base == "" {
		base = "category"
	}
	if _, found := existing[base]; !found {
		return base
	}
	for n := 2; ; n++ {
		id := fmt.Sprintf("%s-%d", base, n)
		if _, found := existing[id]; !found {
			return id
		}
	}
}
func uniqueTagID(base string, existing map[string]*model.Tag) string {
	if base == "" {
		base = "tag"
	}
	if _, found := existing[base]; !found {
		return base
	}
	for n := 2; ; n++ {
		id := fmt.Sprintf("%s-%d", base, n)
		if _, found := existing[id]; !found {
			return id
		}
	}
}

func ReadPath(path string) ([]File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() && strings.EqualFold(filepath.Ext(path), ".zip") {
		archive, err := zip.OpenReader(path)
		if err != nil {
			return nil, err
		}
		defer archive.Close()
		var files []File
		for _, entry := range archive.File {
			if entry.FileInfo().IsDir() || !strings.EqualFold(filepath.Ext(entry.Name), ".md") {
				continue
			}
			reader, err := entry.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(io.LimitReader(reader, 10<<20))
			closeErr := reader.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			files = append(files, File{Name: filepath.ToSlash(entry.Name), Data: data, ModTime: entry.Modified})
		}
		return files, nil
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil, errors.New("import input must be a .md file, directory, or .zip archive")
		}
		data, err := os.ReadFile(path)
		return []File{{Name: filepath.Base(path), Data: data, ModTime: info.ModTime()}}, err
	}
	var files []File
	err = filepath.WalkDir(path, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(path, filePath)
		files = append(files, File{Name: filepath.ToSlash(rel), Data: data, ModTime: entryInfo.ModTime()})
		return nil
	})
	return files, err
}

func ParseMarkdown(file File, locale model.Locale, forcedStatus model.Status) (model.FrontMatter, string, error) {
	body := string(file.Data)
	front := model.FrontMatter{
		Title:        strings.TrimSuffix(filepath.Base(file.Name), filepath.Ext(file.Name)),
		Author:       "import",
		SourceLocale: locale,
		Locale:       locale,
		Status:       model.StatusDraft,
		Date:         file.ModTime,
	}
	if front.Date.IsZero() {
		front.Date = time.Now()
	}
	if yml, parsedBody, err := content.SplitFrontMatter(file.Data); err == nil {
		var supplied frontMatter
		if err := yaml.Unmarshal(yml, &supplied); err != nil {
			return front, "", fmt.Errorf("invalid YAML front matter: %w", err)
		}
		applyImportedFront(&front, supplied)
		body = parsedBody
	} else if toml, parsedBody, ok := splitTOMLFrontMatter(file.Data); ok {
		supplied, err := parseHugoTOML(toml)
		if err != nil {
			return front, "", fmt.Errorf("invalid Hugo TOML front matter: %w", err)
		}
		applyImportedFront(&front, supplied)
		body = parsedBody
	}
	if forcedStatus != "" {
		front.Status = forcedStatus
	}
	if front.Status != model.StatusDraft && front.Status != model.StatusPublished {
		front.Status = model.StatusDraft
	}
	front.Slug = importSlug(front.Slug)
	if front.Slug == "" {
		front.Slug = importSlug(front.Title)
	}
	if front.Slug == "" {
		return front, "", errors.New("could not derive a slug")
	}
	return front, body, nil
}
func applyImportedFront(front *model.FrontMatter, supplied frontMatter) {
	if supplied.Title != "" {
		front.Title = supplied.Title
	}
	front.Slug, front.Description, front.Author = supplied.Slug, supplied.Description, supplied.Author
	if front.Author == "" {
		front.Author = "import"
	}
	if !supplied.Date.IsZero() {
		front.Date = supplied.Date
	}
	if supplied.Status != "" {
		front.Status = supplied.Status
	}
	front.Categories, front.Tags = supplied.Categories, supplied.Tags
}

func splitTOMLFrontMatter(raw []byte) (string, string, bool) {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(raw, []byte("+++\n")) {
		return "", "", false
	}
	rest := raw[4:]
	end := bytes.Index(rest, []byte("\n+++\n"))
	if end < 0 {
		return "", "", false
	}
	body := string(rest[end+5:])
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return string(rest[:end]), body, true
}

func parseHugoTOML(raw string) (frontMatter, error) {
	var out frontMatter
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return out, errors.New("expected key = value")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		value = strings.Trim(value, " \t\"")
		switch key {
		case "title":
			out.Title = value
		case "slug":
			out.Slug = value
		case "description":
			out.Description = value
		case "author":
			out.Author = value
		case "status":
			out.Status = model.Status(value)
		case "draft":
			if value == "true" {
				out.Status = model.StatusDraft
			} else if value == "false" {
				out.Status = model.StatusPublished
			}
		case "date", "publishDate":
			parsed, err := parseImportDate(value)
			if err != nil {
				return out, err
			}
			out.Date = parsed
		case "categories":
			out.Categories = parseTOMLStrings(value)
		case "tags":
			out.Tags = parseTOMLStrings(value)
		}
	}
	return out, nil
}
func parseTOMLStrings(value string) []string {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if value == "" {
		return nil
	}
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.Trim(strings.TrimSpace(item), "\""); item != "" {
			out = append(out, item)
		}
	}
	return out
}
func parseImportDate(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date %q", value)
}
func importSlug(value string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
func uniqueImportSlug(slug string, used map[string]bool) string {
	if !used[slug] {
		return slug
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", slug, n)
		if !used[candidate] {
			return candidate
		}
	}
}
