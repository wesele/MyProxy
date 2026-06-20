package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/middleware"
	"github.com/user/qwenportal/internal/models"
	"github.com/user/qwenportal/internal/proxy"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------------------
// mockStore implements db.Store with configurable return values
// ---------------------------------------------------------------------------

type mockStore struct {
	providers    []models.Provider
	providersErr error

	getProvider    *models.Provider
	getProviderErr error

	createProviderID  int64
	createProviderErr error

	updateProviderErr error
	deleteProviderErr error

	findByName    *models.Provider
	findByNameErr error

	getProviderByModel    *models.Provider
	getProviderByModelErr error

	apiKeys    []models.ApiKey
	apiKeysErr error

	getApiKeyByName    *models.ApiKey
	getApiKeyByNameErr error

	createApiKey    *models.ApiKey
	createApiKeyErr error

	updateApiKeyErr error
	deleteApiKeyErr error

	verifyApiKey    *models.ApiKey
	verifyApiKeyErr error

	insertLogErr error

	stats    *models.StatsResponse
	statsErr error

	modelLogs    []models.RequestLog
	modelLogsErr error

	trainingID      int64
	trainingErr     error
	stopTrainingErr error
	trainingStats    *db.TrainingStats
	trainingStatsErr error
	activeTrainingID int64
	activeTrainingErr error
}

func (m *mockStore) ListProviders() ([]models.Provider, error) {
	if m.providersErr != nil {
		return nil, m.providersErr
	}
	return m.providers, nil
}

func (m *mockStore) GetProvider(id int64) (*models.Provider, error) {
	if m.getProviderErr != nil {
		return nil, m.getProviderErr
	}
	return m.getProvider, nil
}

func (m *mockStore) CreateProvider(p *models.Provider) (int64, error) {
	if m.createProviderErr != nil {
		return 0, m.createProviderErr
	}
	return m.createProviderID, nil
}

func (m *mockStore) UpdateProvider(p *models.Provider) error {
	return m.updateProviderErr
}

func (m *mockStore) DeleteProvider(id int64) error {
	return m.deleteProviderErr
}

func (m *mockStore) FindProviderByName(name string) (*models.Provider, error) {
	if m.findByNameErr != nil {
		return nil, m.findByNameErr
	}
	return m.findByName, nil
}

func (m *mockStore) GetProviderByModel(model string) (*models.Provider, error) {
	if m.getProviderByModelErr != nil {
		return nil, m.getProviderByModelErr
	}
	return m.getProviderByModel, nil
}

func (m *mockStore) ListApiKeys() ([]models.ApiKey, error) {
	if m.apiKeysErr != nil {
		return nil, m.apiKeysErr
	}
	return m.apiKeys, nil
}

func (m *mockStore) GetApiKeyByName(name string) (*models.ApiKey, error) {
	if m.getApiKeyByNameErr != nil {
		return nil, m.getApiKeyByNameErr
	}
	return m.getApiKeyByName, nil
}

func (m *mockStore) CreateApiKey(name string, rateLimitRPM int) (*models.ApiKey, error) {
	if m.createApiKeyErr != nil {
		return nil, m.createApiKeyErr
	}
	return m.createApiKey, nil
}

func (m *mockStore) UpdateApiKeyValue(id int64, keyValue string) error { return nil }
func (m *mockStore) UpdateApiKey(id int64, name string, isActive bool, rateLimitRPM int) error {
	return m.updateApiKeyErr
}

func (m *mockStore) DeleteApiKey(id int64) error {
	return m.deleteApiKeyErr
}

func (m *mockStore) VerifyApiKey(keyValue string) (*models.ApiKey, error) {
	if m.verifyApiKeyErr != nil {
		return nil, m.verifyApiKeyErr
	}
	return m.verifyApiKey, nil
}

func (m *mockStore) InsertRequestLog(log *models.RequestLog) error {
	return m.insertLogErr
}

func (m *mockStore) GetStats(start, end time.Time, modelFilter string) (*models.StatsResponse, error) {
	if m.statsErr != nil {
		return nil, m.statsErr
	}
	return m.stats, nil
}

func (m *mockStore) GetModelLogs(model string, start, end time.Time, limit int) ([]models.RequestLog, error) {
	if m.modelLogsErr != nil {
		return nil, m.modelLogsErr
	}
	return m.modelLogs, nil
}

func (m *mockStore) StartTraining(tool string) (int64, error) {
	if m.trainingErr != nil {
		return 0, m.trainingErr
	}
	return m.trainingID, nil
}

func (m *mockStore) StopTraining(id int64) error {
	return m.stopTrainingErr
}

func (m *mockStore) GetTrainingStats(tool string, days int) (*db.TrainingStats, error) {
	if m.trainingStatsErr != nil {
		return nil, m.trainingStatsErr
	}
	return m.trainingStats, nil
}

func (m *mockStore) GetActiveTraining(tool string) (int64, error) {
	if m.activeTrainingErr != nil {
		return 0, m.activeTrainingErr
	}
	return m.activeTrainingID, nil
}

func (m *mockStore) Close() {}

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

func newTestLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

func newTestContext(method, path string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, body)
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func setupMockStore() *mockStore {
	return &mockStore{}
}

func setupRouter(store db.Store, providers []models.Provider) *proxy.Router {
	r := proxy.NewRouter(store)
	if len(providers) > 0 {
		// Set the router's providers directly for testing
		r.Refresh()
	}
	return r
}

