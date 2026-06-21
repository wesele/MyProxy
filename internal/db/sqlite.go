package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func New(path string) (*SQLiteStore, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(1)

	store := &SQLiteStore{db: conn}
	if err := store.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

func (s *SQLiteStore) Close() {
	if s.db != nil {
		s.db.Close()
	}
}

func (s *SQLiteStore) migrate() error {
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

	for _, schema := range schemas {
		if _, err := s.db.Exec(schema); err != nil {
			return err
		}
	}

	alterStmts := []string{
		`ALTER TABLE request_logs ADD COLUMN input_cache_tokens INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE request_logs ADD COLUMN request_summary TEXT DEFAULT ''`,
		`ALTER TABLE request_logs ADD COLUMN response_summary TEXT DEFAULT ''`,
		`ALTER TABLE request_logs ADD COLUMN api_key_name TEXT DEFAULT ''`,
	}
	for _, stmt := range alterStmts {
		s.db.Exec(stmt)
	}

	s.db.Exec(`ALTER TABLE api_keys ADD COLUMN key_value TEXT DEFAULT ''`)
	s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_name ON api_keys(name)`)

	s.db.Exec(`CREATE TABLE IF NOT EXISTS training_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tool TEXT NOT NULL DEFAULT 'pelvic_floor',
		started_at INTEGER NOT NULL,
		ended_at INTEGER,
		duration_seconds INTEGER NOT NULL DEFAULT 0,
		note TEXT DEFAULT ''
	)`)

	s.convertToUTC()

	return nil
}

func (s *SQLiteStore) convertToUTC() {
	rows, err := s.db.Query(`SELECT id, created_at FROM request_logs WHERE created_at NOT LIKE '%Z'`)
	if err != nil {
		return
	}
	defer rows.Close()

	type fix struct {
		id  int64
		utc string
	}
	var batch []fix

	for rows.Next() {
		var id int64
		var ts string
		if err := rows.Scan(&id, &ts); err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			continue
		}
		batch = append(batch, fix{id, t.UTC().Format(time.RFC3339Nano)})
	}
	rows.Close()

	for _, f := range batch {
		s.db.Exec(`UPDATE request_logs SET created_at = ? WHERE id = ?`, f.utc, f.id)
	}
}
