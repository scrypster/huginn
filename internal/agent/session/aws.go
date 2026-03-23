package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// awsSTSTTL is the default TTL applied to cached STS credentials when no
// expiry is available from the response. AWS STS tokens are typically valid
// for 1 hour; we subtract 5 minutes of buffer.
const awsSTSTTL = 55 * time.Minute

// stsEntry holds a cached set of STS credential key-value pairs with expiry.
type stsEntry struct {
	creds     map[string]string
	region    string
	expiresAt time.Time
}

// STSCredentialCache caches temporary AWS STS credentials to avoid redundant
// re-fetches within a single session. Each profile has its own entry.
// The cache is safe for concurrent use.
type STSCredentialCache struct {
	mu      sync.Mutex
	entries map[string]*stsEntry // keyed by profile name
}

// NewSTSCredentialCache returns an empty STSCredentialCache.
func NewSTSCredentialCache() *STSCredentialCache {
	return &STSCredentialCache{entries: make(map[string]*stsEntry)}
}

// Get returns the cached credentials and region for profile if still valid.
// ok is false if no valid (non-expired) entry exists.
func (c *STSCredentialCache) Get(profile string) (creds map[string]string, region string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, exists := c.entries[profile]
	if !exists {
		return nil, "", false
	}
	if !time.Now().Before(e.expiresAt) {
		// Expired — evict the entry so the next caller re-fetches.
		delete(c.entries, profile)
		return nil, "", false
	}
	return e.creds, e.region, true
}

// Set stores credentials for profile. If expiration is the zero value the
// default awsSTSTTL is used instead.
func (c *STSCredentialCache) Set(profile string, creds map[string]string, region string, expiration time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	expiresAt := expiration
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(awsSTSTTL)
	}
	c.entries[profile] = &stsEntry{creds: creds, region: region, expiresAt: expiresAt}
}

// ParseAWSProfile parses INI-format AWS credentials content and returns the
// key-value pairs for the named profile. Profile "" or "default" both match [default].
// Returns an error if the profile is not found.
func ParseAWSProfile(content, profile string) (map[string]string, error) {
	if profile == "" {
		profile = "default"
	}
	target := "[" + profile + "]"
	lines := strings.Split(content, "\n")
	inSection := false
	result := make(map[string]string)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if inSection {
				break // we've left our section
			}
			if strings.EqualFold(line, target) {
				inSection = true
			}
			continue
		}
		if inSection {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	if !inSection {
		return nil, fmt.Errorf("aws profile %q not found in credentials", profile)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("aws profile %q exists but contains no keys", profile)
	}
	return result, nil
}

// WriteAWSSession populates the session directory with a scoped AWS credentials
// file containing only the allowed profile (written as [default]) and sets the
// AWS_SHARED_CREDENTIALS_FILE and AWS_CONFIG_FILE env vars on the session.
func WriteAWSSession(sess *Session, creds map[string]string, region string) error {
	awsDir := filepath.Join(sess.Dir, ".aws")
	if err := os.MkdirAll(awsDir, 0700); err != nil {
		return fmt.Errorf("create .aws dir: %w", err)
	}

	// Write credentials file (0600)
	credLines := "[default]\n"
	keys := make([]string, 0, len(creds))
	for k := range creds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		credLines += k + " = " + creds[k] + "\n"
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte(credLines), 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	// Write config file (0600) with region if available
	cfgLines := "[default]\n"
	if region != "" {
		cfgLines += "region = " + region + "\n"
	}
	cfgLines += "output = json\n"
	cfgPath := filepath.Join(awsDir, "config")
	if err := os.WriteFile(cfgPath, []byte(cfgLines), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Add env vars to session
	sess.Env = append(sess.Env,
		"AWS_SHARED_CREDENTIALS_FILE="+credPath,
		"AWS_CONFIG_FILE="+cfgPath,
	)
	return nil
}

// defaultSTSCache is the package-level STS credential cache shared across all
// SetupAWS calls within a process lifetime.
var defaultSTSCache = NewSTSCredentialCache()

// SetupAWS reads the user's ~/.aws/credentials, extracts the given profile, and
// writes a scoped credentials file into the session directory.
// If profile is "", the "default" profile is used.
//
// Temporary credentials (e.g. from STS assume-role) are cached in memory with
// expiry tracking. If the cache holds a valid (non-expired) entry for the
// profile it is reused; otherwise the credentials are re-read from disk and the
// cache is refreshed with a default TTL of 55 minutes (AWS STS tokens are
// typically valid for 1 hour). Pass the expiration time explicitly via
// SetupAWSWithExpiry when the caller knows the token's actual expiry.
func SetupAWS(sess *Session, profile string) error {
	return SetupAWSWithExpiry(sess, profile, time.Time{})
}

// SetupAWSWithExpiry is like SetupAWS but accepts an explicit expiration time
// for the credentials (e.g. from an STS AssumeRole response Expiration field).
// Pass a zero time.Time to use the default 55-minute TTL.
func SetupAWSWithExpiry(sess *Session, profile string, expiration time.Time) error {
	key := profile
	if key == "" {
		key = "default"
	}

	// Return cached credentials if still valid.
	if creds, region, ok := defaultSTSCache.Get(key); ok {
		return WriteAWSSession(sess, creds, region)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	credsPath := filepath.Join(home, ".aws", "credentials")
	content, err := os.ReadFile(credsPath)
	if err != nil {
		return fmt.Errorf("read ~/.aws/credentials: %w", err)
	}

	creds, err := ParseAWSProfile(string(content), profile)
	if err != nil {
		return err
	}

	// Try to get region from ~/.aws/config
	region := ""
	cfgPath := filepath.Join(home, ".aws", "config")
	if cfgContent, err := os.ReadFile(cfgPath); err == nil {
		// config uses [profile name] (not [name]) for non-default profiles
		configProfile := profile
		if profile != "" && profile != "default" {
			configProfile = "profile " + profile
		}
		if cfgFields, err := ParseAWSProfile(string(cfgContent), configProfile); err == nil {
			region = cfgFields["region"]
		}
	}

	// Cache the freshly-read credentials. If the caller supplied an expiration
	// (e.g. from STS AssumeRole), it is stored directly; otherwise the default
	// 55-minute TTL is applied.
	defaultSTSCache.Set(key, creds, region, expiration)

	return WriteAWSSession(sess, creds, region)
}
