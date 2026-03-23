//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	workspaceRoot = "/tmp/huginn-workspace"
)

// repoSpec defines a test repository configuration.
type repoSpec struct {
	name         string
	pkg          string
	fileCount    int
	hasSensitive bool
}

var repos = []repoSpec{
	{name: "api-gateway", pkg: "gateway", fileCount: 250, hasSensitive: true},
	{name: "auth-service", pkg: "auth", fileCount: 200, hasSensitive: true},
	{name: "payment-service", pkg: "payment", fileCount: 150, hasSensitive: true},
	{name: "user-service", pkg: "user", fileCount: 200, hasSensitive: false},
	{name: "notification-service", pkg: "notification", fileCount: 150, hasSensitive: false},
	{name: "shared-lib", pkg: "shared", fileCount: 300, hasSensitive: false},
	{name: "infra-config", pkg: "infra", fileCount: 250, hasSensitive: false},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen_workspace: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("workspace generated at %s\n", workspaceRoot)
}

func run() error {
	if err := os.RemoveAll(workspaceRoot); err != nil {
		return fmt.Errorf("remove workspace: %w", err)
	}
	if err := os.MkdirAll(workspaceRoot, 0755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	totalGenerated := 0
	for _, spec := range repos {
		count, err := generateRepo(workspaceRoot, spec)
		if err != nil {
			return fmt.Errorf("generate repo %s: %w", spec.name, err)
		}
		totalGenerated += count
		fmt.Printf("  %s: %d files\n", spec.name, count)
	}

	if err := writeWorkspaceJSON(workspaceRoot); err != nil {
		return fmt.Errorf("write workspace json: %w", err)
	}

	fmt.Printf("total: %d files across %d repos\n", totalGenerated, len(repos))
	return nil
}

func generateRepo(root string, spec repoSpec) (int, error) {
	repoDir := filepath.Join(root, spec.name)
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return 0, err
	}

	if err := initGitRepo(repoDir); err != nil {
		return 0, err
	}

	dirs := []string{
		"cmd", "internal/api", "internal/service", "internal/domain",
		"internal/repo", "internal/model", "pkg", "config", "schema",
	}
	if spec.hasSensitive {
		dirs = append(dirs, "internal/auth", "internal/security")
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(repoDir, d), 0755); err != nil {
			return 0, err
		}
	}

	goMod := fmt.Sprintf("module github.com/huginn-test/%s\n\ngo 1.22\n", spec.name)
	if err := os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return 0, err
	}

	count := 0
	filesPerDir := spec.fileCount / len(dirs)
	remainder := spec.fileCount % len(dirs)

	for i, dir := range dirs {
		n := filesPerDir
		if i == 0 {
			n += remainder
		}
		for j := 0; j < n; j++ {
			filename := fmt.Sprintf("%s_%04d.go", spec.pkg, count)
			fpath := filepath.Join(repoDir, dir, filename)
			content := generateGoFile(spec.pkg, dir, count, spec.hasSensitive && j < 5)
			if err := os.WriteFile(fpath, []byte(content), 0644); err != nil {
				return 0, err
			}
			count++
		}
	}

	if err := commitAll(repoDir); err != nil {
		return 0, err
	}

	return count, nil
}

func generateGoFile(pkg, dir string, idx int, sensitive bool) string {
	var sb strings.Builder
	parts := strings.Split(dir, "/")
	goPackage := parts[len(parts)-1]
	sb.WriteString(fmt.Sprintf("package %s\n\n", goPackage))

	if sensitive {
		sb.WriteString("import (\n\t\"crypto/sha256\"\n\t\"encoding/hex\"\n\t\"fmt\"\n)\n\n")
		sb.WriteString("// SSNRecord holds personally identifiable information.\ntype SSNRecord struct {\n\tSSN         string\n\tDateOfBirth string\n\tFullName    string\n}\n\n")
		sb.WriteString("// HashSSN hashes an SSN for safe storage.\nfunc HashSSN(ssn string) string {\n\th := sha256.Sum256([]byte(ssn))\n\treturn hex.EncodeToString(h[:])\n}\n\n")
		sb.WriteString("// ValidateSSN checks format.\nfunc ValidateSSN(ssn string) error {\n\tif len(ssn) != 11 {\n\t\treturn fmt.Errorf(\"invalid SSN length: %d\", len(ssn))\n\t}\n\treturn nil\n}\n\n")
	} else {
		sb.WriteString("import \"fmt\"\n\n")
	}

	funcName := fmt.Sprintf("Process%04d", idx)
	sb.WriteString(fmt.Sprintf("// %s processes item %d.\nfunc %s(input string) (string, error) {\n\tif input == \"\" {\n\t\treturn \"\", fmt.Errorf(\"empty input\")\n\t}\n\treturn input, nil\n}\n", funcName, idx, funcName))
	return sb.String()
}

func writeWorkspaceJSON(root string) error {
	var sb strings.Builder
	sb.WriteString("{\n  \"name\": \"huginn-stress-test\",\n  \"repos\": [\n")
	for i, spec := range repos {
		comma := ","
		if i == len(repos)-1 {
			comma = ""
		}
		tags := "[]"
		if spec.hasSensitive {
			tags = `["sensitive","pii"]`
		}
		sb.WriteString(fmt.Sprintf("    { \"path\": \"./%s\", \"tags\": %s }%s\n", spec.name, tags, comma))
	}
	sb.WriteString("  ],\n  \"settings\": { \"indexOnOpen\": true }\n}\n")
	return os.WriteFile(filepath.Join(root, "huginn.workspace.json"), []byte(sb.String()), 0644)
}

func initGitRepo(dir string) error {
	for _, args := range [][]string{
		{"git", "-C", dir, "init", "-b", "main"},
		{"git", "-C", dir, "config", "user.email", "test@huginn.dev"},
		{"git", "-C", dir, "config", "user.name", "huginn-test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = os.Environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %w\n%s", args, err, out)
		}
	}
	return nil
}

func commitAll(dir string) error {
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=huginn-test",
		"GIT_AUTHOR_EMAIL=test@huginn.dev",
		"GIT_COMMITTER_NAME=huginn-test",
		"GIT_COMMITTER_EMAIL=test@huginn.dev",
	)
	for _, args := range [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", "initial commit"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %w\n%s", args, err, out)
		}
	}
	return nil
}
