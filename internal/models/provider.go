package models

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

type ModelConfig struct {
	ID              string                 `json:"id,omitempty"`
	Name            string                 `json:"name"`
	DisplayName     string                 `json:"display_name,omitempty"`
	MaxTokens       int                    `json:"max_tokens,omitempty"`
	MaxInputTokens  int                    `json:"max_input_tokens,omitempty"`
	ExtraBody       map[string]interface{} `json:"extra_body,omitempty"`
	InputPrice      float64                `json:"input_price,omitempty"`
	OutputPrice     float64                `json:"output_price,omitempty"`
	InputCachePrice float64                `json:"input_cache_price,omitempty"`
	VirtualTargets  []string               `json:"virtual_targets,omitempty"`
}

func (m *ModelConfig) IsVirtual() bool {
	return len(m.VirtualTargets) > 0
}

type ProviderKey struct {
	ID         int64     `json:"id"`
	ProviderID int64     `json:"provider_id"`
	KeyValue   string    `json:"key_value,omitempty"`
	IsActive   bool      `json:"is_active"`
	CreatedAt  time.Time `json:"created_at"`
}

func generateModelID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return fmt.Sprintf("m_%x_%d", b, time.Now().UnixMilli()%100000)
}

func EnsureModelIDs(models []ModelConfig) {
	for i := range models {
		if models[i].ID == "" {
			models[i].ID = generateModelID()
		}
		if models[i].DisplayName == "" {
			models[i].DisplayName = models[i].Name
		}
	}
}

type Provider struct {
	ID           int64         `json:"id"`
	Name         string        `json:"name"`
	ProviderType string        `json:"provider_type"`
	BaseURL      string        `json:"base_url"`
	APIKey       string        `json:"api_key"`
	Keys         []ProviderKey `json:"keys"`
	ModelsJSON   string        `json:"-"`
	Models       []ModelConfig `json:"models"`
	Priority     int           `json:"priority"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

func (p *Provider) UnmarshalJSON(data []byte) error {
	type providerAlias Provider
	var alias struct {
		Models json.RawMessage `json:"models"`
		*providerAlias
	}
	alias.providerAlias = (*providerAlias)(p)
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}

	if alias.Models == nil || string(alias.Models) == "null" {
		p.Models = []ModelConfig{}
		return nil
	}

	var configs []ModelConfig
	if err := json.Unmarshal(alias.Models, &configs); err == nil {
		EnsureModelIDs(configs)
		p.Models = configs
		return nil
	}

	var names []string
	if err := json.Unmarshal(alias.Models, &names); err != nil {
		return err
	}
	p.Models = make([]ModelConfig, 0, len(names))
	for _, name := range names {
		p.Models = append(p.Models, ModelConfig{Name: name, DisplayName: name})
	}
	EnsureModelIDs(p.Models)
	return nil
}
