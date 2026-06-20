package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/user/qwenportal/internal/models"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func int64Ptr(v int64) *int64 {
	return &v
}

func strPtr(v string) *string {
	return &v
}

func TestProviderCRUD(t *testing.T) {
	t.Run("ListProviders/empty", func(t *testing.T) {
		store := newTestStore(t)
		providers, err := store.ListProviders()
		if err != nil {
			t.Fatalf("ListProviders() error = %v", err)
		}
		if len(providers) != 0 {
			t.Errorf("expected 0 providers, got %d", len(providers))
		}
	})

	t.Run("CreateProvider/basic", func(t *testing.T) {
		store := newTestStore(t)
		p := &models.Provider{
			Name:         "test-provider",
			ProviderType: "openai",
			BaseURL:      "https://api.openai.com/v1",
			APIKey:       "sk-test-key",
			Priority:     10,
		}
		id, err := store.CreateProvider(p)
		if err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
		if id <= 0 {
			t.Errorf("expected positive id, got %d", id)
		}

		got, err := store.GetProvider(id)
		if err != nil {
			t.Fatalf("GetProvider() error = %v", err)
		}
		if got.Name != p.Name {
			t.Errorf("Name = %s, want %s", got.Name, p.Name)
		}
		if got.ProviderType != p.ProviderType {
			t.Errorf("ProviderType = %s, want %s", got.ProviderType, p.ProviderType)
		}
		if got.BaseURL != p.BaseURL {
			t.Errorf("BaseURL = %s, want %s", got.BaseURL, p.BaseURL)
		}
		if got.APIKey != p.APIKey {
			t.Errorf("APIKey = %s, want %s", got.APIKey, p.APIKey)
		}
		if got.Priority != p.Priority {
			t.Errorf("Priority = %d, want %d", got.Priority, p.Priority)
		}
	})

	t.Run("CreateProvider/with models", func(t *testing.T) {
		store := newTestStore(t)
		p := &models.Provider{
			Name:         "model-provider",
			ProviderType: "custom",
			BaseURL:      "https://api.example.com",
			Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "GPT-4", MaxTokens: 8192},
				{Name: "gpt-3.5-turbo", DisplayName: "GPT-3.5", MaxTokens: 4096},
			},
		}
		id, err := store.CreateProvider(p)
		if err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}

		got, err := store.GetProvider(id)
		if err != nil {
			t.Fatalf("GetProvider() error = %v", err)
		}
		if len(got.Models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(got.Models))
		}
		if got.Models[0].Name != "gpt-4" {
			t.Errorf("Models[0].Name = %s, want gpt-4", got.Models[0].Name)
		}
		if got.Models[0].DisplayName != "GPT-4" {
			t.Errorf("Models[0].DisplayName = %s, want GPT-4", got.Models[0].DisplayName)
		}
		if got.Models[0].MaxTokens != 8192 {
			t.Errorf("Models[0].MaxTokens = %d, want 8192", got.Models[0].MaxTokens)
		}
		if got.Models[1].Name != "gpt-3.5-turbo" {
			t.Errorf("Models[1].Name = %s, want gpt-3.5-turbo", got.Models[1].Name)
		}

		if got.Models[0].ID == "" {
			t.Error("Models[0].ID should be auto-generated")
		}
		if got.Models[1].ID == "" {
			t.Error("Models[1].ID should be auto-generated")
		}
	})

	t.Run("ListProviders/with data", func(t *testing.T) {
		store := newTestStore(t)
		_, err := store.CreateProvider(&models.Provider{
			Name: "first", ProviderType: "openai", BaseURL: "https://a.com", Priority: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.CreateProvider(&models.Provider{
			Name: "second", ProviderType: "anthropic", BaseURL: "https://b.com", Priority: 2,
		})
		if err != nil {
			t.Fatal(err)
		}

		providers, err := store.ListProviders()
		if err != nil {
			t.Fatalf("ListProviders() error = %v", err)
		}
		if len(providers) != 2 {
			t.Fatalf("expected 2 providers, got %d", len(providers))
		}
		if providers[0].Name != "first" {
			t.Errorf("first provider Name = %s, want first", providers[0].Name)
		}
		if providers[1].Name != "second" {
			t.Errorf("second provider Name = %s, want second", providers[1].Name)
		}
	})

	t.Run("GetProvider/non-existing", func(t *testing.T) {
		store := newTestStore(t)
		_, err := store.GetProvider(99999)
		if err == nil {
			t.Error("expected error for non-existing provider")
		}
	})

	t.Run("GetProvider/existing", func(t *testing.T) {
		store := newTestStore(t)
		id, _ := store.CreateProvider(&models.Provider{
			Name: "findme", ProviderType: "openai", BaseURL: "https://x.com",
		})
		got, err := store.GetProvider(id)
		if err != nil {
			t.Fatalf("GetProvider() error = %v", err)
		}
		if got.Name != "findme" {
			t.Errorf("Name = %s, want findme", got.Name)
		}
	})

	t.Run("UpdateProvider", func(t *testing.T) {
		store := newTestStore(t)
		id, _ := store.CreateProvider(&models.Provider{
			Name: "original", ProviderType: "openai", BaseURL: "https://old.com", Priority: 1,
			Models: []models.ModelConfig{
				{Name: "old-model", DisplayName: "Old Model"},
			},
		})

		updated := &models.Provider{
			ID:           id,
			Name:         "updated",
			ProviderType: "anthropic",
			BaseURL:      "https://new.com",
			APIKey:       "new-key",
			Priority:     5,
			Models: []models.ModelConfig{
				{Name: "new-model", DisplayName: "New Model", MaxTokens: 16000},
			},
		}
		err := store.UpdateProvider(updated)
		if err != nil {
			t.Fatalf("UpdateProvider() error = %v", err)
		}

		got, _ := store.GetProvider(id)
		if got.Name != "updated" {
			t.Errorf("Name = %s, want updated", got.Name)
		}
		if got.ProviderType != "anthropic" {
			t.Errorf("ProviderType = %s, want anthropic", got.ProviderType)
		}
		if got.BaseURL != "https://new.com" {
			t.Errorf("BaseURL = %s, want https://new.com", got.BaseURL)
		}
		if got.APIKey != "new-key" {
			t.Errorf("APIKey = %s, want new-key", got.APIKey)
		}
		if got.Priority != 5 {
			t.Errorf("Priority = %d, want 5", got.Priority)
		}
		if len(got.Models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(got.Models))
		}
		if got.Models[0].Name != "new-model" {
			t.Errorf("Model name = %s, want new-model", got.Models[0].Name)
		}
	})

	t.Run("DeleteProvider", func(t *testing.T) {
		store := newTestStore(t)
		id, _ := store.CreateProvider(&models.Provider{
			Name: "to-delete", ProviderType: "openai", BaseURL: "https://d.com",
		})
		err := store.DeleteProvider(id)
		if err != nil {
			t.Fatalf("DeleteProvider() error = %v", err)
		}
		_, err = store.GetProvider(id)
		if err == nil {
			t.Error("expected error after delete")
		}
	})

	t.Run("DeleteProvider/non-existing", func(t *testing.T) {
		store := newTestStore(t)
		err := store.DeleteProvider(99999)
		if err != nil {
			t.Errorf("DeleteProvider() on non-existing should not error, got %v", err)
		}
	})

	t.Run("FindProviderByName/found", func(t *testing.T) {
		store := newTestStore(t)
		store.CreateProvider(&models.Provider{
			Name: "unique-name", ProviderType: "openai", BaseURL: "https://u.com",
		})
		got, err := store.FindProviderByName("unique-name")
		if err != nil {
			t.Fatalf("FindProviderByName() error = %v", err)
		}
		if got.Name != "unique-name" {
			t.Errorf("Name = %s, want unique-name", got.Name)
		}
	})

	t.Run("FindProviderByName/not found", func(t *testing.T) {
		store := newTestStore(t)
		_, err := store.FindProviderByName("nonexistent")
		if err == nil {
			t.Error("expected error for non-existing name")
		}
	})

	t.Run("GetProviderByModel/exact match", func(t *testing.T) {
		store := newTestStore(t)
		store.CreateProvider(&models.Provider{
			Name: "p1", ProviderType: "openai", BaseURL: "https://a.com",
			Models: []models.ModelConfig{
				{Name: "gpt-4", DisplayName: "GPT-4"},
			},
		})
		p, err := store.GetProviderByModel("gpt-4")
		if err != nil {
			t.Fatalf("GetProviderByModel() error = %v", err)
		}
		if p.Name != "p1" {
			t.Errorf("Provider name = %s, want p1", p.Name)
		}
	})

	t.Run("GetProviderByModel/prefix match", func(t *testing.T) {
		store := newTestStore(t)
		store.CreateProvider(&models.Provider{
			Name: "p2", ProviderType: "openai", BaseURL: "https://b.com",
			Models: []models.ModelConfig{
				{Name: "claude-3", DisplayName: "Claude 3"},
			},
		})
		p, err := store.GetProviderByModel("claude-3-opus")
		if err != nil {
			t.Fatalf("GetProviderByModel() error = %v", err)
		}
		if p.Name != "p2" {
			t.Errorf("Provider name = %s, want p2", p.Name)
		}
	})

	t.Run("GetProviderByModel/first exact over prefix", func(t *testing.T) {
		store := newTestStore(t)
		store.CreateProvider(&models.Provider{
			Name: "exact-match", ProviderType: "openai", BaseURL: "https://exact.com",
			Models: []models.ModelConfig{
				{Name: "llama-3", DisplayName: "Llama 3"},
			},
		})
		store.CreateProvider(&models.Provider{
			Name: "prefix-match", ProviderType: "openai", BaseURL: "https://prefix.com",
			Models: []models.ModelConfig{
				{Name: "llama-3-70b", DisplayName: "Llama 3 70B"},
			},
		})
		p, err := store.GetProviderByModel("llama-3")
		if err != nil {
			t.Fatalf("GetProviderByModel() error = %v", err)
		}
		if p.Name != "exact-match" {
			t.Errorf("Provider name = %s, want exact-match", p.Name)
		}
	})

	t.Run("GetProviderByModel/not found", func(t *testing.T) {
		store := newTestStore(t)
		_, err := store.GetProviderByModel("nonexistent-model")
		if err == nil {
			t.Error("expected error for unknown model")
		}
	})
}

