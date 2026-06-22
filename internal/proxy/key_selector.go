package proxy

import "github.com/user/qwenportal/internal/models"

type KeySelector struct {
	keys    []models.ProviderKey
	current int
}

func NewKeySelector(keys []models.ProviderKey) *KeySelector {
	active := make([]models.ProviderKey, 0, len(keys))
	for _, k := range keys {
		if k.IsActive && k.KeyValue != "" {
			active = append(active, k)
		}
	}
	return &KeySelector{keys: active, current: 0}
}

func (s *KeySelector) Current() string {
	if s.current >= len(s.keys) {
		return ""
	}
	return s.keys[s.current].KeyValue
}

func (s *KeySelector) Next() string {
	s.current++
	return s.Current()
}

func (s *KeySelector) HasNext() bool {
	return s.current+1 < len(s.keys)
}

func (s *KeySelector) Index() int {
	return s.current
}

func (s *KeySelector) Len() int {
	return len(s.keys)
}
