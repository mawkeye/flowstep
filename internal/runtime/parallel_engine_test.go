package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/mawkeye/flowstep/internal/graph"
	"github.com/mawkeye/flowstep/types"
)

// parallelSentinels returns a Sentinels struct with all parallel-related errors set.
func parallelTestSentinels() graph.Sentinels {
	return graph.Sentinels{
		ErrNoInitialState:              errors.New("no initial state"),
		ErrMultipleInitialStates:       errors.New("multiple initial states"),
		ErrNoTerminalStates:            errors.New("no terminal states"),
		ErrUnreachableState:            errors.New("unreachable state"),
		ErrDeadEndState:                errors.New("dead end state"),
		ErrUnknownState:                errors.New("unknown state"),
		ErrMissingDefault:              errors.New("missing default"),
		ErrDuplicateTransition:         errors.New("duplicate transition"),
		ErrSpawnCycle:                  errors.New("spawn cycle"),
		ErrCompoundStateNoInitialChild: errors.New("no initial child"),
		ErrOrphanedChild:               errors.New("orphaned child"),
		ErrCircularHierarchy:           errors.New("circular hierarchy"),
		ErrParallelStateNoRegions:      errors.New("parallel no regions"),
		ErrParallelRegionNotCompound:   errors.New("region not compound"),
		ErrNestedParallelState:         errors.New("nested parallel"),
	}
}

// newParallelTestEngine creates an engine with parallel sentinel support.
func newParallelTestEngine(is types.InstanceStore) *Engine {
	return New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        is,
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{},
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
		Sentinels:            parallelTestSentinels(),
	})
}

