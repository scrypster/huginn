package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/connections/broker"
)

type providerMeta struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"display_name"`
	Icon         string   `json:"icon"`
	Description  string   `json:"description"`
	Scopes       []string `json:"scopes"`
	MultiAccount bool     `json:"multi_account"`
}

// knownProviders lists the OAuth-capable providers supported by handleListProviders.
// Only providers that require a browser-based OAuth flow are listed here.
// API-key providers (Datadog, Splunk, Elastic, Grafana, PagerDuty, NewRelic, CrowdStrike,
// Terraform, ServiceNow, Notion, Linear, Stripe, GitHub App, Monday, Zapier) are not listed
// because they are configured via StoreAPIKeyConnection, not the OAuth flow.
var knownProviders = []providerMeta{
	{
		Name: "google", DisplayName: "Google", Icon: "G",
		Description:  "Gmail, Google Drive, Google Calendar, Docs, Sheets, Contacts",
		Scopes:       []string{"gmail.readonly", "gmail.send", "calendar.events", "drive.file", "documents", "spreadsheets", "contacts.readonly"},
		MultiAccount: true,
	},
	{
		Name: "github", DisplayName: "GitHub", Icon: "GH",
		Description:  "Repositories, pull requests, issues",
		Scopes:       []string{"repo", "read:user"},
		MultiAccount: false,
	},
	{
		Name: "slack", DisplayName: "Slack", Icon: "#",
		Description:  "Send messages, read channels",
		Scopes:       []string{"channels:read", "chat:write", "users:read"},
		MultiAccount: true,
	},
	{
		Name: "jira", DisplayName: "Jira", Icon: "J",
		Description:  "Issues, projects, sprints",
		Scopes:       []string{"read:jira-work", "write:jira-work"},
		MultiAccount: false,
	},
	{
		Name: "bitbucket", DisplayName: "Bitbucket", Icon: "BB",
		Description:  "Repositories, pull requests",
		Scopes:       []string{"repository", "pullrequest"},
		MultiAccount: false,
	},
}

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	if s.connStore == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	conns, err := s.connStore.List()
	if err != nil {
		jsonError(w, 500, "list connections: "+err.Error())
		return
	}
	if conns == nil {
		conns = []connections.Connection{}
	}
	jsonOK(w, conns)
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if s.connStore == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	type providerResponse struct {
		providerMeta
		Configured bool `json:"configured"`
	}
	s.mu.Lock()
	hasBroker := s.brokerClient != nil
	s.mu.Unlock()
	result := make([]providerResponse, len(knownProviders))
	for i, p := range knownProviders {
		_, localConfigured := s.connProviders[connections.Provider(p.Name)]
		result[i] = providerResponse{providerMeta: p, Configured: localConfigured || hasBroker}
	}
	jsonOK(w, result)
}

func (s *Server) handleStartOAuth(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}

	// Per-IP rate limiting for OAuth flow initiation.
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !s.oauthLimiter.allow(ip) {
		http.Error(w, `{"error":"too many OAuth flows, slow down"}`, http.StatusTooManyRequests)
		return
	}

	var body struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if body.Provider == "" {
		jsonError(w, 400, "provider is required")
		return
	}
	// Broker path: if machine is registered with HuginnCloud, use broker flow.
	s.mu.Lock()
	broker := s.brokerClient
	s.mu.Unlock()
	if broker != nil && isKnownProvider(body.Provider) {
		s.handleStartOAuthBroker(w, r, body.Provider)
		return
	}

	// Local path: require IntegrationProvider with local credentials.
	p, ok := s.connProviders[connections.Provider(body.Provider)]
	if !ok {
		jsonError(w, 400, fmt.Sprintf("provider %q not configured — register with HuginnCloud (huginn cloud register) or add client_id/client_secret to config", body.Provider))
		return
	}
	authURL, err := s.connMgr.StartOAuthFlow(p)
	if err != nil {
		jsonError(w, 500, "start oauth flow: "+err.Error())
		return
	}
	jsonOK(w, map[string]string{"auth_url": authURL})
}

func (s *Server) handleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "id is required")
		return
	}
	if err := s.connMgr.RemoveConnection(id); err != nil {
		var connErr *connections.ConnectionError
		if errors.As(err, &connErr) {
			jsonError(w, connErr.HTTPStatus(), connErr.Message)
		} else {
			jsonError(w, http.StatusInternalServerError, "remove connection: "+err.Error())
		}
		return
	}
	jsonOK(w, map[string]bool{"deleted": true})
}

// handleSetDefaultConnection promotes a connection to be the default for its provider.
//
//	PUT /api/v1/connections/{id}/default
func (s *Server) handleSetDefaultConnection(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, 400, "id is required")
		return
	}
	if err := s.connMgr.SetDefaultConnection(id); err != nil {
		var connErr *connections.ConnectionError
		if errors.As(err, &connErr) {
			jsonError(w, connErr.HTTPStatus(), connErr.Message)
		} else {
			jsonError(w, http.StatusInternalServerError, "set default: "+err.Error())
		}
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// handleSystemTools detects installed CLI tools (gh, aws, gcloud) and their auth status.
func (s *Server) handleSystemTools(w http.ResponseWriter, r *http.Request) {
	tools := []systemToolStatus{
		checkGitHub(),
		checkAWS(),
		checkGCloud(),
	}
	jsonOK(w, tools)
}

type systemToolStatus struct {
	Name      string   `json:"name"`
	Installed bool     `json:"installed"`
	Authed    bool     `json:"authed"`
	Identity  string   `json:"identity"`
	Profiles  []string `json:"profiles"`
	Error     string   `json:"error,omitempty"`
}

func checkGitHub() systemToolStatus {
	s := systemToolStatus{Name: "github"}
	if _, err := exec.LookPath("gh"); err != nil {
		return s
	}
	s.Installed = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// gh auth status lists all logged-in accounts (writes to stderr)
	out, _ := exec.CommandContext(ctx, "gh", "auth", "status").CombinedOutput()
	accounts, active := parseGHAuthStatus(string(out))
	if len(accounts) > 0 {
		s.Authed = true
		s.Identity = active
		s.Profiles = accounts
	}
	return s
}

// parseGHAuthStatus parses `gh auth status` output to extract all logged-in
// accounts and which one is currently active.
// Example line patterns:
//
//	"  ✓ Logged in to github.com account USERNAME (keychain)"
//	"    - Active account: true"
func parseGHAuthStatus(output string) (accounts []string, active string) {
	seen := map[string]bool{}
	var lastAccount string
	for _, line := range strings.Split(output, "\n") {
		t := strings.TrimSpace(line)
		// Extract username from "... account USERNAME ..."
		if strings.Contains(t, "account ") && strings.Contains(t, "github.com") {
			fields := strings.Fields(t)
			for i, f := range fields {
				if f == "account" && i+1 < len(fields) {
					username := "@" + fields[i+1]
					// strip trailing parenthesis if attached (e.g. "USERNAME(keychain)")
					if idx := strings.Index(username, "("); idx > 0 {
						username = username[:idx]
					}
					lastAccount = username
					if !seen[username] {
						seen[username] = true
						accounts = append(accounts, username)
					}
					break
				}
			}
		}
		// Identify the active account
		if strings.Contains(t, "Active account: true") && lastAccount != "" {
			active = lastAccount
		}
	}
	return accounts, active
}

// handleGitHubSwitch runs `gh auth switch --user <username>` to set the active gh account.
func (s *Server) handleGitHubSwitch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		User string `json:"user"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	// Strip @ prefix if present
	user := strings.TrimPrefix(body.User, "@")
	if user == "" {
		jsonError(w, 400, "user is required")
		return
	}
	if err := validateGitHubUsername(user); err != nil {
		jsonError(w, 400, "invalid username: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gh", "auth", "switch", "--user", user).CombinedOutput()
	if err != nil {
		jsonError(w, 500, "gh auth switch: "+strings.TrimSpace(string(out)))
		return
	}
	jsonOK(w, map[string]string{"active": "@" + user})
}

