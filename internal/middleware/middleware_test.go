package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/models"
	"go.uber.org/zap/zaptest"
)

// mockStore implements db.Store for testing.
type mockStore struct {
	verifyApiKeyFn  func(keyValue string) (*models.ApiKey, error)
	insertReqLogFn  func(log *models.RequestLog) error
	getStatsFn      func(start, end time.Time, modelFilter string) (*models.StatsResponse, error)
}

func (m *mockStore) ListProviders() ([]models.Provider, error) { return nil, nil }
func (m *mockStore) GetProvider(id int64) (*models.Provider, error) {
	return nil, nil
}
func (m *mockStore) CreateProvider(p *models.Provider) (int64, error) { return 0, nil }
func (m *mockStore) UpdateProvider(p *models.Provider) error          { return nil }
func (m *mockStore) DeleteProvider(id int64) error                    { return nil }
func (m *mockStore) FindProviderByName(name string) (*models.Provider, error) {
	return nil, nil
}
func (m *mockStore) GetProviderByModel(model string) (*models.Provider, error) {
	return nil, nil
}
func (m *mockStore) ListProviderKeys(providerID int64) ([]models.ProviderKey, error) { return nil, nil }
func (m *mockStore) CreateProviderKey(providerID int64, keyValue string) (*models.ProviderKey, error) { return nil, nil }
func (m *mockStore) UpdateProviderKey(id int64, keyValue string, isActive bool) error { return nil }
func (m *mockStore) DeleteProviderKey(id int64) error { return nil }
func (m *mockStore) ListApiKeys() ([]models.ApiKey, error) { return nil, nil }
func (m *mockStore) GetApiKeyByName(name string) (*models.ApiKey, error) { return nil, fmt.Errorf("not found") }
func (m *mockStore) CreateApiKey(name string, rateLimitRPM int) (*models.ApiKey, error) {
	return nil, nil
}
func (m *mockStore) UpdateApiKey(id int64, name string, isActive bool, rateLimitRPM int) error {
	return nil
}
func (m *mockStore) UpdateApiKeyValue(id int64, keyValue string) error { return nil }
func (m *mockStore) DeleteApiKey(id int64) error { return nil }
func (m *mockStore) VerifyApiKey(keyValue string) (*models.ApiKey, error) {
	if m.verifyApiKeyFn != nil {
		return m.verifyApiKeyFn(keyValue)
	}
	return nil, fmt.Errorf("invalid key")
}
func (m *mockStore) InsertRequestLog(log *models.RequestLog) error {
	if m.insertReqLogFn != nil {
		return m.insertReqLogFn(log)
	}
	return nil
}
func (m *mockStore) GetStats(start, end time.Time, modelFilter string) (*models.StatsResponse, error) {
	if m.getStatsFn != nil {
		return m.getStatsFn(start, end, modelFilter)
	}
	return nil, nil
}
func (m *mockStore) GetModelLogs(model string, start, end time.Time, limit int) ([]models.RequestLog, error) {
	return nil, nil
}
func (m *mockStore) StartTraining(tool string) (int64, error) { return 0, nil }
func (m *mockStore) StopTraining(id int64) error               { return nil }
func (m *mockStore) GetTrainingStats(tool string, days int) (*db.TrainingStats, error) {
	return nil, nil
}
func (m *mockStore) GetActiveTraining(tool string) (int64, error) { return 0, nil }
func (m *mockStore) Close()                                       {}

// createTestContext creates a gin.Context for testing with an optional Authorization header.
func createTestContext(t *testing.T, authHeader string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		c.Request.Header.Set("Authorization", authHeader)
	}
	return c, w
}

// createTestContextWithIP creates a gin.Context for testing with a specific client IP.
func createTestContextWithIP(t *testing.T, ip string, authHeader string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		c.Request.Header.Set("Authorization", authHeader)
	}
	c.Request.Header.Set("X-Forwarded-For", ip)
	return c, w
}

// createTestContextWithPath creates a gin.Context for testing with a specific path.
func createTestContextWithPath(t *testing.T, path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c, w
}

// createTestContextWithMethod creates a gin.Context for testing with a specific HTTP method.
func createTestContextWithMethod(t *testing.T, method string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/", nil)
	return c, w
}

// ========== extractBearerToken Tests ==========

