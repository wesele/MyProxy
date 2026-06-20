package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/auth"
	"go.uber.org/zap"
)

type LoginHandler struct {
	auth   *auth.AuthManager
	logger *zap.Logger
}

func NewLoginHandler(a *auth.AuthManager, l *zap.Logger) *LoginHandler {
	return &LoginHandler{auth: a, logger: l}
}

type loginRequest struct {
	Password string `json:"password" form:"password"`
	Remember bool   `json:"remember" form:"remember"`
}

func (h *LoginHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if err := c.ShouldBindQuery(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
	}

	if req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password required"})
		return
	}

	if !h.auth.VerifyPassword(req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}

	sessionID := h.auth.CreateSession(req.Remember)

	maxAge := 0
	if req.Remember {
		maxAge = 30 * 24 * 3600
	}
	secure := c.Request.TLS != nil

	c.SetCookie("session", sessionID, maxAge, "/", "", secure, true)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *LoginHandler) Logout(c *gin.Context) {
	sessionID, err := c.Cookie("session")
	if err == nil && sessionID != "" {
		h.auth.RevokeSession(sessionID)
	}
	secure := c.Request.TLS != nil
	c.SetCookie("session", "", -1, "/", "", secure, true)
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *LoginHandler) CheckSession(c *gin.Context) {
	sessionID, err := c.Cookie("session")
	if err != nil || sessionID == "" || !h.auth.ValidateSession(sessionID) {
		c.JSON(http.StatusUnauthorized, gin.H{"authenticated": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"authenticated": true})
}
