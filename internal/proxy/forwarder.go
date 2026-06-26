package proxy

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/middleware"
	"github.com/user/qwenportal/internal/models"
	"go.uber.org/zap"
)

type Forwarder struct {
	client            *http.Client
	logger            *zap.Logger
	keyIndexMap       sync.Map // provider ID -> current key index
	virtualModelIndex sync.Map // "providerID:modelName" -> current target index
	modelRateLimiters sync.Map // "providerID:modelName" -> *rateLimiter
}

// rateLimiter tracks requests within a sliding 1-minute window.
type rateLimiter struct {
	mu          sync.Mutex
	count       int
	windowStart time.Time
}

func (rl *rateLimiter) allow(rpm int) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	if now.Sub(rl.windowStart) >= time.Minute {
		rl.count = 0
		rl.windowStart = now
	}
	rl.count++
	if rpm > 0 && rl.count > rpm {
		return false
	}
	return true
}

// NewHTTPClient creates an http.Client with TLS verification disabled
// to handle self-signed or mismatched certificates on upstream APIs.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func NewForwarder(logger *zap.Logger) *Forwarder {
	return &Forwarder{
		client: NewHTTPClient(5 * time.Minute),
		logger: logger,
	}
}

// GetCurrentKeyIndex returns the current key index for the provider.
func (f *Forwarder) GetCurrentKeyIndex(providerID int64) int {
	if providerID <= 0 {
		return 0
	}
	if val, ok := f.keyIndexMap.Load(providerID); ok {
		return val.(int)
	}
	return 0
}

// AdvanceKey advances to the next key index for the provider (wraps around)
// and returns the new index.
func (f *Forwarder) AdvanceKey(providerID int64, keyCount int) int {
	if providerID <= 0 || keyCount <= 1 {
		return 0
	}
	next := (f.GetCurrentKeyIndex(providerID) + 1) % keyCount
	f.keyIndexMap.Store(providerID, next)
	return next
}

// ProviderKeyAt returns the key value at the given index, falling back to
// provider.APIKey if no keys are configured.
func ProviderKeyAt(provider *models.Provider, idx int) string {
	if idx >= 0 && idx < len(provider.Keys) {
		return provider.Keys[idx].KeyValue
	}
	if provider.APIKey != "" {
		return provider.APIKey
	}
	return ""
}

func Truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}

func ExtractRequestSummary(body []byte) string {
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		// Also handle Responses API input field
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Truncate(string(body), 200)
	}
	// Try messages first (chat completions format)
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return Truncate(req.Messages[i].Content, 200)
		}
		if req.Messages[i].Role == "system" && len(req.Messages) == 1 {
			return Truncate(req.Messages[i].Content, 200)
		}
	}
	// Try input field (Responses API format)
	if len(req.Input) > 0 {
		var text string
		if err := json.Unmarshal(req.Input, &text); err == nil {
			return Truncate(text, 200)
		}
		var items []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(req.Input, &items); err == nil {
			for i := len(items) - 1; i >= 0; i-- {
				if items[i].Role == "user" {
					return Truncate(items[i].Content, 200)
				}
			}
			if len(items) > 0 {
				return Truncate(items[0].Content, 200)
			}
		}
	}
	return Truncate(string(body), 200)
}

// estimateCompletionTokens estimates the number of completion tokens from the response body.
func estimateCompletionTokens(respBody []byte) int {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		// Fallback: approximate from raw body length
		if len(respBody) > 0 {
			return len(respBody) / 4
		}
		return 0
	}
	for _, ch := range resp.Choices {
		content := ch.Message.Content
		if content == "" {
			content = ch.Delta.Content
		}
		if content == "" {
			content = ch.Text
		}
		if content != "" {
			n := len([]rune(content))
			if n/4 < 1 {
				return 1
			}
			return n / 4
		}
	}
	return len(respBody) / 4
}

