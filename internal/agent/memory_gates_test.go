package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/tools"
)

type gateStubTool struct {
	name   string
	mu     sync.Mutex
	calls  []map[string]any
	fail   bool
	down   bool
	failN  int // fail the first N calls, then succeed
	failed int
}

func (t *gateStubTool) Name() string                      { return t.name }
func (t *gateStubTool) Description() string               { return t.name }
func (t *gateStubTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *gateStubTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *gateStubTool) Execute(_ context.Context, args map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := map[string]any{}
	for k, v := range args {
		cp[k] = v
	}
	t.calls = append(t.calls, cp)
	if t.down {
		return tools.ToolResult{IsError: true, Error: "connection refused"}
	}
	if t.fail {
		return tools.ToolResult{IsError: true, Error: "muninn rejected write"}
	}
	if t.failN > 0 && t.failed < t.failN {
		t.failed++
		return tools.ToolResult{IsError: true, Error: "muninn rejected write"}
	}
	return tools.ToolResult{Output: `{"id":"mem_1"}`}
}

func (t *gateStubTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

func (t *gateStubTool) lastVault() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.calls) == 0 {
		return ""
	}
	v, _ := t.calls[len(t.calls)-1]["vault"].(string)
	return v
}

func muninnSchemaReg(names ...string) *tools.Registry {
	reg := tools.NewRegistry()
	for _, n := range names {
		reg.Register(&gateStubTool{name: n})
	}
	reg.TagTools(names, "muninndb")
	return reg
}

func TestPinMuninnVault_EmptyAndDefaultBecomeHuginn(t *testing.T) {
	if got := pinMuninnVault(""); got != "huginn" {
		t.Fatalf("empty vault: got %q want huginn", got)
	}
	if got := pinMuninnVault("default"); got != "huginn" {
		t.Fatalf("default vault: got %q want huginn", got)
	}
	if got := pinMuninnVault("DEFAULT"); got != "huginn" {
		t.Fatalf("DEFAULT vault: got %q want huginn", got)
	}
	if got := pinMuninnVault("huginn:agent:mj-bonanno:winston"); got != "huginn" {
		t.Fatalf("colon agent vault: got %q want huginn", got)
	}
	if got := pinMuninnVault("huginn"); got != "huginn" {
		t.Fatalf("huginn rewritten: %q", got)
	}
}

func TestPrefetch_ArgsAlwaysVaultNotDefault(t *testing.T) {
	t.Parallel()
	o := orchForPrefetch(t)
	where := &gateStubTool{name: "muninn_where_left_off"}
	recall := &gateStubTool{name: "muninn_recall"}
	reg := tools.NewRegistry()
	reg.Register(where)
	reg.Register(recall)

	var saw []string
	cb := func(_ string, args map[string]any, _ string, _ bool) {
		v, _ := args["vault"].(string)
		saw = append(saw, v)
		if v == "" || strings.EqualFold(v, "default") {
			t.Errorf("prefetch leaked vault %q", v)
		}
	}
	for _, vault := range []string{"", "default", "DEFAULT"} {
		_ = o.prefetchMemoryContextWithEvents(context.Background(), reg, "pin-agent", vault, "where did we leave the wringer note", cb)
	}
	if where.callCount() == 0 && recall.callCount() == 0 {
		t.Fatal("expected prefetch to invoke muninn tools")
	}
	for _, v := range saw {
		if v != "huginn" {
			t.Errorf("callback vault = %q want huginn", v)
		}
	}
	if where.lastVault() != "huginn" {
		t.Errorf("where_left_off vault = %q want huginn", where.lastVault())
	}
	if recall.lastVault() != "huginn" {
		t.Errorf("recall vault = %q want huginn", recall.lastVault())
	}
}

