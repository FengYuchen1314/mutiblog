package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/fengyuchen/mutiblog/internal/app"
	"github.com/fengyuchen/mutiblog/internal/auth"
	"github.com/fengyuchen/mutiblog/internal/backup"
	"github.com/fengyuchen/mutiblog/internal/config"
	"github.com/fengyuchen/mutiblog/internal/content"
	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"github.com/fengyuchen/mutiblog/internal/importer"
	"github.com/fengyuchen/mutiblog/internal/migrate"
	"github.com/fengyuchen/mutiblog/internal/model"
	"github.com/fengyuchen/mutiblog/internal/taxonomy"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var version = "dev"

type options struct {
	root, config string
	port         int
	dev          bool
}

func main() {
	opts, args, err := parseOptions(os.Args[1:])
	if err != nil {
		fatal(err)
	}
	if len(args) > 0 && args[0] != "serve" {
		if err := command(opts, args); err != nil {
			fatal(err)
		}
		return
	}
	if err := serve(opts); err != nil {
		fatal(err)
	}
}

func parseOptions(args []string) (options, []string, error) {
	root, err := os.Getwd()
	if err != nil {
		return options{}, nil, err
	}
	result := options{root: root}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--root":
			i++
			if i >= len(args) {
				return result, nil, errors.New("--root requires a value")
			}
			result.root = args[i]
		case "--config":
			i++
			if i >= len(args) {
				return result, nil, errors.New("--config requires a value")
			}
			result.config = args[i]
		case "--port":
			i++
			if i >= len(args) {
				return result, nil, errors.New("--port requires a value")
			}
			if _, err := fmt.Sscan(args[i], &result.port); err != nil {
				return result, nil, err
			}
		case "--dev":
			result.dev = true
		default:
			rest = append(rest, args[i])
		}
	}
	result.root, err = filepath.Abs(result.root)
	return result, rest, err
}