// estimatePromptTokens estimates the number of prompt tokens from the request body
// by summing all message content and dividing by 4 (rough English token ratio).
func estimatePromptTokens(body []byte) int {
	var req struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return len(body) / 4
	}
	totalChars := 0
	for _, m := range req.Messages {
		totalChars += len([]rune(m.Content))
	}
	if totalChars == 0 {
		return len(body) / 4
	}
	if totalChars/4 < 1 {
		return 1
	}
	return totalChars / 4
}

func ExtractResponseSummary(body []byte) string {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	if len(resp.Choices) > 0 {
		return Truncate(resp.Choices[0].Message.Content, 200)
	}
	return ""
}

func FindModelConfig(provider *models.Provider, requestedModel string) *models.ModelConfig {
	modelName := requestedModel
	prefix := provider.Name + "."
	if strings.HasPrefix(modelName, prefix) {
		modelName = modelName[len(prefix):]
	}
	for i := range provider.Models {
		if provider.Models[i].Name == modelName || provider.Models[i].DisplayName == modelName {
			return &provider.Models[i]
		}
	}
	return nil
}

func MergeExtraBody(body []byte, extraBody map[string]interface{}) []byte {
	if len(extraBody) == 0 {
		return body
	}
	var reqMap map[string]json.RawMessage
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body
	}
	for k, v := range extraBody {
		raw, err := json.Marshal(v)
		if err == nil {
			reqMap[k] = json.RawMessage(raw)
		}
	}
	merged, _ := json.Marshal(reqMap)
	return merged
}

// checkModelRPM checks whether the model has exceeded its per-minute rate limit.
// Returns true if the request is allowed, false if rate limited.
func (f *Forwarder) checkModelRPM(providerID int64, modelName string, rpm int) bool {
	if rpm <= 0 {
		return true
	}
	key := modelRateLimitKey(providerID, modelName)
	rlIface, _ := f.modelRateLimiters.LoadOrStore(key, &rateLimiter{windowStart: time.Now()})
	return rlIface.(*rateLimiter).allow(rpm)
}

func modelRateLimitKey(providerID int64, modelName string) string {
	return fmt.Sprintf("%d:%s", providerID, modelName)
}

func (f *Forwarder) writeUpstreamResp(c *gin.Context, resp *http.Response, originalBody []byte, isStream bool) {
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, vv := range v {
			c.Header(k, vv)
		}
	}

	if isStream || strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		c.Set("proxy_streaming", true)
		c.Writer.WriteHeader(resp.StatusCode)
		sw := &sseWriter{writer: c.Writer, buf: make([]byte, 0, 4096)}
		flusher, canFlush := c.Writer.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				sw.Write(buf[:n])
				if canFlush {
					flusher.Flush()
				}
			}
			if err != nil {
				break
			}
		}
		pt, ct, ict := ParseTokens(sw.lastUsage)
		if pt+ct+ict == 0 {
			pt = estimatePromptTokens(originalBody)
		}
		if ct == 0 && sw.content.Len() > 0 {
			ct = sw.content.Len() / 4
		}
		if pt+ct+ict > 0 {
			c.Set("proxy_prompt_tokens", pt)
			c.Set("proxy_completion_tokens", ct)
			c.Set("proxy_input_cache_tokens", ict)
		}
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read response"})
		return
	}

	pt, ct, ict := ParseTokens(respBody)
	if pt+ct+ict == 0 {
		pt = estimatePromptTokens(originalBody)
		if ct == 0 {
			ct = estimateCompletionTokens(respBody)
		}
	}
	if pt+ct+ict > 0 {
		c.Set("proxy_prompt_tokens", pt)
		c.Set("proxy_completion_tokens", ct)
		c.Set("proxy_input_cache_tokens", ict)
	}

	c.Set("response_summary", ExtractResponseSummary(respBody))
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
}

