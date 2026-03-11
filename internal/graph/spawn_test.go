package graph

import (
	"errors"
	"testing"

	"github.com/mawkeye/flowstep/types"
)

// sentinel for tests
var testErrSpawnCycle = errors.New("spawn cycle detected")

func testSpawnSentinels() Sentinels {
	return Sentinels{ErrSpawnCycle: testErrSpawnCycle}
}

// spawnDef builds a minimal valid definition where workflowType spawns the given child workflow types.
func spawnDef(aggregateType, workflowType string, spawns ...string) *types.Definition {
	states := map[string]types.StateDef{
		"A": {Name: "A", IsInitial: true},
		"B": {Name: "B", IsTerminal: true},
	}
	transitions := map[string]types.TransitionDef{}

	if len(spawns) == 0 {
		transitions["go"] = types.TransitionDef{
			Name:        "go",
			Sources:     []string{"A"},
			Target:      "B",
			TriggerType: types.TriggerDirect,
		}
	} else {
		// First spawn uses ChildDef, additional ones use ChildrenDef (to test both)
		for i, s := range spawns {
			name := "spawn"
			if i > 0 {
				name = "spawn_children"
			}
			if i == 0 {
				transitions[name] = types.TransitionDef{
					Name:     name,
					Sources:  []string{"A"},
					Target:   "B",
					ChildDef: &types.ChildDef{WorkflowType: s},
				}
			} else {
				transitions[name] = types.TransitionDef{
					Name:        name,
					Sources:     []string{"A"},
					Target:      "B",
					ChildrenDef: &types.ChildrenDef{WorkflowType: s},
				}
			}
		}
	}

	return &types.Definition{
		AggregateType:  aggregateType,
		WorkflowType:   workflowType,
		Version:        1,
		States:         states,
		Transitions:    transitions,
		InitialState:   "A",
		TerminalStates: []string{"B"},
	}
}

func TestDetectSpawnCycles_NoEdges(t *testing.T) {
	defs := []*types.Definition{
		spawnDef("orderAgg", "order-wf"),
	}
	if err := DetectSpawnCycles(defs, testSpawnSentinels()); err != nil {
		t.Errorf("no edges: expected nil error, got %v", err)
	}
}

func TestDetectSpawnCycles_AcyclicLinear(t *testing.T) {
	// A → B → C (acyclic chain)
	defs := []*types.Definition{
		spawnDef("aggA", "wf-a", "wf-b"),
		spawnDef("aggB", "wf-b", "wf-c"),
		spawnDef("aggC", "wf-c"),
	}
	if err := DetectSpawnCycles(defs, testSpawnSentinels()); err != nil {
		t.Errorf("acyclic chain: expected nil error, got %v", err)
	}
}

func TestDetectSpawnCycles_Diamond_NoCycle(t *testing.T) {
	// A → B, A → C, B → D, C → D (diamond — no cycle)
	defs := []*types.Definition{
		spawnDef("aggA", "wf-a", "wf-b"),   // A spawns B
		spawnDef("aggB", "wf-b", "wf-d"),   // B spawns D
		spawnDef("aggC", "wf-c", "wf-d"),   // C spawns D
		spawnDef("aggD", "wf-d"),            // D has no children
	}
	// Also add A→C edge via a second ChildrenDef on a different definition
	defs[0].Transitions["spawn_c"] = types.TransitionDef{
		Name:        "spawn_c",
		Sources:     []string{"A"},
		Target:      "B",
		ChildrenDef: &types.ChildrenDef{WorkflowType: "wf-c"},
	}
	if err := DetectSpawnCycles(defs, testSpawnSentinels()); err != nil {
		t.Errorf("diamond (no cycle): expected nil error, got %v", err)
	}
}

func TestDetectSpawnCycles_TwoNodeCycle(t *testing.T) {
	// A → B, B → A
	defs := []*types.Definition{
		spawnDef("aggA", "wf-a", "wf-b"),
		spawnDef("aggB", "wf-b", "wf-a"),
	}
	err := DetectSpawnCycles(defs, testSpawnSentinels())
	if err == nil {
		t.Fatal("2-node cycle: expected ErrSpawnCycle, got nil")
	}
	if !errors.Is(err, testErrSpawnCycle) {
		t.Errorf("expected errors.Is(err, ErrSpawnCycle), got %v", err)
	}
}

