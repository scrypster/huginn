package threadmgr

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/workforce"
)

func TestDelegateToAgentTool_HopCapStopsRecursion(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston", "Sam", "coder"}})
	reg := companyDelegateRegistry()
	tool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("s", "space", tm, reg)}

	dc := workforce.NewDelegationContext("req", "Winston", 2)
	next, err := dc.WithDelegate("Sam")
	if err != nil {
		t.Fatal(err)
	}
	ctx := workforce.WithDelegationContext(context.Background(), &next)

	result := tool.Execute(ctx, map[string]any{"agent": "coder", "task": "more"})
	if !result.IsError {
		t.Fatal("hop cap must stop A2A recurse")
	}
	if !strings.Contains(result.Error, "max delegation depth") && !strings.Contains(result.Error, "depth") {
		t.Fatalf("want depth error, got %q", result.Error)
	}
	if n := len(tm.ListBySession("s")); n != 0 {
		t.Fatalf("must not spawn past hop cap, got %d", n)
	}
}

func TestDelegateToAgentTool_FirstHopAllowed(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston", "Sam"}})
	reg := companyDelegateRegistry()
	tool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("s", "space", tm, reg)}
	ctx := SetCallingAgent(context.Background(), "Winston")
	result := tool.Execute(ctx, map[string]any{"agent": "Sam", "task": "help"})
	if result.IsError {
		t.Fatalf("first hop Winston→Sam must work, got %s", result.Error)
	}
	if n := len(tm.ListBySession("s")); n != 1 {
		t.Fatalf("want 1 thread, got %d", n)
	}
}

func TestDelegateToAgentTool_CycleRejected(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Winston", "Sam"}})
	reg := companyDelegateRegistry()
	tool := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("s", "space", tm, reg)}
	dc := workforce.NewDelegationContext("req", "Winston", 5)
	next, err := dc.WithDelegate("Sam")
	if err != nil {
		t.Fatal(err)
	}
	ctx := workforce.WithDelegationContext(context.Background(), &next)
	result := tool.Execute(ctx, map[string]any{"agent": "Winston", "task": "back"})
	if !result.IsError {
		t.Fatal("cycle Winston→Sam→Winston must fail")
	}
	if !strings.Contains(result.Error, "cycle") {
		t.Fatalf("want cycle error, got %q", result.Error)
	}
}

func TestPushDelegateHop_CarryRoundTrip(t *testing.T) {
	ctx := SetCallingAgent(context.Background(), "Winston")
	ctx, err := PushDelegateHop(ctx, "Sam")
	if err != nil {
		t.Fatal(err)
	}
	dst := CarryDelegationContext(context.Background(), ctx)
	dc := workforce.GetDelegationContext(dst)
	if dc == nil || len(dc.Stack) < 2 {
		t.Fatalf("carried hop stack missing: %+v", dc)
	}
	if dc.Stack[len(dc.Stack)-1] != "Sam" {
		t.Fatalf("want Sam on stack, got %v", dc.Stack)
	}
}
