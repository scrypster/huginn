package radar

import (
	"math"
	"strings"
)

// Confidence levels for edges/symbols.
type Confidence int

const (
	ConfidenceLow    Confidence = 1
	ConfidenceMedium Confidence = 2
	ConfidenceHigh   Confidence = 3
)

// ImportRecord stores import/importedBy relationships for a file.
type ImportRecord struct {
	Imports    []string `json:"imports"`
	ImportedBy []string `json:"importedBy"`
}

// EdgeRecord stores an edge between two files.
type EdgeRecord struct {
	Confidence Confidence `json:"confidence"`
	Sites      []int      `json:"sites"`
}

// ChurnRecord tracks historical change frequency for a file.
type ChurnRecord struct {
	CommitCount int     `json:"commitCount"`
	RecentCount int     `json:"recentCount"`
	ChurnRate   float64 `json:"churnRate"`
}

// BaselineGraph is the stored call/import graph for a branch.
type BaselineGraph struct {
	Edges map[string][]string `json:"edges"`
}

// BaselinePolicy stores drift detection rules.
type BaselinePolicy struct {
	ForbiddenEdges []ForbiddenEdge `json:"forbiddenEdges"`
	LayerRules     []LayerRule     `json:"layerRules"`
}

// ForbiddenEdge is an explicit deny rule (glob or prefix).
type ForbiddenEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// LayerRule defines a one-way dependency constraint.
type LayerRule struct {
	FromPrefix string `json:"fromPrefix"`
	ToPrefix   string `json:"toPrefix"`
}

// Severity of a radar finding.
type Severity int

const (
	SeverityInfo     Severity = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "INFO"
	case SeverityLow:
		return "LOW"
	case SeverityMedium:
		return "MEDIUM"
	case SeverityHigh:
		return "HIGH"
	case SeverityCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// NotifyAction decides how the TUI surfaces a finding.
type NotifyAction int

const (
	NotifySilent NotifyAction = iota
	NotifyBanner
	NotifyUrgent
)

// Finding is a single radar detection result.
type Finding struct {
	ID          string       `json:"id"`
	Type        string       `json:"type"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Files       []string     `json:"files"`
	Score       RadarScore   `json:"score"`
	Severity    Severity     `json:"severity"`
	Notify      NotifyAction `json:"notify"`
	AckedAt     int64        `json:"ackedAt,omitempty"`
}

// RadarScore holds the 4-component breakdown (each 0-25, total 0-100).
type RadarScore struct {
	ChangeSurface     float64 `json:"changeSurface"`
	Centrality        float64 `json:"centrality"`
	DomainSensitivity float64 `json:"domainSensitivity"`
	DriftPolicy       float64 `json:"driftPolicy"`
	Total             float64 `json:"total"`
}

// ScoreInput bundles everything needed to compute a RadarScore.
type ScoreInput struct {
	ChangedFiles    []string
	ChurnRecords    map[string]ChurnRecord
	ImportRecords   map[string]ImportRecord
	TotalFileCount  int
	DriftViolations int
	NewEdgeCount    int
	CycleCount      int
}

const (
	fileCountCap = 30.0
	churnCap     = 0.8
	centralityCap = 8.0
)

var sensitiveDirs = []string{
	"api/", "auth/", "security/", "payment/", "crypto/",
	"contract/", "schema/", "proto/", "openapi/",
	"internal/auth/", "internal/security/", "internal/payment/",
	"pkg/auth/", "pkg/security/", "pkg/crypto/",
}

var sensitiveTokens = []string{
	"ssn", "dob", "pii", "token", "password", "secret",
	"key", "auth", "credential", "private",
}

func scoreChangeSurface(input ScoreInput) float64 {
	n := float64(len(input.ChangedFiles))
	if n == 0 {
		return 0
	}
	fileScore := clamp01(n/fileCountCap) * 15.0
	var churnScore float64
	if len(input.ChurnRecords) == 0 {
		churnScore = 5.0
	} else {
		var totalChurn float64
		var counted int
		for _, path := range input.ChangedFiles {
			if cr, ok := input.ChurnRecords[path]; ok {
				totalChurn += cr.ChurnRate
				counted++
			}
		}
		if counted > 0 {
			avgChurn := totalChurn / float64(counted)
			churnScore = clamp01(avgChurn/churnCap) * 10.0
		} else {
			churnScore = 5.0
		}
	}
	return fileScore + churnScore
}

func scoreCentrality(input ScoreInput) float64 {
	if len(input.ChangedFiles) == 0 {
		return 0
	}
	var maxFanIn, sumFanIn float64
	for _, path := range input.ChangedFiles {
		fanIn := 0.0
		if rec, ok := input.ImportRecords[path]; ok {
			fanIn = float64(len(rec.ImportedBy))
		}
		sumFanIn += fanIn
		if fanIn > maxFanIn {
			maxFanIn = fanIn
		}
	}
	avgFanIn := sumFanIn / float64(len(input.ChangedFiles))
	raw := 0.6*math.Log2(1+maxFanIn) + 0.4*math.Log2(1+avgFanIn)
	return clamp01(raw/centralityCap) * 25.0
}

func scoreDomainSensitivity(input ScoreInput) float64 {
	if len(input.ChangedFiles) == 0 {
		return 0
	}
	sensitiveCount := 0
	for _, path := range input.ChangedFiles {
		lower := strings.ToLower(path)
		for _, dir := range sensitiveDirs {
			if strings.Contains(lower, dir) {
				sensitiveCount++
				break
			}
		}
	}
	dirScore := (float64(sensitiveCount) / float64(len(input.ChangedFiles))) * 15.0

	tokenMatchCount := 0
	for _, path := range input.ChangedFiles {
		lower := strings.ToLower(path)
		for _, tok := range sensitiveTokens {
			if strings.Contains(lower, tok) {
				tokenMatchCount++
				break
			}
		}
	}
	tokenFraction := float64(tokenMatchCount) / float64(len(input.ChangedFiles))
	var tokenScore float64
	switch {
	case tokenFraction == 0:
		tokenScore = 0
	case tokenFraction < 0.5:
		tokenScore = 5.0
	default:
		tokenScore = 10.0
	}
	return dirScore + tokenScore
}

func scoreDriftPolicy(input ScoreInput) float64 {
	forbiddenScore := math.Min(float64(input.DriftViolations), 5) * 3.0
	newEdgeScore := math.Min(math.Log2(1+float64(input.NewEdgeCount)), 3) * 2.0
	cycleScore := math.Min(float64(input.CycleCount), 2) * 2.0
	total := forbiddenScore + newEdgeScore + cycleScore
	if total > 25.0 {
		total = 25.0
	}
	return total
}

// ComputeScore calculates the full 4-component RadarScore.
func ComputeScore(input ScoreInput) RadarScore {
	cs := scoreChangeSurface(input)
	ce := scoreCentrality(input)
	ds := scoreDomainSensitivity(input)
	dp := scoreDriftPolicy(input)
	return RadarScore{
		ChangeSurface:     round2(cs),
		Centrality:        round2(ce),
		DomainSensitivity: round2(ds),
		DriftPolicy:       round2(dp),
		Total:             round2(cs + ce + ds + dp),
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	// A tiny epsilon (1e-10) compensates for IEEE-754 binary representation
	// errors at decimal half-points. For example, 1.005 in float64 is
	// actually 1.004999..., causing math.Round(1.005*100) to yield 100
	// instead of 101. The epsilon pushes these boundary values over the
	// rounding threshold without affecting values that aren't at a half-point.
	return math.Floor(v*100+0.5+1e-10) / 100
}
