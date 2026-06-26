package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/middleware"
	"github.com/user/qwenportal/internal/models"
	"github.com/user/qwenportal/internal/proxy"
	"go.uber.org/zap"
)

type ResponsesHandler struct {
	forwarder     *proxy.Forwarder
	router        *proxy.Router
	logger        *zap.Logger
	geminiHandler *GeminiHandler
	store         db.Store
	httpClient    *http.Client
}

func NewResponsesHandler(f *proxy.Forwarder, r *proxy.Router, l *zap.Logger, s db.Store) *ResponsesHandler {
	return &ResponsesHandler{
		forwarder:  f,
		router:     r,
		logger:     l,
		store:      s,
		httpClient: proxy.NewHTTPClient(5 * time.Minute),
	}
}

func (h *ResponsesHandler) SetGeminiHandler(gh *GeminiHandler) {
	h.geminiHandler = gh
}

func generateResponseID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "resp_" + hex.EncodeToString(b)
}

func generateMessageID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "msg_" + hex.EncodeToString(b)
}

type responsesInputItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesRequestBody struct {
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Instructions    string          `json:"instructions,omitempty"`
	MaxOutputTokens int             `json:"max_output_tokens,omitempty"`
	Temperature     float64         `json:"temperature,omitempty"`
	TopP            float64         `json:"top_p,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
}

type responsesOutputText struct {
	Type        string   `json:"type"`
	Text        string   `json:"text"`
	Annotations []string `json:"annotations"`
}

type responsesOutputItem struct {
	ID      string                `json:"id"`
	Type    string                `json:"type"`
	Status  string                `json:"status,omitempty"`
	Role    string                `json:"role,omitempty"`
	Content []responsesOutputText `json:"content,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesResponse struct {
	ID        string                `json:"id"`
	Object    string                `json:"object"`
	CreatedAt int64                 `json:"created_at"`
	Model     string                `json:"model"`
	Output    []responsesOutputItem `json:"output"`
	Usage     *responsesUsage       `json:"usage,omitempty"`
}

type responsesError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type responsesErrorResponse struct {
	ID        string          `json:"id"`
	Object    string          `json:"object"`
	CreatedAt int64           `json:"created_at"`
	Error     responsesError  `json:"error"`
}

func parseResponsesInput(input json.RawMessage) ([]responsesInputItem, error) {
	var text string
	if err := json.Unmarshal(input, &text); err == nil {
		return []responsesInputItem{{Role: "user", Content: text}}, nil
	}
	var items []responsesInputItem
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("input must be a string or array of items")
	}
	return items, nil
}

func responsesInputToMessages(input []responsesInputItem, instructions string) []map[string]interface{} {
	var messages []map[string]interface{}
	if instructions != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": instructions,
		})
	}
	for _, item := range input {
		messages = append(messages, map[string]interface{}{
			"role":    item.Role,
			"content": item.Content,
		})
	}
	return messages
}

func buildChatCompletionsBody(respReq responsesRequestBody, upstreamModel string) []byte {
	messages := responsesInputToMessages(parseInputItems(respReq.Input), respReq.Instructions)

	body := map[string]interface{}{
		"model":    upstreamModel,
		"messages": messages,
	}
	if respReq.Stream {
		body["stream"] = true
	}
	if respReq.MaxOutputTokens > 0 {
		body["max_tokens"] = respReq.MaxOutputTokens
	}
	if respReq.Temperature > 0 {
		body["temperature"] = respReq.Temperature
	}
	if respReq.TopP > 0 {
		body["top_p"] = respReq.TopP
	}
	result, _ := json.Marshal(body)
	return result
}

func parseInputItems(input json.RawMessage) []responsesInputItem {
	items, err := parseResponsesInput(input)
	if err != nil {
		return []responsesInputItem{{Role: "user", Content: string(input)}}
	}
	return items
}

func chatCompletionsToResponses(chatBody []byte, model string, respID string, msgID string) []byte {
	var chatResp struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Choices []struct {
			Index        int `json:"index"`
			Message      struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}
	if err := json.Unmarshal(chatBody, &chatResp); err != nil {
		return chatBody
	}

	resp := responsesResponse{
		ID:        respID,
		Object:    "response",
		CreatedAt: chatResp.Created,
		Model:     model,
		Output:    []responsesOutputItem{},
	}

	if len(chatResp.Choices) > 0 {
		content := chatResp.Choices[0].Message.Content
		resp.Output = append(resp.Output, responsesOutputItem{
			ID:     msgID,
			Type:   "message",
			Status: "completed",
			Role:   "assistant",
			Content: []responsesOutputText{{
				Type:        "output_text",
				Text:        content,
				Annotations: []string{},
			}},
		})
	}

	if chatResp.Usage != nil {
		resp.Usage = &responsesUsage{
			InputTokens:  chatResp.Usage.PromptTokens,
			OutputTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:  chatResp.Usage.TotalTokens,
		}
	}

	result, _ := json.Marshal(resp)
	return result
}

