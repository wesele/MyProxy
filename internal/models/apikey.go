package models

import "time"

type ApiKey struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	KeyPrefix    string    `json:"key_prefix"`
	KeyHash      string    `json:"-"`
	KeyValue     string    `json:"key_value"`
	IsActive     bool      `json:"is_active"`
	RateLimitRPM int       `json:"rate_limit_rpm"`
	CreatedAt    time.Time `json:"created_at"`
}