func TestApplyToolbelt_PassiveStripsWriteTools(t *testing.T) {
	names := []string{
		"muninn_recall", "muninn_where_left_off", "muninn_guide",
		"muninn_remember", "muninn_evolve", "muninn_decide", "muninn_trust",
		"muninn_remember_batch", "muninn_create_workflow_vault",
	}
	reg := muninnSchemaReg(names...)
	ag := &agents.Agent{Name: "passive-bot", MemoryMode: "passive"}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	got := map[string]bool{}
	for _, s := range schemas {
		got[s.Function.Name] = true
	}
	for _, must := range []string{"muninn_recall", "muninn_where_left_off", "muninn_guide"} {
		if !got[must] {
			t.Errorf("passive schema missing read tool %s", must)
		}
	}
	for _, mustNot := range []string{
		"muninn_remember", "muninn_evolve", "muninn_decide", "muninn_trust",
		"muninn_remember_batch", "muninn_create_workflow_vault",
	} {
		if got[mustNot] {
			t.Errorf("passive schema still has %s", mustNot)
		}
	}
}

func TestApplyToolbelt_CreateWorkflowVaultHiddenEveryMode(t *testing.T) {
	names := []string{"muninn_recall", "muninn_remember", "muninn_create_workflow_vault"}
	reg := muninnSchemaReg(names...)
	for _, mode := range []string{"", "passive", "conversational", "immersive"} {
		ag := &agents.Agent{Name: "bot", MemoryMode: mode}
		schemas, _ := applyToolbelt(ag, reg, nil, nil)
		for _, s := range schemas {
			if s.Function.Name == muninnCreateWorkflowVault {
				t.Fatalf("mode %q leaked muninn_create_workflow_vault", mode)
			}
		}
	}
}

func TestApplyToolbelt_ConversationalKeepsWrites(t *testing.T) {
	names := []string{"muninn_recall", "muninn_remember", "muninn_evolve", "muninn_decide"}
	reg := muninnSchemaReg(names...)
	ag := &agents.Agent{Name: "bot", MemoryMode: "conversational"}
	schemas, _ := applyToolbelt(ag, reg, nil, nil)
	got := map[string]bool{}
	for _, s := range schemas {
		got[s.Function.Name] = true
	}
	for _, must := range names {
		if !got[must] {
			t.Errorf("conversational missing %s", must)
		}
	}
}

func TestConversational_NewFactWithoutModelWrite_HarnessPersist(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember"}
	reg := tools.NewRegistry()
	reg.Register(remember)

	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		Agent:     "Winston",
		UserMsg:   "Please remember this: the wringer tag is immersion-gates-2026.",
		Assistant: "Got it, I'll keep the wringer tag in mind.",
		Registry:  reg,
	})
	if !dec.Receipt || dec.Source != "harness" {
		t.Fatalf("decision = %+v, want harness receipt", dec)
	}
	if !dec.Emit {
		t.Fatal("conversational must emit even if we had to persist")
	}
	if remember.callCount() != 1 {
		t.Fatalf("remember called %d times, want 1", remember.callCount())
	}
	if remember.lastVault() != "huginn" {
		t.Fatalf("harness persist vault = %q", remember.lastVault())
	}
}

func TestConversational_ModelWriteCountsAsReceipt(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember"}
	reg := tools.NewRegistry()
	reg.Register(remember)

	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		UserMsg:   "Remember this: cats like boxes.",
		Assistant: "Noted.",
		Messages: []backend.Message{{
			Role: "assistant",
			ToolCalls: []backend.ToolCall{{
				Function: backend.ToolCallFunction{Name: "muninn_remember"},
			}},
		}},
		Registry: reg,
	})
	if dec.Source != "model" || !dec.Receipt {
		t.Fatalf("want model receipt, got %+v", dec)
	}
	if remember.callCount() != 0 {
		t.Fatalf("harness must not double-write, calls=%d", remember.callCount())
	}
}

func TestImmersive_NoCloseWithoutReceiptWhenMuninnUp(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember", fail: true}
	reg := tools.NewRegistry()
	reg.Register(remember)

	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "immersive",
		Vault:     "huginn",
		UserMsg:   "The release train is Friday.",
		Assistant: "Friday release train is the plan we will track.",
		Registry:  reg,
	})
	if dec.Emit {
		t.Fatalf("immersive must hold close when Muninn is up and write failed: %+v", dec)
	}
	if dec.Receipt {
		t.Fatal("failed write is not a receipt")
	}
	if dec.Down {
		t.Fatal("rejected write is not Muninn-down")
	}
	if remember.callCount() == 0 {
		t.Fatal("expected harness persist attempt")
	}
}

