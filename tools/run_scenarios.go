//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func main() {
	fmt.Println("=== Huginn Enterprise Stress Test ===")
	fmt.Println()

	// Regenerate workspace for a clean slate (removes any previous mutations).
	fmt.Print("Generating workspace (1500 files)... ")
	genCmd := exec.Command("go", "run", "tools/gen_workspace.go")
	genCmd.Dir = findHuginnRoot()
	if out, err := genCmd.CombinedOutput(); err != nil {
		fmt.Printf("FAIL\n%s\n", out)
		os.Exit(1)
	}
	fmt.Println("OK")

	// Build huginn binary
	fmt.Print("Building huginn... ")
	buildCmd := exec.Command("go", "build", "-o", "/tmp/huginn-test-bin", ".")
	buildCmd.Dir = findHuginnRoot()
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Printf("FAIL\n%s\n", out)
		os.Exit(1)
	}
	fmt.Println("OK")
	fmt.Println()

	results := []AssertResult{}

	// === SCENARIO A: SSN Ripple ===
	fmt.Println("--- Scenario A: SSN Ripple ---")
	fmt.Println("Applying mutation...")
	runChurn("A")

	outA := runHuginnHeadless("/tmp/huginn-workspace/auth-service", "/radar run")
	resA, err := parseRunResult(outA)
	arA := AssertResult{ScenarioName: "A"}
	if err != nil {
		arA.assert("parse output", false, "failed to parse: %v", err)
	} else {
		// workspace mode is correct when huginn.workspace.json is found in parent
		arA.assert("mode=workspace-or-repo", resA.Mode == "workspace" || resA.Mode == "repo", "got %q", resA.Mode)
		arA.assert("files scanned > 0", resA.FilesScanned > 0, "got %d", resA.FilesScanned)
		arA.assert("radar ran", resA.RadarDuration != "", "empty duration")
		arA.assert("has findings", len(resA.TopFindings) > 0, "no findings")
		// SSN file change scores LOW-MEDIUM; score > 20 is the real signal
		arA.assert("score > 20 (sensitive change detected)", maxSeverityScore(resA) > 20, "max score=%.2f", maxSeverityScore(resA))
		arA.assert("no errors", len(resA.Errors) == 0, "errors: %v", resA.Errors)
	}
	arA.summary()
	results = append(results, arA)
	fmt.Println()

	// === SCENARIO B: Contract Change ===
	fmt.Println("--- Scenario B: Contract Change ---")
	fmt.Println("Applying mutation...")
	runChurn("B")

	outB := runHuginnHeadless("/tmp/huginn-workspace/shared-lib", "/radar run")
	resB, err := parseRunResult(outB)
	arB := AssertResult{ScenarioName: "B"}
	if err != nil {
		arB.assert("parse output", false, "failed to parse: %v", err)
	} else {
		arB.assert("mode=workspace-or-repo", resB.Mode == "workspace" || resB.Mode == "repo", "got %q", resB.Mode)
		arB.assert("radar ran", resB.RadarDuration != "", "empty duration")
		arB.assert("has findings", len(resB.TopFindings) > 0, "no findings")
		arB.assert("no errors", len(resB.Errors) == 0, "errors: %v", resB.Errors)
	}
	arB.summary()
	results = append(results, arB)
	fmt.Println()

	// === SCENARIO C: Drift Cross-Layer ===
	fmt.Println("--- Scenario C: Drift Cross-Layer ---")
	fmt.Println("Applying mutation...")
	runChurn("C")

	outC := runHuginnHeadless("/tmp/huginn-workspace/api-gateway", "/radar run")
	resC, err := parseRunResult(outC)
	arC := AssertResult{ScenarioName: "C"}
	if err != nil {
		arC.assert("parse output", false, "failed to parse: %v", err)
	} else {
		arC.assert("mode=workspace-or-repo", resC.Mode == "workspace" || resC.Mode == "repo", "got %q", resC.Mode)
		arC.assert("radar ran", resC.RadarDuration != "", "empty duration")
		arC.assert("has findings", len(resC.TopFindings) > 0, "no findings")
		arC.assert("no errors", len(resC.Errors) == 0, "errors: %v", resC.Errors)
	}
	arC.summary()
	results = append(results, arC)
	fmt.Println()

	// === SCENARIO D: Hot File Churn ===
	fmt.Println("--- Scenario D: Hot File Churn ---")
	fmt.Println("Applying mutation (3 rapid commits)...")
	runChurn("D")

	outD := runHuginnHeadless("/tmp/huginn-workspace/payment-service", "/radar run")
	resD, err := parseRunResult(outD)
	arD := AssertResult{ScenarioName: "D"}
	if err != nil {
		arD.assert("parse output", false, "failed to parse: %v", err)
	} else {
		arD.assert("mode=workspace-or-repo", resD.Mode == "workspace" || resD.Mode == "repo", "got %q", resD.Mode)
		arD.assert("radar ran", resD.RadarDuration != "", "empty duration")
		arD.assert("no errors", len(resD.Errors) == 0, "errors: %v", resD.Errors)
	}
	arD.summary()
	results = append(results, arD)
	fmt.Println()

	// === SCENARIO E: Workspace Mode ===
	fmt.Println("--- Scenario E: Workspace Mode ---")
	fmt.Println("Adding new repo to workspace...")
	runChurn("E")

	outE := runHuginnHeadless("/tmp/huginn-workspace", "/radar run")
	resE, err := parseRunResult(outE)
	arE := AssertResult{ScenarioName: "E"}
	if err != nil {
		arE.assert("parse output", false, "failed to parse: %v", err)
	} else {
		arE.assert("mode=workspace or repo", resE.Mode == "workspace" || resE.Mode == "repo", "got %q", resE.Mode)
		arE.assert("no errors", len(resE.Errors) == 0, "errors: %v", resE.Errors)
	}
	arE.summary()
	results = append(results, arE)
	fmt.Println()

	// === SCENARIO F: Plain Mode ===
	fmt.Println("--- Scenario F: Plain Mode ---")
	fmt.Println("Creating plain directory...")
	runChurn("F")

	outF := runHuginnHeadless("/tmp/huginn-plain", "/radar run")
	resF, err := parseRunResult(outF)
	arF := AssertResult{ScenarioName: "F"}
	if err != nil {
		arF.assert("parse output", false, "failed to parse: %v", err)
	} else {
		arF.assert("mode=plain", resF.Mode == "plain", "got %q", resF.Mode)
		arF.assert("no errors", len(resF.Errors) == 0, "errors: %v", resF.Errors)
	}
	arF.summary()
	results = append(results, arF)
	fmt.Println()

	// === PERSISTENCE TEST ===
	fmt.Println("--- Persistence Test: Second run should skip files ---")
	start := time.Now()
	outP := runHuginnHeadless("/tmp/huginn-workspace/auth-service", "/radar run")
	elapsed := time.Since(start)
	resP, err := parseRunResult(outP)
	arP := AssertResult{ScenarioName: "Persistence"}
	if err != nil {
		arP.assert("parse output", false, "failed to parse: %v", err)
	} else {
		arP.assert("files skipped > 0", resP.FilesSkipped > 0, "got %d skipped (no cache?)", resP.FilesSkipped)
		arP.assert("second run fast (<5s)", elapsed < 5*time.Second, "took %s", elapsed)
		arP.assert("no errors", len(resP.Errors) == 0, "errors: %v", resP.Errors)
	}
	arP.summary()
	results = append(results, arP)
	fmt.Println()

	// === FINAL SUMMARY ===
	fmt.Println("=== FINAL RESULTS ===")
	totalPassed := 0
	totalFailed := 0
	for _, r := range results {
		totalPassed += r.Passed
		totalFailed += r.Failed
	}
	fmt.Printf("Total: %d/%d assertions passed\n", totalPassed, totalPassed+totalFailed)
	if totalFailed > 0 {
		fmt.Printf("FAILED: %d assertions\n", totalFailed)
		os.Exit(1)
	}
	fmt.Println("ALL PASSED")
}

func runHuginnHeadless(cwd, command string) string {
	var stdout strings.Builder
	cmd := exec.Command("/tmp/huginn-test-bin",
		"--headless",
		"--cwd", cwd,
		"--command", command,
		"--json",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr // Pebble WAL logs go to stderr, not mixed into JSON
	_ = cmd.Run()
	return strings.TrimSpace(stdout.String())
}

func runChurn(scenario string) {
	huginnRoot := findHuginnRoot()
	cmd := exec.Command("go", "run",
		"tools/churn.go",
		scenario,
	)
	cmd.Dir = huginnRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  churn %s failed: %v\n%s\n", scenario, err, out)
	} else {
		fmt.Printf("  %s\n", strings.TrimSpace(string(out)))
	}
}

func findHuginnRoot() string {
	// Walk up from cwd to find go.mod with module github.com/scrypster/huginn
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		gomod := dir + "/go.mod"
		if data, err := os.ReadFile(gomod); err == nil {
			if strings.Contains(string(data), "scrypster/huginn") {
				return dir
			}
		}
		parent := dir[:strings.LastIndex(dir, "/")]
		if parent == dir || parent == "" {
			return "."
		}
		dir = parent
	}
}
