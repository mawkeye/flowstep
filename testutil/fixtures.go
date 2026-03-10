package testutil

import (
	"testing"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

// OrderWorkflow returns a simple linear workflow: CREATED -> PROCESSING -> DONE.
func OrderWorkflow(t *testing.T) *types.Definition {
	t.Helper()
	def, err := flowstep.Define("order", "order_workflow").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.State("PROCESSING"),
			flowstep.Terminal("DONE"),
			flowstep.Terminal("CANCELLED"),
		).
		Transition("start_processing",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
			flowstep.Event("OrderStarted"),
		).
		Transition("complete",
			flowstep.From("PROCESSING"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
		).
		Transition("cancel",
			flowstep.From("CREATED", "PROCESSING"),
			flowstep.To("CANCELLED"),
			flowstep.Event("OrderCancelled"),
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
	def, err := flowstep.Define("request", "approval_workflow").
		Version(1).
		States(
			flowstep.Initial("SUBMITTED"),
			flowstep.WaitState("PENDING_APPROVAL"),
			flowstep.Terminal("APPROVED"),
			flowstep.Terminal("REJECTED"),
		).
		Transition("submit_for_approval",
			flowstep.From("SUBMITTED"),
			flowstep.To("PENDING_APPROVAL"),
			flowstep.Event("ApprovalRequested"),
			flowstep.EmitTask(types.TaskDef{
				Type:        "approval",
				Description: "Approve or reject the request",
				Options:     []string{"approve", "reject"},
			}),
		).
		Transition("approve",
			flowstep.From("PENDING_APPROVAL"),
			flowstep.To("APPROVED"),
			flowstep.Event("RequestApproved"),
			flowstep.OnTaskCompleted("approval"),
		).
		Transition("reject",
			flowstep.From("PENDING_APPROVAL"),
			flowstep.To("REJECTED"),
			flowstep.Event("RequestRejected"),
			flowstep.OnTaskCompleted("approval"),
		).
		Build()
	if err != nil {
		t.Fatalf("testutil: failed to build ApprovalWorkflow: %v", err)
	}
	return def
}
