package broker

// RelayResult holds the OAuth credential fields extracted from a relay JWT.
type RelayResult struct {
	Provider     string
	AccessToken  string
	RefreshToken string
	AccountLabel string
	Expiry       int64 // Unix timestamp; 0 means no expiry
}