func TestExtractBearerToken_Valid(t *testing.T) {
	c, _ := createTestContext(t, "Bearer abc123-test-key")
	token := extractBearerToken(c)
	if token != "abc123-test-key" {
		t.Errorf("expected 'abc123-test-key', got '%s'", token)
	}
}

func TestExtractBearerToken_Missing(t *testing.T) {
	c, _ := createTestContext(t, "")
	token := extractBearerToken(c)
	if token != "" {
		t.Errorf("expected empty string, got '%s'", token)
	}
}

func TestExtractBearerToken_WrongFormat(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"Basic auth", "Basic dXNlcjpwYXNz"},
		{"No prefix", "abc123"},
		{"Lowercase bearer", "bearer abc123"},
		{"Empty Bearer", "Bearer "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := createTestContext(t, tt.value)
			token := extractBearerToken(c)
			if token != "" {
				t.Errorf("expected empty string for '%s', got '%s'", tt.value, token)
			}
		})
	}
}

func TestExtractBearerToken_BearerWithExtraWhitespace(t *testing.T) {
	c, _ := createTestContext(t, "Bearer   multiple-spaces")
	token := extractBearerToken(c)
	if token != "  multiple-spaces" {
		t.Errorf("expected '  multiple-spaces', got '%s'", token)
	}
}

// ========== AuthMiddleware Tests ==========

func TestAuthMiddleware_ValidBearerToken(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			if keyValue == "valid-key" {
				return &models.ApiKey{ID: 1, Name: "test-key", IsActive: true}, nil
			}
			return nil, fmt.Errorf("invalid key")
		},
	}

	c, w := createTestContext(t, "Bearer valid-key")
	handler := AuthMiddleware(store)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
	apiKey := GetApiKey(c)
	if apiKey == nil {
		t.Fatal("expected api key to be set in context")
	}
	if apiKey.ID != 1 || apiKey.Name != "test-key" {
		t.Errorf("unexpected api key data: %+v", apiKey)
	}
}

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContext(t, "")
	handler := AuthMiddleware(store)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_BearerPrefixOnly(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContext(t, "Bearer ")
	handler := AuthMiddleware(store)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return nil, fmt.Errorf("key not found")
		},
	}
	c, w := createTestContext(t, "Bearer invalid-key")
	handler := AuthMiddleware(store)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_NonBearerFormat(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContext(t, "Basic dXNlcjpwYXNz")
	handler := AuthMiddleware(store)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

// ========== AdminAuth Tests ==========

func TestAdminAuth_LocalhostIPv4(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContextWithIP(t, "127.0.0.1", "")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for localhost, got %d", w.Code)
	}
}

func TestAdminAuth_LocalhostIPv6(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContextWithIP(t, "::1", "")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for ::1, got %d", w.Code)
	}
}

func TestAdminAuth_PrivateIP_192_168(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContextWithIP(t, "192.168.1.100", "")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for 192.168.x.x, got %d", w.Code)
	}
}

func TestAdminAuth_PrivateIP_10(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContextWithIP(t, "10.0.0.1", "")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for 10.x.x.x, got %d", w.Code)
	}
}

func TestAdminAuth_ValidBearerFromPublicIP(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			if keyValue == "admin-key" {
				return &models.ApiKey{ID: 2, Name: "admin", IsActive: true}, nil
			}
			return nil, fmt.Errorf("invalid")
		},
	}
	c, w := createTestContextWithIP(t, "203.0.113.1", "Bearer admin-key")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for valid bearer from public IP, got %d", w.Code)
	}
	apiKey := GetApiKey(c)
	if apiKey == nil || apiKey.ID != 2 {
		t.Errorf("expected api key with ID 2, got %+v", apiKey)
	}
}

func TestAdminAuth_PublicIPNoAuth(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContextWithIP(t, "203.0.113.1", "")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for public IP without auth, got %d", w.Code)
	}
}

func TestAdminAuth_PublicIPInvalidToken(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return nil, fmt.Errorf("key not found")
		},
	}
	c, w := createTestContextWithIP(t, "203.0.113.1", "Bearer bad-key")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 for invalid token from public IP, got %d", w.Code)
	}
}