func command(opts options, args []string) error {
	switch args[0] {
	case "version":
		if len(args) == 2 && args[1] == "--json" {
			return json.NewEncoder(os.Stdout).Encode(map[string]string{"version": version})
		}
		if len(args) != 1 {
			return errors.New("usage: blog version [--json]")
		}
		fmt.Println(version)
		return nil
	case "config":
		return configCommand(opts, args[1:])
	case "doctor":
		instance, warnings, err := app.New(app.Options{Root: opts.root, ConfigFile: opts.config})
		if err != nil {
			return err
		}
		defer instance.Close()
		fmt.Println("configuration: ok")
		fmt.Println("content index:", instance.Index.Stats().Articles, "articles")
		fmt.Println("state database: ok")
		for _, warning := range warnings {
			fmt.Println("warning:", warning)
		}
		return nil
	case "rebuild":
		if len(args) != 1 {
			return errors.New("usage: blog rebuild")
		}
		instance, _, err := app.New(app.Options{Root: opts.root, ConfigFile: opts.config})
		if err != nil {
			return err
		}
		defer instance.Close()
		if err = instance.Rebuild(context.Background()); err != nil {
			return err
		}
		fmt.Println("rebuild complete:", instance.Render.Output())
		return nil
	case "verify":
		fix := len(args) == 2 && args[1] == "--fix"
		if len(args) > 2 || (len(args) == 2 && !fix) {
			return errors.New("usage: blog verify [--fix]")
		}
		return verifyCommand(opts, fix)
	case "index":
		if len(args) != 2 || (args[1] != "errors" && args[1] != "rebuild") {
			return errors.New("usage: blog index errors | rebuild")
		}
		instance, _, err := app.New(app.Options{Root: opts.root, ConfigFile: opts.config})
		if err != nil {
			return err
		}
		defer instance.Close()
		if args[1] == "rebuild" {
			if err := instance.Index.RebuildAll(context.Background(), instance.Content, instance.Taxonomy); err != nil {
				return err
			}
			fmt.Println("index rebuild complete:", instance.Index.Stats().Articles, "articles")
			return nil
		}
		errs := instance.Index.Errors()
		if len(errs) == 0 {
			fmt.Println("index: no parse errors")
			return nil
		}
		for _, item := range errs {
			fmt.Println(item.Path+":", item.Err)
		}
		return fmt.Errorf("index: found %d parse errors", len(errs))
	case "admin":
		return adminCommand(opts, args[1:])
	case "backup":
		return backupCommand(opts, args[1:])
	case "export":
		return exportCommand(opts, args[1:])
	case "import":
		return importCommand(opts, args[1:])
	case "migrate":
		return migrateCommand(opts, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func migrateCommand(opts options, args []string) error {
	dryRun, target := false, migrate.CurrentContentSchema
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--to":
			i++
			if i >= len(args) {
				return errors.New("--to requires a positive schema version")
			}
			if _, err := fmt.Sscan(args[i], &target); err != nil || target < 1 {
				return errors.New("--to requires a positive schema version")
			}
		default:
			return fmt.Errorf("unknown migrate option %q", args[i])
		}
	}
	if target > migrate.CurrentContentSchema {
		return fmt.Errorf("cannot migrate to schema %d; this binary supports %d", target, migrate.CurrentContentSchema)
	}
	cfg, _, err := config.Load(opts.root, opts.config)
	if err != nil {
		return err
	}
	if cfg.SchemaVersion > migrate.CurrentContentSchema {
		return fmt.Errorf("content schema %d is newer than this binary (supports %d); upgrade the application or restore a backup", cfg.SchemaVersion, migrate.CurrentContentSchema)
	}
	if cfg.SchemaVersion >= target {
		fmt.Printf("migrate: schema %d is already at target %d\n", cfg.SchemaVersion, target)
		return nil
	}
	// Always start from the dry-run traversal. It both makes the preview exact
	// and ensures a backup is created before the first content file is touched.
	report, err := migrate.Run(context.Background(), content.NewStore(cfg.Paths.Content), cfg.SchemaVersion, target, true)
	if err != nil {
		return err
	}
	for _, change := range report.Changed {
		fmt.Println("would migrate:", change)
	}
	if dryRun {
		fmt.Printf("migrate: schema %d -> %d; %d files would change\n", report.From, report.To, len(report.Changed))
		return nil
	}
	backupItem, err := backup.Create(backup.Options{Root: opts.root, Content: cfg.Paths.Content, Data: cfg.Paths.Data, Config: filepath.Join(opts.root, "config"), Media: cfg.Paths.Media, OutputDir: cfg.Backup.Dir, IncludeMedia: cfg.Backup.IncludeMedia, Keep: cfg.Backup.Keep, Name: "backup-" + time.Now().UTC().Format("20060102-150405") + "-pre-migration-v" + fmt.Sprint(target) + ".zip"})
	if err != nil {
		return fmt.Errorf("pre-migration backup failed: %w", err)
	}
	report, err = migrate.Run(context.Background(), content.NewStore(cfg.Paths.Content), cfg.SchemaVersion, target, false)
	if err != nil {
		return fmt.Errorf("migration failed after backup %s: %w", backupItem.Path, err)
	}
	if _, err := config.UpdateSchemaVersion(opts.root, opts.config, target); err != nil {
		return fmt.Errorf("migration content was applied but schema marker could not be updated; restore %s or rerun: %w", backupItem.Path, err)
	}
	fmt.Printf("migrate: schema %d -> %d; changed=%d; backup=%s\n", report.From, report.To, len(report.Changed), backupItem.Path)
	return nil
}

type importFile struct {
	Name    string
	Data    []byte
	ModTime time.Time
}
type importReport struct {
	Found, Imported, Skipped int
	Failures                 []string
}
type importedFrontMatter struct {
	Title, Slug, Description, Author string
	Date                             time.Time `yaml:"date"`
	Status                           model.Status
	Categories, Tags                 []string
}

// importCommand converts external Markdown into canonical article bundles. It
// shares the exact parser and validation path used by the authenticated import API.
func importCommand(opts options, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: blog import <path.md|directory|archive.zip> [--dry-run] [--locale L] [--status draft|published]")
	}
	path, dryRun, locale := args[0], false, model.Locale("")
	var forcedStatus model.Status
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dryRun = true
		case "--locale":
			i++
			if i >= len(args) || strings.TrimSpace(args[i]) == "" {
				return errors.New("--locale requires a value")
			}
			locale = model.Locale(args[i])
		case "--status":
			i++
			if i >= len(args) || (args[i] != "draft" && args[i] != "published") {
				return errors.New("--status must be draft or published")
			}
			forcedStatus = model.Status(args[i])
		default:
			return fmt.Errorf("unknown import option %q", args[i])
		}
	}
	files, err := importer.ReadPath(path)
	if err != nil {
		return err
	}
	instance, _, err := app.New(app.Options{Root: opts.root, ConfigFile: opts.config})
	if err != nil {
		return err
	}
	defer instance.Close()
	if locale == "" {
		locale = model.Locale(instance.Config.I18n.SourceLocale)
	}
	validLocale := false
	for _, configured := range instance.Config.I18n.Locales {
		if configured.Code == string(locale) && configured.Enabled {
			validLocale = true
			break
		}
	}
	if !validLocale {
		return fmt.Errorf("import locale %q is not an enabled configured locale", locale)
	}
	report, runErr := (importer.Service{Content: instance.Content, Index: instance.Index, Taxonomy: instance.Taxonomy}).Run(context.Background(), files, importer.Options{Locale: locale, Status: forcedStatus, DryRun: dryRun, CreateMissingTaxonomy: true})
	fmt.Printf("import: found=%d imported=%d skipped=%d dryRun=%t\n", report.Found, report.Imported, report.Skipped, dryRun)
	for _, failure := range report.Failures {
		fmt.Fprintln(os.Stderr, "skipped:", failure)
	}
	return runErr
}

