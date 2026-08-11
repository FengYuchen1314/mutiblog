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
