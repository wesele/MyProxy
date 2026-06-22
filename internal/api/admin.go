package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/auth"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/models"
	"github.com/user/qwenportal/internal/proxy"
	"go.uber.org/zap"
)

type modelTestRequest struct {
	Models        []string `json:"models"`
	Message       string   `json:"message"`
	TimeoutSec    int      `json:"timeout_seconds"`
}

func (h *AdminHandler) TestModels(c *gin.Context) {
	var req modelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if len(req.Models) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "models list is required"})
		return
	}
	if req.Message == "" {
		req.Message = "hi"
	}
	timeout := 30
	if req.TimeoutSec > 0 {
		timeout = req.TimeoutSec
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flush := func() {
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}
	}

	writeEvent := func(model string, success bool, latencyMs int64, ct int, tps float64, content, errMsg string) {
		result := map[string]interface{}{
			"model":            model,
			"success":          success,
			"latency_ms":       latencyMs,
			"completion_tokens": ct,
			"tokens_per_sec":   tps,
			"content":          content,
		}
		if errMsg != "" {
			result["error"] = errMsg
		}
		data, _ := json.Marshal(result)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flush()
	}

	writeDelta := func(model, delta, fullContent string) {
		result := map[string]interface{}{
			"model":   model,
			"stream":  true,
			"delta":   delta,
			"content": fullContent,
		}
		data, _ := json.Marshal(result)
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flush()
	}

	for _, model := range req.Models {
		provider, err := h.router.FindProvider(model)
		if err != nil {
			writeEvent(model, false, 0, 0, 0, "", "no provider found for model")
			continue
		}

		start := time.Now()
		client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
		baseURL := strings.TrimRight(provider.BaseURL, "/")

		switch provider.ProviderType {
		case "anthropic":
			body := map[string]interface{}{
				"model":      model,
				"messages":   []map[string]string{{"role": "user", "content": req.Message}},
				"max_tokens": 200,
			}
			bodyBytes, _ := json.Marshal(body)
			httpReq, testErr := http.NewRequest("POST", baseURL+"/messages", strings.NewReader(string(bodyBytes)))
			if testErr != nil {
				writeEvent(model, false, 0, 0, 0, "", fmt.Sprintf("request creation failed: %v", testErr))
				continue
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("x-api-key", provider.APIKey)
			httpReq.Header.Set("anthropic-version", "2023-06-01")

			resp, err := client.Do(httpReq)
			if err != nil {
				e2eLatency := time.Since(start).Milliseconds()
				writeEvent(model, false, e2eLatency, 0, 0, "", fmt.Sprintf("request failed: %v", err))
				continue
			}
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			e2eLatency := time.Since(start).Milliseconds()
			if resp.StatusCode >= 400 {
				writeEvent(model, false, e2eLatency, 0, 0, "", fmt.Sprintf("HTTP %d", resp.StatusCode))
				continue
			}
			ct := parseTokensFromBody(respBody, provider.ProviderType)
			content := extractContentFromBody(respBody, provider.ProviderType)
			tps := 0.0
			if ct > 0 && e2eLatency > 0 {
				tps = float64(ct) / (float64(e2eLatency) / 1000.0)
			}
			writeEvent(model, true, e2eLatency, ct, tps, content, "")
		case "gemini":
			body := map[string]interface{}{
				"contents": []map[string]interface{}{
					{"role": "user", "parts": []map[string]string{{"text": req.Message}}},
				},
				"generationConfig": map[string]int{"maxOutputTokens": 200},
			}
			bodyBytes, _ := json.Marshal(body)
			url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, model, provider.APIKey)
			httpReq, testErr := http.NewRequest("POST", url, strings.NewReader(string(bodyBytes)))
			if testErr != nil {
				writeEvent(model, false, 0, 0, 0, "", fmt.Sprintf("request creation failed: %v", testErr))
				continue
			}
			httpReq.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(httpReq)
			if err != nil {
				e2eLatency := time.Since(start).Milliseconds()
				writeEvent(model, false, e2eLatency, 0, 0, "", fmt.Sprintf("request failed: %v", err))
				continue
			}
			respBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			e2eLatency := time.Since(start).Milliseconds()
			if resp.StatusCode >= 400 {
				writeEvent(model, false, e2eLatency, 0, 0, "", fmt.Sprintf("HTTP %d", resp.StatusCode))
				continue
			}
			content := extractContentFromBody(respBody, provider.ProviderType)
			ct := parseTokensFromBody(respBody, provider.ProviderType)
			if ct == 0 && len(content) > 0 {
				ct = len([]rune(content)) / 2
			}
			tps := 0.0
			if ct > 0 && e2eLatency > 0 {
				tps = float64(ct) / (float64(e2eLatency) / 1000.0)
			}
			writeEvent(model, true, e2eLatency, ct, tps, content, "")
		default:
			body := map[string]interface{}{
				"model":      model,
				"messages":   []map[string]string{{"role": "user", "content": req.Message}},
				"max_tokens": 200,
				"stream":     true,
			}
			bodyBytes, _ := json.Marshal(body)
			httpReq, testErr := http.NewRequest("POST", baseURL+"/chat/completions", strings.NewReader(string(bodyBytes)))
			if testErr != nil {
				writeEvent(model, false, 0, 0, 0, "", fmt.Sprintf("request creation failed: %v", testErr))
				continue
			}
			httpReq.Header.Set("Content-Type", "application/json")
			httpReq.Header.Set("Accept", "text/event-stream")
			httpReq.Header.Set("Authorization", "Bearer "+provider.APIKey)

			resp, err := client.Do(httpReq)
			if err != nil {
				e2eLatency := time.Since(start).Milliseconds()
				writeEvent(model, false, e2eLatency, 0, 0, "", fmt.Sprintf("request failed: %v", err))
				continue
			}

			if resp.StatusCode >= 400 {
				respBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				e2eLatency := time.Since(start).Milliseconds()
				writeEvent(model, false, e2eLatency, 0, 0, "", fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)))
				continue
			}

			contentType := resp.Header.Get("Content-Type")
			reader := bufio.NewReaderSize(resp.Body, 64*1024)
			peek, _ := reader.Peek(6)
			isSSE := len(peek) >= 6 && string(peek[:6]) == "data: "

			if strings.Contains(contentType, "text/event-stream") || isSSE {
				var contentBuf strings.Builder
				ct := 0

				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						break
					}
					line = strings.TrimSpace(line)
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					data := strings.TrimPrefix(line, "data: ")
					if data == "[DONE]" {
						break
					}

					var chunk struct {
						Choices []struct {
							Delta struct {
								Content          string `json:"content"`
								ReasoningContent string `json:"reasoning_content"`
							} `json:"delta"`
						} `json:"choices"`
						Usage *struct {
							CompletionTokens        int `json:"completion_tokens"`
							CompletionTokensDetails *struct {
								ReasoningTokens int `json:"reasoning_tokens"`
							} `json:"completion_tokens_details"`
						} `json:"usage"`
					}
					if err := json.Unmarshal([]byte(data), &chunk); err != nil {
						continue
					}

					if chunk.Usage != nil {
						ct = chunk.Usage.CompletionTokens
						if chunk.Usage.CompletionTokensDetails != nil {
							ct += chunk.Usage.CompletionTokensDetails.ReasoningTokens
						}
					}

					if len(chunk.Choices) > 0 {
						deltaContent := chunk.Choices[0].Delta.Content
						deltaReasoning := chunk.Choices[0].Delta.ReasoningContent
						if deltaReasoning != "" {
							contentBuf.WriteString(deltaReasoning)
							writeDelta(model, deltaReasoning, contentBuf.String())
						}
						if deltaContent != "" {
							contentBuf.WriteString(deltaContent)
							writeDelta(model, deltaContent, contentBuf.String())
						}
					}
				}
				resp.Body.Close()
				e2eLatency := time.Since(start).Milliseconds()

				if ct == 0 {
					ct = contentBuf.Len() / 4
				}
				tps := 0.0
				if ct > 0 && e2eLatency > 0 {
					tps = float64(ct) / (float64(e2eLatency) / 1000.0)
				}
				writeEvent(model, true, e2eLatency, ct, tps, contentBuf.String(), "")
			} else {
				bodyBytes, _ := io.ReadAll(reader)
				resp.Body.Close()
				ct := parseTokensFromBody(bodyBytes, provider.ProviderType)
				content := extractContentFromBody(bodyBytes, provider.ProviderType)
				e2eLatency := time.Since(start).Milliseconds()
				tps := 0.0
				if ct > 0 && e2eLatency > 0 {
					tps = float64(ct) / (float64(e2eLatency) / 1000.0)
				}
				writeEvent(model, true, e2eLatency, ct, tps, content, "")
			}
		}
	}

	fmt.Fprintf(c.Writer, "data: {\"done\":true}\n\n")
	flush()
}

