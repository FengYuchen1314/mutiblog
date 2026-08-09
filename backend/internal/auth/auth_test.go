package auth

import (
	"github.com/fengyuchen/mutiblog/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestPasswordAndSignedSession(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") || VerifyPassword(hash, "wrong password") {
		t.Fatal("password verification failed")
	}
	token, err := Sign(
		"01234567890123456789012345678901",
		Claims{UID: "admin", ExpiresAt: time.Now().Add(time.Minute).Unix()},
	)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Parse("01234567890123456789012345678901", token)
	if err != nil || claims.UID != "admin" {
		t.Fatalf("claims=%#v err=%v", claims, err)
	}
	if !Can("admin", "user.manage") || Can("editor", "user.manage") {
		t.Fatal("permissions")
	}
	_ = model.Locale("")
}

func TestPasswordParamsAreUsable(t *testing.T) {
	params := PasswordParams()
	if params.Memory < 32*1024 || params.Time < 3 || params.KeyLen != 32 {
		t.Fatalf("password params = %#v", params)
	}
}

func TestResetPasswordInvalidatesSessions(t *testing.T) {
	users, err := OpenUsers(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := users.CreateAdmin("admin", "admin@example.test", "correct horse battery staple", "en")
	if err != nil {
		t.Fatal(err)
	}
	oldVersion := user.TokenVersion
	if err := users.ResetPassword("admin", "another correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	updated, ok := users.ByUsername("admin")
	if !ok || updated.TokenVersion != oldVersion+1 ||
		!VerifyPassword(updated.PasswordHash, "another correct horse battery staple") {
		t.Fatalf("updated=%#v", updated)
	}
}

func TestUserRecoveryMutationsInvalidateSessions(t *testing.T) {
	users, err := OpenUsers(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := users.CreateAdmin("admin", "admin@example.test", "correct horse battery staple", "en")
	if err != nil {
		t.Fatal(err)
	}
	if err = users.SetRole("admin", "editor"); err != nil {
		t.Fatal(err)
	}
	updated, ok := users.ByUsername("admin")
	if !ok || updated.Role != "editor" || updated.TokenVersion != user.TokenVersion+1 {
		t.Fatalf("role update = %#v", updated)
	}
	if err = users.SetDisabled("admin", true); err != nil {
		t.Fatal(err)
	}
	updated, _ = users.ByUsername("admin")
	if !updated.Disabled || updated.TokenVersion != user.TokenVersion+2 {
		t.Fatalf("disable = %#v", updated)
	}
	if err = users.SetDisabled("admin", false); err != nil {
		t.Fatal(err)
	}
	updated, _ = users.ByUsername("admin")
	if updated.Disabled || updated.TokenVersion != user.TokenVersion+3 {
		t.Fatalf("unlock = %#v", updated)
	}
}