func TestImmersive_MuninnDownStillEmits(t *testing.T) {
	t.Run("tool missing", func(t *testing.T) {
		reg := tools.NewRegistry()
		dec := applyMemoryGate(context.Background(), memoryGateInput{
			Mode:      "immersive",
			Vault:     "huginn",
			UserMsg:   "The release train is Friday.",
			Assistant: "Friday release train is the plan we will track.",
			Registry:  reg,
		})
		if !dec.Emit {
			t.Fatalf("must emit when Muninn is down (no tool): %+v", dec)
		}
		if !dec.Down {
			t.Fatalf("expected Down: %+v", dec)
		}
	})
	t.Run("connection refused", func(t *testing.T) {
		remember := &gateStubTool{name: "muninn_remember", down: true}
		reg := tools.NewRegistry()
		reg.Register(remember)
		dec := applyMemoryGate(context.Background(), memoryGateInput{
			Mode:      "immersive",
			Vault:     "huginn",
			UserMsg:   "The release train is Friday.",
			Assistant: "Friday release train is the plan we will track.",
			Registry:  reg,
		})
		if !dec.Emit {
			t.Fatalf("must emit when Muninn connection is dead: %+v", dec)
		}
		if !dec.Down {
			t.Fatalf("expected Down: %+v", dec)
		}
	})
}

func TestConversational_PersistFailRetriesThenEmits(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember", fail: true}
	reg := tools.NewRegistry()
	reg.Register(remember)
	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		UserMsg:   "Remember this: wringer token is not a secret, just a tag.",
		Assistant: "I'll remember the wringer tag.",
		Registry:  reg,
	})
	if !dec.Emit {
		t.Fatalf("conversational must continue after persist fail: %+v", dec)
	}
	if dec.Receipt {
		t.Fatal("failed persist is not a receipt")
	}
	if remember.callCount() < 2 {
		t.Fatalf("expected retry, calls=%d", remember.callCount())
	}
}

func TestPassive_NoHarnessPersist(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember"}
	reg := tools.NewRegistry()
	reg.Register(remember)
	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "passive",
		Vault:     "huginn",
		UserMsg:   "Remember this: should not persist in passive.",
		Assistant: "Understood.",
		Registry:  reg,
	})
	if !dec.Emit || dec.Receipt || remember.callCount() != 0 {
		t.Fatalf("passive must skip persist: dec=%+v calls=%d", dec, remember.callCount())
	}
}

func TestPrefetch_ImmersiveGuideOncePerSession(t *testing.T) {
	o := orchForPrefetch(t)
	where := &gateStubTool{name: "muninn_where_left_off"}
	recall := &gateStubTool{name: "muninn_recall"}
	guide := &gateStubTool{name: "muninn_guide"}
	reg := tools.NewRegistry()
	reg.Register(where)
	reg.Register(recall)
	reg.Register(guide)

	ctx := WithMemoryGate(context.Background(), "immersive", "sess-guide-1", "Winston")
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Winston", "huginn", "hello again", nil)
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Winston", "huginn", "hello again later", nil)
	if guide.callCount() != 1 {
		t.Fatalf("guide calls=%d want 1 (once per session)", guide.callCount())
	}
	if guide.lastVault() != "huginn" {
		t.Fatalf("guide vault=%q", guide.lastVault())
	}
}

func TestAppendHistoryHonoringGate_HoldCloseSkipsAssistant(t *testing.T) {
	sess := newSession("gate-sess")
	appendHistoryHonoringGate(sess, "hi", "secret findings", nil, true)
	hist := sess.snapshotHistory()
	if len(hist) != 1 || hist[0].Role != "user" {
		t.Fatalf("hold-close history = %#v", hist)
	}
	appendHistoryHonoringGate(sess, "hi2", "visible", nil, false)
	hist = sess.snapshotHistory()
	if len(hist) != 3 {
		t.Fatalf("after emit, history len=%d", len(hist))
	}
	if hist[2].Role != "assistant" || hist[2].Content != "visible" {
		t.Fatalf("assistant row = %#v", hist[2])
	}
}