func parseTokensFromBody(body []byte, providerType string) int {
	if len(body) == 0 {
		return 0
	}
	switch providerType {
	case "anthropic":
		var resp struct {
			Usage *struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && resp.Usage != nil {
			return resp.Usage.OutputTokens
		}
	case "gemini":
		var resp struct {
			UsageMetadata *struct {
				CandidatesTokenCount  int `json:"candidatesTokenCount"`
				CandidatesTokensDetails []struct {
					Modality   string `json:"modality"`
					TokenCount int    `json:"tokenCount"`
				} `json:"candidatesTokensDetails"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && resp.UsageMetadata != nil {
			ct := resp.UsageMetadata.CandidatesTokenCount
			detailsTotal := 0
			for _, d := range resp.UsageMetadata.CandidatesTokensDetails {
				detailsTotal += d.TokenCount
			}
			if detailsTotal > ct {
				ct = detailsTotal
			}
			return ct
		}
	default:
		var resp struct {
			Usage *struct {
				CompletionTokens        int `json:"completion_tokens"`
				CompletionTokensDetails *struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				} `json:"completion_tokens_details"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && resp.Usage != nil {
			ct := resp.Usage.CompletionTokens
			if resp.Usage.CompletionTokensDetails != nil {
				ct += resp.Usage.CompletionTokensDetails.ReasoningTokens
			}
			return ct
		}
		var fallback struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &fallback); err == nil && len(fallback.Choices) > 0 {
			return len(fallback.Choices[0].Message.Content) / 4
		}
	}
	return 0
}

func extractContentFromBody(body []byte, providerType string) string {
	if len(body) == 0 {
		return ""
	}
	switch providerType {
	case "anthropic":
		var resp struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Content) > 0 {
			return resp.Content[0].Text
		}
	case "gemini":
		var resp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Candidates) > 0 {
			var sb strings.Builder
			for _, p := range resp.Candidates[0].Content.Parts {
				sb.WriteString(p.Text)
			}
			return sb.String()
		}
	default:
		var resp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &resp); err == nil && len(resp.Choices) > 0 {
			return resp.Choices[0].Message.Content
		}
	}
	return ""
}

