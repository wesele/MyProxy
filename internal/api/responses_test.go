package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/qwenportal/internal/models"
	"github.com/user/qwenportal/internal/proxy"
)

func TestParseResponsesInput(t *testing.T) {
	t.Run("string input returns single user item", func(t *testing.T) {
		input := json.RawMessage(`"Hello, world!"`)
		items, err := parseResponsesInput(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		if items[0].Role != "user" {
			t.Errorf("expected role 'user', got %q", items[0].Role)
		}
		if items[0].Content != "Hello, world!" {
			t.Errorf("expected content 'Hello, world!', got %q", items[0].Content)
		}
	})

	t.Run("array input", func(t *testing.T) {
		input := json.RawMessage(`[{"role":"user","content":"hi"},{"role":"assistant","content":"hello"}]`)
		items, err := parseResponsesInput(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
		if items[0].Role != "user" || items[0].Content != "hi" {
			t.Errorf("first item mismatch: %+v", items[0])
		}
		if items[1].Role != "assistant" || items[1].Content != "hello" {
			t.Errorf("second item mismatch: %+v", items[1])
		}
	})

	t.Run("invalid input returns error", func(t *testing.T) {
		input := json.RawMessage(`{"invalid":true}`)
		_, err := parseResponsesInput(input)
		if err == nil {
			t.Error("expected error for invalid input")
		}
	})
}

func TestResponsesInputToMessages(t *testing.T) {
	t.Run("without instructions", func(t *testing.T) {
		items := []responsesInputItem{
			{Role: "user", Content: "hello"},
		}
		msgs := responsesInputToMessages(items, "")
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		if msgs[0]["role"] != "user" {
			t.Errorf("expected role 'user', got %v", msgs[0]["role"])
		}
	})

	t.Run("with instructions prepends system", func(t *testing.T) {
		items := []responsesInputItem{
			{Role: "user", Content: "hello"},
		}
		msgs := responsesInputToMessages(items, "Be helpful")
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}
		if msgs[0]["role"] != "system" {
			t.Errorf("expected first message role 'system', got %v", msgs[0]["role"])
		}
		if msgs[0]["content"] != "Be helpful" {
			t.Errorf("expected system content 'Be helpful', got %v", msgs[0]["content"])
		}
		if msgs[1]["role"] != "user" {
			t.Errorf("expected second message role 'user', got %v", msgs[1]["role"])
		}
	})
}

func TestBuildChatCompletionsBody(t *testing.T) {
	t.Run("basic conversion", func(t *testing.T) {
		input := json.RawMessage(`"hello"`)
		req := responsesRequestBody{
			Model: "gpt-4",
			Input: input,
		}
		body := buildChatCompletionsBody(req, "gpt-4")
		var parsed map[string]interface{}
		json.Unmarshal(body, &parsed)
		if parsed["model"] != "gpt-4" {
			t.Errorf("expected model 'gpt-4', got %v", parsed["model"])
		}
		msgs := parsed["messages"].([]interface{})
		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}
		msg := msgs[0].(map[string]interface{})
		if msg["role"] != "user" || msg["content"] != "hello" {
			t.Errorf("message mismatch: %+v", msg)
		}
	})

	t.Run("with stream and params", func(t *testing.T) {
		input := json.RawMessage(`[{"role":"user","content":"hi"}]`)
		req := responsesRequestBody{
			Model:           "gpt-4",
			Input:           input,
			Instructions:    "Be concise",
			MaxOutputTokens: 100,
			Temperature:     0.7,
			TopP:            0.9,
			Stream:          true,
		}
		body := buildChatCompletionsBody(req, "gpt-4-turbo")
		var parsed map[string]interface{}
		json.Unmarshal(body, &parsed)
		if parsed["model"] != "gpt-4-turbo" {
			t.Errorf("expected model 'gpt-4-turbo', got %v", parsed["model"])
		}
		if parsed["stream"] != true {
			t.Error("expected stream=true")
		}
		if parsed["max_tokens"] != float64(100) {
			t.Errorf("expected max_tokens=100, got %v", parsed["max_tokens"])
		}
		if parsed["temperature"] != 0.7 {
			t.Errorf("expected temperature=0.7, got %v", parsed["temperature"])
		}
		if parsed["top_p"] != 0.9 {
			t.Errorf("expected top_p=0.9, got %v", parsed["top_p"])
		}
		msgs := parsed["messages"].([]interface{})
		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages (system+user), got %d", len(msgs))
		}
	})
}