func TestLoopMemoryGate_ImmersiveHoldClose(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember", fail: true}
	reg := tools.NewRegistry()
	reg.Register(remember)
	res := &LoopResult{
		FinalContent: "Friday release train is the plan we will track.",
		Messages:     []backend.Message{{Role: "assistant", Content: "Friday release train is the plan we will track."}},
	}
	applyLoopMemoryGate(context.Background(), RunLoopConfig{
		MemoryMode:    "immersive",
		MemoryVault:   "huginn",
		MemoryUserMsg: "The release train is Friday.",
		Tools:         reg,
	}, res)
	if !res.HoldClose {
		t.Fatal("expected HoldClose when immersive persist fails and Muninn is up")
	}
	if res.MemoryReceipt {
		t.Fatal("no receipt")
	}
}

func TestLoopMemoryGate_ImmersiveDownEmits(t *testing.T) {
	res := &LoopResult{
		FinalContent: "Friday release train is the plan we will track.",
		Messages:     []backend.Message{{Role: "assistant", Content: "Friday release train is the plan we will track."}},
	}
	applyLoopMemoryGate(context.Background(), RunLoopConfig{
		MemoryMode:    "immersive",
		MemoryVault:   "huginn",
		MemoryUserMsg: "The release train is Friday.",
		Tools:         tools.NewRegistry(),
	}, res)
	if res.HoldClose {
		t.Fatal("Muninn down must still emit")
	}
}

func TestConversational_SmallTalkNoPersist(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember"}
	reg := tools.NewRegistry()
	reg.Register(remember)
	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		UserMsg:   "ok thanks",
		Assistant: "Anytime.",
		Registry:  reg,
	})
	if remember.callCount() != 0 {
		t.Fatalf("small talk must not MCP-push, calls=%d", remember.callCount())
	}
	if dec.Receipt {
		t.Fatal("small talk is not a receipt")
	}
	if !dec.Emit {
		t.Fatal("small talk must still emit")
	}
}

func TestMarkdownFallback_WhenMuninnDown(t *testing.T) {
	home := t.TempDir()
	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		Agent:     "Winston",
		Home:      home,
		UserMsg:   "Remember this: wringer tag is immersion-gates-2026.",
		Assistant: "I'll keep the wringer tag.",
		Registry:  tools.NewRegistry(),
	})
	if !dec.Emit || !dec.Receipt || dec.Source != "markdown" || !dec.Down {
		t.Fatalf("want markdown receipt while Muninn down, got %+v", dec)
	}
	got, err := mem.ReadNotes(home, "Winston")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "immersion-gates-2026") {
		t.Fatalf("MD fallback missing atomic fact, got %q", got)
	}
}

func TestMarkdownFallback_NotUsedWhenMuninnWrites(t *testing.T) {
	home := t.TempDir()
	remember := &gateStubTool{name: "muninn_remember"}
	reg := tools.NewRegistry()
	reg.Register(remember)
	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		Agent:     "Winston",
		Home:      home,
		UserMsg:   "Remember this: wringer tag is immersion-gates-2026.",
		Assistant: "I'll keep the wringer tag.",
		Registry:  reg,
	})
	if dec.Source != "harness" || !dec.Receipt {
		t.Fatalf("want harness receipt, got %+v", dec)
	}
	got, err := mem.ReadNotes(home, "Winston")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "immersion-gates-2026") {
		t.Fatal("must not also spam MD when Muninn wrote")
	}
}

func TestImmersive_MuninnUpFailDoesNotSpamMarkdown(t *testing.T) {
	home := t.TempDir()
	remember := &gateStubTool{name: "muninn_remember", fail: true}
	reg := tools.NewRegistry()
	reg.Register(remember)
	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "immersive",
		Vault:     "huginn",
		Agent:     "Winston",
		Home:      home,
		UserMsg:   "The release train is Friday.",
		Assistant: "Friday release train is the plan we will track.",
		Registry:  reg,
	})
	if dec.Emit {
		t.Fatalf("immersive hold when Muninn is up: %+v", dec)
	}
	got, _ := mem.ReadNotes(home, "Winston")
	if strings.Contains(got, "Friday") {
		t.Fatal("must not fall back to MD while Muninn is up")
	}
}

