package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/models"
	"go.uber.org/zap"
)

type LogEntry struct {
	RequestID   string
	StartTime   time.Time
	Model       string
	RequestType string
}

func RequestLogger(logger *zap.Logger, store db.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Only log actual LLM API calls, skip admin and internal endpoints
		if strings.HasPrefix(path, "/admin/") || path == "/v1/models" || path == "/" {
			c.Next()
			return
		}

		entry := &LogEntry{
			RequestID:   uuid.New().String(),
			StartTime:   time.Now(),
			RequestType: "chat",
		}

		switch {
		case strings.Contains(path, "/embeddings"):
			entry.RequestType = "embedding"
		case strings.Contains(path, "/messages"):
			entry.RequestType = "message"
		case strings.Contains(path, "/responses"):
			entry.RequestType = "responses"
		}

		c.Set("log_entry", entry)
		c.Set("request_id", entry.RequestID)
		c.Header("X-Request-ID", entry.RequestID)

		c.Next()

		elapsed := time.Since(entry.StartTime)

		if logEntry, exists := c.Get("log_entry"); exists {
			entry = logEntry.(*LogEntry)
		}

		statusCode := c.Writer.Status()

		promptTokens := 0
		completionTokens := 0
		inputCacheTokens := 0
		requestSummary := ""
		responseSummary := ""

		if pt, exists := c.Get("proxy_prompt_tokens"); exists {
			promptTokens = pt.(int)
		}
		if ct, exists := c.Get("proxy_completion_tokens"); exists {
			completionTokens = ct.(int)
		}
		if ict, exists := c.Get("proxy_input_cache_tokens"); exists {
			inputCacheTokens = ict.(int)
		}
		if rs, exists := c.Get("request_summary"); exists {
			requestSummary = rs.(string)
		}
		if rs2, exists := c.Get("response_summary"); exists {
			responseSummary = rs2.(string)
		}

		logger.Info("request",
			zap.String("request_id", entry.RequestID),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", statusCode),
			zap.Duration("latency", elapsed),
			zap.String("model", entry.Model),
			zap.String("type", entry.RequestType),
			zap.Int("prompt_tokens", promptTokens),
			zap.Int("completion_tokens", completionTokens),
			zap.Int("input_cache_tokens", inputCacheTokens),
		)

	go func() {
		reqLog := &models.RequestLog{
			RequestID:        entry.RequestID,
			Model:            entry.Model,
			RequestType:      entry.RequestType,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			InputCacheTokens: inputCacheTokens,
			LatencyMs:        elapsed.Milliseconds(),
			StatusCode:       statusCode,
			IsError:          statusCode >= 400,
			RequestSummary:   requestSummary,
			ResponseSummary:  responseSummary,
			CreatedAt:        time.Now(),
		}

		if apiKey, exists := c.Get("api_key"); exists {
			if ak, ok := apiKey.(*models.ApiKey); ok {
				reqLog.ApiKeyID = &ak.ID
				reqLog.ApiKeyName = ak.Name
			}
		}
		if providerID, exists := c.Get("provider_id"); exists {
			if pid, ok := providerID.(int64); ok {
				reqLog.ProviderID = &pid
			}
		}
		if keyIdx, exists := c.Get("provider_key_index"); exists {
			if ki, ok := keyIdx.(int); ok {
				reqLog.ProviderKeyIndex = ki
			}
		}

		store.InsertRequestLog(reqLog)
	}()
	}
}
