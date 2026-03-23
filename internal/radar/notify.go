package radar

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

const (
	suppressInfoLow  = 24 * time.Hour
	suppressMedium   = 4 * time.Hour
	suppressHighCrit = 1 * time.Hour
)

// AckStore abstracts finding acknowledgment persistence.
type AckStore interface {
	GetAck(findingID string) (ackedAt int64, found bool)
	SetAck(findingID string, ackedAt int64) error
}

// PebbleAckStore implements AckStore backed by Pebble.
type PebbleAckStore struct {
	DB     *pebble.DB
	RepoID string
}

func (s *PebbleAckStore) ackKey(findingID string) []byte {
	return []byte(fmt.Sprintf("repo/%s/radar/ack/%s", s.RepoID, findingID))
}

func (s *PebbleAckStore) GetAck(findingID string) (int64, bool) {
	val, closer, err := s.DB.Get(s.ackKey(findingID))
	if err != nil {
		return 0, false
	}
	defer closer.Close()
	var ts int64
	if err := jsonUnmarshal(val, &ts); err != nil {
		return 0, false
	}
	return ts, true
}

func (s *PebbleAckStore) SetAck(findingID string, ackedAt int64) error {
	val, err := jsonMarshal(ackedAt)
	if err != nil {
		return err
	}
	return s.DB.Set(s.ackKey(findingID), val, pebble.Sync)
}

// ClassifyFinding returns severity and notify action for a score.
func ClassifyFinding(score RadarScore, findingType string, files []string, ackStore AckStore) (Severity, NotifyAction) {
	sev := scoreToSeverity(score.Total)
	notify := severityToNotify(sev)

	if score.DriftPolicy >= 20 && sev < SeverityMedium {
		sev = SeverityMedium
		notify = NotifyBanner
	}

	findingID := computeFindingID(findingType, files)
	if ackStore != nil {
		if ackedAt, found := ackStore.GetAck(findingID); found {
			window := suppressionWindow(sev)
			if time.Since(time.Unix(ackedAt, 0)) < window {
				notify = NotifySilent
			}
		}
	}
	return sev, notify
}

func scoreToSeverity(total float64) Severity {
	switch {
	case total >= 80:
		return SeverityCritical
	case total >= 60:
		return SeverityHigh
	case total >= 40:
		return SeverityMedium
	case total >= 20:
		return SeverityLow
	default:
		return SeverityInfo
	}
}

func severityToNotify(sev Severity) NotifyAction {
	switch sev {
	case SeverityCritical, SeverityHigh:
		return NotifyUrgent
	case SeverityMedium:
		return NotifyBanner
	default:
		return NotifySilent
	}
}

func suppressionWindow(sev Severity) time.Duration {
	switch sev {
	case SeverityCritical, SeverityHigh:
		return suppressHighCrit
	case SeverityMedium:
		return suppressMedium
	default:
		return suppressInfoLow
	}
}

