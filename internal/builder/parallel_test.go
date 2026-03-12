package builder

import (
	"testing"

	"github.com/mawkeye/flowstep/types"
)

// minimalDef returns a minimal valid DefBuilder (no validation fn).
func minimalParallelTestBuilder() *DefBuilder {
	return New("order", "order-wf", nil, nil).
		States(
			StateOption{def: types.StateDef{Name: "CREATED", IsInitial: true}},
			StateOption{def: types.StateDef{Name: "DONE", IsTerminal: true}},
		).
		Transition("process", From("CREATED"), To("DONE"))
}

// TestPriority_SetsOnTransitionDef verifies that Priority(5) wires the priority
// value through Build() into the resulting TransitionDef.Priority field.
func TestPriority_SetsOnTransitionDef(t *testing.T) {
	b := New("order", "order-wf", nil, nil).
		States(
			StateOption{def: types.StateDef{Name: "CREATED", IsInitial: true}},
			StateOption{def: types.StateDef{Name: "DONE", IsTerminal: true}},
		).
		Transition("process", From("CREATED"), To("DONE"), Priority(5))

	def, err := b.Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	td, ok := def.Transitions["process"]
	if !ok {
		t.Fatal("transition 'process' not found in definition")
	}
	if td.Priority != 5 {
		t.Errorf("TransitionDef.Priority = %d, want 5", td.Priority)
	}
}

// TestPriority_DefaultIsZero verifies that omitting Priority() results in Priority == 0.
func TestPriority_DefaultIsZero(t *testing.T) {
	b := minimalParallelTestBuilder()
	def, err := b.Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	td := def.Transitions["process"]
	if td.Priority != 0 {
		t.Errorf("TransitionDef.Priority = %d, want 0 (default)", td.Priority)
	}
}

// TestParallelStateBuilder_Done_ProducesCorrectStateDef verifies that
// ParallelState().Region().Done() produces the correct StateDefs in Build().
func TestParallelStateBuilder_Done_ProducesCorrectStateDef(t *testing.T) {
	b := New("editor", "editor-wf", nil, nil).
		States(
			StateOption{def: types.StateDef{Name: "IDLE", IsInitial: true}},
			ParallelState("editing").
				Region("bold_region",
					State("bold_off"),
					State("bold_on"),
				).
				Region("italic_region",
					State("italic_off"),
					State("italic_on"),
				).
				Done(),
			StateOption{def: types.StateDef{Name: "DONE", IsTerminal: true}},
		).
		Transition("start", From("IDLE"), To("editing")).
		Transition("finish", From("editing"), To("DONE"))

	def, err := b.Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// The parallel state itself
	editing, ok := def.States["editing"]
	if !ok {
		t.Fatal("state 'editing' not found in definition")
	}
	if !editing.IsParallel {
		t.Error("editing.IsParallel = false, want true")
	}
	if !editing.IsCompound {
		t.Error("editing.IsCompound = false, want true")
	}

	// Region states
	for _, regionName := range []string{"bold_region", "italic_region"} {
		region, ok := def.States[regionName]
		if !ok {
			t.Fatalf("region state %q not found in definition", regionName)
		}
		if !region.IsCompound {
			t.Errorf("region %q IsCompound = false, want true", regionName)
		}
		if region.Parent != "editing" {
			t.Errorf("region %q Parent = %q, want 'editing'", regionName, region.Parent)
		}
	}

	// Leaf states appear in def.States
	for _, leafName := range []string{"bold_off", "bold_on", "italic_off", "italic_on"} {
		if _, ok := def.States[leafName]; !ok {
			t.Errorf("leaf state %q not found in definition after Build()", leafName)
		}
	}

	// bold_off is the initial child of bold_region (first state passed to Region())
	boldRegion := def.States["bold_region"]
	if boldRegion.InitialChild != "bold_off" {
		t.Errorf("bold_region.InitialChild = %q, want 'bold_off'", boldRegion.InitialChild)
	}
	if boldRegion.InitialChild != "bold_off" {
		t.Errorf("bold_region.InitialChild = %q, want 'bold_off'", boldRegion.InitialChild)
	}

	// Leaf parents are their region
	if def.States["bold_off"].Parent != "bold_region" {
		t.Errorf("bold_off.Parent = %q, want 'bold_region'", def.States["bold_off"].Parent)
	}
	if def.States["italic_on"].Parent != "italic_region" {
		t.Errorf("italic_on.Parent = %q, want 'italic_region'", def.States["italic_on"].Parent)
	}
}