func TestAdminAuth_LocalhostWithInvalidTokenStillPasses(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return nil, fmt.Errorf("invalid key")
		},
	}
	c, w := createTestContextWithIP(t, "127.0.0.1", "Bearer bad-key")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for localhost even with invalid token, got %d", w.Code)
	}
}

func TestAdminAuth_PrivateIP10_WithInvalidTokenStillPasses(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return nil, fmt.Errorf("invalid key")
		},
	}
	c, w := createTestContextWithIP(t, "10.5.5.5", "Bearer bad-key")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for private 10.x.x.x IP even with invalid token, got %d", w.Code)
	}
}

func TestAdminAuth_192_168_WithNoAuthPasses(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContextWithIP(t, "192.168.0.1", "")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for 192.168.x.x without auth, got %d", w.Code)
	}
}

// ========== GetApiKey Tests ==========

func TestGetApiKey_NotSet(t *testing.T) {
	c, _ := createTestContext(t, "")
	key := GetApiKey(c)
	if key != nil {
		t.Errorf("expected nil, got %+v", key)
	}
}

func TestGetApiKey_WhenSet(t *testing.T) {
	c, _ := createTestContext(t, "")
	expected := &models.ApiKey{ID: 42, Name: "my-key", IsActive: true}
	c.Set("api_key", expected)

	key := GetApiKey(c)
	if key == nil {
		t.Fatal("expected non-nil api key")
	}
	if key.ID != 42 || key.Name != "my-key" {
		t.Errorf("unexpected api key: %+v", key)
	}
}

func TestGetApiKey_BadTypeInContext(t *testing.T) {
	c, _ := createTestContext(t, "")
	c.Set("api_key", "not-an-apikey")

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when api_key is wrong type")
		}
	}()
	GetApiKey(c)
}

// ========== CORS Tests ==========

func TestCORS_SetsHeadersOnGET(t *testing.T) {
	c, w := createTestContext(t, "")
	handler := CORS()
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected Access-Control-Allow-Origin: *")
	}
	if w.Header().Get("Access-Control-Allow-Methods") != "GET, POST, PUT, DELETE, OPTIONS" {
		t.Error("unexpected Access-Control-Allow-Methods")
	}
	if w.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type, X-Request-ID" {
		t.Error("unexpected Access-Control-Allow-Headers")
	}
}

func TestCORS_OPTIONSReturns204(t *testing.T) {
	c, w := createTestContextWithMethod(t, http.MethodOptions)
	handler := CORS()
	handler(c)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 No Content, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS headers on OPTIONS response")
	}
}

func TestCORS_PUTRequest(t *testing.T) {
	c, w := createTestContextWithMethod(t, http.MethodPut)
	handler := CORS()
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for PUT, got %d", w.Code)
	}
}

func TestCORS_DELETERequest(t *testing.T) {
	c, w := createTestContextWithMethod(t, http.MethodDelete)
	handler := CORS()
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for DELETE, got %d", w.Code)
	}
}

// ========== RequestLogger Tests ==========

func TestRequestLogger_SetsRequestIDHeader(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, w := createTestContextWithPath(t, "/v1/chat/completions")
	handler := RequestLogger(logger, store)
	handler(c)

	reqID := w.Header().Get("X-Request-ID")
	if reqID == "" {
		t.Error("expected X-Request-ID header to be set")
	}
}

func TestRequestLogger_SetsLogEntryInContext(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, w := createTestContextWithPath(t, "/v1/chat/completions")
	handler := RequestLogger(logger, store)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	entry, exists := c.Get("log_entry")
	if !exists {
		t.Fatal("expected log_entry in context")
	}
	logEntry, ok := entry.(*LogEntry)
	if !ok {
		t.Fatalf("expected *LogEntry, got %T", entry)
	}
	if logEntry.RequestID == "" {
		t.Error("expected non-empty RequestID")
	}
	if logEntry.RequestType != "chat" {
		t.Errorf("expected default RequestType 'chat', got '%s'", logEntry.RequestType)
	}
}

func TestRequestLogger_SetsRequestIDInContext(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/chat/completions")
	handler := RequestLogger(logger, store)
	handler(c)

	reqIDVal, exists := c.Get("request_id")
	if !exists {
		t.Fatal("expected request_id in context")
	}
	reqID, ok := reqIDVal.(string)
	if !ok {
		t.Fatalf("expected string request_id, got %T", reqIDVal)
	}
	if reqID == "" {
		t.Error("expected non-empty request_id")
	}
}

