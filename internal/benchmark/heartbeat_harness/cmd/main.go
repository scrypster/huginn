package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	heartbeatharness "github.com/scrypster/huginn/internal/benchmark/heartbeat_harness"
)

func main() {
	output := flag.String("output", "", "path to markdown scorecard output")
	enforceThresholds := flag.Bool("enforce-thresholds", false, "exit non-zero if benchmark quality gate fails")
	flag.Parse()

	if *output == "" {
		fmt.Fprintln(os.Stderr, "missing required -output path")
		os.Exit(2)
	}

	now := time.Now().UTC()
	results := heartbeatharness.RunDefaultSuite(now)
	report := heartbeatharness.BuildMarkdownReport(now, results)
	eval := heartbeatharness.EvaluateGate(results, heartbeatharness.DefaultGateThresholds())

	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir output dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, []byte(report), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("heartbeat benchmark scorecard written: %s\n", *output)
	fmt.Println(eval.Summary())
	if !eval.Passed {
		fmt.Println(eval.FailureText())
	}
	if *enforceThresholds && !eval.Passed {
		os.Exit(1)
	}
}
