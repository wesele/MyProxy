package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/middleware"
	"github.com/user/qwenportal/internal/proxy"
	"go.uber.org/zap"
)

type ClaudeHandler struct {
	forwarder *proxy.Forwarder
	router    *proxy.Router
	logger    *zap.Logger
}

func NewClaudeHandler(f *proxy.Forwarder, r *proxy.Router, l *zap.Logger) *ClaudeHandler {
	return &ClaudeHandler{forwarder: f, router: r, logger: l}
}

func (h *ClaudeHandler) Messages(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var reqBody struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}

	c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

	if err := c.ShouldBindJSON(&reqBody); err != nil {
		c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
	}

	model := reqBody.Model
	if model == "" {
		var fallback struct {
			Model string `json:"model"`
		}
		json.Unmarshal(body, &fallback)
		model = fallback.Model
	}

	provider, err := h.router.FindProvider(model)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.Set("provider_id", provider.ID)
	if entry, exists := c.Get("log_entry"); exists {
		entry.(*middleware.LogEntry).Model = model
	}

	c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

	h.forwarder.Forward(c, provider, "/messages")
}
