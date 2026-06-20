package db

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/user/qwenportal/internal/models"
)

func HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func GenerateKeyValue() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), len(time.Now().String()))))
	return "sk-" + hex.EncodeToString(h[:])[:40]
}

func (s *SQLiteStore) ListApiKeys() ([]models.ApiKey, error) {
	rows, err := s.db.Query(`SELECT id, name, key_prefix, key_value, key_hash, is_active, rate_limit_rpm, created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.ApiKey
	for rows.Next() {
		var k models.ApiKey
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.KeyValue, &k.KeyHash, &k.IsActive, &k.RateLimitRPM, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (s *SQLiteStore) GetApiKeyByName(name string) (*models.ApiKey, error) {
	var k models.ApiKey
	err := s.db.QueryRow(`SELECT id, name, key_prefix, key_value, key_hash, is_active, rate_limit_rpm, created_at FROM api_keys WHERE name = ?`, name).
		Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.KeyValue, &k.KeyHash, &k.IsActive, &k.RateLimitRPM, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("api key not found: %s", name)
	}
	return &k, nil
}

func (s *SQLiteStore) CreateApiKey(name string, rateLimitRPM int) (*models.ApiKey, error) {
	keyValue := GenerateKeyValue()
	keyHash := HashKey(keyValue)
	keyPrefix := keyValue[:12]

	result, err := s.db.Exec(`INSERT INTO api_keys (name, key_prefix, key_value, key_hash, is_active, rate_limit_rpm, created_at) VALUES (?, ?, ?, ?, 1, ?, ?)`,
		name, keyPrefix, keyValue, keyHash, rateLimitRPM, time.Now())
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &models.ApiKey{
		ID:           id,
		Name:         name,
		KeyPrefix:    keyPrefix,
		KeyValue:     keyValue,
		IsActive:     true,
		RateLimitRPM: rateLimitRPM,
	}, nil
}

func (s *SQLiteStore) UpdateApiKey(id int64, name string, isActive bool, rateLimitRPM int) error {
	_, err := s.db.Exec(`UPDATE api_keys SET name=?, is_active=?, rate_limit_rpm=? WHERE id=?`,
		name, isActive, rateLimitRPM, id)
	return err
}

func (s *SQLiteStore) DeleteApiKey(id int64) error {
	_, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	return err
}

func (s *SQLiteStore) VerifyApiKey(keyValue string) (*models.ApiKey, error) {
	keyHash := HashKey(keyValue)

	var k models.ApiKey
	err := s.db.QueryRow(`SELECT id, name, key_prefix, key_value, key_hash, is_active, rate_limit_rpm, created_at FROM api_keys WHERE key_hash = ? AND is_active = 1`, keyHash).
		Scan(&k.ID, &k.Name, &k.KeyPrefix, &k.KeyValue, &k.KeyHash, &k.IsActive, &k.RateLimitRPM, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid api key")
	}
	return &k, nil
}
