package auth

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	DefaultPassword  = "Cogent~2020"
	sessionDuration  = 24 * time.Hour
	rememberDuration = 30 * 24 * time.Hour
	cleanupInterval  = 10 * time.Minute
)

type sessionInfo struct {
	expiresAt time.Time
}

type AuthManager struct {
	mu       sync.RWMutex
	password string
	sessions map[string]sessionInfo
}

func NewAuthManager(passwordPath string) *AuthManager {
	am := &AuthManager{
		password: loadOrCreatePassword(passwordPath),
		sessions: make(map[string]sessionInfo),
	}
	go am.cleanupLoop()
	return am
}

func loadOrCreatePassword(path string) string {
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data))
	}
	pw := []byte(DefaultPassword)
	os.WriteFile(path, pw, 0600)
	return DefaultPassword
}

func (am *AuthManager) VerifyPassword(password string) bool {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return am.password == password
}

func (am *AuthManager) CreateSession(remember bool) string {
	token := make([]byte, 32)
	rand.Reader.Read(token)
	sessionID := hex.EncodeToString(token)

	duration := sessionDuration
	if remember {
		duration = rememberDuration
	}

	am.mu.Lock()
	am.sessions[sessionID] = sessionInfo{expiresAt: time.Now().Add(duration)}
	am.mu.Unlock()

	return sessionID
}

func (am *AuthManager) ValidateSession(sessionID string) bool {
	am.mu.RLock()
	info, exists := am.sessions[sessionID]
	am.mu.RUnlock()

	if !exists {
		return false
	}
	if time.Now().After(info.expiresAt) {
		am.mu.Lock()
		delete(am.sessions, sessionID)
		am.mu.Unlock()
		return false
	}
	return true
}

func (am *AuthManager) RevokeSession(sessionID string) {
	am.mu.Lock()
	delete(am.sessions, sessionID)
	am.mu.Unlock()
}

func (am *AuthManager) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		am.cleanupExpired()
	}
}

func (am *AuthManager) cleanupExpired() {
	now := time.Now()
	am.mu.Lock()
	for id, info := range am.sessions {
		if now.After(info.expiresAt) {
			delete(am.sessions, id)
		}
	}
	am.mu.Unlock()
}
