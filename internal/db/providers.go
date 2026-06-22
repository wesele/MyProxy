package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/user/qwenportal/internal/models"
)

func unmarshalModels(jsonStr string) []models.ModelConfig {
	if jsonStr == "" || jsonStr == "[]" {
		return []models.ModelConfig{}
	}

	var configs []models.ModelConfig
	if err := json.Unmarshal([]byte(jsonStr), &configs); err == nil {
		models.EnsureModelIDs(configs)
		return configs
	}

	var names []string
	if err := json.Unmarshal([]byte(jsonStr), &names); err != nil {
		return []models.ModelConfig{}
	}

	configs = make([]models.ModelConfig, 0, len(names))
	for _, name := range names {
		configs = append(configs, models.ModelConfig{
			Name:        name,
			DisplayName: name,
		})
	}
	models.EnsureModelIDs(configs)
	return configs
}

func marshalModels(models []models.ModelConfig) string {
	data, _ := json.Marshal(models)
	return string(data)
}

const providerCols = `id, name, provider_type, base_url, api_key, models_json, priority, created_at, updated_at`

func scanProvider(row interface{ Scan(dest ...interface{}) error }, p *models.Provider) error {
	return row.Scan(&p.ID, &p.Name, &p.ProviderType, &p.BaseURL, &p.APIKey, &p.ModelsJSON, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
}

func (s *SQLiteStore) ListProviders() ([]models.Provider, error) {
	rows, err := s.db.Query(`SELECT ` + providerCols + ` FROM providers ORDER BY priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.Provider
	for rows.Next() {
		var p models.Provider
		if err := scanProvider(rows, &p); err != nil {
			return nil, err
		}
		p.Models = unmarshalModels(p.ModelsJSON)
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Batch-load all provider keys
	if len(providers) > 0 {
		ids := make([]interface{}, 0, len(providers))
		placeholders := ""
		for i, p := range providers {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			ids = append(ids, p.ID)
		}
		keyRows, err := s.db.Query(`SELECT id, provider_id, key_value, is_active, created_at FROM provider_keys WHERE provider_id IN (`+placeholders+`) ORDER BY id`, ids...)
		if err == nil {
			keyMap := make(map[int64][]models.ProviderKey)
			for keyRows.Next() {
				var k models.ProviderKey
				if err := keyRows.Scan(&k.ID, &k.ProviderID, &k.KeyValue, &k.IsActive, &k.CreatedAt); err == nil {
					keyMap[k.ProviderID] = append(keyMap[k.ProviderID], k)
				}
			}
			keyRows.Close()
			for i := range providers {
				providers[i].Keys = keyMap[providers[i].ID]
				if len(providers[i].Keys) > 0 {
					providers[i].APIKey = providers[i].Keys[0].KeyValue
				}
			}
		}
	}
	return providers, nil
}

func (s *SQLiteStore) GetProvider(id int64) (*models.Provider, error) {
	var p models.Provider
	err := scanProvider(s.db.QueryRow(`SELECT `+providerCols+` FROM providers WHERE id = ?`, id), &p)
	if err != nil {
		return nil, err
	}
	p.Models = unmarshalModels(p.ModelsJSON)
	p.Keys, _ = s.ListProviderKeys(p.ID)
	if len(p.Keys) > 0 {
		p.APIKey = p.Keys[0].KeyValue
	}
	return &p, nil
}

func (s *SQLiteStore) CreateProvider(p *models.Provider) (int64, error) {
	modelsJSON := marshalModels(p.Models)
	result, err := s.db.Exec(`INSERT INTO providers (name, provider_type, base_url, api_key, models_json, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProviderType, p.BaseURL, p.APIKey, modelsJSON, p.Priority, time.Now(), time.Now())
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()

	// If keys provided via Keys field, use those
	if len(p.Keys) > 0 {
		for _, k := range p.Keys {
			if k.KeyValue != "" {
				if _, err := s.CreateProviderKey(id, k.KeyValue); err != nil {
					return 0, err
				}
			}
		}
	} else if p.APIKey != "" {
		if _, err := s.CreateProviderKey(id, p.APIKey); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func (s *SQLiteStore) UpdateProvider(p *models.Provider) error {
	modelsJSON := marshalModels(p.Models)
	_, err := s.db.Exec(`UPDATE providers SET name=?, provider_type=?, base_url=?, api_key=?, models_json=?, priority=?, updated_at=? WHERE id=?`,
		p.Name, p.ProviderType, p.BaseURL, p.APIKey, modelsJSON, p.Priority, time.Now(), p.ID)
	if err != nil {
		return err
	}

	if len(p.Keys) > 0 {
		existing, _ := s.ListProviderKeys(p.ID)
		existingMap := make(map[int64]bool)
		for _, ek := range existing {
			existingMap[ek.ID] = true
		}
		for _, k := range p.Keys {
			if k.ID > 0 && existingMap[k.ID] {
				s.UpdateProviderKey(k.ID, k.KeyValue, k.IsActive)
				delete(existingMap, k.ID)
			} else if k.KeyValue != "" {
				s.CreateProviderKey(p.ID, k.KeyValue)
			}
		}
		for id := range existingMap {
			s.DeleteProviderKey(id)
		}
	}
	return nil
}

func (s *SQLiteStore) DeleteProvider(id int64) error {
	_, err := s.db.Exec(`DELETE FROM providers WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) FindProviderByName(name string) (*models.Provider, error) {
	var p models.Provider
	err := scanProvider(s.db.QueryRow(`SELECT `+providerCols+` FROM providers WHERE name = ?`, name), &p)
	if err != nil {
		return nil, err
	}
	p.Models = unmarshalModels(p.ModelsJSON)
	p.Keys, _ = s.ListProviderKeys(p.ID)
	if len(p.Keys) > 0 {
		p.APIKey = p.Keys[0].KeyValue
	}
	return &p, nil
}

func (s *SQLiteStore) GetProviderByModel(model string) (*models.Provider, error) {
	providers, err := s.ListProviders()
	if err != nil {
		return nil, err
	}

	for _, p := range providers {
		for _, m := range p.Models {
			if m.Name == model {
				return &p, nil
			}
		}
	}

	for _, p := range providers {
		for _, m := range p.Models {
			if strings.HasPrefix(model, m.Name) {
				return &p, nil
			}
		}
	}

	return nil, fmt.Errorf("no provider found for model: %s", model)
}
