package server

// storeRelayKey saves the relay_key for a provider's in-progress cloud OAuth flow.
func (s *Server) storeRelayKey(provider, relayKey string) {
	s.relayKeysMu.Lock()
	defer s.relayKeysMu.Unlock()
	s.relayKeys[provider] = relayKey
}

// claimRelayKey retrieves and deletes the relay_key for provider.
// Returns ("", false) if not found.
func (s *Server) claimRelayKey(provider string) (string, bool) {
	s.relayKeysMu.Lock()
	defer s.relayKeysMu.Unlock()
	k, ok := s.relayKeys[provider]
	if ok {
		delete(s.relayKeys, provider)
	}
	return k, ok
}
