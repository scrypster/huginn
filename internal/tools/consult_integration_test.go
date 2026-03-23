package tools_test

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/tools"
)

func TestRegisterConsultTool_Registered(t *testing.T) {
	reg := tools.NewRegistry()
	agentReg := agents.NewRegistry()
	agentReg.Register(&agents.Agent{Name: "Mark", ModelID: "m"})

	var depth int32
	tools.RegisterBuiltins(reg, "/tmp", time.Minute)
	agents.RegisterConsultTool(reg, agentReg, nil, &depth, nil, nil, nil)

	tool, ok := reg.Get("consult_agent")
	if !ok {
		t.Fatal("consult_agent not registered")
	}
	if tool.Name() != "consult_agent" {
		t.Errorf("expected consult_agent, got %s", tool.Name())
	}
}

func TestRegisterConsultTool_NilRegistry_NoOp(t *testing.T) {
	reg := tools.NewRegistry()
	var depth int32
	// nil agentReg should be a no-op
	agents.RegisterConsultTool(reg, nil, nil, &depth, nil, nil, nil)
	_, ok := reg.Get("consult_agent")
	if ok {
		t.Error("expected consult_agent NOT registered when agentReg is nil")
	}
}
