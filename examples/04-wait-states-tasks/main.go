// Example 04: Wait States and Tasks (Human-in-the-Loop)
//
// Demonstrates pausing a workflow for a human decision:
//
//   - WaitState: marks a state as waiting for external input
//   - EmitTask: creates a pending task when entering the wait state
//   - OnTaskCompleted: declares that a transition fires when a task is completed
//   - engine.CompleteTask: submits a human choice to resolve the task
//
// Run from the module root:
//
//	go run ./examples/04-wait-states-tasks/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/adapters/memstore"
	"github.com/mawkeye/flowstate/types"
)

func main() {
	// 1. Define the workflow
	//
	// States: SUBMITTED → PENDING_APPROVAL (wait) → APPROVED or REJECTED
	//
	// Entering PENDING_APPROVAL emits a task with two options: "approve" or "reject".
	// Completing the task with choice "approve" triggers the "approve" transition.
	// Completing the task with choice "reject" triggers the "reject" transition.
	def, err := flowstate.Define("expense", "expense_workflow").
		Version(1).
		States(
			flowstate.Initial("SUBMITTED"),
			flowstate.WaitState("PENDING_APPROVAL"), // pauses for human input
			flowstate.Terminal("APPROVED"),
			flowstate.Terminal("REJECTED"),
		).
		Transition("submit_for_approval",
			flowstate.From("SUBMITTED"),
			flowstate.To("PENDING_APPROVAL"),
			flowstate.Event("ApprovalRequested"),
			// Emit a task when entering PENDING_APPROVAL
			flowstate.EmitTask(types.TaskDef{
				Type:        "expense_approval",
				Description: "Review and approve or reject the expense claim",
				Options:     []string{"approve", "reject"},
				Timeout:     48 * time.Hour,
			}),
		).
		// One OnTaskCompleted transition per option
		Transition("approve",
			flowstate.From("PENDING_APPROVAL"),
			flowstate.To("APPROVED"),
			flowstate.OnTaskCompleted("expense_approval"),
			flowstate.Event("ExpenseApproved"),
		).
		Transition("reject",
			flowstate.From("PENDING_APPROVAL"),
			flowstate.To("REJECTED"),
			flowstate.OnTaskCompleted("expense_approval"),
			flowstate.Event("ExpenseRejected"),
		).
		Build()
	if err != nil {
		log.Fatalf("build workflow: %v", err)
	}

	// Write Mermaid diagram
	md := "# Expense Workflow\n\n```mermaid\n" + types.Mermaid(def) + "```\n"
	if err := os.WriteFile("examples/04-wait-states-tasks/workflow.md", []byte(md), 0644); err != nil {
		log.Fatalf("write workflow.md: %v", err)
	}
	fmt.Println("Wrote Mermaid diagram to examples/04-wait-states-tasks/workflow.md")

	// TaskStore is required for wait states
	taskStore := memstore.NewTaskStore()

	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(memstore.NewEventStore()),
		flowstate.WithInstanceStore(memstore.NewInstanceStore()),
		flowstate.WithTaskStore(taskStore),
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

	// 2. Path A: expense-1 gets approved
	fmt.Println("--- Expense 1: approved ---")

	result, err := engine.Transition(ctx, "expense", "expense-1", "submit_for_approval", "employee-1", nil)
	if err != nil {
		log.Fatalf("submit_for_approval: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)
	// Output: Transition: SUBMITTED -> PENDING_APPROVAL

	// The task was created — inspect it
	task := result.TaskCreated
	if task == nil {
		log.Fatal("expected task to be created")
	}
	fmt.Printf("Task created: id=%s type=%s options=%v\n", task.ID, task.TaskType, task.Options)

	// Manager approves the expense
	result, err = engine.CompleteTask(ctx, task.ID, "approve", "manager-1")
	if err != nil {
		log.Fatalf("complete task (approve): %v", err)
	}
	fmt.Printf("Task completed: choice=approve -> %s (terminal: %v)\n", result.NewState, result.IsTerminal)
	// Output: Task completed: choice=approve -> APPROVED (terminal: true)

	// 3. Path B: expense-2 gets rejected
	fmt.Println("\n--- Expense 2: rejected ---")

	result, err = engine.Transition(ctx, "expense", "expense-2", "submit_for_approval", "employee-2", nil)
	if err != nil {
		log.Fatalf("submit_for_approval expense-2: %v", err)
	}
	fmt.Printf("Transition: %s -> %s\n", result.PreviousState, result.NewState)

	task = result.TaskCreated
	if task == nil {
		log.Fatal("expected task to be created")
	}
	fmt.Printf("Task created: id=%s type=%s\n", task.ID, task.TaskType)

	result, err = engine.CompleteTask(ctx, task.ID, "reject", "manager-1")
	if err != nil {
		log.Fatalf("complete task (reject): %v", err)
	}
	fmt.Printf("Task completed: choice=reject -> %s (terminal: %v)\n", result.NewState, result.IsTerminal)
	// Output: Task completed: choice=reject -> REJECTED (terminal: true)
}
