package api

import (
	"bytes"
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

	// Resolve display name to upstream model name
	upstreamModel := model
	for _, m := range provider.Models {
		if m.DisplayName == model && m.Name != model {
			upstreamModel = m.Name
			break
		}
	}

	c.Set("provider_id", provider.ID)
	if entry, exists := c.Get("log_entry"); exists {
		entry.(*middleware.LogEntry).Model = model
	}

	var finalBody []byte
	if upstreamModel != model {
		var bodyMap map[string]interface{}
		if err := json.Unmarshal(body, &bodyMap); err == nil {
			bodyMap["model"] = upstreamModel
			finalBody, _ = json.Marshal(bodyMap)
		}
	}
	if finalBody == nil {
		finalBody = body
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(finalBody))

	h.forwarder.Forward(c, provider, "/messages")
}
