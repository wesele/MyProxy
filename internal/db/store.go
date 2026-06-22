package db

import (
	"time"

	"github.com/user/qwenportal/internal/models"
)

type Store interface {
	ListProviders() ([]models.Provider, error)
	GetProvider(id int64) (*models.Provider, error)
	CreateProvider(p *models.Provider) (int64, error)
	UpdateProvider(p *models.Provider) error
	DeleteProvider(id int64) error
	FindProviderByName(name string) (*models.Provider, error)
	GetProviderByModel(model string) (*models.Provider, error)

	ListProviderKeys(providerID int64) ([]models.ProviderKey, error)
	CreateProviderKey(providerID int64, keyValue string) (*models.ProviderKey, error)
	UpdateProviderKey(id int64, keyValue string, isActive bool) error
	DeleteProviderKey(id int64) error

	ListApiKeys() ([]models.ApiKey, error)
	GetApiKeyByName(name string) (*models.ApiKey, error)
	CreateApiKey(name string, rateLimitRPM int) (*models.ApiKey, error)
	UpdateApiKey(id int64, name string, isActive bool, rateLimitRPM int) error
	UpdateApiKeyValue(id int64, keyValue string) error
	DeleteApiKey(id int64) error
	VerifyApiKey(keyValue string) (*models.ApiKey, error)

	InsertRequestLog(log *models.RequestLog) error
	GetStats(start, end time.Time, modelFilter string) (*models.StatsResponse, error)
	GetModelLogs(model string, start, end time.Time, limit int) ([]models.RequestLog, error)

	StartTraining(tool string) (int64, error)
	StopTraining(id int64) error
	GetTrainingStats(tool string, days int) (*TrainingStats, error)
	GetActiveTraining(tool string) (int64, error)

	Close()
}