func TestPrefetch_ConversationalSkipsSmallTalkMCP(t *testing.T) {
	o := orchForPrefetch(t)
	recall := &gateStubTool{name: "muninn_recall"}
	where := &gateStubTool{name: "muninn_where_left_off"}
	reg := tools.NewRegistry()
	reg.Register(where)
	reg.Register(recall)
	ctx := WithMemoryGate(context.Background(), "conversational", "sess-c-small", "Ada")
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Ada", "huginn", "Let's plan the Friday release train", nil)
	first := recall.callCount()
	if first == 0 {
		t.Fatal("session-start conversational pull must recall")
	}
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Ada", "huginn", "ok thanks", nil)
	if recall.callCount() != first {
		t.Fatalf("small talk must not MCP-recall, first=%d after=%d", first, recall.callCount())
	}
}

func TestPrefetch_ConversationalTopicShiftPulls(t *testing.T) {
	o := orchForPrefetch(t)
	recall := &gateStubTool{name: "muninn_recall"}
	where := &gateStubTool{name: "muninn_where_left_off"}
	reg := tools.NewRegistry()
	reg.Register(where)
	reg.Register(recall)
	ctx := WithMemoryGate(context.Background(), "conversational", "sess-c-shift", "Ada")
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Ada", "huginn", "Let's plan the Friday release train", nil)
	first := recall.callCount()
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Ada", "huginn", "Switching topics: how do we rotate the CI token?", nil)
	if recall.callCount() <= first {
		t.Fatalf("topic shift must pull again, first=%d after=%d", first, recall.callCount())
	}
}

func TestPrefetch_PassiveSessionStartOnly(t *testing.T) {
	o := orchForPrefetch(t)
	recall := &gateStubTool{name: "muninn_recall"}
	where := &gateStubTool{name: "muninn_where_left_off"}
	reg := tools.NewRegistry()
	reg.Register(where)
	reg.Register(recall)
	ctx := WithMemoryGate(context.Background(), "passive", "sess-p1", "Ivy")
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Ivy", "huginn", "hello there teammate", nil)
	first := recall.callCount()
	if first == 0 {
		t.Fatal("passive session-start must pull")
	}
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Ivy", "huginn", "completely different subject about rotating tokens", nil)
	if recall.callCount() != first {
		t.Fatalf("passive must not pull again, first=%d after=%d", first, recall.callCount())
	}
}

func TestPrefetch_ImmersivePullsEveryTurn(t *testing.T) {
	o := orchForPrefetch(t)
	recall := &gateStubTool{name: "muninn_recall"}
	where := &gateStubTool{name: "muninn_where_left_off"}
	reg := tools.NewRegistry()
	reg.Register(where)
	reg.Register(recall)
	ctx := WithMemoryGate(context.Background(), "immersive", "sess-i1", "Max")
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Max", "huginn", "hello there teammate", nil)
	first := recall.callCount()
	_ = o.prefetchMemoryContextWithEvents(ctx, reg, "Max", "huginn", "completely different subject about rotating tokens", nil)
	if recall.callCount() <= first {
		t.Fatalf("immersive must pull every turn, first=%d after=%d", first, recall.callCount())
	}
}

func TestTopicShifted_SmallTalkAndShift(t *testing.T) {
	if topicShifted("plan the Friday release train", "ok thanks") {
		t.Fatal("ack is not a topic shift")
	}
	if !topicShifted("plan the Friday release train", "how do we rotate the CI token") {
		t.Fatal("expected topic shift")
	}
}

func TestNotesFallbackPath_UsesAgentFile(t *testing.T) {
	home := t.TempDir()
	if !persistMarkdownFallback(home, "Winston", "wringer tag is immersion-gates-2026") {
		t.Fatal("expected MD write")
	}
	// Isolated temp dir — never the live huginn vault notes.
	if _, err := os.Stat(filepath.Join(home, "agents", "Winston.memory.md")); err != nil {
		t.Fatal(err)
	}
}