func TestRequestLogger_ChatDetection(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/chat/completions")
	handler := RequestLogger(logger, store)
	handler(c)

	entry, _ := c.Get("log_entry")
	logEntry := entry.(*LogEntry)
	if logEntry.RequestType != "chat" {
		t.Errorf("expected 'chat', got '%s'", logEntry.RequestType)
	}
}

func TestRequestLogger_EmbeddingDetection(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/embeddings")
	handler := RequestLogger(logger, store)
	handler(c)

	entry, _ := c.Get("log_entry")
	logEntry := entry.(*LogEntry)
	if logEntry.RequestType != "embedding" {
		t.Errorf("expected 'embedding', got '%s'", logEntry.RequestType)
	}
}

func TestRequestLogger_MessageDetection(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/messages")
	handler := RequestLogger(logger, store)
	handler(c)

	entry, _ := c.Get("log_entry")
	logEntry := entry.(*LogEntry)
	if logEntry.RequestType != "message" {
		t.Errorf("expected 'message', got '%s'", logEntry.RequestType)
	}
}

func TestRequestLogger_GenericPathDefaultsToChat(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/responses")
	handler := RequestLogger(logger, store)
	handler(c)

	entry, _ := c.Get("log_entry")
	logEntry := entry.(*LogEntry)
	if logEntry.RequestType != "responses" {
		t.Errorf("expected 'responses', got '%s'", logEntry.RequestType)
	}
}

func TestRequestLogger_SkipsAdminPath(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/admin/api/providers")
	handler := RequestLogger(logger, store)
	handler(c)

	_, exists := c.Get("log_entry")
	if exists {
		t.Error("expected no log_entry for admin path")
	}
}

func TestRequestLogger_SkipsModelsPath(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/models")
	handler := RequestLogger(logger, store)
	handler(c)

	_, exists := c.Get("log_entry")
	if exists {
		t.Error("expected no log_entry for /v1/models path")
	}
}

func TestRequestLogger_SkipsRootPath(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/")
	handler := RequestLogger(logger, store)
	handler(c)

	_, exists := c.Get("log_entry")
	if exists {
		t.Error("expected no log_entry for root path")
	}
}

func TestRequestLogger_InsertsRequestLog(t *testing.T) {
	var capturedLog *models.RequestLog
	store := &mockStore{
		insertReqLogFn: func(log *models.RequestLog) error {
			capturedLog = log
			return nil
		},
	}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/chat/completions")
	handler := RequestLogger(logger, store)
	handler(c)

	// The insert happens in a goroutine; wait briefly for it.
	time.Sleep(50 * time.Millisecond)

	if capturedLog == nil {
		t.Fatal("expected InsertRequestLog to be called")
	}
	if capturedLog.RequestType != "chat" {
		t.Errorf("expected 'chat', got '%s'", capturedLog.RequestType)
	}
}

func TestRequestLogger_InsertsRequestLogWithApiKey(t *testing.T) {
	var capturedLog *models.RequestLog
	store := &mockStore{
		insertReqLogFn: func(log *models.RequestLog) error {
			capturedLog = log
			return nil
		},
	}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/chat/completions")
	apiKey := &models.ApiKey{ID: 7, Name: "test-key"}
	c.Set("api_key", apiKey)

	handler := RequestLogger(logger, store)
	handler(c)

	time.Sleep(50 * time.Millisecond)

	if capturedLog == nil {
		t.Fatal("expected InsertRequestLog to be called")
	}
	if capturedLog.ApiKeyID == nil || *capturedLog.ApiKeyID != 7 {
		t.Errorf("expected ApiKeyID=7, got %v", capturedLog.ApiKeyID)
	}
}

func TestRequestLogger_InsertsRequestLogWithProviderID(t *testing.T) {
	var capturedLog *models.RequestLog
	store := &mockStore{
		insertReqLogFn: func(log *models.RequestLog) error {
			capturedLog = log
			return nil
		},
	}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/chat/completions")
	c.Set("provider_id", int64(99))

	handler := RequestLogger(logger, store)
	handler(c)

	time.Sleep(50 * time.Millisecond)

	if capturedLog == nil {
		t.Fatal("expected InsertRequestLog to be called")
	}
	if capturedLog.ProviderID == nil || *capturedLog.ProviderID != 99 {
		t.Errorf("expected ProviderID=99, got %v", capturedLog.ProviderID)
	}
}

