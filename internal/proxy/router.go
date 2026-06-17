package proxy

import (
	"fmt"
	"strings"
	"sync"

	"github.com/user/qwenportal/internal/db"
	"github.com/user/qwenportal/internal/models"
)

type Router struct {
	mu        sync.RWMutex
	providers []models.Provider
}

func NewRouter() *Router {
	return &Router{}
}

func (r *Router) Refresh() error {
	providers, err := db.ListProviders()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.providers = providers
	r.mu.Unlock()
	return nil
}

func (r *Router) FindProvider(model string) (*models.Provider, error) {
	r.mu.RLock()
	providers := r.providers
	r.mu.RUnlock()

	for i := range providers {
		p := &providers[i]
		for _, m := range p.Models {
			if m.Name == model || m.DisplayName == model {
				cp := *p
				return &cp, nil
			}
		}
	}

	var wildcardIdx int
	hasWildcard := false
	for i := range providers {
		p := &providers[i]
		for _, m := range p.Models {
			if strings.HasPrefix(model, m.Name) {
				cp := *p
				return &cp, nil
			}
			if m.Name == "*" {
				wildcardIdx = i
				hasWildcard = true
			}
		}
	}

	if hasWildcard {
		cp := providers[wildcardIdx]
		return &cp, nil
	}

	return nil, fmt.Errorf("no provider found for model: %s", model)
}
