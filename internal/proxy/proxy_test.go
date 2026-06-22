package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/models"
)

// =============================================================================
// mockStore implements db.Store for testing
// =============================================================================
type mockStore struct {
	providers []models.Provider
	err       error
	mu        sync.Mutex
}

func (m *mockStore) ListProviders() ([]models.Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.providers, nil
}
func (m *mockStore) setProviders(providers []models.Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = providers
}
func (m *mockStore) GetProvider(id int64) (*models.Provider, error)       { return nil, nil }
func (m *mockStore) CreateProvider(p *models.Provider) (int64, error)     { return 0, nil }
func (m *mockStore) UpdateProvider(p *models.Provider) error              { return nil }
func (m *mockStore) DeleteProvider(id int64) error                        { return nil }
func (m *mockStore) FindProviderByName(name string) (*models.Provider, error) { return nil, nil }
func (m *mockStore) GetProviderByModel(model string) (*models.Provider, error) { return nil, nil }
func (m *mockStore) ListProviderKeys(providerID int64) ([]models.ProviderKey, error) { return nil, nil }
func (m *mockStore) CreateProviderKey(providerID int64, keyValue string) (*models.ProviderKey, error) { return nil, nil }
func (m *mockStore) UpdateProviderKey(id int64, keyValue string, isActive bool) error { return nil }
func (m *mockStore) DeleteProviderKey(id int64) error { return nil }
func (m *mockStore) ListApiKeys() ([]models.ApiKey, error)                { return nil, nil }
func (m *mockStore) GetApiKeyByName(name string) (*models.ApiKey, error)  { return nil, fmt.Errorf("not found") }
func (m *mockStore) CreateApiKey(name string, rateLimitRPM int) (*models.ApiKey, error) { return nil, nil }
func (m *mockStore) UpdateApiKeyValue(id int64, keyValue string) error { return nil }
func (m *mockStore) UpdateApiKey(id int64, name string, isActive bool, rateLimitRPM int) error { return nil }
func (m *mockStore) DeleteApiKey(id int64) error                        { return nil }
func (m *mockStore) VerifyApiKey(keyValue string) (*models.ApiKey, error) { return nil, nil }
func (m *mockStore) InsertRequestLog(log *models.RequestLog) error       { return nil }
func (m *mockStore) GetStats(start, end time.Time, modelFilter string) (*models.StatsResponse, error) { return nil, nil }
func (m *mockStore) GetModelLogs(model string, start, end time.Time, limit int) ([]models.RequestLog, error) { return nil, nil }
func (m *mockStore) StartTraining(tool string) (int64, error)            { return 0, nil }
func (m *mockStore) StopTraining(id int64) error                         { return nil }
func (m *mockStore) GetTrainingStats(tool string, days int) (*db.TrainingStats, error) { return nil, nil }
func (m *mockStore) GetActiveTraining(tool string) (int64, error)        { return 0, nil }
func (m *mockStore) Close()                                              {}

func makeProviders() []models.Provider {
	return []models.Provider{
		{
			ID:           1,
			Name:         "openai-provider",
			ProviderType: "openai",
			BaseURL:      "https://api.openai.com/v1",
			APIKey:       "sk-test",
			Priority:     10,
			Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "GPT-4"},
				{Name: "gpt-4-turbo", DisplayName: "GPT-4 Turbo"},
				{Name: "gpt-3.5-turbo", DisplayName: "ChatGPT"},
			},
		},
		{
			ID:           2,
			Name:         "anthropic-provider",
			ProviderType: "anthropic",
			BaseURL:      "https://api.anthropic.com/v1",
			APIKey:       "sk-ant-test",
			Priority:     5,
			Models: []models.ModelConfig{
				{Name: "claude-3-opus", DisplayName: "Claude 3 Opus"},
				{Name: "claude-3-sonnet", DisplayName: "Claude 3 Sonnet"},
				{Name: "*"},
			},
		},
	}
}

// =============================================================================
// Router Tests
// =============================================================================

func TestRouter_FindProvider_ExactMatch(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)
	_ = r.Refresh() // ignore error, providers pre-set

	p, err := r.FindProvider("gpt-4")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.Name != "openai-provider" {
		t.Errorf("expected openai-provider, got %s", p.Name)
	}
}