func TestVaultPinnedTool_OmitVaultRewrittenToHuginn(t *testing.T) {
	// Model-shaped muninn_recall: empty / default (Lab company vault) must
	// never reach Muninn as omit-or-default.
	inner := &gateStubTool{name: "muninn_recall"}
	pinned := &vaultPinnedTool{inner: inner, vault: pinMuninnVault("")}

	pinned.Execute(context.Background(), map[string]any{"context": []string{"harden-muninn-2026"}})
	if inner.lastVault() != "huginn" {
		t.Fatalf("omitted vault = %q want huginn", inner.lastVault())
	}

	pinned.Execute(context.Background(), map[string]any{"vault": "", "context": []string{"x"}})
	if inner.lastVault() != "huginn" {
		t.Fatalf("empty vault = %q want huginn", inner.lastVault())
	}

	pinned.Execute(context.Background(), map[string]any{"vault": "default", "context": []string{"x"}})
	if inner.lastVault() != "huginn" {
		t.Fatalf("Lab company vault default leaked as %q", inner.lastVault())
	}

	pinned.Execute(context.Background(), map[string]any{"vault": "DEFAULT", "context": []string{"x"}})
	if inner.lastVault() != "huginn" {
		t.Fatalf("DEFAULT vault leaked as %q", inner.lastVault())
	}

	inner2 := &gateStubTool{name: "muninn_recall"}
	pinnedAgent := &vaultPinnedTool{inner: inner2, vault: pinMuninnVault("huginn:agent:mj-bonanno:winston")}
	pinnedAgent.Execute(context.Background(), map[string]any{"vault": "", "context": []string{"x"}})
	if inner2.lastVault() != "huginn" {
		t.Fatalf("colon agent vault omit = %q want huginn", inner2.lastVault())
	}
	pinned.Execute(context.Background(), map[string]any{"vault": "huginn:agent:mj-bonanno:winston", "context": []string{"x"}})
	if inner.lastVault() != "huginn" {
		t.Fatalf("model-shaped colon vault = %q want huginn", inner.lastVault())
	}
	pinned.Execute(context.Background(), map[string]any{"vault": "huginn", "context": []string{"x"}})
	if inner.lastVault() != "huginn" {
		t.Fatalf("explicit huginn rewritten to %q", inner.lastVault())
	}
}

func TestPinMuninnVault_LabCompanyDefaultNotSent(t *testing.T) {
	if got := pinMuninnVault("default"); got != "huginn" {
		t.Fatalf("lab company vault default: got %q", got)
	}
}

// --- Defect 2: implicit declarative-fact memory writes ---

func TestIsDeclarativeFactAsk_ForTheRecordAndFYI(t *testing.T) {
	for _, msg := range []string{
		"@Winston for the record: our staging server is called valkyrie and deploys happen on Fridays.",
		"FYI the release branch is cut every other Thursday.",
		"just so you know, the on-call rotation starts Monday.",
		"heads up, the API key rotates monthly.",
		"Our staging server is called valkyrie.",
		"The deploy window is Fridays at 5pm.",
	} {
		if !isDeclarativeFactAsk(msg) {
			t.Errorf("%q: want declarative fact, got false", msg)
		}
	}
}

func TestIsDeclarativeFactAsk_QuestionsAndSmallTalkNotFacts(t *testing.T) {
	for _, msg := range []string{
		"What is our staging server called?",
		"Is the deploy window Fridays?",
		"good morning",
		"thanks!",
		"can you check the logs?",
	} {
		if isDeclarativeFactAsk(msg) {
			t.Errorf("%q: want not a declarative fact, got true", msg)
		}
	}
}

func TestDetectNewFact_DeclarativeFactWithoutRememberVerb(t *testing.T) {
	need, kind := detectNewFact(
		"@Winston for the record: our staging server is called valkyrie and deploys happen on Fridays.",
		"For the record: our staging server is called valkyrie and deploys happen on Fridays.",
		"conversational",
	)
	if !need || kind != "declarative_fact" {
		t.Fatalf("need=%v kind=%q, want true/declarative_fact", need, kind)
	}
}

