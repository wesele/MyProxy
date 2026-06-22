package db

import (
	"time"

	"github.com/user/qwenportal/internal/models"
)

func (s *SQLiteStore) ListProviderKeys(providerID int64) ([]models.ProviderKey, error) {
	rows, err := s.db.Query(`SELECT id, provider_id, key_value, is_active, created_at FROM provider_keys WHERE provider_id = ? ORDER BY id`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.ProviderKey
	for rows.Next() {
		var k models.ProviderKey
		if err := rows.Scan(&k.ID, &k.ProviderID, &k.KeyValue, &k.IsActive, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *SQLiteStore) CreateProviderKey(providerID int64, keyValue string) (*models.ProviderKey, error) {
	result, err := s.db.Exec(`INSERT INTO provider_keys (provider_id, key_value, is_active, created_at) VALUES (?, ?, 1, ?)`,
		providerID, keyValue, time.Now())
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &models.ProviderKey{
		ID:         id,
		ProviderID: providerID,
		KeyValue:   keyValue,
		IsActive:   true,
		CreatedAt:  time.Now(),
	}, nil
}

func (s *SQLiteStore) UpdateProviderKey(id int64, keyValue string, isActive bool) error {
	_, err := s.db.Exec(`UPDATE provider_keys SET key_value=?, is_active=? WHERE id=?`,
		keyValue, isActive, id)
	return err
}

func (s *SQLiteStore) DeleteProviderKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM provider_keys WHERE id = ?`, id)
	return err
}
