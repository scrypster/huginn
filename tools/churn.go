//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const wsRoot = "/tmp/huginn-workspace"

// Scenario is a named mutation applied to the workspace.
type Scenario struct {
	Name        string
	Description string
	Apply       func() error
}

var scenarios = []Scenario{
	{
		Name:        "A",
		Description: "SSN ripple: modify SSN handling in auth-service, should cascade to api-gateway",
		Apply:       scenarioA,
	},
	{
		Name:        "B",
		Description: "Contract change: modify shared-lib schema, impacts downstream services",
		Apply:       scenarioB,
	},
	{
		Name:        "C",
		Description: "Drift cross-layer: infra imports domain (architectural violation)",
		Apply:       scenarioC,
	},
	{
		Name:        "D",
		Description: "Hot file churn: rapid repeated edits to payment-service core file",
		Apply:       scenarioD,
	},
	{
		Name:        "E",
		Description: "Workspace mode: add new repo to workspace, triggers full re-index",
		Apply:       scenarioE,
	},
	{
		Name:        "F",
		Description: "Plain mode: operate outside any git repo",
		Apply:       scenarioF,
	},
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: go run tools/churn.go <scenario-letter>\n")
		fmt.Fprintf(os.Stderr, "scenarios:\n")
		for _, s := range scenarios {
			fmt.Fprintf(os.Stderr, "  %s  %s\n", s.Name, s.Description)
		}
		os.Exit(1)
	}

	letter := strings.ToUpper(os.Args[1])
	for _, s := range scenarios {
		if s.Name == letter {
			fmt.Printf("applying scenario %s: %s\n", s.Name, s.Description)
			if err := s.Apply(); err != nil {
				fmt.Fprintf(os.Stderr, "scenario %s failed: %v\n", s.Name, err)
				os.Exit(1)
			}
			fmt.Printf("scenario %s applied\n", s.Name)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "unknown scenario: %s\n", letter)
	os.Exit(1)
}

