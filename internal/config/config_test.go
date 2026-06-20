package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("valid YAML config with custom values", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		content := `server:
  host: 127.0.0.1
  port: 9090
database:
  path: custom/path.db
logging:
  level: debug
webui:
  enabled: true
  python: python3
  port: 8081
`
		os.WriteFile(configPath, []byte(content), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Server.Host != "127.0.0.1" {
			t.Errorf("Server.Host = %q, want %q", cfg.Server.Host, "127.0.0.1")
		}
		if cfg.Server.Port != 9090 {
			t.Errorf("Server.Port = %d, want %d", cfg.Server.Port, 9090)
		}
		if cfg.Database.Path != "custom/path.db" {
			t.Errorf("Database.Path = %q, want %q", cfg.Database.Path, "custom/path.db")
		}
		if cfg.Logging.Level != "debug" {
			t.Errorf("Logging.Level = %q, want %q", cfg.Logging.Level, "debug")
		}
		if !cfg.WebUI.Enabled {
			t.Errorf("WebUI.Enabled = %v, want %v", cfg.WebUI.Enabled, true)
		}
		if cfg.WebUI.Python != "python3" {
			t.Errorf("WebUI.Python = %q, want %q", cfg.WebUI.Python, "python3")
		}
		if cfg.WebUI.Port != 8081 {
			t.Errorf("WebUI.Port = %d, want %d", cfg.WebUI.Port, 8081)
		}
	})

	t.Run("all default values are set for empty config", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		os.WriteFile(configPath, []byte(`server: {}
database: {}
logging: {}
webui: {}
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		tests := []struct {
			field string
			got   any
			want  any
		}{
			{"Server.Host", cfg.Server.Host, "0.0.0.0"},
			{"Server.Port", cfg.Server.Port, 8080},
			{"Server.IsTLSEnabled()", cfg.Server.IsTLSEnabled(), true},
			{"Server.CertFile", cfg.Server.CertFile, "data/cert.pem"},
			{"Server.KeyFile", cfg.Server.KeyFile, "data/key.pem"},
			{"Database.Path", cfg.Database.Path, "data/qwenportal.db"},
			{"Logging.Level", cfg.Logging.Level, "info"},
			{"WebUI.Python", cfg.WebUI.Python, "python"},
		}
		for _, tt := range tests {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.field, tt.got, tt.want)
			}
		}
	})

	t.Run("explicit tls disabled", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		content := `server:
  tls_enabled: false
`
		os.WriteFile(configPath, []byte(content), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Server.IsTLSEnabled() {
			t.Error("expected TLS to be disabled")
		}
	})

	t.Run("explicit tls enabled", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		content := `server:
  tls_enabled: true
`
		os.WriteFile(configPath, []byte(content), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !cfg.Server.IsTLSEnabled() {
			t.Error("expected TLS to be enabled")
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})

	t.Run("invalid YAML returns error", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		os.WriteFile(configPath, []byte("server: [[[bad: yaml: :::"), 0644)

		_, err := Load(configPath)
		if err == nil {
			t.Error("expected error for invalid YAML, got nil")
		}
	})

	t.Run("custom values override defaults", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		os.WriteFile(configPath, []byte(`server:
  host: 10.0.0.1
  port: 3000
database:
  path: override.db
logging:
  level: warn
webui:
  python: /usr/bin/python3
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Server.Host != "10.0.0.1" {
			t.Errorf("host = %q, want 10.0.0.1", cfg.Server.Host)
		}
		if cfg.Server.Port != 3000 {
			t.Errorf("port = %d, want 3000", cfg.Server.Port)
		}
		if cfg.Database.Path != "override.db" {
			t.Errorf("db path = %q, want override.db", cfg.Database.Path)
		}
		if cfg.Logging.Level != "warn" {
			t.Errorf("log level = %q, want warn", cfg.Logging.Level)
		}
		if cfg.WebUI.Python != "/usr/bin/python3" {
			t.Errorf("python = %q, want /usr/bin/python3", cfg.WebUI.Python)
		}
	})

	t.Run("partial override applies defaults for missing fields", func(t *testing.T) {
		dir := t.TempDir()
		configPath := filepath.Join(dir, "config.yaml")
		os.WriteFile(configPath, []byte(`server:
  port: 9090
`), 0644)

		cfg, err := Load(configPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Server.Port != 9090 {
			t.Errorf("Port = %d, want 9090", cfg.Server.Port)
		}
		if cfg.Server.Host != "0.0.0.0" {
			t.Errorf("Host = %q, want 0.0.0.0 (default)", cfg.Server.Host)
		}
		if cfg.Database.Path != "data/qwenportal.db" {
			t.Errorf("Database.Path = %q, want data/qwenportal.db (default)", cfg.Database.Path)
		}
	})
}
