package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/models"
	"go.uber.org/zap"
)

type Forwarder struct {
	client *http.Client
	logger *zap.Logger
}

func NewForwarder(logger *zap.Logger) *Forwarder {
	return &Forwarder{
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		logger: logger,
	}
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
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Truncate(string(body), 200)
	}
	// Find last user message
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return Truncate(req.Messages[i].Content, 200)
		}
		// Also accept "system" as context summary
		if req.Messages[i].Role == "system" && len(req.Messages) == 1 {
			return Truncate(req.Messages[i].Content, 200)
		}
	}
	return Truncate(string(body), 200)
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

func (f *Forwarder) Forward(c *gin.Context, provider *models.Provider, path string) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read request body"})
		return
	}

	// Store request summary on context
	c.Set("request_summary", ExtractRequestSummary(body))

	var reqBody struct {
		Stream bool `json:"stream"`
	}
	isStream := false
	if err := json.Unmarshal(body, &reqBody); err == nil && reqBody.Stream {
		isStream = true
	}

	targetURL := strings.TrimRight(provider.BaseURL, "/") + path

	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}

	if provider.ProviderType == "anthropic" {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if provider.ProviderType == "gemini" {
		// Gemini uses API key as query param, handled in gemini.go
	} else {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
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
	defer resp.Body.Close()

	for k, v := range resp.Header {
		for _, vv := range v {
			c.Header(k, vv)
		}
	}

	if isStream || strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		c.Set("proxy_streaming", true)
		c.Writer.WriteHeader(resp.StatusCode)
		// Wrap writer to capture token usage from SSE
		sw := &sseWriter{writer: c.Writer, buf: make([]byte, 0, 4096)}
		if _, err := io.Copy(sw, resp.Body); err == nil {
			pt, ct, ict := parseTokens(sw.lastUsage)
			if pt+ct+ict == 0 {
				// Fallback: estimate tokens from content when API doesn't provide usage info
				pt = estimatePromptTokens(body)
				if sw.content.Len() > 0 {
					ct = sw.content.Len() / 4
				}
			}
			if pt+ct+ict > 0 {
				c.Set("proxy_prompt_tokens", pt)
				c.Set("proxy_completion_tokens", ct)
				c.Set("proxy_input_cache_tokens", ict)
			}
		}
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read response"})
		return
	}

	pt, ct, ict := parseTokens(respBody)
	c.Set("proxy_prompt_tokens", pt)
	c.Set("proxy_completion_tokens", ct)
	c.Set("proxy_input_cache_tokens", ict)

	// Store response summary
	c.Set("response_summary", ExtractResponseSummary(respBody))

	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
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

func parseTokens(body []byte) (int, int, int) {
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
