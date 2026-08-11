package themes

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/content"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"gopkg.in/yaml.v3"
)

const (
	maxArchiveBytes      = 20 << 20
	maxUncompressedBytes = 100 << 20
	maxArchiveFiles      = 500
)

const MaxArchiveSize = maxArchiveBytes

const CompatibilityVersion = "0.1.0"

var (
	ErrInvalid        = errors.New("invalid theme package")
	ErrNotFound       = errors.New("theme not found")
	ErrActive         = errors.New("active theme cannot be removed")
	ErrIncompatible   = errors.New("theme is incompatible")
	ErrCleanupPending = errors.New("theme operation committed with cleanup pending")
)

type Manifest struct {
	SchemaVersion     int               `yaml:"schemaVersion" json:"schemaVersion"`
	ID                string            `yaml:"id" json:"id"`
	Name              string            `yaml:"name" json:"name"`
	Version           string            `yaml:"version" json:"version"`
	Requires          string            `yaml:"requires,omitempty" json:"requires,omitempty"`
	Engine            string            `yaml:"engine" json:"engine"`
	Server            string            `yaml:"server" json:"server"`
	Assets            string            `yaml:"assets,omitempty" json:"assets,omitempty"`
	Screenshot        string            `yaml:"screenshot,omitempty" json:"screenshot,omitempty"`
	SettingsSchema    string            `yaml:"settingsSchema,omitempty" json:"settingsSchema,omitempty"`
	SettingsReload    string            `yaml:"settingsReload,omitempty" json:"settingsReload,omitempty"`
	PostTemplates     []ContentTemplate `yaml:"postTemplates,omitempty" json:"postTemplates,omitempty"`
	PageTemplates     []ContentTemplate `yaml:"pageTemplates,omitempty" json:"pageTemplates,omitempty"`
	CategoryTemplates []ContentTemplate `yaml:"categoryTemplates,omitempty" json:"categoryTemplates,omitempty"`
}

type ContentTemplate struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
}

type View struct {
	Manifest
	Active        bool        `json:"active"`
	BuiltIn       bool        `json:"builtIn"`
	ScreenshotURL string      `json:"screenshotUrl,omitempty"`
	Status        string      `json:"status"`
	Conditions    []Condition `json:"conditions"`
}

