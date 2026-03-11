package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/internal/graph"
	"github.com/mawkeye/flowstep/types"
)

// ─── resolveRoute tests ───────────────────────────────────────────────────────

type alwaysTrue struct{}

func (a *alwaysTrue) Evaluate(_ context.Context, _ any, _ map[string]any) (bool, error) {
	return true, nil
}

type alwaysFalse struct{}

func (a *alwaysFalse) Evaluate(_ context.Context, _ any, _ map[string]any) (bool, error) {
	return false, nil
}

func TestResolveRoute_firstMatchWins(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	tr := types.TransitionDef{
		Routes: []types.Route{
			{Condition: &alwaysTrue{}, Target: "state-a"},
			{Condition: &alwaysTrue{}, Target: "state-b"},
		},
	}
	target, err := e.resolveRoute(context.Background(), tr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "state-a" {
		t.Errorf("expected state-a, got %s", target)
	}
}

func TestResolveRoute_fallsToDefault(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	tr := types.TransitionDef{
		Routes: []types.Route{
			{Condition: &alwaysFalse{}, Target: "state-a"},
			{IsDefault: true, Target: "default-state"},
		},
	}
	target, err := e.resolveRoute(context.Background(), tr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "default-state" {
		t.Errorf("expected default-state, got %s", target)
	}
}

func TestResolveRoute_noMatchReturnsError(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	tr := types.TransitionDef{
		Routes: []types.Route{
			{Condition: &alwaysFalse{}, Target: "state-a"},
		},
	}
	_, err := e.resolveRoute(context.Background(), tr, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─── runGuards tests ──────────────────────────────────────────────────────────

type failingGuard struct{ msg string }

func (f *failingGuard) Check(_ context.Context, _ any, _ map[string]any) error {
	return errors.New(f.msg)
}

func TestRunGuards_allPass(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	ct := &graph.CompiledTransition{
		Def:        &types.TransitionDef{Name: "go", Guards: []types.Guard{&aggregateCapturingGuard{}}},
		GuardNames: []string{"aggregateCapturingGuard"},
	}
	if err := e.runGuards(context.Background(), "wf", ct, nil, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRunGuards_firstFailureReturnsErrGuardFailed(t *testing.T) {
	obs := &capturingGuardObserver{}
	e := New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        newMemInstanceStore(errInstanceNotFound),
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		Observers:            NewObserverRegistry([]types.Observer{obs}),
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
	})
	ct := &graph.CompiledTransition{
		Def:        &types.TransitionDef{Name: "go", Guards: []types.Guard{&failingGuard{"blocked"}}},
		GuardNames: []string{"*runtime.failingGuard"},
	}
	err := e.runGuards(context.Background(), "my-workflow", ct, nil, nil)
	if !errors.Is(err, types.ErrGuardFailed) {
		t.Errorf("expected types.ErrGuardFailed, got %v", err)
	}
	if obs.guardFailedWorkflow != "my-workflow" {
		t.Errorf("expected OnGuardFailed called with workflow=my-workflow, got %q", obs.guardFailedWorkflow)
	}
	if obs.guardFailedTransition != "go" {
		t.Errorf("expected OnGuardFailed called with transition=go, got %q", obs.guardFailedTransition)
	}
	if obs.guardFailedErr == nil {
		t.Error("expected OnGuardFailed to receive the guard error, got nil")
	}
}

type namedFailingGuard struct{}

func (n *namedFailingGuard) Check(_ context.Context, _ any, _ map[string]any) error {
	return errors.New("denied")
}

func (n *namedFailingGuard) Name() string { return "my-custom-guard" }

func TestRunGuards_usesPrecomputedGuardName(t *testing.T) {
	obs := &capturingGuardObserver{}
	e := New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        newMemInstanceStore(errInstanceNotFound),
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		Observers:            NewObserverRegistry([]types.Observer{obs}),
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
	})
	guard := &namedFailingGuard{}
	ct := &graph.CompiledTransition{
		Def: &types.TransitionDef{
			Name:   "go",
			Guards: []types.Guard{guard},
		},
		GuardNames: []string{"my-custom-guard"},
	}
	err := e.runGuards(context.Background(), "my-workflow", ct, nil, nil)
	if !errors.Is(err, types.ErrGuardFailed) {
		t.Errorf("expected types.ErrGuardFailed, got %v", err)
	}
	if obs.guardFailedGuardName != "my-custom-guard" {
		t.Errorf("expected guardName=my-custom-guard, got %q", obs.guardFailedGuardName)
	}
}

// ─── copyMap tests ────────────────────────────────────────────────────────────

func TestCopyMap_nil(t *testing.T) {
	if copyMap(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestCopyMap_shallow(t *testing.T) {
	orig := map[string]any{"a": 1, "b": "hello"}
	cp := copyMap(orig)
	if cp["a"] != 1 || cp["b"] != "hello" {
		t.Error("shallow copy values wrong")
	}
	// Modifying copy must not affect original
	cp["a"] = 99
	if orig["a"] != 1 {
		t.Error("copy shares reference with original")
	}
}

func TestCopyMap_deepNested(t *testing.T) {
	orig := map[string]any{"nested": map[string]any{"x": 1}}
	cp := copyMap(orig)
	nested := cp["nested"].(map[string]any)
	nested["x"] = 99
	origNested := orig["nested"].(map[string]any)
	if origNested["x"] != 1 {
		t.Error("nested map is not a deep copy")
	}
}

// ─── Task 7: computeExitSequence + computeEntrySequence tests ─────────────────

// twoLevelAncestry: PROCESSING→[VALIDATING, APPROVED], both children's ancestry = [PROCESSING]
func twoLevelAncestry() map[string][]string {
	return map[string][]string{
		"VALIDATING": {"PROCESSING"},
		"APPROVED":   {"PROCESSING"},
	}
}

// threeLevelAncestry: ROOT→ORDER→[VALIDATING, APPROVED]
func threeLevelAncestry() map[string][]string {
	return map[string][]string{
		"ORDER":      {"ROOT"},
		"VALIDATING": {"ORDER", "ROOT"},
		"APPROVED":   {"ORDER", "ROOT"},
	}
}

func TestComputeExitSequence_SiblingTransition(t *testing.T) {
	// VALIDATING→APPROVED, LCA=PROCESSING: exit only VALIDATING (not PROCESSING)
	got := computeExitSequence("VALIDATING", "PROCESSING", twoLevelAncestry())
	want := []string{"VALIDATING"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("exit seq = %v, want %v", got, want)
	}
}

func TestComputeExitSequence_FlatWorkflow(t *testing.T) {
	// No ancestry, lca="" — exit only source
	got := computeExitSequence("CREATED", "", map[string][]string{})
	if len(got) != 1 || got[0] != "CREATED" {
		t.Errorf("exit seq = %v, want [CREATED]", got)
	}
}

func TestComputeExitSequence_ThreeLevel(t *testing.T) {
	// VALIDATING→APPROVED, LCA=ORDER: exit [VALIDATING]
	got := computeExitSequence("VALIDATING", "ORDER", threeLevelAncestry())
	want := []string{"VALIDATING"}
	if len(got) != 1 || got[0] != "VALIDATING" {
		t.Errorf("exit seq = %v, want %v", got, want)
	}
}

func TestComputeExitSequence_CrossHierarchy(t *testing.T) {
	// VALIDATING→APPROVED, LCA=ROOT: exit [VALIDATING, ORDER]
	got := computeExitSequence("VALIDATING", "ROOT", threeLevelAncestry())
	want := []string{"VALIDATING", "ORDER"}
	if len(got) != len(want) {
		t.Fatalf("exit seq = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("exit seq[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestComputeEntrySequence_SiblingTransition(t *testing.T) {
	// LCA=PROCESSING, target=APPROVED: enter [APPROVED] only
	got := computeEntrySequence("PROCESSING", "APPROVED", twoLevelAncestry())
	want := []string{"APPROVED"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("entry seq = %v, want %v", got, want)
	}
}

func TestComputeEntrySequence_FlatToHierarchy(t *testing.T) {
	// LCA="" (flat source), target=VALIDATING: enter [PROCESSING, VALIDATING] (full path)
	got := computeEntrySequence("", "VALIDATING", twoLevelAncestry())
	want := []string{"PROCESSING", "VALIDATING"}
	if len(got) != len(want) {
		t.Fatalf("entry seq = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry seq[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// ─── Task 8: collectSubtreeStates tests ───────────────────────────────────────

func flatDef() *types.Definition {
	return &types.Definition{
		States: map[string]types.StateDef{
			"CREATED":    {Name: "CREATED"},
			"PROCESSING": {Name: "PROCESSING"},
			"DONE":       {Name: "DONE"},
		},
	}
}

func hierarchyDef() *types.Definition {
	return &types.Definition{
		States: map[string]types.StateDef{
			"PROCESSING": {Name: "PROCESSING", IsCompound: true, Children: []string{"VALIDATING", "APPROVED"}},
			"VALIDATING": {Name: "VALIDATING", Parent: "PROCESSING"},
			"APPROVED":   {Name: "APPROVED", Parent: "PROCESSING"},
		},
	}
}

func TestCollectSubtreeStates_LeafState_ReturnsSelf(t *testing.T) {
	got := collectSubtreeStates([]string{"VALIDATING"}, flatDef())
	if len(got) != 1 || got[0] != "VALIDATING" {
		t.Errorf("expected [VALIDATING], got %v", got)
	}
}

func TestCollectSubtreeStates_CompoundState_IncludesChildren(t *testing.T) {
	got := collectSubtreeStates([]string{"PROCESSING"}, hierarchyDef())
	wantSet := map[string]bool{"PROCESSING": true, "VALIDATING": true, "APPROVED": true}
	if len(got) != len(wantSet) {
		t.Fatalf("expected 3 states, got %v", got)
	}
	for _, s := range got {
		if !wantSet[s] {
			t.Errorf("unexpected state %q in result", s)
		}
	}
}

func TestCollectSubtreeStates_MultipleRoots_Union(t *testing.T) {
	got := collectSubtreeStates([]string{"VALIDATING", "PROCESSING"}, hierarchyDef())
	// VALIDATING subtree = {VALIDATING}, PROCESSING subtree = {PROCESSING, VALIDATING, APPROVED}
	// Union = {PROCESSING, VALIDATING, APPROVED}
	wantSet := map[string]bool{"PROCESSING": true, "VALIDATING": true, "APPROVED": true}
	if len(got) != len(wantSet) {
		t.Fatalf("expected 3 states, got %v", got)
	}
	for _, s := range got {
		if !wantSet[s] {
			t.Errorf("unexpected state %q in result", s)
		}
	}
}

func TestCollectSubtreeStates_EmptyRoots(t *testing.T) {
	got := collectSubtreeStates([]string{}, flatDef())
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestComputeEntrySequence_ThreeLevel_FlatSource(t *testing.T) {
	// LCA="", target=VALIDATING: enter [ROOT, ORDER, VALIDATING]
	got := computeEntrySequence("", "VALIDATING", threeLevelAncestry())
	want := []string{"ROOT", "ORDER", "VALIDATING"}
	if len(got) != len(want) {
		t.Fatalf("entry seq = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry seq[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