func checkAWS() systemToolStatus {
	s := systemToolStatus{Name: "aws"}
	if _, err := exec.LookPath("aws"); err != nil {
		return s
	}
	s.Installed = true
	s.Profiles = awsProfiles()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--query", "Arn", "--output", "text").Output()
	if err == nil && len(out) > 0 {
		s.Authed = true
		s.Identity = strings.TrimSpace(string(out))
	} else if len(s.Profiles) > 0 {
		s.Authed = true
		s.Identity = fmt.Sprintf("%d profile(s) configured", len(s.Profiles))
	}
	return s
}

func awsProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var profiles []string
	for _, filename := range []string{".aws/credentials", ".aws/config"} {
		data, err := os.ReadFile(filepath.Join(home, filename))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				name := strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")
				name = strings.TrimPrefix(name, "profile ")
				if name != "" && !seen[name] {
					seen[name] = true
					profiles = append(profiles, name)
				}
			}
		}
	}
	sort.Strings(profiles)
	return profiles
}

func checkGCloud() systemToolStatus {
	s := systemToolStatus{Name: "gcloud"}
	if _, err := exec.LookPath("gcloud"); err != nil {
		return s
	}
	s.Installed = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "auth", "list", "--filter=status:ACTIVE", "--format=value(account)").Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		s.Authed = true
		s.Identity = strings.TrimSpace(string(out))
	}
	ctxC, cancelC := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelC()
	outC, errC := exec.CommandContext(ctxC, "gcloud", "config", "configurations", "list", "--format=value(name)").Output()
	if errC == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(outC)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				s.Profiles = append(s.Profiles, line)
			}
		}
	}
	return s
}