func TestConversational_DeclarativeFactWithoutRememberVerb_HarnessPersist(t *testing.T) {
	remember := &gateStubTool{name: "muninn_remember"}
	reg := tools.NewRegistry()
	reg.Register(remember)

	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		Agent:     "Winston",
		UserMsg:   "@Winston for the record: our staging server is called valkyrie and deploys happen on Fridays.",
		Assistant: "For the record: our staging server is called valkyrie and deploys happen on Fridays.",
		Registry:  reg,
	})
	if !dec.Receipt || dec.Source != "harness" {
		t.Fatalf("decision = %+v, want harness receipt", dec)
	}
	if remember.callCount() != 1 {
		t.Fatalf("remember called %d times, want 1", remember.callCount())
	}
	content, _ := remember.calls[0]["content"].(string)
	if !strings.Contains(strings.ToLower(content), "valkyrie") {
		t.Fatalf("stored content missing fact: %q", content)
	}
	if strings.Contains(strings.ToLower(content), "for the record") {
		t.Fatalf("stored content kept the wrapper: %q", content)
	}
}

// gateRecallStubTool returns a canned recall payload instead of recording
// generic calls — the contradiction/evolve path needs muninn_recall to
// answer with a scored hit before harnessPersist decides evolve vs remember.
type gateRecallStubTool struct {
	name   string
	output string
	mu     sync.Mutex
	calls  []map[string]any
}

func (t *gateRecallStubTool) Name() string                      { return t.name }
func (t *gateRecallStubTool) Description() string               { return t.name }
func (t *gateRecallStubTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *gateRecallStubTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *gateRecallStubTool) Execute(_ context.Context, args map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, args)
	return tools.ToolResult{Output: t.output}
}
func (t *gateRecallStubTool) callCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.calls)
}

func TestContradiction_StrongRecallHitSameSubject_Evolves(t *testing.T) {
	recall := &gateRecallStubTool{name: "muninn_recall", output: `{"results":[{"id":"mem_odin","content":"my dog is named Odin","score":0.92}]}`}
	remember := &gateStubTool{name: "muninn_remember"}
	evolve := &gateStubTool{name: "muninn_evolve"}
	reg := tools.NewRegistry()
	reg.Register(recall)
	reg.Register(remember)
	reg.Register(evolve)

	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		UserMsg:   "actually my dog is named Loki.",
		Assistant: "Got it, updating your dog's name to Loki.",
		Registry:  reg,
	})
	if !dec.Receipt || dec.Source != "harness" {
		t.Fatalf("decision = %+v, want harness receipt", dec)
	}
	if evolve.callCount() != 1 {
		t.Fatalf("evolve called %d times, want 1", evolve.callCount())
	}
	if remember.callCount() != 0 {
		t.Fatalf("remember called %d times, want 0 (must evolve not duplicate)", remember.callCount())
	}
	id, _ := evolve.calls[0]["id"].(string)
	if id != "mem_odin" {
		t.Fatalf("evolve id = %q, want mem_odin", id)
	}
	content, _ := evolve.calls[0]["content"].(string)
	if !strings.Contains(strings.ToLower(content), "loki") {
		t.Fatalf("evolve content missing new fact: %q", content)
	}
}

func TestContradiction_NoPriorMemory_RemembersFirstTime(t *testing.T) {
	recall := &gateRecallStubTool{name: "muninn_recall", output: `{"results":[]}`}
	remember := &gateStubTool{name: "muninn_remember"}
	evolve := &gateStubTool{name: "muninn_evolve"}
	reg := tools.NewRegistry()
	reg.Register(recall)
	reg.Register(remember)
	reg.Register(evolve)

	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		UserMsg:   "my dog is named Odin.",
		Assistant: "Got it, your dog is named Odin.",
		Registry:  reg,
	})
	if !dec.Receipt || dec.Source != "harness" {
		t.Fatalf("decision = %+v, want harness receipt", dec)
	}
	if remember.callCount() != 1 {
		t.Fatalf("remember called %d times, want 1", remember.callCount())
	}
	if evolve.callCount() != 0 {
		t.Fatalf("evolve called %d times, want 0", evolve.callCount())
	}
}

