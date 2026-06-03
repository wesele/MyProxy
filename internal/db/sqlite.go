package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

var DB *sql.DB

func Init(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	var err error
	DB, err = sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}

	DB.SetMaxOpenConns(1)

	if err := migrate(); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	return nil
}

func migrate() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			provider_type TEXT NOT NULL DEFAULT 'openai',
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL DEFAULT '',
			models_json TEXT NOT NULL DEFAULT '[]',
			is_active INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			is_active INTEGER NOT NULL DEFAULT 1,
			rate_limit_rpm INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			api_key_id INTEGER,
			provider_id INTEGER,
			model TEXT NOT NULL DEFAULT '',
			request_type TEXT NOT NULL DEFAULT 'chat',
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			status_code INTEGER NOT NULL DEFAULT 200,
			is_error INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_created ON request_logs(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_provider ON request_logs(provider_id)`,
		`CREATE INDEX IF NOT EXISTS idx_request_logs_model ON request_logs(model)`,
	}

	for _, s := range schemas {
		if _, err := DB.Exec(s); err != nil {
			return err
		}
	}

	// Migration: add columns for extended stats
	alterStmts := []string{
		`ALTER TABLE request_logs ADD COLUMN input_cache_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE request_logs ADD COLUMN request_summary TEXT DEFAULT ''`,
		`ALTER TABLE request_logs ADD COLUMN response_summary TEXT DEFAULT ''`,
	}
	for _, s := range alterStmts {
		DB.Exec(s)
	}

	// Training records for Tools (epoch seconds to avoid datetime format issues)
	DB.Exec(`CREATE TABLE IF NOT EXISTS training_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tool TEXT NOT NULL DEFAULT 'pelvic_floor',
		started_at INTEGER NOT NULL,
		ended_at INTEGER,
		duration_seconds INTEGER NOT NULL DEFAULT 0,
		note TEXT DEFAULT ''
	)`)

	return nil
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
