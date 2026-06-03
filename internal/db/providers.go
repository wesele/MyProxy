package db

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/user/qwenportal/internal/models"
)

func ListProviders() ([]models.Provider, error) {
	rows, err := DB.Query(`SELECT id, name, provider_type, base_url, api_key, models_json, is_active, priority, created_at, updated_at FROM providers ORDER BY priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.Provider
	for rows.Next() {
		var p models.Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.ProviderType, &p.BaseURL, &p.APIKey, &p.ModelsJSON, &p.IsActive, &p.Priority, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(p.ModelsJSON), &p.Models)
		providers = append(providers, p)
	}
	return providers, nil
}

func ListActiveProviders() ([]models.Provider, error) {
	rows, err := DB.Query(`SELECT id, name, provider_type, base_url, api_key, models_json, is_active, priority, created_at, updated_at FROM providers WHERE is_active = 1 ORDER BY priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []models.Provider
	for rows.Next() {
		var p models.Provider
		if err := rows.Scan(&p.ID, &p.Name, &p.ProviderType, &p.BaseURL, &p.APIKey, &p.ModelsJSON, &p.IsActive, &p.Priority, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(p.ModelsJSON), &p.Models)
		providers = append(providers, p)
	}
	return providers, nil
}

func GetProvider(id int64) (*models.Provider, error) {
	var p models.Provider
	err := DB.QueryRow(`SELECT id, name, provider_type, base_url, api_key, models_json, is_active, priority, created_at, updated_at FROM providers WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.ProviderType, &p.BaseURL, &p.APIKey, &p.ModelsJSON, &p.IsActive, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(p.ModelsJSON), &p.Models)
	return &p, nil
}

func CreateProvider(p *models.Provider) (int64, error) {
	modelsJSON, _ := json.Marshal(p.Models)
	result, err := DB.Exec(`INSERT INTO providers (name, provider_type, base_url, api_key, models_json, is_active, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProviderType, p.BaseURL, p.APIKey, string(modelsJSON), p.IsActive, p.Priority, time.Now(), time.Now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateProvider(p *models.Provider) error {
	modelsJSON, _ := json.Marshal(p.Models)
	_, err := DB.Exec(`UPDATE providers SET name=?, provider_type=?, base_url=?, api_key=?, models_json=?, is_active=?, priority=?, updated_at=? WHERE id=?`,
		p.Name, p.ProviderType, p.BaseURL, p.APIKey, string(modelsJSON), p.IsActive, p.Priority, time.Now(), p.ID)
	return err
}

func DeleteProvider(id int64) error {
	_, err := DB.Exec(`DELETE FROM providers WHERE id = ?`, id)
	return err
}

func FindProviderByName(name string) (*models.Provider, error) {
	var p models.Provider
	err := DB.QueryRow(`SELECT id, name, provider_type, base_url, api_key, models_json, is_active, priority, created_at, updated_at FROM providers WHERE name = ?`, name).
		Scan(&p.ID, &p.Name, &p.ProviderType, &p.BaseURL, &p.APIKey, &p.ModelsJSON, &p.IsActive, &p.Priority, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(p.ModelsJSON), &p.Models)
	return &p, nil
}

func GetProviderByModel(model string) (*models.Provider, error) {
	providers, err := ListActiveProviders()
	if err != nil {
		return nil, err
	}

	for _, p := range providers {
		for _, m := range p.Models {
			if m == model {
				return &p, nil
			}
		}
	}

	for _, p := range providers {
		for _, m := range p.Models {
			if strings.HasPrefix(model, m) {
				return &p, nil
			}
		}
	}

	return nil, fmt.Errorf("no provider found for model: %s", model)
}