func TestRequestLogger_LogsErrorStatus(t *testing.T) {
	var capturedLog *models.RequestLog
	store := &mockStore{
		insertReqLogFn: func(log *models.RequestLog) error {
			capturedLog = log
			return nil
		},
	}
	logger := zaptest.NewLogger(t)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogger(logger, store))
	r.GET("/v1/chat/completions", func(c *gin.Context) {
		c.Status(http.StatusInternalServerError)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	r.ServeHTTP(w, req)

	time.Sleep(50 * time.Millisecond)

	if capturedLog == nil {
		t.Fatal("expected InsertRequestLog to be called")
	}
	if capturedLog.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", capturedLog.StatusCode)
	}
	if !capturedLog.IsError {
		t.Error("expected IsError to be true for 500 status")
	}
}

func TestRequestLogger_TokenFieldsInContext(t *testing.T) {
	var capturedLog *models.RequestLog
	store := &mockStore{
		insertReqLogFn: func(log *models.RequestLog) error {
			capturedLog = log
			return nil
		},
	}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/v1/chat/completions")
	c.Set("proxy_prompt_tokens", 150)
	c.Set("proxy_completion_tokens", 80)
	c.Set("proxy_input_cache_tokens", 30)
	c.Set("request_summary", "user request summary")
	c.Set("response_summary", "model response summary")

	handler := RequestLogger(logger, store)
	handler(c)

	time.Sleep(50 * time.Millisecond)

	if capturedLog == nil {
		t.Fatal("expected InsertRequestLog to be called")
	}
	if capturedLog.PromptTokens != 150 {
		t.Errorf("expected PromptTokens=150, got %d", capturedLog.PromptTokens)
	}
	if capturedLog.CompletionTokens != 80 {
		t.Errorf("expected CompletionTokens=80, got %d", capturedLog.CompletionTokens)
	}
	if capturedLog.InputCacheTokens != 30 {
		t.Errorf("expected InputCacheTokens=30, got %d", capturedLog.InputCacheTokens)
	}
	if capturedLog.RequestSummary != "user request summary" {
		t.Errorf("expected RequestSummary='user request summary', got '%s'", capturedLog.RequestSummary)
	}
	if capturedLog.ResponseSummary != "model response summary" {
		t.Errorf("expected ResponseSummary='model response summary', got '%s'", capturedLog.ResponseSummary)
	}
}

// ========== LogEntry Type Tests ==========

func TestLogEntry_Fields(t *testing.T) {
	now := time.Now()
	entry := &LogEntry{
		RequestID:   "req-123",
		StartTime:   now,
		Model:       "qwen-turbo",
		RequestType: "chat",
	}

	if entry.RequestID != "req-123" {
		t.Errorf("expected RequestID 'req-123', got '%s'", entry.RequestID)
	}
	if !entry.StartTime.Equal(now) {
		t.Error("expected StartTime to match")
	}
	if entry.Model != "qwen-turbo" {
		t.Errorf("expected Model 'qwen-turbo', got '%s'", entry.Model)
	}
	if entry.RequestType != "chat" {
		t.Errorf("expected RequestType 'chat', got '%s'", entry.RequestType)
	}
}

// ========== Handler Chain Integration Tests ==========

func TestHandlerChain_AuthThenCORS(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return &models.ApiKey{ID: 1, IsActive: true}, nil
		},
	}

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request.Header.Set("Authorization", "Bearer any-key")

	AuthMiddleware(store)(c)
	if !c.IsAborted() {
		CORS()(c)
	}

	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("expected CORS headers in chained handler")
	}
}

func TestHandlerChain_LoggerWithAuth(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return &models.ApiKey{ID: 5, Name: "chain-test"}, nil
		},
		insertReqLogFn: func(log *models.RequestLog) error { return nil },
	}
	logger := zaptest.NewLogger(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	c.Request.Header.Set("Authorization", "Bearer token-1")

	RequestLogger(logger, store)(c)
	AuthMiddleware(store)(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 in chain, got %d", w.Code)
	}

	_, hasLog := c.Get("log_entry")
	if !hasLog {
		t.Error("expected log_entry in context after logger middleware")
	}
	apiKey := GetApiKey(c)
	if apiKey == nil {
		t.Error("expected api_key in context after auth middleware")
	}
}