func TestApiKeyCRUD(t *testing.T) {
	t.Run("ListApiKeys/empty", func(t *testing.T) {
		store := newTestStore(t)
		keys, err := store.ListApiKeys()
		if err != nil {
			t.Fatalf("ListApiKeys() error = %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(keys))
		}
	})

	t.Run("CreateApiKey", func(t *testing.T) {
		store := newTestStore(t)
		key, err := store.CreateApiKey("test-key", 60)
		if err != nil {
			t.Fatalf("CreateApiKey() error = %v", err)
		}
		if key.ID <= 0 {
			t.Errorf("expected positive id, got %d", key.ID)
		}
		if key.Name != "test-key" {
			t.Errorf("Name = %s, want test-key", key.Name)
		}
		if key.RateLimitRPM != 60 {
			t.Errorf("RateLimitRPM = %d, want 60", key.RateLimitRPM)
		}
		if !key.IsActive {
			t.Error("IsActive should be true")
		}
		if key.KeyValue == "" {
			t.Error("KeyValue should not be empty")
		}
		if !strings.HasPrefix(key.KeyValue, "sk-") {
			t.Errorf("KeyValue should start with sk-, got %s", key.KeyValue)
		}
		if len(key.KeyValue) != 43 {
			t.Errorf("KeyValue length = %d, want 43", len(key.KeyValue))
		}
		if key.KeyPrefix != key.KeyValue[:12] {
			t.Errorf("KeyPrefix = %s, want first 12 chars of KeyValue", key.KeyPrefix)
		}
		if key.KeyHash != "" {
			t.Logf("KeyHash is set: %s", key.KeyHash)
		}
	})

	t.Run("ListApiKeys/with data", func(t *testing.T) {
		store := newTestStore(t)
		store.CreateApiKey("key1", 10)
		store.CreateApiKey("key2", 20)

		keys, err := store.ListApiKeys()
		if err != nil {
			t.Fatalf("ListApiKeys() error = %v", err)
		}
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(keys))
		}
		if keys[0].Name != "key2" {
			t.Errorf("first key Name = %s, want key2 (most recent first)", keys[0].Name)
		}
		if keys[1].Name != "key1" {
			t.Errorf("second key Name = %s, want key1", keys[1].Name)
		}
	})

	t.Run("VerifyApiKey/valid", func(t *testing.T) {
		store := newTestStore(t)
		key, _ := store.CreateApiKey("valid-key", 100)
		verified, err := store.VerifyApiKey(key.KeyValue)
		if err != nil {
			t.Fatalf("VerifyApiKey() error = %v", err)
		}
		if verified.ID != key.ID {
			t.Errorf("ID = %d, want %d", verified.ID, key.ID)
		}
		if !verified.IsActive {
			t.Error("verified key should be active")
		}
	})

	t.Run("VerifyApiKey/invalid", func(t *testing.T) {
		store := newTestStore(t)
		_, err := store.VerifyApiKey("sk-nonexistentkey1234567890")
		if err == nil {
			t.Error("expected error for invalid key")
		}
	})

	t.Run("VerifyApiKey/inactive", func(t *testing.T) {
		store := newTestStore(t)
		key, _ := store.CreateApiKey("inactive-key", 50)
		store.UpdateApiKey(key.ID, key.Name, false, key.RateLimitRPM)

		_, err := store.VerifyApiKey(key.KeyValue)
		if err == nil {
			t.Error("expected error for inactive key")
		}
	})

	t.Run("UpdateApiKey", func(t *testing.T) {
		store := newTestStore(t)
		key, _ := store.CreateApiKey("original-name", 30)
		err := store.UpdateApiKey(key.ID, "updated-name", true, 120)
		if err != nil {
			t.Fatalf("UpdateApiKey() error = %v", err)
		}

		verified, _ := store.VerifyApiKey(key.KeyValue)
		if verified.Name != "updated-name" {
			t.Errorf("Name = %s, want updated-name", verified.Name)
		}
		if verified.RateLimitRPM != 120 {
			t.Errorf("RateLimitRPM = %d, want 120", verified.RateLimitRPM)
		}
	})

	t.Run("DeleteApiKey", func(t *testing.T) {
		store := newTestStore(t)
		key, _ := store.CreateApiKey("to-delete", 10)
		err := store.DeleteApiKey(key.ID)
		if err != nil {
			t.Fatalf("DeleteApiKey() error = %v", err)
		}

		keys, _ := store.ListApiKeys()
		if len(keys) != 0 {
			t.Errorf("expected 0 keys after delete, got %d", len(keys))
		}
	})
}

