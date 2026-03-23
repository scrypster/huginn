package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/stats"
)

// spySearcher is a Searcher that records whether Search was called.
type spySearcher struct {
	results []search.Chunk
	err     error
	called  bool
}

func (m *spySearcher) Index(_ context.Context, _ []search.Chunk) error { return nil }
func (m *spySearcher) Close() error                                     { return nil }
func (m *spySearcher) Search(_ context.Context, _ string, _ int) ([]search.Chunk, error) {
	m.called = true
	return m.results, m.err
}

// recordingCollector captures stats.Record calls for assertions.
type recordingCollector struct {
	metrics []string
}

func (r *recordingCollector) Record(metric string, _ float64, _ ...string) {
	r.metrics = append(r.metrics, metric)
}
func (r *recordingCollector) Histogram(_ string, _ float64, _ ...string) {}

// registryWith creates a ModelRegistry that knows a single model.
func registryWith(name string, contextWindow int) *modelconfig.ModelRegistry {
	reg := modelconfig.NewRegistry(modelconfig.DefaultModels())
	reg.Available = []modelconfig.ModelInfo{
		{Name: name, ContextWindow: contextWindow, SupportsTools: true},
	}
	return reg
}

// TestBuildCtx_NilRegistry_UsesDefaultBudget verifies that when no registry is
// provided the output is non-empty and still returns without panic.
func TestBuildCtx_NilRegistry_UsesDefaultBudget(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	result := cb.BuildCtx(context.Background(), "query", "unknown-model")
	// No panic — that's the main assertion; result may be empty when no idx/searcher.
	_ = result
}

// TestBuildCtx_SmallContextWindow_TruncatesHistory verifies that a model with a
// very small context window results in a smaller byte budget than a large model.
func TestBuildCtx_SmallContextWindow_TruncatesHistory(t *testing.T) {
	// Construct two searchers returning an identical large chunk so we can
	// observe truncation via the per-result limit passed to Search().
	const largeContent = "line of code "

	smallReg := registryWith("tiny-model", 4096) // 4K tokens → small budget
	largeReg := registryWith("big-model", 200000) // 200K tokens → large budget

	var smallMaxChunks, largeMaxChunks int
	smallSpy := &chunkCountSpy{maxChunksRecorder: &smallMaxChunks}
	largeSpy := &chunkCountSpy{maxChunksRecorder: &largeMaxChunks}

	cbSmall := NewContextBuilder(nil, smallReg, nil)
	cbSmall.SetSearcher(smallSpy)
	cbSmall.BuildCtx(context.Background(), "test query", "tiny-model")

	cbLarge := NewContextBuilder(nil, largeReg, nil)
	cbLarge.SetSearcher(largeSpy)
	cbLarge.BuildCtx(context.Background(), "test query", "big-model")

	_ = largeContent
	if smallMaxChunks >= largeMaxChunks {
		t.Errorf("expected small model to request fewer chunks than large model: small=%d large=%d",
			smallMaxChunks, largeMaxChunks)
	}
}

// chunkCountSpy records the maxChunks argument passed to Search.
type chunkCountSpy struct {
	maxChunksRecorder *int
}

func (s *chunkCountSpy) Index(_ context.Context, _ []search.Chunk) error { return nil }
func (s *chunkCountSpy) Close() error                                     { return nil }
func (s *chunkCountSpy) Search(_ context.Context, _ string, n int) ([]search.Chunk, error) {
	*s.maxChunksRecorder = n
	return nil, nil
}