func (h *ResponsesHandler) Responses(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, responsesErrorResponse{
			Object: "response",
			Error:  responsesError{Type: "invalid_request_error", Message: "failed to read request body"},
		})
		return
	}

	var respReq responsesRequestBody
	if err := json.Unmarshal(body, &respReq); err != nil {
		c.JSON(http.StatusBadRequest, responsesErrorResponse{
			Object: "response",
			Error:  responsesError{Type: "invalid_request_error", Message: "invalid request body"},
		})
		return
	}

	if respReq.Model == "" {
		c.JSON(http.StatusBadRequest, responsesErrorResponse{
			Object: "response",
			Error:  responsesError{Type: "invalid_request_error", Message: "model is required"},
		})
		return
	}

	if respReq.Input == nil || len(respReq.Input) == 0 {
		c.JSON(http.StatusBadRequest, responsesErrorResponse{
			Object: "response",
			Error:  responsesError{Type: "invalid_request_error", Message: "input is required"},
		})
		return
	}

	if entry, exists := c.Get("log_entry"); exists {
		entry.(*middleware.LogEntry).Model = respReq.Model
	}

	provider, err := h.router.FindProvider(respReq.Model)
	if err != nil {
		c.JSON(http.StatusNotFound, responsesErrorResponse{
			Object: "response",
			Error:  responsesError{Type: "not_found", Message: err.Error()},
		})
		return
	}

	modelName := strings.TrimPrefix(respReq.Model, provider.Name+".")
	upstreamModel := modelName
	for _, m := range provider.Models {
		if m.DisplayName == modelName && m.Name != modelName {
			upstreamModel = m.Name
			break
		}
	}

	c.Set("provider_id", provider.ID)

	if provider.ProviderType == "gemini" && h.geminiHandler != nil {
		h.handleGeminiResponses(c, body, provider, upstreamModel, respReq)
		return
	}

	h.handleStandardResponses(c, provider, upstreamModel, respReq)
}

func (h *ResponsesHandler) handleStandardResponses(c *gin.Context, provider *models.Provider, upstreamModel string, respReq responsesRequestBody) {
	chatBody := buildChatCompletionsBody(respReq, upstreamModel)

	respID := generateResponseID()
	msgID := generateMessageID()

	// Merge model-specific extra_body if present
	if mc := proxy.FindModelConfig(provider, respReq.Model); mc != nil && mc.ExtraBody != nil {
		chatBody = proxy.MergeExtraBody(chatBody, mc.ExtraBody)
	}

	c.Set("request_summary", extractResponsesSummary(respReq))

	targetURL := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"

	keyCount := len(provider.Keys)
	if keyCount == 0 {
		keyCount = 1
	}
	keyIdx := h.forwarder.GetCurrentKeyIndex(provider.ID)

	for attempt := 0; attempt < keyCount; {
		key := proxy.ProviderKeyAt(provider, keyIdx)

		req, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(chatBody))
		if err != nil {
			c.JSON(http.StatusInternalServerError, responsesErrorResponse{
				Object: "response",
				Error:  responsesError{Type: "server_error", Message: "failed to create request"},
			})
			return
		}

		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")

		resp, err := h.httpClient.Do(req)
		if err != nil {
			h.logger.Error("upstream request failed", zap.Error(err))
			c.JSON(http.StatusBadGateway, responsesErrorResponse{
				Object: "response",
				Error:  responsesError{Type: "upstream_error", Message: "upstream request failed: " + err.Error()},
			})
			return
		}

		if resp.StatusCode == 429 && attempt+1 < keyCount {
			resp.Body.Close()
			h.logger.Warn("responses rate limited (429), switching to next key",
				zap.Int("from_key_index", keyIdx),
				zap.Int("to_key_index", (keyIdx+1)%keyCount),
			)
			keyIdx = h.forwarder.AdvanceKey(provider.ID, keyCount)
			attempt++
			continue
		}

		c.Set("provider_key_index", keyIdx)
		defer resp.Body.Close()

		if respReq.Stream || strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			h.streamChatCompletionsToResponses(c, resp.Body, respID, msgID, upstreamModel)
			return
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, responsesErrorResponse{
				Object: "response",
				Error:  responsesError{Type: "server_error", Message: "failed to read response"},
			})
			return
		}

		pt, ct, ict := proxy.ParseTokens(respBody)
		c.Set("proxy_prompt_tokens", pt)
		c.Set("proxy_completion_tokens", ct)
		c.Set("proxy_input_cache_tokens", ict)

		translated := chatCompletionsToResponses(respBody, upstreamModel, respID, msgID)
		c.Set("response_summary", proxy.ExtractResponseSummary(respBody))

		c.Data(resp.StatusCode, "application/json", translated)
		return
	}
}