func (f *Forwarder) Forward(c *gin.Context, provider *models.Provider, path string) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
		return
	}

	// Store request summary on context
	c.Set("request_summary", ExtractRequestSummary(body))

	var reqBody struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	isStream := false
	json.Unmarshal(body, &reqBody)
	if reqBody.Stream {
		isStream = true
	}

	// Merge model-specific extra_body if present
	if mc := FindModelConfig(provider, reqBody.Model); mc != nil && mc.ExtraBody != nil {
		body = MergeExtraBody(body, mc.ExtraBody)
	}

	// Check model-level RPM limit
	if mc := FindModelConfig(provider, reqBody.Model); mc != nil {
		mcName := mc.Name
		if mc.DisplayName != "" {
			mcName = mc.DisplayName
		}
		if !f.checkModelRPM(provider.ID, mcName, mc.RPM) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded", "model": mcName})
			return
		}
	}

	keyCount := len(provider.Keys)
	if keyCount == 0 {
		keyCount = 1 // fallback: try at least once (may use empty APIKey)
	}
	keyIdx := f.GetCurrentKeyIndex(provider.ID)

	targetURL := strings.TrimRight(provider.BaseURL, "/") + path

	for attempt := 0; attempt < keyCount; {
		key := ProviderKeyAt(provider, keyIdx)

		req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, bytes.NewReader(body))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
			return
		}

		if provider.ProviderType == "anthropic" {
			req.Header.Set("x-api-key", key)
			req.Header.Set("anthropic-version", "2023-06-01")
		} else if provider.ProviderType == "gemini" {
			// Gemini uses API key as query param, handled in gemini.go
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		req.Header.Set("Content-Type", "application/json")

		for k := range c.Request.Header {
			if k == "Authorization" || k == "Content-Type" {
				continue
			}
			req.Header.Set(k, c.Request.Header.Get(k))
		}

		resp, err := f.client.Do(req)
		if err != nil {
			f.logger.Error("upstream request failed", zap.Error(err))
			c.JSON(http.StatusBadGateway, gin.H{"error": "upstream request failed: " + err.Error()})
			return
		}

		if resp.StatusCode == 429 && attempt+1 < keyCount {
			resp.Body.Close()
			f.logger.Warn("rate limited (429), switching to next key",
				zap.Int("from_key_index", keyIdx),
				zap.Int("to_key_index", (keyIdx+1)%keyCount),
			)
			keyIdx = f.AdvanceKey(provider.ID, keyCount)
			attempt++
			continue
		}

		c.Set("provider_key_index", keyIdx)

		f.writeUpstreamResp(c, resp, body, isStream)
		return
	}
}

// virtualModelKey generates a key for the virtual model index map.
func virtualModelKey(providerID int64, modelName string) string {
	return fmt.Sprintf("%d:%s", providerID, modelName)
}

// getVirtualModelIndex returns the current target index for a virtual model.
func (f *Forwarder) getVirtualModelIndex(providerID int64, modelName string) int {
	key := virtualModelKey(providerID, modelName)
	if val, ok := f.virtualModelIndex.Load(key); ok {
		return val.(int)
	}
	return 0
}

// advanceVirtualModelIndex advances to the next target index (wraps around).
func (f *Forwarder) advanceVirtualModelIndex(providerID int64, modelName string, targetCount int) int {
	if targetCount <= 1 {
		return 0
	}
	current := f.getVirtualModelIndex(providerID, modelName)
	next := (current + 1) % targetCount
	f.virtualModelIndex.Store(virtualModelKey(providerID, modelName), next)
	return next
}