func TestHandlerChain_All(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return &models.ApiKey{ID: 3, IsActive: true}, nil
		},
		insertReqLogFn: func(log *models.RequestLog) error { return nil },
	}
	logger := zaptest.NewLogger(t)

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodOptions, "/v1/embeddings", nil)
	c.Request.Header.Set("Authorization", "Bearer multi-token")

	CORS()(c)
	if !c.IsAborted() {
		t.Fatal("expected OPTIONS to be aborted by CORS")
	}

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS origin header")
	}

	// Test a non-OPTIONS request with the full chain
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
	c2.Request.Header.Set("Authorization", "Bearer multi-token")

	RequestLogger(logger, store)(c2)
	AuthMiddleware(store)(c2)
	CORS()(c2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 for full chain POST, got %d", w2.Code)
	}

	entry, _ := c2.Get("log_entry")
	logEntry := entry.(*LogEntry)
	if logEntry.RequestType != "embedding" {
		t.Errorf("expected 'embedding' type, got '%s'", logEntry.RequestType)
	}
}

// ========== edge case: nil logger ==========

func TestRequestLogger_NilLoggerPanics(t *testing.T) {
	store := &mockStore{
		insertReqLogFn: func(log *models.RequestLog) error { return nil },
	}
	c, _ := createTestContextWithPath(t, "/v1/chat/completions")
	handler := RequestLogger(nil, store)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic with nil logger")
		}
	}()
	handler(c)
}

// ========== misc ==========

func TestAuthMiddleware_NextNotCalledOnFailure(t *testing.T) {
	var called bool
	store := &mockStore{}
	c, w := createTestContext(t, "Bearer bad-token")

	handler := AuthMiddleware(store)
	handler(c)

	c.Next() // should be no-op since aborted
	called = !c.IsAborted()

	if called {
		t.Error("expected handler to be aborted")
	}
	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAdminAuth_InvalidBearerFallsThroughToIPCheck(t *testing.T) {
	store := &mockStore{
		verifyApiKeyFn: func(keyValue string) (*models.ApiKey, error) {
			return nil, fmt.Errorf("not found")
		},
	}
	c, w := createTestContextWithIP(t, "192.168.2.2", "Bearer wrong-key")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 falling through to IP check, got %d", w.Code)
	}
}

func TestAdminAuth_EmptyAuthHeaderLocalhost(t *testing.T) {
	store := &mockStore{}
	c, w := createTestContextWithIP(t, "127.0.0.1", "")
	handler := AdminAuth(store, nil)
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAdminAuth_NonPrivateIPRejected(t *testing.T) {
	store := &mockStore{}
	tests := []string{
		"172.16.0.1",
		"8.8.8.8",
		"100.64.0.1",
	}
	for _, ip := range tests {
		t.Run("IP="+ip, func(t *testing.T) {
			c, w := createTestContextWithIP(t, ip, "")
			handler := AdminAuth(store, nil)
			handler(c)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 for IP %s, got %d", ip, w.Code)
			}
		})
	}
}

func TestCORS_POSTRequest(t *testing.T) {
	c, w := createTestContextWithMethod(t, http.MethodPost)
	handler := CORS()
	handler(c)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for POST, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS origin header")
	}
}

func TestRequestLogger_DifferentEmbeddingPath(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/api/embeddings")
	handler := RequestLogger(logger, store)
	handler(c)

	entry, _ := c.Get("log_entry")
	logEntry := entry.(*LogEntry)
	if logEntry.RequestType != "embedding" {
		t.Errorf("expected 'embedding', got '%s'", logEntry.RequestType)
	}
}

func TestRequestLogger_DifferentMessagePath(t *testing.T) {
	store := &mockStore{}
	logger := zaptest.NewLogger(t)
	c, _ := createTestContextWithPath(t, "/api/conversations/messages")
	handler := RequestLogger(logger, store)
	handler(c)

	entry, _ := c.Get("log_entry")
	logEntry := entry.(*LogEntry)
	if logEntry.RequestType != "message" {
		t.Errorf("expected 'message', got '%s'", logEntry.RequestType)
	}
}