func TestRouter_FindProvider_DisplayNameMatch(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)
	_ = r.Refresh()

	p, err := r.FindProvider("GPT-4")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if p.Name != "openai-provider" {
		t.Errorf("expected openai-provider, got %s", p.Name)
	}
}

func TestRouter_FindProvider_PrefixMatch(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)
	_ = r.Refresh()

	p, err := r.FindProvider("gpt-4o-2024-05-13")
	if err != nil {
		t.Fatalf("expected prefix match, got error: %v", err)
	}
	if p.Name != "openai-provider" {
		t.Errorf("expected openai-provider, got %s", p.Name)
	}
}

func TestRouter_FindProvider_WildcardMatch(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)
	_ = r.Refresh()

	p, err := r.FindProvider("some-unknown-model")
	if err != nil {
		t.Fatalf("expected wildcard match, got error: %v", err)
	}
	if p.Name != "anthropic-provider" {
		t.Errorf("expected anthropic-provider, got %s", p.Name)
	}
}

func TestRouter_FindProvider_NoMatch(t *testing.T) {
	store := &mockStore{providers: []models.Provider{
		{ID: 1, Name: "openai", Models: []models.ModelConfig{
			{Name: "gpt-4", DisplayName: "GPT-4"},
		}},
	}}
	r := NewRouter(store)
	_ = r.Refresh()

	_, err := r.FindProvider("nonexistent-model")
	if err == nil {
		t.Fatal("expected error for no match")
	}
	if !strings.Contains(err.Error(), "no provider found") {
		t.Errorf("expected 'no provider found' in error, got %v", err)
	}
}

func TestRouter_FindProvider_PrefixedFormat(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)
	_ = r.Refresh()

	t.Run("matches provider_prefix.model_name", func(t *testing.T) {
		p, err := r.FindProvider("openai-provider.gpt-4")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.Name != "openai-provider" {
			t.Errorf("expected openai-provider, got %s", p.Name)
		}
	})

	t.Run("matches provider_prefix.display_name", func(t *testing.T) {
		p, err := r.FindProvider("openai-provider.GPT-4")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.Name != "openai-provider" {
			t.Errorf("expected openai-provider, got %s", p.Name)
		}
	})

	t.Run("matches second provider with prefix", func(t *testing.T) {
		p, err := r.FindProvider("anthropic-provider.claude-3-opus")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.Name != "anthropic-provider" {
			t.Errorf("expected anthropic-provider, got %s", p.Name)
		}
	})

	t.Run("unprefixed model still matches via standard logic", func(t *testing.T) {
		p, err := r.FindProvider("gpt-4")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if p.Name != "openai-provider" {
			t.Errorf("expected openai-provider, got %s", p.Name)
		}
	})

}

func TestRouter_Refresh(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)

	err := r.Refresh()
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	p, err := r.FindProvider("gpt-4")
	if err != nil {
		t.Fatalf("expected to find provider after refresh: %v", err)
	}
	if p.Name != "openai-provider" {
		t.Errorf("expected openai-provider, got %s", p.Name)
	}
}

func TestRouter_FindProvider_ReturnsCopy(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)
	_ = r.Refresh()

	p1, _ := r.FindProvider("gpt-4")
	p2, _ := r.FindProvider("gpt-4")

	if p1 == p2 {
		t.Error("expected FindProvider to return copies (different pointers)")
	}
	p1.Name = "modified"
	if p2.Name == "modified" {
		t.Error("modifying one result should not affect the other")
	}
}

func TestRouter_ConcurrentAccess(t *testing.T) {
	store := &mockStore{providers: makeProviders()}
	r := NewRouter(store)
	_ = r.Refresh()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	const goroutines = 50
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_, err := r.FindProvider("gpt-4")
				if err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				err := r.Refresh()
				if err != nil {
					errCh <- err
					return
				}
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent access error: %v", err)
	}
}

// =============================================================================
// Truncate Tests
// =============================================================================

func TestTruncate_ShortString(t *testing.T) {
	result := Truncate("hello", 10)
	if result != "hello" {
		t.Errorf("expected 'hello', got '%s'", result)
	}
}

