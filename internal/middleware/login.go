package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/auth"
)

func isLocalhost(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1"
}

func LoginRequired(authManager *auth.AuthManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if path == "/admin/login" || path == "/admin/api/login" {
			c.Next()
			return
		}

		if strings.HasPrefix(path, "/admin/static/") {
			c.Next()
			return
		}

		if isLocalhost(c.ClientIP()) {
			c.Next()
			return
		}

		sessionID, err := c.Cookie("session")
		if err == nil && sessionID != "" && authManager.ValidateSession(sessionID) {
			c.Next()
			return
		}

		wantsJSON := strings.Contains(c.GetHeader("Accept"), "application/json") ||
			strings.HasPrefix(path, "/admin/api/")

		if wantsJSON {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "login required"})
			return
		}

		c.Redirect(http.StatusFound, "/admin/login")
		c.Abort()
	}
}