func (h *ResponsesHandler) handleGeminiResponses(c *gin.Context, origBody []byte, provider *models.Provider, upstreamModel string, respReq responsesRequestBody) {
	respID := generateResponseID()
	msgID := generateMessageID()

	// Parse original responses input
	items := parseInputItems(respReq.Input)

	// Build messages array
	messages := responsesInputToMessages(items, respReq.Instructions)

	// Create a chat completions style body for translation
	chatBodyMap := map[string]interface{}{
		"model":    upstreamModel,
		"messages": messages,
	}
	if respReq.Stream {
		chatBodyMap["stream"] = true
	}
	if respReq.MaxOutputTokens > 0 {
		chatBodyMap["max_tokens"] = respReq.MaxOutputTokens
	}
	if respReq.Temperature > 0 {
		chatBodyMap["temperature"] = respReq.Temperature
	}
	if respReq.TopP > 0 {
		chatBodyMap["top_p"] = respReq.TopP
	}
	chatBytes, _ := json.Marshal(chatBodyMap)

	geminiBody, geminiModel, err := translateOpenAIToGemini(chatBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, responsesErrorResponse{
			Object: "response",
			Error:  responsesError{Type: "server_error", Message: err.Error()},
		})
		return
	}

	c.Set("request_summary", extractResponsesSummary(respReq))

	baseURL := strings.TrimRight(provider.BaseURL, "/")

	keyCount := len(provider.Keys)
	if keyCount == 0 {
		keyCount = 1
	}
	keyIdx := h.forwarder.GetCurrentKeyIndex(provider.ID)

	for attempt := 0; attempt < keyCount; {
		key := proxy.ProviderKeyAt(provider, keyIdx)

		var targetURL string
		if respReq.Stream {
			targetURL = fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", baseURL, geminiModel, key)
		} else {
			targetURL = fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, geminiModel, key)
		}

		httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(geminiBody))
		if err != nil {
			c.JSON(http.StatusInternalServerError, responsesErrorResponse{
				Object: "response",
				Error:  responsesError{Type: "server_error", Message: "failed to create request"},
			})
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := h.httpClient.Do(httpReq)
		if err != nil {
			h.logger.Error("gemini upstream request failed", zap.Error(err))
			c.JSON(http.StatusBadGateway, responsesErrorResponse{
				Object: "response",
				Error:  responsesError{Type: "upstream_error", Message: "upstream request failed: " + err.Error()},
			})
			return
		}

		if resp.StatusCode == 429 && attempt+1 < keyCount {
			resp.Body.Close()
			h.logger.Warn("responses gemini rate limited (429), switching to next key",
				zap.Int("from_key_index", keyIdx),
				zap.Int("to_key_index", (keyIdx+1)%keyCount),
			)
			keyIdx = h.forwarder.AdvanceKey(provider.ID, keyCount)
			attempt++
			continue
		}

		c.Set("provider_key_index", keyIdx)
		defer resp.Body.Close()

		if respReq.Stream {
			h.streamGeminiToResponses(c, resp.Body, respID, msgID, upstreamModel)
			return
		}

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, responsesErrorResponse{
				Object: "response",
				Error:  responsesError{Type: "server_error", Message: "failed to read response"},
			})
			return
		}

		pt, ct, ict := parseGeminiUsage(respBody)
		c.Set("proxy_prompt_tokens", pt)
		c.Set("proxy_completion_tokens", ct)
		c.Set("proxy_input_cache_tokens", ict)

		// Convert gemini -> chat completions -> responses
		chatResp := translateGeminiToOpenAI(respBody, upstreamModel)
		translated := chatCompletionsToResponses(chatResp, upstreamModel, respID, msgID)
		c.Set("response_summary", proxy.ExtractResponseSummary(chatResp))

		c.Data(resp.StatusCode, "application/json", translated)
		return
	}
}

