package flowstate

import (
	"errors"
	"testing"
)

func TestBuildMinimalWorkflow(t *testing.T) {
	def, err := Define("order", "simple").
		Version(1).
		States(
			Initial("CREATED"),
			Terminal("DONE"),
		).
		Transition("complete",
			From("CREATED"),
			To("DONE"),
			Event("OrderCompleted"),
		).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.AggregateType != "order" {
		t.Errorf("expected order, got %s", def.AggregateType)
	}
	if def.WorkflowType != "simple" {
		t.Errorf("expected simple, got %s", def.WorkflowType)
	}
	if def.Version != 1 {
		t.Errorf("expected version 1, got %d", def.Version)
	}
	if def.InitialState != "CREATED" {
		t.Errorf("expected CREATED, got %s", def.InitialState)
	}
	if len(def.TerminalStates) != 1 || def.TerminalStates[0] != "DONE" {
		t.Errorf("expected [DONE], got %v", def.TerminalStates)
	}
}

func TestBuildNoInitialState(t *testing.T) {
	_, err := Define("order", "bad").
		States(Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Build()

	if !errors.Is(err, ErrNoInitialState) {
		t.Errorf("expected ErrNoInitialState, got %v", err)
	}
}

func TestBuildMultipleInitialStates(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), Initial("B"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Build()

	if !errors.Is(err, ErrMultipleInitialStates) {
		t.Errorf("expected ErrMultipleInitialStates, got %v", err)
	}
}

func TestBuildNoTerminalStates(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), State("B")).
		Transition("x", From("A"), To("B"), Event("X")).
		Build()

	if !errors.Is(err, ErrNoTerminalStates) {
		t.Errorf("expected ErrNoTerminalStates, got %v", err)
	}
}

func TestBuildUnreachableState(t *testing.T) {
	// B is unreachable but has an outgoing transition (so not a dead end)
	_, err := Define("order", "bad").
		States(Initial("A"), State("B"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Transition("y", From("B"), To("DONE"), Event("Y")).
		Build()

	if !errors.Is(err, ErrUnreachableState) {
		t.Errorf("expected ErrUnreachableState, got %v", err)
	}
}

func TestBuildDeadEndState(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), State("B"), Terminal("DONE")).
		Transition("x", From("A"), To("B"), Event("X")).
		Build()

	if !errors.Is(err, ErrDeadEndState) {
		t.Errorf("expected ErrDeadEndState, got %v", err)
	}
}

func TestBuildUnknownState(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), Terminal("DONE")).
		Transition("x", From("A"), To("UNKNOWN"), Event("X")).
		Build()

	if !errors.Is(err, ErrUnknownState) {
		t.Errorf("expected ErrUnknownState, got %v", err)
	}
}

func TestBuildDuplicateTransition(t *testing.T) {
	_, err := Define("order", "bad").
		States(Initial("A"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Transition("x", From("A"), To("DONE"), Event("Y")).
		Build()

	if !errors.Is(err, ErrDuplicateTransition) {
		t.Errorf("expected ErrDuplicateTransition, got %v", err)
	}
}

func TestBuildDefaultVersion(t *testing.T) {
	def, err := Define("order", "simple").
		States(Initial("A"), Terminal("DONE")).
		Transition("x", From("A"), To("DONE"), Event("X")).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Version != 1 {
		t.Errorf("expected default version 1, got %d", def.Version)
	}
}

func TestBuildMultiSourceTransition(t *testing.T) {
	def, err := Define("order", "multi").
		States(Initial("A"), State("B"), Terminal("DONE")).
		Transition("ab", From("A"), To("B"), Event("AB")).
		Transition("finish", From("A", "B"), To("DONE"), Event("Finished")).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tr := def.Transitions["finish"]
	if len(tr.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(tr.Sources))
	}
}
