package services

import (
	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/pricing"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/storage"
)

// AppContext holds all shared dependencies that TUI screens need.
// Created once at startup and passed to every screen's New() constructor.
// Fields are exported so screens can read them directly.
type AppContext struct {
	Cfg           *config.Config
	Orch          *agent.Orchestrator
	Version       string
	AgentReg      *agents.AgentRegistry
	SessionStore  session.StoreInterface
	StatsReg      *stats.Registry
	Store         *storage.Store
	Idx           *repo.Index
	WorkspaceRoot string
	NotepadMgr    *notepad.Manager
	PriceTracker  *pricing.SessionTracker
	SkillReg      *skills.SkillRegistry
	MuninnEndpoint  string
	MuninnConnected bool

	// Services — typed interfaces over the raw structs above.
	// Screens should prefer these over accessing the raw fields directly.
	Agents AgentService
	Spaces SpaceService
	Config ConfigService
	Stats  StatsService
}