func TestTruncate_LongString(t *testing.T) {
	result := Truncate("hello world this is a long string", 10)
	expected := "hello worl..."
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestTruncate_ExactBoundary(t *testing.T) {
	s := "0123456789"
	result := Truncate(s, 10)
	if result != s {
		t.Errorf("expected '%s', got '%s'", s, result)
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	result := Truncate("", 5)
	if result != "" {
		t.Errorf("expected '', got '%s'", result)
	}
}

func TestTruncate_Unicode(t *testing.T) {
	result := Truncate("こんにちは世界", 4)
	expected := "こんにち..."
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestTruncate_UnicodeWithinLimit(t *testing.T) {
	result := Truncate("こんにちは", 10)
	if result != "こんにちは" {
		t.Errorf("expected 'こんにちは', got '%s'", result)
	}
}

// =============================================================================
// ExtractRequestSummary Tests
// =============================================================================

func TestExtractRequestSummary_WithUserMessage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are helpful"},{"role":"user","content":"What is Go?"}]}`)
	result := ExtractRequestSummary(body)
	if result != "What is Go?" {
		t.Errorf("expected 'What is Go?', got '%s'", result)
	}
}

func TestExtractRequestSummary_SystemOnly(t *testing.T) {
	body := []byte(`{"messages":[{"role":"system","content":"You are a helpful assistant"}]}`)
	result := ExtractRequestSummary(body)
	if result != "You are a helpful assistant" {
		t.Errorf("expected system content, got '%s'", result)
	}
}

func TestExtractRequestSummary_EmptyBody(t *testing.T) {
	result := ExtractRequestSummary([]byte{})
	if result != "" {
		t.Errorf("expected '', got '%s'", result)
	}
}

func TestExtractRequestSummary_NoMessages(t *testing.T) {
	body := []byte(`{"model":"gpt-4","temperature":0.7}`)
	result := ExtractRequestSummary(body)
	if result != `{"model":"gpt-4","temperature":0.7}` {
		t.Errorf("expected raw body, got '%s'", result)
	}
}

func TestExtractRequestSummary_InvalidJSON(t *testing.T) {
	body := []byte(`not valid json at all`)
	result := ExtractRequestSummary(body)
	if result != "not valid json at all" {
		t.Errorf("expected raw body, got '%s'", result)
	}
}

func TestExtractRequestSummary_LastUserMessage(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"First message"},{"role":"assistant","content":"Reply"},{"role":"user","content":"Second message"}]}`)
	result := ExtractRequestSummary(body)
	if result != "Second message" {
		t.Errorf("expected 'Second message', got '%s'", result)
	}
}

// =============================================================================
// ExtractResponseSummary Tests
// =============================================================================

func TestExtractResponseSummary_ValidResponse(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"Hello! How can I help?"}}]}`)
	result := ExtractResponseSummary(body)
	if result != "Hello! How can I help?" {
		t.Errorf("expected 'Hello! How can I help?', got '%s'", result)
	}
}

func TestExtractResponseSummary_EmptyResponse(t *testing.T) {
	result := ExtractResponseSummary([]byte{})
	if result != "" {
		t.Errorf("expected '', got '%s'", result)
	}
}

func TestExtractResponseSummary_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	result := ExtractResponseSummary(body)
	if result != "" {
		t.Errorf("expected '', got '%s'", result)
	}
}

func TestExtractResponseSummary_NoChoices(t *testing.T) {
	body := []byte(`{"choices":[]}`)
	result := ExtractResponseSummary(body)
	if result != "" {
		t.Errorf("expected '', got '%s'", result)
	}
}

