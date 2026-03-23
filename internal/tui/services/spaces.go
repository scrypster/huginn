package services

import (
	"github.com/scrypster/huginn/internal/spaces"
)

// SpaceService is the typed interface for space (DM/channel) management.
type SpaceService interface {
	OpenDM(agentName string) (*spaces.Space, error)
	CreateChannel(name, leadAgent string, members []string, icon, color string) (*spaces.Space, error)
	GetSpace(id string) (*spaces.Space, error)
	ListSpaces(opts spaces.ListOpts) (spaces.ListSpacesResult, error)
	UpdateSpace(id string, updates spaces.SpaceUpdates) (*spaces.Space, error)
	ArchiveSpace(id string) error
	MarkRead(spaceID string) error
	UnseenCount(spaceID string) (int, error)
	ListSessionsForSpace(spaceID string) ([]spaces.SessionRef, error)
}

// DirectSpaceService implements SpaceService using the in-process StoreInterface.
type DirectSpaceService struct {
	store spaces.StoreInterface
}

// NewDirectSpaceService wraps a spaces.StoreInterface.
func NewDirectSpaceService(store spaces.StoreInterface) SpaceService {
	return &DirectSpaceService{store: store}
}

func (s *DirectSpaceService) OpenDM(agentName string) (*spaces.Space, error) {
	return s.store.OpenDM(agentName)
}

func (s *DirectSpaceService) CreateChannel(name, leadAgent string, members []string, icon, color string) (*spaces.Space, error) {
	return s.store.CreateChannel(name, leadAgent, members, icon, color)
}

func (s *DirectSpaceService) GetSpace(id string) (*spaces.Space, error) {
	return s.store.GetSpace(id)
}

func (s *DirectSpaceService) ListSpaces(opts spaces.ListOpts) (spaces.ListSpacesResult, error) {
	return s.store.ListSpaces(opts)
}

func (s *DirectSpaceService) UpdateSpace(id string, updates spaces.SpaceUpdates) (*spaces.Space, error) {
	return s.store.UpdateSpace(id, updates)
}

func (s *DirectSpaceService) ArchiveSpace(id string) error {
	return s.store.ArchiveSpace(id)
}

func (s *DirectSpaceService) MarkRead(spaceID string) error {
	return s.store.MarkRead(spaceID)
}

func (s *DirectSpaceService) UnseenCount(spaceID string) (int, error) {
	return s.store.UnseenCount(spaceID)
}

func (s *DirectSpaceService) ListSessionsForSpace(spaceID string) ([]spaces.SessionRef, error) {
	return s.store.ListSessionsForSpace(spaceID)
}
