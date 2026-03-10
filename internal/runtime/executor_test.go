package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/mawkeye/flowstep/types"
)

// ─── nil aggregate tests ──────────────────────────────────────────────────────

type aggregateCapturingGuard struct {
	captured any
}

func (g *aggregateCapturingGuard) Check(_ context.Context, aggregate any, _ map[string]any) error {
	g.captured = aggregate
	return nil
}

func TestValidateTransition_passesInstanceToGuard(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	guard := &aggregateCapturingGuard{}
	def := &types.Definition{
		WorkflowType:   "test",
		AggregateType:  "TestAgg",
		InitialState:   "pending",
		TerminalStates: []string{"done"},
		States: map[string]types.StateDef{
			"pending": {Name: "pending"},
			"done":    {Name: "done", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"complete": {
				Name:    "complete",
				Sources: []string{"pending"},
				Target:  "done",
				Event:   "Completed",
				Guards:  []types.Guard{guard},
			},
		},
	}
	inst := simpleInstance("pending")

	_, _, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if guard.captured == nil {
		t.Error("guard received nil aggregate — expected *WorkflowInstance")
	}
	if _, ok := guard.captured.(*types.WorkflowInstance); !ok {
		t.Errorf("expected *WorkflowInstance, got %T", guard.captured)
	}
}

// ─── validateTransition tests ─────────────────────────────────────────────────

func TestValidateTransition_valid(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("pending")

	tr, target, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if tr.Name != "complete" {
		t.Errorf("expected tr.Name=complete, got %q", tr.Name)
	}
	if target != "done" {
		t.Errorf("expected target=done, got %q", target)
	}
}

func TestValidateTransition_unknownTransition(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("pending")

	_, _, err := e.validateTransition(context.Background(), def, inst, "nonexistent", nil)
	if !errors.Is(err, errInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestValidateTransition_wrongSourceState(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("done") // terminal — but also wrong source

	_, _, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if err == nil {
		t.Fatal("expected error for wrong source state, got nil")
	}
}

func TestValidateTransition_alreadyTerminal(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("done")

	_, _, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if !errors.Is(err, errAlreadyTerminal) {
		t.Errorf("expected ErrAlreadyTerminal, got %v", err)
	}
}
