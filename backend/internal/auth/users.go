package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fengyuchen/mutiblog/internal/fsutil"
	"github.com/fengyuchen/mutiblog/internal/model"
	"gopkg.in/yaml.v3"
)

type Users struct {
	dir        string
	mu         sync.RWMutex
	byID       map[string]*model.User
	byUsername map[string]*model.User
}

func OpenUsers(dataRoot string) (*Users, error) {
	u := &Users{
		dir:        filepath.Join(dataRoot, "users"),
		byID:       map[string]*model.User{},
		byUsername: map[string]*model.User{},
	}
	return u, u.Reload()
}
func (u *Users) Reload() error {
	if err := fsutil.EnsureDir(u.dir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(u.dir)
	if err != nil {
		return err
	}
	byID := map[string]*model.User{}
	byName := map[string]*model.User{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(u.dir, entry.Name()))
		if err != nil {
			return err
		}
		var user model.User
		if err := yaml.Unmarshal(data, &user); err != nil {
			return err
		}
		if user.ID == "" {
			user.ID = strings.TrimSuffix(entry.Name(), ".yaml")
		}
		if user.Username == "" {
			return fmt.Errorf("user %s lacks username", user.ID)
		}
		if _, exists := byName[strings.ToLower(user.Username)]; exists {
			return fmt.Errorf("duplicate username %s", user.Username)
		}
		byID[user.ID] = &user
		byName[strings.ToLower(user.Username)] = &user
	}
	u.mu.Lock()
	u.byID = byID
	u.byUsername = byName
	u.mu.Unlock()
	return nil
}
func (u *Users) Empty() bool { u.mu.RLock(); defer u.mu.RUnlock(); return len(u.byID) == 0 }
func (u *Users) ByUsername(name string) (*model.User, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	value, ok := u.byUsername[strings.ToLower(name)]
	return value, ok
}
func (u *Users) ByID(id string) (*model.User, bool) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	value, ok := u.byID[id]
	return value, ok
}
func (u *Users) List() []*model.User {
	u.mu.RLock()
	defer u.mu.RUnlock()
	out := make([]*model.User, 0, len(u.byID))
	for _, user := range u.byID {
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}
func (u *Users) Save(user *model.User) error {
	if user == nil || user.ID == "" || user.Username == "" {
		return errors.New("user id and username are required")
	}
	data, err := yaml.Marshal(user)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(filepath.Join(u.dir, user.ID+".yaml"), data, 0o600); err != nil {
		return err
	}
	return u.Reload()
}
func (u *Users) CreateAdmin(username, email, password, locale string) (*model.User, error) {
	if !u.Empty() {
		return nil, errors.New("an administrator already exists")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	user := &model.User{
		ID:           strings.ToLower(username),
		Username:     username,
		Email:        email,
		DisplayName:  username,
		Role:         "admin",
		PasswordHash: hash,
		TokenVersion: 1,
		Locale:       model.Locale(locale),
		CreatedAt:    now,
	}
	if err := u.Save(user); err != nil {
		return nil, err
	}
	return user, nil
}
func (u *Users) ResetPassword(username, password string) error {
	user, ok := u.ByUsername(username)
	if !ok {
		return fmt.Errorf("user %q not found", username)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	copy := *user
	copy.PasswordHash = hash
	copy.TokenVersion++
	return u.Save(&copy)
}
func (u *Users) Create(username, email, password, role, locale string) (*model.User, error) {
	if username == "" || email == "" || role == "" {
		return nil, errors.New("username, email, and role are required")
	}
	if _, exists := u.ByUsername(username); exists {
		return nil, fmt.Errorf("username %q already exists", username)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	user := &model.User{
		ID:           strings.ToLower(username),
		Username:     username,
		Email:        email,
		DisplayName:  username,
		Role:         role,
		PasswordHash: hash,
		TokenVersion: 1,
		Locale:       model.Locale(locale),
		CreatedAt:    time.Now(),
	}
	if err = u.Save(user); err != nil {
		return nil, err
	}
	return user, nil
}

// SetRole and SetDisabled are intentionally file-backed so CLI recovery
// commands work even when the HTTP server is unavailable.
func (u *Users) SetRole(username, role string) error {
	user, ok := u.ByUsername(username)
	if !ok {
		return fmt.Errorf("user %q not found", username)
	}
	copy := *user
	copy.Role = role
	copy.TokenVersion++
	return u.Save(&copy)
}

func (u *Users) SetDisabled(username string, disabled bool) error {
	user, ok := u.ByUsername(username)
	if !ok {
		return fmt.Errorf("user %q not found", username)
	}
	copy := *user
	if copy.Disabled != disabled {
		copy.Disabled = disabled
		copy.TokenVersion++
	}
	return u.Save(&copy)
}

func (u *Users) Delete(id string) error {
	if _, ok := u.ByID(id); !ok {
		return fmt.Errorf("user %q not found", id)
	}
	if err := os.Remove(filepath.Join(u.dir, id+".yaml")); err != nil {
		return err
	}
	return u.Reload()
}