// scenarioA: Modify SSN handling file in auth-service.
// This should trigger HIGH/CRITICAL finding due to sensitive domain + SSN token.
func scenarioA() error {
	repoDir := filepath.Join(wsRoot, "auth-service")
	targetFile := filepath.Join(repoDir, "internal/auth/auth_0000.go")

	content := `package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// SSNRecord holds PII data - MODIFIED in scenario A.
type SSNRecord struct {
	SSN         string
	DateOfBirth string
	FullName    string
	// Added: employer EIN for cross-reference
	EmployerEIN string
}

// HashSSN hashes SSN using SHA-256.
func HashSSN(ssn string) string {
	h := sha256.Sum256([]byte(ssn))
	return hex.EncodeToString(h[:])
}

// ValidateSSN validates SSN format - now accepts dashes or digits.
func ValidateSSN(ssn string) error {
	clean := strings.ReplaceAll(ssn, "-", "")
	if len(clean) != 9 {
		return fmt.Errorf("invalid SSN: expected 9 digits, got %d", len(clean))
	}
	return nil
}

// LookupSSN performs a lookup (scenario A modification).
func LookupSSN(ssn string) (*SSNRecord, error) {
	if err := ValidateSSN(ssn); err != nil {
		return nil, err
	}
	// Stub implementation for stress testing
	return &SSNRecord{SSN: HashSSN(ssn)}, nil
}
`
	if err := os.WriteFile(targetFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return gitCommit(repoDir, "scenario A: modify SSN handling", targetFile)
}

// scenarioB: Modify shared-lib schema to break downstream contracts.
func scenarioB() error {
	repoDir := filepath.Join(wsRoot, "shared-lib")
	targetFile := filepath.Join(repoDir, "schema/shared_0000.go")

	content := `package schema

import "fmt"

// ContractV2 is the new contract schema - BREAKING CHANGE in scenario B.
// Previously ContractV1 had only ID and Name.
type ContractV2 struct {
	ID        string
	Name      string
	// New required fields - breaks existing consumers
	Version   int
	Signature string
	Metadata  map[string]string
}

// ValidateContract validates a contract - now requires Version >= 2.
func ValidateContract(c *ContractV2) error {
	if c.ID == "" {
		return fmt.Errorf("contract ID required")
	}
	if c.Version < 2 {
		return fmt.Errorf("contract version must be >= 2, got %d", c.Version)
	}
	return nil
}

// MigrateV1ToV2 migrates old contracts.
func MigrateV1ToV2(id, name string) *ContractV2 {
	return &ContractV2{
		ID:      id,
		Name:    name,
		Version: 2,
	}
}
`
	if err := os.WriteFile(targetFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return gitCommit(repoDir, "scenario B: breaking contract change in shared-lib schema", targetFile)
}

// scenarioC: Create a cross-layer architectural violation in infra importing domain.
func scenarioC() error {
	repoDir := filepath.Join(wsRoot, "api-gateway")
	// Create a file in infra that imports from domain (violation: infra rank 5 -> domain rank 10 is OK,
	// but cmd rank 40 importing from infra is OK; let's create cmd importing internal/security which is sensitive)
	violationFile := filepath.Join(repoDir, "internal/service/drift_violation.go")
	if err := os.MkdirAll(filepath.Dir(violationFile), 0755); err != nil {
		return err
	}

	content := `package service

import "fmt"

// DriftViolation intentionally creates an architectural violation.
// This service layer (rank 20) references auth patterns directly.
// Scenario C: cross-layer drift detection test.

// SSNServiceBridge is a DRIFT VIOLATION: service layer handling PII directly.
type SSNServiceBridge struct {
	SSN string
	DOB string
}

// ProcessPII processes PII at wrong layer (should be in domain/auth, not service).
func ProcessPII(ssn, dob string) (*SSNServiceBridge, error) {
	if ssn == "" || dob == "" {
		return nil, fmt.Errorf("SSN and DOB required")
	}
	return &SSNServiceBridge{SSN: ssn, DOB: dob}, nil
}
`
	if err := os.WriteFile(violationFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return gitCommit(repoDir, "scenario C: add cross-layer drift violation", violationFile)
}

// scenarioD: Rapid churn on payment-service core file (multiple commits).
func scenarioD() error {
	repoDir := filepath.Join(wsRoot, "payment-service")
	targetFile := filepath.Join(repoDir, "internal/service/payment_0000.go")

	for i := 0; i < 3; i++ {
		content := fmt.Sprintf(`package service

import "fmt"

// PaymentProcessor handles payment logic - churn revision %d.
type PaymentProcessor struct {
	Version int
	Retry   int
}

// Process executes a payment (revision %d).
func Process(amount float64) error {
	if amount <= 0 {
		return fmt.Errorf("invalid amount: %.2f (revision %d)", amount, %d)
	}
	return nil
}
`, i, i, i, i)
		if err := os.WriteFile(targetFile, []byte(content), 0644); err != nil {
			return fmt.Errorf("write churn %d: %w", i, err)
		}
		msg := fmt.Sprintf("scenario D: payment churn revision %d", i)
		if err := gitCommit(repoDir, msg, targetFile); err != nil {
			return err
		}
	}
	return nil
}

// scenarioE: Add a new repository to the workspace.
func scenarioE() error {
	// Add new repo to workspace
	newRepoDir := filepath.Join(wsRoot, "analytics-service")
	if err := os.MkdirAll(filepath.Join(newRepoDir, "internal/api"), 0755); err != nil {
		return err
	}

	goMod := "module github.com/huginn-test/analytics-service\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(newRepoDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return err
	}

	mainFile := filepath.Join(newRepoDir, "internal/api/analytics.go")
	content := `package api

import "fmt"

// AnalyticsEvent is a new service added in scenario E.
type AnalyticsEvent struct {
	EventType string
	UserID    string
	Timestamp int64
}

// Track records an analytics event.
func Track(event *AnalyticsEvent) error {
	if event.EventType == "" {
		return fmt.Errorf("event type required")
	}
	return nil
}
`
	if err := os.WriteFile(mainFile, []byte(content), 0644); err != nil {
		return err
	}

	// Init git repo
	for _, args := range [][]string{
		{"git", "-C", newRepoDir, "init", "-b", "main"},
		{"git", "-C", newRepoDir, "config", "user.email", "test@huginn.dev"},
		{"git", "-C", newRepoDir, "config", "user.name", "huginn-test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %w\n%s", args, err, out)
		}
	}
	if err := gitCommitAll(newRepoDir, "scenario E: initial analytics-service"); err != nil {
		return err
	}

	// Update huginn.workspace.json to include the new repo
	wsJSON := filepath.Join(wsRoot, "huginn.workspace.json")
	data, err := os.ReadFile(wsJSON)
	if err != nil {
		return err
	}
	// Insert the new repo: add comma to previous last item, then append new entry.
	// The last repo line ends with `}` before `  ],`; we need `,` after it.
	updated := strings.Replace(string(data),
		"{ \"path\": \"./infra-config\", \"tags\": [] }",
		"{ \"path\": \"./infra-config\", \"tags\": [] },\n    { \"path\": \"./analytics-service\", \"tags\": [] }",
		1)
	return os.WriteFile(wsJSON, []byte(updated), 0644)
}

// scenarioF: Create a plain directory (no git) for plain-mode testing.
func scenarioF() error {
	plainDir := "/tmp/huginn-plain"
	if err := os.MkdirAll(filepath.Join(plainDir, "src"), 0755); err != nil {
		return err
	}

	// Write some source files
	for i := 0; i < 20; i++ {
		content := fmt.Sprintf(`package src

import "fmt"

// Plain%04d is a plain-mode test function.
func Plain%04d() string {
	return fmt.Sprintf("plain-%d")
}
`, i, i, i)
		fpath := filepath.Join(plainDir, "src", fmt.Sprintf("plain_%04d.go", i))
		if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
			return fmt.Errorf("write plain file %d: %w", i, err)
		}
	}

	fmt.Printf("plain directory created at %s\n", plainDir)
	return nil
}

func gitCommit(dir, msg, file string) error {
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=huginn-test",
		"GIT_AUTHOR_EMAIL=test@huginn.dev",
		"GIT_COMMITTER_NAME=huginn-test",
		"GIT_COMMITTER_EMAIL=test@huginn.dev",
	)
	rel, err := filepath.Rel(dir, file)
	if err != nil {
		rel = file
	}
	for _, args := range [][]string{
		{"git", "-C", dir, "add", rel},
		{"git", "-C", dir, "commit", "-m", msg},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %w\n%s", args, err, out)
		}
	}
	return nil
}

func gitCommitAll(dir, msg string) error {
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=huginn-test",
		"GIT_AUTHOR_EMAIL=test@huginn.dev",
		"GIT_COMMITTER_NAME=huginn-test",
		"GIT_COMMITTER_EMAIL=test@huginn.dev",
	)
	for _, args := range [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", msg},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %w\n%s", args, err, out)
		}
	}
	return nil
}