// parallelWorkflowDef returns a valid parallel workflow definition with pre-wired
// Children slices (as Build() would produce them).
//
//	IDLE -> editing (parallel: bold_region, italic_region) -> DONE
func parallelWorkflowDef() *types.Definition {
	return &types.Definition{
		WorkflowType:  "editor",
		AggregateType: "Editor",
		Version:       1,
		States: map[string]types.StateDef{
			"IDLE":          {Name: "IDLE", IsInitial: true},
			"editing":       {Name: "editing", IsParallel: true, IsCompound: true, Children: []string{"bold_region", "italic_region"}},
			"bold_region":   {Name: "bold_region", IsCompound: true, Parent: "editing", InitialChild: "bold_off", Children: []string{"bold_off", "bold_on"}},
			"bold_off":      {Name: "bold_off", Parent: "bold_region"},
			"bold_on":       {Name: "bold_on", Parent: "bold_region"},
			"italic_region": {Name: "italic_region", IsCompound: true, Parent: "editing", InitialChild: "italic_off", Children: []string{"italic_off", "italic_on"}},
			"italic_off":    {Name: "italic_off", Parent: "italic_region"},
			"italic_on":     {Name: "italic_on", Parent: "italic_region"},
			"DONE":          {Name: "DONE", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"startEditing": {Name: "startEditing", Sources: []string{"IDLE"}, Target: "editing", TriggerType: types.TriggerDirect},
			"toggleBold":   {Name: "toggleBold", Sources: []string{"bold_off"}, Target: "bold_on", TriggerType: types.TriggerDirect},
			"untoggleBold": {Name: "untoggleBold", Sources: []string{"bold_on"}, Target: "bold_off", TriggerType: types.TriggerDirect},
			"toggleItalic":   {Name: "toggleItalic", Sources: []string{"italic_off"}, Target: "italic_on", TriggerType: types.TriggerDirect},
			"untoggleItalic": {Name: "untoggleItalic", Sources: []string{"italic_on"}, Target: "italic_off", TriggerType: types.TriggerDirect},
			"finishEditing": {Name: "finishEditing", Sources: []string{"editing"}, Target: "DONE", TriggerType: types.TriggerDirect},
		},
		InitialState:   "IDLE",
		TerminalStates: []string{"DONE"},
	}
}

// TestParallelEnter_SetsCurrentStateToParallelName verifies that transitioning INTO
// a parallel state sets CurrentState to the parallel state name (not a leaf).
func TestParallelEnter_SetsCurrentStateToParallelName(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newParallelTestEngine(is)
	ctx := context.Background()

	if err := e.Register(parallelWorkflowDef()); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	result, err := e.Transition(ctx, "Editor", "agg-1", "startEditing", "actor", nil)
	if err != nil {
		t.Fatalf("Transition() error: %v", err)
	}

	if result.Instance.CurrentState != "editing" {
		t.Errorf("CurrentState = %q, want 'editing'", result.Instance.CurrentState)
	}
}

// TestParallelEnter_PopulatesActiveInParallel verifies that entering a parallel state
// populates ActiveInParallel with each region's initial leaf.
func TestParallelEnter_PopulatesActiveInParallel(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newParallelTestEngine(is)
	ctx := context.Background()

	if err := e.Register(parallelWorkflowDef()); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	result, err := e.Transition(ctx, "Editor", "agg-1", "startEditing", "actor", nil)
	if err != nil {
		t.Fatalf("Transition() error: %v", err)
	}

	aip := result.Instance.ActiveInParallel
	if aip == nil {
		t.Fatal("ActiveInParallel is nil, want map with region entries")
	}
	if leaf, ok := aip["bold_region"]; !ok || leaf != "bold_off" {
		t.Errorf("ActiveInParallel[bold_region] = %q, want 'bold_off'", aip["bold_region"])
	}
	if leaf, ok := aip["italic_region"]; !ok || leaf != "italic_off" {
		t.Errorf("ActiveInParallel[italic_region] = %q, want 'italic_off'", aip["italic_region"])
	}
}

// TestForceState_ParallelTarget_PopulatesActiveInParallel verifies that ForceState to
// a parallel state populates ActiveInParallel.
func TestForceState_ParallelTarget_PopulatesActiveInParallel(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newParallelTestEngine(is)
	ctx := context.Background()

	def := parallelWorkflowDef()
	if err := e.Register(def); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Create an instance first (from IDLE)
	inst := &types.WorkflowInstance{
		ID:            "force-test",
		WorkflowType:  "editor",
		AggregateType: "Editor",
		AggregateID:   "agg-force",
		CurrentState:  "IDLE",
		StateData:     map[string]any{},
	}
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	result, err := e.ForceState(ctx, "Editor", "agg-force", "editing", "admin", "test")
	if err != nil {
		t.Fatalf("ForceState() error: %v", err)
	}

	if result.Instance.CurrentState != "editing" {
		t.Errorf("CurrentState = %q, want 'editing'", result.Instance.CurrentState)
	}
	aip := result.Instance.ActiveInParallel
	if aip == nil {
		t.Fatal("ActiveInParallel is nil after ForceState to parallel state")
	}
	if aip["bold_region"] != "bold_off" {
		t.Errorf("ActiveInParallel[bold_region] = %q, want 'bold_off'", aip["bold_region"])
	}
	if aip["italic_region"] != "italic_off" {
		t.Errorf("ActiveInParallel[italic_region] = %q, want 'italic_off'", aip["italic_region"])
	}
}

// setupParallelInstance creates an engine registered with parallelWorkflowDef and an
// instance already inside the parallel "editing" state.
func setupParallelInstance(t *testing.T) (*Engine, *memInstanceStore) {
	t.Helper()
	is := newMemInstanceStore(errInstanceNotFound)
	e := newParallelTestEngine(is)

	if err := e.Register(parallelWorkflowDef()); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Transition into editing so ActiveInParallel is populated
	if _, err := e.Transition(context.Background(), "Editor", "agg-1", "startEditing", "actor", nil); err != nil {
		t.Fatalf("startEditing Transition() error: %v", err)
	}
	return e, is
}

// TestParallelDispatch_SingleRegionTransition verifies that a transition sourced from a
// leaf in one region updates only that region's active leaf.
func TestParallelDispatch_SingleRegionTransition(t *testing.T) {
	e, _ := setupParallelInstance(t)
	ctx := context.Background()

	result, err := e.Transition(ctx, "Editor", "agg-1", "toggleBold", "actor", nil)
	if err != nil {
		t.Fatalf("Transition(toggleBold) error: %v", err)
	}

	if result.Instance.CurrentState != "editing" {
		t.Errorf("CurrentState = %q, want 'editing'", result.Instance.CurrentState)
	}
	aip := result.Instance.ActiveInParallel
	if aip["bold_region"] != "bold_on" {
		t.Errorf("ActiveInParallel[bold_region] = %q, want 'bold_on'", aip["bold_region"])
	}
	if aip["italic_region"] != "italic_off" {
		t.Errorf("ActiveInParallel[italic_region] = %q, want 'italic_off' (unchanged)", aip["italic_region"])
	}
}

// TestParallelDispatch_ParallelClockIncrements verifies ParallelClock increments per transition.
func TestParallelDispatch_ParallelClockIncrements(t *testing.T) {
	e, _ := setupParallelInstance(t)
	ctx := context.Background()

	result1, err := e.Transition(ctx, "Editor", "agg-1", "toggleBold", "actor", nil)
	if err != nil {
		t.Fatalf("first Transition error: %v", err)
	}
	result2, err := e.Transition(ctx, "Editor", "agg-1", "toggleItalic", "actor", nil)
	if err != nil {
		t.Fatalf("second Transition error: %v", err)
	}

	if result1.Instance.ParallelClock != 1 {
		t.Errorf("ParallelClock after 1st = %d, want 1", result1.Instance.ParallelClock)
	}
	if result2.Instance.ParallelClock != 2 {
		t.Errorf("ParallelClock after 2nd = %d, want 2", result2.Instance.ParallelClock)
	}
}

// TestParallelDispatch_ProducesParallelTransitionEvent verifies that intra-parallel
// dispatch produces a DomainEvent with EventType = "ParallelTransition".
func TestParallelDispatch_ProducesParallelTransitionEvent(t *testing.T) {
	cs := &capturingEventStore{}
	is := newMemInstanceStore(errInstanceNotFound)
	e := New(Deps{
		EventStore:           cs,
		InstanceStore:        is,
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{},
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
		Sentinels:            parallelTestSentinels(),
	})

	if err := e.Register(parallelWorkflowDef()); err != nil {
		t.Fatalf("Register() error: %v", err)
	}
	cs.appended = nil // clear registration events

	if _, err := e.Transition(context.Background(), "Editor", "agg-1", "startEditing", "actor", nil); err != nil {
		t.Fatalf("startEditing error: %v", err)
	}
	cs.appended = nil // clear entry event

	if _, err := e.Transition(context.Background(), "Editor", "agg-1", "toggleBold", "actor", nil); err != nil {
		t.Fatalf("toggleBold error: %v", err)
	}

	if len(cs.appended) != 1 {
		t.Fatalf("expected 1 event appended, got %d", len(cs.appended))
	}
	ev := cs.appended[0]
	if ev.EventType != "ParallelTransition" {
		t.Errorf("EventType = %q, want 'ParallelTransition'", ev.EventType)
	}
	// Payload must contain _parallel_regions and _region_sequence_map
	if _, ok := ev.Payload["_parallel_regions"]; !ok {
		t.Error("Payload missing '_parallel_regions'")
	}
	if _, ok := ev.Payload["_region_sequence_map"]; !ok {
		t.Error("Payload missing '_region_sequence_map'")
	}
}

// TestParallelDispatch_ExitParallelState verifies that a transition sourced from the
// parallel state itself (exitEditing sources: ["editing"]) exits all regions and clears
// ActiveInParallel.
func TestParallelDispatch_ExitParallelState(t *testing.T) {
	e, _ := setupParallelInstance(t)
	ctx := context.Background()

	result, err := e.Transition(ctx, "Editor", "agg-1", "finishEditing", "actor", nil)
	if err != nil {
		t.Fatalf("finishEditing Transition() error: %v", err)
	}

	if result.Instance.CurrentState != "DONE" {
		t.Errorf("CurrentState = %q, want 'DONE'", result.Instance.CurrentState)
	}
	if len(result.Instance.ActiveInParallel) != 0 {
		t.Errorf("ActiveInParallel = %v, want empty after exit", result.Instance.ActiveInParallel)
	}
}

// TestParallelDispatch_InvalidTransition verifies ErrInvalidTransition when no active leaf matches.
func TestParallelDispatch_InvalidTransition(t *testing.T) {
	e, _ := setupParallelInstance(t)
	ctx := context.Background()

	// "untoggleBold" sources: ["bold_on"], but current is bold_off — no match
	_, err := e.Transition(ctx, "Editor", "agg-1", "untoggleBold", "actor", nil)
	if err == nil {
		t.Fatal("expected ErrInvalidTransition, got nil")
	}
	if !errors.Is(err, errInvalidTransition) {
		t.Errorf("expected errInvalidTransition, got: %v", err)
	}
}

// parallelWorkflowForConflict extends parallelWorkflowDef with two additional transitions
// used to exercise ancestor-sourced matching and structural exit precedence:
//
//   - "boldActivate": sourced from bold_region (compound ancestor of bold_off), not the leaf.
//     Tests the ancestor-bubbling path in collectParallelCandidates.
//   - "exitOrContinue": sourced from both "editing" (parallel state) AND "bold_off" (leaf).
//     Tests that structural exit precedence (Case 1) fires before intra-region matching.
func parallelWorkflowForConflict() *types.Definition {
	def := parallelWorkflowDef()
	def.Transitions["boldActivate"] = types.TransitionDef{
		Name:        "boldActivate",
		Sources:     []string{"bold_region"},
		Target:      "bold_on",
		TriggerType: types.TriggerDirect,
		Priority:    5,
	}
	def.Transitions["exitOrContinue"] = types.TransitionDef{
		Name:        "exitOrContinue",
		Sources:     []string{"editing", "bold_off"},
		Target:      "DONE",
		TriggerType: types.TriggerDirect,
	}
	return def
}

// TestParallelConflict is a table-driven test covering conflict-resolution paths:
// ancestor-sourced matching and structural exit precedence.
func TestParallelConflict(t *testing.T) {
	tests := []struct {
		name             string
		transition       string
		wantCurrentState string
		wantBoldLeaf     string
		wantItalicLeaf   string
		wantAIPCleared   bool
	}{
		{
			// boldActivate sources: ["bold_region"] — matches bold_off via ancestor bubbling.
			// Only bold_region is updated; italic_region stays at italic_off.
			name:             "ancestor_sourced_matches_active_leaf_via_bubbling",
			transition:       "boldActivate",
			wantCurrentState: "editing",
			wantBoldLeaf:     "bold_on",
			wantItalicLeaf:   "italic_off",
		},
		{
			// exitOrContinue sources: ["editing", "bold_off"] — editing (parallel state)
			// is in sources, so structural Case 1 fires before intra-region candidate
			// matching; the workflow exits to DONE and ActiveInParallel is cleared.
			name:             "structural_exit_precedence_over_intra_region_candidate",
			transition:       "exitOrContinue",
			wantCurrentState: "DONE",
			wantAIPCleared:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := newMemInstanceStore(errInstanceNotFound)
			e := newParallelTestEngine(is)
			ctx := context.Background()

			if err := e.Register(parallelWorkflowForConflict()); err != nil {
				t.Fatalf("Register() error: %v", err)
			}
			if _, err := e.Transition(ctx, "Editor", "agg-1", "startEditing", "actor", nil); err != nil {
				t.Fatalf("startEditing: %v", err)
			}

			result, err := e.Transition(ctx, "Editor", "agg-1", tt.transition, "actor", nil)
			if err != nil {
				t.Fatalf("Transition(%q) error: %v", tt.transition, err)
			}

			if result.Instance.CurrentState != tt.wantCurrentState {
				t.Errorf("CurrentState = %q, want %q", result.Instance.CurrentState, tt.wantCurrentState)
			}
			if tt.wantAIPCleared {
				if len(result.Instance.ActiveInParallel) != 0 {
					t.Errorf("ActiveInParallel = %v, want empty after exit", result.Instance.ActiveInParallel)
				}
			} else {
				aip := result.Instance.ActiveInParallel
				if aip["bold_region"] != tt.wantBoldLeaf {
					t.Errorf("bold_region leaf = %q, want %q", aip["bold_region"], tt.wantBoldLeaf)
				}
				if aip["italic_region"] != tt.wantItalicLeaf {
					t.Errorf("italic_region leaf = %q, want %q", aip["italic_region"], tt.wantItalicLeaf)
				}
			}
		})
	}
}

// TestForceState_ClearsActiveInParallel verifies that ForceState from a parallel state
// to a non-parallel state clears ActiveInParallel.
func TestForceState_ClearsActiveInParallel(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newParallelTestEngine(is)
	ctx := context.Background()

	def := parallelWorkflowDef()
	if err := e.Register(def); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Set up instance already in parallel state
	inst := &types.WorkflowInstance{
		ID:            "clear-test",
		WorkflowType:  "editor",
		AggregateType: "Editor",
		AggregateID:   "agg-clear",
		CurrentState:  "editing",
		StateData:     map[string]any{},
		ActiveInParallel: map[string]string{
			"bold_region":   "bold_off",
			"italic_region": "italic_off",
		},
	}
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	result, err := e.ForceState(ctx, "Editor", "agg-clear", "IDLE", "admin", "test")
	if err != nil {
		t.Fatalf("ForceState() error: %v", err)
	}

	if result.Instance.CurrentState != "IDLE" {
		t.Errorf("CurrentState = %q, want 'IDLE'", result.Instance.CurrentState)
	}
	if len(result.Instance.ActiveInParallel) != 0 {
		t.Errorf("ActiveInParallel = %v, want empty after ForceState to non-parallel", result.Instance.ActiveInParallel)
	}
}
