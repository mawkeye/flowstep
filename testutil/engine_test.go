package testutil_test

import (
	"context"
	"testing"
	"time"

	"github.com/mawkeye/flowstate/testutil"
)

func TestNewTestEngine(t *testing.T) {
	te := testutil.NewTestEngine(t)
	if te.Engine == nil {
		t.Fatal("engine should not be nil")
	}
}

func TestFakeClock(t *testing.T) {
	clock := testutil.NewFakeClock()
	t1 := clock.Now()
	clock.Advance(5 * time.Minute)
	t2 := clock.Now()
	if t2.Sub(t1) != 5*time.Minute {
		t.Errorf("expected 5 minute advance, got %v", t2.Sub(t1))
	}
}

func TestOrderWorkflowFixture(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := testutil.OrderWorkflow(t)
	te.Engine.Register(def)

	ctx := context.Background()

	_, err := te.Engine.Transition(ctx, "order", "o-1", "start_processing", "user-1", nil)
	if err != nil {
		t.Fatalf("start_processing failed: %v", err)
	}
	testutil.AssertState(t, te, "order", "o-1", "PROCESSING")

	_, err = te.Engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}
	testutil.AssertState(t, te, "order", "o-1", "DONE")
	testutil.AssertEventCount(t, te, "order", "o-1", 2)
}

func TestApprovalWorkflowFixture(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := testutil.ApprovalWorkflow(t)
	te.Engine.Register(def)

	ctx := context.Background()

	// Submit for approval — creates pending task
	result, err := te.Engine.Transition(ctx, "request", "r-1", "submit_for_approval", "user-1", nil)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	testutil.AssertState(t, te, "request", "r-1", "PENDING_APPROVAL")

	if result.TaskCreated == nil {
		t.Fatal("expected task to be created")
	}

	// Complete the task with "approve" choice
	_, err = te.Engine.CompleteTask(ctx, result.TaskCreated.ID, "approve", "approver-1")
	if err != nil {
		t.Fatalf("complete task failed: %v", err)
	}
	testutil.AssertState(t, te, "request", "r-1", "APPROVED")
}

func TestAssertEventChain(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := testutil.OrderWorkflow(t)
	te.Engine.Register(def)

	ctx := context.Background()

	result, err := te.Engine.Transition(ctx, "order", "o-1", "start_processing", "user-1", nil)
	if err != nil {
		t.Fatalf("start failed: %v", err)
	}
	_, err = te.Engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil)
	if err != nil {
		t.Fatalf("complete failed: %v", err)
	}

	testutil.AssertEventChain(t, te, result.Event.CorrelationID, "OrderStarted", "OrderCompleted")
}
