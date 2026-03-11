package graph

import (
	"context"
	"testing"

	"github.com/mawkeye/flowstep/types"
)

// ─── guard stubs for Task 2 tests ─────────────────────────────────────────────

// plainGuard implements Guard but NOT NamedGuard — should fall back to %T.
type plainGuard struct{}

func (plainGuard) Check(_ context.Context, _ any, _ map[string]any) error { return nil }

// namedGuard implements both Guard and NamedGuard.
type namedGuard struct{ name string }

func (n namedGuard) Check(_ context.Context, _ any, _ map[string]any) error { return nil }
func (n namedGuard) Name() string                                            { return n.name }

// minimalDef builds a valid flat definition with 2 states and 1 transition.
func minimalDef() *types.Definition {
	return &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true},
			"DONE":    {Name: "DONE", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"process": {
				Name:        "process",
				Sources:     []string{"CREATED"},
				Target:      "DONE",
				TriggerType: types.TriggerDirect,
			},
		},
		InitialState:   "CREATED",
		TerminalStates: []string{"DONE"},
	}
}

// threeStateDef builds a flat 3-state definition for richer tests.
func threeStateDef() *types.Definition {
	return &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"CREATED":    {Name: "CREATED", IsInitial: true},
			"PROCESSING": {Name: "PROCESSING"},
			"DONE":       {Name: "DONE", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"start": {
				Name:        "start",
				Sources:     []string{"CREATED"},
				Target:      "PROCESSING",
				TriggerType: types.TriggerDirect,
			},
			"finish": {
				Name:        "finish",
				Sources:     []string{"PROCESSING"},
				Target:      "DONE",
				TriggerType: types.TriggerDirect,
			},
		},
		InitialState:   "CREATED",
		TerminalStates: []string{"DONE"},
	}
}

func TestCompile_FlatWorkflow_Basics(t *testing.T) {
	def := minimalDef()
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if cm == nil {
		t.Fatal("Compile() returned nil CompiledMachine")
	}

	// Named field (not embedded)
	if cm.Definition == nil {
		t.Error("CompiledMachine.Definition is nil")
	}
	if cm.Definition != def {
		t.Error("CompiledMachine.Definition should be the same pointer")
	}

	// Non-nil empty maps for hierarchy fields
	if cm.Ancestry == nil {
		t.Error("Ancestry should be initialized (not nil)")
	}
	if cm.InitialLeafMap == nil {
		t.Error("InitialLeafMap should be initialized (not nil)")
	}
	if cm.RegionIndex == nil {
		t.Error("RegionIndex should be initialized (not nil)")
	}
	if cm.DepthMap == nil {
		t.Error("DepthMap should be initialized (not nil)")
	}
	if cm.TransitionsByState == nil {
		t.Error("TransitionsByState should be initialized (not nil)")
	}
}

func TestCompile_FlatWorkflow_DepthMap(t *testing.T) {
	def := threeStateDef()
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	// All states in flat workflow should be at depth 0
	for stateName := range def.States {
		d, ok := cm.DepthMap[stateName]
		if !ok {
			t.Errorf("DepthMap missing state %q", stateName)
			continue
		}
		if d != 0 {
			t.Errorf("DepthMap[%q] = %d, want 0", stateName, d)
		}
	}
	if len(cm.DepthMap) != len(def.States) {
		t.Errorf("DepthMap has %d entries, want %d", len(cm.DepthMap), len(def.States))
	}
}

func TestCompile_FlatWorkflow_TransitionsByState(t *testing.T) {
	def := threeStateDef()
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	// "start" originates from CREATED
	createdTrs := cm.TransitionsByState["CREATED"]
	if len(createdTrs) != 1 {
		t.Errorf("TransitionsByState[CREATED] has %d transitions, want 1", len(createdTrs))
	} else if createdTrs[0].Def.Name != "start" {
		t.Errorf("TransitionsByState[CREATED][0].Def.Name = %q, want %q", createdTrs[0].Def.Name, "start")
	}

	// "finish" originates from PROCESSING
	processingTrs := cm.TransitionsByState["PROCESSING"]
	if len(processingTrs) != 1 {
		t.Errorf("TransitionsByState[PROCESSING] has %d transitions, want 1", len(processingTrs))
	} else if processingTrs[0].Def.Name != "finish" {
		t.Errorf("TransitionsByState[PROCESSING][0].Def.Name = %q, want %q", processingTrs[0].Def.Name, "finish")
	}

	// terminal state has no outgoing transitions
	doneTrs := cm.TransitionsByState["DONE"]
	if len(doneTrs) != 0 {
		t.Errorf("TransitionsByState[DONE] has %d transitions, want 0", len(doneTrs))
	}
}

