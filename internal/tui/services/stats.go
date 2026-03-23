package services

import (
	"github.com/scrypster/huginn/internal/stats"
)

// StatsService is the typed interface for retrieving metrics snapshots.
type StatsService interface {
	Snapshot() stats.Stats
}

// DirectStatsService implements StatsService using a *stats.Registry.
type DirectStatsService struct {
	reg *stats.Registry
}

// NewDirectStatsService wraps a *stats.Registry.
func NewDirectStatsService(reg *stats.Registry) StatsService {
	return &DirectStatsService{reg: reg}
}

func (s *DirectStatsService) Snapshot() stats.Stats {
	return s.reg.Snapshot()
}
