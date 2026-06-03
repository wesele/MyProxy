package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/middleware"
	"github.com/user/qwenportal/internal/proxy"
	"go.uber.org/zap"
)

type GeminiHandler struct {
	forwarder *proxy.Forwarder
	router    *proxy.Router
	logger    *zap.Logger
}

func NewGeminiHandler(f *proxy.Forwarder, r *proxy.Router, l *zap.Logger) *GeminiHandler {
	return &GeminiHandler{forwarder: f, router: r, logger: l}
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiRequest struct {
	Contents         []geminiContent    `json:"contents"`
	SystemInstruction *geminiContent    `json:"systemInstruction,omitempty"`
	GenerationConfig *geminiGenConfig   `json:"generationConfig,omitempty"`
}

type geminiGenConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
	TopP            float64 `json:"topP,omitempty"`
	TopK            int     `json:"topK,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	UsageMetadata *geminiUsage `json:"usageMetadata,omitempty"`
}

type geminiCandidate struct {
	Content       geminiContent `json:"content"`
	FinishReason  string        `json:"finishReason"`
	Index         int           `json:"index,omitempty"`
}

type geminiUsageDetails struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

type geminiUsage struct {
	PromptTokenCount       int                  `json:"promptTokenCount"`
	CandidatesTokenCount   int                  `json:"candidatesTokenCount"`
	TotalTokenCount        int                  `json:"totalTokenCount"`
	CandidatesTokensDetails []geminiUsageDetails `json:"candidatesTokensDetails,omitempty"`
}

type geminiSSEChunk struct {
	Candidates    []geminiCandidate `json:"candidates,omitempty"`
	UsageMetadata *geminiUsage      `json:"usageMetadata,omitempty"`
}

func translateOpenAIToGemini(body []byte) ([]byte, string, error) {
	var openAIReq struct {
		Model        string  `json:"model"`
		Messages     []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Stream       bool    `json:"stream"`
		MaxTokens    int     `json:"max_tokens"`
		Temperature  float64 `json:"temperature"`
		TopP         float64 `json:"top_p"`
		TopK         int     `json:"top_k"`
	}
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		return nil, "", fmt.Errorf("failed to parse request: %w", err)
	}

	gReq := geminiRequest{}
	var systemContent string

	for _, msg := range openAIReq.Messages {
		switch msg.Role {
		case "system":
			systemContent = msg.Content
		case "user":
			gReq.Contents = append(gReq.Contents, geminiContent{
				Role:  "user",
				Parts: []geminiPart{{Text: msg.Content}},
			})
		case "assistant":
			gReq.Contents = append(gReq.Contents, geminiContent{
				Role:  "model",
				Parts: []geminiPart{{Text: msg.Content}},
			})
		}
	}

	if systemContent != "" {
		gReq.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: systemContent}},
		}
	}

	if openAIReq.MaxTokens > 0 || openAIReq.Temperature > 0 || openAIReq.TopP > 0 || openAIReq.TopK > 0 {
		cfg := &geminiGenConfig{}
		if openAIReq.MaxTokens > 0 {
			cfg.MaxOutputTokens = openAIReq.MaxTokens
		}
		if openAIReq.Temperature > 0 {
			cfg.Temperature = openAIReq.Temperature
		}
		if openAIReq.TopP > 0 {
			cfg.TopP = openAIReq.TopP
		}
		if openAIReq.TopK > 0 {
			cfg.TopK = openAIReq.TopK
		}
		gReq.GenerationConfig = cfg
	}

	bodyBytes, err := json.Marshal(gReq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	return bodyBytes, openAIReq.Model, nil
}

func translateGeminiToOpenAI(body []byte, model string) []byte {
	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return body
	}

	openAIResp := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{},
	}

	for i, c := range geminiResp.Candidates {
		content := ""
		for _, p := range c.Content.Parts {
			content += p.Text
		}

		finishReason := "stop"
		switch c.FinishReason {
		case "STOP":
			finishReason = "stop"
		case "MAX_TOKENS":
			finishReason = "length"
		case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			finishReason = "content_filter"
		default:
			if c.FinishReason != "" {
				finishReason = strings.ToLower(c.FinishReason)
			}
		}

		openAIResp["choices"] = append(openAIResp["choices"].([]map[string]interface{}), map[string]interface{}{
			"index":         i,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": content,
			},
			"finish_reason": finishReason,
		})
	}

	if geminiResp.UsageMetadata != nil {
		pt := geminiResp.UsageMetadata.PromptTokenCount
		ttl := geminiResp.UsageMetadata.TotalTokenCount
		if ttl == 0 {
			ttl = pt + geminiResp.UsageMetadata.CandidatesTokenCount
		}
		ct := geminiResp.UsageMetadata.CandidatesTokenCount
		reasoning := 0
		for _, d := range geminiResp.UsageMetadata.CandidatesTokensDetails {
			if d.Modality == "THINK" {
				reasoning += d.TokenCount
			}
		}
		usage := map[string]interface{}{
			"prompt_tokens":     pt,
			"completion_tokens": ct,
			"total_tokens":      ttl,
		}
		if reasoning > 0 {
			usage["completion_tokens_details"] = map[string]int{
				"reasoning_tokens": reasoning,
			}
		}
		openAIResp["usage"] = usage
	}

	result, _ := json.Marshal(openAIResp)
	return result
}

func translateGeminiChunkToOpenAI(chunkData []byte, model string) []byte {
	var geminiChunk geminiSSEChunk
	if err := json.Unmarshal(chunkData, &geminiChunk); err != nil {
		return chunkData
	}

	if len(geminiChunk.Candidates) == 0 {
		return nil
	}

	c := geminiChunk.Candidates[0]
	content := ""
	for _, p := range c.Content.Parts {
		content += p.Text
	}

	finishReason := ""
	switch c.FinishReason {
	case "STOP":
		finishReason = "stop"
	case "MAX_TOKENS":
		finishReason = "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		finishReason = "content_filter"
	default:
		if c.FinishReason != "" {
			finishReason = strings.ToLower(c.FinishReason)
		}
	}

	openAIChunk := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]string{
					"content": content,
				},
			},
		},
	}

	if finishReason != "" {
		openAIChunk["choices"].([]map[string]interface{})[0]["finish_reason"] = finishReason
		openAIChunk["choices"].([]map[string]interface{})[0]["delta"] = map[string]string{}
	}

	if geminiChunk.UsageMetadata != nil {
		pt := geminiChunk.UsageMetadata.PromptTokenCount
		ct := geminiChunk.UsageMetadata.CandidatesTokenCount
		openAIChunk["usage"] = map[string]int{
			"prompt_tokens":     pt,
			"completion_tokens": ct,
			"total_tokens":      pt + ct,
		}
	}

	result, _ := json.Marshal(openAIChunk)
	return result
}

func (h *GeminiHandler) ChatCompletions(c *gin.Context) {
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

	geminiBody, modelName, err := translateOpenAIToGemini(body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	baseURL := strings.TrimRight(provider.BaseURL, "/")
	var targetURL string
	if reqBody.Stream {
		targetURL = fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", baseURL, modelName, provider.APIKey)
	} else {
		targetURL = fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, modelName, provider.APIKey)
	}

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(geminiBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create request"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		h.logger.Error("gemini upstream request failed", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if reqBody.Stream || strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		c.Writer.WriteHeader(resp.StatusCode)
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		reader := newGeminiStreamReader(resp.Body, modelName)
		io.Copy(c.Writer, reader)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read response"})
		return
	}

	pt, ct, ict := parseGeminiUsage(respBody)
	c.Set("proxy_prompt_tokens", pt)
	c.Set("proxy_completion_tokens", ct)
	c.Set("proxy_input_cache_tokens", ict)

	c.Set("request_summary", proxy.ExtractRequestSummary(body))

	translated := translateGeminiToOpenAI(respBody, modelName)
	c.Set("response_summary", proxy.ExtractResponseSummary(translated))

	c.Data(resp.StatusCode, "application/json", translated)
}

func parseGeminiUsage(body []byte) (int, int, int) {
	var resp geminiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, 0, 0
	}
	if resp.UsageMetadata != nil {
		ct := resp.UsageMetadata.CandidatesTokenCount
		// candidatesTokenCount already includes thinking tokens;
		// also account for any tokens in candidatesTokensDetails breakdown
		detailsTotal := 0
		for _, d := range resp.UsageMetadata.CandidatesTokensDetails {
			detailsTotal += d.TokenCount
		}
		if detailsTotal > ct {
			ct = detailsTotal
		}
		return resp.UsageMetadata.PromptTokenCount, ct, 0
	}
	return 0, 0, 0
}

type geminiStreamReader struct {
	reader    io.ReadCloser
	buf       []byte
	done      bool
	model     string
}

func newGeminiStreamReader(reader io.ReadCloser, model string) *geminiStreamReader {
	return &geminiStreamReader{
		reader: reader,
		model:  model,
	}
}

func (r *geminiStreamReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}

	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	buf := make([]byte, 4096)
	n, err := r.reader.Read(buf)
	if err != nil && err != io.EOF {
		return 0, err
	}
	if n == 0 || err == io.EOF {
		r.done = true
		return 0, io.EOF
	}

	data := buf[:n]
	var result []byte

	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			chunkData := bytes.TrimSpace(line[6:])
			if len(chunkData) == 0 || bytes.Equal(chunkData, []byte("[DONE]")) {
				continue
			}
			translated := translateGeminiChunkToOpenAI(chunkData, r.model)
			if translated != nil {
				result = append(result, []byte("data: ")...)
				result = append(result, translated...)
				result = append(result, []byte("\n\n")...)
			}
		}
	}

	if len(result) == 0 {
		return 0, nil
	}

	n = copy(p, result)
	if n < len(result) {
		r.buf = append([]byte{}, result[n:]...)
	}
	return n, nil
}
