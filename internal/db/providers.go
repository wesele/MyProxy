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
			Name: name,
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

func ListProviders() ([]models.Provider, error) {
	rows, err := DB.Query(`SELECT ` + providerCols + ` FROM providers ORDER BY priority, name`)
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
	return providers, nil
}

func GetProvider(id int64) (*models.Provider, error) {
	var p models.Provider
	err := scanProvider(DB.QueryRow(`SELECT `+providerCols+` FROM providers WHERE id = ?`, id), &p)
	if err != nil {
		return nil, err
	}
	p.Models = unmarshalModels(p.ModelsJSON)
	return &p, nil
}

func CreateProvider(p *models.Provider) (int64, error) {
	modelsJSON := marshalModels(p.Models)
	result, err := DB.Exec(`INSERT INTO providers (name, provider_type, base_url, api_key, models_json, priority, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProviderType, p.BaseURL, p.APIKey, modelsJSON, p.Priority, time.Now(), time.Now())
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func UpdateProvider(p *models.Provider) error {
	modelsJSON := marshalModels(p.Models)
	_, err := DB.Exec(`UPDATE providers SET name=?, provider_type=?, base_url=?, api_key=?, models_json=?, priority=?, updated_at=? WHERE id=?`,
		p.Name, p.ProviderType, p.BaseURL, p.APIKey, modelsJSON, p.Priority, time.Now(), p.ID)
	return err
}

func DeleteProvider(id int64) error {
	_, err := DB.Exec(`DELETE FROM providers WHERE id = ?`, id)
	return err
}

func FindProviderByName(name string) (*models.Provider, error) {
	var p models.Provider
	err := scanProvider(DB.QueryRow(`SELECT `+providerCols+` FROM providers WHERE name = ?`, name), &p)
	if err != nil {
		return nil, err
	}
	p.Models = unmarshalModels(p.ModelsJSON)
	return &p, nil
}

func GetProviderByModel(model string) (*models.Provider, error) {
	providers, err := ListProviders()
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