func computeFindingID(findingType string, files []string) string {
	sorted := make([]string, len(files))
	copy(sorted, files)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(findingType))
	h.Write([]byte{0})
	for _, f := range sorted {
		h.Write([]byte(f))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// EvaluateInput bundles everything needed for a full radar evaluation.
type EvaluateInput struct {
	DB           *pebble.DB
	RepoID       string
	SHA          string
	Branch       string
	ChangedFiles []string
	ChurnRecords map[string]ChurnRecord
	TotalFiles   int
	AckStore     AckStore
}

// Evaluate runs the full radar pipeline: BFS → Drift → Score → Classify.
func Evaluate(input EvaluateInput) ([]Finding, error) {
	var findings []Finding

	impact, err := ComputeImpact(input.DB, input.RepoID, input.SHA, input.ChangedFiles)
	if err != nil {
		return nil, fmt.Errorf("impact traversal: %w", err)
	}

	drift, err := DetectDrift(input.DB, input.RepoID, input.SHA, input.Branch, input.ChangedFiles)
	if err != nil {
		return nil, fmt.Errorf("drift detection: %w", err)
	}

	importRecords := make(map[string]ImportRecord, len(impact.Impacted))
	for _, node := range impact.Impacted {
		rec, err := getImportRecord(input.DB, input.RepoID, input.SHA, node.Path)
		if err == nil {
			importRecords[node.Path] = *rec
		}
	}

	scoreInput := ScoreInput{
		ChangedFiles:    input.ChangedFiles,
		ChurnRecords:    input.ChurnRecords,
		ImportRecords:   importRecords,
		TotalFileCount:  input.TotalFiles,
		DriftViolations: len(drift.ForbiddenEdges) + len(drift.CrossLayerViolations),
		NewEdgeCount:    len(drift.NewEdges),
		CycleCount:      len(drift.NewCycles),
	}
	score := ComputeScore(scoreInput)

	// Finding: high-impact change
	{
		sev, notify := ClassifyFinding(score, "high-impact", input.ChangedFiles, input.AckStore)
		impactedPaths := make([]string, len(impact.Impacted))
		for i, n := range impact.Impacted {
			impactedPaths[i] = n.Path
		}
		findings = append(findings, Finding{
			ID:          computeFindingID("high-impact", input.ChangedFiles),
			Type:        "high-impact",
			Title:       fmt.Sprintf("Change impacts %d files (depth %d)", impact.NodesVisited, BFSMaxDepth),
			Description: fmt.Sprintf("BFS from %d changed files reached %d nodes. Truncated: %v", len(input.ChangedFiles), impact.NodesVisited, impact.Truncated),
			Files:       input.ChangedFiles,
			Score:       score,
			Severity:    sev,
			Notify:      notify,
		})
	}

	// Findings: cycles
	for _, cycle := range drift.NewCycles {
		cycleScore := score
		cycleScore.DriftPolicy = 25
		cycleScore.Total = cycleScore.ChangeSurface + cycleScore.Centrality + cycleScore.DomainSensitivity + 25
		sev, notify := ClassifyFinding(cycleScore, "cycle", cycle.Nodes, input.AckStore)
		findings = append(findings, Finding{
			ID:          computeFindingID("cycle", cycle.Nodes),
			Type:        "cycle",
			Title:       fmt.Sprintf("Import cycle: %s", strings.Join(cycle.Nodes, " -> ")),
			Description: fmt.Sprintf("Cycle of length %d", len(cycle.Nodes)),
			Files:       cycle.Nodes,
			Score:       cycleScore,
			Severity:    sev,
			Notify:      notify,
		})
	}

	// Findings: forbidden edges
	for _, v := range drift.ForbiddenEdges {
		files := []string{v.From, v.To}
		sev, notify := ClassifyFinding(score, "forbidden-edge", files, input.AckStore)
		findings = append(findings, Finding{
			ID:          computeFindingID("forbidden-edge", files),
			Type:        "forbidden-edge",
			Title:       fmt.Sprintf("Forbidden import: %s -> %s", v.From, v.To),
			Description: v.Rule,
			Files:       files,
			Score:       score,
			Severity:    sev,
			Notify:      notify,
		})
	}

	// Findings: cross-layer violations
	for _, v := range drift.CrossLayerViolations {
		files := []string{v.From, v.To}
		sev, notify := ClassifyFinding(score, "cross-layer", files, input.AckStore)
		findings = append(findings, Finding{
			ID:          computeFindingID("cross-layer", files),
			Type:        "cross-layer",
			Title:       fmt.Sprintf("Cross-layer violation: %s -> %s", v.From, v.To),
			Description: v.Rule,
			Files:       files,
			Score:       score,
			Severity:    sev,
			Notify:      notify,
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity
		}
		return findings[i].Score.Total > findings[j].Score.Total
	})

	return findings, nil
}
