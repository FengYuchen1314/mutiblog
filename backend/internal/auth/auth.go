// Package auth implements password hashing, signed sessions, and permission checks.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/FengYuchen1314/mutiblog/internal/model"
	"golang.org/x/crypto/argon2"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	minPasswordLength = 12
)

type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
	KeyLen  uint32
}

// DefaultParams follows the documented Argon2id baseline. 64 MiB remains
// viable on the 512 MiB deployment target while making offline guessing
// materially more expensive than a lower-memory configuration.
var DefaultParams = Params{Memory: 64 * 1024, Time: 3, Threads: 2, KeyLen: 32}

func HashPassword(password string) (string, error) {
	if len(password) < minPasswordLength {
		return "", fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	p := PasswordParams()
	hash := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return fmt.Sprintf(
		"$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		p.Memory,
		p.Time,
		p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// PasswordParams lowers Argon2 memory on small Linux hosts while increasing
// iterations. Existing PHC strings carry their own parameters, so this only
// affects newly created/reset passwords.
func PasswordParams() Params {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return DefaultParams
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kilobytes, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr == nil && kilobytes < 1536*1024 {
			return Params{Memory: 32 * 1024, Time: 4, Threads: 2, KeyLen: 32}
		}
		break
	}
	return DefaultParams
}
func VerifyPassword(encoded, password string) bool {
	p, salt, want, err := parsePHC(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, p.Time, p.Memory, p.Threads, p.KeyLen)
	return subtle.ConstantTimeCompare(got, want) == 1
}
func parsePHC(s string) (Params, []byte, []byte, error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return Params{}, nil, nil, errors.New("invalid password hash")
	}
	p := DefaultParams
	for _, entry := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			return p, nil, nil, errors.New("invalid params")
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return p, nil, nil, err
		}
		switch k {
		case "m":
			p.Memory = uint32(n)
		case "t":
			p.Time = uint32(n)
		case "p":
			p.Threads = uint8(n)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, err
	}
	p.KeyLen = uint32(len(hash))
	return p, salt, hash, nil
}

type Claims struct {
	UID          string `json:"uid"`
	TokenVersion int    `json:"tv"`
	ExpiresAt    int64  `json:"exp"`
	IssuedAt     int64  `json:"iat"`
}

func Sign(secret string, c Claims) (string, error) {
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func Parse(secret, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Claims{}, errors.New("invalid session")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return Claims{}, errors.New("invalid session signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Claims{}, err
	}
	if c.ExpiresAt < time.Now().Unix() {
		return Claims{}, errors.New("session expired")
	}
	return c, nil
}
func NewClaims(user *model.User, ttl time.Duration) Claims {
	now := time.Now()
	return Claims{UID: user.ID, TokenVersion: user.TokenVersion, IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix()}
}
func Can(role, action string) bool {
	for _, allowed := range permissions[action] {
		if role == allowed {
			return true
		}
	}
	return false
}

var permissions = map[string][]string{
	"post.read":      {"admin", "editor", "author", "translator"},
	"post.write":     {"admin", "editor"},
	"post.publish":   {"admin", "editor"},
	"post.delete":    {"admin", "editor"},
	"post.translate": {"admin", "editor", "translator"},
	"taxonomy.write": {"admin", "editor"},
	"media.write":    {"admin", "editor", "author"},
	"theme.settings": {"admin"},
	"theme.activate": {"admin"},
	"settings.write": {"admin"},
	"user.manage":    {"admin"},
	"backup.manage":  {"admin"},
	"render.rebuild": {"admin", "editor"},
	"log.read":       {"admin"},
}
