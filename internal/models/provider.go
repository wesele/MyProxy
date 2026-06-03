package models

import "time"

type Provider struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	ProviderType string    `json:"provider_type"`
	BaseURL      string    `json:"base_url"`
	APIKey       string    `json:"api_key,omitempty"`
	ModelsJSON   string    `json:"-"`
	Models       []string  `json:"models"`
	IsActive     bool      `json:"is_active"`
	Priority     int       `json:"priority"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
