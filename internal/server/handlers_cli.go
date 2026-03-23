package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// cliToolStatus describes the detected state of a CLI tool.
type cliToolStatus struct {
	Name            string            `json:"name"`
	DisplayName     string            `json:"display_name"`
	Icon            string            `json:"icon"`
	IconColor       string            `json:"icon_color"`
	Description     string            `json:"description"`
	Installed       bool              `json:"installed"`
	Version         string            `json:"version,omitempty"`
	Authenticated   bool              `json:"authenticated"`
	Account         string            `json:"account,omitempty"`
	AuthHint        string            `json:"auth_hint,omitempty"`
	InstallCommands map[string]string `json:"install_commands"`
}

type cliToolDef struct {
	name            string
	displayName     string
	icon            string
	iconColor       string
	description     string
	installCommands map[string]string
	authHint        string
	detect          func() (version string, account string, authenticated bool)
}

var cliToolDefs = []cliToolDef{
	{
		name:        "gh",
		displayName: "GitHub",
		icon:        "GH",
		iconColor:   "#333333",
		description: "Repositories, pull requests, issues, and workflows",
		installCommands: map[string]string{
			"mac":     "brew install gh",
			"linux":   "sudo apt install gh  # or: https://cli.github.com/manual/installation",
			"windows": "winget install --id GitHub.cli",
		},
		authHint: "gh auth login",
		detect:   detectGH,
	},
	{
		name:        "aws",
		displayName: "AWS CLI",
		icon:        "AWS",
		iconColor:   "#FF9900",
		description: "S3, EC2, Lambda, IAM, and all AWS services",
		installCommands: map[string]string{
			"mac":     "brew install awscli",
			"linux":   "curl 'https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip' -o awscliv2.zip && unzip awscliv2.zip && sudo ./aws/install",
			"windows": "winget install -e --id Amazon.AWSCLI",
		},
		authHint: "aws configure",
		detect:   detectAWS,
	},
	{
		name:        "gcloud",
		displayName: "Google Cloud",
		icon:        "GC",
		iconColor:   "#4285F4",
		description: "Compute Engine, Cloud Storage, Cloud Functions, and all GCP services",
		installCommands: map[string]string{
			"mac":     "brew install --cask google-cloud-sdk",
			"linux":   "curl https://sdk.cloud.google.com | bash",
			"windows": "winget install -e --id Google.CloudSDK",
		},
		authHint: "gcloud auth login",
		detect:   detectGCloud,
	},
}

func (s *Server) handleCLIStatus(w http.ResponseWriter, r *http.Request) {
	results := make([]cliToolStatus, len(cliToolDefs))
	var wg sync.WaitGroup
	for i, def := range cliToolDefs {
		wg.Add(1)
		go func(idx int, d cliToolDef) {
			defer wg.Done()
			status := cliToolStatus{
				Name:            d.name,
				DisplayName:     d.displayName,
				Icon:            d.icon,
				IconColor:       d.iconColor,
				Description:     d.description,
				InstallCommands: d.installCommands,
				AuthHint:        d.authHint,
			}
			if _, err := exec.LookPath(d.name); err == nil {
				status.Installed = true
				status.Version, status.Account, status.Authenticated = d.detect()
			}
			results[idx] = status
		}(i, def)
	}
	wg.Wait()

	jsonOK(w, results)
}

// runCLI runs a CLI command with a 5-second timeout.
func runCLI(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func detectGH() (version, account string, authenticated bool) {
	if out, err := runCLI("gh", "--version"); err == nil {
		// "gh version 2.45.0 (2024-01-15)"
		if parts := strings.Fields(out); len(parts) >= 3 {
			version = parts[2]
		}
	}
	if out, err := runCLI("gh", "auth", "status"); err == nil {
		authenticated = true
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "account") {
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "account" && i+1 < len(parts) {
						account = parts[i+1]
						break
					}
				}
				break
			}
		}
	}
	return
}

func detectAWS() (version, account string, authenticated bool) {
	if out, err := runCLI("aws", "--version"); err == nil {
		// "aws-cli/2.15.3 Python/3.11.6 ..."
		if parts := strings.Fields(out); len(parts) >= 1 {
			if idx := strings.Index(parts[0], "/"); idx >= 0 {
				version = parts[0][idx+1:]
			}
		}
	}
	if out, err := runCLI("aws", "sts", "get-caller-identity", "--output", "json"); err == nil {
		var identity struct {
			Account string `json:"Account"`
			Arn     string `json:"Arn"`
		}
		if json.Unmarshal([]byte(out), &identity) == nil && identity.Account != "" {
			authenticated = true
			if identity.Arn != "" {
				parts := strings.Split(identity.Arn, "/")
				user := parts[len(parts)-1]
				account = user + " · " + identity.Account
			} else {
				account = identity.Account
			}
		}
	}
	return
}

func detectGCloud() (version, account string, authenticated bool) {
	if out, err := runCLI("gcloud", "--version"); err == nil {
		// "Google Cloud SDK 461.0.0\n..."
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "Google Cloud SDK") {
				if parts := strings.Fields(line); len(parts) >= 4 {
					version = parts[3]
				}
				break
			}
		}
	}
	if out, err := runCLI("gcloud", "auth", "list", "--format=value(account)", "--filter=status=ACTIVE"); err == nil {
		out = strings.TrimSpace(out)
		if out != "" {
			authenticated = true
			account = out
		}
	}
	return
}