func resolveImportCategories(values []string, locale model.Locale, existing map[string]*model.Category, store *taxonomy.Store, dryRun bool) ([]string, []string, error) {
	resolved, created := make([]string, 0, len(values)), []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id := findImportCategory(value, locale, existing)
		if id == "" {
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
func resolveImportTags(values []string, locale model.Locale, existing map[string]*model.Tag, store *taxonomy.Store, dryRun bool) ([]string, []string, error) {
	resolved, created := make([]string, 0, len(values)), []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id := findImportTag(value, locale, existing)
		if id == "" {
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

func readImportFiles(path string) ([]importFile, error) {
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
		var files []importFile
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
			files = append(files, importFile{Name: filepath.ToSlash(entry.Name), Data: data, ModTime: entry.Modified})
		}
		return files, nil
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil, errors.New("import input must be a .md file, directory, or .zip archive")
		}
		data, err := os.ReadFile(path)
		return []importFile{{Name: filepath.Base(path), Data: data, ModTime: info.ModTime()}}, err
	}
	var files []importFile
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
		files = append(files, importFile{Name: filepath.ToSlash(rel), Data: data, ModTime: entryInfo.ModTime()})
		return nil
	})
	return files, err
}

func importMarkdown(file importFile, locale model.Locale, forcedStatus model.Status) (model.FrontMatter, string, error) {
	body := string(file.Data)
	front := model.FrontMatter{Title: strings.TrimSuffix(filepath.Base(file.Name), filepath.Ext(file.Name)), Author: "import", SourceLocale: locale, Locale: locale, Status: model.StatusDraft, Date: file.ModTime}
	if front.Date.IsZero() {
		front.Date = time.Now()
	}
	if yml, parsedBody, err := content.SplitFrontMatter(file.Data); err == nil {
		var supplied importedFrontMatter
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
func applyImportedFront(front *model.FrontMatter, supplied importedFrontMatter) {
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

func parseHugoTOML(raw string) (importedFrontMatter, error) {
	var out importedFrontMatter
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

func configCommand(opts options, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: blog config validate | get <path> | set <path> <value>")
	}
	switch args[0] {
	case "validate":
		if len(args) != 1 {
			return errors.New("usage: blog config validate")
		}
		_, warnings, err := config.Load(opts.root, opts.config)
		if err != nil {
			return err
		}
		fmt.Println("configuration is valid")
		for _, warning := range warnings {
			fmt.Println("warning:", warning)
		}
		return nil
	case "get":
		if len(args) != 2 {
			return errors.New("usage: blog config get <path>")
		}
		value, err := configValue(opts, args[1])
		if err != nil {
			return err
		}
		if secretConfigPath(args[1]) {
			fmt.Println("[redacted]")
			return nil
		}
		encoded, err := yaml.Marshal(value)
		if err != nil {
			return err
		}
		fmt.Print(string(encoded))
		return nil
	case "set":
		if len(args) != 3 {
			return errors.New("usage: blog config set <path> <value>")
		}
		if err := setConfigValue(opts, args[1], args[2]); err != nil {
			return err
		}
		fmt.Println("configuration updated:", args[1])
		return nil
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func configPath(opts options) string {
	if opts.config == "" {
		return filepath.Join(opts.root, "config", "config.yaml")
	}
	if filepath.IsAbs(opts.config) {
		return opts.config
	}
	return filepath.Join(opts.root, opts.config)
}

func configValue(opts options, dottedPath string) (*yaml.Node, error) {
	data, err := os.ReadFile(configPath(opts))
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	return yamlPath(&document, strings.Split(dottedPath, "."), false)
}

func setConfigValue(opts options, dottedPath, rawValue string) error {
	path := configPath(opts)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	target, err := yamlPath(&document, strings.Split(dottedPath, "."), true)
	if err != nil {
		return err
	}
	value, err := parseYAMLValue(rawValue)
	if err != nil {
		return fmt.Errorf("parse value: %w", err)
	}
	*target = *value
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	// Validate a sibling candidate before replacing the live configuration. This
	// keeps a typo in `config set` from ever being observed by a running
	// fsnotify watcher as an invalid config file.
	candidate, err := os.CreateTemp(filepath.Dir(path), ".config-validate-*.yaml")
	if err != nil {
		return err
	}
	candidatePath := candidate.Name()
	if err := candidate.Close(); err != nil {
		_ = os.Remove(candidatePath)
		return err
	}
	defer os.Remove(candidatePath)
	if err := fsutil.AtomicWrite(candidatePath, encoded, 0o600); err != nil {
		return err
	}
	if _, _, err := config.Load(opts.root, candidatePath); err != nil {
		return fmt.Errorf("configuration value is invalid: %w", err)
	}
	if err := fsutil.AtomicWrite(path, encoded, 0o600); err != nil {
		return err
	}
	return nil
}

func parseYAMLValue(raw string) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte("value: "+raw), &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 || len(document.Content[0].Content) != 2 {
		return nil, errors.New("expected one YAML value")
	}
	return document.Content[0].Content[1], nil
}

// yamlPath walks mapping nodes only. Configuration lists deliberately remain
// whole-value settings so a CLI write cannot accidentally retarget an element
// after reordering locales or proxy lists.
func yamlPath(document *yaml.Node, parts []string, create bool) (*yaml.Node, error) {
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return nil, errors.New("configuration path is required")
	}
	current := document
	if current.Kind == yaml.DocumentNode {
		if len(current.Content) != 1 {
			return nil, errors.New("invalid YAML document")
		}
		current = current.Content[0]
	}
	for i, part := range parts {
		if current.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%q is not a mapping", strings.Join(parts[:i], "."))
		}
		var value *yaml.Node
		for j := 0; j < len(current.Content); j += 2 {
			if current.Content[j].Value == part {
				value = current.Content[j+1]
				break
			}
		}
		if value == nil {
			if !create {
				return nil, fmt.Errorf("configuration key %q was not found", strings.Join(parts[:i+1], "."))
			}
			current.Content = append(current.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: part}, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
			value = current.Content[len(current.Content)-1]
		}
		if i == len(parts)-1 {
			return value, nil
		}
		current = value
	}
	return nil, errors.New("configuration path is required")
}

func secretConfigPath(path string) bool {
	path = strings.ToLower(path)
	return strings.Contains(path, "secret") || strings.Contains(path, "password") || strings.Contains(path, "apikey") || strings.Contains(path, "api_key")
}

type outputDiff struct {
	Path string
	Kind string
}

func verifyCommand(opts options, fix bool) error {
	instance, _, err := app.New(app.Options{Root: opts.root, ConfigFile: opts.config})
	if err != nil {
		return err
	}
	defer instance.Close()
	current := instance.Render.Output()
	candidate := filepath.Join(filepath.Dir(current), ".verify-"+fmt.Sprint(time.Now().UnixNano()))
	defer os.RemoveAll(candidate)
	defer os.RemoveAll(candidate + ".releases")
	if err = instance.RebuildTo(context.Background(), candidate); err != nil {
		return fmt.Errorf("verify rebuild: %w", err)
	}
	diffs, err := compareTrees(current, candidate)
	if err != nil {
		return err
	}
	if len(diffs) == 0 {
		fmt.Println("verify: static output is current")
		return nil
	}
	for _, diff := range diffs {
		fmt.Printf("%s\t%s\n", diff.Kind, diff.Path)
	}
	if !fix {
		return fmt.Errorf("verify: found %d output differences (run with --fix to repair)", len(diffs))
	}
	for _, diff := range diffs {
		target := filepath.Join(current, filepath.FromSlash(diff.Path))
		source := filepath.Join(candidate, filepath.FromSlash(diff.Path))
		switch diff.Kind {
		case "missing", "changed":
			data, readErr := os.ReadFile(source)
			if readErr != nil {
				return readErr
			}
			if writeErr := fsutil.AtomicWrite(target, data, 0o644); writeErr != nil {
				return writeErr
			}
		case "extra":
			if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return removeErr
			}
		}
	}
	fmt.Printf("verify: repaired %d output differences\n", len(diffs))
	return nil
}

func compareTrees(current, candidate string) ([]outputDiff, error) {
	left, err := collectFiles(current)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	right, err := collectFiles(candidate)
	if err != nil {
		return nil, err
	}
	diffs := make([]outputDiff, 0)
	for path, before := range left {
		after, ok := right[path]
		if !ok {
			diffs = append(diffs, outputDiff{Path: path, Kind: "extra"})
		} else if !bytes.Equal(before, after) {
			diffs = append(diffs, outputDiff{Path: path, Kind: "changed"})
		}
	}
	for path := range right {
		if _, ok := left[path]; !ok {
			diffs = append(diffs, outputDiff{Path: path, Kind: "missing"})
		}
	}
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Path < diffs[j].Path || (diffs[i].Path == diffs[j].Path && diffs[i].Kind < diffs[j].Kind)
	})
	return diffs, nil
}

func collectFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	resolved, err := filepath.EvalSymlinks(root)
	if err == nil {
		root = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}
func backupCommand(opts options, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: blog backup create [--no-media] | list")
	}
	if args[0] == "restore" {
		if len(args) != 3 || args[2] != "--yes" {
			return errors.New("usage: blog backup restore <file> --yes")
		}
		archivePath := args[1]
		if !filepath.IsAbs(archivePath) {
			archivePath = filepath.Join(opts.root, archivePath)
		}
		return backup.Restore(backup.RestoreOptions{Root: opts.root, Archive: archivePath, Confirm: true})
	}
	cfg, _, err := config.Load(opts.root, opts.config)
	if err != nil {
		return err
	}
	backupOptions := backup.Options{Content: cfg.Paths.Content, Data: cfg.Paths.Data, Config: filepath.Join(opts.root, "config"), Media: cfg.Paths.Media, OutputDir: cfg.Backup.Dir, IncludeMedia: cfg.Backup.IncludeMedia, Keep: cfg.Backup.Keep}
	switch args[0] {
	case "create":
		if len(args) > 1 && args[1] == "--no-media" {
			backupOptions.IncludeMedia = false
		}
		item, err := backup.Create(backupOptions)
		if err != nil {
			return err
		}
		fmt.Println(item.Path)
		return nil
	case "list":
		items, err := backup.List(backupOptions.OutputDir)
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Printf("%s\t%d\n", item.Path, item.Size)
		}
		return nil
	default:
		return fmt.Errorf("unknown backup command %q", args[0])
	}
}