func TestCompile_FlatWorkflow_TransitionsByState_MultiSource(t *testing.T) {
	def := &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"A": {Name: "A", IsInitial: true},
			"B": {Name: "B"},
			"C": {Name: "C", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"to_c": {
				Name:        "to_c",
				Sources:     []string{"A", "B"},
				Target:      "C",
				TriggerType: types.TriggerDirect,
			},
			"a_to_b": {
				Name:        "a_to_b",
				Sources:     []string{"A"},
				Target:      "B",
				TriggerType: types.TriggerDirect,
			},
		},
		InitialState:   "A",
		TerminalStates: []string{"C"},
	}
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	// A has 2 transitions: to_c and a_to_b
	if len(cm.TransitionsByState["A"]) != 2 {
		t.Errorf("TransitionsByState[A] has %d transitions, want 2", len(cm.TransitionsByState["A"]))
	}

	// B has 1 transition: to_c (multi-source)
	if len(cm.TransitionsByState["B"]) != 1 {
		t.Errorf("TransitionsByState[B] has %d transitions, want 1", len(cm.TransitionsByState["B"]))
	}
}

func TestCompile_DefinitionHash_Deterministic(t *testing.T) {
	def := threeStateDef()
	cm1, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("first Compile() error = %v", err)
	}
	cm2, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("second Compile() error = %v", err)
	}
	if cm1.DefinitionHash != cm2.DefinitionHash {
		t.Errorf("hash not deterministic: got %q and %q", cm1.DefinitionHash, cm2.DefinitionHash)
	}
}

func TestCompile_DefinitionHash_SameContentDifferentMaps(t *testing.T) {
	// Two separate map literals with same logical content must hash identically
	def1 := &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true},
			"DONE":    {Name: "DONE", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"process": {Name: "process", Sources: []string{"CREATED"}, Target: "DONE"},
		},
		InitialState:   "CREATED",
		TerminalStates: []string{"DONE"},
	}
	def2 := &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"DONE":    {Name: "DONE", IsTerminal: true},
			"CREATED": {Name: "CREATED", IsInitial: true},
		},
		Transitions: map[string]types.TransitionDef{
			"process": {Name: "process", Sources: []string{"CREATED"}, Target: "DONE"},
		},
		InitialState:   "CREATED",
		TerminalStates: []string{"DONE"},
	}
	cm1, err := Compile(def1, Sentinels{})
	if err != nil {
		t.Fatalf("Compile(def1) error = %v", err)
	}
	cm2, err := Compile(def2, Sentinels{})
	if err != nil {
		t.Fatalf("Compile(def2) error = %v", err)
	}
	if cm1.DefinitionHash != cm2.DefinitionHash {
		t.Errorf("same content, different map construction: hashes differ: %q vs %q", cm1.DefinitionHash, cm2.DefinitionHash)
	}
}

func TestCompile_DefinitionHash_Different(t *testing.T) {
	def1 := minimalDef()
	def2 := threeStateDef()
	cm1, err := Compile(def1, Sentinels{})
	if err != nil {
		t.Fatalf("Compile(def1) error = %v", err)
	}
	cm2, err := Compile(def2, Sentinels{})
	if err != nil {
		t.Fatalf("Compile(def2) error = %v", err)
	}
	if cm1.DefinitionHash == cm2.DefinitionHash {
		t.Error("different definitions produced the same hash")
	}
}

func TestCompile_DefinitionHash_NonEmpty(t *testing.T) {
	def := minimalDef()
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if cm.DefinitionHash == "" {
		t.Error("DefinitionHash should not be empty")
	}
}

func TestCompiledMachine_LCA_FlatWorkflow(t *testing.T) {
	def := threeStateDef()
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	// Flat workflow: no common ancestor
	ancestor, found := cm.LCA("CREATED", "DONE")
	if found {
		t.Errorf("LCA found = true, want false for flat workflow, ancestor = %q", ancestor)
	}
	if ancestor != "" {
		t.Errorf("LCA ancestor = %q, want \"\" for flat workflow", ancestor)
	}

	// Same state: in flat workflow, no ancestor
	ancestor2, found2 := cm.LCA("CREATED", "CREATED")
	if found2 {
		t.Errorf("LCA same state found = true, want false for flat workflow")
	}
	if ancestor2 != "" {
		t.Errorf("LCA same state ancestor = %q, want \"\" for flat workflow", ancestor2)
	}
}