func TestExtractResponseSummary_MultipleChoices(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"First"}},{"message":{"content":"Second"}}]}`)
	result := ExtractResponseSummary(body)
	if result != "First" {
		t.Errorf("expected 'First', got '%s'", result)
	}
}

// =============================================================================
// estimatePromptTokens Tests
// =============================================================================

func TestEstimatePromptTokens_FromContent(t *testing.T) {
	body := []byte(`{"messages":[{"content":"Hello world"},{"content":"How are you today?"}]}`)
	result := estimatePromptTokens(body)
	expected := (11 + 19) / 4
	if result != expected {
		t.Errorf("expected %d, got %d", expected, result)
	}
}

func TestEstimatePromptTokens_EmptyBodyFallback(t *testing.T) {
	body := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	result := estimatePromptTokens(body)
	if result != 2 {
		t.Errorf("expected 2, got %d", result)
	}
}

func TestEstimatePromptTokens_NoMessages(t *testing.T) {
	body := []byte(`{"model":"gpt-4"}`)
	result := estimatePromptTokens(body)
	if result != len(body)/4 {
		t.Errorf("expected %d, got %d", len(body)/4, result)
	}
}

func TestEstimatePromptTokens_SmallContent(t *testing.T) {
	body := []byte(`{"messages":[{"content":"Hi"}]}`)
	result := estimatePromptTokens(body)
	if result != 1 {
		t.Errorf("expected 1 for small content, got %d", result)
	}
}

func TestEstimatePromptTokens_UnicodeContent(t *testing.T) {
	body := []byte(`{"messages":[{"content":"こんにちは世界"}]}`)
	result := estimatePromptTokens(body)
	// 7 runes / 4 = 1
	if result != 1 {
		t.Errorf("expected 1, got %d", result)
	}
}

// =============================================================================
// mergeExtraBody Tests
// =============================================================================

func TestMergeExtraBody_EmptyExtra(t *testing.T) {
	original := []byte(`{"model":"gpt-4","temperature":0.5}`)
	result := MergeExtraBody(original, nil)
	if string(result) != string(original) {
		t.Errorf("expected unchanged body, got '%s'", result)
	}
}

func TestMergeExtraBody_MergeExtraFields(t *testing.T) {
	original := []byte(`{"model":"gpt-4","temperature":0.5}`)
	extra := map[string]interface{}{"max_tokens": 1024, "top_p": 0.9}
	result := MergeExtraBody(original, extra)

	var m map[string]interface{}
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if m["max_tokens"].(float64) != 1024 {
		t.Errorf("expected max_tokens=1024, got %v", m["max_tokens"])
	}
	if m["top_p"].(float64) != 0.9 {
		t.Errorf("expected top_p=0.9, got %v", m["top_p"])
	}
}

func TestMergeExtraBody_OverrideExistingFields(t *testing.T) {
	original := []byte(`{"model":"gpt-4","max_tokens":512}`)
	extra := map[string]interface{}{"max_tokens": 2048}
	result := MergeExtraBody(original, extra)

	var m map[string]interface{}
	json.Unmarshal(result, &m)
	if m["max_tokens"].(float64) != 2048 {
		t.Errorf("expected overridden max_tokens=2048, got %v", m["max_tokens"])
	}
}

func TestMergeExtraBody_InvalidJSON(t *testing.T) {
	original := []byte(`not json`)
	extra := map[string]interface{}{"key": "value"}
	result := MergeExtraBody(original, extra)
	if string(result) != string(original) {
		t.Errorf("expected original body on invalid JSON, got '%s'", result)
	}
}

// =============================================================================
// findModelConfig Tests
// =============================================================================

func TestFindModelConfig_ExactNameMatch(t *testing.T) {
	provider := &models.Provider{
		Models: []models.ModelConfig{
			{Name: "gpt-4", DisplayName: "GPT-4"},
			{Name: "gpt-3.5-turbo", DisplayName: "ChatGPT"},
		},
	}
	result := FindModelConfig(provider, "gpt-4")
	if result == nil {
		t.Fatal("expected to find config")
	}
	if result.Name != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", result.Name)
	}
}

func TestFindModelConfig_DisplayNameMatch(t *testing.T) {
	provider := &models.Provider{
		Models: []models.ModelConfig{
			{Name: "gpt-4", DisplayName: "GPT-4"},
			{Name: "gpt-3.5-turbo", DisplayName: "ChatGPT"},
		},
	}
	result := FindModelConfig(provider, "GPT-4")
	if result == nil {
		t.Fatal("expected to find config by display name")
	}
	if result.Name != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", result.Name)
	}
}

func TestFindModelConfig_NotFound(t *testing.T) {
	provider := &models.Provider{
		Models: []models.ModelConfig{
			{Name: "gpt-4", DisplayName: "GPT-4"},
		},
	}
	result := FindModelConfig(provider, "claude-3")
	if result != nil {
		t.Errorf("expected nil, got %+v", result)
	}
}

func TestFindModelConfig_EmptyModels(t *testing.T) {
	provider := &models.Provider{Models: []models.ModelConfig{}}
	result := FindModelConfig(provider, "gpt-4")
	if result != nil {
		t.Errorf("expected nil for empty models, got %+v", result)
	}
}

// =============================================================================
// ParseTokens Tests
// =============================================================================

func TestParseTokens_OpenAIFormat(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":50}}`)
	pt, ct, ict := ParseTokens(body)
	if pt != 100 {
		t.Errorf("expected prompt_tokens=100, got %d", pt)
	}
	if ct != 50 {
		t.Errorf("expected completion_tokens=50, got %d", ct)
	}
	if ict != 0 {
		t.Errorf("expected input_cache_tokens=0, got %d", ict)
	}
}

