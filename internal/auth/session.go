package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"
)

type Session struct {
	Username  string
	CSRFToken string
	ExpiresAt time.Time
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[[sha256.Size]byte]Session
	ttl      time.Duration
}

func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{sessions: make(map[[sha256.Size]byte]Session), ttl: ttl}
}

func (s *SessionStore) Create(username string) (token string, session Session, err error) {
	token, err = randomToken(32)
	if err != nil {
		return "", Session{}, err
	}
	csrf, err := randomToken(24)
	if err != nil {
		return "", Session{}, err
	}
	session = Session{Username: username, CSRFToken: csrf, ExpiresAt: time.Now().Add(s.ttl)}
	s.mu.Lock()
	s.sessions[sha256.Sum256([]byte(token))] = session
	s.removeExpiredLocked(time.Now())
	s.mu.Unlock()
	return token, session, nil
}

func (s *SessionStore) Get(token string) (Session, bool) {
	key := sha256.Sum256([]byte(token))
	s.mu.RLock()
	session, ok := s.sessions[key]
	s.mu.RUnlock()
	if !ok || time.Now().After(session.ExpiresAt) {
		if ok {
			s.Delete(token)
		}
		return Session{}, false
	}
	return session, true
}

func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, sha256.Sum256([]byte(token)))
	s.mu.Unlock()
}

func (s *SessionStore) removeExpiredLocked(now time.Time) {
	for key, session := range s.sessions {
		if now.After(session.ExpiresAt) {
			delete(s.sessions, key)
		}
	}
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