// ForwardVirtual forwards a request through a virtual model.
// It iterates through the target model list, and on 429 rotates to the next target.
func (f *Forwarder) ForwardVirtual(c *gin.Context, router *Router, provider *models.Provider,
	virtualModel string, targets []string, path string, body []byte) {

	if len(targets) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "virtual model has no targets"})
		return
	}

	c.Set("request_summary", ExtractRequestSummary(body))

	var reqBody struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	isStream := false
	json.Unmarshal(body, &reqBody)
	if reqBody.Stream {
		isStream = true
	}

	targetCount := len(targets)
	startIdx := f.getVirtualModelIndex(provider.ID, virtualModel)

	// Check virtual model's own RPM limit
	if mc := FindModelConfig(provider, virtualModel); mc != nil && mc.RPM > 0 {
		mcName := mc.Name
		if mc.DisplayName != "" {
			mcName = mc.DisplayName
		}
		if !f.checkModelRPM(provider.ID, mcName, mc.RPM) {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded", "model": mcName})
			return
		}
	}

	for attempt := 0; attempt < targetCount; attempt++ {
		idx := (startIdx + attempt) % targetCount
		targetID := targets[idx]
		attemptLog := zap.String("target", targetID)

		// Resolve target provider
		targetProvider, err := router.FindProvider(targetID)
		if err != nil {
			f.logger.Warn("virtual target provider not found, skipping", attemptLog)
			continue
		}

		// Determine upstream model name from targetID
		targetModelName := targetID
		prefix := targetProvider.Name + "."
		if strings.HasPrefix(targetModelName, prefix) {
			targetModelName = targetModelName[len(prefix):]
		}

		// Skip if target is itself virtual (prevent recursion)
		if mc := FindModelConfig(targetProvider, targetModelName); mc != nil && mc.IsVirtual() {
			f.logger.Warn("virtual target is itself virtual, skipping", attemptLog)
			continue
		}

		// Resolve upstream model name (DisplayName -> Name)
		upstreamModel := targetModelName
		for _, m := range targetProvider.Models {
			if m.DisplayName == targetModelName && m.Name != targetModelName {
				upstreamModel = m.Name
				break
			}
		}

		// Set log entry model to "[virtualModel].[actualModel]" for traceability
		if entry, exists := c.Get("log_entry"); exists {
			entry.(*middleware.LogEntry).Model = virtualModel + "." + targetID
		}

		// Modify request body to use target's upstream model.
		// Use json.RawMessage to preserve original byte representation of all fields
		// except "model", so that nested content (messages, tools, etc.) is untouched.
		var modifiedBody []byte
		var bodyMap map[string]json.RawMessage
		if err := json.Unmarshal(body, &bodyMap); err == nil {
			bodyMap["model"] = json.RawMessage(`"` + upstreamModel + `"`)
			modifiedBody, _ = json.Marshal(bodyMap)
		} else {
			modifiedBody = body
		}

		// Merge model-specific extra_body
		if mc := FindModelConfig(targetProvider, targetModelName); mc != nil && mc.ExtraBody != nil {
			modifiedBody = MergeExtraBody(modifiedBody, mc.ExtraBody)
		}

		// Check target model's RPM limit (skip to next target if rate limited)
		if mc := FindModelConfig(targetProvider, targetModelName); mc != nil && mc.RPM > 0 {
			if !f.checkModelRPM(targetProvider.ID, mc.Name, mc.RPM) {
				f.logger.Warn("virtual target model rate limited, trying next", attemptLog, zap.Int("rpm", mc.RPM))
				continue
			}
		}

		// Update context for logging
		c.Set("provider_id", targetProvider.ID)

		// Build target URL
		targetURL := strings.TrimRight(targetProvider.BaseURL, "/") + path

		// Key rotation for this target provider
		keyCount := len(targetProvider.Keys)
		if keyCount == 0 {
			keyCount = 1
		}
		keyIdx := f.GetCurrentKeyIndex(targetProvider.ID)

		allKeysExhausted := true
		for keyAttempt := 0; keyAttempt < keyCount; {
			key := ProviderKeyAt(targetProvider, keyIdx)

			httpReq, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, bytes.NewReader(modifiedBody))
			if err != nil {
				f.logger.Error("virtual target: failed to create request", zap.Error(err))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
				return
			}

			if targetProvider.ProviderType == "anthropic" {
				httpReq.Header.Set("x-api-key", key)
				httpReq.Header.Set("anthropic-version", "2023-06-01")
			} else if targetProvider.ProviderType == "gemini" {
				// Gemini uses API key as query param, handled in gemini.go
			} else {
				httpReq.Header.Set("Authorization", "Bearer "+key)
			}
			httpReq.Header.Set("Content-Type", "application/json")

			for k := range c.Request.Header {
				if k == "Authorization" || k == "Content-Type" {
					continue
				}
				httpReq.Header.Set(k, c.Request.Header.Get(k))
			}

			resp, err := f.client.Do(httpReq)
			if err != nil {
				f.logger.Error("virtual target: upstream request failed", zap.Error(err))
				c.JSON(http.StatusBadGateway, gin.H{"error": "upstream request failed: " + err.Error()})
				return
			}

			if resp.StatusCode == 429 {
				resp.Body.Close()
				if keyAttempt+1 < keyCount {
					f.logger.Warn("virtual target rate limited (429), switching to next key",
						attemptLog,
						zap.Int("from_key_index", keyIdx),
						zap.Int("to_key_index", (keyIdx+1)%keyCount),
					)
					keyIdx = f.AdvanceKey(targetProvider.ID, keyCount)
					keyAttempt++
					continue
				}
				// All keys for this target exhausted
				f.logger.Warn("virtual target all keys rate limited, switching to next target", attemptLog)
				break
			}

			// Success - save virtual model index for next-request round-robin
			f.virtualModelIndex.Store(virtualModelKey(provider.ID, virtualModel), idx)
			allKeysExhausted = false

			c.Set("provider_key_index", keyIdx)

			f.writeUpstreamResp(c, resp, body, isStream)
			return
		}

		if allKeysExhausted {
			f.logger.Warn("virtual target exhausted, trying next",
				attemptLog,
				zap.Int("target_index", idx),
				zap.Int("remaining_targets", targetCount-attempt-1),
			)
		}
	}

	// All targets exhausted
	c.JSON(http.StatusTooManyRequests, gin.H{"error": "all virtual model targets returned 429"})
}

type openAIUsageDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type openAICompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type openAIUsage struct {
	PromptTokens             int                            `json:"prompt_tokens"`
	CompletionTokens         int                            `json:"completion_tokens"`
	PromptTokensDetails      *openAIUsageDetails            `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails  *openAICompletionTokensDetails `json:"completion_tokens_details,omitempty"`
	CacheCreationInputTokens int                            `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                            `json:"cache_read_input_tokens,omitempty"`
}

type openAIResponse struct {
	Usage *openAIUsage `json:"usage"`
}

func ParseTokens(body []byte) (int, int, int) {
	if len(body) == 0 {
		return 0, 0, 0
	}
	var resp openAIResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0, 0
	}
	if resp.Usage != nil {
		ict := 0
		if resp.Usage.PromptTokensDetails != nil {
			ict = resp.Usage.PromptTokensDetails.CachedTokens
		}
		if ict == 0 && resp.Usage.CacheReadInputTokens > 0 {
			ict = resp.Usage.CacheReadInputTokens
		}
		if ict == 0 && resp.Usage.CacheCreationInputTokens > 0 {
			ict = resp.Usage.CacheCreationInputTokens
		}
		ct := resp.Usage.CompletionTokens
		if resp.Usage.CompletionTokensDetails != nil && resp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			ct += resp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		return resp.Usage.PromptTokens, ct, ict
	}
	return 0, 0, 0
}

// sseWriter captures the last SSE data line containing "usage" for token counting,
// and accumulates response content from delta.content for token estimation fallback.
type sseWriter struct {
	writer    io.Writer
	buf       []byte
	lastUsage []byte
	content   strings.Builder
}

func (w *sseWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	// Scan for complete SSE lines
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := w.buf[:idx]
		w.buf = w.buf[idx+1:]
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := bytes.TrimSpace(line[6:])
			if bytes.Equal(data, []byte("[DONE]")) {
				continue
			}
			if bytes.Contains(data, []byte(`"usage"`)) {
				w.lastUsage = append([]byte{}, data...)
			}
			// Extract content from delta for token estimation fallback
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal(data, &chunk); err == nil {
				for _, c := range chunk.Choices {
					if c.Delta.Content != "" {
						w.content.WriteString(c.Delta.Content)
					}
				}
			}
		}
	}
	return w.writer.Write(p)
}
