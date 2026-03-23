package radar

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// openTestDB opens a Pebble database in a temp directory.
func openTestDB(t *testing.T) *pebble.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := pebble.Open(filepath.Join(dir, "test.pebble"), &pebble.Options{})
	if err != nil {
		t.Fatalf("pebble.Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("db.Close: %v", err)
		}
	})
	return db
}

// mustMarshal panics if json.Marshal fails.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return b
}

// writeImportRecord writes an ImportRecord to the DB.
func writeImportRecord(t *testing.T, db *pebble.DB, repoID, sha, path string, rec ImportRecord) {
	t.Helper()
	key := impKey(repoID, sha, path)
	val := mustMarshal(t, rec)
	if err := db.Set(key, val, pebble.Sync); err != nil {
		t.Fatalf("writeImportRecord: %v", err)
	}
}

// writeEdge writes a snap edge to the DB for scanCurrentEdges.
//
// The key uses a \x00 separator between from and to so that both values
// can contain slashes (e.g. "cmd/main.go" -> "internal/service/svc.go").
func writeEdge(t *testing.T, db *pebble.DB, repoID, sha, from, to string) {
	t.Helper()
	key := snapEdgeKey(repoID, sha, from, to)
	if err := db.Set(key, []byte("1"), pebble.Sync); err != nil {
		t.Fatalf("writeEdge: %v", err)
	}
}

// writeBaselineGraph writes a BaselineGraph to the DB.
func writeBaselineGraph(t *testing.T, db *pebble.DB, repoID, branch string, bg BaselineGraph) {
	t.Helper()
	val := mustMarshal(t, bg)
	if err := db.Set(baselineGraphKey(repoID, branch), val, pebble.Sync); err != nil {
		t.Fatalf("writeBaselineGraph: %v", err)
	}
}

// writeBaselinePolicy writes a BaselinePolicy to the DB.
func writeBaselinePolicy(t *testing.T, db *pebble.DB, repoID, branch string, bp BaselinePolicy) {
	t.Helper()
	val := mustMarshal(t, bp)
	if err := db.Set(baselinePolicyKey(repoID, branch), val, pebble.Sync); err != nil {
		t.Fatalf("writeBaselinePolicy: %v", err)
	}
}

// memAckStore is an in-memory AckStore for testing.
type memAckStore struct {
	mu   sync.Mutex
	data map[string]int64
}

func newMemAckStore() *memAckStore {
	return &memAckStore{data: make(map[string]int64)}
}

func (m *memAckStore) GetAck(findingID string) (int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[findingID]
	return v, ok
}

func (m *memAckStore) SetAck(findingID string, ackedAt int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[findingID] = ackedAt
	return nil
}

// ---------------------------------------------------------------------------
// Severity.String
// ---------------------------------------------------------------------------

