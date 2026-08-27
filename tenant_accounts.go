package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	errBadCredentials = errors.New("invalid email or password")
	errEmailExists     = errors.New("email already registered")
	errTenantSuspended = errors.New("tenant is suspended")
	errUserSuspended   = errors.New("account is suspended")
	errForbidden       = errors.New("owner role required")
	errNotFound        = errors.New("account not found")
)

type accountState string

const (
	stateActive    accountState = "active"
	stateSuspended accountState = "suspended"
)

type account struct {
	ID           string       `json:"id"`
	TenantID     string       `json:"tenant_id"`
	Email        string       `json:"email"`
	Role         string       `json:"role"`
	State        accountState `json:"state"`
	PasswordSalt []byte       `json:"-"`
	PasswordHash []byte       `json:"-"`
}

type tenant struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	State accountState `json:"state"`
}

type session struct {
	ID        string
	UserID    string
	ExpiresAt time.Time
}

type accountStore struct {
	mu       sync.RWMutex
	tenants  map[string]*tenant
	accounts map[string]*account
	byEmail  map[string]string
	sessions map[string]session
	now      func() time.Time
}

func newAccountStore() *accountStore {
	return &accountStore{tenants: map[string]*tenant{}, accounts: map[string]*account{}, byEmail: map[string]string{}, sessions: map[string]session{}, now: time.Now}
}

func (s *accountStore) onboard(tenantName, email, password string) (*tenant, *account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	email = strings.ToLower(strings.TrimSpace(email))
	if _, exists := s.byEmail[email]; exists {
		return nil, nil, errEmailExists
	}
	salt := randomBytes(16)
	t := &tenant{ID: randomID(), Name: strings.TrimSpace(tenantName), State: stateActive}
	a := &account{ID: randomID(), TenantID: t.ID, Email: email, Role: "owner", State: stateActive, PasswordSalt: salt, PasswordHash: derivePassword(password, salt)}
	s.tenants[t.ID], s.accounts[a.ID], s.byEmail[email] = t, a, a.ID
	return t, a, nil
}

func (s *accountStore) login(email, password string) (session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return session{}, errBadCredentials
	}
	a := s.accounts[id]
	if subtle.ConstantTimeCompare(a.PasswordHash, derivePassword(password, a.PasswordSalt)) != 1 {
		return session{}, errBadCredentials
	}
	if a.State != stateActive {
		return session{}, errUserSuspended
	}
	if s.tenants[a.TenantID].State != stateActive {
		return session{}, errTenantSuspended
	}
	sess := session{ID: randomID(), UserID: a.ID, ExpiresAt: s.now().Add(12 * time.Hour)}
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *accountStore) authenticate(sessionID string) (*account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sessionID]
	if !ok || !sess.ExpiresAt.After(s.now()) {
		return nil, errBadCredentials
	}
	a := s.accounts[sess.UserID]
	if a.State != stateActive || s.tenants[a.TenantID].State != stateActive {
		return nil, errBadCredentials
	}
	copy := *a
	return &copy, nil
}

func (s *accountStore) setUserState(actorID, targetID string, state accountState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	actor, ok := s.accounts[actorID]
	if !ok || actor.Role != "owner" {
		return errForbidden
	}
	target, ok := s.accounts[targetID]
	if !ok || target.TenantID != actor.TenantID {
		return errNotFound
	}
	target.State = state
	if state == stateSuspended {
		for id, sess := range s.sessions {
			if sess.UserID == targetID {
				delete(s.sessions, id)
			}
		}
	}
	return nil
}

func derivePassword(password string, salt []byte) []byte {
	value := []byte(password)
	for i := 0; i < 120000; i++ {
		mac := hmac.New(sha256.New, salt)
		mac.Write(value)
		value = mac.Sum(nil)
	}
	return value
}

func randomID() string { return base64.RawURLEncoding.EncodeToString(randomBytes(24)) }

func randomBytes(size int) []byte {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return value
}
