package services

import (
	"github.com/scrypster/huginn/internal/config"
)

// ConfigService is the typed interface for reading and persisting configuration.
type ConfigService interface {
	Get() config.Config
	Save(cfg config.Config) error
}

// DirectConfigService implements ConfigService using a *config.Config pointer.
type DirectConfigService struct {
	cfg *config.Config
}

// NewDirectConfigService wraps a *config.Config.
func NewDirectConfigService(cfg *config.Config) ConfigService {
	return &DirectConfigService{cfg: cfg}
}

func (s *DirectConfigService) Get() config.Config {
	return *s.cfg
}

func (s *DirectConfigService) Save(cfg config.Config) error {
	return cfg.Save()
}