func TestChatCompletionsToResponses(t *testing.T) {
	t.Run("basic conversion", func(t *testing.T) {
		chatBody := []byte(`{
			"id":"chatcmpl-123","object":"chat.completion","created":1728000000,"model":"gpt-4",
			"choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`)
		result := chatCompletionsToResponses(chatBody, "gpt-4", "resp_abc", "msg_xyz")
		var resp responsesResponse
		if err := json.Unmarshal(result, &resp); err != nil {
			t.Fatalf("invalid response JSON: %v", err)
		}
		if resp.ID != "resp_abc" {
			t.Errorf("expected id 'resp_abc', got %q", resp.ID)
		}
		if resp.Object != "response" {
			t.Errorf("expected object 'response', got %q", resp.Object)
		}
		if resp.Model != "gpt-4" {
			t.Errorf("expected model 'gpt-4', got %q", resp.Model)
		}
		if len(resp.Output) != 1 {
			t.Fatalf("expected 1 output item, got %d", len(resp.Output))
		}
		item := resp.Output[0]
		if item.ID != "msg_xyz" {
			t.Errorf("expected item id 'msg_xyz', got %q", item.ID)
		}
		if item.Type != "message" {
			t.Errorf("expected type 'message', got %q", item.Type)
		}
		if item.Status != "completed" {
			t.Errorf("expected status 'completed', got %q", item.Status)
		}
		if item.Role != "assistant" {
			t.Errorf("expected role 'assistant', got %q", item.Role)
		}
		if len(item.Content) != 1 {
			t.Fatalf("expected 1 content item, got %d", len(item.Content))
		}
		if item.Content[0].Type != "output_text" {
			t.Errorf("expected content type 'output_text', got %q", item.Content[0].Type)
		}
		if item.Content[0].Text != "Hello!" {
			t.Errorf("expected text 'Hello!', got %q", item.Content[0].Text)
		}
		if resp.Usage == nil {
			t.Fatal("expected usage")
		}
		if resp.Usage.InputTokens != 10 {
			t.Errorf("expected input_tokens=10, got %d", resp.Usage.InputTokens)
		}
		if resp.Usage.OutputTokens != 5 {
			t.Errorf("expected output_tokens=5, got %d", resp.Usage.OutputTokens)
		}
		if resp.Usage.TotalTokens != 15 {
			t.Errorf("expected total_tokens=15, got %d", resp.Usage.TotalTokens)
		}
	})

	t.Run("no choices returns empty output", func(t *testing.T) {
		chatBody := []byte(`{"id":"chatcmpl-123","object":"chat.completion","created":0,"model":"m","choices":[]}`)
		result := chatCompletionsToResponses(chatBody, "m", "resp_1", "msg_1")
		var resp responsesResponse
		json.Unmarshal(result, &resp)
		if len(resp.Output) != 0 {
			t.Errorf("expected empty output, got %d items", len(resp.Output))
		}
	})

	t.Run("unparseable body returns original", func(t *testing.T) {
		original := []byte("not json")
		result := chatCompletionsToResponses(original, "m", "r", "msg")
		if string(result) != string(original) {
			t.Errorf("expected original body returned")
		}
	})
}