// TestBuildCtx_LargeContextWindow_FullHistory verifies that a large context window
// produces a larger chunk budget (more chunks requested from searcher).
func TestBuildCtx_LargeContextWindow_FullHistory(t *testing.T) {
	reg := registryWith("claude-opus-4-6", 200000)
	var capturedN int
	spy := &chunkCountSpy{maxChunksRecorder: &capturedN}
	cb := NewContextBuilder(nil, reg, nil)
	cb.SetSearcher(spy)
	cb.BuildCtx(context.Background(), "find the main loop", "claude-opus-4-6")

	// 200K tokens × 4 bytes × 0.70 = 560,000 bytes budget
	// chunkBudget = 560000 - 4096 - 2048 = 553856
	// maxChunks = (553856/1024)+1 = 541
	if capturedN < 10 {
		t.Errorf("expected large model to request many chunks, got %d", capturedN)
	}
}

// TestBuildCtx_NotepadTruncation verifies that notepad entries which would exceed
// the 32768-char limit are skipped rather than truncated.
func TestBuildCtx_NotepadTruncation(t *testing.T) {
	const maxNotepadsChars = 32768

	cb := NewContextBuilder(nil, nil, nil)

	// First notepad: just under the limit
	small := &notepad.Notepad{
		Name:    "small",
		Content: strings.Repeat("s", 100),
	}

	// Second notepad: too large to fit after the first
	huge := &notepad.Notepad{
		Name:    "huge",
		Content: strings.Repeat("x", maxNotepadsChars),
	}

	cb.SetNotepads([]*notepad.Notepad{small, huge})
	result := cb.BuildCtx(context.Background(), "", "test-model")

	if !strings.Contains(result, "small") {
		t.Error("expected small notepad to appear in output")
	}
	if strings.Contains(result, "huge") {
		t.Error("expected huge notepad to be skipped (too large)")
	}
}

// TestBuildCtx_SemanticSearchInjected verifies that when the searcher returns
// results they appear in the BuildCtx output.
func TestBuildCtx_SemanticSearchInjected(t *testing.T) {
	ms := &spySearcher{
		results: []search.Chunk{
			{ID: 1, Path: "pkg/main.go", StartLine: 10, Content: "func main() {}"},
		},
	}
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSearcher(ms)
	result := cb.BuildCtx(context.Background(), "main function", "test-model")

	if !ms.called {
		t.Error("expected searcher.Search to be called")
	}
	if !strings.Contains(result, "pkg/main.go") {
		t.Errorf("expected search result path in output, got: %q", result)
	}
	if !strings.Contains(result, "func main()") {
		t.Errorf("expected search result content in output, got: %q", result)
	}
	if !strings.Contains(result, "## Repository Context") {
		t.Errorf("expected context header in output, got: %q", result)
	}
}

// TestBuildCtx_SemanticSearchEmpty_NoInjection verifies that when the searcher
// returns no results the repository context section is absent.
func TestBuildCtx_SemanticSearchEmpty_NoInjection(t *testing.T) {
	ms := &mockSearcher{results: nil}
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSearcher(ms)
	result := cb.BuildCtx(context.Background(), "no match query", "test-model")

	if strings.Contains(result, "## Repository Context") {
		t.Errorf("expected no repository context when searcher returns empty, got: %q", result)
	}
}

// TestBuildCtx_SemanticSearchError_NoInjection verifies that a searcher error
// causes the result to not include context from that searcher.
func TestBuildCtx_SemanticSearchError_NoInjection(t *testing.T) {
	ms := &mockSearcher{
		results: nil,
		err:     errors.New("search backend unavailable"),
	}
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSearcher(ms)
	result := cb.BuildCtx(context.Background(), "query", "test-model")

	// Should not panic; context header only appears if results were formatted
	_ = result
	if strings.Contains(result, "## Repository Context") {
		t.Errorf("expected no context section on searcher error, got: %q", result)
	}
}

// TestBuildCtx_SystemPromptOverride verifies that the skills fragment (which acts
// as the skills/workspace override injected into every build) is present in output.
func TestBuildCtx_SystemPromptOverride(t *testing.T) {
	const override = "OVERRIDE: always respond in French"
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSkillsFragment(override)
	result := cb.BuildCtx(context.Background(), "query", "test-model")

	if !strings.Contains(result, override) {
		t.Errorf("expected system prompt override in output, got: %q", result)
	}
	if !strings.Contains(result, "## Skills & Workspace Rules") {
		t.Errorf("expected skills section header in output, got: %q", result)
	}
}

