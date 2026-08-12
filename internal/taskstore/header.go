package taskstore

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

// Header is the common identity shared by every durable task record. Task
// implementations intentionally share one directory, so readers must
// distinguish a valid foreign record from a damaged or unknown record.
type Header struct {
	SchemaVersion int    `yaml:"schemaVersion"`
	ID            string `yaml:"id"`
	Kind          string `yaml:"kind"`
}

// Progress is the durable, transport-neutral progress contract shared by all
// background work. Current/Total describe real completed work units while
// Percent is persisted so a task never appears to move backwards when a phase
// discovers additional work. Terminal failures deliberately retain their last
// observed value; only successful completion is forced to 100.
type Progress struct {
	Phase   string `yaml:"phase" json:"phase"`
	Current int    `yaml:"current" json:"current"`
	Total   int    `yaml:"total" json:"total"`
	Percent int    `yaml:"percent" json:"percent"`
	Message string `yaml:"message,omitempty" json:"message,omitempty"`
}

// Advance returns a normalized, monotonically non-decreasing progress value.
// Callers may use explicitPercent for phase-weighted work (for example the
// renderer boundary between snapshot and activation), or pass a negative value
// to derive it from current/total.
func Advance(previous Progress, phase string, current, total, explicitPercent int, message string) Progress {
	if current < 0 {
		current = 0
	}
	if total < 0 {
		total = 0
	}
	if current < previous.Current {
		current = previous.Current
	}
	if total < previous.Total {
		total = previous.Total
	}
	if total > 0 && current > total {
		current = total
	}
	percent := explicitPercent
	if percent < 0 {
		percent = 0
		if total > 0 {
			percent = current * 100 / total
		}
	}
	if percent < previous.Percent {
		percent = previous.Percent
	}
	if percent > 100 {
		percent = 100
	}
	return Progress{Phase: phase, Current: current, Total: total, Percent: percent, Message: message}
}

// Complete marks successful work as fully complete while preserving its real
// unit counts. Failed and needs-review paths must not call this helper.
func Complete(previous Progress, message string) Progress {
	total := previous.Total
	if total < 1 {
		total = 1
	}
	return Progress{Phase: "completed", Current: total, Total: total, Percent: 100, Message: message}
}

func Validate(header Header, expectedID string) error {
	if header.SchemaVersion != domain.SchemaVersion || header.ID != expectedID || !safeID(header.ID) {
		return errors.New("task identity does not match its path")
	}
	switch header.Kind {
	case "Backup":
		if !strings.HasPrefix(header.ID, "backup-task-") {
			return errors.New("backup task ID is invalid")
		}
	case "Translation":
		if !strings.HasPrefix(header.ID, "translation-") {
			return errors.New("translation task ID is invalid")
		}
	case "StaticBuild":
		if !validStaticBuildID(header.ID) {
			return errors.New("static build task ID is invalid")
		}
	case "ScheduledPublish":
		if !strings.HasPrefix(header.ID, "scheduled-publish-") || !validStaticBuildID(strings.TrimPrefix(header.ID, "scheduled-publish-")) {
			return errors.New("scheduled publish task ID is invalid")
		}
	case "IndexRebuild":
		if !ValidIndexRebuildID(header.ID) {
			return errors.New("index rebuild task ID is invalid")
		}
	case "LocaleProvision":
		if !ValidLocaleProvisionID(header.ID) {
			return errors.New("locale provisioning task ID is invalid")
		}
	default:
		return errors.New("task kind is unsupported")
	}
	return nil
}

func safeID(id string) bool {
	return id != "" && len(id) < 128 && filepath.Base(id) == id && !strings.ContainsAny(id, "/\\")
}

func validStaticBuildID(id string) bool {
	if len(id) != 35 || id[26] != '-' || id[27:] != strings.ToLower(id[27:]) {
		return false
	}
	if _, err := time.Parse("20060102T150405.000000000Z", id[:26]); err != nil {
		return false
	}
	random, err := hex.DecodeString(id[27:])
	return err == nil && len(random) == 4
}

// ValidStaticBuildID lets the HTTP layer and browser-generated correlation IDs
// use exactly the same validation as the durable task store.
func ValidStaticBuildID(id string) bool { return validStaticBuildID(id) }

// ValidIndexRebuildID validates the durable ID used by a manual search-index
// rebuild. Keeping the timestamp/random suffix compatible with static build
// task IDs gives the console one collision-resistant browser-side generator,
// while the explicit prefix prevents a task from being interpreted as a
// public-release build.
func ValidIndexRebuildID(id string) bool {
	const prefix = "index-rebuild-"
	return strings.HasPrefix(id, prefix) && validStaticBuildID(strings.TrimPrefix(id, prefix))
}

// ValidLocaleProvisionID validates the parent task used for an asynchronous
// site-wide locale provisioning batch. Reusing the static-build suffix gives
// the task a collision-resistant, sortable identity while keeping its prefix
// distinct from the build child it owns.
func ValidLocaleProvisionID(id string) bool {
	const prefix = "locale-provision-"
	return strings.HasPrefix(id, prefix) && validStaticBuildID(strings.TrimPrefix(id, prefix))
}