type AdminHandler struct {
	logger      *zap.Logger
	router      *proxy.Router
	store       db.Store
	authManager *auth.AuthManager
}

func NewAdminHandler(l *zap.Logger, r *proxy.Router, s db.Store, a *auth.AuthManager) *AdminHandler {
	return &AdminHandler{logger: l, router: r, store: s, authManager: a}
}

func maskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func isMaskedKey(key string) bool {
	return strings.Contains(key, "****")
}

func (h *AdminHandler) resolveAPIKey(baseURL, providerType, key string) string {
	if !isMaskedKey(key) {
		return key
	}
	providers, err := h.store.ListProviders()
	if err != nil {
		return key
	}
	normalizedBase := strings.TrimRight(baseURL, "/")
	for _, p := range providers {
		pBase := strings.TrimRight(p.BaseURL, "/")
		if pBase == normalizedBase && p.ProviderType == providerType {
			return p.APIKey
		}
	}
	return key
}

func (h *AdminHandler) ListProviders(c *gin.Context) {
	providers, err := h.store.ListProviders()
	if err != nil {
		h.logger.Error("list providers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if providers == nil {
		providers = []models.Provider{}
	}
	for i := range providers {
		if providers[i].APIKey != "" {
			providers[i].APIKey = maskAPIKey(providers[i].APIKey)
		}
	}
	c.JSON(http.StatusOK, providers)
}

func (h *AdminHandler) ExportProviders(c *gin.Context) {
	providers, err := h.store.ListProviders()
	if err != nil {
		h.logger.Error("export providers", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if providers == nil {
		providers = []models.Provider{}
	}
	c.Header("Content-Disposition", "attachment; filename=providers-backup.json")
	c.JSON(http.StatusOK, providers)
}

func (h *AdminHandler) ImportProviders(c *gin.Context) {
	var payload struct {
		Providers []models.Provider `json:"providers"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	imported := 0
	updated := 0
	skipped := 0

	for _, p := range payload.Providers {
		existing, err := h.store.FindProviderByName(p.Name)
		if err != nil {
			if _, err := h.store.CreateProvider(&p); err != nil {
				h.logger.Warn("import create failed", zap.String("name", p.Name), zap.Error(err))
				skipped++
				continue
			}
			imported++
		} else {
			p.ID = existing.ID
			if p.APIKey == "" {
				p.APIKey = existing.APIKey
			}
			if err := h.store.UpdateProvider(&p); err != nil {
				h.logger.Warn("import update failed", zap.String("name", p.Name), zap.Error(err))
				skipped++
				continue
			}
			updated++
		}
	}

	h.router.Refresh()

	c.JSON(http.StatusOK, gin.H{
		"imported": imported,
		"updated":  updated,
		"skipped":  skipped,
	})
}

func (h *AdminHandler) CreateProvider(c *gin.Context) {
	var p models.Provider
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := h.store.CreateProvider(&p)
	if err != nil {
		h.logger.Error("create provider", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	created, _ := h.store.GetProvider(id)
	created.APIKey = maskAPIKey(created.APIKey)
	h.router.Refresh()
	c.JSON(http.StatusCreated, created)
}

func (h *AdminHandler) GetProvider(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	p, err := h.store.GetProvider(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	if c.Query("show_key") != "1" {
		p.APIKey = maskAPIKey(p.APIKey)
	}
	c.JSON(http.StatusOK, p)
}

func (h *AdminHandler) UpdateProvider(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	existing, err := h.store.GetProvider(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}

	var p models.Provider
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p.ID = id

	if p.APIKey == "" || p.APIKey == maskAPIKey(existing.APIKey) {
		p.APIKey = existing.APIKey
	}

	// Restore masked key values from existing keys
	for i := range p.Keys {
		if strings.Contains(p.Keys[i].KeyValue, "****") {
			for _, ek := range existing.Keys {
				if ek.ID == p.Keys[i].ID {
					p.Keys[i].KeyValue = ek.KeyValue
					break
				}
			}
			if strings.Contains(p.Keys[i].KeyValue, "****") && p.APIKey != "" {
				p.Keys[i].KeyValue = p.APIKey
			}
		}
	}

	if err := h.store.UpdateProvider(&p); err != nil {
		h.logger.Error("update provider", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	updated, _ := h.store.GetProvider(id)
	updated.APIKey = maskAPIKey(updated.APIKey)
	h.router.Refresh()
	c.JSON(http.StatusOK, updated)
}

func (h *AdminHandler) DeleteProvider(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.store.DeleteProvider(id); err != nil {
		h.logger.Error("delete provider", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.router.Refresh()
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *AdminHandler) ListApiKeys(c *gin.Context) {
	keys, err := h.store.ListApiKeys()
	if err != nil {
		h.logger.Error("list api keys", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if keys == nil {
		keys = []models.ApiKey{}
	}
	c.JSON(http.StatusOK, keys)
}

func (h *AdminHandler) CreateApiKey(c *gin.Context) {
	var req struct {
		Name         string `json:"name"`
		RateLimitRPM int    `json:"rate_limit_rpm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	key, err := h.store.CreateApiKey(req.Name, req.RateLimitRPM)
	if err != nil {
		h.logger.Error("create api key", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, key)
}

func (h *AdminHandler) UpdateApiKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req struct {
		Name         string `json:"name"`
		IsActive     *bool  `json:"is_active"`
		RateLimitRPM int    `json:"rate_limit_rpm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	if err := h.store.UpdateApiKey(id, req.Name, isActive, req.RateLimitRPM); err != nil {
		h.logger.Error("update api key", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

func (h *AdminHandler) DeleteApiKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.store.DeleteApiKey(id); err != nil {
		h.logger.Error("delete api key", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

type genTokenRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (h *AdminHandler) GenToken(c *gin.Context) {
	var req genTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password is required"})
		return
	}
	if h.authManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auth not configured"})
		return
	}
	if !h.authManager.VerifyPassword(req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}
	existing, err := h.store.GetApiKeyByName(req.Name)
	if err == nil && existing != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "token name already exists"})
		return
	}
	key, err := h.store.CreateApiKey(req.Name, 0)
	if err != nil {
		h.logger.Error("create api key", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, key)
}

func (h *AdminHandler) ListProviderKeys(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider id"})
		return
	}
	keys, err := h.store.ListProviderKeys(id)
	if err != nil {
		h.logger.Error("list provider keys", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if keys == nil {
		keys = []models.ProviderKey{}
	}
	for i := range keys {
		if keys[i].KeyValue != "" {
			keys[i].KeyValue = maskAPIKey(keys[i].KeyValue)
		}
	}
	c.JSON(http.StatusOK, keys)
}

func (h *AdminHandler) GetProviderKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("keyId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	pid, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider id"})
		return
	}
	keys, err := h.store.ListProviderKeys(pid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	for _, k := range keys {
		if k.ID == id {
			if c.Query("show_key") != "1" {
				k.KeyValue = maskAPIKey(k.KeyValue)
			}
			c.JSON(http.StatusOK, k)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "key not found"})
}

func (h *AdminHandler) CreateProviderKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider id"})
		return
	}
	var req struct {
		KeyValue string `json:"key_value"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.KeyValue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "key_value is required"})
		return
	}
	key, err := h.store.CreateProviderKey(id, req.KeyValue)
	if err != nil {
		h.logger.Error("create provider key", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	key.KeyValue = maskAPIKey(key.KeyValue)
	h.router.Refresh()
	c.JSON(http.StatusCreated, key)
}

func (h *AdminHandler) UpdateProviderKey(c *gin.Context) {
	keyId, err := strconv.ParseInt(c.Param("keyId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	var req struct {
		KeyValue string `json:"key_value"`
		IsActive *bool  `json:"is_active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	if err := h.store.UpdateProviderKey(keyId, req.KeyValue, isActive); err != nil {
		h.logger.Error("update provider key", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.router.Refresh()
	c.JSON(http.StatusOK, gin.H{"message": "updated"})
}

func (h *AdminHandler) DeleteProviderKey(c *gin.Context) {
	keyId, err := strconv.ParseInt(c.Param("keyId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	if err := h.store.DeleteProviderKey(keyId); err != nil {
		h.logger.Error("delete provider key", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.router.Refresh()
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

type providerTestReq struct {
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	ProviderType string `json:"provider_type"`
	Model        string `json:"model"`
}

type upstreamModelInfo struct {
	ID              string  `json:"id"`
	Object          string  `json:"object,omitempty"`
	Created         int64   `json:"created,omitempty"`
	OwnedBy         string  `json:"owned_by,omitempty"`
	MaxTokens       int     `json:"max_tokens,omitempty"`
	MaxInputTokens  int     `json:"max_input_tokens,omitempty"`
	ContextWindow   int     `json:"context_window,omitempty"`
	InputPrice      float64 `json:"input_price,omitempty"`
	OutputPrice     float64 `json:"output_price,omitempty"`
	InputCachePrice float64 `json:"input_cache_price,omitempty"`
	Pricing         *struct {
		Prompt     float64 `json:"prompt,omitempty"`
		Completion float64 `json:"completion,omitempty"`
		Input      float64 `json:"input,omitempty"`
		Output     float64 `json:"output,omitempty"`
		CacheInput float64 `json:"cache_input,omitempty"`
	} `json:"pricing,omitempty"`
}

func (h *AdminHandler) FetchProviderModels(c *gin.Context) {
	var req providerTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.APIKey = h.resolveAPIKey(req.BaseURL, req.ProviderType, req.APIKey)

	baseURL := strings.TrimRight(req.BaseURL, "/")
	modelURL := baseURL + "/models"

	httpReq, err := http.NewRequest("GET", modelURL, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
		return
	}

	if req.ProviderType == "anthropic" {
		httpReq.Header.Set("x-api-key", req.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	} else if req.ProviderType == "gemini" {
		modelURL = baseURL + "/models?key=" + req.APIKey
		httpReq, _ = http.NewRequest("GET", modelURL, nil)
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("request failed: %v", err)})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	richModels := parseUpstreamModels(body)
	if richModels != nil {
		c.JSON(http.StatusOK, gin.H{"models": richModels})
		return
	}

	if resp.StatusCode != 200 {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, string(body))})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": []models.ModelConfig{}, "note": "could not parse model list"})
}

func parseUpstreamModels(body []byte) []models.ModelConfig {
	var openAIList struct {
		Data []upstreamModelInfo `json:"data"`
	}
	if json.Unmarshal(body, &openAIList) == nil && len(openAIList.Data) > 0 {
		result := make([]models.ModelConfig, 0, len(openAIList.Data))
		for _, m := range openAIList.Data {
			mc := models.ModelConfig{
				Name:        m.ID,
				DisplayName: m.ID,
			}
			if m.MaxTokens > 0 {
				mc.MaxTokens = m.MaxTokens
			}
			if m.MaxInputTokens > 0 {
				mc.MaxInputTokens = m.MaxInputTokens
			} else if m.ContextWindow > 0 {
				mc.MaxInputTokens = m.ContextWindow
			}
			if m.Pricing != nil {
				if m.Pricing.Input > 0 {
					mc.InputPrice = m.Pricing.Input
				} else if m.Pricing.Prompt > 0 {
					mc.InputPrice = m.Pricing.Prompt
				}
				if m.Pricing.Output > 0 {
					mc.OutputPrice = m.Pricing.Output
				} else if m.Pricing.Completion > 0 {
					mc.OutputPrice = m.Pricing.Completion
				}
			}
			if m.InputPrice > 0 {
				mc.InputPrice = m.InputPrice
			}
			if m.OutputPrice > 0 {
				mc.OutputPrice = m.OutputPrice
			}
			if m.InputCachePrice > 0 {
				mc.InputCachePrice = m.InputCachePrice
			} else if m.Pricing != nil && m.Pricing.CacheInput > 0 {
				mc.InputCachePrice = m.Pricing.CacheInput
			}
			result = append(result, mc)
		}
		return result
	}

	var geminiList struct {
		Models []struct {
			Name        string  `json:"name"`
			Version     string  `json:"version"`
			InputPrice  float64 `json:"input_price,omitempty"`
			OutputPrice float64 `json:"output_price,omitempty"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &geminiList) == nil && len(geminiList.Models) > 0 {
		result := make([]models.ModelConfig, 0, len(geminiList.Models))
		for _, m := range geminiList.Models {
			name := m.Name
			if len(name) > 7 && name[:7] == "models/" {
				name = name[7:]
			}
			mc := models.ModelConfig{
				Name:        name,
				DisplayName: name,
			}
			if m.InputPrice > 0 {
				mc.InputPrice = m.InputPrice
			}
			if m.OutputPrice > 0 {
				mc.OutputPrice = m.OutputPrice
			}
			result = append(result, mc)
		}
		return result
	}

	return nil
}

func (h *AdminHandler) TestProvider(c *gin.Context) {
	var req providerTestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	req.APIKey = h.resolveAPIKey(req.BaseURL, req.ProviderType, req.APIKey)

	model := req.Model
	if model == "" {
		model = "gpt-3.5-turbo"
	}

	testBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 5,
		"stream":     false,
	}
	bodyBytes, _ := json.Marshal(testBody)

	baseURL := strings.TrimRight(req.BaseURL, "/")
	chatURL := baseURL + "/chat/completions"

	httpReq, err := http.NewRequest("POST", chatURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if req.ProviderType == "anthropic" {
		httpReq.Header.Set("x-api-key", req.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	} else if req.ProviderType == "gemini" {
		geminiBody := map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"role": "user",
					"parts": []map[string]string{
						{"text": "hi"},
					},
				},
			},
			"generationConfig": map[string]int{
				"maxOutputTokens": 5,
			},
		}
		bodyBytes, _ := json.Marshal(geminiBody)
		chatURL = baseURL + "/models/" + model + ":generateContent?key=" + req.APIKey
		httpReq, _ = http.NewRequest("POST", chatURL, strings.NewReader(string(bodyBytes)))
		httpReq.Header.Set("Content-Type", "application/json")
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	start := time.Now()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"success": false, "error": fmt.Sprintf("request failed: %v", err), "latency_ms": latency,
		})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		c.JSON(http.StatusOK, gin.H{
			"success": false, "error": fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, string(respBody)),
			"latency_ms": latency,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "latency_ms": latency})
}

func (h *AdminHandler) GetStats(c *gin.Context) {
	startStr := c.Query("start")
	endStr := c.Query("end")
	hoursStr := c.DefaultQuery("hours", "24")
	modelFilter := c.Query("model")

	end := time.Now()
	if endStr != "" {
		if parsed, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = parsed
		}
	}

	var start time.Time
	if startStr != "" {
		if parsed, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = parsed
		}
	}
	if start.IsZero() {
		hours, err := strconv.Atoi(hoursStr)
		if err != nil || hours <= 0 {
			hours = 24
		}
		start = end.Add(-time.Duration(hours) * time.Hour)
	}

	stats, err := h.store.GetStats(start, end, modelFilter)
	if err != nil {
		h.logger.Error("get stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *AdminHandler) GetModelLogs(c *gin.Context) {
	model := c.Query("model")
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model query param required"})
		return
	}

	startStr := c.Query("start")
	endStr := c.Query("end")
	hoursStr := c.DefaultQuery("hours", "24")

	end := time.Now()
	if endStr != "" {
		if parsed, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = parsed
		}
	}

	var start time.Time
	if startStr != "" {
		if parsed, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = parsed
		}
	}
	if start.IsZero() {
		hours, err := strconv.Atoi(hoursStr)
		if err != nil || hours <= 0 {
			hours = 24
		}
		start = end.Add(-time.Duration(hours) * time.Hour)
	}

	limitStr := c.DefaultQuery("limit", "100")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	logs, err := h.store.GetModelLogs(model, start, end, limit)
	if err != nil {
		h.logger.Error("get model logs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if logs == nil {
		logs = []models.RequestLog{}
	}
	c.JSON(http.StatusOK, logs)
}