func exportCommand(opts options, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: blog export <path.zip> [--scope content,data,media,config]")
	}
	if len(args) != 1 && len(args) != 3 || len(args) == 3 && args[1] != "--scope" {
		return errors.New("usage: blog export <path.zip> [--scope content,data,media,config]")
	}
	target := args[0]
	if !filepath.IsAbs(target) {
		target = filepath.Join(opts.root, target)
	}
	if filepath.Ext(target) != ".zip" {
		return errors.New("export path must end in .zip")
	}
	scopes := map[string]bool{"content": true, "data": true, "media": true, "config": true}
	if len(args) == 3 {
		scopes = map[string]bool{}
		for _, raw := range strings.Split(args[2], ",") {
			scope := strings.TrimSpace(raw)
			if scope != "content" && scope != "data" && scope != "media" && scope != "config" {
				return fmt.Errorf("unknown export scope %q", scope)
			}
			scopes[scope] = true
		}
		if len(scopes) == 0 {
			return errors.New("export scope must include at least one of content,data,media,config")
		}
	}
	cfg, _, err := config.Load(opts.root, opts.config)
	if err != nil {
		return err
	}
	item, err := backup.Create(backup.Options{
		Root: opts.root, Content: cfg.Paths.Content, Data: cfg.Paths.Data, Config: filepath.Join(opts.root, "config"), Media: cfg.Paths.Media,
		OutputDir: filepath.Dir(target), Name: filepath.Base(target), Scopes: scopes, IncludeMedia: scopes["media"], Keep: cfg.Backup.Keep,
	})
	if err != nil {
		return err
	}
	fmt.Println(item.Path)
	return nil
}
func adminCommand(opts options, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: blog admin create | list | reset-password <username> [--stdin|--random] | unlock <username> | set-role <username> <role> | hash-password [--stdin]")
	}
	cfg, _, err := config.Load(opts.root, opts.config)
	if err != nil {
		return err
	}
	users, err := auth.OpenUsers(cfg.Paths.Data)
	if err != nil {
		return err
	}
	switch args[0] {
	case "create":
		if len(args) != 1 {
			return errors.New("usage: blog admin create")
		}
		reader := bufio.NewReader(os.Stdin)
		username, err := promptLine(reader, "Username: ")
		if err != nil {
			return err
		}
		email, err := promptLine(reader, "Email: ")
		if err != nil {
			return err
		}
		password, err := promptPassword(reader, "New password: ", false)
		if err != nil {
			return err
		}
		confirm, err := promptPassword(reader, "Confirm password: ", false)
		if err != nil {
			return err
		}
		if password != confirm {
			return errors.New("passwords do not match")
		}
		_, err = users.Create(username, email, password, "admin", cfg.I18n.DefaultLocale)
		return err
	case "list":
		for _, user := range users.List() {
			fmt.Printf("%s\t%s\t%s\n", user.Username, user.Role, user.Email)
		}
		return nil
	case "reset-password":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("usage: blog admin reset-password <username> [--stdin|--random]")
		}
		stdin := len(args) == 3 && args[2] == "--stdin"
		randomPassword := len(args) == 3 && args[2] == "--random"
		if len(args) == 3 && !stdin && !randomPassword {
			return errors.New("usage: blog admin reset-password <username> [--stdin|--random]")
		}
		password, err := passwordForCLI(bufio.NewReader(os.Stdin), stdin, randomPassword)
		if err != nil {
			return err
		}
		if err = users.ResetPassword(args[1], password); err != nil {
			return err
		}
		if randomPassword {
			fmt.Println("generated password:", password)
		}
		fmt.Println("password reset; all existing sessions are invalidated")
		return nil
	case "unlock":
		if len(args) != 2 {
			return errors.New("usage: blog admin unlock <username>")
		}
		if err := users.SetDisabled(args[1], false); err != nil {
			return err
		}
		fmt.Println("account enabled; any persisted lock state has been cleared")
		return nil
	case "set-role":
		if len(args) != 3 || !validRole(args[2]) {
			return errors.New("usage: blog admin set-role <username> <admin|editor|author|translator>")
		}
		return users.SetRole(args[1], args[2])
	case "hash-password":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--stdin") {
			return errors.New("usage: blog admin hash-password [--stdin]")
		}
		password, err := promptPassword(bufio.NewReader(os.Stdin), "Password: ", len(args) == 2)
		if err != nil {
			return err
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		fmt.Println(hash)
		return nil
	default:
		return fmt.Errorf("unknown admin command %q", args[0])
	}
}