func extractResponsesSummary(req responsesRequestBody) string {
	items := parseInputItems(req.Input)
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Role == "user" {
			return proxy.Truncate(items[i].Content, 200)
		}
	}
	if len(items) > 0 {
		return proxy.Truncate(items[0].Content, 200)
	}
	return proxy.Truncate(string(req.Input), 200)
}

func (h *ResponsesHandler) streamChatCompletionsToResponses(c *gin.Context, upstreamBody io.ReadCloser, respID string, msgID string, model string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, canFlush := c.Writer.(http.Flusher)

	createdAt := time.Now().Unix()

	// Send response.created event
	createdEvent, _ := json.Marshal(map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":         respID,
			"object":     "response",
			"created_at": createdAt,
			"model":      model,
			"output":     []interface{}{},
		},
	})
	fmt.Fprintf(c.Writer, "event: response.created\ndata: %s\n\n", createdEvent)
	if canFlush {
		flusher.Flush()
	}

	var fullContent strings.Builder
	var finalUsage []byte

	buf := make([]byte, 4096)
	lineBuf := make([]byte, 0, 4096)
	for {
		n, err := upstreamBody.Read(buf)
		if n > 0 {
			lineBuf = append(lineBuf, buf[:n]...)
			for {
				idx := bytes.IndexByte(lineBuf, '\n')
				if idx < 0 {
					break
				}
				line := lineBuf[:idx]
				lineBuf = lineBuf[idx+1:]
				line = bytes.TrimSpace(line)
				if !bytes.HasPrefix(line, []byte("data: ")) {
					continue
				}
				data := bytes.TrimSpace(line[6:])
				if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
					continue
				}

				if bytes.Contains(data, []byte(`"usage"`)) {
					finalUsage = append([]byte{}, data...)
				}

				var chunk struct {
					Choices []struct {
						Index        int `json:"index"`
						Delta        struct {
							Role    string `json:"role"`
							Content string `json:"content"`
						} `json:"delta"`
						FinishReason string `json:"finish_reason"`
					} `json:"choices"`
				}
				if err := json.Unmarshal(data, &chunk); err != nil {
					continue
				}

				for _, choice := range chunk.Choices {
					if choice.Delta.Content != "" {
						fullContent.WriteString(choice.Delta.Content)
						// Send delta event for each content chunk
						deltaEvent, _ := json.Marshal(map[string]interface{}{
							"type":          "response.output_text.delta",
							"delta":         choice.Delta.Content,
							"item_id":       msgID,
							"content_index": 0,
						})
						fmt.Fprintf(c.Writer, "event: response.output_text.delta\ndata: %s\n\n", deltaEvent)
						if canFlush {
							flusher.Flush()
						}
					}

					if choice.FinishReason != "" {
						doneEvent, _ := json.Marshal(map[string]interface{}{
							"type":          "response.output_text.done",
							"item_id":       msgID,
							"content_index": 0,
							"text":          fullContent.String(),
						})
						fmt.Fprintf(c.Writer, "event: response.output_text.done\ndata: %s\n\n", doneEvent)
						if canFlush {
							flusher.Flush()
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	// Process any remaining data in lineBuf
	if len(lineBuf) > 0 {
		line := bytes.TrimSpace(lineBuf)
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := bytes.TrimSpace(line[6:])
			if len(data) > 0 && !bytes.Equal(data, []byte("[DONE]")) {
				if bytes.Contains(data, []byte(`"usage"`)) {
					finalUsage = append([]byte{}, data...)
				}
			}
		}
	}

	// Build usage
	pt, ct, ict := proxy.ParseTokens(finalUsage)
	if pt+ct+ict == 0 && fullContent.Len() > 0 {
		ct = fullContent.Len() / 4
	}
	c.Set("proxy_prompt_tokens", pt)
	c.Set("proxy_completion_tokens", ct)
	c.Set("proxy_input_cache_tokens", ict)

	usage := map[string]int{
		"input_tokens":  pt,
		"output_tokens": ct,
		"total_tokens":  pt + ct + ict,
	}

	// Recalculate prevContentLen... actually we don't need this. just use final version.
	outputItems := []map[string]interface{}{
		{
			"id":     msgID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]interface{}{
				{
					"type":        "output_text",
					"text":        fullContent.String(),
					"annotations": []string{},
				},
			},
		},
	}

	completedEvent, _ := json.Marshal(map[string]interface{}{
		"type": "response.completed",
		"response": map[string]interface{}{
			"id":         respID,
			"object":     "response",
			"created_at": createdAt,
			"model":      model,
			"output":     outputItems,
			"usage":      usage,
		},
	})
	fmt.Fprintf(c.Writer, "event: response.completed\ndata: %s\n\n", completedEvent)
	if canFlush {
		flusher.Flush()
	}
}

func (h *ResponsesHandler) streamGeminiToResponses(c *gin.Context, upstreamBody io.ReadCloser, respID string, msgID string, model string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, canFlush := c.Writer.(http.Flusher)

	createdAt := time.Now().Unix()

	// Send response.created event
	createdEvent, _ := json.Marshal(map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":         respID,
			"object":     "response",
			"created_at": createdAt,
			"model":      model,
			"output":     []interface{}{},
		},
	})
	fmt.Fprintf(c.Writer, "event: response.created\ndata: %s\n\n", createdEvent)
	if canFlush {
		flusher.Flush()
	}

	var fullContent strings.Builder
	var finalUsage []byte

	reader := newGeminiStreamReader(upstreamBody, model)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			// The geminiStreamReader returns OpenAI-format SSE data: lines.
			// Parse them to extract content and usage.
			sseData := buf[:n]
			for {
				idx := bytes.IndexByte(sseData, '\n')
				if idx < 0 {
					break
				}
				line := sseData[:idx]
				sseData = sseData[idx+1:]
				line = bytes.TrimSpace(line)
				if !bytes.HasPrefix(line, []byte("data: ")) {
					continue
				}
				data := bytes.TrimSpace(line[6:])
				if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
					continue
				}

				if bytes.Contains(data, []byte(`"usage"`)) {
					finalUsage = append([]byte{}, data...)
				}

				var chunk struct {
					Choices []struct {
						Index        int `json:"index"`
						Delta        struct {
							Content string `json:"content"`
						} `json:"delta"`
						FinishReason string `json:"finish_reason"`
					} `json:"choices"`
				}
				if err := json.Unmarshal(data, &chunk); err != nil {
					continue
				}

				for _, choice := range chunk.Choices {
					if choice.Delta.Content != "" {
						fullContent.WriteString(choice.Delta.Content)
						deltaEvent, _ := json.Marshal(map[string]interface{}{
							"type":          "response.output_text.delta",
							"delta":         choice.Delta.Content,
							"item_id":       msgID,
							"content_index": 0,
						})
						fmt.Fprintf(c.Writer, "event: response.output_text.delta\ndata: %s\n\n", deltaEvent)
						if canFlush {
							flusher.Flush()
						}
					}

					if choice.FinishReason != "" {
						doneEvent, _ := json.Marshal(map[string]interface{}{
							"type":          "response.output_text.done",
							"item_id":       msgID,
							"content_index": 0,
							"text":          fullContent.String(),
						})
						fmt.Fprintf(c.Writer, "event: response.output_text.done\ndata: %s\n\n", doneEvent)
						if canFlush {
							flusher.Flush()
						}
					}
				}
			}
		}
		if err != nil {
			break
		}
	}

	pt, ct, ict := proxy.ParseTokens(finalUsage)
	if pt+ct+ict == 0 && fullContent.Len() > 0 {
		ct = fullContent.Len() / 4
	}
	c.Set("proxy_prompt_tokens", pt)
	c.Set("proxy_completion_tokens", ct)
	c.Set("proxy_input_cache_tokens", ict)

	usage := map[string]int{
		"input_tokens":  pt,
		"output_tokens": ct,
		"total_tokens":  pt + ct + ict,
	}

	outputItems := []map[string]interface{}{
		{
			"id":     msgID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]interface{}{
				{
					"type":        "output_text",
					"text":        fullContent.String(),
					"annotations": []string{},
				},
			},
		},
	}

	completedEvent, _ := json.Marshal(map[string]interface{}{
		"type": "response.completed",
		"response": map[string]interface{}{
			"id":         respID,
			"object":     "response",
			"created_at": createdAt,
			"model":      model,
			"output":     outputItems,
			"usage":      usage,
		},
	})
	fmt.Fprintf(c.Writer, "event: response.completed\ndata: %s\n\n", completedEvent)
	if canFlush {
		flusher.Flush()
	}
}