type trainingAction struct {
	Tool string `json:"tool"`
}

type trainingStartResponse struct {
	ID        int64  `json:"id"`
	StartedAt string `json:"started_at"`
}

func (h *AdminHandler) TrainingStart(c *gin.Context) {
	var req trainingAction
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Tool == "" {
		req.Tool = "pelvic_floor"
	}
	id, err := h.store.StartTraining(req.Tool)
	if err != nil {
		h.logger.Error("start training", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, trainingStartResponse{ID: id, StartedAt: time.Now().Format("15:04:05")})
}

func (h *AdminHandler) TrainingStop(c *gin.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.StopTraining(req.ID); err != nil {
		h.logger.Error("stop training", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "stopped"})
}

func (h *AdminHandler) TrainingStats(c *gin.Context) {
	tool := c.DefaultQuery("tool", "pelvic_floor")
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	if days < 1 {
		days = 7
	}
	stats, err := h.store.GetTrainingStats(tool, days)
	if err != nil {
		h.logger.Error("training stats", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *AdminHandler) TrainingActive(c *gin.Context) {
	tool := c.DefaultQuery("tool", "pelvic_floor")
	id, err := h.store.GetActiveTraining(tool)
	if err != nil {
		h.logger.Error("active training", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	active := false
	if id > 0 {
		active = true
	}
	c.JSON(http.StatusOK, gin.H{"active": active, "id": id})
}
