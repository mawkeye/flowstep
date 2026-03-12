// Example 07: Parallel States (Orthogonal Regions)
//
// Demonstrates parallel states — multiple independent state machines running
// simultaneously inside a single workflow instance.
//
// Scenario — text editor with independent bold and italic formatting:
//
//	IDLE -> editing (parallel: bold_region, italic_region) -> DONE
//
//	bold_region:   bold_off <-> bold_on
//	italic_region: italic_off <-> italic_on
//
// While in the "editing" parallel state:
//   - Each region tracks its own active leaf (bold_off/bold_on, italic_off/italic_on)
//   - Transitions update only the affected region atomically
//   - A single "finishEditing" transition exits all regions and clears the parallel context
//
// Run from the module root:
//
//	go run ./examples/07-parallel-states/
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/memstore"
	"github.com/mawkeye/flowstep/types"
)

func buildEditorWorkflow() (*types.Definition, error) {
	return flowstep.Define("Editor", "editor_workflow").
		Version(1).
		States(
			flowstep.Initial("IDLE"),
			flowstep.ParallelState("editing").
				Region("bold_region",
					flowstep.State("bold_off"),
					flowstep.State("bold_on"),
				).
				Region("italic_region",
					flowstep.State("italic_off"),
					flowstep.State("italic_on"),
				).
				Done(),
			flowstep.Terminal("DONE"),
		).
		Transition("startEditing",
			flowstep.From("IDLE"),
			flowstep.To("editing"),
			flowstep.Event("EditingStarted"),
		).
		Transition("toggleBold",
			flowstep.From("bold_off"),
			flowstep.To("bold_on"),
			flowstep.Event("BoldEnabled"),
		).
		Transition("untoggleBold",
			flowstep.From("bold_on"),
			flowstep.To("bold_off"),
			flowstep.Event("BoldDisabled"),
		).
		Transition("toggleItalic",
			flowstep.From("italic_off"),
			flowstep.To("italic_on"),
			flowstep.Event("ItalicEnabled"),
		).
		Transition("untoggleItalic",
			flowstep.From("italic_on"),
			flowstep.To("italic_off"),
			flowstep.Event("ItalicDisabled"),
		).
		Transition("finishEditing",
			flowstep.From("editing"),
			flowstep.To("DONE"),
			flowstep.Event("EditingFinished"),
		).
		Build()
}

func printState(label string, result *types.TransitionResult) {
	inst := result.Instance
	fmt.Printf("%-20s state=%-10s parallel=%v\n", label+":", inst.CurrentState, inst.ActiveInParallel)
}

func main() {
	def, err := buildEditorWorkflow()
	if err != nil {
		log.Fatalf("build workflow: %v", err)
	}

	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(memstore.NewEventStore()),
		flowstep.WithInstanceStore(memstore.NewInstanceStore()),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
	)
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}
	defer engine.Shutdown(context.Background())

	if err := engine.Register(def); err != nil {
		log.Fatalf("register workflow: %v", err)
	}

	ctx := context.Background()
	const aggType = "Editor"
	const aggID = "doc-1"

	fmt.Println("=== Text Editor Parallel State Example ===")
	fmt.Println()

	// 1. Enter parallel editing state
	result, err := engine.Transition(ctx, aggType, aggID, "startEditing", "user", nil)
	if err != nil {
		log.Fatalf("startEditing: %v", err)
	}
	printState("startEditing", result)

	// 2. Toggle bold in bold_region only — italic_region unchanged
	result, err = engine.Transition(ctx, aggType, aggID, "toggleBold", "user", nil)
	if err != nil {
		log.Fatalf("toggleBold: %v", err)
	}
	printState("toggleBold", result)

	// 3. Toggle italic in italic_region only — bold_region unchanged
	result, err = engine.Transition(ctx, aggType, aggID, "toggleItalic", "user", nil)
	if err != nil {
		log.Fatalf("toggleItalic: %v", err)
	}
	printState("toggleItalic", result)

	// 4. Untoggle bold
	result, err = engine.Transition(ctx, aggType, aggID, "untoggleBold", "user", nil)
	if err != nil {
		log.Fatalf("untoggleBold: %v", err)
	}
	printState("untoggleBold", result)

	// 5. Exit all regions and finish
	result, err = engine.Transition(ctx, aggType, aggID, "finishEditing", "user", nil)
	if err != nil {
		log.Fatalf("finishEditing: %v", err)
	}
	printState("finishEditing", result)
	fmt.Printf("\nFinal state: %s (terminal: %v)\n", result.NewState, result.IsTerminal)
}
