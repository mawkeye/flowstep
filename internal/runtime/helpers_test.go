package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

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
	tr := types.TransitionDef{Name: "go", Guards: []types.Guard{&aggregateCapturingGuard{}}}
	if err := e.runGuards(context.Background(), "wf", tr, nil, nil); err != nil {
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
	tr := types.TransitionDef{
		Name:   "go",
		Guards: []types.Guard{&failingGuard{"blocked"}},
	}
	err := e.runGuards(context.Background(), "my-workflow", tr, nil, nil)
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
