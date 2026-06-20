package api

import (
	"bytes"
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
	store         db.Store
}

func NewOpenAIHandler(f *proxy.Forwarder, r *proxy.Router, l *zap.Logger, s db.Store) *OpenAIHandler {
	return &OpenAIHandler{forwarder: f, router: r, logger: l, store: s}
}

func (h *OpenAIHandler) SetGeminiHandler(gh *GeminiHandler) {
	h.geminiHandler = gh
}

func (h *OpenAIHandler) ListModels(c *gin.Context) {
	providers, err := h.store.ListProviders()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list providers"})
		return
	}

	type Model struct {
		ID              string  `json:"id"`
		Object          string  `json:"object"`
		Created         int64   `json:"created"`
		OwnedBy         string  `json:"owned_by"`
		DisplayName     string  `json:"display_name,omitempty"`
		MaxTokens       int     `json:"max_tokens,omitempty"`
		MaxInputTokens  int     `json:"max_input_tokens,omitempty"`
		InputPrice      float64 `json:"input_price,omitempty"`
		OutputPrice     float64 `json:"output_price,omitempty"`
		InputCachePrice float64 `json:"input_cache_price,omitempty"`
	}

	var models []Model
	for _, p := range providers {
		for _, m := range p.Models {
			model := Model{
				ID:      p.Name + "." + m.DisplayName,
				Object:  "model",
				Created: 1700000000,
				OwnedBy: p.Name,
			}
			if m.DisplayName != m.Name {
				model.DisplayName = m.DisplayName
			}
			if m.MaxTokens > 0 {
				model.MaxTokens = m.MaxTokens
			}
			if m.MaxInputTokens > 0 {
				model.MaxInputTokens = m.MaxInputTokens
			}
			if m.InputPrice > 0 {
				model.InputPrice = m.InputPrice
			}
			if m.OutputPrice > 0 {
				model.OutputPrice = m.OutputPrice
			}
			if m.InputCachePrice > 0 {
				model.InputCachePrice = m.InputCachePrice
			}
			models = append(models, model)
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

	if entry, exists := c.Get("log_entry"); exists {
		entry.(*middleware.LogEntry).Model = reqBody.Model
	}

	provider, err := h.router.FindProvider(reqBody.Model)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	modelName := strings.TrimPrefix(reqBody.Model, provider.Name+".")
	upstreamModel := modelName
	for _, m := range provider.Models {
		if m.DisplayName == modelName && m.Name != modelName {
			upstreamModel = m.Name
			break
		}
	}

	c.Set("provider_id", provider.ID)

	var finalBody []byte
	if upstreamModel != reqBody.Model {
		var bodyMap map[string]interface{}
		if err := json.Unmarshal(body, &bodyMap); err == nil {
			bodyMap["model"] = upstreamModel
			finalBody, _ = json.Marshal(bodyMap)
		}
	}
	if finalBody == nil {
		finalBody = body
	}

	if provider.ProviderType == "gemini" && h.geminiHandler != nil {
		c.Request.Body = io.NopCloser(bytes.NewReader(finalBody))
		h.geminiHandler.ChatCompletions(c)
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewReader(finalBody))

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

	if entry, exists := c.Get("log_entry"); exists {
		entry.(*middleware.LogEntry).Model = reqBody.Model
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
