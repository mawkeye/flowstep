// Example 02: Guards and Conditional Routing
//
// Demonstrates:
//   - Guards: deterministic precondition checks that block transitions
//   - Conditional routing: branch to different states based on runtime conditions
//
// Run from the module root:
//
//	go run ./examples/02-guards-and-routing/
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/adapters/memstore"
	"github.com/mawkeye/flowstate/types"
)

// minimumAmountGuard blocks the transition if the amount param is below the minimum.
// Guards MUST be deterministic — no I/O, no side effects, read params only.
type minimumAmountGuard struct {
	minimum float64
}

func (g *minimumAmountGuard) Check(_ context.Context, _ any, params map[string]any) error {
	amount, ok := params["amount"].(float64)
	if !ok || amount < g.minimum {
		return fmt.Errorf("amount %.2f is below minimum %.2f", amount, g.minimum)
	}
	return nil
}

// highValueCondition routes to MANUAL_REVIEW when amount exceeds the threshold.
// Conditions MUST be deterministic — no I/O, read params only.
type highValueCondition struct {
	threshold float64
}

func (c *highValueCondition) Evaluate(_ context.Context, _ any, params map[string]any) (bool, error) {
	amount, ok := params["amount"].(float64)
	if !ok {
		return false, nil
	}
	return amount > c.threshold, nil
}

func main() {
	// 1. Define the workflow
	//
	// States: SUBMITTED → MANUAL_REVIEW (terminal) or AUTO_APPROVED (terminal)
	//
	// The "evaluate" transition uses conditional routing:
	//   - If amount > 10000: MANUAL_REVIEW
	//   - Default (all other cases): AUTO_APPROVED
	//
	// An additional guard blocks the transition if amount < 1.00.
	def, err := flowstate.Define("loan", "loan_workflow").
		Version(1).
		States(
			flowstate.Initial("SUBMITTED"),
			flowstate.Terminal("MANUAL_REVIEW"),
			flowstate.Terminal("AUTO_APPROVED"),
		).
		Transition("evaluate",
			flowstate.From("SUBMITTED"),
			// Guard runs first — blocks transition if amount < 1.00
			flowstate.Guards(&minimumAmountGuard{minimum: 1.00}),
			// Routes: first matching condition wins; Default is required
			flowstate.Route(
				flowstate.When(&highValueCondition{threshold: 10_000}),
				flowstate.To("MANUAL_REVIEW"),
			),
			flowstate.Route(
				flowstate.Default(),
				flowstate.To("AUTO_APPROVED"),
			),
			flowstate.Event("LoanEvaluated"),
		).
		Build()
	if err != nil {
		log.Fatalf("build workflow: %v", err)
	}

	// Write Mermaid diagram
	md := "# Loan Workflow\n\n```mermaid\n" + types.Mermaid(def) + "```\n"
	if err := os.WriteFile("examples/02-guards-and-routing/workflow.md", []byte(md), 0644); err != nil {
		log.Fatalf("write workflow.md: %v", err)
	}
	fmt.Println("Wrote Mermaid diagram to examples/02-guards-and-routing/workflow.md")

	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(memstore.NewEventStore()),
		flowstate.WithInstanceStore(memstore.NewInstanceStore()),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
	)
	if err != nil {
		log.Fatalf("create engine: %v", err)
	}
	defer engine.Shutdown(context.Background())

	if err := engine.Register(def); err != nil {
		log.Fatalf("register workflow: %v", err)
	}

	ctx := context.Background()

	// 2. Guard failure: amount below minimum
	fmt.Println("--- Guard failure (amount too low) ---")
	_, err = engine.Transition(ctx, "loan", "loan-1", "evaluate", "system",
		map[string]any{"amount": 0.50},
	)
	if errors.Is(err, flowstate.ErrGuardFailed) {
		fmt.Printf("Guard blocked transition: %v\n", err)
		// Guard failure details are reported via the Hooks.OnGuardFailed callback.
	} else {
		log.Fatalf("expected ErrGuardFailed, got %v", err)
	}

	// 3. Routing: high-value loan → MANUAL_REVIEW
	fmt.Println("\n--- High-value loan (> 10000) → MANUAL_REVIEW ---")
	result, err := engine.Transition(ctx, "loan", "loan-1", "evaluate", "system",
		map[string]any{"amount": 50_000.00},
	)
	if err != nil {
		log.Fatalf("evaluate high-value: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)
	// Output: Transition: SUBMITTED -> MANUAL_REVIEW

	// 4. Routing: standard loan → AUTO_APPROVED (different aggregate)
	fmt.Println("\n--- Standard loan (<= 10000) → AUTO_APPROVED ---")
	result, err = engine.Transition(ctx, "loan", "loan-2", "evaluate", "system",
		map[string]any{"amount": 500.00},
	)
	if err != nil {
		log.Fatalf("evaluate standard: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)
	// Output: Transition: SUBMITTED -> AUTO_APPROVED

	// 5. Show that Guard and Condition are separate interfaces
	fmt.Println("\n--- Guard and Condition types ---")
	var _ types.Guard = &minimumAmountGuard{}
	var _ types.Condition = &highValueCondition{}
	fmt.Println("Guard:     minimumAmountGuard — blocks if amount < minimum")
	fmt.Println("Condition: highValueCondition — routes to MANUAL_REVIEW if amount > threshold")
}