func TestResponsesHandler_MissingModel(t *testing.T) {
	store := setupMockStore()
	router := setupRouter(store, nil)
	handler := NewResponsesHandler(nil, router, newTestLogger(), store)

	body := `{"input":"hello"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp responsesErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Message != "model is required" {
		t.Errorf("expected 'model is required', got %q", resp.Error.Message)
	}
}

func TestResponsesHandler_MissingInput(t *testing.T) {
	store := setupMockStore()
	router := setupRouter(store, nil)
	handler := NewResponsesHandler(nil, router, newTestLogger(), store)

	body := `{"model":"gpt-4"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp responsesErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error.Message != "input is required" {
		t.Errorf("expected 'input is required', got %q", resp.Error.Message)
	}
}

func TestResponsesHandler_UnknownModel(t *testing.T) {
	store := setupMockStore()
	store.providers = []models.Provider{
		{ID: 1, Name: "openai", BaseURL: "http://localhost", Models: []models.ModelConfig{
			{Name: "gpt-4", DisplayName: "gpt-4"},
		}},
	}
	router := proxy.NewRouter(store)
	router.Refresh()
	handler := NewResponsesHandler(nil, router, newTestLogger(), store)

	body := `{"model":"unknown-model","input":"hello"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResponsesHandler_UpstreamSuccess(t *testing.T) {
	upstream := startTestUpstream(`{
		"id":"chatcmpl-123","object":"chat.completion","created":1728000000,"model":"gpt-4",
		"choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`, 200)
	defer upstream.Close()

	store := setupMockStore()
	store.providers = []models.Provider{
		{
			ID: 1, Name: "openai", BaseURL: upstream.URL, ProviderType: "openai",
			Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "gpt-4"},
			},
		},
	}
	router := proxy.NewRouter(store)
	router.Refresh()

	handler := NewResponsesHandler(proxy.NewForwarder(newTestLogger()), router, newTestLogger(), store)

	body := `{"model":"gpt-4","input":"hello"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp responsesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if resp.Object != "response" {
		t.Errorf("expected object 'response', got %q", resp.Object)
	}
	if !strings.HasPrefix(resp.ID, "resp_") {
		t.Errorf("expected id to start with 'resp_', got %q", resp.ID)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("expected 1 output, got %d", len(resp.Output))
	}
	if resp.Output[0].Content[0].Text != "Hello!" {
		t.Errorf("expected text 'Hello!', got %q", resp.Output[0].Content[0].Text)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 10 {
		t.Errorf("expected usage with 10 input tokens, got %+v", resp.Usage)
	}
}

func TestResponsesHandler_ResolvesDisplayName(t *testing.T) {
	upstream := startTestUpstream(`{
		"id":"chatcmpl-123","object":"chat.completion","created":1728000000,"model":"gpt-4-turbo-2024-04-09",
		"choices":[{"index":0,"message":{"role":"assistant","content":"Hi!"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
	}`, 200)
	defer upstream.Close()

	store := setupMockStore()
	store.providers = []models.Provider{
		{
			ID: 1, Name: "openai", BaseURL: upstream.URL, ProviderType: "openai",
			Models: []models.ModelConfig{
				{Name: "gpt-4-turbo-2024-04-09", DisplayName: "GPT-4 Turbo"},
			},
		},
	}
	router := proxy.NewRouter(store)
	router.Refresh()

	handler := NewResponsesHandler(proxy.NewForwarder(newTestLogger()), router, newTestLogger(), store)

	body := `{"model":"GPT-4 Turbo","input":"hello"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResponsesHandler_WithInstructions(t *testing.T) {
	upstream := startTestUpstream(`{
		"id":"chatcmpl-123","object":"chat.completion","created":1728000000,"model":"gpt-4",
		"choices":[{"index":0,"message":{"role":"assistant","content":"Be concise: OK!"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`, 200)
	defer upstream.Close()

	store := setupMockStore()
	store.providers = []models.Provider{
		{
			ID: 1, Name: "openai", BaseURL: upstream.URL, ProviderType: "openai",
			Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "gpt-4"},
			},
		},
	}
	router := proxy.NewRouter(store)
	router.Refresh()

	handler := NewResponsesHandler(proxy.NewForwarder(newTestLogger()), router, newTestLogger(), store)

	body := `{"model":"gpt-4","input":"hello","instructions":"Be concise"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestResponsesHandler_StreamingUpstream(t *testing.T) {
	upstreamBody := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1728000000,"model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1728000000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1728000000,"model":"gpt-4","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1728000000,"model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1728000000,"model":"gpt-4","usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}

data: [DONE]
`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(upstreamBody))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer upstream.Close()

	store := setupMockStore()
	store.providers = []models.Provider{
		{
			ID: 1, Name: "openai", BaseURL: upstream.URL, ProviderType: "openai",
			Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "gpt-4"},
			},
		},
	}
	router := proxy.NewRouter(store)
	router.Refresh()

	handler := NewResponsesHandler(proxy.NewForwarder(newTestLogger()), router, newTestLogger(), store)

	body := `{"model":"gpt-4","input":"hello","stream":true}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	output := w.Body.String()
	// Check for expected SSE events
	if !strings.Contains(output, "event: response.created") {
		t.Error("expected response.created event")
	}
	if !strings.Contains(output, "event: response.output_text.delta") {
		t.Error("expected response.output_text.delta events")
	}
	if !strings.Contains(output, "event: response.output_text.done") {
		t.Error("expected response.output_text.done event")
	}
	if !strings.Contains(output, "event: response.completed") {
		t.Error("expected response.completed event")
	}
	if !strings.Contains(output, `"text":"Hello world"`) {
		t.Errorf("expected full text 'Hello world', got: %s", output)
	}
}

func TestResponsesHandler_UpstreamError(t *testing.T) {
	upstream := startTestUpstream(`{"error":"internal error"}`, 500)
	defer upstream.Close()

	store := setupMockStore()
	store.providers = []models.Provider{
		{
			ID: 1, Name: "openai", BaseURL: upstream.URL, ProviderType: "openai",
			Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "gpt-4"},
			},
		},
	}
	router := proxy.NewRouter(store)
	router.Refresh()

	handler := NewResponsesHandler(proxy.NewForwarder(newTestLogger()), router, newTestLogger(), store)

	body := `{"model":"gpt-4","input":"hello"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestResponsesHandler_InvalidRequestBody(t *testing.T) {
	store := setupMockStore()
	router := setupRouter(store, nil)
	handler := NewResponsesHandler(nil, router, newTestLogger(), store)

	body := `not json`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestExtractResponsesSummary(t *testing.T) {
	t.Run("string input", func(t *testing.T) {
		input := json.RawMessage(`"hello world"`)
		req := responsesRequestBody{Input: input}
		summary := extractResponsesSummary(req)
		if summary != "hello world" {
			t.Errorf("expected 'hello world', got %q", summary)
		}
	})

	t.Run("array input with user", func(t *testing.T) {
		input := json.RawMessage(`[{"role":"system","content":"be helpful"},{"role":"user","content":"what time is it"}]`)
		req := responsesRequestBody{Input: input}
		summary := extractResponsesSummary(req)
		if summary != "what time is it" {
			t.Errorf("expected 'what time is it', got %q", summary)
		}
	})

	t.Run("empty input fallback to raw bytes", func(t *testing.T) {
		req := responsesRequestBody{Input: json.RawMessage(`"hello"`)}
		summary := extractResponsesSummary(req)
		if summary != "hello" {
			t.Errorf("expected 'hello', got %q", summary)
		}
	})
}

func TestResponsesHandler_GeminiProvider(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"candidates":[{"content":{"parts":[{"text":"Hi from Gemini!"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
		}`))
	}))
	defer upstream.Close()

	store := setupMockStore()
	store.providers = []models.Provider{
		{
			ID: 1, Name: "gemini-provider", BaseURL: upstream.URL, ProviderType: "gemini", APIKey: "test-key",
			Models: []models.ModelConfig{
				{Name: "gemini-pro", DisplayName: "gemini-pro"},
			},
		},
	}
	router := proxy.NewRouter(store)
	router.Refresh()

	logger := newTestLogger()
	handler := NewResponsesHandler(proxy.NewForwarder(logger), router, logger, store)
	geminiHandler := NewGeminiHandler(proxy.NewForwarder(logger), router, logger)
	handler.SetGeminiHandler(geminiHandler)

	body := `{"model":"gemini-pro","input":"hello"}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp responsesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid response JSON: %v", err)
	}
	if resp.Object != "response" {
		t.Errorf("expected object 'response', got %q", resp.Object)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("expected 1 output, got %d", len(resp.Output))
	}
	text := resp.Output[0].Content[0].Text
	if !strings.Contains(text, "Hi from Gemini!") {
		t.Errorf("expected text containing 'Hi from Gemini!', got %q", text)
	}
}

func TestResponsesHandler_GeminiProvider_Streaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello \"}]},\"finishReason\":null}]}\n\n"))
		w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"world\"}]},\"finishReason\":null}]}\n\n"))
		w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"!\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":3,\"totalTokenCount\":6}}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer upstream.Close()

	store := setupMockStore()
	store.providers = []models.Provider{
		{
			ID: 1, Name: "gemini-provider", BaseURL: upstream.URL, ProviderType: "gemini", APIKey: "test-key",
			Models: []models.ModelConfig{
				{Name: "gemini-pro", DisplayName: "gemini-pro"},
			},
		},
	}
	router := proxy.NewRouter(store)
	router.Refresh()

	logger := newTestLogger()
	handler := NewResponsesHandler(proxy.NewForwarder(logger), router, logger, store)
	geminiHandler := NewGeminiHandler(proxy.NewForwarder(logger), router, logger)
	handler.SetGeminiHandler(geminiHandler)

	body := `{"model":"gemini-pro","input":"hello","stream":true}`
	c, w := newTestContext("POST", "/v1/responses", strings.NewReader(body))
	handler.Responses(c)

	output := w.Body.String()
	if !strings.Contains(output, "event: response.created") {
		t.Error("expected response.created event for gemini stream")
	}
	if !strings.Contains(output, "event: response.output_text.delta") {
		t.Error("expected delta events for gemini stream")
	}
	if !strings.Contains(output, `"text":"Hello world!"`) {
		t.Errorf("expected full text 'Hello world!', got: %s", output)
	}
}