func validRole(role string) bool {
	return role == "admin" || role == "editor" || role == "author" || role == "translator"
}

func promptLine(reader *bufio.Reader, label string) (string, error) {
	fmt.Print(label)
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("value is required")
	}
	return value, nil
}

func promptPassword(reader *bufio.Reader, label string, stdin bool) (string, error) {
	if stdin {
		value, err := reader.ReadString('\n')
		if err != nil && len(value) == 0 {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("password prompt requires a TTY; use --stdin for scripts")
	}
	fmt.Print(label)
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(value), err
}

func passwordForCLI(reader *bufio.Reader, stdin, randomPassword bool) (string, error) {
	if randomPassword {
		raw := make([]byte, 24)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(raw), nil
	}
	password, err := promptPassword(reader, "New password: ", stdin)
	if err != nil || stdin {
		return password, err
	}
	confirm, err := promptPassword(reader, "Confirm password: ", false)
	if err != nil {
		return "", err
	}
	if password != confirm {
		return "", errors.New("passwords do not match")
	}
	return password, nil
}
func serve(opts options) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	instance, warnings, err := app.New(app.Options{Root: opts.root, ConfigFile: opts.config, Dev: opts.dev, Logger: logger})
	if err != nil {
		return err
	}
	if opts.port > 0 {
		instance.Config.Server.Port = opts.port
	}
	for _, warning := range warnings {
		logger.Warn(warning)
	}
	logger.Info("mutiblog starting", "version", version, "root", opts.root, "address", fmt.Sprintf("%s:%d", instance.Config.Server.Host, instance.Config.Server.Port), "theme", instance.Config.Theme.Active, "locales", len(instance.Config.I18n.Locales))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := instance.Serve(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("mutiblog stopped")
	return nil
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