func TestCompile_HierarchyMaps_EmptyForFlat(t *testing.T) {
	def := threeStateDef()
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	if len(cm.Ancestry) != 0 {
		t.Errorf("Ancestry should be empty for flat workflow, got len=%d", len(cm.Ancestry))
	}
	if len(cm.InitialLeafMap) != 0 {
		t.Errorf("InitialLeafMap should be empty for flat workflow, got len=%d", len(cm.InitialLeafMap))
	}
	if len(cm.RegionIndex) != 0 {
		t.Errorf("RegionIndex should be empty for flat workflow, got len=%d", len(cm.RegionIndex))
	}
}

// ─── Task 2: guard name resolution tests ─────────────────────────────────────

func TestCompile_GuardNames_NamedGuard(t *testing.T) {
	def := &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"A": {Name: "A", IsInitial: true},
			"B": {Name: "B", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"go": {
				Name:    "go",
				Sources: []string{"A"},
				Target:  "B",
				Guards:  []types.Guard{namedGuard{name: "my-guard"}},
			},
		},
		InitialState:   "A",
		TerminalStates: []string{"B"},
	}
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	trs := cm.TransitionsByState["A"]
	if len(trs) != 1 {
		t.Fatalf("expected 1 transition from A, got %d", len(trs))
	}
	ct := trs[0]
	if len(ct.GuardNames) != 1 {
		t.Fatalf("expected 1 guard name, got %d", len(ct.GuardNames))
	}
	if ct.GuardNames[0] != "my-guard" {
		t.Errorf("guard name = %q, want %q", ct.GuardNames[0], "my-guard")
	}
}

func TestCompile_GuardNames_PlainGuard_Fallback(t *testing.T) {
	def := &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"A": {Name: "A", IsInitial: true},
			"B": {Name: "B", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"go": {
				Name:    "go",
				Sources: []string{"A"},
				Target:  "B",
				Guards:  []types.Guard{plainGuard{}},
			},
		},
		InitialState:   "A",
		TerminalStates: []string{"B"},
	}
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	trs := cm.TransitionsByState["A"]
	if len(trs) != 1 {
		t.Fatalf("expected 1 transition from A, got %d", len(trs))
	}
	ct := trs[0]
	if len(ct.GuardNames) != 1 {
		t.Fatalf("expected 1 guard name (fallback), got %d", len(ct.GuardNames))
	}
	// fallback uses fmt.Sprintf("%T", guard)
	want := "graph.plainGuard"
	if ct.GuardNames[0] != want {
		t.Errorf("guard fallback name = %q, want %q", ct.GuardNames[0], want)
	}
}

func TestCompile_GuardNames_Mixed(t *testing.T) {
	def := &types.Definition{
		AggregateType: "order",
		WorkflowType:  "order-workflow",
		Version:       1,
		States: map[string]types.StateDef{
			"A": {Name: "A", IsInitial: true},
			"B": {Name: "B", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"go": {
				Name:    "go",
				Sources: []string{"A"},
				Target:  "B",
				Guards: []types.Guard{
					namedGuard{name: "first"},
					plainGuard{},
					namedGuard{name: "third"},
				},
			},
		},
		InitialState:   "A",
		TerminalStates: []string{"B"},
	}
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	trs := cm.TransitionsByState["A"]
	if len(trs) != 1 {
		t.Fatalf("expected 1 transition, got %d", len(trs))
	}
	ct := trs[0]
	if len(ct.GuardNames) != 3 {
		t.Fatalf("expected 3 guard names, got %d: %v", len(ct.GuardNames), ct.GuardNames)
	}
	if ct.GuardNames[0] != "first" {
		t.Errorf("guard[0] = %q, want %q", ct.GuardNames[0], "first")
	}
	if ct.GuardNames[1] != "graph.plainGuard" {
		t.Errorf("guard[1] = %q, want %q", ct.GuardNames[1], "graph.plainGuard")
	}
	if ct.GuardNames[2] != "third" {
		t.Errorf("guard[2] = %q, want %q", ct.GuardNames[2], "third")
	}
}

func TestCompile_CompiledTransition_GuardNamesNilFree(t *testing.T) {
	def := minimalDef()
	cm, err := Compile(def, Sentinels{})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	// Transition with no guards should have non-nil (but empty) GuardNames
	for _, trs := range cm.TransitionsByState {
		for _, ct := range trs {
			if ct.GuardNames == nil {
				t.Errorf("CompiledTransition %q: GuardNames is nil, want empty slice", ct.Def.Name)
			}
			if ct.Def == nil {
				t.Errorf("CompiledTransition %q: Def is nil", ct.Def.Name)
			}
		}
	}
}
