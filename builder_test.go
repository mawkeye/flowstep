package flowstep

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

// ─── Task 2: Builder DSL hierarchy tests ─────────────────────────────────────

func TestState_WithParentModifier(t *testing.T) {
	so := State("VALIDATING", Parent("PROCESSING"))
	if so.Def().Name != "VALIDATING" {
		t.Errorf("Name = %q, want VALIDATING", so.Def().Name)
	}
	if so.Def().Parent != "PROCESSING" {
		t.Errorf("Parent = %q, want PROCESSING", so.Def().Parent)
	}
}

func TestState_WithEntryExitActivity(t *testing.T) {
	so := State("PROCESSING", EntryActivityOpt("on_enter"), ExitActivityOpt("on_exit"))
	if so.Def().EntryActivity != "on_enter" {
		t.Errorf("EntryActivity = %q, want on_enter", so.Def().EntryActivity)
	}
	if so.Def().ExitActivity != "on_exit" {
		t.Errorf("ExitActivity = %q, want on_exit", so.Def().ExitActivity)
	}
}

func TestCompoundState_SetsIsCompoundAndInitialChild(t *testing.T) {
	so := CompoundState("PROCESSING", InitialChild("VALIDATING"))
	if !so.Def().IsCompound {
		t.Error("IsCompound = false, want true")
	}
	if so.Def().InitialChild != "VALIDATING" {
		t.Errorf("InitialChild = %q, want VALIDATING", so.Def().InitialChild)
	}
}

func TestBuild_PopulatesChildrenFromParent(t *testing.T) {
	def, err := Define("order", "hierarchical").
		States(
			Initial("CREATED"),
			CompoundState("PROCESSING", InitialChild("VALIDATING")),
			State("VALIDATING", Parent("PROCESSING")),
			State("APPROVED", Parent("PROCESSING")),
			Terminal("DONE"),
		).
		Transition("start", From("CREATED"), To("PROCESSING"), Event("Start")).
		Transition("approve", From("VALIDATING"), To("APPROVED"), Event("Approve")).
		Transition("finish", From("APPROVED"), To("DONE"), Event("Finish")).
		Build()

	if err != nil {
		t.Fatalf("unexpected Build error: %v", err)
	}

	processing := def.States["PROCESSING"]
	if !processing.IsCompound {
		t.Error("PROCESSING.IsCompound should be true")
	}
	if len(processing.Children) != 2 {
		t.Errorf("PROCESSING.Children len = %d, want 2", len(processing.Children))
	}

	validating := def.States["VALIDATING"]
	if validating.Parent != "PROCESSING" {
		t.Errorf("VALIDATING.Parent = %q, want PROCESSING", validating.Parent)
	}
}

func TestBuild_BackwardCompatible_FlatWorkflow(t *testing.T) {
	// Existing flat workflow should build without changes
	def, err := Define("order", "flat").
		States(Initial("CREATED"), State("PROCESSING"), Terminal("DONE")).
		Transition("start", From("CREATED"), To("PROCESSING"), Event("Start")).
		Transition("finish", From("PROCESSING"), To("DONE"), Event("Finish")).
		Build()

	if err != nil {
		t.Fatalf("flat workflow Build() failed: %v", err)
	}
	for name, st := range def.States {
		if st.Parent != "" {
			t.Errorf("state %q.Parent = %q, want empty for flat workflow", name, st.Parent)
		}
		if len(st.Children) != 0 {
			t.Errorf("state %q.Children = %v, want empty for flat workflow", name, st.Children)
		}
	}
}

// ─── Task 4 (history states): Builder WithHistory tests ──────────────────────

func TestWithHistory_Shallow_SetsHistoryMode(t *testing.T) {
	def, err := Define("order", "history-shallow").
		States(
			Initial("CREATED"),
			CompoundState("PROCESSING", InitialChild("VALIDATING")),
			State("VALIDATING", Parent("PROCESSING")),
			Terminal("DONE"),
		).
		Transition("start", From("CREATED"), To("PROCESSING"), Event("Start")).
		Transition("resume", From("CREATED"), To("PROCESSING"), WithHistory(HistoryShallow)).
		Transition("approve", From("VALIDATING"), To("DONE"), Event("Approve")).
		Build()

	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	tr := def.Transitions["resume"]
	if tr.HistoryMode != HistoryShallow {
		t.Errorf("HistoryMode = %q, want %q", tr.HistoryMode, HistoryShallow)
	}
}

func TestWithHistory_Deep_SetsHistoryMode(t *testing.T) {
	def, err := Define("order", "history-deep").
		States(
			Initial("CREATED"),
			CompoundState("PROCESSING", InitialChild("VALIDATING")),
			State("VALIDATING", Parent("PROCESSING")),
			Terminal("DONE"),
		).
		Transition("start", From("CREATED"), To("PROCESSING"), Event("Start")).
		Transition("resume", From("CREATED"), To("PROCESSING"), WithHistory(HistoryDeep)).
		Transition("approve", From("VALIDATING"), To("DONE"), Event("Approve")).
		Build()

	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	tr := def.Transitions["resume"]
	if tr.HistoryMode != HistoryDeep {
		t.Errorf("HistoryMode = %q, want %q", tr.HistoryMode, HistoryDeep)
	}
}

func TestTransition_WithoutHistory_HasEmptyHistoryMode(t *testing.T) {
	def, err := Define("order", "no-history").
		States(Initial("CREATED"), Terminal("DONE")).
		Transition("finish", From("CREATED"), To("DONE"), Event("Finish")).
		Build()

	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}
	tr := def.Transitions["finish"]
	if tr.HistoryMode != "" {
		t.Errorf("HistoryMode = %q, want empty", tr.HistoryMode)
	}
}