func TestSeverity_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sev  Severity
		want string
	}{
		{SeverityInfo, "INFO"},
		{SeverityLow, "LOW"},
		{SeverityMedium, "MEDIUM"},
		{SeverityHigh, "HIGH"},
		{SeverityCritical, "CRITICAL"},
		{Severity(99), "UNKNOWN"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.sev.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// scoreToSeverity
// ---------------------------------------------------------------------------

func TestScoreToSeverity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		score float64
		want  Severity
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
		{-1, SeverityInfo},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("score_%.0f", tc.score), func(t *testing.T) {
			t.Parallel()
			if got := scoreToSeverity(tc.score); got != tc.want {
				t.Errorf("scoreToSeverity(%.2f) = %v, want %v", tc.score, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// severityToNotify
// ---------------------------------------------------------------------------

func TestSeverityToNotify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sev    Severity
		notify NotifyAction
	}{
		{SeverityCritical, NotifyUrgent},
		{SeverityHigh, NotifyUrgent},
		{SeverityMedium, NotifyBanner},
		{SeverityLow, NotifySilent},
		{SeverityInfo, NotifySilent},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.sev.String(), func(t *testing.T) {
			t.Parallel()
			if got := severityToNotify(tc.sev); got != tc.notify {
				t.Errorf("severityToNotify(%v) = %v, want %v", tc.sev, got, tc.notify)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// suppressionWindow
// ---------------------------------------------------------------------------

func TestSuppressionWindow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		sev  Severity
		want time.Duration
	}{
		{SeverityCritical, suppressHighCrit},
		{SeverityHigh, suppressHighCrit},
		{SeverityMedium, suppressMedium},
		{SeverityLow, suppressInfoLow},
		{SeverityInfo, suppressInfoLow},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.sev.String(), func(t *testing.T) {
			t.Parallel()
			if got := suppressionWindow(tc.sev); got != tc.want {
				t.Errorf("suppressionWindow(%v) = %v, want %v", tc.sev, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// computeFindingID
// ---------------------------------------------------------------------------

func TestComputeFindingID_Deterministic(t *testing.T) {
	t.Parallel()
	id1 := computeFindingID("high-impact", []string{"a.go", "b.go"})
	id2 := computeFindingID("high-impact", []string{"a.go", "b.go"})
	if id1 != id2 {
		t.Errorf("non-deterministic: %q != %q", id1, id2)
	}
}

func TestComputeFindingID_FileOrderIndependent(t *testing.T) {
	t.Parallel()
	id1 := computeFindingID("cycle", []string{"a.go", "b.go", "c.go"})
	id2 := computeFindingID("cycle", []string{"c.go", "a.go", "b.go"})
	if id1 != id2 {
		t.Errorf("file order should not matter: %q != %q", id1, id2)
	}
}

func TestComputeFindingID_TypeChangesID(t *testing.T) {
	t.Parallel()
	files := []string{"a.go"}
	id1 := computeFindingID("high-impact", files)
	id2 := computeFindingID("cycle", files)
	if id1 == id2 {
		t.Error("different types should produce different IDs")
	}
}

func TestComputeFindingID_Length16(t *testing.T) {
	t.Parallel()
	id := computeFindingID("test", []string{"a.go", "b.go"})
	if len(id) != 16 {
		t.Errorf("expected ID length 16, got %d: %q", len(id), id)
	}
}

func TestComputeFindingID_EmptyFiles(t *testing.T) {
	t.Parallel()
	id := computeFindingID("type", []string{})
	if len(id) != 16 {
		t.Errorf("expected ID length 16 even with no files, got %d", len(id))
	}
}

func TestComputeFindingID_OnlyHex(t *testing.T) {
	t.Parallel()
	id := computeFindingID("test", []string{"x.go"})
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("non-hex char %q in ID %q", c, id)
		}
	}
}

// ---------------------------------------------------------------------------
// ClassifyFinding
// ---------------------------------------------------------------------------

func TestClassifyFinding_HighScore_NoAck(t *testing.T) {
	t.Parallel()
	score := RadarScore{Total: 85}
	sev, notify := ClassifyFinding(score, "high-impact", []string{"main.go"}, nil)
	if sev != SeverityCritical {
		t.Errorf("expected Critical, got %v", sev)
	}
	if notify != NotifyUrgent {
		t.Errorf("expected Urgent, got %v", notify)
	}
}

func TestClassifyFinding_LowScore_NoAck(t *testing.T) {
	t.Parallel()
	score := RadarScore{Total: 10}
	sev, notify := ClassifyFinding(score, "low", []string{"x.go"}, nil)
	if sev != SeverityInfo {
		t.Errorf("expected Info, got %v", sev)
	}
	if notify != NotifySilent {
		t.Errorf("expected Silent, got %v", notify)
	}
}

// DriftPolicy >= 20 should upgrade severity to at least Medium.
func TestClassifyFinding_DriftPolicyUpgrade(t *testing.T) {
	t.Parallel()
	score := RadarScore{Total: 5, DriftPolicy: 20} // Total is low but DriftPolicy is high
	sev, notify := ClassifyFinding(score, "drift", []string{"a.go"}, nil)
	if sev < SeverityMedium {
		t.Errorf("expected at least Medium due to DriftPolicy, got %v", sev)
	}
	if notify != NotifyBanner {
		t.Errorf("expected Banner, got %v", notify)
	}
}

// DriftPolicy < 20 must not trigger the upgrade path.
func TestClassifyFinding_DriftPolicyBelowThreshold(t *testing.T) {
	t.Parallel()
	score := RadarScore{Total: 5, DriftPolicy: 19}
	sev, _ := ClassifyFinding(score, "drift", []string{"a.go"}, nil)
	if sev != SeverityInfo {
		t.Errorf("DriftPolicy 19 must not upgrade; got %v", sev)
	}
}

// DriftPolicy >= 20 but severity already >= Medium must not downgrade.
func TestClassifyFinding_DriftPolicyNoDowngrade(t *testing.T) {
	t.Parallel()
	score := RadarScore{Total: 90, DriftPolicy: 20} // already Critical
	sev, notify := ClassifyFinding(score, "high", []string{"a.go"}, nil)
	if sev != SeverityCritical {
		t.Errorf("expected Critical (no downgrade), got %v", sev)
	}
	if notify != NotifyUrgent {
		t.Errorf("expected Urgent, got %v", notify)
	}
}

// Acknowledged finding within suppression window must be silenced.
func TestClassifyFinding_SuppressedWithinWindow(t *testing.T) {
	t.Parallel()
	acks := newMemAckStore()
	score := RadarScore{Total: 90} // Critical

	files := []string{"main.go"}
	findingID := computeFindingID("high-impact", files)
	// Ack just now — within any suppression window
	_ = acks.SetAck(findingID, time.Now().Unix())

	_, notify := ClassifyFinding(score, "high-impact", files, acks)
	if notify != NotifySilent {
		t.Errorf("expected Silent for acked finding, got %v", notify)
	}
}

// Ack older than suppression window must NOT silence.
func TestClassifyFinding_AckExpired(t *testing.T) {
	t.Parallel()
	acks := newMemAckStore()
	score := RadarScore{Total: 90} // Critical — suppress window = 1h

	files := []string{"main.go"}
	findingID := computeFindingID("high-impact", files)
	// Ack 2 hours ago — expired
	_ = acks.SetAck(findingID, time.Now().Add(-2*time.Hour).Unix())

	_, notify := ClassifyFinding(score, "high-impact", files, acks)
	if notify == NotifySilent {
		t.Errorf("expected non-silent for expired ack, got Silent")
	}
}

// NilAckStore must not panic.
func TestClassifyFinding_NilAckStore(t *testing.T) {
	t.Parallel()
	score := RadarScore{Total: 50}
	sev, notify := ClassifyFinding(score, "t", []string{"f.go"}, nil)
	_ = sev
	_ = notify
}

// ---------------------------------------------------------------------------
// PebbleAckStore
// ---------------------------------------------------------------------------

func TestPebbleAckStore_SetAndGet(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := &PebbleAckStore{DB: db, RepoID: "repo1"}

	findingID := "test-finding-001"
	now := time.Now().Unix()

	if _, found := store.GetAck(findingID); found {
		t.Error("expected not found initially")
	}

	if err := store.SetAck(findingID, now); err != nil {
		t.Fatalf("SetAck: %v", err)
	}

	ackedAt, found := store.GetAck(findingID)
	if !found {
		t.Fatal("expected found after SetAck")
	}
	if ackedAt != now {
		t.Errorf("ackedAt: got %d, want %d", ackedAt, now)
	}
}

func TestPebbleAckStore_Overwrite(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := &PebbleAckStore{DB: db, RepoID: "repo1"}

	_ = store.SetAck("id", 1000)
	_ = store.SetAck("id", 2000)

	ackedAt, found := store.GetAck("id")
	if !found {
		t.Fatal("expected found")
	}
	if ackedAt != 2000 {
		t.Errorf("expected 2000, got %d", ackedAt)
	}
}

func TestPebbleAckStore_IsolatedByRepoID(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store1 := &PebbleAckStore{DB: db, RepoID: "repo-a"}
	store2 := &PebbleAckStore{DB: db, RepoID: "repo-b"}

	_ = store1.SetAck("finding1", 999)

	_, found := store2.GetAck("finding1")
	if found {
		t.Error("ack from repo-a should not be visible from repo-b")
	}
}

// ---------------------------------------------------------------------------
// ComputeScore
// ---------------------------------------------------------------------------

func TestComputeScore_AllZero(t *testing.T) {
	t.Parallel()
	score := ComputeScore(ScoreInput{})
	if score.Total != 0 {
		t.Errorf("expected Total=0 for empty input, got %v", score.Total)
	}
	if score.ChangeSurface != 0 {
		t.Errorf("ChangeSurface: %v", score.ChangeSurface)
	}
}

func TestComputeScore_MaxFiles(t *testing.T) {
	t.Parallel()
	// 30 files = fileCountCap — should max out file portion of ChangeSurface
	files := make([]string, 30)
	for i := range files {
		files[i] = fmt.Sprintf("file%d.go", i)
	}
	score := ComputeScore(ScoreInput{ChangedFiles: files})
	if score.ChangeSurface < 15.0 {
		t.Errorf("expected ChangeSurface >= 15 with 30 files, got %v", score.ChangeSurface)
	}
}

func TestComputeScore_SensitiveFiles(t *testing.T) {
	t.Parallel()
	files := []string{"internal/auth/handler.go", "internal/payment/process.go"}
	score := ComputeScore(ScoreInput{ChangedFiles: files})
	if score.DomainSensitivity == 0 {
		t.Errorf("expected non-zero DomainSensitivity for auth/payment files, got %v", score.DomainSensitivity)
	}
}

func TestComputeScore_SensitiveTokens(t *testing.T) {
	t.Parallel()
	// Files with sensitive tokens in their names
	files := []string{"internal/user_password.go", "auth_token.go"}
	score := ComputeScore(ScoreInput{ChangedFiles: files})
	if score.DomainSensitivity == 0 {
		t.Errorf("expected DomainSensitivity > 0 for token-matched files")
	}
}

func TestComputeScore_HighFanIn(t *testing.T) {
	t.Parallel()
	path := "internal/core/base.go"
	importedBy := make([]string, 50)
	for i := range importedBy {
		importedBy[i] = fmt.Sprintf("other%d.go", i)
	}
	score := ComputeScore(ScoreInput{
		ChangedFiles: []string{path},
		ImportRecords: map[string]ImportRecord{
			path: {ImportedBy: importedBy},
		},
	})
	if score.Centrality == 0 {
		t.Errorf("expected non-zero Centrality for high-fanin file, got %v", score.Centrality)
	}
}

func TestComputeScore_DriftViolations(t *testing.T) {
	t.Parallel()
	score := ComputeScore(ScoreInput{
		ChangedFiles:    []string{"a.go"},
		DriftViolations: 5,
	})
	if score.DriftPolicy == 0 {
		t.Errorf("expected non-zero DriftPolicy, got %v", score.DriftPolicy)
	}
}

func TestComputeScore_DriftCap(t *testing.T) {
	t.Parallel()
	// Extreme values — DriftPolicy must be capped at 25
	score := ComputeScore(ScoreInput{
		ChangedFiles:    []string{"a.go"},
		DriftViolations: 1000,
		NewEdgeCount:    1000,
		CycleCount:      1000,
	})
	if score.DriftPolicy > 25.0 {
		t.Errorf("DriftPolicy exceeds 25: %v", score.DriftPolicy)
	}
}

func TestComputeScore_TotalIsSum(t *testing.T) {
	t.Parallel()
	files := []string{"internal/auth/handler.go"}
	score := ComputeScore(ScoreInput{
		ChangedFiles:    files,
		DriftViolations: 2,
		CycleCount:      1,
	})
	expected := round2(score.ChangeSurface + score.Centrality + score.DomainSensitivity + score.DriftPolicy)
	if math.Abs(score.Total-expected) > 0.01 {
		t.Errorf("Total %v != sum of components %v", score.Total, expected)
	}
}

func TestComputeScore_ComponentsNonNegative(t *testing.T) {
	t.Parallel()
	score := ComputeScore(ScoreInput{
		ChangedFiles: []string{"a.go"},
	})
	if score.ChangeSurface < 0 {
		t.Errorf("ChangeSurface negative: %v", score.ChangeSurface)
	}
	if score.Centrality < 0 {
		t.Errorf("Centrality negative: %v", score.Centrality)
	}
	if score.DomainSensitivity < 0 {
		t.Errorf("DomainSensitivity negative: %v", score.DomainSensitivity)
	}
	if score.DriftPolicy < 0 {
		t.Errorf("DriftPolicy negative: %v", score.DriftPolicy)
	}
}

func TestComputeScore_ChurnRate(t *testing.T) {
	t.Parallel()
	path := "internal/hot.go"
	score := ComputeScore(ScoreInput{
		ChangedFiles: []string{path},
		ChurnRecords: map[string]ChurnRecord{
			path: {ChurnRate: 0.9}, // above churnCap
		},
	})
	if score.ChangeSurface == 0 {
		t.Error("expected ChangeSurface > 0 with high churn")
	}
}

func TestComputeScore_Round2(t *testing.T) {
	t.Parallel()
	// round2 uses fmt.Sprintf("%.2f") + ParseFloat to get correct decimal
	// rounding, avoiding IEEE-754 binary representation artifacts.
	tests := []struct {
		in   float64
		want float64
	}{
		{1.234, 1.23},
		{1.235, 1.24},
		{1.005, 1.01},   // was 1.00 with math.Round due to binary float
		{0.005, 0.01},   // boundary case
		{2.455, 2.46},   // boundary case
		{99.995, 100.00}, // boundary case
		{0, 0},
		{25.0, 25.0},
		{12.5678, 12.57},
		{math.NaN(), 0},
		{math.Inf(1), 0},
		{math.Inf(-1), 0},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("%v", tc.in), func(t *testing.T) {
			t.Parallel()
			got := round2(tc.in)
			if math.IsNaN(got) || got != tc.want {
				t.Errorf("round2(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestClamp01(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{0.5, 0.5},
		{1, 1},
		{-1, 0},
		{1.5, 1},
		{math.NaN(), 0},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("%v", tc.in), func(t *testing.T) {
			t.Parallel()
			got := clamp01(tc.in)
			if got != tc.want {
				t.Errorf("clamp01(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// inferLayer
// ---------------------------------------------------------------------------

func TestInferLayer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path      string
		wantNil   bool
		wantName  string
		wantRank  int
	}{
		{"internal/api/handler.go", false, "api", 30},
		{"internal/service/svc.go", false, "application", 20},
		{"internal/domain/model.go", false, "domain", 10},
		{"internal/infra/repo.go", false, "infra", 5},
		{"cmd/main.go", false, "cmd", 40},
		{"pkg/util.go", false, "pkg", 15},
		{"random/path/file.go", true, "", 0},
		{"src/api/handler.go", false, "api", 30},
		{"app/services/svc.go", false, "application", 20},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			layer := inferLayer(tc.path)
			if tc.wantNil {
				if layer != nil {
					t.Errorf("expected nil layer for %q, got %+v", tc.path, layer)
				}
				return
			}
			if layer == nil {
				t.Fatalf("expected non-nil layer for %q", tc.path)
			}
			if layer.Name != tc.wantName {
				t.Errorf("layer.Name: got %q, want %q", layer.Name, tc.wantName)
			}
			if layer.Rank != tc.wantRank {
				t.Errorf("layer.Rank: got %d, want %d", layer.Rank, tc.wantRank)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isLayerViolation
// ---------------------------------------------------------------------------

func TestIsLayerViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		from      string
		to        string
		wantViol  bool
	}{
		// domain -> service: domain (10) imports application (20) — violation
		{"internal/domain/model.go", "internal/service/svc.go", true},
		// cmd -> api: cmd (40) imports api (30) — OK (higher rank importing lower)
		{"cmd/main.go", "internal/api/handler.go", false},
		// service -> domain: application (20) imports domain (10) — OK
		{"internal/service/svc.go", "internal/domain/model.go", false},
		// infra -> anything: toLayer.Rank==5 is always OK (no violation)
		{"internal/domain/model.go", "internal/infra/repo.go", false},
		// unknown paths: no layer detected → no violation
		{"random/a.go", "random/b.go", false},
		// one side unknown
		{"internal/domain/model.go", "random/b.go", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("%s->%s", tc.from, tc.to), func(t *testing.T) {
			t.Parallel()
			v := isLayerViolation(tc.from, tc.to)
			gotViol := v != nil
			if gotViol != tc.wantViol {
				t.Errorf("isLayerViolation(%q, %q) violation=%v, want %v",
					tc.from, tc.to, gotViol, tc.wantViol)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// matchGlobOrPrefix
// ---------------------------------------------------------------------------

func TestMatchGlobOrPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		pattern string
		want    bool
	}{
		{"internal/auth/handler.go", "internal/auth/*", true},
		{"internal/auth/handler.go", "internal/auth", true},
		{"internal/auth/sub/deep.go", "internal/auth", true},
		{"internal/other.go", "internal/auth", false},
		{"internal/auth/handler.go", "internal/auth/handler.go", true},
		{"internal/auth/handler.go", "internal/other/handler.go", false},
		{"cmd/main.go", "cmd*", true},
		{"cmd/main.go", "internal*", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("%s_%s", tc.path, tc.pattern), func(t *testing.T) {
			t.Parallel()
			if got := matchGlobOrPrefix(tc.path, tc.pattern); got != tc.want {
				t.Errorf("matchGlobOrPrefix(%q, %q) = %v, want %v", tc.path, tc.pattern, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// detectCycles
// ---------------------------------------------------------------------------

func TestDetectCycles_NoCycle(t *testing.T) {
	t.Parallel()
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
	}
	cycles := detectCycles(adj, maxCycleLen)
	if len(cycles) != 0 {
		t.Errorf("expected no cycles, got %v", cycles)
	}
}

func TestDetectCycles_SimpleCycle(t *testing.T) {
	t.Parallel()
	// a -> b -> a
	adj := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}
	cycles := detectCycles(adj, maxCycleLen)
	if len(cycles) == 0 {
		t.Error("expected a cycle, got none")
	}
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	t.Parallel()
	adj := map[string][]string{
		"a": {"a"},
	}
	cycles := detectCycles(adj, maxCycleLen)
	// Self-loop: a -> a, cycle length 1
	if len(cycles) == 0 {
		t.Error("expected self-loop cycle")
	}
}

func TestDetectCycles_ThreeNodeCycle(t *testing.T) {
	t.Parallel()
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	cycles := detectCycles(adj, maxCycleLen)
	if len(cycles) == 0 {
		t.Error("expected a 3-node cycle")
	}
}

func TestDetectCycles_Deduplication(t *testing.T) {
	t.Parallel()
	// Multiple paths but same cycle
	adj := map[string][]string{
		"a": {"b", "c"},
		"b": {"a"},
		"c": {"a"},
	}
	// There are two cycles: a->b->a and a->c->a but they're distinct
	cycles := detectCycles(adj, maxCycleLen)
	// Just ensure no panic and some result
	_ = cycles
}

func TestDetectCycles_MaxLenFilter(t *testing.T) {
	t.Parallel()
	// Long cycle: a->b->c->d->e->a (5 nodes)
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"d"},
		"d": {"e"},
		"e": {"a"},
	}
	// With maxLen=3, this 5-node cycle should be filtered out
	cycles := detectCycles(adj, 3)
	if len(cycles) != 0 {
		t.Errorf("expected 0 cycles with maxLen=3 for 5-node cycle, got %d", len(cycles))
	}

	// With maxLen=8, it should be found
	cycles = detectCycles(adj, 8)
	if len(cycles) == 0 {
		t.Error("expected cycle with maxLen=8")
	}
}

func TestDetectCycles_EmptyGraph(t *testing.T) {
	t.Parallel()
	cycles := detectCycles(map[string][]string{}, maxCycleLen)
	if len(cycles) != 0 {
		t.Errorf("expected 0 cycles for empty graph, got %d", len(cycles))
	}
}

// ---------------------------------------------------------------------------
// canonicalCycleKey
// ---------------------------------------------------------------------------

func TestCanonicalCycleKey_Empty(t *testing.T) {
	t.Parallel()
	if got := canonicalCycleKey(nil); got != "" {
		t.Errorf("expected empty for nil, got %q", got)
	}
	if got := canonicalCycleKey([]string{}); got != "" {
		t.Errorf("expected empty for empty, got %q", got)
	}
}

func TestCanonicalCycleKey_Rotation(t *testing.T) {
	t.Parallel()
	nodes1 := []string{"a", "b", "c"}
	nodes2 := []string{"b", "c", "a"} // rotation of nodes1
	nodes3 := []string{"c", "a", "b"} // another rotation

	k1 := canonicalCycleKey(nodes1)
	k2 := canonicalCycleKey(nodes2)
	k3 := canonicalCycleKey(nodes3)

	if k1 != k2 || k2 != k3 {
		t.Errorf("rotations should have same key: %q %q %q", k1, k2, k3)
	}
}

func TestCanonicalCycleKey_DifferentCycles(t *testing.T) {
	t.Parallel()
	k1 := canonicalCycleKey([]string{"a", "b"})
	k2 := canonicalCycleKey([]string{"a", "c"})
	if k1 == k2 {
		t.Errorf("different cycles should have different keys: %q = %q", k1, k2)
	}
}

// ---------------------------------------------------------------------------
// extractCycle
// ---------------------------------------------------------------------------

func TestExtractCycle_Found(t *testing.T) {
	t.Parallel()
	stack := []string{"a", "b", "c", "d"}
	cycle := extractCycle(stack, "b")
	if len(cycle) == 0 {
		t.Fatal("expected cycle")
	}
	if cycle[0] != "b" {
		t.Errorf("cycle should start at target, got %v", cycle)
	}
}

func TestExtractCycle_TargetNotInStack(t *testing.T) {
	t.Parallel()
	stack := []string{"a", "b", "c"}
	cycle := extractCycle(stack, "z")
	if cycle != nil {
		t.Errorf("expected nil when target not in stack, got %v", cycle)
	}
}

func TestExtractCycle_EmptyStack(t *testing.T) {
	t.Parallel()
	cycle := extractCycle([]string{}, "a")
	if cycle != nil {
		t.Errorf("expected nil for empty stack")
	}
}

// ---------------------------------------------------------------------------
// buildEdgeSet
// ---------------------------------------------------------------------------

func TestBuildEdgeSet(t *testing.T) {
	t.Parallel()
	bg := &BaselineGraph{
		Edges: map[string][]string{
			"a": {"b", "c"},
			"b": {"c"},
		},
	}
	set := buildEdgeSet(bg)
	expected := map[string]bool{
		"a→b": true,
		"a→c": true,
		"b→c": true,
	}
	for k, v := range expected {
		if set[k] != v {
			t.Errorf("edge %q: got %v, want %v", k, set[k], v)
		}
	}
	if len(set) != 3 {
		t.Errorf("expected 3 edges, got %d", len(set))
	}
}

func TestBuildEdgeSet_Empty(t *testing.T) {
	t.Parallel()
	set := buildEdgeSet(&BaselineGraph{Edges: map[string][]string{}})
	if len(set) != 0 {
		t.Errorf("expected empty set")
	}
}

// ---------------------------------------------------------------------------
// reachableSubgraph
// ---------------------------------------------------------------------------

func TestReachableSubgraph_Basic(t *testing.T) {
	t.Parallel()
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"d"},
		"d": {},
	}
	// Seed from "a", depth 4 — should reach b, c, d
	sub := reachableSubgraph(adj, []string{"a"}, 4)

	// "a" should be in sub (it's a seed)
	// "b", "c" should be reachable within depth 4 from "a"
	if _, ok := sub["a"]; !ok {
		// a has neighbors so should appear
		t.Error("expected 'a' in subgraph")
	}
}

func TestReachableSubgraph_DepthLimit(t *testing.T) {
	t.Parallel()
	// Chain: a -> b -> c -> d -> e
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"d"},
		"d": {"e"},
	}
	// With depth=1, from "a" we should only see "a" and "b"
	sub := reachableSubgraph(adj, []string{"a"}, 1)

	// "c", "d", "e" should NOT be reachable
	if _, ok := sub["c"]; ok {
		t.Error("'c' should not be reachable with depth=1")
	}
}

func TestReachableSubgraph_EmptySeeds(t *testing.T) {
	t.Parallel()
	adj := map[string][]string{"a": {"b"}}
	sub := reachableSubgraph(adj, []string{}, 4)
	if len(sub) != 0 {
		t.Errorf("expected empty subgraph with no seeds, got %v", sub)
	}
}

func TestReachableSubgraph_CyclicGraph(t *testing.T) {
	t.Parallel()
	// Cycle: a -> b -> c -> a
	adj := map[string][]string{
		"a": {"b"},
		"b": {"c"},
		"c": {"a"},
	}
	// Must not loop infinitely
	sub := reachableSubgraph(adj, []string{"a"}, 10)
	_ = sub // just ensure no infinite loop/panic
}

// ---------------------------------------------------------------------------
// incrementLastByte
// ---------------------------------------------------------------------------

func TestIncrementLastByte_Basic(t *testing.T) {
	t.Parallel()
	input := []byte("prefix/")
	result := incrementLastByte(input)
	if result[len(result)-1] != '/' + 1 {
		t.Errorf("expected incremented last byte, got %d", result[len(result)-1])
	}
	// Original must not be mutated
	if string(input) != "prefix/" {
		t.Error("input was mutated")
	}
}

func TestIncrementLastByte_LastByteFF(t *testing.T) {
	t.Parallel()
	input := []byte{0x61, 0xFF} // "a\xFF"
	result := incrementLastByte(input)
	// 0xFF overflows -> carry to previous byte
	if result[0] != 0x62 {
		t.Errorf("expected carry: got 0x%02x", result[0])
	}
}

func TestIncrementLastByte_AllFF(t *testing.T) {
	t.Parallel()
	input := []byte{0xFF, 0xFF}
	result := incrementLastByte(input)
	// All FF -> append 0x00
	if len(result) < len(input) {
		t.Errorf("expected at least as long as input: got len %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// WriteFanInCache
// ---------------------------------------------------------------------------

func TestWriteFanInCache(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	imports := map[string]ImportRecord{
		"a.go": {ImportedBy: []string{"b.go", "c.go"}},
		"b.go": {ImportedBy: []string{"c.go"}},
		"c.go": {ImportedBy: []string{}},
	}

	batch := db.NewBatch()
	defer batch.Close()

	if err := WriteFanInCache(batch, "repo1", "sha1", imports); err != nil {
		t.Fatalf("WriteFanInCache: %v", err)
	}
	if err := batch.Commit(pebble.Sync); err != nil {
		t.Fatalf("batch.Commit: %v", err)
	}

	// Verify fan-in key for "a.go" was written
	key := fanInKey("repo1", "sha1", "a.go")
	val, closer, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get fanInKey: %v", err)
	}
	defer closer.Close()

	var count int
	if err := json.Unmarshal(val, &count); err != nil {
		t.Fatalf("Unmarshal fanIn: %v", err)
	}
	if count != 2 {
		t.Errorf("expected fanIn=2 for a.go, got %d", count)
	}
}

func TestWriteFanInCache_EmptyImports(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	batch := db.NewBatch()
	defer batch.Close()

	if err := WriteFanInCache(batch, "repo1", "sha1", map[string]ImportRecord{}); err != nil {
		t.Fatalf("WriteFanInCache empty: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ComputeImpact
// ---------------------------------------------------------------------------

func TestComputeImpact_EmptyFiles(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	result, err := ComputeImpact(db, "repo1", "sha1", []string{})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}
	if result.NodesVisited != 0 {
		t.Errorf("expected 0 nodes for empty files, got %d", result.NodesVisited)
	}
}

func TestComputeImpact_SingleFile_NoImporters(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	result, err := ComputeImpact(db, "repo1", "sha1", []string{"a.go"})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}
	// Just the seed file itself
	if result.NodesVisited != 1 {
		t.Errorf("expected 1 node visited, got %d", result.NodesVisited)
	}
	if result.Truncated {
		t.Error("should not be truncated for 1 file")
	}
}

func TestComputeImpact_WithImporters(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"

	// a.go is imported by b.go and c.go
	writeImportRecord(t, db, repoID, sha, "a.go", ImportRecord{
		Imports:    []string{},
		ImportedBy: []string{"b.go", "c.go"},
	})
	// b.go is imported by d.go
	writeImportRecord(t, db, repoID, sha, "b.go", ImportRecord{
		Imports:    []string{"a.go"},
		ImportedBy: []string{"d.go"},
	})

	result, err := ComputeImpact(db, repoID, sha, []string{"a.go"})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}
	// Should have visited: a.go (seed), b.go, c.go, d.go
	if result.NodesVisited < 3 {
		t.Errorf("expected >= 3 nodes, got %d", result.NodesVisited)
	}
}

func TestComputeImpact_DepthLimit(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"

	// Chain: a <- b <- c <- d <- e <- f (depth 5 from a)
	chain := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go"}
	for i := 0; i < len(chain)-1; i++ {
		// chain[i] is imported by chain[i+1]
		writeImportRecord(t, db, repoID, sha, chain[i], ImportRecord{
			ImportedBy: []string{chain[i+1]},
		})
	}

	result, err := ComputeImpact(db, repoID, sha, []string{"a.go"})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}
	// BFSMaxDepth=4 — should not reach f.go (distance 5)
	for _, node := range result.Impacted {
		if node.Path == "f.go" {
			t.Error("f.go should not be reachable (beyond BFSMaxDepth)")
		}
	}
}

func TestComputeImpact_SortedOutput(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"

	// Multiple importers at distance 1
	writeImportRecord(t, db, repoID, sha, "base.go", ImportRecord{
		ImportedBy: []string{"z.go", "a.go", "m.go"},
	})

	result, err := ComputeImpact(db, repoID, sha, []string{"base.go"})
	if err != nil {
		t.Fatalf("ComputeImpact: %v", err)
	}

	// Verify sorting: distance asc, then path asc
	for i := 1; i < len(result.Impacted); i++ {
		a := result.Impacted[i-1]
		b := result.Impacted[i]
		if a.Distance > b.Distance {
			t.Errorf("not sorted by distance: index %d (dist %d) after index %d (dist %d)",
				i, b.Distance, i-1, a.Distance)
		}
		if a.Distance == b.Distance && a.Path > b.Path {
			t.Errorf("not sorted by path within same distance: %q > %q", a.Path, b.Path)
		}
	}
}

// ---------------------------------------------------------------------------
// DetectDrift
// ---------------------------------------------------------------------------

func TestDetectDrift_NoEdges_NoViolations(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	result, err := DetectDrift(db, "repo1", "sha1", "main", []string{})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(result.ForbiddenEdges) != 0 {
		t.Errorf("expected 0 forbidden edges, got %d", len(result.ForbiddenEdges))
	}
	if len(result.CrossLayerViolations) != 0 {
		t.Errorf("expected 0 cross-layer violations")
	}
	if len(result.NewEdges) != 0 {
		t.Errorf("expected 0 new edges")
	}
}

func TestDetectDrift_NewEdge_DetectedAsNew(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"
	const branch = "main"

	// Baseline has no edges
	writeBaselineGraph(t, db, repoID, branch, BaselineGraph{
		Edges: map[string][]string{},
	})

	// Both from and to can contain slashes thanks to the \x00 separator.
	writeEdge(t, db, repoID, sha, "cmd/main.go", "internal/service/svc.go")

	result, err := DetectDrift(db, repoID, sha, branch, []string{})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(result.NewEdges) != 1 {
		t.Errorf("expected 1 new edge, got %d", len(result.NewEdges))
	}
	if result.NewEdges[0].From != "cmd/main.go" || result.NewEdges[0].To != "internal/service/svc.go" {
		t.Errorf("unexpected new edge: %+v", result.NewEdges[0])
	}
}

func TestDetectDrift_EdgeInBaseline_NotNew(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"
	const branch = "main"

	// Baseline already has this edge
	writeBaselineGraph(t, db, repoID, branch, BaselineGraph{
		Edges: map[string][]string{
			"cmd/main.go": {"internal/service/svc.go"},
		},
	})
	writeEdge(t, db, repoID, sha, "cmd/main.go", "internal/service/svc.go")

	result, err := DetectDrift(db, repoID, sha, branch, []string{})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(result.NewEdges) != 0 {
		t.Errorf("expected 0 new edges (edge was in baseline), got %d", len(result.NewEdges))
	}
}

func TestDetectDrift_ForbiddenEdge_Detected(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"
	const branch = "main"

	// Policy forbids "cmd/*" -> "internal/domain*"
	writeBaselinePolicy(t, db, repoID, branch, BaselinePolicy{
		ForbiddenEdges: []ForbiddenEdge{
			{From: "cmd/*", To: "internal/domain*"},
		},
	})
	writeEdge(t, db, repoID, sha, "cmd/main.go", "internal/domain/model.go")

	result, err := DetectDrift(db, repoID, sha, branch, []string{})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(result.ForbiddenEdges) != 1 {
		t.Errorf("expected 1 forbidden edge, got %d", len(result.ForbiddenEdges))
	}
}

func TestDetectDrift_CrossLayerViolation_Detected(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"
	const branch = "main"

	// pkg (rank 15) importing internal/service (rank 20) is a layer violation.
	writeEdge(t, db, repoID, sha, "pkg/util.go", "internal/service/svc.go")

	result, err := DetectDrift(db, repoID, sha, branch, []string{})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(result.CrossLayerViolations) != 1 {
		t.Errorf("expected 1 cross-layer violation (pkg->service), got %d: %+v",
			len(result.CrossLayerViolations), result.CrossLayerViolations)
	}
}

func TestDetectDrift_Cycles_Detected(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"
	const branch = "main"

	// Create cycle: pkg/a.go -> pkg/b.go -> pkg/a.go
	writeEdge(t, db, repoID, sha, "pkg/a.go", "pkg/b.go")
	writeEdge(t, db, repoID, sha, "pkg/b.go", "pkg/a.go")

	result, err := DetectDrift(db, repoID, sha, branch, []string{"pkg/a.go", "pkg/b.go"})
	if err != nil {
		t.Fatalf("DetectDrift: %v", err)
	}
	if len(result.NewCycles) == 0 {
		t.Error("expected at least 1 cycle detected")
	}
}

// ---------------------------------------------------------------------------
// Evaluate (integration)
// ---------------------------------------------------------------------------

func TestEvaluate_MinimalInput(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	input := EvaluateInput{
		DB:           db,
		RepoID:       "repo1",
		SHA:          "abc123",
		Branch:       "main",
		ChangedFiles: []string{"cmd/main.go"},
		TotalFiles:   10,
	}

	findings, err := Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// Must produce at least the "high-impact" finding
	if len(findings) == 0 {
		t.Error("expected at least 1 finding")
	}
	if findings[0].Type != "high-impact" {
		t.Errorf("expected 'high-impact' finding, got %q", findings[0].Type)
	}
}

func TestEvaluate_FindingsSortedBySeverityDesc(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"

	// Create a cycle to get a cycle finding
	writeEdge(t, db, repoID, sha, "pkg/a.go", "pkg/b.go")
	writeEdge(t, db, repoID, sha, "pkg/b.go", "pkg/a.go")

	input := EvaluateInput{
		DB:           db,
		RepoID:       repoID,
		SHA:          sha,
		Branch:       "main",
		ChangedFiles: []string{"pkg/a.go", "pkg/b.go"},
		TotalFiles:   10,
	}

	findings, err := Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	for i := 1; i < len(findings); i++ {
		if findings[i-1].Severity < findings[i].Severity {
			t.Errorf("findings not sorted by severity desc at index %d: %v < %v",
				i, findings[i-1].Severity, findings[i].Severity)
		}
	}
}

func TestEvaluate_FindingHasRequiredFields(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	input := EvaluateInput{
		DB:           db,
		RepoID:       "repo1",
		SHA:          "sha1",
		Branch:       "main",
		ChangedFiles: []string{"cmd/main.go"},
		TotalFiles:   5,
	}

	findings, err := Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	for _, f := range findings {
		if f.ID == "" {
			t.Errorf("finding has empty ID: %+v", f)
		}
		if f.Type == "" {
			t.Errorf("finding has empty Type: %+v", f)
		}
		if f.Title == "" {
			t.Errorf("finding has empty Title: %+v", f)
		}
	}
}

func TestEvaluate_WithForbiddenEdge(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	const repoID = "repo1"
	const sha = "sha1"

	writeBaselinePolicy(t, db, repoID, "main", BaselinePolicy{
		ForbiddenEdges: []ForbiddenEdge{
			{From: "cmd/*", To: "internal/domain*"},
		},
	})
	writeEdge(t, db, repoID, sha, "cmd/main.go", "internal/domain/model.go")

	input := EvaluateInput{
		DB:           db,
		RepoID:       repoID,
		SHA:          sha,
		Branch:       "main",
		ChangedFiles: []string{"cmd/main.go"},
		TotalFiles:   10,
	}

	findings, err := Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	types := make(map[string]bool)
	for _, f := range findings {
		types[f.Type] = true
	}
	if !types["forbidden-edge"] {
		t.Error("expected a 'forbidden-edge' finding")
	}
}

func TestEvaluate_WithAckStore(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	acks := newMemAckStore()
	input := EvaluateInput{
		DB:           db,
		RepoID:       "repo1",
		SHA:          "sha1",
		Branch:       "main",
		ChangedFiles: []string{"cmd/main.go"},
		TotalFiles:   5,
		AckStore:     acks,
	}

	findings, err := Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// Must not panic with a valid ack store
	_ = findings
}

// ---------------------------------------------------------------------------
// checkForbidden
// ---------------------------------------------------------------------------

func TestCheckForbidden_NoPolicies(t *testing.T) {
	t.Parallel()
	result := checkForbidden("a.go", "b.go", &BaselinePolicy{})
	if result != nil {
		t.Errorf("expected nil with no policies, got %+v", result)
	}
}

func TestCheckForbidden_Match(t *testing.T) {
	t.Parallel()
	policy := &BaselinePolicy{
		ForbiddenEdges: []ForbiddenEdge{
			{From: "cmd", To: "internal/domain"},
		},
	}
	result := checkForbidden("cmd/main.go", "internal/domain/model.go", policy)
	if result == nil {
		t.Error("expected violation")
	}
}

func TestCheckForbidden_NoMatch(t *testing.T) {
	t.Parallel()
	policy := &BaselinePolicy{
		ForbiddenEdges: []ForbiddenEdge{
			{From: "cmd", To: "internal/domain"},
		},
	}
	result := checkForbidden("internal/service/svc.go", "internal/domain/model.go", policy)
	if result != nil {
		t.Errorf("expected no violation, got %+v", result)
	}
}

// ---------------------------------------------------------------------------
// JSON helpers (internal)
// ---------------------------------------------------------------------------

func TestJsonMarshalUnmarshal_RoundTrip(t *testing.T) {
	t.Parallel()
	type payload struct {
		X int    `json:"x"`
		Y string `json:"y"`
	}
	orig := payload{X: 42, Y: "hello"}
	data, err := jsonMarshal(orig)
	if err != nil {
		t.Fatalf("jsonMarshal: %v", err)
	}
	var got payload
	if err := jsonUnmarshal(data, &got); err != nil {
		t.Fatalf("jsonUnmarshal: %v", err)
	}
	if got != orig {
		t.Errorf("round-trip failed: got %+v, want %+v", got, orig)
	}
}

func TestJsonUnmarshal_InvalidInput(t *testing.T) {
	t.Parallel()
	var v map[string]any
	err := jsonUnmarshal([]byte("not-json"), &v)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// deduplicateCycles
// ---------------------------------------------------------------------------

func TestDeduplicateCycles(t *testing.T) {
	t.Parallel()
	cycles := []Cycle{
		{Nodes: []string{"a", "b", "c"}},
		{Nodes: []string{"b", "c", "a"}}, // same cycle, rotated
		{Nodes: []string{"x", "y"}},
	}
	unique := deduplicateCycles(cycles)
	if len(unique) != 2 {
		t.Errorf("expected 2 unique cycles, got %d: %v", len(unique), unique)
	}
}

func TestDeduplicateCycles_Empty(t *testing.T) {
	t.Parallel()
	unique := deduplicateCycles(nil)
	if len(unique) != 0 {
		t.Errorf("expected 0, got %d", len(unique))
	}
}

// ---------------------------------------------------------------------------
// buildAdjacencyFromEdges
// ---------------------------------------------------------------------------

func TestBuildAdjacencyFromEdges(t *testing.T) {
	t.Parallel()
	edges := []edgePair{
		{From: "a", To: "b"},
		{From: "a", To: "c"},
		{From: "b", To: "c"},
	}
	adj := buildAdjacencyFromEdges(edges)
	if len(adj["a"]) != 2 {
		t.Errorf("expected 2 neighbors for 'a', got %d", len(adj["a"]))
	}
	if len(adj["b"]) != 1 {
		t.Errorf("expected 1 neighbor for 'b', got %d", len(adj["b"]))
	}
}

// ---------------------------------------------------------------------------
// Stress / concurrent
// ---------------------------------------------------------------------------

func TestComputeScore_Concurrent(t *testing.T) {
	t.Parallel()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			input := ScoreInput{
				ChangedFiles:    []string{fmt.Sprintf("file%d.go", i)},
				DriftViolations: i % 5,
			}
			score := ComputeScore(input)
			if score.Total < 0 {
				panic("negative total")
			}
		}(i)
	}
	wg.Wait()
}

func TestClassifyFinding_Concurrent(t *testing.T) {
	t.Parallel()
	acks := newMemAckStore()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			score := RadarScore{Total: float64(i)}
			files := []string{fmt.Sprintf("file%d.go", i)}
			sev, notify := ClassifyFinding(score, "test", files, acks)
			_ = sev
			_ = notify
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// tempFile helper: verify os.TempDir works without tempDir helper in nested
// ---------------------------------------------------------------------------

func TestTempDirUsed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("temp dir not accessible: %v", err)
	}
}

// ---------------------------------------------------------------------------
// scoreChangeSurface edge cases
// ---------------------------------------------------------------------------

func TestScoreChangeSurface_NoFiles(t *testing.T) {
	t.Parallel()
	got := scoreChangeSurface(ScoreInput{ChangedFiles: []string{}})
	if got != 0 {
		t.Errorf("expected 0 for no files, got %v", got)
	}
}

func TestScoreChangeSurface_NoChurnRecords_DefaultsTo5(t *testing.T) {
	t.Parallel()
	// No churn records at all → churn portion defaults to 5
	got := scoreChangeSurface(ScoreInput{ChangedFiles: []string{"a.go"}})
	// fileScore = (1/30)*15 = 0.5, churnScore = 5.0 → total ~5.5
	if got < 5.0 {
		t.Errorf("expected >= 5 (churn default), got %v", got)
	}
}

func TestScoreChangeSurface_ChurnRecordForFile(t *testing.T) {
	t.Parallel()
	// High churn rate = 0.8 (== churnCap) → churnScore = 10
	got := scoreChangeSurface(ScoreInput{
		ChangedFiles: []string{"a.go"},
		ChurnRecords: map[string]ChurnRecord{
			"a.go": {ChurnRate: 0.8},
		},
	})
	if got < 10.0 {
		t.Errorf("expected >= 10 for max churn, got %v", got)
	}
}

func TestScoreChangeSurface_ExceedsFileCountCap(t *testing.T) {
	t.Parallel()
	// 60 files — beyond cap of 30; fileScore should still be capped at 15
	files := make([]string, 60)
	for i := range files {
		files[i] = fmt.Sprintf("f%d.go", i)
	}
	got := scoreChangeSurface(ScoreInput{ChangedFiles: files})
	if got > 25.0 {
		t.Errorf("ChangeSurface should not exceed 25, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// scoreCentrality edge cases
// ---------------------------------------------------------------------------

func TestScoreCentrality_NoFiles(t *testing.T) {
	t.Parallel()
	got := scoreCentrality(ScoreInput{ChangedFiles: []string{}})
	if got != 0 {
		t.Errorf("expected 0 for no files, got %v", got)
	}
}

func TestScoreCentrality_FileNotInImportRecords(t *testing.T) {
	t.Parallel()
	// File with no import record → fanIn=0
	got := scoreCentrality(ScoreInput{
		ChangedFiles:  []string{"a.go"},
		ImportRecords: map[string]ImportRecord{},
	})
	if got != 0 {
		t.Errorf("expected 0 centrality for file with no importers, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// scoreDomainSensitivity edge cases
// ---------------------------------------------------------------------------

func TestScoreDomainSensitivity_NoFiles(t *testing.T) {
	t.Parallel()
	got := scoreDomainSensitivity(ScoreInput{ChangedFiles: []string{}})
	if got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestScoreDomainSensitivity_NonSensitiveFile(t *testing.T) {
	t.Parallel()
	got := scoreDomainSensitivity(ScoreInput{ChangedFiles: []string{"internal/logger/log.go"}})
	if got != 0 {
		t.Errorf("expected 0 for non-sensitive file, got %v", got)
	}
}

func TestScoreDomainSensitivity_TokenMatch(t *testing.T) {
	t.Parallel()
	// Contains "token" in the name
	got := scoreDomainSensitivity(ScoreInput{ChangedFiles: []string{"utils/token_parser.go"}})
	if got == 0 {
		t.Error("expected non-zero for file containing token keyword")
	}
}

// ---------------------------------------------------------------------------
// scoreDriftPolicy edge cases
// ---------------------------------------------------------------------------

func TestScoreDriftPolicy_ZeroInputs(t *testing.T) {
	t.Parallel()
	got := scoreDriftPolicy(ScoreInput{})
	if got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestScoreDriftPolicy_MaxCap(t *testing.T) {
	t.Parallel()
	got := scoreDriftPolicy(ScoreInput{
		DriftViolations: 100,
		NewEdgeCount:    100,
		CycleCount:      100,
	})
	if got > 25.0 {
		t.Errorf("expected <= 25, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Key-builder helpers
// ---------------------------------------------------------------------------

func TestImpKey(t *testing.T) {
	t.Parallel()
	key := impKey("repo1", "sha1", "cmd/main.go")
	expected := "repo/repo1/snap/sha1/imp/cmd/main.go"
	if string(key) != expected {
		t.Errorf("impKey: got %q, want %q", key, expected)
	}
}

func TestFanInKey(t *testing.T) {
	t.Parallel()
	key := fanInKey("repo1", "sha1", "cmd/main.go")
	expected := "repo/repo1/snap/sha1/fanin/cmd/main.go"
	if string(key) != expected {
		t.Errorf("fanInKey: got %q, want %q", key, expected)
	}
}

func TestBaselineGraphKey(t *testing.T) {
	t.Parallel()
	key := baselineGraphKey("repo1", "main")
	expected := "repo/repo1/baseline/main/graph"
	if string(key) != expected {
		t.Errorf("baselineGraphKey: got %q, want %q", key, expected)
	}
}

func TestBaselinePolicyKey(t *testing.T) {
	t.Parallel()
	key := baselinePolicyKey("repo1", "main")
	expected := "repo/repo1/baseline/main/policy"
	if string(key) != expected {
		t.Errorf("baselinePolicyKey: got %q, want %q", key, expected)
	}
}

// ---------------------------------------------------------------------------
// Ensure Finding fields serialize correctly (round-trip check)
// ---------------------------------------------------------------------------

func TestFinding_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	f := Finding{
		ID:          "abc123def45678",
		Type:        "high-impact",
		Title:       "Change impacts 5 files",
		Description: "BFS from 2 changed files",
		Files:       []string{"a.go", "b.go"},
		Score:       RadarScore{ChangeSurface: 5, Centrality: 3, DomainSensitivity: 2, DriftPolicy: 1, Total: 11},
		Severity:    SeverityLow,
		Notify:      NotifySilent,
		AckedAt:     1234567890,
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got Finding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ID != f.ID || got.Type != f.Type || got.Severity != f.Severity {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// Verify defaultLayers coverage (smoke test for inferLayer on all default paths)
// ---------------------------------------------------------------------------

func TestDefaultLayers_AllInferrable(t *testing.T) {
	t.Parallel()
	for prefix := range defaultLayers {
		path := prefix + "/somefile.go"
		layer := inferLayer(path)
		if layer == nil {
			t.Errorf("inferLayer(%q) returned nil for default layer prefix %q", path, prefix)
		}
	}
}

// ---------------------------------------------------------------------------
// sort stability: findings sort by Severity desc, then Score.Total desc
// ---------------------------------------------------------------------------

func TestEvaluate_FindingsSortStability(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)

	input := EvaluateInput{
		DB:           db,
		RepoID:       "repo1",
		SHA:          "sha1",
		Branch:       "main",
		ChangedFiles: []string{"cmd/main.go"},
		TotalFiles:   100,
	}

	findings, err := Evaluate(input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	// Verify the sort contract
	isSorted := sort.SliceIsSorted(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity > findings[j].Severity
		}
		return findings[i].Score.Total > findings[j].Score.Total
	})
	if !isSorted {
		t.Error("findings are not correctly sorted by severity desc, then score desc")
	}
}