func TestRequestLogs(t *testing.T) {
	now := time.Now().UTC()
	windowStart := now.Add(-1 * time.Hour)
	windowEnd := now.Add(1 * time.Hour)

	t.Run("InsertRequestLog", func(t *testing.T) {
		store := newTestStore(t)
		apiKeyID := int64(1)
		providerID := int64(2)
		log := &models.RequestLog{
			RequestID:        "req-12345",
			ApiKeyID:         &apiKeyID,
			ProviderID:       &providerID,
			Model:            "gpt-4",
			RequestType:      "chat",
			PromptTokens:     100,
			CompletionTokens: 200,
			InputCacheTokens: 50,
			LatencyMs:        1500,
			StatusCode:       200,
			IsError:          false,
			RequestSummary:   "hello",
			ResponseSummary:  "hi there",
			CreatedAt:        now,
		}
		err := store.InsertRequestLog(log)
		if err != nil {
			t.Fatalf("InsertRequestLog() error = %v", err)
		}
	})

	t.Run("InsertRequestLog/error entry", func(t *testing.T) {
		store := newTestStore(t)
		log := &models.RequestLog{
			RequestID:        "req-error",
			Model:            "gpt-3.5",
			RequestType:      "chat",
			PromptTokens:     10,
			CompletionTokens: 0,
			LatencyMs:        500,
			StatusCode:       500,
			IsError:          true,
			CreatedAt:        now,
		}
		err := store.InsertRequestLog(log)
		if err != nil {
			t.Fatalf("InsertRequestLog() error = %v", err)
		}
	})

	t.Run("GetStats/empty", func(t *testing.T) {
		store := newTestStore(t)
		stats, err := store.GetStats(windowStart, windowEnd, "")
		if err != nil {
			t.Fatalf("GetStats() error = %v", err)
		}
		if stats == nil {
			t.Fatal("GetStats() returned nil")
		}
		if stats.TotalRequests != 0 {
			t.Errorf("TotalRequests = %d, want 0", stats.TotalRequests)
		}
		if stats.ModelBreakdown == nil {
			t.Error("ModelBreakdown should not be nil")
		}
	})

	t.Run("GetStats/with data", func(t *testing.T) {
		store := newTestStore(t)
		for i := 0; i < 5; i++ {
			store.InsertRequestLog(&models.RequestLog{
				RequestID:        fmt.Sprintf("req-%d", i),
				Model:            "gpt-4",
				RequestType:      "chat",
				PromptTokens:     10,
				CompletionTokens: 20,
				LatencyMs:        int64(100 * (i + 1)),
				StatusCode:       200,
				IsError:          false,
				CreatedAt:        now,
			})
		}
		for i := 0; i < 3; i++ {
			store.InsertRequestLog(&models.RequestLog{
				RequestID:        fmt.Sprintf("req-err-%d", i),
				Model:            "gpt-4",
				RequestType:      "chat",
				PromptTokens:     5,
				CompletionTokens: 0,
				LatencyMs:        200,
				StatusCode:       500,
				IsError:          true,
				CreatedAt:        now,
			})
		}

		stats, err := store.GetStats(windowStart, windowEnd, "")
		if err != nil {
			t.Fatalf("GetStats() error = %v", err)
		}
		if stats.TotalRequests != 8 {
			t.Errorf("TotalRequests = %d, want 8", stats.TotalRequests)
		}
		count, ok := stats.ModelBreakdown["gpt-4"]
		if !ok {
			t.Error("gpt-4 not found in ModelBreakdown")
		}
		if count != 8 {
			t.Errorf("ModelBreakdown[gpt-4] = %d, want 8", count)
		}
		if stats.ErrorRate <= 0 {
			t.Error("ErrorRate should be > 0")
		}
		if stats.TotalTokens != 5*30+3*5 {
			t.Errorf("TotalTokens = %d, want %d", stats.TotalTokens, 5*30+3*5)
		}
	})

	t.Run("GetStats/with model filter", func(t *testing.T) {
		store := newTestStore(t)
		store.InsertRequestLog(&models.RequestLog{
			RequestID: "req-a", Model: "gpt-4", RequestType: "chat",
			PromptTokens: 10, CompletionTokens: 20, LatencyMs: 100,
			StatusCode: 200, IsError: false, CreatedAt: now,
		})
		store.InsertRequestLog(&models.RequestLog{
			RequestID: "req-b", Model: "claude-3", RequestType: "chat",
			PromptTokens: 5, CompletionTokens: 10, LatencyMs: 200,
			StatusCode: 200, IsError: false, CreatedAt: now,
		})
		store.InsertRequestLog(&models.RequestLog{
			RequestID: "req-c", Model: "gpt-4", RequestType: "chat",
			PromptTokens: 15, CompletionTokens: 25, LatencyMs: 300,
			StatusCode: 200, IsError: true, CreatedAt: now,
		})

		stats, err := store.GetStats(windowStart, windowEnd, "gpt-4")
		if err != nil {
			t.Fatalf("GetStats() error = %v", err)
		}
		if stats.TotalRequests != 2 {
			t.Errorf("TotalRequests = %d, want 2", stats.TotalRequests)
		}
		count, ok := stats.ModelBreakdown["gpt-4"]
		if !ok {
			t.Error("gpt-4 not found in ModelBreakdown")
		}
		if count != 2 {
			t.Errorf("ModelBreakdown[gpt-4] = %d, want 2", count)
		}
		if _, ok := stats.ModelBreakdown["claude-3"]; ok {
			t.Error("claude-3 should not be in model-filtered breakdown")
		}
	})

	t.Run("GetModelLogs", func(t *testing.T) {
		store := newTestStore(t)
		store.InsertRequestLog(&models.RequestLog{
			RequestID: "log-a", Model: "gpt-4", RequestType: "chat",
			PromptTokens: 10, CompletionTokens: 20,
			LatencyMs: 100, StatusCode: 200, IsError: false,
			RequestSummary: "sum-a", ResponseSummary: "rsp-a",
			CreatedAt: now.Add(-30 * time.Minute),
		})
		store.InsertRequestLog(&models.RequestLog{
			RequestID: "log-b", Model: "gpt-4", RequestType: "completion",
			PromptTokens: 30, CompletionTokens: 40,
			LatencyMs: 200, StatusCode: 200, IsError: false,
			RequestSummary: "sum-b", ResponseSummary: "rsp-b",
			CreatedAt: now.Add(-10 * time.Minute),
		})
		store.InsertRequestLog(&models.RequestLog{
			RequestID: "log-c", Model: "claude-3", RequestType: "chat",
			PromptTokens: 5, CompletionTokens: 10,
			LatencyMs: 300, StatusCode: 200, IsError: false,
			CreatedAt: now,
		})

		logs, err := store.GetModelLogs("gpt-4", windowStart, windowEnd, 10)
		if err != nil {
			t.Fatalf("GetModelLogs() error = %v", err)
		}
		if len(logs) != 2 {
			t.Fatalf("expected 2 logs, got %d", len(logs))
		}
		if logs[0].RequestID != "log-b" {
			t.Errorf("first log RequestID = %s, want log-b (most recent first)", logs[0].RequestID)
		}
		if logs[1].RequestID != "log-a" {
			t.Errorf("second log RequestID = %s, want log-a", logs[1].RequestID)
		}
		if logs[0].RequestSummary != "sum-b" {
			t.Errorf("RequestSummary = %s, want sum-b", logs[0].RequestSummary)
		}
		if logs[0].ResponseSummary != "rsp-b" {
			t.Errorf("ResponseSummary = %s, want rsp-b", logs[0].ResponseSummary)
		}
	})

	t.Run("GetModelLogs/with limit", func(t *testing.T) {
		store := newTestStore(t)
		for i := 0; i < 5; i++ {
			store.InsertRequestLog(&models.RequestLog{
				RequestID: fmt.Sprintf("limit-%d", i), Model: "gpt-4",
				RequestType: "chat", PromptTokens: 1, CompletionTokens: 1,
				LatencyMs: 10, StatusCode: 200, IsError: false,
				CreatedAt: now.Add(time.Duration(i) * time.Minute),
			})
		}

		logs, err := store.GetModelLogs("gpt-4", windowStart, windowEnd, 3)
		if err != nil {
			t.Fatalf("GetModelLogs() error = %v", err)
		}
		if len(logs) != 3 {
			t.Errorf("expected 3 logs (limit), got %d", len(logs))
		}
	})
}