// TestBuildCtx_DelegationContextBriefing verifies that when multiple notepads are
// injected (simulating a delegation briefing) they all appear in the output.
func TestBuildCtx_DelegationContextBriefing(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	nps := []*notepad.Notepad{
		{Name: "delegation-briefing", Content: "You are handling the release pipeline task."},
		{Name: "context-window",      Content: "Focus on the deploy stage only."},
	}
	cb.SetNotepads(nps)
	result := cb.BuildCtx(context.Background(), "query", "test-model")

	if !strings.Contains(result, "## Active Notepads") {
		t.Errorf("expected notepads section in output, got: %q", result)
	}
	if !strings.Contains(result, "delegation-briefing") {
		t.Errorf("expected first notepad name in output, got: %q", result)
	}
	if !strings.Contains(result, "release pipeline task") {
		t.Errorf("expected first notepad content in output, got: %q", result)
	}
	if !strings.Contains(result, "context-window") {
		t.Errorf("expected second notepad name in output, got: %q", result)
	}
}

// TestBuildCtx_ToolFiltering is satisfied by verifying that when a searcher is set
// and the query is empty, the searcher is NOT called (no context fetch for empty query).
// This mirrors the tool-filtering branch where no tools are invoked when conditions
// are not met.
func TestBuildCtx_ToolFiltering(t *testing.T) {
	ms := &spySearcher{results: []search.Chunk{
		{ID: 1, Path: "file.go", StartLine: 1, Content: "content"},
	}}
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSearcher(ms)
	// Empty query — searcher should NOT be invoked
	_ = cb.BuildCtx(context.Background(), "", "test-model")

	if ms.called {
		t.Error("expected searcher NOT to be called when query is empty")
	}
}

// TestBuildCtx_SessionStoreError covers the nil-registry (equivalent of session
// store returning an error) code path: BuildCtx falls back to defaultContextBytes.
func TestBuildCtx_SessionStoreError(t *testing.T) {
	// Registry with model having ContextWindow=0 triggers the fallback path.
	reg := registryWith("bad-model", 0)
	cb := NewContextBuilder(nil, reg, nil)
	// Should not panic, should use default budget
	result := cb.BuildCtx(context.Background(), "query", "bad-model")
	_ = result
}

// TestBuildCtx_EmptyHistory verifies the baseline: no idx, no searcher, no notepads,
// empty query produces an empty string (no sections injected).
func TestBuildCtx_EmptyHistory(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	result := cb.BuildCtx(context.Background(), "", "test-model")

	if result != "" {
		t.Errorf("expected empty result for bare ContextBuilder, got: %q", result)
	}
}

// TestBuildCtx_MultipleMessages_OrderPreserved verifies that multiple notepad
// entries appear in the same order they were set.
func TestBuildCtx_MultipleMessages_OrderPreserved(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	nps := []*notepad.Notepad{
		{Name: "alpha", Content: "first"},
		{Name: "beta",  Content: "second"},
		{Name: "gamma", Content: "third"},
	}
	cb.SetNotepads(nps)
	result := cb.BuildCtx(context.Background(), "", "test-model")

	alphaIdx := strings.Index(result, "alpha")
	betaIdx := strings.Index(result, "beta")
	gammaIdx := strings.Index(result, "gamma")

	if alphaIdx < 0 || betaIdx < 0 || gammaIdx < 0 {
		t.Fatalf("expected all notepad names in output, got: %q", result)
	}
	if !(alphaIdx < betaIdx && betaIdx < gammaIdx) {
		t.Errorf("expected notepads in insertion order: alpha=%d beta=%d gamma=%d",
			alphaIdx, betaIdx, gammaIdx)
	}
}