func TestParseTokens_WithCacheTokens(t *testing.T) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 200,
			"completion_tokens": 80,
			"prompt_tokens_details": {"cached_tokens": 50}
		}
	}`)
	_, _, ict := ParseTokens(body)
	if ict != 50 {
		t.Errorf("expected input_cache_tokens=50, got %d", ict)
	}
}

func TestParseTokens_WithCacheReadInputTokens(t *testing.T) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 200,
			"completion_tokens": 80,
			"cache_read_input_tokens": 75
		}
	}`)
	_, _, ict := ParseTokens(body)
	if ict != 75 {
		t.Errorf("expected input_cache_tokens=75, got %d", ict)
	}
}

func TestParseTokens_WithCacheCreationInputTokens(t *testing.T) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 300,
			"completion_tokens": 120,
			"cache_creation_input_tokens": 90
		}
	}`)
	_, _, ict := ParseTokens(body)
	if ict != 90 {
		t.Errorf("expected input_cache_tokens=90, got %d", ict)
	}
}

func TestParseTokens_WithReasoningTokens(t *testing.T) {
	body := []byte(`{
		"usage": {
			"prompt_tokens": 100,
			"completion_tokens": 50,
			"completion_tokens_details": {"reasoning_tokens": 200}
		}
	}`)
	_, ct, _ := ParseTokens(body)
	if ct != 250 {
		t.Errorf("expected completion_tokens=250 (50+200), got %d", ct)
	}
}

func TestParseTokens_EmptyBody(t *testing.T) {
	pt, ct, ict := ParseTokens([]byte{})
	if pt != 0 || ct != 0 || ict != 0 {
		t.Errorf("expected all zeros, got pt=%d ct=%d ict=%d", pt, ct, ict)
	}
}

func TestParseTokens_InvalidJSON(t *testing.T) {
	pt, ct, ict := ParseTokens([]byte(`not json`))
	if pt != 0 || ct != 0 || ict != 0 {
		t.Errorf("expected all zeros, got pt=%d ct=%d ict=%d", pt, ct, ict)
	}
}

func TestParseTokens_NoUsage(t *testing.T) {
	body := []byte(`{"id":"chatcmpl-123"}`)
	pt, ct, ict := ParseTokens(body)
	if pt != 0 || ct != 0 || ict != 0 {
		t.Errorf("expected all zeros, got pt=%d ct=%d ict=%d", pt, ct, ict)
	}
}

// =============================================================================
// estimateCompletionTokens Tests
// =============================================================================

func TestEstimateCompletionTokens_OpenAIChoices(t *testing.T) {
	body := []byte(`{"choices":[{"message":{"content":"Hello world!"}}]}`)
	n := estimateCompletionTokens(body)
	if n < 1 {
		t.Errorf("expected >0 tokens, got %d", n)
	}
}

func TestEstimateCompletionTokens_DeltaContent(t *testing.T) {
	body := []byte(`{"choices":[{"delta":{"content":"Hello world!"}}]}`)
	n := estimateCompletionTokens(body)
	if n < 1 {
		t.Errorf("expected >0 tokens, got %d", n)
	}
}

func TestEstimateCompletionTokens_TextChoice(t *testing.T) {
	body := []byte(`{"choices":[{"text":"Hello world!"}]}`)
	n := estimateCompletionTokens(body)
	if n < 1 {
		t.Errorf("expected >0 tokens, got %d", n)
	}
}

func TestEstimateCompletionTokens_EmptyBody(t *testing.T) {
	n := estimateCompletionTokens([]byte{})
	if n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestEstimateCompletionTokens_LongContent(t *testing.T) {
	content := "Hello " + strings.Repeat("world ", 100)
	body := []byte(`{"choices":[{"message":{"content":"` + content + `"}}]}`)
	n := estimateCompletionTokens(body)
	if n < 100 {
		t.Errorf("expected >=100 tokens for long content, got %d", n)
	}
}

func TestEstimateCompletionTokens_InvalidJSON(t *testing.T) {
	body := []byte(`not json but long enough to get some estimate`)
	n := estimateCompletionTokens(body)
	if n < 1 {
		t.Errorf("expected >0 tokens for raw body fallback, got %d", n)
	}
}

// =============================================================================
// sseWriter Tests
// =============================================================================

type testWriter struct {
	written []byte
}

func (tw *testWriter) Write(p []byte) (int, error) {
	tw.written = append(tw.written, p...)
	return len(p), nil
}

func TestSSEWriter_WritesData(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	n, err := sw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != 5 {
		t.Errorf("expected n=5, got %d", n)
	}
	if string(tw.written) != "hello" {
		t.Errorf("expected 'hello', got '%s'", tw.written)
	}
}

func TestSSEWriter_CapturesLastUsage(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	sw.Write([]byte("data: {\"usage\":{\"prompt_tokens\":100}}\n"))
	sw.Write([]byte("data: {\"usage\":{\"prompt_tokens\":200}}\n"))

	var usage map[string]interface{}
	json.Unmarshal(sw.lastUsage, &usage)
	if u, ok := usage["usage"]; ok {
		usageMap := u.(map[string]interface{})
		if usageMap["prompt_tokens"].(float64) != 200 {
			t.Errorf("expected prompt_tokens=200 in last usage, got %v", usageMap["prompt_tokens"])
		}
	} else {
		t.Error("expected lastUsage to contain usage data")
	}
}

func TestSSEWriter_IgnoresDone(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	sw.Write([]byte("data: {\"usage\":{\"prompt_tokens\":50}}\n"))
	prevLastUsage := string(sw.lastUsage)
	sw.Write([]byte("data: [DONE]\n"))

	if string(sw.lastUsage) != prevLastUsage {
		t.Error("[DONE] should not overwrite lastUsage")
	}
}

func TestSSEWriter_CapturesDeltaContent(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	sw.Write([]byte(`data: {"choices":[{"delta":{"content":"Hello"}}]}` + "\n"))
	sw.Write([]byte(`data: {"choices":[{"delta":{"content":" world"}}]}` + "\n"))
	sw.Write([]byte(`data: {"choices":[{"delta":{"content":"!"}}]}` + "\n"))

	if sw.content.String() != "Hello world!" {
		t.Errorf("expected 'Hello world!', got '%s'", sw.content.String())
	}
}

func TestSSEWriter_MultipleChoicesDelta(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	sw.Write([]byte(`data: {"choices":[{"delta":{"content":"A"}},{"delta":{"content":"B"}}]}` + "\n"))

	if sw.content.String() != "AB" {
		t.Errorf("expected 'AB', got '%s'", sw.content.String())
	}
}

func TestSSEWriter_EmptyDeltaContent(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	sw.Write([]byte(`data: {"choices":[{"delta":{"content":""}}]}` + "\n"))

	if sw.content.String() != "" {
		t.Errorf("expected '', got '%s'", sw.content.String())
	}
}

func TestSSEWriter_PartialLineBuffering(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	sw.Write([]byte("data: {\"usage\":{\"completion_tokens\":99")) // no newline yet
	if string(sw.lastUsage) != "" {
		t.Error("should not process partial line")
	}
	sw.Write([]byte("}}\n"))
	if string(sw.lastUsage) == "" {
		t.Error("should process after newline is received")
	}
}

func TestSSEWriter_NonDataLines(t *testing.T) {
	tw := &testWriter{}
	sw := &sseWriter{writer: tw, buf: make([]byte, 0, 4096)}

	sw.Write([]byte("event: message\n"))
	sw.Write([]byte("id: 1\n"))

	if sw.content.String() != "" {
		t.Error("should ignore non-data lines")
	}
}

// =============================================================================
// NewRouter / NewForwarder Tests
// =============================================================================

func TestNewRouter(t *testing.T) {
	store := &mockStore{}
	r := NewRouter(store)
	if r == nil {
		t.Fatal("expected non-nil router")
	}
	if r.store != store {
		t.Error("router should reference the given store")
	}
}

func TestRouter_NoProviders(t *testing.T) {
	store := &mockStore{providers: []models.Provider{}}
	r := NewRouter(store)

	_, err := r.FindProvider("gpt-4")
	if err == nil {
		t.Fatal("expected error when no providers loaded")
	}
}

func TestRouter_Refresh_StoreError(t *testing.T) {
	store := &mockStore{err: fmt.Errorf("db error")}
	r := NewRouter(store)

	err := r.Refresh()
	if err == nil {
		t.Fatal("expected error from store")
	}
}

func TestRouter_ExactBeforePrefix(t *testing.T) {
	store := &mockStore{providers: []models.Provider{
		{ID: 1, Name: "fallback", Models: []models.ModelConfig{{Name: "gpt-4-turbo"}}},
		{ID: 2, Name: "primary", Models: []models.ModelConfig{{Name: "gpt-4"}}},
	}}
	r := NewRouter(store)
	_ = r.Refresh()

	p, err := r.FindProvider("gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "primary" {
		t.Errorf("expected 'primary', got '%s'", p.Name)
	}
}

func TestRouter_PrefixBeforeWildcard(t *testing.T) {
	store := &mockStore{providers: []models.Provider{
		{ID: 1, Name: "catch-all", Models: []models.ModelConfig{{Name: "*"}}},
		{ID: 2, Name: "specific", Models: []models.ModelConfig{{Name: "gpt-4"}}},
	}}
	r := NewRouter(store)
	_ = r.Refresh()

	p, err := r.FindProvider("gpt-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "specific" {
		t.Errorf("expected 'specific' (exact match before wildcard), got '%s'", p.Name)
	}
}

// =============================================================================
// KeySelector Tests
// =============================================================================

func TestKeySelector_Empty(t *testing.T) {
	ks := NewKeySelector(nil)
	if ks.Current() != "" {
		t.Error("expected empty string for nil keys")
	}
	if ks.HasNext() {
		t.Error("expected no next for empty keys")
	}
	if ks.Len() != 0 {
		t.Error("expected len 0 for empty keys")
	}
}

func TestKeySelector_SingleKey(t *testing.T) {
	ks := NewKeySelector([]models.ProviderKey{
		{KeyValue: "sk-test-1", IsActive: true},
	})
	if ks.Current() != "sk-test-1" {
		t.Errorf("expected 'sk-test-1', got '%s'", ks.Current())
	}
	if ks.HasNext() {
		t.Error("expected no next for single key")
	}
	if ks.Len() != 1 {
		t.Errorf("expected len 1, got %d", ks.Len())
	}
	if ks.Index() != 0 {
		t.Errorf("expected index 0, got %d", ks.Index())
	}
}

func TestKeySelector_MultipleKeys(t *testing.T) {
	ks := NewKeySelector([]models.ProviderKey{
		{KeyValue: "sk-key-1", IsActive: true},
		{KeyValue: "sk-key-2", IsActive: true},
		{KeyValue: "sk-key-3", IsActive: true},
	})
	if ks.Current() != "sk-key-1" {
		t.Errorf("expected first key, got '%s'", ks.Current())
	}
	if !ks.HasNext() {
		t.Error("expected next to be available")
	}
	if ks.Next() != "sk-key-2" {
		t.Errorf("expected second key, got '%s'", ks.Current())
	}
	if ks.Index() != 1 {
		t.Errorf("expected index 1, got %d", ks.Index())
	}
	if !ks.HasNext() {
		t.Error("expected next to still be available")
	}
	if ks.Next() != "sk-key-3" {
		t.Errorf("expected third key, got '%s'", ks.Current())
	}
	if ks.HasNext() {
		t.Error("expected no next after last key")
	}
	if ks.Next() != "" {
		t.Error("expected empty after exhaustion")
	}
}

func TestKeySelector_FiltersInactive(t *testing.T) {
	ks := NewKeySelector([]models.ProviderKey{
		{KeyValue: "sk-key-1", IsActive: false},
		{KeyValue: "sk-key-2", IsActive: true},
	})
	if ks.Len() != 1 {
		t.Errorf("expected 1 active key, got %d", ks.Len())
	}
	if ks.Current() != "sk-key-2" {
		t.Errorf("expected active key, got '%s'", ks.Current())
	}
}

func TestKeySelector_FiltersEmptyValue(t *testing.T) {
	ks := NewKeySelector([]models.ProviderKey{
		{KeyValue: "", IsActive: true},
		{KeyValue: "sk-key-1", IsActive: true},
	})
	if ks.Len() != 1 {
		t.Errorf("expected 1 non-empty key, got %d", ks.Len())
	}
	if ks.Current() != "sk-key-1" {
		t.Errorf("expected key with value, got '%s'", ks.Current())
	}
}