func TestTraining(t *testing.T) {
	t.Run("StartTraining", func(t *testing.T) {
		store := newTestStore(t)
		id, err := store.StartTraining("pelvic_floor")
		if err != nil {
			t.Fatalf("StartTraining() error = %v", err)
		}
		if id <= 0 {
			t.Errorf("expected positive id, got %d", id)
		}
	})

	t.Run("StopTraining", func(t *testing.T) {
		store := newTestStore(t)
		id, _ := store.StartTraining("pelvic_floor")
		time.Sleep(10 * time.Millisecond)
		err := store.StopTraining(id)
		if err != nil {
			t.Fatalf("StopTraining() error = %v", err)
		}

		activeID, err := store.GetActiveTraining("pelvic_floor")
		if err != nil {
			t.Fatalf("GetActiveTraining() error = %v", err)
		}
		if activeID > 0 {
			t.Error("expected no active training after stop")
		}
	})

	t.Run("StopTraining/non-existing", func(t *testing.T) {
		store := newTestStore(t)
		err := store.StopTraining(99999)
		if err == nil {
			t.Error("expected error for non-existing training record")
		}
	})

	t.Run("GetTrainingStats/no records", func(t *testing.T) {
		store := newTestStore(t)
		stats, err := store.GetTrainingStats("pelvic_floor", 7)
		if err != nil {
			t.Fatalf("GetTrainingStats() error = %v", err)
		}
		if stats == nil {
			t.Fatal("GetTrainingStats() returned nil")
		}
		if len(stats.Dates) != 7 {
			t.Errorf("expected 7 dates, got %d", len(stats.Dates))
		}
	})

	t.Run("GetTrainingStats/with records", func(t *testing.T) {
		store := newTestStore(t)
		id, _ := store.StartTraining("pelvic_floor")
		time.Sleep(1100 * time.Millisecond)
		store.StopTraining(id)

		stats, err := store.GetTrainingStats("pelvic_floor", 7)
		if err != nil {
			t.Fatalf("GetTrainingStats() error = %v", err)
		}
		if stats == nil {
			t.Fatal("GetTrainingStats() returned nil")
		}
		if len(stats.Dates) != 7 {
			t.Fatalf("expected 7 dates, got %d", len(stats.Dates))
		}
		today := time.Now().Format("2006-01-02")
		found := false
		for _, d := range stats.Dates {
			if d.Date == today {
				found = true
				if d.Total <= 0 {
					t.Error("Total should be > 0 for today")
				}
				if len(d.Sessions) != 1 {
					t.Errorf("expected 1 session for today, got %d", len(d.Sessions))
				} else if d.Sessions[0].Duration <= 0 {
					t.Error("Session duration should be > 0")
				}
			}
		}
		if !found {
			t.Errorf("today's date %s not found in stats dates", today)
		}
	})

	t.Run("GetActiveTraining/no active", func(t *testing.T) {
		store := newTestStore(t)
		id, err := store.GetActiveTraining("pelvic_floor")
		if err != nil {
			t.Fatalf("GetActiveTraining() error = %v", err)
		}
		if id != 0 {
			t.Errorf("expected 0 for no active training, got %d", id)
		}
	})

	t.Run("GetActiveTraining/with active", func(t *testing.T) {
		store := newTestStore(t)
		createdID, _ := store.StartTraining("pelvic_floor")
		activeID, err := store.GetActiveTraining("pelvic_floor")
		if err != nil {
			t.Fatalf("GetActiveTraining() error = %v", err)
		}
		if activeID != createdID {
			t.Errorf("active ID = %d, want %d", activeID, createdID)
		}
	})

	t.Run("StartTraining/custom tool", func(t *testing.T) {
		store := newTestStore(t)
		id, err := store.StartTraining("yoga")
		if err != nil {
			t.Fatalf("StartTraining() error = %v", err)
		}
		if id <= 0 {
			t.Error("expected positive id")
		}

		stats, _ := store.GetTrainingStats("yoga", 7)
		if len(stats.Dates) != 7 {
			t.Fatalf("expected 7 dates for yoga stats")
		}
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("CreateProvider/empty models", func(t *testing.T) {
		store := newTestStore(t)
		id, err := store.CreateProvider(&models.Provider{
			Name: "empty-models", ProviderType: "openai", BaseURL: "https://e.com",
			Models: []models.ModelConfig{},
		})
		if err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
		got, _ := store.GetProvider(id)
		if len(got.Models) != 0 {
			t.Errorf("expected 0 models, got %d", len(got.Models))
		}
	})

	t.Run("CreateProvider/nil models", func(t *testing.T) {
		store := newTestStore(t)
		id, err := store.CreateProvider(&models.Provider{
			Name: "nil-models", ProviderType: "openai", BaseURL: "https://n.com",
		})
		if err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
		got, _ := store.GetProvider(id)
		if len(got.Models) != 0 {
			t.Errorf("expected 0 models, got %d", len(got.Models))
		}
	})

	t.Run("CreateProvider/model display names and prices", func(t *testing.T) {
		store := newTestStore(t)
		p := &models.Provider{
			Name: "priced-models", ProviderType: "openai", BaseURL: "https://p.com",
			Models: []models.ModelConfig{
				{
					Name: "gpt-4", DisplayName: "GPT-4 Turbo",
					InputPrice:  0.03,
					OutputPrice: 0.06,
					MaxTokens:   128000,
				},
				{
					Name: "claude-3", DisplayName: "Claude 3 Opus",
					InputPrice:  15.00,
					OutputPrice: 75.00,
					ExtraBody:   map[string]interface{}{"top_k": 5},
				},
			},
		}
		id, err := store.CreateProvider(p)
		if err != nil {
			t.Fatalf("CreateProvider() error = %v", err)
		}
		got, _ := store.GetProvider(id)
		if len(got.Models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(got.Models))
		}
		if got.Models[0].DisplayName != "GPT-4 Turbo" {
			t.Errorf("DisplayName = %s, want GPT-4 Turbo", got.Models[0].DisplayName)
		}
		if got.Models[0].InputPrice != 0.03 {
			t.Errorf("InputPrice = %f, want 0.03", got.Models[0].InputPrice)
		}
		if got.Models[0].OutputPrice != 0.06 {
			t.Errorf("OutputPrice = %f, want 0.06", got.Models[0].OutputPrice)
		}
		if got.Models[0].MaxTokens != 128000 {
			t.Errorf("MaxTokens = %d, want 128000", got.Models[0].MaxTokens)
		}
		if got.Models[1].ExtraBody == nil {
			t.Error("ExtraBody should not be nil")
		} else if got.Models[1].ExtraBody["top_k"] != float64(5) {
			t.Errorf("ExtraBody[top_k] = %v, want 5", got.Models[1].ExtraBody["top_k"])
		}
	})

	t.Run("CreateProvider/default provider_type", func(t *testing.T) {
		store := newTestStore(t)
		id, _ := store.CreateProvider(&models.Provider{
			Name: "default-type", BaseURL: "https://d.com",
		})
		got, _ := store.GetProvider(id)
		if got.ProviderType != "" {
			t.Logf("ProviderType default = %q (depends on Go zero value)", got.ProviderType)
		}
	})

	t.Run("HashKey", func(t *testing.T) {
		result := HashKey("test-key-value")
		if result == "" {
			t.Error("HashKey returned empty string")
		}
		if len(result) != 64 {
			t.Errorf("HashKey length = %d, want 64", len(result))
		}

		result2 := HashKey("test-key-value")
		if result != result2 {
			t.Error("HashKey should be deterministic")
		}

		result3 := HashKey("different-key")
		if result == result3 {
			t.Error("HashKey should produce different hashes for different inputs")
		}
	})

	t.Run("GenerateKeyValue", func(t *testing.T) {
		key1 := GenerateKeyValue()
		time.Sleep(time.Millisecond)
		key2 := GenerateKeyValue()

		if key1 == "" {
			t.Error("GenerateKeyValue returned empty string")
		}
		if !strings.HasPrefix(key1, "sk-") {
			t.Errorf("key should start with sk-, got %s", key1)
		}
		if len(key1) != 43 {
			t.Errorf("key length = %d, want 43", len(key1))
		}
		if key1 == key2 {
			t.Error("two generated keys should be different")
		}
	})

	t.Run("InsertRequestLog/nil foreign keys", func(t *testing.T) {
		store := newTestStore(t)
		log := &models.RequestLog{
			RequestID:        "nil-fk",
			Model:            "gpt-4",
			RequestType:      "chat",
			PromptTokens:     50,
			CompletionTokens: 60,
			LatencyMs:        700,
			StatusCode:       200,
			IsError:          false,
			CreatedAt:        time.Now().UTC(),
		}
		err := store.InsertRequestLog(log)
		if err != nil {
			t.Fatalf("InsertRequestLog() with nil FKs error = %v", err)
		}
	})

	t.Run("GetStats/outside time window", func(t *testing.T) {
		store := newTestStore(t)
		now := time.Now().UTC()
		store.InsertRequestLog(&models.RequestLog{
			RequestID: "old-req", Model: "gpt-4", RequestType: "chat",
			PromptTokens: 10, CompletionTokens: 20, LatencyMs: 100,
			StatusCode: 200, IsError: false,
			CreatedAt: now.Add(-48 * time.Hour),
		})

		recentStart := now.Add(-1 * time.Hour)
		recentEnd := now.Add(1 * time.Hour)
		stats, err := store.GetStats(recentStart, recentEnd, "")
		if err != nil {
			t.Fatalf("GetStats() error = %v", err)
		}
		if stats.TotalRequests != 0 {
			t.Errorf("TotalRequests = %d, want 0 (old log outside window)", stats.TotalRequests)
		}
	})

	t.Run("CreateApiKey/various rate limits", func(t *testing.T) {
		store := newTestStore(t)
		key, err := store.CreateApiKey("rated", 0)
		if err != nil {
			t.Fatalf("CreateApiKey() error = %v", err)
		}
		if key.RateLimitRPM != 0 {
			t.Errorf("RateLimitRPM = %d, want 0", key.RateLimitRPM)
		}

		key2, _ := store.CreateApiKey("unlimited", 99999)
		if key2.RateLimitRPM != 99999 {
			t.Errorf("RateLimitRPM = %d, want 99999", key2.RateLimitRPM)
		}
	})
}