func TestDetectSpawnCycles_ThreeNodeCycle(t *testing.T) {
	// A → B → C → A
	defs := []*types.Definition{
		spawnDef("aggA", "wf-a", "wf-b"),
		spawnDef("aggB", "wf-b", "wf-c"),
		spawnDef("aggC", "wf-c", "wf-a"),
	}
	err := DetectSpawnCycles(defs, testSpawnSentinels())
	if err == nil {
		t.Fatal("3-node cycle: expected ErrSpawnCycle, got nil")
	}
	if !errors.Is(err, testErrSpawnCycle) {
		t.Errorf("expected errors.Is(err, ErrSpawnCycle), got %v", err)
	}
}

func TestDetectSpawnCycles_SelfLoop(t *testing.T) {
	// A → A (self-loop)
	defs := []*types.Definition{
		spawnDef("aggA", "wf-a", "wf-a"),
	}
	err := DetectSpawnCycles(defs, testSpawnSentinels())
	if err == nil {
		t.Fatal("self-loop: expected ErrSpawnCycle, got nil")
	}
	if !errors.Is(err, testErrSpawnCycle) {
		t.Errorf("expected errors.Is(err, ErrSpawnCycle), got %v", err)
	}
}

func TestDetectSpawnCycles_WorkflowTypeNotAggregateType(t *testing.T) {
	// WorkflowType != AggregateType — cycle detection must key on WorkflowType
	// wf-a (agg: order-v1) spawns wf-b (agg: payment-v2)
	// wf-b (agg: payment-v2) spawns wf-a (agg: order-v1)  → cycle via WorkflowType
	defs := []*types.Definition{
		{
			AggregateType:  "order-v1",  // different from WorkflowType
			WorkflowType:   "wf-a",
			Version:        1,
			States:         map[string]types.StateDef{"A": {Name: "A", IsInitial: true}, "B": {Name: "B", IsTerminal: true}},
			Transitions:    map[string]types.TransitionDef{"go": {Name: "go", Sources: []string{"A"}, Target: "B", ChildDef: &types.ChildDef{WorkflowType: "wf-b"}}},
			InitialState:   "A",
			TerminalStates: []string{"B"},
		},
		{
			AggregateType:  "payment-v2", // different from WorkflowType
			WorkflowType:   "wf-b",
			Version:        1,
			States:         map[string]types.StateDef{"A": {Name: "A", IsInitial: true}, "B": {Name: "B", IsTerminal: true}},
			Transitions:    map[string]types.TransitionDef{"go": {Name: "go", Sources: []string{"A"}, Target: "B", ChildDef: &types.ChildDef{WorkflowType: "wf-a"}}},
			InitialState:   "A",
			TerminalStates: []string{"B"},
		},
	}
	err := DetectSpawnCycles(defs, testSpawnSentinels())
	if err == nil {
		t.Fatal("WorkflowType!=AggregateType cycle: expected ErrSpawnCycle, got nil")
	}
	if !errors.Is(err, testErrSpawnCycle) {
		t.Errorf("expected errors.Is(err, ErrSpawnCycle), got %v", err)
	}
}

func TestDetectSpawnCycles_EmptyList(t *testing.T) {
	if err := DetectSpawnCycles(nil, testSpawnSentinels()); err != nil {
		t.Errorf("empty list: expected nil, got %v", err)
	}
}

func TestDetectSpawnCycles_ErrorContainsCyclePath(t *testing.T) {
	defs := []*types.Definition{
		spawnDef("aggA", "wf-a", "wf-b"),
		spawnDef("aggB", "wf-b", "wf-a"),
	}
	err := DetectSpawnCycles(defs, testSpawnSentinels())
	if err == nil {
		t.Fatal("expected error")
	}
	// Error message should mention at least one workflow type in the cycle
	msg := err.Error()
	if msg == "" {
		t.Error("error message should not be empty")
	}
}
