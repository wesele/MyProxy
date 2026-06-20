package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/auth"
)

func newAuthManager(t *testing.T) *auth.AuthManager {
	t.Helper()
	dir := t.TempDir()
	return auth.NewAuthManager(dir + "/password.txt")
}

func TestLoginRequired_SkipLoginPath(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/login", nil)

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /admin/login, got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("expected not aborted for /admin/login")
	}
}

func TestLoginRequired_SkipAPILoginPath(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/api/login", nil)

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for /admin/api/login, got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("expected not aborted for /admin/api/login")
	}
}

func TestLoginRequired_NoCookieRedirects(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	c.Request.Header.Set("Accept", "text/html")

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/admin/login" {
		t.Errorf("expected Location /admin/login, got %s", loc)
	}
}

func TestLoginRequired_NoCookieAPIRequest(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/stats", nil)
	c.Request.Header.Set("Accept", "application/json")

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for API request, got %d", w.Code)
	}
}

func TestLoginRequired_APIRequestDefaultsToJSON(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/providers", nil)

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for /admin/api path, got %d", w.Code)
	}
}

func TestLoginRequired_ValidCookiePasses(t *testing.T) {
	am := newAuthManager(t)
	sessionID := am.CreateSession(false)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	c.Request.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	c.Request.Header.Set("Accept", "text/html")

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid cookie, got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("expected not aborted with valid cookie")
	}
}

func TestLoginRequired_InvalidCookie(t *testing.T) {
	am := newAuthManager(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	c.Request.AddCookie(&http.Cookie{Name: "session", Value: "invalid-session-id"})
	c.Request.Header.Set("Accept", "text/html")

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 for invalid cookie, got %d", w.Code)
	}
}

func TestLoginRequired_ExpiredSession(t *testing.T) {
	am := newAuthManager(t)
	sessionID := am.CreateSession(false)

	// Manually expire the session
	am.RevokeSession(sessionID)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	c.Request.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	c.Request.Header.Set("Accept", "text/html")

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 for expired session, got %d", w.Code)
	}
}

func TestLoginRequired_RememberSession(t *testing.T) {
	am := newAuthManager(t)
	sessionID := am.CreateSession(true)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	c.Request.AddCookie(&http.Cookie{Name: "session", Value: sessionID})
	c.Request.Header.Set("Accept", "text/html")

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with remember session, got %d", w.Code)
	}
}

func TestLoginRequired_LocalhostBypass(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/api/providers/fetch-models", nil)
	c.Request.Header.Set("Accept", "application/json")
	c.Request.RemoteAddr = "127.0.0.1:12345"

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for localhost request, got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("expected not aborted for localhost request")
	}
}

func TestLoginRequired_SkipStaticPaths(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/static/style.css", nil)

	handler := LoginRequired(am)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for static path, got %d", w.Code)
	}
	if c.IsAborted() {
		t.Error("expected not aborted for static path")
	}
}

func TestFullRouter_LoginLogoutAccess(t *testing.T) {
	am := newAuthManager(t)
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.POST("/admin/api/login", func(c *gin.Context) {
		var req struct {
			Password string `json:"password"`
			Remember bool   `json:"remember"`
		}
		c.ShouldBindJSON(&req)
		if !am.VerifyPassword(req.Password) {
			c.JSON(401, gin.H{"error": "invalid"})
			return
		}
		sid := am.CreateSession(req.Remember)
		c.SetCookie("session", sid, 0, "/", "", false, true)
		c.JSON(200, gin.H{"success": true})
	})
	r.POST("/admin/api/logout", func(c *gin.Context) {
		sid, _ := c.Cookie("session")
		if sid != "" {
			am.RevokeSession(sid)
		}
		c.SetCookie("session", "", -1, "/", "", false, true)
		c.JSON(200, gin.H{"success": true})
	})

	adminAPI := r.Group("/admin/api", LoginRequired(am))
	adminAPI.GET("/stats", func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/admin") {
			if p != "/admin/login" {
				sid, err := c.Cookie("session")
				if err != nil || sid == "" || !am.ValidateSession(sid) {
					c.Redirect(http.StatusFound, "/admin/login")
					return
				}
			}
			c.String(200, "OK")
			return
		}
		c.String(404, "not found")
	})

	// Step 1: Login
	loginBody := `{"password":"` + auth.DefaultPassword + `","remember":true}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/login", strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	resp := w.Result()
	var sessionCookie string
	for _, ck := range resp.Cookies() {
		if ck.Name == "session" && ck.Value != "" {
			sessionCookie = ck.Value
		}
	}
	if sessionCookie == "" {
		t.Fatal("no session cookie after login")
	}

	// Step 2: Access admin/dashboard — should succeed
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
	req2.Header.Set("Accept", "text/html")
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("dashboard after login: expected 200, got %d", w2.Code)
	}

	// Step 3: Logout
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/admin/api/logout", nil)
	req3.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
	r.ServeHTTP(w3, req3)

	// Step 4: Access admin/dashboard again — MUST be blocked
	w4 := httptest.NewRecorder()
	req4 := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req4.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
	req4.Header.Set("Accept", "text/html")
	r.ServeHTTP(w4, req4)
	if w4.Code != http.StatusFound {
		t.Errorf("dashboard after logout: expected 302 redirect, got %d", w4.Code)
	}
	loc := w4.Result().Header.Get("Location")
	if loc != "/admin/login" {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}

	// Step 5: Access dashboard without cookie — should redirect
	w5 := httptest.NewRecorder()
	req5 := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	req5.Header.Set("Accept", "text/html")
	r.ServeHTTP(w5, req5)
	if w5.Code != http.StatusFound {
		t.Errorf("dashboard without cookie: expected 302 redirect, got %d", w5.Code)
	}

	// Step 6: Login page should be accessible without auth
	w6 := httptest.NewRecorder()
	req6 := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	r.ServeHTTP(w6, req6)
	if w6.Code != http.StatusOK {
		t.Errorf("login page: expected 200, got %d", w6.Code)
	}

	// Step 7: Admin API without session should get 401
	w7 := httptest.NewRecorder()
	req7 := httptest.NewRequest(http.MethodGet, "/admin/api/stats", nil)
	r.ServeHTTP(w7, req7)
	if w7.Code != http.StatusUnauthorized {
		t.Errorf("admin API without session: expected 401, got %d", w7.Code)
	}
}
