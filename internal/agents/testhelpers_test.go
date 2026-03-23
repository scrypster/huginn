package agents_test

import "github.com/scrypster/huginn/internal/agents"

// def is the shared package-level test fixture for AgentDef.
// Skills is nil and MemoryMode is "" (zero values).
// Tests requiring non-zero Skills or specific MemoryMode define their own local def.
var def = agents.AgentDef{
	Name:      "X",
	Model:     "m",
	IsDefault: true,
}

// cfg is a shared package-level test fixture for AgentsConfig.
var cfg = &agents.AgentsConfig{
	Agents: []agents.AgentDef{def},
}

// d is a package-level fixture for validation tests (valid name and model).
var d = agents.AgentDef{
	Name:  "ValidName",
	Model: "test-model",
}

// agent is used by perfile tests — name "atomic-test" produces filename "atomic-test.json".
var agent = agents.AgentDef{
	Name:  "atomic-test",
	Model: "m",
}

// first and second are used by overwrite tests — both produce "overwrite-me.json".
var first = agents.AgentDef{
	Name:  "overwrite-me",
	Model: "model-v1",
}
var second = agents.AgentDef{
	Name:  "overwrite-me",
	Model: "model-v2",
}

// target is used by concurrent delete tests.
var target = agents.AgentDef{
	Name:  "delete-target",
	Model: "m",
}

// testAgents is used by coverage_boost tests.
var testAgents = []agents.AgentDef{
	{Name: "Alpha", Model: "alpha-model"},
	{Name: "Beta", Model: "beta-model"},
}
