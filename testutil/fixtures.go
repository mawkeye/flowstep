package testutil

import (
	"testing"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

// OrderWorkflow returns a simple linear workflow: CREATED -> PROCESSING -> DONE.
func OrderWorkflow(t *testing.T) *types.Definition {
	t.Helper()
	def, err := flowstate.Define("order", "order_workflow").
		Version(1).
		States(
			flowstate.Initial("CREATED"),
			flowstate.State("PROCESSING"),
			flowstate.Terminal("DONE"),
			flowstate.Terminal("CANCELLED"),
		).
		Transition("start_processing",
			flowstate.From("CREATED"),
			flowstate.To("PROCESSING"),
			flowstate.Event("OrderStarted"),
		).
		Transition("complete",
			flowstate.From("PROCESSING"),
			flowstate.To("DONE"),
			flowstate.Event("OrderCompleted"),
		).
		Transition("cancel",
			flowstate.From("CREATED", "PROCESSING"),
			flowstate.To("CANCELLED"),
			flowstate.Event("OrderCancelled"),
		).
		Build()
	if err != nil {
		t.Fatalf("testutil: failed to build OrderWorkflow: %v", err)
	}
	return def
}

// ApprovalWorkflow returns a human-in-the-loop workflow with a wait state for approval.
func ApprovalWorkflow(t *testing.T) *types.Definition {
	t.Helper()
	def, err := flowstate.Define("request", "approval_workflow").
		Version(1).
		States(
			flowstate.Initial("SUBMITTED"),
			flowstate.WaitState("PENDING_APPROVAL"),
			flowstate.Terminal("APPROVED"),
			flowstate.Terminal("REJECTED"),
		).
		Transition("submit_for_approval",
			flowstate.From("SUBMITTED"),
			flowstate.To("PENDING_APPROVAL"),
			flowstate.Event("ApprovalRequested"),
			flowstate.EmitTask(types.TaskDef{
				Type:        "approval",
				Description: "Approve or reject the request",
				Options:     []string{"approve", "reject"},
			}),
		).
		Transition("approve",
			flowstate.From("PENDING_APPROVAL"),
			flowstate.To("APPROVED"),
			flowstate.Event("RequestApproved"),
			flowstate.OnTaskCompleted("approval"),
		).
		Transition("reject",
			flowstate.From("PENDING_APPROVAL"),
			flowstate.To("REJECTED"),
			flowstate.Event("RequestRejected"),
			flowstate.OnTaskCompleted("approval"),
		).
		Build()
	if err != nil {
		t.Fatalf("testutil: failed to build ApprovalWorkflow: %v", err)
	}
	return def
}