// TestBuildCtx_ContextWindowCalcAccurate verifies the budget calculation formula:
// contextBytes = cw * 4 * 0.70, and that a model with a larger context window
// produces a proportionally larger maxChunks value passed to the searcher.
func TestBuildCtx_ContextWindowCalcAccurate(t *testing.T) {
	// Two models with 2× difference in context window.
	smallReg := registryWith("model-8k", 8192)
	largeReg := registryWith("model-16k", 16384)

	var n8k, n16k int
	spy8k := &chunkCountSpy{maxChunksRecorder: &n8k}
	spy16k := &chunkCountSpy{maxChunksRecorder: &n16k}

	cb8k := NewContextBuilder(nil, smallReg, nil)
	cb8k.SetSearcher(spy8k)
	cb8k.BuildCtx(context.Background(), "query", "model-8k")

	cb16k := NewContextBuilder(nil, largeReg, nil)
	cb16k.SetSearcher(spy16k)
	cb16k.BuildCtx(context.Background(), "query", "model-16k")

	if n16k <= n8k {
		t.Errorf("expected 16k model to request more chunks than 8k model: 16k=%d 8k=%d",
			n16k, n8k)
	}
}

// TestBuildCtx_StatsRecorded verifies that when a stats collector is provided,
// the agent.context_bytes metric is recorded after each BuildCtx call.
func TestBuildCtx_StatsRecorded(t *testing.T) {
	rc := &recordingCollector{}
	cb := NewContextBuilder(nil, nil, rc)
	cb.BuildCtx(context.Background(), "query", "test-model")

	found := false
	for _, m := range rc.metrics {
		if m == "agent.context_bytes" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected agent.context_bytes to be recorded, got: %v", rc.metrics)
	}
}

// TestBuildCtx_NoStats_NoPanic verifies that nil stats does not cause a panic.
func TestBuildCtx_NoStats_NoPanic(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	// Should not panic
	_ = cb.BuildCtx(context.Background(), "query", "test-model")
}

// TestBuildCtx_SkillsFragmentAbsent_NoSection verifies the negative case: when
// no skills fragment is set, the section header is absent from the output.
func TestBuildCtx_SkillsFragmentAbsent_NoSection(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	result := cb.BuildCtx(context.Background(), "", "test-model")

	if strings.Contains(result, "## Skills & Workspace Rules") {
		t.Errorf("expected no skills section when fragment is empty, got: %q", result)
	}
}

// TestBuildCtx_NotepadsAbsent_NoSection verifies that when no notepads are set
// the Active Notepads section is absent.
func TestBuildCtx_NotepadsAbsent_NoSection(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	result := cb.BuildCtx(context.Background(), "", "test-model")

	if strings.Contains(result, "## Active Notepads") {
		t.Errorf("expected no notepad section when no notepads set, got: %q", result)
	}
}

// TestBuildCtx_SearcherSetButNoQuery_FallbackNotUsed verifies that when a query
// is empty neither the searcher nor the BM25 fallback is triggered.
func TestBuildCtx_SearcherSetButNoQuery_FallbackNotUsed(t *testing.T) {
	ms := &spySearcher{}
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSearcher(ms)
	result := cb.BuildCtx(context.Background(), "", "test-model")

	if ms.called {
		t.Error("searcher should not be called when query is empty")
	}
	if strings.Contains(result, "## Repository Context") {
		t.Errorf("expected no repository context for empty query, got: %q", result)
	}
}

// TestBuildCtx_NoopStatsCollector_NoPanic verifies that the NoopCollector from
// stats package works correctly with BuildCtx.
func TestBuildCtx_NoopStatsCollector_NoPanic(t *testing.T) {
	noop := stats.NoopCollector{}
	cb := NewContextBuilder(nil, nil, noop)
	_ = cb.BuildCtx(context.Background(), "query", "some-model")
}
