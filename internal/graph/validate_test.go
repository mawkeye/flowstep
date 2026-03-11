package graph

import (
	"errors"
	"testing"

	"github.com/mawkeye/flowstep/types"
)

// ─── Hierarchy validation test helpers ───────────────────────────────────────

var (
	testErrCompoundNoInitialChild = errors.New("test: compound no initial child")
	testErrOrphanedChild          = errors.New("test: orphaned child")
	testErrCircularHierarchy      = errors.New("test: circular hierarchy")
)

func hierarchySentinels() Sentinels {
	return Sentinels{
		ErrNoInitialState:              errors.New("test: no initial"),
		ErrNoTerminalStates:            errors.New("test: no terminal"),
		ErrDeadEndState:                errors.New("test: dead end"),
		ErrUnreachableState:            errors.New("test: unreachable"),
		ErrUnknownState:                errors.New("test: unknown state"),
		ErrMissingDefault:              errors.New("test: missing default"),
		ErrCompoundStateNoInitialChild: testErrCompoundNoInitialChild,
		ErrOrphanedChild:               testErrOrphanedChild,
		ErrCircularHierarchy:           testErrCircularHierarchy,
	}
}

// minimalHierarchyDef builds a valid 2-level compound workflow.
//
//	CREATED (initial) -> PROCESSING (compound, initial=VALIDATING)
//	  VALIDATING (leaf) -> APPROVED (leaf, terminal)
func minimalHierarchyDef() *types.Definition {
	return &types.Definition{
		AggregateType: "order",
		WorkflowType:  "hierarchical",
		Version:       1,
		States: map[string]types.StateDef{
			"CREATED":    {Name: "CREATED", IsInitial: true},
			"PROCESSING": {Name: "PROCESSING", IsCompound: true, InitialChild: "VALIDATING", Children: []string{"VALIDATING", "APPROVED"}},
			"VALIDATING": {Name: "VALIDATING", Parent: "PROCESSING"},
			"APPROVED":   {Name: "APPROVED", Parent: "PROCESSING", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"start":   {Name: "start", Sources: []string{"CREATED"}, Target: "PROCESSING"},
			"approve": {Name: "approve", Sources: []string{"VALIDATING"}, Target: "APPROVED"},
		},
		InitialState:   "CREATED",
		TerminalStates: []string{"APPROVED"},
	}
}

// ─── checkCompoundStates tests ────────────────────────────────────────────────

func TestValidate_Hierarchy_ValidDefinition(t *testing.T) {
	def := minimalHierarchyDef()
	if err := Validate(def, hierarchySentinels()); err != nil {
		t.Errorf("valid hierarchy definition should pass validation, got: %v", err)
	}
}

func TestValidate_CompoundState_NoInitialChild(t *testing.T) {
	def := minimalHierarchyDef()
	// Remove InitialChild from PROCESSING
	proc := def.States["PROCESSING"]
	proc.InitialChild = ""
	def.States["PROCESSING"] = proc

	err := Validate(def, hierarchySentinels())
	if !errors.Is(err, testErrCompoundNoInitialChild) {
		t.Errorf("compound state without InitialChild should return ErrCompoundStateNoInitialChild, got: %v", err)
	}
}

func TestValidate_CompoundState_InitialChildNotInChildren(t *testing.T) {
	def := minimalHierarchyDef()
	proc := def.States["PROCESSING"]
	proc.InitialChild = "NONEXISTENT"
	def.States["PROCESSING"] = proc

	err := Validate(def, hierarchySentinels())
	if !errors.Is(err, testErrCompoundNoInitialChild) {
		t.Errorf("compound state with non-member InitialChild should return ErrCompoundStateNoInitialChild, got: %v", err)
	}
}

// ─── checkOrphanedChildren tests ─────────────────────────────────────────────

func TestValidate_OrphanedChild_ParentNotExist(t *testing.T) {
	def := minimalHierarchyDef()
	// Point VALIDATING to a non-existent parent
	val := def.States["VALIDATING"]
	val.Parent = "GHOST"
	def.States["VALIDATING"] = val

	err := Validate(def, hierarchySentinels())
	if !errors.Is(err, testErrOrphanedChild) {
		t.Errorf("state with non-existent Parent should return ErrOrphanedChild, got: %v", err)
	}
}

// ─── checkCompoundNotTerminal tests ──────────────────────────────────────────

func TestValidate_CompoundState_CannotBeTerminal(t *testing.T) {
	def := minimalHierarchyDef()
	proc := def.States["PROCESSING"]
	proc.IsTerminal = true
	def.States["PROCESSING"] = proc
	def.TerminalStates = append(def.TerminalStates, "PROCESSING")

	err := Validate(def, hierarchySentinels())
	// Should fail — compound states cannot be terminal
	// The error could be ErrCompoundStateNoInitialChild or a new error; any non-nil error is acceptable
	// since the specific sentinel for this is ErrDeadEndState (reused) or a new one.
	// Per task scope: use ErrDeadEndState sentinel (compound terminal = structural error).
	if err == nil {
		t.Error("compound state marked terminal should fail validation")
	}
}

// ─── checkWaitLeafOnly tests ──────────────────────────────────────────────────

func TestValidate_IsWait_OnCompoundState_Fails(t *testing.T) {
	def := minimalHierarchyDef()
	proc := def.States["PROCESSING"]
	proc.IsWait = true
	def.States["PROCESSING"] = proc

	err := Validate(def, hierarchySentinels())
	if err == nil {
		t.Error("IsWait on compound state should fail validation")
	}
}

// ─── checkInitialNotCompound tests ───────────────────────────────────────────

func TestValidate_InitialState_CannotBeCompound(t *testing.T) {
	def := minimalHierarchyDef()
	// Make PROCESSING both initial and compound (CREATED is normally the initial).
	proc := def.States["PROCESSING"]
	proc.IsInitial = true
	def.States["PROCESSING"] = proc
	// Remove IsInitial from CREATED to keep only one initial state.
	created := def.States["CREATED"]
	created.IsInitial = false
	def.States["CREATED"] = created
	def.InitialState = "PROCESSING"

	err := Validate(def, hierarchySentinels())
	if err == nil {
		t.Error("initial compound state should fail validation")
	}
}

// ─── flat workflow backward compat ────────────────────────────────────────────

func TestValidate_FlatWorkflow_PassesHierarchyChecks(t *testing.T) {
	def := minimalDef() // from compiled_test.go — flat workflow helper
	if err := Validate(def, hierarchySentinels()); err != nil {
		t.Errorf("flat workflow should pass all hierarchy checks, got: %v", err)
	}
}
