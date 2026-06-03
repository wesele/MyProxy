package db

import (
	"math"
	"sort"
	"time"

	"github.com/user/qwenportal/internal/models"
)

func InsertRequestLog(log *models.RequestLog) error {
	_, err := DB.Exec(`INSERT INTO request_logs
		(request_id, api_key_id, provider_id, model, request_type,
		 prompt_tokens, completion_tokens, input_cache_tokens,
		 latency_ms, status_code, is_error,
		 request_summary, response_summary, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		log.RequestID, log.ApiKeyID, log.ProviderID, log.Model, log.RequestType,
		log.PromptTokens, log.CompletionTokens, log.InputCacheTokens,
		log.LatencyMs, log.StatusCode, log.IsError,
		log.RequestSummary, log.ResponseSummary,
		log.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func percentile(sorted []int64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(n)/100.0)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return float64(sorted[idx])
}

const noModelFilter = "model != '' AND model IS NOT NULL"

func modelClause(model string) string {
	if model == "" {
		return noModelFilter
	}
	return "model = ?"
}

func GetStats(hours int, modelFilter string) (*models.StatsResponse, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	mClause := modelClause(modelFilter)

	var stats models.StatsResponse
	stats.ModelBreakdown = make(map[string]int64)

	// Aggregate
	args := []interface{}{since}
	if modelFilter != "" {
		args = append(args, modelFilter)
	}
	err := DB.QueryRow(`SELECT
		COUNT(*),
		COALESCE(AVG(CASE WHEN is_error=0 THEN latency_ms END), 0),
		COALESCE(SUM(prompt_tokens+completion_tokens), 0)
		FROM request_logs WHERE created_at >= ? AND `+mClause, args...).
		Scan(&stats.TotalRequests, &stats.AvgLatencyMs, &stats.TotalTokens)
	if err != nil {
		return nil, err
	}

	var errorCount int64
	eargs := []interface{}{since}
	if modelFilter != "" {
		eargs = append(eargs, modelFilter)
	}
	DB.QueryRow(`SELECT COUNT(*) FROM request_logs WHERE created_at >= ? AND is_error = 1 AND `+mClause, eargs...).
		Scan(&errorCount)
	if stats.TotalRequests > 0 {
		stats.ErrorRate = float64(errorCount) / float64(stats.TotalRequests) * 100
	}

	// Per-model detailed stats
	margs := []interface{}{since}
	if modelFilter != "" {
		margs = append(margs, modelFilter)
	}
	mRows, _ := DB.Query(`
		SELECT model,
		       COUNT(*) as total,
		       SUM(CASE WHEN is_error=1 THEN 1 ELSE 0 END) as errors,
		       COALESCE(AVG(CASE WHEN is_error=0 THEN latency_ms END), 0) as avg_latency,
		       COALESCE(SUM(prompt_tokens), 0) as prompt_tok,
		       COALESCE(SUM(completion_tokens), 0) as comp_tok,
		       COALESCE(SUM(input_cache_tokens), 0) as cache_tok
		FROM request_logs
		WHERE created_at >= ? AND `+mClause+`
		GROUP BY model
		ORDER BY total DESC`, margs...)
	if mRows != nil {
		defer mRows.Close()
		for mRows.Next() {
			var ms models.ModelStats
			mRows.Scan(&ms.Model, &ms.TotalRequests, &ms.ErrorCount,
				&ms.AvgLatencyMs, &ms.PromptTokens, &ms.CompletionTokens, &ms.InputCacheTokens)
			if ms.TotalRequests > 0 {
				ms.ErrorRate = float64(ms.ErrorCount) / float64(ms.TotalRequests) * 100
			}
			ms.TotalTokens = ms.PromptTokens + ms.CompletionTokens
			stats.ModelBreakdown[ms.Model] = ms.TotalRequests
			stats.ModelStats = append(stats.ModelStats, ms)
		}
	}

	// Percentiles + throughput per model
	for i, ms := range stats.ModelStats {
		largs := []interface{}{since, ms.Model}
		lRows, _ := DB.Query(`SELECT latency_ms, prompt_tokens, completion_tokens FROM request_logs WHERE created_at >= ? AND model = ? AND is_error = 0`, largs...)
		if lRows != nil {
			var latencies []int64
			totalPrompt := int64(0)
			totalComplete := int64(0)
			totalLatSec := 0.0
			totalOutputLatSec := 0.0
			for lRows.Next() {
				var l int64
				var pt, ct int64
				lRows.Scan(&l, &pt, &ct)
				latencies = append(latencies, l)
				totalPrompt += pt
				totalComplete += ct
				latSec := float64(l) / 1000.0
				totalLatSec += latSec
				if ct > 0 {
					totalOutputLatSec += latSec
				}
			}
			lRows.Close()
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			stats.ModelStats[i].P50LatencyMs = percentile(latencies, 50)
			stats.ModelStats[i].P95LatencyMs = percentile(latencies, 95)
			stats.ModelStats[i].P99LatencyMs = percentile(latencies, 99)
			if totalLatSec > 0 {
				stats.ModelStats[i].AvgTokensPerSec = float64(totalPrompt+totalComplete) / totalLatSec
			}
			if totalOutputLatSec > 0 {
				stats.ModelStats[i].OutputToksPerSec = float64(totalComplete) / totalOutputLatSec
			}
		}
	}

	// Hourly request breakdown by model (for stacked bar)
	hargs := []interface{}{since}
	if modelFilter != "" {
		hargs = append(hargs, modelFilter)
	}
	hRows, _ := DB.Query(`
		SELECT strftime('%Y-%m-%d %H:00', substr(created_at, 1, 19)) as hour,
		       model,
		       COUNT(*) as cnt
		FROM request_logs
		WHERE created_at >= ? AND `+mClause+`
		GROUP BY hour, model
		ORDER BY hour, model
	`, hargs...)
	if hRows != nil {
		defer hRows.Close()
		for hRows.Next() {
			var hb models.HourlyModelBucket
			hRows.Scan(&hb.Hour, &hb.Model, &hb.Count)
			stats.HourlyByModel = append(stats.HourlyByModel, hb)
		}
	}

	// Hourly token breakdown by model (for stacked bar)
	targs := []interface{}{since}
	if modelFilter != "" {
		targs = append(targs, modelFilter)
	}
	tRows, _ := DB.Query(`
		SELECT strftime('%Y-%m-%d %H:00', substr(created_at, 1, 19)) as hour,
		       model,
		       COALESCE(SUM(prompt_tokens), 0) as pt,
		       COALESCE(SUM(completion_tokens), 0) as ct,
		       COALESCE(SUM(input_cache_tokens), 0) as cht
		FROM request_logs
		WHERE created_at >= ? AND `+mClause+`
		GROUP BY hour, model
		ORDER BY hour, model
	`, targs...)
	if tRows != nil {
		defer tRows.Close()
		for tRows.Next() {
			var tb models.HourlyTokenBucket
			tRows.Scan(&tb.Hour, &tb.Model, &tb.PromptTokens, &tb.CompletionToks, &tb.CacheTokens)
			stats.HourlyTokensByMod = append(stats.HourlyTokensByMod, tb)
		}
	}

	// Aggregate hourly (for backward compat / simple line)
	agArgs := []interface{}{since}
	if modelFilter != "" {
		agArgs = append(agArgs, modelFilter)
	}
	aggRows, _ := DB.Query(`
		SELECT strftime('%Y-%m-%d %H:00', substr(created_at, 1, 19)) as hour,
		       COUNT(*) as cnt,
		       SUM(CASE WHEN is_error=1 THEN 1 ELSE 0 END) as errs
		FROM request_logs
		WHERE created_at >= ? AND `+mClause+`
		GROUP BY hour
		ORDER BY hour
	`, agArgs...)
	if aggRows != nil {
		defer aggRows.Close()
		for aggRows.Next() {
			var hs models.HourlyStats
			aggRows.Scan(&hs.Hour, &hs.Count, &hs.Errors)
			stats.HourlyRequests = append(stats.HourlyRequests, hs)
		}
	}

	return &stats, nil
}

func GetModelLogs(model string, hours int, limit int) ([]models.RequestLog, error) {
	since := time.Now().Add(-time.Duration(hours) * time.Hour)

	query := `SELECT id, request_id, model, request_type,
		prompt_tokens, completion_tokens, input_cache_tokens,
		latency_ms, status_code, is_error,
		COALESCE(request_summary, ''), COALESCE(response_summary, ''),
		created_at
		FROM request_logs
		WHERE created_at >= ? AND model = ?
		ORDER BY created_at DESC LIMIT ?`

	rows, err := DB.Query(query, since, model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.RequestLog
	for rows.Next() {
		var l models.RequestLog
		rows.Scan(&l.ID, &l.RequestID, &l.Model, &l.RequestType,
			&l.PromptTokens, &l.CompletionTokens, &l.InputCacheTokens,
			&l.LatencyMs, &l.StatusCode, &l.IsError,
			&l.RequestSummary, &l.ResponseSummary,
			&l.CreatedAt)
		logs = append(logs, l)
	}
	return logs, nil
}
