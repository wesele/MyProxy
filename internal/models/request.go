package models

import "time"

type RequestLog struct {
	ID               int64     `json:"id"`
	RequestID        string    `json:"request_id"`
	ApiKeyID         *int64    `json:"api_key_id"`
	ProviderID       *int64    `json:"provider_id"`
	Model            string    `json:"model"`
	RequestType      string    `json:"request_type"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	InputCacheTokens int       `json:"input_cache_tokens"`
	LatencyMs        int64     `json:"latency_ms"`
	StatusCode       int       `json:"status_code"`
	IsError          bool      `json:"is_error"`
	RequestSummary   string    `json:"request_summary"`
	ResponseSummary  string    `json:"response_summary"`
	CreatedAt        time.Time `json:"created_at"`
}

type StatsResponse struct {
	TotalRequests     int64                `json:"total_requests"`
	ErrorRate         float64              `json:"error_rate"`
	AvgLatencyMs      float64              `json:"avg_latency_ms"`
	P50LatencyMs      float64              `json:"p50_latency_ms"`
	P95LatencyMs      float64              `json:"p95_latency_ms"`
	P99LatencyMs      float64              `json:"p99_latency_ms"`
	TotalTokens       int64                `json:"total_tokens"`
	ModelBreakdown    map[string]int64     `json:"model_breakdown"`
	HourlyRequests    []HourlyStats        `json:"hourly_requests"`
	ModelStats        []ModelStats         `json:"model_stats"`
	HourlyByModel     []HourlyModelBucket  `json:"hourly_by_model"`
	HourlyTokensByMod []HourlyTokenBucket  `json:"hourly_tokens_by_model"`
}

type ModelStats struct {
	Model              string  `json:"model"`
	TotalRequests      int64   `json:"total_requests"`
	ErrorCount         int64   `json:"error_count"`
	ErrorRate          float64 `json:"error_rate"`
	AvgLatencyMs       float64 `json:"avg_latency_ms"`
	P50LatencyMs       float64 `json:"p50_latency_ms"`
	P95LatencyMs       float64 `json:"p95_latency_ms"`
	P99LatencyMs       float64 `json:"p99_latency_ms"`
	PromptTokens       int64   `json:"prompt_tokens"`
	CompletionTokens   int64   `json:"completion_tokens"`
	InputCacheTokens   int64   `json:"input_cache_tokens"`
	TotalTokens        int64   `json:"total_tokens"`
	AvgTokensPerSec    float64 `json:"avg_tokens_per_sec"`
	OutputToksPerSec   float64 `json:"output_toks_per_sec"`
}

type HourlyStats struct {
	Hour   string `json:"hour"`
	Count  int    `json:"count"`
	Errors int    `json:"errors"`
}

type HourlyModelBucket struct {
	Hour  string `json:"hour"`
	Model string `json:"model"`
	Count int    `json:"count"`
}

type HourlyTokenBucket struct {
	Hour           string `json:"hour"`
	Model          string `json:"model"`
	PromptTokens   int64  `json:"prompt_tokens"`
	CompletionToks int64  `json:"completion_tokens"`
	CacheTokens    int64  `json:"cache_tokens"`
}
