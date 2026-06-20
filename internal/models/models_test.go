package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestProviderUnmarshalJSON(t *testing.T) {
	t.Run("models as array of objects", func(t *testing.T) {
		data := `{
			"id": 1,
			"name": "test-provider",
			"provider_type": "openai",
			"base_url": "https://api.openai.com",
			"api_key": "sk-12345",
			"models": [
				{
					"id": "m_abc",
					"name": "gpt-4",
					"display_name": "GPT-4",
					"max_tokens": 4096,
					"max_input_tokens": 8192,
					"extra_body": {"top_p": 0.9, "temperature": 0.7},
					"input_price": 0.03,
					"output_price": 0.06,
					"input_cache_price": 0.015
				}
			],
			"priority": 10
		}`
		var p Provider
		err := json.Unmarshal([]byte(data), &p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.ID != 1 {
			t.Errorf("ID = %d, want 1", p.ID)
		}
		if p.Name != "test-provider" {
			t.Errorf("Name = %q, want test-provider", p.Name)
		}
		if p.ProviderType != "openai" {
			t.Errorf("ProviderType = %q, want openai", p.ProviderType)
		}
		if p.BaseURL != "https://api.openai.com" {
			t.Errorf("BaseURL = %q, want https://api.openai.com", p.BaseURL)
		}
		if p.APIKey != "sk-12345" {
			t.Errorf("APIKey = %q, want sk-12345", p.APIKey)
		}
		if p.Priority != 10 {
			t.Errorf("Priority = %d, want 10", p.Priority)
		}
		if len(p.Models) != 1 {
			t.Fatalf("len(Models) = %d, want 1", len(p.Models))
		}
		m := p.Models[0]
		if m.ID != "m_abc" {
			t.Errorf("Model.ID = %q, want m_abc", m.ID)
		}
		if m.Name != "gpt-4" {
			t.Errorf("Model.Name = %q, want gpt-4", m.Name)
		}
		if m.DisplayName != "GPT-4" {
			t.Errorf("Model.DisplayName = %q, want GPT-4", m.DisplayName)
		}
		if m.MaxTokens != 4096 {
			t.Errorf("Model.MaxTokens = %d, want 4096", m.MaxTokens)
		}
		if m.MaxInputTokens != 8192 {
			t.Errorf("Model.MaxInputTokens = %d, want 8192", m.MaxInputTokens)
		}
		if m.InputPrice != 0.03 {
			t.Errorf("Model.InputPrice = %f, want 0.03", m.InputPrice)
		}
		if m.OutputPrice != 0.06 {
			t.Errorf("Model.OutputPrice = %f, want 0.06", m.OutputPrice)
		}
		if m.InputCachePrice != 0.015 {
			t.Errorf("Model.InputCachePrice = %f, want 0.015", m.InputCachePrice)
		}
		if m.ExtraBody == nil {
			t.Error("ExtraBody should not be nil")
		} else {
			if v, ok := m.ExtraBody["top_p"]; !ok || v != 0.9 {
				t.Errorf("ExtraBody[\"top_p\"] = %v, want 0.9", v)
			}
			if v, ok := m.ExtraBody["temperature"]; !ok || v != 0.7 {
				t.Errorf("ExtraBody[\"temperature\"] = %v, want 0.7", v)
			}
		}
	})

	t.Run("models as array of strings (legacy format)", func(t *testing.T) {
		data := `{"name":"legacy","models":["gpt-4","gpt-3.5-turbo"]}`
		var p Provider
		err := json.Unmarshal([]byte(data), &p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Models) != 2 {
			t.Fatalf("len(Models) = %d, want 2", len(p.Models))
		}
		for i, m := range p.Models {
			if m.ID == "" {
				t.Errorf("model[%d].ID should be auto-generated", i)
			}
			if m.DisplayName == "" {
				t.Errorf("model[%d].DisplayName should be set", i)
			}
			if !strings.HasPrefix(m.ID, "m_") {
				t.Errorf("model[%d].ID should start with m_, got %q", i, m.ID)
			}
		}
		if p.Models[0].Name != "gpt-4" {
			t.Errorf("model[0].Name = %q, want gpt-4", p.Models[0].Name)
		}
		if p.Models[0].DisplayName != "gpt-4" {
			t.Errorf("model[0].DisplayName = %q, want gpt-4", p.Models[0].DisplayName)
		}
		if p.Models[1].Name != "gpt-3.5-turbo" {
			t.Errorf("model[1].Name = %q, want gpt-3.5-turbo", p.Models[1].Name)
		}
		if p.Models[0].ID == p.Models[1].ID {
			t.Error("model IDs should be unique")
		}
	})

	t.Run("null models", func(t *testing.T) {
		data := `{"name":"test","models":null}`
		var p Provider
		err := json.Unmarshal([]byte(data), &p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Models == nil {
			t.Error("Models should be empty slice, not nil")
		}
		if len(p.Models) != 0 {
			t.Errorf("len(Models) = %d, want 0", len(p.Models))
		}
	})

	t.Run("missing models field", func(t *testing.T) {
		data := `{"name":"test"}`
		var p Provider
		err := json.Unmarshal([]byte(data), &p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Models == nil {
			t.Error("Models should be empty slice, not nil")
		}
		if len(p.Models) != 0 {
			t.Errorf("len(Models) = %d, want 0", len(p.Models))
		}
	})

	t.Run("empty models array", func(t *testing.T) {
		data := `{"name":"test","models":[]}`
		var p Provider
		err := json.Unmarshal([]byte(data), &p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Models == nil {
			t.Error("Models should be empty slice, not nil")
		}
		if len(p.Models) != 0 {
			t.Errorf("len(Models) = %d, want 0", len(p.Models))
		}
	})

	t.Run("generates model IDs on unmarshal", func(t *testing.T) {
		data := `{"models":[{"name":"gpt-4","display_name":"GPT-4"}]}`
		var p Provider
		err := json.Unmarshal([]byte(data), &p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.Models) != 1 {
			t.Fatalf("len(Models) = %d, want 1", len(p.Models))
		}
		if p.Models[0].ID == "" {
			t.Error("ID should be auto-generated via EnsureModelIDs")
		}
		if !strings.HasPrefix(p.Models[0].ID, "m_") {
			t.Errorf("ID should start with m_, got %q", p.Models[0].ID)
		}
	})
}

func TestModelConfig(t *testing.T) {
	t.Run("all fields populated and JSON round-trip", func(t *testing.T) {
		original := ModelConfig{
			ID:              "m_abc123",
			Name:            "gpt-4",
			DisplayName:     "GPT-4",
			MaxTokens:       4096,
			MaxInputTokens:  8192,
			ExtraBody:       map[string]any{"top_p": 0.9},
			InputPrice:      0.03,
			OutputPrice:     0.06,
			InputCachePrice: 0.015,
		}
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var restored ModelConfig
		err = json.Unmarshal(data, &restored)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if restored.ID != original.ID {
			t.Errorf("ID = %q, want %q", restored.ID, original.ID)
		}
		if restored.Name != original.Name {
			t.Errorf("Name = %q, want %q", restored.Name, original.Name)
		}
		if restored.DisplayName != original.DisplayName {
			t.Errorf("DisplayName = %q, want %q", restored.DisplayName, original.DisplayName)
		}
		if restored.MaxTokens != original.MaxTokens {
			t.Errorf("MaxTokens = %d, want %d", restored.MaxTokens, original.MaxTokens)
		}
		if restored.MaxInputTokens != original.MaxInputTokens {
			t.Errorf("MaxInputTokens = %d, want %d", restored.MaxInputTokens, original.MaxInputTokens)
		}
		if restored.InputPrice != original.InputPrice {
			t.Errorf("InputPrice = %f, want %f", restored.InputPrice, original.InputPrice)
		}
		if restored.OutputPrice != original.OutputPrice {
			t.Errorf("OutputPrice = %f, want %f", restored.OutputPrice, original.OutputPrice)
		}
		if restored.InputCachePrice != original.InputCachePrice {
			t.Errorf("InputCachePrice = %f, want %f", restored.InputCachePrice, original.InputCachePrice)
		}
		if restored.ExtraBody == nil || restored.ExtraBody["top_p"] != 0.9 {
			t.Error("ExtraBody not preserved")
		}
	})

	t.Run("zero-value fields are omitted from JSON", func(t *testing.T) {
		m := ModelConfig{Name: "test-model"}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		raw := string(data)
		if strings.Contains(raw, `"max_tokens"`) {
			t.Error("zero max_tokens should be omitted")
		}
		if strings.Contains(raw, `"id"`) {
			t.Error("empty id should be omitted")
		}
		if !strings.Contains(raw, `"name":"test-model"`) {
			t.Error("name field should be present")
		}
	})
}

func TestEnsureModelIDs(t *testing.T) {
	t.Run("generates IDs for models missing them", func(t *testing.T) {
		models := []ModelConfig{
			{Name: "gpt-4"},
			{Name: "gpt-3.5-turbo"},
		}
		EnsureModelIDs(models)

		for i, m := range models {
			if m.ID == "" {
				t.Errorf("model[%d].ID is empty", i)
			}
			if !strings.HasPrefix(m.ID, "m_") {
				t.Errorf("model[%d].ID = %q, should start with m_", i, m.ID)
			}
		}
		if models[0].ID == models[1].ID {
			t.Error("generated model IDs should be unique")
		}
	})

	t.Run("sets DisplayName from Name when DisplayName is empty", func(t *testing.T) {
		models := []ModelConfig{
			{Name: "gpt-4"},
			{Name: "gpt-3.5-turbo", DisplayName: "Custom Display"},
		}
		EnsureModelIDs(models)

		if models[0].DisplayName != "gpt-4" {
			t.Errorf("DisplayName = %q, want gpt-4", models[0].DisplayName)
		}
		if models[1].DisplayName != "Custom Display" {
			t.Errorf("DisplayName = %q, want Custom Display (kept)", models[1].DisplayName)
		}
	})

	t.Run("preserves existing IDs and DisplayNames", func(t *testing.T) {
		models := []ModelConfig{
			{ID: "existing-id", Name: "gpt-4", DisplayName: "GPT-4"},
		}
		EnsureModelIDs(models)

		if models[0].ID != "existing-id" {
			t.Errorf("ID = %q, want existing-id", models[0].ID)
		}
		if models[0].DisplayName != "GPT-4" {
			t.Errorf("DisplayName = %q, want GPT-4", models[0].DisplayName)
		}
	})

	t.Run("handles empty slice", func(t *testing.T) {
		models := []ModelConfig{}
		EnsureModelIDs(models)
		if len(models) != 0 {
			t.Error("empty slice should remain empty")
		}
	})
}

func TestApiKey(t *testing.T) {
	t.Run("struct fields marshal and unmarshal correctly", func(t *testing.T) {
		now := time.Now().Truncate(time.Second)
		k := ApiKey{
			ID:           42,
			Name:         "my-key",
			KeyPrefix:    "sk-abcd",
			KeyHash:      "sha256:abcdef123456",
			KeyValue:     "sk-abcd1234567890",
			IsActive:     true,
			RateLimitRPM: 1000,
			CreatedAt:    now,
		}

		data, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		raw := string(data)

		if strings.Contains(raw, "key_hash") {
			t.Error("key_hash should not be serialized (json:\"-\")")
		}
		if !strings.Contains(raw, `"key_value"`) {
			t.Error("key_value should be serialized when set")
		}
		if !strings.Contains(raw, `"id":42`) {
			t.Error("id should be present")
		}
		if !strings.Contains(raw, `"name":"my-key"`) {
			t.Error("name should be present")
		}
		if !strings.Contains(raw, `"key_prefix":"sk-abcd"`) {
			t.Error("key_prefix should be present")
		}
		if !strings.Contains(raw, `"is_active":true`) {
			t.Error("is_active should be present")
		}
		if !strings.Contains(raw, `"rate_limit_rpm":1000`) {
			t.Error("rate_limit_rpm should be present")
		}

		var k2 ApiKey
		err = json.Unmarshal(data, &k2)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if k2.ID != k.ID {
			t.Errorf("ID = %d, want %d", k2.ID, k.ID)
		}
		if k2.Name != k.Name {
			t.Errorf("Name = %q, want %q", k2.Name, k.Name)
		}
		if k2.KeyPrefix != k.KeyPrefix {
			t.Errorf("KeyPrefix = %q, want %q", k2.KeyPrefix, k.KeyPrefix)
		}
		if k2.KeyHash != "" {
			t.Error("KeyHash should be empty after unmarshal (not in JSON)")
		}
		if k2.KeyValue != k.KeyValue {
			t.Errorf("KeyValue = %q, want %q", k2.KeyValue, k.KeyValue)
		}
		if k2.IsActive != k.IsActive {
			t.Errorf("IsActive = %v, want %v", k2.IsActive, k.IsActive)
		}
		if k2.RateLimitRPM != k.RateLimitRPM {
			t.Errorf("RateLimitRPM = %d, want %d", k2.RateLimitRPM, k.RateLimitRPM)
		}
		if !k2.CreatedAt.Equal(k.CreatedAt) {
			t.Errorf("CreatedAt = %v, want %v", k2.CreatedAt, k.CreatedAt)
		}
	})

	t.Run("omitempty key_value when empty", func(t *testing.T) {
		k := ApiKey{
			ID:   1,
			Name: "test",
		}
		data, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		raw := string(data)
		if strings.Contains(raw, `"key_value"`) {
			t.Error("empty key_value should be omitted")
		}
	})
}

func TestRequestLog(t *testing.T) {
	t.Run("struct fields marshal and unmarshal correctly", func(t *testing.T) {
		apiKeyID := int64(10)
		providerID := int64(20)
		now := time.Now().Truncate(time.Second)
		r := RequestLog{
			ID:               100,
			RequestID:        "req-abc-123",
			ApiKeyID:         &apiKeyID,
			ProviderID:       &providerID,
			Model:            "gpt-4",
			RequestType:      "chat",
			PromptTokens:     150,
			CompletionTokens: 80,
			InputCacheTokens: 20,
			LatencyMs:        450,
			StatusCode:       200,
			IsError:          false,
			RequestSummary:   "Hello, how are you?",
			ResponseSummary:  "I am fine, thank you!",
			CreatedAt:        now,
		}

		data, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		raw := string(data)

		if !strings.Contains(raw, `"request_id":"req-abc-123"`) {
			t.Error("request_id should be present")
		}
		if !strings.Contains(raw, `"model":"gpt-4"`) {
			t.Error("model should be present")
		}
		if !strings.Contains(raw, `"prompt_tokens":150`) {
			t.Error("prompt_tokens should be present")
		}
		if !strings.Contains(raw, `"completion_tokens":80`) {
			t.Error("completion_tokens should be present")
		}
		if !strings.Contains(raw, `"latency_ms":450`) {
			t.Error("latency_ms should be present")
		}
		if !strings.Contains(raw, `"status_code":200`) {
			t.Error("status_code should be present")
		}
		if strings.Contains(raw, `"is_error":true`) {
			t.Error("is_error should be false")
		}

		var r2 RequestLog
		err = json.Unmarshal(data, &r2)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if r2.ID != r.ID {
			t.Errorf("ID = %d, want %d", r2.ID, r.ID)
		}
		if r2.RequestID != r.RequestID {
			t.Errorf("RequestID = %q, want %q", r2.RequestID, r.RequestID)
		}
		if r2.ApiKeyID == nil || *r2.ApiKeyID != *r.ApiKeyID {
			t.Errorf("ApiKeyID = %v, want %v", r2.ApiKeyID, r.ApiKeyID)
		}
		if r2.ProviderID == nil || *r2.ProviderID != *r.ProviderID {
			t.Errorf("ProviderID = %v, want %v", r2.ProviderID, r.ProviderID)
		}
		if r2.Model != r.Model {
			t.Errorf("Model = %q, want %q", r2.Model, r.Model)
		}
		if r2.RequestType != r.RequestType {
			t.Errorf("RequestType = %q, want %q", r2.RequestType, r.RequestType)
		}
		if r2.PromptTokens != r.PromptTokens {
			t.Errorf("PromptTokens = %d, want %d", r2.PromptTokens, r.PromptTokens)
		}
		if r2.CompletionTokens != r.CompletionTokens {
			t.Errorf("CompletionTokens = %d, want %d", r2.CompletionTokens, r.CompletionTokens)
		}
		if r2.LatencyMs != r.LatencyMs {
			t.Errorf("LatencyMs = %d, want %d", r2.LatencyMs, r.LatencyMs)
		}
		if r2.StatusCode != r.StatusCode {
			t.Errorf("StatusCode = %d, want %d", r2.StatusCode, r.StatusCode)
		}
		if r2.IsError != r.IsError {
			t.Errorf("IsError = %v, want %v", r2.IsError, r.IsError)
		}
		if r2.RequestSummary != r.RequestSummary {
			t.Errorf("RequestSummary = %q, want %q", r2.RequestSummary, r.RequestSummary)
		}
		if r2.ResponseSummary != r.ResponseSummary {
			t.Errorf("ResponseSummary = %q, want %q", r2.ResponseSummary, r.ResponseSummary)
		}
	})
}

func TestStatsResponse(t *testing.T) {
	t.Run("all nested types marshal and unmarshal correctly", func(t *testing.T) {
		s := StatsResponse{
			TotalRequests: 5000,
			ErrorRate:     0.025,
			AvgLatencyMs:  320.5,
			P50LatencyMs:  280.0,
			P95LatencyMs:  600.0,
			P99LatencyMs:  900.0,
			TotalTokens:   150000,
			ModelBreakdown: map[string]int64{
				"gpt-4":        2000,
				"gpt-3.5":      3000,
			},
			HourlyRequests: []HourlyStats{
				{Hour: "2024-01-01T10:00:00Z", Count: 150, Errors: 3},
				{Hour: "2024-01-01T11:00:00Z", Count: 200, Errors: 5},
			},
			ModelStats: []ModelStats{
				{
					Model:            "gpt-4",
					TotalRequests:    2000,
					ErrorCount:       10,
					ErrorRate:        0.005,
					AvgLatencyMs:     350.0,
					P50LatencyMs:     300.0,
					P95LatencyMs:     650.0,
					P99LatencyMs:     950.0,
					PromptTokens:     50000,
					CompletionTokens: 30000,
					InputCacheTokens: 5000,
					TotalTokens:      85000,
					AvgTokensPerSec:  45.0,
					OutputToksPerSec: 30.0,
				},
			},
			HourlyByModel: []HourlyModelBucket{
				{Hour: "2024-01-01T10:00:00Z", Model: "gpt-4", Count: 100},
				{Hour: "2024-01-01T10:00:00Z", Model: "gpt-3.5", Count: 50},
			},
			HourlyTokensByMod: []HourlyTokenBucket{
				{
					Hour:           "2024-01-01T10:00:00Z",
					Model:          "gpt-4",
					PromptTokens:   10000,
					CompletionToks: 5000,
					CacheTokens:    1000,
				},
			},
		}

		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var s2 StatsResponse
		err = json.Unmarshal(data, &s2)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if s2.TotalRequests != s.TotalRequests {
			t.Errorf("TotalRequests = %d, want %d", s2.TotalRequests, s.TotalRequests)
		}
		if s2.ErrorRate != s.ErrorRate {
			t.Errorf("ErrorRate = %f, want %f", s2.ErrorRate, s.ErrorRate)
		}
		if s2.AvgLatencyMs != s.AvgLatencyMs {
			t.Errorf("AvgLatencyMs = %f, want %f", s2.AvgLatencyMs, s.AvgLatencyMs)
		}
		if s2.P50LatencyMs != s.P50LatencyMs {
			t.Errorf("P50LatencyMs = %f, want %f", s2.P50LatencyMs, s.P50LatencyMs)
		}
		if s2.P95LatencyMs != s.P95LatencyMs {
			t.Errorf("P95LatencyMs = %f, want %f", s2.P95LatencyMs, s.P95LatencyMs)
		}
		if s2.P99LatencyMs != s.P99LatencyMs {
			t.Errorf("P99LatencyMs = %f, want %f", s2.P99LatencyMs, s.P99LatencyMs)
		}
		if s2.TotalTokens != s.TotalTokens {
			t.Errorf("TotalTokens = %d, want %d", s2.TotalTokens, s.TotalTokens)
		}
		if len(s2.ModelBreakdown) != 2 {
			t.Errorf("len(ModelBreakdown) = %d, want 2", len(s2.ModelBreakdown))
		}
		if s2.ModelBreakdown["gpt-4"] != 2000 {
			t.Errorf("ModelBreakdown[gpt-4] = %d, want 2000", s2.ModelBreakdown["gpt-4"])
		}
		if len(s2.HourlyRequests) != 2 {
			t.Errorf("len(HourlyRequests) = %d, want 2", len(s2.HourlyRequests))
		}
		if len(s2.ModelStats) != 1 {
			t.Errorf("len(ModelStats) = %d, want 1", len(s2.ModelStats))
		}
		if s2.ModelStats[0].Model != "gpt-4" {
			t.Errorf("ModelStats[0].Model = %q, want gpt-4", s2.ModelStats[0].Model)
		}
		if s2.ModelStats[0].TotalRequests != 2000 {
			t.Errorf("ModelStats[0].TotalRequests = %d, want 2000", s2.ModelStats[0].TotalRequests)
		}
		if len(s2.HourlyByModel) != 2 {
			t.Errorf("len(HourlyByModel) = %d, want 2", len(s2.HourlyByModel))
		}
		if len(s2.HourlyTokensByMod) != 1 {
			t.Errorf("len(HourlyTokensByMod) = %d, want 1", len(s2.HourlyTokensByMod))
		}
		if s2.HourlyTokensByMod[0].PromptTokens != 10000 {
			t.Errorf("HourlyTokensByMod[0].PromptTokens = %d, want 10000", s2.HourlyTokensByMod[0].PromptTokens)
		}
	})

	t.Run("empty StatsResponse", func(t *testing.T) {
		s := StatsResponse{}
		data, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}
		var s2 StatsResponse
		err = json.Unmarshal(data, &s2)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if s2.TotalRequests != 0 {
			t.Error("TotalRequests should be 0")
		}
	})
}