// startTestUpstream starts a test HTTP server that returns a canned chat completion.
func startTestUpstream(body string, statusCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write([]byte(body))
	}))
}

// ---------------------------------------------------------------------------
// OpenAIHandler tests
// ---------------------------------------------------------------------------

func TestOpenAIHandler_ListModels(t *testing.T) {
	t.Run("returns models list when providers exist", func(t *testing.T) {
		store := setupMockStore()
		store.providers = []models.Provider{
			{
				ID:   1,
				Name: "openai",
				Models: []models.ModelConfig{
					{Name: "gpt-4", DisplayName: "gpt-4"},
					{Name: "gpt-3.5-turbo", DisplayName: "GPT-3.5 Turbo", MaxTokens: 4096},
				},
			},
		}
		router := setupRouter(store, nil)
		handler := NewOpenAIHandler(nil, router, newTestLogger(), store)

		c, w := newTestContext("GET", "/v1/models", nil)
		handler.ListModels(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp struct {
			Object string `json:"object"`
			Data   []struct {
				ID          string  `json:"id"`
				DisplayName string  `json:"display_name,omitempty"`
				MaxTokens   int     `json:"max_tokens,omitempty"`
			} `json:"data"`
		}
		json.NewDecoder(w.Body).Decode(&resp)

		if resp.Object != "list" {
			t.Errorf("expected object=list, got %s", resp.Object)
		}
		if len(resp.Data) != 2 {
			t.Fatalf("expected 2 models, got %d", len(resp.Data))
		}
		if resp.Data[0].ID != "openai.gpt-4" {
			t.Errorf("expected first model openai.gpt-4, got %s", resp.Data[0].ID)
		}
		if resp.Data[1].DisplayName != "GPT-3.5 Turbo" {
			t.Errorf("expected display_name, got %q", resp.Data[1].DisplayName)
		}
		if resp.Data[1].MaxTokens != 4096 {
			t.Errorf("expected max_tokens=4096, got %d", resp.Data[1].MaxTokens)
		}
	})

	t.Run("returns empty when no providers", func(t *testing.T) {
		store := setupMockStore()
		store.providers = []models.Provider{}
		router := setupRouter(store, nil)
		handler := NewOpenAIHandler(nil, router, newTestLogger(), store)

		c, w := newTestContext("GET", "/v1/models", nil)
		handler.ListModels(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["data"] != nil {
			t.Errorf("expected null data for empty models, got %v", resp["data"])
		}
	})

	t.Run("handles store error", func(t *testing.T) {
		store := setupMockStore()
		store.providersErr = fmt.Errorf("db error")
		router := setupRouter(store, nil)
		handler := NewOpenAIHandler(nil, router, newTestLogger(), store)

		c, w := newTestContext("GET", "/v1/models", nil)
		handler.ListModels(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})
}

func TestOpenAIHandler_ChatCompletions(t *testing.T) {
	t.Run("model is required", func(t *testing.T) {
		store := setupMockStore()
		router := setupRouter(store, nil)
		handler := NewOpenAIHandler(nil, router, newTestLogger(), store)

		body := `{"messages":[{"role":"user","content":"hi"}]}`
		c, w := newTestContext("POST", "/v1/chat/completions", strings.NewReader(body))
		handler.ChatCompletions(c)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["error"] != "model is required" {
			t.Errorf("expected 'model is required', got %v", resp["error"])
		}
	})

	t.Run("returns 404 for unknown model", func(t *testing.T) {
		store := setupMockStore()
		store.providers = []models.Provider{
			{ID: 1, Name: "openai", BaseURL: "http://localhost", Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "gpt-4"},
			}},
		}
		router := proxy.NewRouter(store)
		router.Refresh()
		handler := NewOpenAIHandler(nil, router, newTestLogger(), store)

		body := `{"model":"unknown-model","messages":[{"role":"user","content":"hi"}]}`
		c, w := newTestContext("POST", "/v1/chat/completions", strings.NewReader(body))
		// Set up a log_entry to verify model is set even on 404
		entry := &middleware.LogEntry{RequestID: "test-123"}
		c.Set("log_entry", entry)
		handler.ChatCompletions(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
		if entry.Model != "unknown-model" {
			t.Errorf("expected log_entry.Model='unknown-model', got %q", entry.Model)
		}
	})

	t.Run("resolves display name to upstream name", func(t *testing.T) {
		upstream := startTestUpstream(`{"id":"test","choices":[{"message":{"content":"hello"}}]}`, 200)
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

		handler := NewOpenAIHandler(proxy.NewForwarder(newTestLogger()), router, newTestLogger(), store)

		body := `{"model":"GPT-4 Turbo","messages":[{"role":"user","content":"hi"}]}`
		c, w := newTestContext("POST", "/v1/chat/completions", strings.NewReader(body))
		handler.ChatCompletions(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestOpenAIHandler_Embeddings(t *testing.T) {
	t.Run("model is required", func(t *testing.T) {
		store := setupMockStore()
		router := setupRouter(store, nil)
		handler := NewOpenAIHandler(nil, router, newTestLogger(), store)

		body := `{}`
		c, w := newTestContext("POST", "/v1/embeddings", strings.NewReader(body))
		handler.Embeddings(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})

	t.Run("returns 404 for unknown model", func(t *testing.T) {
		store := setupMockStore()
		store.providers = []models.Provider{
			{ID: 1, Name: "openai", Models: []models.ModelConfig{
				{Name: "text-embedding-3-small", DisplayName: "text-embedding-3-small"},
			}},
		}
		router := proxy.NewRouter(store)
		router.Refresh()
		handler := NewOpenAIHandler(nil, router, newTestLogger(), store)

		body := `{"model":"unknown-embed","input":"test"}`
		c, w := newTestContext("POST", "/v1/embeddings", strings.NewReader(body))
		handler.Embeddings(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

// ---------------------------------------------------------------------------
// ClaudeHandler tests
// ---------------------------------------------------------------------------

func TestClaudeHandler_Messages(t *testing.T) {
	t.Run("handles missing model", func(t *testing.T) {
		store := setupMockStore()
		store.providers = []models.Provider{
			{ID: 1, Name: "anthropic", Models: []models.ModelConfig{
				{Name: "claude-3", DisplayName: "claude-3"},
			}},
		}
		router := proxy.NewRouter(store)
		router.Refresh()
		handler := NewClaudeHandler(nil, router, newTestLogger())

		body := `{"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
		c, w := newTestContext("POST", "/v1/messages", strings.NewReader(body))
		handler.Messages(c)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for missing model, got %d", w.Code)
		}
	})

	t.Run("resolves display name", func(t *testing.T) {
		upstream := startTestUpstream(`{"id":"test","content":[{"text":"hello"}]}`, 200)
		defer upstream.Close()

		store := setupMockStore()
		store.providers = []models.Provider{
			{
				ID: 1, Name: "anthropic", BaseURL: upstream.URL, ProviderType: "anthropic",
				Models: []models.ModelConfig{
					{Name: "claude-3-5-sonnet-20240620", DisplayName: "Claude 3.5 Sonnet"},
				},
			},
		}
		router := proxy.NewRouter(store)
		router.Refresh()

		handler := NewClaudeHandler(proxy.NewForwarder(newTestLogger()), router, newTestLogger())

		body := `{"model":"Claude 3.5 Sonnet","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
		c, w := newTestContext("POST", "/v1/messages", strings.NewReader(body))
		handler.Messages(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// AdminHandler tests
// ---------------------------------------------------------------------------

func TestAdminHandler_ListProviders(t *testing.T) {
	t.Run("returns providers with masked API keys", func(t *testing.T) {
		store := setupMockStore()
		store.providers = []models.Provider{
			{ID: 1, Name: "openai", APIKey: "sk-abc1234567890xyz"},
			{ID: 2, Name: "empty-key", APIKey: ""},
		}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/providers", nil)
		handler.ListProviders(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var providers []models.Provider
		json.NewDecoder(w.Body).Decode(&providers)

		if len(providers) != 2 {
			t.Fatalf("expected 2 providers, got %d", len(providers))
		}
		if providers[0].APIKey != "sk-a****0xyz" {
			t.Errorf("expected masked key 'sk-a****0xyz', got %q", providers[0].APIKey)
		}
		if providers[1].APIKey != "" {
			t.Errorf("expected empty key to stay empty, got %q", providers[1].APIKey)
		}
	})

	t.Run("handles store error", func(t *testing.T) {
		store := setupMockStore()
		store.providersErr = fmt.Errorf("db error")
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/providers", nil)
		handler.ListProviders(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})
}

func TestAdminHandler_CreateProvider(t *testing.T) {
	t.Run("creates and returns masked key", func(t *testing.T) {
		store := setupMockStore()
		store.createProviderID = 1
		store.getProvider = &models.Provider{
			ID: 1, Name: "new-provider", APIKey: "sk-secret12345678xx",
			Models: []models.ModelConfig{},
		}
		router := proxy.NewRouter(store)
		handler := NewAdminHandler(newTestLogger(), router, store, nil)

		body := `{"name":"new-provider","api_key":"sk-secret12345678xx","provider_type":"openai"}`
		c, w := newTestContext("POST", "/api/providers", strings.NewReader(body))
		handler.CreateProvider(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp models.Provider
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.APIKey != "sk-s****78xx" {
			t.Errorf("expected masked key, got %q", resp.APIKey)
		}
	})

	t.Run("handles create error", func(t *testing.T) {
		store := setupMockStore()
		store.createProviderErr = fmt.Errorf("create failed")
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		body := `{"name":"bad"}`
		c, w := newTestContext("POST", "/api/providers", strings.NewReader(body))
		handler.CreateProvider(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})
}

func TestAdminHandler_GetProvider(t *testing.T) {
	t.Run("handles missing id param", func(t *testing.T) {
		handler := NewAdminHandler(newTestLogger(), nil, nil, nil)

		// Set up a route with a param
		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/providers/abc", nil)
		// gin.CreateTestContext doesn't parse params by default; set it manually
		c.Params = gin.Params{{Key: "id", Value: "abc"}}

		handler.GetProvider(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("returns masked key by default", func(t *testing.T) {
		store := setupMockStore()
		store.getProvider = &models.Provider{
			ID: 1, Name: "test", APIKey: "sk-abcdefghijklmnop",
		}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/providers/1", nil)
		c.Params = gin.Params{{Key: "id", Value: "1"}}

		handler.GetProvider(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp models.Provider
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.APIKey != "sk-a****mnop" {
			t.Errorf("expected masked key, got %q", resp.APIKey)
		}
	})

	t.Run("returns full key with show_key=1", func(t *testing.T) {
		store := setupMockStore()
		store.getProvider = &models.Provider{
			ID: 1, Name: "test", APIKey: "sk-abcdefghijklmnop",
		}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/api/providers/1?show_key=1", nil)
		c.Params = gin.Params{{Key: "id", Value: "1"}}

		handler.GetProvider(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp models.Provider
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.APIKey != "sk-abcdefghijklmnop" {
			t.Errorf("expected full key, got %q", resp.APIKey)
		}
	})
}

func TestAdminHandler_UpdateProvider(t *testing.T) {
	t.Run("preserves masked key", func(t *testing.T) {
		existing := &models.Provider{
			ID: 1, Name: "test", APIKey: "sk-real-key-12345",
			Models: []models.ModelConfig{},
		}
		store := setupMockStore()
		store.getProvider = existing
		router := proxy.NewRouter(store)
		handler := NewAdminHandler(newTestLogger(), router, store, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/api/providers/1", strings.NewReader(
			`{"name":"updated","api_key":"sk-r****2345"}`,
		))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: "1"}}

		handler.UpdateProvider(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("handles invalid id", func(t *testing.T) {
		handler := NewAdminHandler(newTestLogger(), nil, nil, nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/api/providers/abc", nil)
		c.Params = gin.Params{{Key: "id", Value: "abc"}}

		handler.UpdateProvider(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestAdminHandler_DeleteProvider(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := setupMockStore()
		router := proxy.NewRouter(store)
		handler := NewAdminHandler(newTestLogger(), router, store, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("DELETE", "/api/providers/1", nil)
		c.Params = gin.Params{{Key: "id", Value: "1"}}

		handler.DeleteProvider(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("handles invalid id", func(t *testing.T) {
		handler := NewAdminHandler(newTestLogger(), nil, nil, nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("DELETE", "/api/providers/abc", nil)
		c.Params = gin.Params{{Key: "id", Value: "abc"}}

		handler.DeleteProvider(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})
}

func TestAdminHandler_ListApiKeys(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := setupMockStore()
		store.apiKeys = []models.ApiKey{
			{ID: 1, Name: "key1", KeyPrefix: "qw_"},
			{ID: 2, Name: "key2", KeyPrefix: "qw_"},
		}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/keys", nil)
		handler.ListApiKeys(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var keys []models.ApiKey
		json.NewDecoder(w.Body).Decode(&keys)
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}
	})

	t.Run("empty list", func(t *testing.T) {
		store := setupMockStore()
		store.apiKeys = nil
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/keys", nil)
		handler.ListApiKeys(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var keys []models.ApiKey
		json.NewDecoder(w.Body).Decode(&keys)
		if len(keys) != 0 {
			t.Errorf("expected empty list, got %d", len(keys))
		}
	})

	t.Run("store error", func(t *testing.T) {
		store := setupMockStore()
		store.apiKeysErr = fmt.Errorf("db error")
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/keys", nil)
		handler.ListApiKeys(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})
}

func TestAdminHandler_CreateApiKey(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		store := setupMockStore()
		store.createApiKey = &models.ApiKey{
			ID: 1, Name: "mykey", KeyPrefix: "qw_", KeyValue: "qw_abc123",
		}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		body := `{"name":"mykey","rate_limit_rpm":100}`
		c, w := newTestContext("POST", "/api/keys", strings.NewReader(body))
		handler.CreateApiKey(c)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", w.Code)
		}
	})

	t.Run("handles errors", func(t *testing.T) {
		store := setupMockStore()
		store.createApiKeyErr = fmt.Errorf("create failed")
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		body := `{"name":"bad"}`
		c, w := newTestContext("POST", "/api/keys", strings.NewReader(body))
		handler.CreateApiKey(c)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", w.Code)
		}
	})
}

func TestAdminHandler_GetStats(t *testing.T) {
	t.Run("default 24h range", func(t *testing.T) {
		store := setupMockStore()
		store.stats = &models.StatsResponse{TotalRequests: 42}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/stats", nil)
		handler.GetStats(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var stats models.StatsResponse
		json.NewDecoder(w.Body).Decode(&stats)
		if stats.TotalRequests != 42 {
			t.Errorf("expected 42 total requests, got %d", stats.TotalRequests)
		}
	})

	t.Run("custom range", func(t *testing.T) {
		store := setupMockStore()
		store.stats = &models.StatsResponse{TotalRequests: 10}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		now := time.Now()
		start := now.Add(-48 * time.Hour).Format(time.RFC3339)
		end := now.Format(time.RFC3339)

		c, w := newTestContext("GET", fmt.Sprintf("/api/stats?start=%s&end=%s", start, end), nil)
		handler.GetStats(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("with model filter", func(t *testing.T) {
		store := setupMockStore()
		store.stats = &models.StatsResponse{}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/stats?model=gpt-4&hours=1", nil)
		handler.GetStats(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func TestAdminHandler_TrainingStart(t *testing.T) {
	t.Run("start training with default tool", func(t *testing.T) {
		store := setupMockStore()
		store.trainingID = 7
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		body := `{}`
		c, w := newTestContext("POST", "/api/training/start", strings.NewReader(body))
		handler.TrainingStart(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp trainingStartResponse
		json.NewDecoder(w.Body).Decode(&resp)
		if resp.ID != 7 {
			t.Errorf("expected id 7, got %d", resp.ID)
		}
		if resp.StartedAt == "" {
			t.Error("expected non-empty started_at")
		}
	})

	t.Run("start training with custom tool", func(t *testing.T) {
		store := setupMockStore()
		store.trainingID = 3
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		body := `{"tool":"running"}`
		c, w := newTestContext("POST", "/api/training/start", strings.NewReader(body))
		handler.TrainingStart(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func TestAdminHandler_TrainingStop(t *testing.T) {
	t.Run("training stop success", func(t *testing.T) {
		store := setupMockStore()
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		body := `{"id":1}`
		c, w := newTestContext("POST", "/api/training/stop", strings.NewReader(body))
		handler.TrainingStop(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func TestAdminHandler_TrainingStats(t *testing.T) {
	t.Run("get training stats", func(t *testing.T) {
		store := setupMockStore()
		store.trainingStats = &db.TrainingStats{
			Dates: []db.TrainingDate{
				{Date: "2025-01-01", Total: 3600},
			},
		}
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/training/stats?tool=pelvic_floor&days=30", nil)
		handler.TrainingStats(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func TestAdminHandler_TrainingActive(t *testing.T) {
	t.Run("training active", func(t *testing.T) {
		store := setupMockStore()
		store.activeTrainingID = 5
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/training/active", nil)
		handler.TrainingActive(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["active"] != true {
			t.Errorf("expected active=true, got %v", resp["active"])
		}
		if resp["id"] != float64(5) {
			t.Errorf("expected id=5, got %v", resp["id"])
		}
	})

	t.Run("no active training", func(t *testing.T) {
		store := setupMockStore()
		store.activeTrainingID = 0
		handler := NewAdminHandler(newTestLogger(), nil, store, nil)

		c, w := newTestContext("GET", "/api/training/active", nil)
		handler.TrainingActive(c)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if resp["active"] != false {
			t.Errorf("expected active=false, got %v", resp["active"])
		}
	})
}

// ---------------------------------------------------------------------------
// Helper function tests (package-level in admin.go)
// ---------------------------------------------------------------------------

func TestMaskAPIKey(t *testing.T) {
	t.Run("short key", func(t *testing.T) {
		result := maskAPIKey("short")
		if result != "****" {
			t.Errorf("expected '****', got %q", result)
		}
	})

	t.Run("exactly 8 chars", func(t *testing.T) {
		result := maskAPIKey("12345678")
		if result != "****" {
			t.Errorf("expected '****', got %q", result)
		}
	})

	t.Run("long key", func(t *testing.T) {
		result := maskAPIKey("sk-abc1234567890xyz")
		if result != "sk-a****0xyz" {
			t.Errorf("expected 'sk-a****0xyz', got %q", result)
		}
	})
}

func TestIsMaskedKey(t *testing.T) {
	t.Run("masked", func(t *testing.T) {
		if !isMaskedKey("sk-a****0xyz") {
			t.Error("expected true for masked key")
		}
	})

	t.Run("unmasked", func(t *testing.T) {
		if isMaskedKey("sk-real-key") {
			t.Error("expected false for unmasked key")
		}
	})
}

func TestParseTokensFromBody(t *testing.T) {
	t.Run("OpenAI format with usage", func(t *testing.T) {
		body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":50}}`)
		result := parseTokensFromBody(body, "openai")
		if result != 50 {
			t.Errorf("expected 50, got %d", result)
		}
	})

	t.Run("OpenAI format with reasoning tokens", func(t *testing.T) {
		body := []byte(`{"usage":{"completion_tokens":50,"completion_tokens_details":{"reasoning_tokens":20}}}`)
		result := parseTokensFromBody(body, "openai")
		if result != 70 {
			t.Errorf("expected 70, got %d", result)
		}
	})

	t.Run("OpenAI fallback to choices", func(t *testing.T) {
		body := []byte(`{"choices":[{"message":{"content":"hello world, this is a test"}}]}`)
		result := parseTokensFromBody(body, "openai")
		if result != 6 {
			t.Errorf("expected 6 (27/4), got %d", result)
		}
	})

	t.Run("Anthropic format", func(t *testing.T) {
		body := []byte(`{"usage":{"output_tokens":35}}`)
		result := parseTokensFromBody(body, "anthropic")
		if result != 35 {
			t.Errorf("expected 35, got %d", result)
		}
	})

	t.Run("Gemini format", func(t *testing.T) {
		body := []byte(`{"usageMetadata":{"candidatesTokenCount":40,"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":30}]}}`)
		result := parseTokensFromBody(body, "gemini")
		if result != 40 {
			t.Errorf("expected 40, got %d", result)
		}
	})

	t.Run("Gemini with details exceeding count", func(t *testing.T) {
		body := []byte(`{"usageMetadata":{"candidatesTokenCount":20,"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":25},{"modality":"THINK","tokenCount":5}]}}`)
		result := parseTokensFromBody(body, "gemini")
		if result != 30 {
			t.Errorf("expected 30 (details total), got %d", result)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		if result := parseTokensFromBody([]byte{}, "openai"); result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	t.Run("unparseable body", func(t *testing.T) {
		if result := parseTokensFromBody([]byte("not json"), "openai"); result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})
}

func TestExtractContentFromBody(t *testing.T) {
	t.Run("OpenAI format", func(t *testing.T) {
		body := []byte(`{"choices":[{"message":{"content":"Hello, world!"}}]}`)
		result := extractContentFromBody(body, "openai")
		if result != "Hello, world!" {
			t.Errorf("expected 'Hello, world!', got %q", result)
		}
	})

	t.Run("OpenAI empty choices", func(t *testing.T) {
		body := []byte(`{"choices":[]}`)
		result := extractContentFromBody(body, "openai")
		if result != "" {
			t.Errorf("expected empty, got %q", result)
		}
	})

	t.Run("Anthropic format", func(t *testing.T) {
		body := []byte(`{"content":[{"text":"Hi from Claude"}]}`)
		result := extractContentFromBody(body, "anthropic")
		if result != "Hi from Claude" {
			t.Errorf("expected 'Hi from Claude', got %q", result)
		}
	})

	t.Run("Gemini format", func(t *testing.T) {
		body := []byte(`{"candidates":[{"content":{"parts":[{"text":"Part1"},{"text":"Part2"}]}}]}`)
		result := extractContentFromBody(body, "gemini")
		if result != "Part1Part2" {
			t.Errorf("expected 'Part1Part2', got %q", result)
		}
	})

	t.Run("Gemini empty candidates", func(t *testing.T) {
		body := []byte(`{"candidates":[]}`)
		result := extractContentFromBody(body, "gemini")
		if result != "" {
			t.Errorf("expected empty, got %q", result)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		if result := extractContentFromBody([]byte{}, "openai"); result != "" {
			t.Errorf("expected empty, got %q", result)
		}
	})
}

func TestParseUpstreamModels(t *testing.T) {
	t.Run("OpenAI format data array", func(t *testing.T) {
		body := []byte(`{"data":[{"id":"gpt-4","object":"model"},{"id":"gpt-3.5-turbo","max_tokens":4096}]}`)
		result := parseUpstreamModels(body)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 models, got %d", len(result))
		}
		if result[0].Name != "gpt-4" {
			t.Errorf("expected 'gpt-4', got %q", result[0].Name)
		}
		if result[1].MaxTokens != 4096 {
			t.Errorf("expected max_tokens 4096, got %d", result[1].MaxTokens)
		}
	})

	t.Run("OpenAI format with pricing", func(t *testing.T) {
		body := []byte(`{"data":[{"id":"gpt-4","input_price":0.03,"output_price":0.06,"input_cache_price":0.015}]}`)
		result := parseUpstreamModels(body)
		if result == nil || len(result) != 1 {
			t.Fatal("expected 1 model")
		}
		if result[0].InputPrice != 0.03 {
			t.Errorf("expected input_price 0.03, got %f", result[0].InputPrice)
		}
		if result[0].OutputPrice != 0.06 {
			t.Errorf("expected output_price 0.06, got %f", result[0].OutputPrice)
		}
		if result[0].InputCachePrice != 0.015 {
			t.Errorf("expected input_cache_price 0.015, got %f", result[0].InputCachePrice)
		}
	})

	t.Run("OpenAI format with nested pricing struct", func(t *testing.T) {
		body := []byte(`{"data":[{"id":"model","pricing":{"prompt":0.01,"completion":0.03,"cache_input":0.005}}]}`)
		result := parseUpstreamModels(body)
		if result == nil || len(result) != 1 {
			t.Fatal("expected 1 model")
		}
		if result[0].InputPrice != 0.01 {
			t.Errorf("expected input_price 0.01, got %f", result[0].InputPrice)
		}
		if result[0].OutputPrice != 0.03 {
			t.Errorf("expected output_price 0.03, got %f", result[0].OutputPrice)
		}
	})

	t.Run("OpenAI format with context_window", func(t *testing.T) {
		body := []byte(`{"data":[{"id":"model","context_window":128000}]}`)
		result := parseUpstreamModels(body)
		if result == nil || len(result) != 1 {
			t.Fatal("expected 1 model")
		}
		if result[0].MaxInputTokens != 128000 {
			t.Errorf("expected MaxInputTokens 128000, got %d", result[0].MaxInputTokens)
		}
	})

	t.Run("Gemini format models array", func(t *testing.T) {
		body := []byte(`{"models":[{"name":"models/gemini-pro","version":"v1","input_price":0.01,"output_price":0.02}]}`)
		result := parseUpstreamModels(body)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 model, got %d", len(result))
		}
		if result[0].Name != "gemini-pro" {
			t.Errorf("expected 'gemini-pro' (prefix stripped), got %q", result[0].Name)
		}
	})

	t.Run("nil for unparseable", func(t *testing.T) {
		body := []byte(`not json at all`)
		result := parseUpstreamModels(body)
		if result != nil {
			t.Errorf("expected nil for unparseable, got %v", result)
		}
	})

	t.Run("nil for empty JSON", func(t *testing.T) {
		body := []byte(`{}`)
		result := parseUpstreamModels(body)
		if result != nil {
			t.Errorf("expected nil for empty object, got %v", result)
		}
	})
}

// ---------------------------------------------------------------------------
// Gemini protocol translation tests (package-level funcs in gemini.go)
// ---------------------------------------------------------------------------

func TestTranslateOpenAIToGemini(t *testing.T) {
	t.Run("basic conversion", func(t *testing.T) {
		body := []byte(`{"model":"gemini-pro","messages":[{"role":"user","content":"Hello"}],"stream":true}`)
		result, model, err := translateOpenAIToGemini(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model != "gemini-pro" {
			t.Errorf("expected model 'gemini-pro', got %q", model)
		}

		var gReq geminiRequest
		json.Unmarshal(result, &gReq)
		if len(gReq.Contents) != 1 {
			t.Fatalf("expected 1 content, got %d", len(gReq.Contents))
		}
		if gReq.Contents[0].Role != "user" {
			t.Errorf("expected role 'user', got %q", gReq.Contents[0].Role)
		}
		if gReq.Contents[0].Parts[0].Text != "Hello" {
			t.Errorf("expected text 'Hello', got %q", gReq.Contents[0].Parts[0].Text)
		}
	})

	t.Run("system prompt handling", func(t *testing.T) {
		body := []byte(`{"model":"gemini-pro","messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"Hi"}]}`)
		result, _, err := translateOpenAIToGemini(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var gReq geminiRequest
		json.Unmarshal(result, &gReq)
		if gReq.SystemInstruction == nil {
			t.Fatal("expected systemInstruction to be set")
		}
		if gReq.SystemInstruction.Parts[0].Text != "You are helpful" {
			t.Errorf("expected system text, got %q", gReq.SystemInstruction.Parts[0].Text)
		}
		if len(gReq.Contents) != 1 {
			t.Fatalf("expected 1 content (system excluded), got %d", len(gReq.Contents))
		}
	})

	t.Run("assistant role becomes model", func(t *testing.T) {
		body := []byte(`{"model":"gemini-pro","messages":[{"role":"assistant","content":"I am a bot"}]}`)
		result, _, err := translateOpenAIToGemini(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var gReq geminiRequest
		json.Unmarshal(result, &gReq)
		if len(gReq.Contents) != 1 {
			t.Fatalf("expected 1 content, got %d", len(gReq.Contents))
		}
		if gReq.Contents[0].Role != "model" {
			t.Errorf("expected role 'model', got %q", gReq.Contents[0].Role)
		}
	})

	t.Run("generation config", func(t *testing.T) {
		body := []byte(`{"model":"gemini-pro","messages":[{"role":"user","content":"Hi"}],"max_tokens":100,"temperature":0.7,"top_p":0.9,"top_k":40}`)
		result, _, err := translateOpenAIToGemini(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var gReq geminiRequest
		json.Unmarshal(result, &gReq)
		if gReq.GenerationConfig == nil {
			t.Fatal("expected generationConfig to be set")
		}
		if gReq.GenerationConfig.MaxOutputTokens != 100 {
			t.Errorf("expected maxOutputTokens 100, got %d", gReq.GenerationConfig.MaxOutputTokens)
		}
		if gReq.GenerationConfig.Temperature != 0.7 {
			t.Errorf("expected temperature 0.7, got %f", gReq.GenerationConfig.Temperature)
		}
		if gReq.GenerationConfig.TopP != 0.9 {
			t.Errorf("expected topP 0.9, got %f", gReq.GenerationConfig.TopP)
		}
		if gReq.GenerationConfig.TopK != 40 {
			t.Errorf("expected topK 40, got %d", gReq.GenerationConfig.TopK)
		}
	})

	t.Run("no generation config when all zero", func(t *testing.T) {
		body := []byte(`{"model":"gemini-pro","messages":[{"role":"user","content":"Hi"}]}`)
		result, _, err := translateOpenAIToGemini(body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var gReq geminiRequest
		json.Unmarshal(result, &gReq)
		if gReq.GenerationConfig != nil {
			t.Error("expected generationConfig to be nil")
		}
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		_, _, err := translateOpenAIToGemini([]byte("not json"))
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestTranslateGeminiToOpenAI(t *testing.T) {
	t.Run("basic conversion", func(t *testing.T) {
		geminiResp := `{"candidates":[{"content":{"parts":[{"text":"Hello, human!"}]},"finishReason":"STOP"}]}`
		result := translateGeminiToOpenAI([]byte(geminiResp), "gemini-pro")
		if result == nil {
			t.Fatal("expected non-nil result")
		}

		var oaiResp map[string]interface{}
		json.Unmarshal(result, &oaiResp)

		if oaiResp["object"] != "chat.completion" {
			t.Errorf("expected object 'chat.completion', got %v", oaiResp["object"])
		}
		if oaiResp["model"] != "gemini-pro" {
			t.Errorf("expected model 'gemini-pro', got %v", oaiResp["model"])
		}

		choices := oaiResp["choices"].([]interface{})
		if len(choices) != 1 {
			t.Fatalf("expected 1 choice, got %d", len(choices))
		}
		choice := choices[0].(map[string]interface{})
		msg := choice["message"].(map[string]interface{})
		if msg["content"] != "Hello, human!" {
			t.Errorf("expected 'Hello, human!', got %v", msg["content"])
		}
		if choice["finish_reason"] != "stop" {
			t.Errorf("expected finish_reason 'stop', got %v", choice["finish_reason"])
		}
	})

	t.Run("finish reason MAX_TOKENS becomes length", func(t *testing.T) {
		geminiResp := `{"candidates":[{"content":{"parts":[{"text":"cut off"}]},"finishReason":"MAX_TOKENS"}]}`
		result := translateGeminiToOpenAI([]byte(geminiResp), "m")
		var oaiResp map[string]interface{}
		json.Unmarshal(result, &oaiResp)
		choices := oaiResp["choices"].([]interface{})
		if choices[0].(map[string]interface{})["finish_reason"] != "length" {
			t.Errorf("expected 'length', got %v", choices[0].(map[string]interface{})["finish_reason"])
		}
	})

	t.Run("finish reason SAFETY becomes content_filter", func(t *testing.T) {
		geminiResp := `{"candidates":[{"content":{"parts":[{"text":"blocked"}]},"finishReason":"SAFETY"}]}`
		result := translateGeminiToOpenAI([]byte(geminiResp), "m")
		var oaiResp map[string]interface{}
		json.Unmarshal(result, &oaiResp)
		choices := oaiResp["choices"].([]interface{})
		if choices[0].(map[string]interface{})["finish_reason"] != "content_filter" {
			t.Errorf("expected 'content_filter', got %v", choices[0].(map[string]interface{})["finish_reason"])
		}
	})

	t.Run("usage metadata translation", func(t *testing.T) {
		geminiResp := `{
			"candidates":[{"content":{"parts":[{"text":"Ok"}]},"finishReason":"STOP"}],
			"usageMetadata":{
				"promptTokenCount":10,
				"candidatesTokenCount":5,
				"totalTokenCount":15,
				"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":3},{"modality":"THINK","tokenCount":2}]
			}
		}`
		result := translateGeminiToOpenAI([]byte(geminiResp), "m")
		var oaiResp map[string]interface{}
		json.Unmarshal(result, &oaiResp)

		usage, ok := oaiResp["usage"].(map[string]interface{})
		if !ok {
			t.Fatal("expected usage in response")
		}
		if usage["prompt_tokens"].(float64) != 10 {
			t.Errorf("expected prompt_tokens 10, got %v", usage["prompt_tokens"])
		}
		if usage["completion_tokens"].(float64) != 5 {
			t.Errorf("expected completion_tokens 5, got %v", usage["completion_tokens"])
		}
		details, ok := usage["completion_tokens_details"].(map[string]interface{})
		if !ok {
			t.Fatal("expected completion_tokens_details")
		}
		if details["reasoning_tokens"].(float64) != 2 {
			t.Errorf("expected reasoning_tokens 2, got %v", details["reasoning_tokens"])
		}
	})

	t.Run("usage with zero totalTokenCount", func(t *testing.T) {
		geminiResp := `{
			"candidates":[{"content":{"parts":[{"text":"Ok"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}
		}`
		result := translateGeminiToOpenAI([]byte(geminiResp), "m")
		var oaiResp map[string]interface{}
		json.Unmarshal(result, &oaiResp)
		usage := oaiResp["usage"].(map[string]interface{})
		if usage["total_tokens"].(float64) != 15 {
			t.Errorf("expected total_tokens 15, got %v", usage["total_tokens"])
		}
	})

	t.Run("unparseable response returns original", func(t *testing.T) {
		original := []byte("not json")
		result := translateGeminiToOpenAI(original, "m")
		if string(result) != string(original) {
			t.Errorf("expected original body returned, got %s", result)
		}
	})
}

func TestTranslateGeminiChunkToOpenAI(t *testing.T) {
	t.Run("chunk translation", func(t *testing.T) {
		chunk := `{"candidates":[{"content":{"parts":[{"text":"streaming chunk"}]},"finishReason":"STOP"}]}`
		result := translateGeminiChunkToOpenAI([]byte(chunk), "gemini-pro")
		if result == nil {
			t.Fatal("expected non-nil result")
		}

		var oaiChunk map[string]interface{}
		json.Unmarshal(result, &oaiChunk)

		if oaiChunk["object"] != "chat.completion.chunk" {
			t.Errorf("expected object 'chat.completion.chunk', got %v", oaiChunk["object"])
		}
		choices := oaiChunk["choices"].([]interface{})
		choice := choices[0].(map[string]interface{})
		delta := choice["delta"].(map[string]interface{})
		if delta["content"] != "streaming chunk" {
			t.Errorf("expected 'streaming chunk', got %v", delta["content"])
		}
		if choice["finish_reason"] != "stop" {
			t.Errorf("expected finish_reason 'stop', got %v", choice["finish_reason"])
		}
	})

	t.Run("empty candidates returns nil", func(t *testing.T) {
		chunk := `{"candidates":[]}`
		result := translateGeminiChunkToOpenAI([]byte(chunk), "m")
		if result != nil {
			t.Errorf("expected nil for empty candidates, got %s", result)
		}
	})

	t.Run("no finish reason omits field", func(t *testing.T) {
		chunk := `{"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`
		result := translateGeminiChunkToOpenAI([]byte(chunk), "m")
		var oaiChunk map[string]interface{}
		json.Unmarshal(result, &oaiChunk)
		choices := oaiChunk["choices"].([]interface{})
		choice := choices[0].(map[string]interface{})
		if _, exists := choice["finish_reason"]; exists {
			t.Error("expected finish_reason to be absent")
		}
	})

	t.Run("usage in chunk", func(t *testing.T) {
		chunk := `{
			"candidates":[{"content":{"parts":[{"text":"done"}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}
		}`
		result := translateGeminiChunkToOpenAI([]byte(chunk), "m")
		var oaiChunk map[string]interface{}
		json.Unmarshal(result, &oaiChunk)

		usage, ok := oaiChunk["usage"].(map[string]interface{})
		if !ok {
			t.Fatal("expected usage in chunk")
		}
		if usage["prompt_tokens"].(float64) != 5 {
			t.Errorf("expected prompt_tokens 5, got %v", usage["prompt_tokens"])
		}
		if usage["completion_tokens"].(float64) != 3 {
			t.Errorf("expected completion_tokens 3, got %v", usage["completion_tokens"])
		}
	})

	t.Run("unparseable returns original", func(t *testing.T) {
		original := []byte("invalid")
		result := translateGeminiChunkToOpenAI(original, "m")
		if string(result) != string(original) {
			t.Errorf("expected original body returned, got %s", result)
		}
	})
}
