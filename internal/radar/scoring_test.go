package radar

import (
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// ComputeScore — total equals sum of components
// ---------------------------------------------------------------------------

func TestComputeScore_TotalEqualsComponents(t *testing.T) {
	input := ScoreInput{
		ChangedFiles:    []string{"a.go", "b.go", "auth/login.go"},
		ChurnRecords:    map[string]ChurnRecord{"a.go": {ChurnRate: 0.5}},
		ImportRecords:   map[string]ImportRecord{"auth/login.go": {ImportedBy: []string{"x", "y", "z"}}},
		TotalFileCount:  100,
		DriftViolations: 2,
		NewEdgeCount:    5,
		CycleCount:      1,
	}
	score := ComputeScore(input)
	expected := round2(score.ChangeSurface + score.Centrality + score.DomainSensitivity + score.DriftPolicy)
	if math.Abs(score.Total-expected) > 0.01 {
		t.Errorf("Total (%v) != sum of components (%v)", score.Total, expected)
	}
}

// ---------------------------------------------------------------------------
// ComputeScore — overall bounds with extreme input
// ---------------------------------------------------------------------------

func TestComputeScore_ExtremeInput_AllBounded(t *testing.T) {
	files := make([]string, 50)
	churn := make(map[string]ChurnRecord)
	imports := make(map[string]ImportRecord)
	importedBy := make([]string, 20)
	for j := range importedBy {
		importedBy[j] = "dep.go"
	}
	for i := range files {
		name := "auth/secret_key_" + string(rune('a'+i%26)) + ".go"
		files[i] = name
		churn[name] = ChurnRecord{ChurnRate: 1.0}
		imports[name] = ImportRecord{ImportedBy: importedBy}
	}

	input := ScoreInput{
		ChangedFiles:    files,
		ChurnRecords:    churn,
		ImportRecords:   imports,
		TotalFileCount:  1000,
		DriftViolations: 100,
		NewEdgeCount:    1000,
		CycleCount:      100,
	}
	score := ComputeScore(input)

	if score.Total > 100 {
		t.Errorf("total score exceeds 100: %v", score.Total)
	}
	if score.ChangeSurface > 25 {
		t.Errorf("ChangeSurface exceeds 25: %v", score.ChangeSurface)
	}
	if score.Centrality > 25 {
		t.Errorf("Centrality exceeds 25: %v", score.Centrality)
	}
	if score.DomainSensitivity > 25 {
		t.Errorf("DomainSensitivity exceeds 25: %v", score.DomainSensitivity)
	}
	if score.DriftPolicy > 25 {
		t.Errorf("DriftPolicy exceeds 25: %v", score.DriftPolicy)
	}
}

// ---------------------------------------------------------------------------
// scoreChangeSurface
// ---------------------------------------------------------------------------

func TestScoreChangeSurface_WithHighChurn(t *testing.T) {
	input := ScoreInput{
		ChangedFiles: []string{"hot.go"},
		ChurnRecords: map[string]ChurnRecord{
			"hot.go": {ChurnRate: 0.8},
		},
	}
	score := scoreChangeSurface(input)
	// churnScore = clamp01(0.8/0.8) * 10 = 10; fileScore ≈ 0.5
	if score < 10 {
		t.Errorf("expected change surface ≥ 10 for max-churn file, got %v", score)
	}
}

func TestScoreChangeSurface_NoChurnRecord_DefaultsFive(t *testing.T) {
	input := ScoreInput{
		ChangedFiles: []string{"file.go"},
		ChurnRecords: map[string]ChurnRecord{},
	}
	// No record for file.go → default churnScore = 5
	score := scoreChangeSurface(input)
	// Should include the default 5.0
	if score < 5 {
		t.Errorf("expected at least 5 (default churn) for file with no churn record, got %v", score)
	}
}

// ---------------------------------------------------------------------------
// scoreCentrality
// ---------------------------------------------------------------------------

func TestScoreCentrality_MultipleDependents(t *testing.T) {
	importedBy := make([]string, 50)
	for i := range importedBy {
		importedBy[i] = "dep.go"
	}
	input := ScoreInput{
		ChangedFiles: []string{"core.go"},
		ImportRecords: map[string]ImportRecord{
			"core.go": {ImportedBy: importedBy},
		},
	}
	score := scoreCentrality(input)
	if score <= 0 {
		t.Errorf("expected positive centrality for high fan-in, got %v", score)
	}
	if score > 25 {
		t.Errorf("expected centrality ≤ 25, got %v", score)
	}
}

// ---------------------------------------------------------------------------
// scoreDomainSensitivity
// ---------------------------------------------------------------------------

func TestScoreDomainSensitivity_NormalFile_ZeroScore(t *testing.T) {
	input := ScoreInput{
		ChangedFiles: []string{"internal/handlers/user.go"},
	}
	score := scoreDomainSensitivity(input)
	if score != 0 {
		t.Errorf("expected 0 for non-sensitive file, got %v", score)
	}
}

func TestScoreDomainSensitivity_AuthDir_HighScore(t *testing.T) {
	input := ScoreInput{
		ChangedFiles: []string{"auth/login.go"},
	}
	score := scoreDomainSensitivity(input)
	if score < 15 {
		t.Errorf("expected domain sensitivity ≥ 15 for auth/ dir, got %v", score)
	}
}

func TestScoreDomainSensitivity_TokenInFilename(t *testing.T) {
	input := ScoreInput{
		ChangedFiles: []string{"internal/token_manager.go"},
	}
	score := scoreDomainSensitivity(input)
	if score < 5 {
		t.Errorf("expected sensitivity ≥ 5 for 'token' in filename, got %v", score)
	}
}

// ---------------------------------------------------------------------------
// scoreToSeverity — boundary conditions
// ---------------------------------------------------------------------------

func TestScoreToSeverity_Boundaries(t *testing.T) {
	tests := []struct {
		total    float64
		expected Severity
	}{
		{0, SeverityInfo},
		{19.99, SeverityInfo},
		{20, SeverityLow},
		{39.99, SeverityLow},
		{40, SeverityMedium},
		{59.99, SeverityMedium},
		{60, SeverityHigh},
		{79.99, SeverityHigh},
		{80, SeverityCritical},
		{100, SeverityCritical},
	}
	for _, tt := range tests {
		got := scoreToSeverity(tt.total)
		if got != tt.expected {
			t.Errorf("scoreToSeverity(%v) = %v (%s), want %v (%s)",
				tt.total, got, got, tt.expected, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// ClassifyFinding — DriftPolicy forces Medium for low total
// ---------------------------------------------------------------------------

func TestClassifyFinding_DriftPolicyThreshold_ForcesMedium(t *testing.T) {
	// Total score is low (Info) but DriftPolicy ≥ 20 → must upgrade to at least Medium
	score := RadarScore{Total: 5, DriftPolicy: 20}
	sev, notify := ClassifyFinding(score, "test", []string{"file.go"}, nil)
	if sev < SeverityMedium {
		t.Errorf("expected sev >= Medium when DriftPolicy>=20 (got %v)", sev)
	}
	if notify != NotifyBanner {
		t.Errorf("expected NotifyBanner for drift upgrade, got %v", notify)
	}
}

func TestClassifyFinding_DriftPolicyBelowThreshold_NoUpgrade(t *testing.T) {
	// DriftPolicy = 19: should NOT upgrade from Info
	score := RadarScore{Total: 5, DriftPolicy: 19}
	sev, _ := ClassifyFinding(score, "test", []string{"file.go"}, nil)
	if sev != SeverityInfo {
		t.Errorf("expected SeverityInfo when DriftPolicy < 20 (got %v)", sev)
	}
}

// ---------------------------------------------------------------------------
// round2 edge cases not covered elsewhere
// ---------------------------------------------------------------------------

func TestRound2_HalfPoint(t *testing.T) {
	// 1.005 should round to 1.01
	got := round2(1.005)
	if math.Abs(got-1.01) > 0.005 {
		t.Errorf("round2(1.005) = %v, want ~1.01", got)
	}
}

func TestRound2_NaN_ReturnsZero(t *testing.T) {
	if got := round2(math.NaN()); got != 0 {
		t.Errorf("round2(NaN) = %v, want 0", got)
	}
}

func TestRound2_Inf_ReturnsZero(t *testing.T) {
	if got := round2(math.Inf(1)); got != 0 {
		t.Errorf("round2(+Inf) = %v, want 0", got)
	}
	if got := round2(math.Inf(-1)); got != 0 {
		t.Errorf("round2(-Inf) = %v, want 0", got)
	}
}
