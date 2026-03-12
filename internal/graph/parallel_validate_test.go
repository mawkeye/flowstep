package graph

import (
	"errors"
	"testing"

	"github.com/mawkeye/flowstep/types"
)

var (
	testErrParallelStateNoRegions    = errors.New("test: parallel state has no regions")
	testErrParallelRegionNotCompound = errors.New("test: parallel region not compound")
	testErrNestedParallelState       = errors.New("test: nested parallel state")
)

func parallelSentinels() Sentinels {
	s := hierarchySentinels()
	s.ErrParallelStateNoRegions = testErrParallelStateNoRegions
	s.ErrParallelRegionNotCompound = testErrParallelRegionNotCompound
	s.ErrNestedParallelState = testErrNestedParallelState
	return s
}

// minimalParallelDef builds a valid parallel workflow:
//
//	IDLE -> editing (parallel: bold_region, italic_region) -> DONE
func minimalParallelDef() *types.Definition {
	return &types.Definition{
		AggregateType: "editor",
		WorkflowType:  "editor-wf",
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
			"start":       {Name: "start", Sources: []string{"IDLE"}, Target: "editing", TriggerType: types.TriggerDirect},
			"toggleBold":  {Name: "toggleBold", Sources: []string{"bold_off"}, Target: "bold_on", TriggerType: types.TriggerDirect},
			"untoggleBold": {Name: "untoggleBold", Sources: []string{"bold_on"}, Target: "bold_off", TriggerType: types.TriggerDirect},
			"toggleItalic":   {Name: "toggleItalic", Sources: []string{"italic_off"}, Target: "italic_on", TriggerType: types.TriggerDirect},
			"untoggleItalic": {Name: "untoggleItalic", Sources: []string{"italic_on"}, Target: "italic_off", TriggerType: types.TriggerDirect},
			"finish":      {Name: "finish", Sources: []string{"editing"}, Target: "DONE", TriggerType: types.TriggerDirect},
		},
		InitialState:   "IDLE",
		TerminalStates: []string{"DONE"},
	}
}

// TestValidate_Parallel_ValidDefinition verifies that a valid parallel workflow passes validation.
func TestValidate_Parallel_ValidDefinition(t *testing.T) {
	def := minimalParallelDef()
	if err := Validate(def, parallelSentinels()); err != nil {
		t.Errorf("valid parallel definition should pass validation, got: %v", err)
	}
}

// TestValidate_Parallel_NoRegions verifies that a parallel state with no children fails.
func TestValidate_Parallel_NoRegions(t *testing.T) {
	def := minimalParallelDef()
	// Remove children from the parallel state
	editing := def.States["editing"]
	editing.Children = nil
	def.States["editing"] = editing

	err := Validate(def, parallelSentinels())
	if err == nil {
		t.Fatal("expected error for parallel state with no regions, got nil")
	}
	if !errors.Is(err, testErrParallelStateNoRegions) {
		t.Errorf("expected ErrParallelStateNoRegions, got: %v", err)
	}
}

// TestValidate_Parallel_NonCompoundRegion verifies that a parallel child that is not compound fails.
func TestValidate_Parallel_NonCompoundRegion(t *testing.T) {
	def := minimalParallelDef()
	// Make bold_region non-compound
	boldRegion := def.States["bold_region"]
	boldRegion.IsCompound = false
	boldRegion.InitialChild = ""
	boldRegion.Children = nil
	def.States["bold_region"] = boldRegion

	err := Validate(def, parallelSentinels())
	if err == nil {
		t.Fatal("expected error for non-compound region, got nil")
	}
	if !errors.Is(err, testErrParallelRegionNotCompound) {
		t.Errorf("expected ErrParallelRegionNotCompound, got: %v", err)
	}
}

// TestValidate_Parallel_NestedParallel verifies that a parallel state inside another parallel state fails.
func TestValidate_Parallel_NestedParallel(t *testing.T) {
	def := minimalParallelDef()
	// Make bold_region itself a parallel state — nested parallel
	boldRegion := def.States["bold_region"]
	boldRegion.IsParallel = true
	def.States["bold_region"] = boldRegion

	err := Validate(def, parallelSentinels())
	if err == nil {
		t.Fatal("expected error for nested parallel state, got nil")
	}
	if !errors.Is(err, testErrNestedParallelState) {
		t.Errorf("expected ErrNestedParallelState, got: %v", err)
	}
}

// TestValidate_Parallel_CompoundStatesExemptFromInitialChild verifies that parallel states
// are exempt from the InitialChild requirement (checkCompoundStates must not reject them).
func TestValidate_Parallel_CompoundStatesExemptFromInitialChild(t *testing.T) {
	def := minimalParallelDef()
	// Ensure the parallel state itself has no InitialChild (it shouldn't need one)
	editing := def.States["editing"]
	editing.InitialChild = ""
	def.States["editing"] = editing

	if err := Validate(def, parallelSentinels()); err != nil {
		t.Errorf("parallel state without InitialChild should pass validation, got: %v", err)
	}
}

// TestValidate_Parallel_ReachabilityCoversAllRegions verifies that reachability
// check reaches all states inside parallel regions (no false ErrUnreachableState).
func TestValidate_Parallel_ReachabilityCoversAllRegions(t *testing.T) {
	def := minimalParallelDef()
	if err := Validate(def, parallelSentinels()); err != nil {
		t.Errorf("all states in parallel regions should be reachable, got: %v", err)
	}
}
