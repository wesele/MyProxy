package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewAuthManager_CreatesDefaultPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "password.txt")
	am := NewAuthManager(path)

	if !am.VerifyPassword(DefaultPassword) {
		t.Error("expected default password to be valid")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("password file should exist")
	}
	if string(data) != DefaultPassword {
		t.Errorf("expected %q, got %q", DefaultPassword, string(data))
	}
}

func TestNewAuthManager_LoadsExistingPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "password.txt")
	existing := "my-custom-password-123"
	os.WriteFile(path, []byte(existing), 0600)

	am := NewAuthManager(path)

	if !am.VerifyPassword(existing) {
		t.Error("expected existing password to be valid")
	}
	if am.VerifyPassword(DefaultPassword) {
		t.Error("expected default password to be invalid")
	}
}

func TestVerifyPassword(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"correct password", DefaultPassword, true},
		{"wrong password", "wrong-password", false},
		{"empty password", "", false},
		{"similar password", "Cogent~2021", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := am.VerifyPassword(tt.input); got != tt.expected {
				t.Errorf("VerifyPassword(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCreateSession_ReturnsNonEmpty(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))
	sessionID := am.CreateSession(false)
	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}
	if len(sessionID) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(sessionID))
	}
}

func TestValidateSession_Valid(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))
	sessionID := am.CreateSession(false)
	if !am.ValidateSession(sessionID) {
		t.Error("expected valid session")
	}
}

func TestValidateSession_Invalid(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))
	if am.ValidateSession("nonexistent-session") {
		t.Error("expected invalid session")
	}
	if am.ValidateSession("") {
		t.Error("expected invalid empty session")
	}
}

func TestRevokeSession(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))
	sessionID := am.CreateSession(false)

	am.RevokeSession(sessionID)
	if am.ValidateSession(sessionID) {
		t.Error("expected revoked session to be invalid")
	}
}

func TestCreateSession_Remember(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))
	sessionID := am.CreateSession(true)
	if !am.ValidateSession(sessionID) {
		t.Error("expected remember session to be valid")
	}
}

func TestMultipleSessions(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))
	s1 := am.CreateSession(false)
	s2 := am.CreateSession(false)
	s3 := am.CreateSession(true)

	if !am.ValidateSession(s1) {
		t.Error("expected s1 valid")
	}
	if !am.ValidateSession(s2) {
		t.Error("expected s2 valid")
	}
	if !am.ValidateSession(s3) {
		t.Error("expected s3 valid")
	}

	am.RevokeSession(s2)
	if am.ValidateSession(s2) {
		t.Error("expected s2 invalid after revoke")
	}
	if !am.ValidateSession(s1) {
		t.Error("expected s1 still valid")
	}
	if !am.ValidateSession(s3) {
		t.Error("expected s3 still valid")
	}
}

func TestCleanupExpired(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))

	sessionID := am.CreateSession(false)
	am.mu.Lock()
	am.sessions[sessionID] = sessionInfo{expiresAt: time.Now().Add(-1 * time.Hour)}
	am.mu.Unlock()

	if am.ValidateSession(sessionID) {
		t.Error("expected expired session to be invalid")
	}

	am.cleanupExpired()

	am.mu.RLock()
	_, exists := am.sessions[sessionID]
	am.mu.RUnlock()
	if exists {
		t.Error("expected expired session to be cleaned up")
	}
}

func TestConcurrentAccess(t *testing.T) {
	am := NewAuthManager(filepath.Join(t.TempDir(), "pwd.txt"))

	done := make(chan bool)
	for i := 0; i < 20; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				sid := am.CreateSession(j%2 == 0)
				am.ValidateSession(sid)
				if j%3 == 0 {
					am.RevokeSession(sid)
				}
			}
			done <- true
		}()
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
