package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/middleware"
	"github.com/user/qwenportal/internal/proxy"
	"go.uber.org/zap"
)

type OpenAIHandler struct {
	forwarder     *proxy.Forwarder
	router        *proxy.Router
	logger        *zap.Logger
	geminiHandler *GeminiHandler
}

func NewOpenAIHandler(f *proxy.Forwarder, r *proxy.Router, l *zap.Logger) *OpenAIHandler {
	return &OpenAIHandler{forwarder: f, router: r, logger: l}
}

func (h *OpenAIHandler) SetGeminiHandler(gh *GeminiHandler) {
	h.geminiHandler = gh
}

func (h *OpenAIHandler) ListModels(c *gin.Context) {
	providers, err := db.ListActiveProviders()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list providers"})
		return
	}

	type Model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	var models []Model
	for _, p := range providers {
		for _, m := range p.Models {
			models = append(models, Model{
				ID:      m,
				Object:  "model",
				Created: 1700000000,
				OwnedBy: p.Name,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}

func (h *OpenAIHandler) ChatCompletions(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var reqBody struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if reqBody.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model is required"})
		return
	}

	provider, err := h.router.FindProvider(reqBody.Model)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.Set("provider_id", provider.ID)
	if entry, exists := c.Get("log_entry"); exists {
		entry.(*middleware.LogEntry).Model = reqBody.Model
	}

	if provider.ProviderType == "gemini" && h.geminiHandler != nil {
		c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
		h.geminiHandler.ChatCompletions(c)
		return
	}

	c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

	h.forwarder.Forward(c, provider, "/chat/completions")
}

func (h *OpenAIHandler) Embeddings(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var reqBody struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	provider, err := h.router.FindProvider(reqBody.Model)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.Set("provider_id", provider.ID)
	c.Request.Body = io.NopCloser(strings.NewReader(string(body)))

	h.forwarder.Forward(c, provider, "/embeddings")
}
