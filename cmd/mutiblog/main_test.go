package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/auth"
	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestResetPasswordReplacesInitializedAdministratorCredential(t *testing.T) {
	root := t.TempDir()
	repository, err := fsrepo.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	oldHash, err := auth.HashPassword("old password for tests")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	admin := domain.AdminConfig{SchemaVersion: domain.SchemaVersion, Username: "admin", PasswordHash: oldHash, CreatedAt: now, UpdatedAt: now}
	if err := repository.WriteYAML("config/admin.yaml", admin, true); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteFile("config/initialized", []byte("initialized\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	newPassword := "new password for tests"
	if err := resetPassword([]string{"--data-dir", root}, strings.NewReader(newPassword+"\n")); err != nil {
		t.Fatal(err)
	}
	if err := repository.ReadYAML("config/admin.yaml", &admin); err != nil {
		t.Fatal(err)
	}
	valid, err := auth.VerifyPassword(newPassword, admin.PasswordHash)
	if err != nil || !valid {
		t.Fatalf("new credential valid = %t, %v", valid, err)
	}
	if info, err := filepath.Glob(filepath.Join(root, "config", ".mutiblog-*")); err != nil || len(info) != 0 {
		t.Fatalf("temporary files = %v, %v", info, err)
	}
}