type Condition struct {
	Type    string `json:"type"`
	Status  bool   `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type Runtime struct {
	ID                string
	ModulePath        string
	AssetsPath        string
	Settings          map[string]any
	PostTemplates     []ContentTemplate
	PageTemplates     []ContentTemplate
	CategoryTemplates []ContentTemplate
}

type SettingsView struct {
	ThemeID string         `json:"themeId"`
	Active  bool           `json:"active"`
	Schema  map[string]any `json:"schema"`
	Values  map[string]any `json:"values"`
}

type Service struct {
	repository *fsrepo.Repository
}

type Installation struct {
	View           View
	repository     *fsrepo.Repository
	target         string
	backup         string
	markerRelative string
	finished       bool
}

type installTransaction struct {
	SchemaVersion int    `yaml:"schemaVersion"`
	ThemeID       string `yaml:"themeId"`
	Backup        string `yaml:"backup,omitempty"`
	State         string `yaml:"state"`
}

func NewService(repository *fsrepo.Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Recover() (int, error) {
	root := filepath.Join(s.repository.Root(), "themes", "installed")
	recovered, err := s.recoverInstallTransactions(root)
	if err != nil {
		return recovered, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return recovered, err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(root, name)
		if strings.HasPrefix(name, ".install-") {
			if err := os.RemoveAll(path); err != nil {
				return recovered, err
			}
			recovered++
			continue
		}
		if strings.HasPrefix(name, ".uninstall-") {
			id := strings.TrimPrefix(name, ".uninstall-")
			if !content.ValidPublicID(id, false) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				return recovered, ErrInvalid
			}
			if err := os.RemoveAll(path); err != nil {
				return recovered, err
			}
			recovered++
			continue
		}
		if !strings.HasPrefix(name, ".previous-") {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return recovered, ErrInvalid
		}
		var manifest Manifest
		if err := readYAMLFile(filepath.Join(path, "theme.yaml"), &manifest); err != nil || !validManifest(manifest) {
			return recovered, ErrInvalid
		}
		target := filepath.Join(root, manifest.ID)
		if targetInfo, err := os.Lstat(target); err == nil {
			if !targetInfo.IsDir() || targetInfo.Mode()&os.ModeSymlink != 0 {
				return recovered, ErrInvalid
			}
			// Legacy versions had no transaction marker. A leftover previous
			// package means Commit did not finish, so conservatively restore it.
			if err := os.RemoveAll(target); err != nil {
				return recovered, err
			}
			if err := os.Rename(path, target); err != nil {
				return recovered, err
			}
		} else if errors.Is(err, os.ErrNotExist) {
			if err := os.Rename(path, target); err != nil {
				return recovered, err
			}
		} else {
			return recovered, err
		}
		recovered++
	}
	settingsRecovered, err := s.recoverSettingDeletes()
	return recovered + settingsRecovered, err
}

func (s *Service) List() ([]View, error) {
	site, err := s.siteConfig()
	if err != nil {
		return nil, err
	}
	activeID := site.ActiveTheme
	if activeID == "" {
		activeID = "earth"
	}
	items := []View{{
		Manifest:   Manifest{SchemaVersion: 1, ID: "earth", Name: "Earth", Version: "1.0.0", Engine: "react-ssr", Server: "built-in", SettingsSchema: "settings.schema.json", SettingsReload: "rebuild"},
		Active:     activeID == "earth",
		BuiltIn:    true,
		Status:     "ready",
		Conditions: []Condition{{Type: "Compatible", Status: true, Reason: "BuiltIn", Message: "The built-in theme matches this MutiBlog release."}},
	}}
	entries, err := s.repository.ReadDir("themes/installed")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		manifest, err := s.readManifest(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read installed theme %s: %w", entry.Name(), err)
		}
		items = append(items, themeView(manifest, activeID == manifest.ID))
	}
	if len(items) > 1 {
		sort.Slice(items[1:], func(i, j int) bool { return items[i+1].ID < items[j+1].ID })
	}
	return items, nil
}

func (s *Service) recoverInstallTransactions(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".transaction-") || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return recovered, ErrInvalid
		}
		relative := filepath.Join("themes", "installed", entry.Name())
		var transaction installTransaction
		if err := s.repository.ReadYAML(relative, &transaction); err != nil || !validInstallTransaction(transaction) {
			return recovered, ErrInvalid
		}
		target := filepath.Join(root, transaction.ThemeID)
		backup := ""
		if transaction.Backup != "" {
			backup = filepath.Join(root, transaction.Backup)
		}
		targetExists, err := validOptionalThemeDirectory(target)
		if err != nil {
			return recovered, err
		}
		backupExists := false
		if backup != "" {
			backupExists, err = validOptionalThemeDirectory(backup)
			if err != nil {
				return recovered, err
			}
		}
		switch transaction.State {
		case "pending":
			switch {
			case backupExists:
				if targetExists {
					if err := os.RemoveAll(target); err != nil {
						return recovered, err
					}
				}
				if err := os.Rename(backup, target); err != nil {
					return recovered, err
				}
			case transaction.Backup == "" && targetExists:
				// A fresh installation reached the target rename but never
				// committed, so it must disappear on recovery.
				if err := os.RemoveAll(target); err != nil {
					return recovered, err
				}
				// A planned upgrade with no backup still present stopped before
				// its first rename; the target is the untouched old package.
			}
		case "committed":
			if !targetExists {
				return recovered, ErrInvalid
			}
			if backupExists {
				if err := os.RemoveAll(backup); err != nil {
					return recovered, err
				}
			}
		default:
			return recovered, ErrInvalid
		}
		if err := s.repository.RemoveFile(relative); err != nil && !errors.Is(err, os.ErrNotExist) {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func validInstallTransaction(transaction installTransaction) bool {
	if transaction.SchemaVersion != domain.SchemaVersion || !content.ValidPublicID(transaction.ThemeID, true) || (transaction.State != "pending" && transaction.State != "committed") {
		return false
	}
	if transaction.Backup == "" {
		return true
	}
	return filepath.Base(transaction.Backup) == transaction.Backup && strings.HasPrefix(transaction.Backup, ".previous-"+transaction.ThemeID+"-")
}

func validOptionalThemeDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, ErrInvalid
	}
	return true, nil
}

func (s *Service) Install(reader io.Reader) (View, error) {
	installation, err := s.BeginInstall(reader)
	if err != nil {
		return View{}, err
	}
	if err := installation.Commit(); err != nil {
		if errors.Is(err, ErrCleanupPending) {
			return installation.View, nil
		}
		return View{}, err
	}
	return installation.View, nil
}

func (s *Service) BeginInstall(reader io.Reader) (*Installation, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxArchiveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveBytes {
		return nil, ErrInvalid
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(archive.File) == 0 || len(archive.File) > maxArchiveFiles {
		return nil, ErrInvalid
	}

	var total uint64
	for _, file := range archive.File {
		total += file.UncompressedSize64
		if total > maxUncompressedBytes || !safeArchivePath(file.Name) || file.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInvalid
		}
	}

	installedRoot := filepath.Join(s.repository.Root(), "themes", "installed")
	staging, err := os.MkdirTemp(installedRoot, ".install-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	if err := extractArchive(archive, staging); err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := readYAMLFile(filepath.Join(staging, "theme.yaml"), &manifest); err != nil || !validManifest(manifest) {
		return nil, ErrInvalid
	}
	if err := requireRegularFile(filepath.Join(staging, filepath.FromSlash(manifest.Server))); err != nil {
		return nil, ErrInvalid
	}
	if manifest.Assets != "" {
		if err := requireDirectory(filepath.Join(staging, filepath.FromSlash(manifest.Assets))); err != nil {
			return nil, ErrInvalid
		}
	}
	if manifest.Screenshot != "" {
		if err := requireRegularFile(filepath.Join(staging, filepath.FromSlash(manifest.Screenshot))); err != nil {
			return nil, ErrInvalid
		}
	}
	if manifest.SettingsSchema != "" {
		schemaPath := filepath.Join(staging, filepath.FromSlash(manifest.SettingsSchema))
		if err := requireRegularFile(schemaPath); err != nil || validateSchemaFile(schemaPath) != nil {
			return nil, ErrInvalid
		}
	}

	target := filepath.Join(installedRoot, manifest.ID)
	backup := ""
	markerName := ".transaction-" + strings.TrimPrefix(filepath.Base(staging), ".install-") + ".yaml"
	markerRelative := filepath.Join("themes", "installed", markerName)
	if _, err := os.Stat(target); err == nil {
		backup = filepath.Join(installedRoot, fmt.Sprintf(".previous-%s-%d", manifest.ID, time.Now().UTC().UnixNano()))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	transaction := installTransaction{SchemaVersion: domain.SchemaVersion, ThemeID: manifest.ID, State: "pending"}
	if backup != "" {
		transaction.Backup = filepath.Base(backup)
	}
	if err := s.repository.WriteYAML(markerRelative, transaction, false); err != nil {
		return nil, err
	}
	if backup != "" {
		if err := os.Rename(target, backup); err != nil {
			_ = s.repository.RemoveFile(markerRelative)
			return nil, err
		}
	}
	if err := os.Rename(staging, target); err != nil {
		if backup != "" {
			_ = os.Rename(backup, target)
		}
		_ = s.repository.RemoveFile(markerRelative)
		return nil, err
	}
	site, err := s.siteConfig()
	if err != nil {
		installation := &Installation{repository: s.repository, target: target, backup: backup, markerRelative: markerRelative}
		_ = installation.Rollback()
		return nil, err
	}
	return &Installation{View: themeView(manifest, site.ActiveTheme == manifest.ID), repository: s.repository, target: target, backup: backup, markerRelative: markerRelative}, nil
}

func themeView(manifest Manifest, active bool) View {
	compatible := requirementSatisfied(manifest.Requires, CompatibilityVersion)
	view := View{Manifest: manifest, Active: active, Status: "ready", Conditions: []Condition{{
		Type: "Compatible", Status: compatible, Reason: "RequirementSatisfied", Message: "The theme API requirement is satisfied.",
	}}}
	if !compatible {
		view.Status = "incompatible"
		view.Conditions[0].Reason = "RequirementNotSatisfied"
		view.Conditions[0].Message = "The theme requires " + manifest.Requires + "; this installation provides " + CompatibilityVersion + "."
	}
	if manifest.Screenshot != "" {
		view.ScreenshotURL = "/api/v1/admin/themes/" + manifest.ID + "/screenshot?v=" + url.QueryEscape(manifest.Version)
	}
	return view
}

func (s *Service) Screenshot(id string) (string, error) {
	if id == "earth" {
		return "", ErrNotFound
	}
	manifest, err := s.readManifest(id)
	if err != nil {
		return "", err
	}
	if manifest.Screenshot == "" {
		return "", ErrNotFound
	}
	path := filepath.Join(s.repository.Root(), "themes", "installed", id, filepath.FromSlash(manifest.Screenshot))
	if err := requireRegularFile(path); err != nil {
		return "", ErrNotFound
	}
	return path, nil
}

func (installation *Installation) Commit() error {
	if installation.finished {
		return nil
	}
	transaction := installTransaction{SchemaVersion: domain.SchemaVersion, ThemeID: installation.View.ID, State: "committed"}
	if installation.backup != "" {
		transaction.Backup = filepath.Base(installation.backup)
	}
	if installation.repository == nil || installation.markerRelative == "" {
		return ErrInvalid
	}
	if err := installation.repository.WriteYAML(installation.markerRelative, transaction, false); err != nil {
		return err
	}
	installation.finished = true
	if installation.backup != "" {
		if err := os.RemoveAll(installation.backup); err != nil {
			return fmt.Errorf("%w: %v", ErrCleanupPending, err)
		}
	}
	if err := installation.repository.RemoveFile(installation.markerRelative); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrCleanupPending, err)
	}
	return nil
}

func (installation *Installation) Rollback() error {
	if installation.finished {
		return nil
	}
	installation.finished = true
	if err := os.RemoveAll(installation.target); err != nil {
		return err
	}
	if installation.backup != "" {
		if err := os.Rename(installation.backup, installation.target); err != nil {
			return err
		}
	}
	if installation.repository == nil || installation.markerRelative == "" {
		return ErrInvalid
	}
	err := installation.repository.RemoveFile(installation.markerRelative)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Service) Activate(id string) error {
	if id != "earth" {
		manifest, err := s.readManifest(id)
		if err != nil {
			return err
		}
		if !requirementSatisfied(manifest.Requires, CompatibilityVersion) {
			return ErrIncompatible
		}
	}
	site, err := s.siteConfig()
	if err != nil {
		return err
	}
	site.ActiveTheme = id
	site.UpdatedAt = time.Now().UTC()
	return s.repository.WriteYAML("config/site.yaml", site, false)
}

func (s *Service) Uninstall(id string) error {
	return s.UninstallWithSettings(id, false)
}

func (s *Service) UninstallWithSettings(id string, deleteSettings bool) error {
	if id == "earth" || !content.ValidPublicID(id, false) {
		return ErrInvalid
	}
	site, err := s.siteConfig()
	if err != nil {
		return err
	}
	if site.ActiveTheme == id {
		return ErrActive
	}
	target := filepath.Join(s.repository.Root(), "themes", "installed", id)
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	staging := filepath.Join(s.repository.Root(), "themes", "installed", ".uninstall-"+id)
	if _, err := os.Lstat(staging); err == nil {
		return ErrCleanupPending
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if deleteSettings {
		if err := s.repository.WriteFile(deleteSettingsMarkerPath(id), []byte("pending\n"), 0o600); err != nil {
			return err
		}
	}
	if err := os.Rename(target, staging); err != nil {
		if deleteSettings {
			_ = s.repository.RemoveFile(deleteSettingsMarkerPath(id))
		}
		return err
	}
	if deleteSettings {
		err := s.repository.RemoveFile(settingsPath(id))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrCleanupPending, err)
		}
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("%w: %v", ErrCleanupPending, err)
	}
	if deleteSettings {
		if err := s.repository.RemoveFile(deleteSettingsMarkerPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrCleanupPending, err)
		}
	}
	return nil
}

func (s *Service) recoverSettingDeletes() (int, error) {
	root := filepath.Join(s.repository.Root(), "themes", "settings")
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".delete-") {
			continue
		}
		id := strings.TrimPrefix(entry.Name(), ".delete-")
		if !content.ValidPublicID(id, false) || !entry.Type().IsRegular() {
			return recovered, ErrInvalid
		}
		target := filepath.Join(s.repository.Root(), "themes", "installed", id)
		staging := filepath.Join(s.repository.Root(), "themes", "installed", ".uninstall-"+id)
		_, targetErr := os.Lstat(target)
		_, stagingErr := os.Lstat(staging)
		switch {
		case targetErr == nil && errors.Is(stagingErr, os.ErrNotExist):
			// The process stopped before the package rename; no deletion was
			// committed, so preserve both the package and its settings.
		case errors.Is(targetErr, os.ErrNotExist) && errors.Is(stagingErr, os.ErrNotExist):
			if err := s.repository.RemoveFile(settingsPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return recovered, err
			}
		default:
			return recovered, ErrInvalid
		}
		if err := s.repository.RemoveFile(deleteSettingsMarkerPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

func (s *Service) Runtime() (Runtime, error) {
	site, err := s.siteConfig()
	if err != nil {
		return Runtime{}, err
	}
	id := site.ActiveTheme
	if id == "" {
		id = "earth"
	}
	return s.RuntimeFor(id)
}

func (s *Service) RuntimeFor(id string) (Runtime, error) {
	if id == "earth" {
		settings, err := s.Settings("earth")
		if err != nil {
			return Runtime{}, err
		}
		return Runtime{ID: "earth", Settings: settings.Values}, nil
	}
	manifest, err := s.readManifest(id)
	if err != nil {
		return Runtime{}, err
	}
	if !requirementSatisfied(manifest.Requires, CompatibilityVersion) {
		return Runtime{}, ErrIncompatible
	}
	root := filepath.Join(s.repository.Root(), "themes", "installed", id)
	settings, err := s.Settings(id)
	if err != nil {
		return Runtime{}, err
	}
	runtime := Runtime{
		ID:                id,
		ModulePath:        filepath.Join(root, filepath.FromSlash(manifest.Server)),
		Settings:          settings.Values,
		PostTemplates:     append([]ContentTemplate(nil), manifest.PostTemplates...),
		PageTemplates:     append([]ContentTemplate(nil), manifest.PageTemplates...),
		CategoryTemplates: append([]ContentTemplate(nil), manifest.CategoryTemplates...),
	}
	if manifest.Assets != "" {
		runtime.AssetsPath = filepath.Join(root, filepath.FromSlash(manifest.Assets))
	}
	return runtime, nil
}

func (s *Service) Settings(id string) (SettingsView, error) {
	var schemaData []byte
	if id == "earth" {
		schemaData = []byte(earthSettingsSchema)
	} else {
		manifest, err := s.readManifest(id)
		if err != nil {
			return SettingsView{}, err
		}
		if manifest.SettingsSchema == "" {
			schemaData = []byte(`{"type":"object","properties":{}}`)
		} else {
			var err error
			schemaData, err = s.repository.ReadFile(filepath.Join("themes", "installed", id, manifest.SettingsSchema))
			if err != nil {
				return SettingsView{}, ErrInvalid
			}
		}
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaData, &schema); err != nil || schema["type"] != "object" {
		return SettingsView{}, ErrInvalid
	}
	values := defaultsForSchema(schema)
	var saved map[string]any
	if err := s.repository.ReadYAML(settingsPath(id), &saved); err == nil {
		mergeSettings(values, saved)
	} else if !errors.Is(err, os.ErrNotExist) {
		return SettingsView{}, err
	}
	if err := validateSettings(schema, values); err != nil {
		return SettingsView{}, ErrInvalid
	}
	site, err := s.siteConfig()
	if err != nil {
		return SettingsView{}, err
	}
	activeID := site.ActiveTheme
	if activeID == "" {
		activeID = "earth"
	}
	return SettingsView{ThemeID: id, Active: activeID == id, Schema: schema, Values: values}, nil
}

func (s *Service) SaveSettings(id string, values map[string]any) (SettingsView, error) {
	view, err := s.Settings(id)
	if err != nil {
		return SettingsView{}, err
	}
	// Settings submissions are patches. The console can save one settings
	// group at a time, so replacing the persisted document here would silently
	// discard a value saved by an earlier group. Keep the persisted document as
	// overrides only, so later schema-default changes still take effect. Apply
	// the patch deeply to those overrides and validate the resulting effective
	// values (schema defaults plus the saved overrides).
	// Arrays intentionally replace rather than merge, which makes ordering and
	// removals unambiguous for widget and social-link lists.
	overrides := map[string]any{}
	if err := s.repository.ReadYAML(settingsPath(id), &overrides); err != nil && !errors.Is(err, os.ErrNotExist) {
		return SettingsView{}, err
	}
	if overrides == nil {
		overrides = map[string]any{}
	}
	mergeSettings(overrides, values)
	effective := defaultsForSchema(view.Schema)
	mergeSettings(effective, overrides)
	if err := validateSettings(view.Schema, effective); err != nil {
		return SettingsView{}, ErrInvalid
	}
	if err := s.repository.WriteYAML(settingsPath(id), overrides, false); err != nil {
		return SettingsView{}, err
	}
	return s.Settings(id)
}

func (s *Service) ResetSettings(id string) (SettingsView, error) {
	_, err := s.Settings(id)
	if err != nil {
		return SettingsView{}, err
	}
	if err := s.repository.RemoveFile(settingsPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return SettingsView{}, err
	}
	return s.Settings(id)
}

func (s *Service) siteConfig() (domain.SiteConfig, error) {
	var site domain.SiteConfig
	if err := s.repository.ReadYAML("config/site.yaml", &site); err != nil {
		return domain.SiteConfig{}, err
	}
	return site, nil
}

func (s *Service) readManifest(id string) (Manifest, error) {
	if !content.ValidPublicID(id, false) {
		return Manifest{}, ErrNotFound
	}
	root := filepath.Join(s.repository.Root(), "themes", "installed", id)
	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, ErrNotFound
	}
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return Manifest{}, ErrInvalid
	}
	var manifest Manifest
	if err := s.repository.ReadYAML(filepath.Join("themes", "installed", id, "theme.yaml"), &manifest); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, ErrNotFound
		}
		return Manifest{}, err
	}
	if !validManifest(manifest) || manifest.ID != id {
		return Manifest{}, ErrInvalid
	}
	if err := requireRegularFile(filepath.Join(root, filepath.FromSlash(manifest.Server))); err != nil {
		return Manifest{}, ErrInvalid
	}
	if manifest.Assets != "" {
		if err := requireSafeDirectoryTree(filepath.Join(root, filepath.FromSlash(manifest.Assets))); err != nil {
			return Manifest{}, ErrInvalid
		}
	}
	if manifest.Screenshot != "" {
		if err := requireRegularFile(filepath.Join(root, filepath.FromSlash(manifest.Screenshot))); err != nil {
			return Manifest{}, ErrInvalid
		}
	}
	if manifest.SettingsSchema != "" {
		schemaPath := filepath.Join(root, filepath.FromSlash(manifest.SettingsSchema))
		if err := requireRegularFile(schemaPath); err != nil || validateSchemaFile(schemaPath) != nil {
			return Manifest{}, ErrInvalid
		}
	}
	return manifest, nil
}

func extractArchive(archive *zip.Reader, destination string) error {
	for _, file := range archive.File {
		target := filepath.Join(destination, filepath.FromSlash(file.Name))
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		destinationFile, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(destinationFile, io.LimitReader(source, maxUncompressedBytes+1))
		closeErr := destinationFile.Close()
		sourceErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if sourceErr != nil {
			return sourceErr
		}
	}
	return nil
}

func validManifest(manifest Manifest) bool {
	return manifest.SchemaVersion == 1 &&
		content.ValidPublicID(manifest.ID, true) &&
		manifest.ID != "earth" &&
		strings.TrimSpace(manifest.Name) != "" &&
		strings.TrimSpace(manifest.Version) != "" &&
		validRequirement(manifest.Requires) &&
		manifest.Engine == "react-ssr" &&
		safeRelativeFile(manifest.Server, ".mjs") &&
		(manifest.Assets == "" || safeRelativeDirectory(manifest.Assets)) &&
		(manifest.Screenshot == "" || safeScreenshot(manifest.Screenshot)) &&
		(manifest.SettingsSchema == "" || safeRelativeFile(manifest.SettingsSchema, ".json")) &&
		(manifest.SettingsReload == "" || manifest.SettingsReload == "rebuild") &&
		validContentTemplates(manifest.PostTemplates, "post") &&
		validContentTemplates(manifest.PageTemplates, "page") &&
		validContentTemplates(manifest.CategoryTemplates, "category")
}

func validRequirement(requirement string) bool {
	if strings.TrimSpace(requirement) == "" {
		return true
	}
	for _, token := range strings.Fields(requirement) {
		if _, _, ok := parseRequirementToken(token); !ok {
			return false
		}
	}
	return true
}

func requirementSatisfied(requirement, current string) bool {
	if strings.TrimSpace(requirement) == "" {
		return true
	}
	currentVersion, ok := parseCompatibilityVersion(current)
	if !ok {
		return false
	}
	for _, token := range strings.Fields(requirement) {
		operator, required, ok := parseRequirementToken(token)
		if !ok || !compareRequirement(currentVersion, operator, required) {
			return false
		}
	}
	return true
}

func parseRequirementToken(token string) (string, [3]int, bool) {
	operator := "="
	versionText := token
	for _, candidate := range []string{">=", "<=", ">", "<", "="} {
		if strings.HasPrefix(token, candidate) {
			operator = candidate
			versionText = strings.TrimPrefix(token, candidate)
			break
		}
	}
	version, ok := parseCompatibilityVersion(versionText)
	return operator, version, ok
}

func parseCompatibilityVersion(value string) ([3]int, bool) {
	var version [3]int
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != len(version) {
		return version, false
	}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && strings.HasPrefix(part, "0")) {
			return version, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return version, false
		}
		version[index] = number
	}
	return version, true
}

func compareRequirement(current [3]int, operator string, required [3]int) bool {
	comparison := 0
	for index := range current {
		if current[index] < required[index] {
			comparison = -1
			break
		}
		if current[index] > required[index] {
			comparison = 1
			break
		}
	}
	switch operator {
	case ">=":
		return comparison >= 0
	case "<=":
		return comparison <= 0
	case ">":
		return comparison > 0
	case "<":
		return comparison < 0
	default:
		return comparison == 0
	}
}

func safeScreenshot(path string) bool {
	if !safeRelative(path) {
		return false
	}
	switch filepath.Ext(path) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func validContentTemplates(templates []ContentTemplate, reserved string) bool {
	seen := map[string]bool{reserved: true}
	for _, template := range templates {
		if !content.ValidPublicID(template.ID, true) || seen[template.ID] || strings.TrimSpace(template.Name) == "" {
			return false
		}
		seen[template.ID] = true
	}
	return true
}

func (s *Service) ActiveTemplates(kind string) ([]ContentTemplate, error) {
	runtime, err := s.Runtime()
	if err != nil {
		return nil, err
	}
	defaultID := "post"
	custom := runtime.PostTemplates
	if strings.EqualFold(kind, "page") {
		defaultID = "page"
		custom = runtime.PageTemplates
	} else if strings.EqualFold(kind, "category") {
		defaultID = "category"
		custom = runtime.CategoryTemplates
	}
	return append([]ContentTemplate{{ID: defaultID, Name: "Default"}}, custom...), nil
}

func (s *Service) SupportsActiveTemplate(kind, id string) (bool, error) {
	templates, err := s.ActiveTemplates(kind)
	if err != nil {
		return false, err
	}
	for _, template := range templates {
		if template.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func safeArchivePath(name string) bool {
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(trimmed)))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && clean == trimmed
}

func safeRelativeFile(path, extension string) bool {
	return safeRelative(path) && filepath.Ext(path) == extension
}

func safeRelativeDirectory(path string) bool {
	return safeRelative(strings.TrimSuffix(path, "/"))
}

func safeRelative(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ErrInvalid
	}
	return nil
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	return nil
}

func requireSafeDirectoryTree(root string) error {
	if err := requireDirectory(root); err != nil {
		return err
	}
	return filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			return ErrInvalid
		}
		return nil
	})
}

func readYAMLFile(path string, destination any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, destination)
}

func settingsPath(id string) string {
	return filepath.Join("themes", "settings", id+".yaml")
}

func deleteSettingsMarkerPath(id string) string {
	return filepath.Join("themes", "settings", ".delete-"+id)
}

func validateSchemaFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil || schema["type"] != "object" {
		return ErrInvalid
	}
	return nil
}

func defaultsForSchema(schema map[string]any) map[string]any {
	values := map[string]any{}
	properties, _ := schema["properties"].(map[string]any)
	for key, raw := range properties {
		property, _ := raw.(map[string]any)
		if defaultValue, exists := property["default"]; exists {
			values[key] = defaultValue
			continue
		}
		if property["type"] == "object" {
			values[key] = defaultsForSchema(property)
		}
	}
	return values
}

func mergeSettings(destination, source map[string]any) {
	for key, value := range source {
		if sourceObject, ok := value.(map[string]any); ok {
			if destinationObject, ok := destination[key].(map[string]any); ok {
				mergeSettings(destinationObject, sourceObject)
				continue
			}
		}
		destination[key] = value
	}
}

func validateSettings(schema, values map[string]any) error {
	properties, _ := schema["properties"].(map[string]any)
	for key, value := range values {
		rawProperty, exists := properties[key]
		property, ok := rawProperty.(map[string]any)
		if !exists || !ok || validateSettingValue(property, value) != nil {
			return ErrInvalid
		}
	}
	return nil
}

func validateSettingValue(schema map[string]any, value any) error {
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return ErrInvalid
		}
		return validateSettings(schema, object)
	case "array":
		items, ok := value.([]any)
		if !ok {
			return ErrInvalid
		}
		if minimum, ok := schema["minItems"].(float64); ok && len(items) < int(minimum) {
			return ErrInvalid
		}
		if maximum, ok := schema["maxItems"].(float64); ok && len(items) > int(maximum) {
			return ErrInvalid
		}
		itemSchema, ok := schema["items"].(map[string]any)
		if !ok {
			return ErrInvalid
		}
		for _, item := range items {
			if validateSettingValue(itemSchema, item) != nil {
				return ErrInvalid
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok || !validEnum(schema, text) {
			return ErrInvalid
		}
		if minimum, ok := schema["minLength"].(float64); ok && len([]rune(text)) < int(minimum) {
			return ErrInvalid
		}
		if maximum, ok := schema["maxLength"].(float64); ok && len([]rune(text)) > int(maximum) {
			return ErrInvalid
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return ErrInvalid
		}
	case "number":
		if _, ok := value.(float64); !ok {
			if _, ok := value.(int); !ok {
				return ErrInvalid
			}
		}
	case "integer":
		switch number := value.(type) {
		case int:
		case float64:
			if number != float64(int64(number)) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validEnum(schema map[string]any, value string) bool {
	options, exists := schema["enum"].([]any)
	if !exists {
		return true
	}
	for _, option := range options {
		if option == value {
			return true
		}
	}
	return false
}