func TestContradiction_UnrelatedRecallHit_RemembersNotEvolve(t *testing.T) {
	recall := &gateRecallStubTool{name: "muninn_recall", output: `{"results":[{"id":"mem_cat","content":"my cat is named Whiskers","score":0.95}]}`}
	remember := &gateStubTool{name: "muninn_remember"}
	evolve := &gateStubTool{name: "muninn_evolve"}
	reg := tools.NewRegistry()
	reg.Register(recall)
	reg.Register(remember)
	reg.Register(evolve)

	dec := applyMemoryGate(context.Background(), memoryGateInput{
		Mode:      "conversational",
		Vault:     "huginn",
		UserMsg:   "my dog is named Odin.",
		Assistant: "Got it, your dog is named Odin.",
		Registry:  reg,
	})
	if !dec.Receipt || dec.Source != "harness" {
		t.Fatalf("decision = %+v, want harness receipt", dec)
	}
	if remember.callCount() != 1 {
		t.Fatalf("unrelated recall hit must still remember, calls=%d", remember.callCount())
	}
	if evolve.callCount() != 0 {
		t.Fatalf("unrelated recall hit must not evolve, calls=%d", evolve.callCount())
	}
}

func TestDistillFactContent_StripsRememberWrapperAndInstructionCruft(t *testing.T) {
	got := distillFactContent("Please remember this: the wringer tag is immersion-gates-2026. Confirm in one short sentence. Do not invent other vaults.")
	want := "the wringer tag is immersion-gates-2026."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDistillFactContent_StripsForTheRecordWrapper(t *testing.T) {
	got := distillFactContent("for the record: our staging server is called valkyrie and deploys happen on Fridays.")
	want := "our staging server is called valkyrie and deploys happen on Fridays."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDistillFactContent_StackedWrappersFullyUnwrap(t *testing.T) {
	got := distillFactContent("Please remember this: for the record, the deploy window is Fridays.")
	want := "the deploy window is Fridays."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDistillFactContent_PlainFactUnchanged(t *testing.T) {
	got := distillFactContent("cats like boxes.")
	want := "cats like boxes."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestIsQuestionShaped_TrailingMarkAndInterrogativeOpeners verifies the
// question detector used to gate model-initiated memory writes: it must
// catch both a trailing '?' and a bare interrogative opener without one.
func TestIsQuestionShaped_TrailingMarkAndInterrogativeOpeners(t *testing.T) {
	yes := []string{
		"what's our production database called?",
		"@Winston what's our production database called?",
		"What is the deploy window",
		"Who owns the staging server",
		"is the CI pipeline green",
		"Can you tell me the vault name",
	}
	for _, s := range yes {
		if !isQuestionShaped(s) {
			t.Errorf("isQuestionShaped(%q) = false, want true", s)
		}
	}

	no := []string{
		"our production database is named yggdrasil",
		"for the record, the staging server is called valkyrie",
		"remember this: deploys happen on Fridays",
		"",
	}
	for _, s := range no {
		if isQuestionShaped(s) {
			t.Errorf("isQuestionShaped(%q) = true, want false", s)
		}
	}
}

// Opus-vet data-loss findings 2026-08-28.
func TestBestSubjectHit_WordBoundaryAndBand(t *testing.T) {
	hits := []recallHit{
		{ID: "dogma", Content: "my dogma is stoicism", Score: 0.9},
		{ID: "dog", Content: "my dog is named Odin", Score: 0.8},
		{ID: "weakdog", Content: "my dog likes parks", Score: 0.9, Band: "weak"},
	}
	got, ok := bestSubjectHit(hits, "my dog")
	if !ok || got.ID != "dog" {
		t.Fatalf("bestSubjectHit = %+v ok=%v, want id dog (dogma must not word-boundary-match; weak band excluded)", got, ok)
	}
}

func TestHarnessPersist_ComplementaryFactRemembersNotEvolve(t *testing.T) {
	// "my dog is a golden retriever" after "my dog is named Odin" is
	// complementary, not a correction — must ADD, never evolve-destroy.
	if looksLikeCorrection("my dog is a golden retriever") {
		t.Fatal("complementary fact misread as correction")
	}
	if !looksLikeCorrection("actually my dog is named Loki") {
		t.Fatal("explicit correction not detected")
	}
}