// handleOAuthRelayFromCloud handles POST /api/v1/connections/oauth/relay.
// The cloud app sends the relay JWT it received from the OAuth broker's postMessage.
// This endpoint verifies it using the stored relay_key and stores the tokens locally.
//
// Request:  {"relay_jwt": "<signed relay JWT>"}
// Response: {"ok": true}
func (s *Server) handleOAuthRelayFromCloud(w http.ResponseWriter, r *http.Request) {
	if s.connMgr == nil {
		jsonError(w, 503, "connections not configured")
		return
	}

	var body struct {
		RelayJWT string `json:"relay_jwt"`
		FlowID   string `json:"flow_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if body.RelayJWT == "" {
		jsonError(w, 400, "relay_jwt is required")
		return
	}
	if body.FlowID == "" {
		jsonError(w, 400, "flow_id is required")
		return
	}

	// Look up the relay_key by flow ID. Using the opaque flow ID (returned by
	// startOAuthViaCloudBroker) as the map key prevents concurrent flows for
	// the same provider from colliding.
	relayKey, ok := s.claimRelayKey(body.FlowID)
	if !ok {
		jsonError(w, 400, "no pending OAuth flow for this flow_id")
		return
	}

	// Verify and parse the relay JWT using the stored relay_key.
	result, err := broker.ParseRelayJWT(body.RelayJWT, relayKey)
	if err != nil {
		// Re-store the relay_key since validation failed — the flow may retry.
		s.storeRelayKey(body.FlowID, relayKey)
		jsonError(w, 401, "invalid relay_jwt: "+err.Error())
		return
	}

	// Store the token in the OS Keychain via connMgr.
	oauthTok := result.ToOAuthToken()
	if err := s.connMgr.StoreExternalToken(r.Context(), connections.Provider(result.Provider), oauthTok, result.AccountLabel); err != nil {
		jsonError(w, 500, "store token: "+err.Error())
		return
	}

	jsonOK(w, map[string]bool{"ok": true})
}

// handleOAuthCallback completes an OAuth flow.
// NO auth middleware — this is a public endpoint called by the OAuth provider.
// Redirects to the frontend SPA using hash routing (/#/connections).
// All user-controlled values are URL-encoded before being placed into the redirect
// target to prevent open-redirect and parameter-injection attacks.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errParam := r.URL.Query().Get("error")

	if errParam != "" {
		http.Redirect(w, r, "/#/connections?error="+url.QueryEscape(errParam), http.StatusFound)
		return
	}
	if state == "" || code == "" {
		http.Redirect(w, r, "/#/connections?error=missing_params", http.StatusFound)
		return
	}
	if s.connMgr == nil {
		http.Redirect(w, r, "/#/connections?error=not_configured", http.StatusFound)
		return
	}
	conn, err := s.connMgr.HandleOAuthCallback(r.Context(), state, code)
	if err != nil {
		http.Redirect(w, r, "/#/connections?error=callback_failed", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/#/connections?connected="+url.QueryEscape(string(conn.Provider)), http.StatusFound)
}
