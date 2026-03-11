package flowstep_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/testutil"
	"github.com/mawkeye/flowstep/types"
)

// ─── Activity recorder ────────────────────────────────────────────────────────

// hierActivity tracks entry/exit call order for hierarchical integration tests.
type hierActivity struct {
	mu    sync.Mutex
	name  string
	order *[]string // shared slice across all activities in the test
}

func (a *hierActivity) Name() string { return a.name }
func (a *hierActivity) Execute(_ context.Context, _ types.ActivityInput) (*types.ActivityResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	*a.order = append(*a.order, a.name)
	return &types.ActivityResult{}, nil
}

// ─── Hierarchical workflow definition ─────────────────────────────────────────
//
// CREATED (initial, leaf)
//   → PROCESSING (compound, initial: VALIDATING)
//       VALIDATING (leaf, entry: on_enter_validating, exit: on_exit_validating)
//         → APPROVED (leaf, terminal, entry: on_enter_approved)
//         → REJECTED (leaf, terminal, entry: on_enter_rejected)
//   → CANCELLED (leaf, terminal, entry: on_enter_cancelled)
//
// "start"  : CREATED → PROCESSING (resolves to VALIDATING)
// "approve": PROCESSING → APPROVED  (event bubbling from VALIDATING)
// "reject" : PROCESSING → REJECTED  (event bubbling from VALIDATING)
// "cancel" : CREATED → CANCELLED    (flat transition, no hierarchy)
// "cancel_processing": PROCESSING → CANCELLED (bubbles from VALIDATING)

func buildHierarchicalWorkflow(t *testing.T) *types.Definition {
	t.Helper()
	def, err := flowstep.Define("TestAgg", "hierarchical_order").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.CompoundState("PROCESSING",
				flowstep.InitialChild("VALIDATING"),
				flowstep.EntryActivityOpt("on_enter_processing"),
				flowstep.ExitActivityOpt("on_exit_processing"),
			),
			flowstep.State("VALIDATING",
				flowstep.Parent("PROCESSING"),
				flowstep.EntryActivityOpt("on_enter_validating"),
				flowstep.ExitActivityOpt("on_exit_validating"),
			),
			flowstep.Terminal("APPROVED",
				flowstep.Parent("PROCESSING"),
				flowstep.EntryActivityOpt("on_enter_approved"),
			),
			flowstep.Terminal("REJECTED",
				flowstep.Parent("PROCESSING"),
				flowstep.EntryActivityOpt("on_enter_rejected"),
			),
			flowstep.Terminal("CANCELLED",
				flowstep.EntryActivityOpt("on_enter_cancelled"),
			),
		).
		Transition("start",
			flowstep.From("CREATED"),
			flowstep.To("PROCESSING"),
		).
		Transition("approve",
			flowstep.From("PROCESSING"),
			flowstep.To("APPROVED"),
		).
		Transition("reject",
			flowstep.From("PROCESSING"),
			flowstep.To("REJECTED"),
		).
		Transition("cancel_processing",
			flowstep.From("PROCESSING"),
			flowstep.To("CANCELLED"),
		).
		Build()
	if err != nil {
		t.Fatalf("buildHierarchicalWorkflow: %v", err)
	}
	return def
}

func registerHierActivities(runner interface{ Register(flowstep.Activity) }, order *[]string, names ...string) {
	for _, name := range names {
		n := name // capture
		runner.Register(&hierActivity{name: n, order: order})
	}
}

// ─── Scenario 1: Happy path — compound target resolution ─────────────────────

func TestHierarchical_HappyPath_CompoundTargetResolvesToLeaf(t *testing.T) {
	var order []string
	te := testutil.NewTestEngine(t)
	registerHierActivities(te.ActivityRunner, &order,
		"on_enter_processing", "on_exit_processing",
		"on_enter_validating", "on_exit_validating",
		"on_enter_approved",
	)
	if err := te.Engine.Register(buildHierarchicalWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	result, err := te.Engine.Transition(ctx, "TestAgg", "order-1", "start", "actor", nil)
	if err != nil {
		t.Fatalf("Transition start: %v", err)
	}
	// Compound target PROCESSING resolves to its initial leaf VALIDATING.
	if result.NewState != "VALIDATING" {
		t.Errorf("NewState = %q, want VALIDATING", result.NewState)
	}
	if result.Instance.CurrentState != "VALIDATING" {
		t.Errorf("CurrentState = %q, want VALIDATING", result.Instance.CurrentState)
	}
}

// ─── Scenario 2: Entry/exit activities fire in correct order ─────────────────

func TestHierarchical_EntryExitActivities_FireInOrder(t *testing.T) {
	var order []string
	te := testutil.NewTestEngine(t)
	registerHierActivities(te.ActivityRunner, &order,
		"on_enter_processing", "on_exit_processing",
		"on_enter_validating", "on_exit_validating",
		"on_enter_approved",
	)
	if err := te.Engine.Register(buildHierarchicalWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// "start": CREATED → PROCESSING(leaf=VALIDATING); LCA("","VALIDATING")="" → entry [PROCESSING, VALIDATING]
	if _, err := te.Engine.Transition(ctx, "TestAgg", "order-2", "start", "actor", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	wantAfterStart := []string{"on_enter_processing", "on_enter_validating"}
	for i, w := range wantAfterStart {
		if i >= len(order) || order[i] != w {
			t.Errorf("activity[%d] = %q, want %q (order=%v)", i, safeIdx(order, i), w, order)
		}
	}

	// "approve": VALIDATING → APPROVED; LCA=PROCESSING → exit [VALIDATING], entry [APPROVED]
	order = nil
	if _, err := te.Engine.Transition(ctx, "TestAgg", "order-2", "approve", "actor", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	wantAfterApprove := []string{"on_exit_validating", "on_enter_approved"}
	for i, w := range wantAfterApprove {
		if i >= len(order) || order[i] != w {
			t.Errorf("approve activity[%d] = %q, want %q (order=%v)", i, safeIdx(order, i), w, order)
		}
	}
}

// ─── Scenario 3: Event bubbling — cancel from VALIDATING via PROCESSING ──────

func TestHierarchical_EventBubbling_CancelFromValidating(t *testing.T) {
	var order []string
	te := testutil.NewTestEngine(t)
	registerHierActivities(te.ActivityRunner, &order,
		"on_enter_processing", "on_exit_processing",
		"on_enter_validating", "on_exit_validating",
		"on_enter_cancelled",
	)
	if err := te.Engine.Register(buildHierarchicalWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Move to VALIDATING first.
	if _, err := te.Engine.Transition(ctx, "TestAgg", "order-3", "start", "actor", nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	// "cancel_processing" is on PROCESSING; current state is VALIDATING.
	// Event bubbles: VALIDATING → PROCESSING → transition fires → CANCELLED.
	// Exit: [VALIDATING, PROCESSING], entry: [CANCELLED]
	order = nil
	result, err := te.Engine.Transition(ctx, "TestAgg", "order-3", "cancel_processing", "actor", nil)
	if err != nil {
		t.Fatalf("cancel_processing: %v", err)
	}
	if result.NewState != "CANCELLED" {
		t.Errorf("NewState = %q, want CANCELLED", result.NewState)
	}
	// Both VALIDATING and PROCESSING exit activities fire, then CANCELLED entry.
	wantOrder := []string{"on_exit_validating", "on_exit_processing", "on_enter_cancelled"}
	if len(order) != len(wantOrder) {
		t.Fatalf("activity order = %v, want %v", order, wantOrder)
	}
	for i, w := range wantOrder {
		if order[i] != w {
			t.Errorf("activity[%d] = %q, want %q", i, order[i], w)
		}
	}
}

// ─── Scenario 4: Dangling task cleanup on subtree exit ────────────────────────

func TestHierarchical_DanglingTaskCleanup_OnSubtreeExit(t *testing.T) {
	var order []string
	te := testutil.NewTestEngine(t)
	registerHierActivities(te.ActivityRunner, &order,
		"on_enter_processing", "on_exit_processing",
		"on_enter_validating", "on_exit_validating",
		"on_enter_cancelled",
	)
	if err := te.Engine.Register(buildHierarchicalWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Move to VALIDATING.
	if _, err := te.Engine.Transition(ctx, "TestAgg", "order-4", "start", "actor", nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Manually create a pending task associated with VALIDATING state.
	pendingTask := types.PendingTask{
		ID:            "dangling-task-1",
		WorkflowType:  "hierarchical_order",
		AggregateType: "TestAgg",
		AggregateID:   "order-4",
		State:         "VALIDATING",
		Status:        types.TaskStatusPending,
	}
	if err := te.TaskStore.Create(ctx, nil, pendingTask); err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Fire cancel_processing — exits VALIDATING subtree.
	if _, err := te.Engine.Transition(ctx, "TestAgg", "order-4", "cancel_processing", "actor", nil); err != nil {
		t.Fatalf("cancel_processing: %v", err)
	}

	// The VALIDATING task must now be CANCELLED.
	got, err := te.TaskStore.Get(ctx, "dangling-task-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Status != types.TaskStatusCancelled {
		t.Errorf("task status = %q, want CANCELLED", got.Status)
	}
}

// ─── Scenario 5: Mermaid output contains compound nesting ────────────────────

func TestHierarchical_MermaidOutput_ContainsCompoundBlock(t *testing.T) {
	def := buildHierarchicalWorkflow(t)
	diagram := types.Mermaid(def)

	checks := []string{
		"state PROCESSING {",
		"[*] --> VALIDATING",
		"PROCESSING --> APPROVED : approve",
	}
	for _, want := range checks {
		if !strings.Contains(diagram, want) {
			t.Errorf("Mermaid diagram missing %q\ngot:\n%s", want, diagram)
		}
	}
}

// ─── Scenario 6: Flat workflow backward compat ───────────────────────────────

func TestHierarchical_FlatWorkflowBackwardCompat(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := testutil.OrderWorkflow(t)
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	r1, err := te.Engine.Transition(ctx, "order", "flat-1", "start_processing", "actor", nil)
	if err != nil {
		t.Fatalf("start_processing: %v", err)
	}
	if r1.NewState != "PROCESSING" {
		t.Errorf("NewState = %q, want PROCESSING", r1.NewState)
	}

	r2, err := te.Engine.Transition(ctx, "order", "flat-1", "complete", "actor", nil)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if r2.NewState != "DONE" {
		t.Errorf("NewState = %q, want DONE", r2.NewState)
	}
	if !r2.IsTerminal {
		t.Error("IsTerminal should be true after reaching DONE")
	}
}

// ─── History States integration tests ─────────────────────────────────────────

// buildHistoryWorkflow creates:
//
//	IDLE (initial)
//	  → PROCESSING (compound, initial: REVIEWING)
//	      REVIEWING (leaf)
//	        → SHIPPING (leaf)
//	  → DONE (terminal)
//
// Transitions:
//
//	"enter"          : IDLE → PROCESSING (no history)
//	"advance"        : REVIEWING → SHIPPING
//	"pause"          : PROCESSING → IDLE
//	"resume_shallow" : IDLE → PROCESSING [shallow history]
//	"resume_deep"    : IDLE → PROCESSING [deep history]
//	"finish"         : PROCESSING → DONE
func buildHistoryWorkflow(t *testing.T) *types.Definition {
	t.Helper()
	def, err := flowstep.Define("TestAgg", "history_workflow").
		Version(1).
		States(
			flowstep.Initial("IDLE"),
			flowstep.CompoundState("PROCESSING",
				flowstep.InitialChild("REVIEWING"),
			),
			flowstep.State("REVIEWING", flowstep.Parent("PROCESSING")),
			flowstep.State("SHIPPING", flowstep.Parent("PROCESSING")),
			flowstep.Terminal("DONE"),
		).
		Transition("enter",
			flowstep.From("IDLE"),
			flowstep.To("PROCESSING"),
		).
		Transition("advance",
			flowstep.From("REVIEWING"),
			flowstep.To("SHIPPING"),
		).
		Transition("pause",
			flowstep.From("PROCESSING"),
			flowstep.To("IDLE"),
		).
		Transition("resume_shallow",
			flowstep.From("IDLE"),
			flowstep.To("PROCESSING"),
			flowstep.WithHistory(flowstep.HistoryShallow),
		).
		Transition("resume_deep",
			flowstep.From("IDLE"),
			flowstep.To("PROCESSING"),
			flowstep.WithHistory(flowstep.HistoryDeep),
		).
		Transition("finish",
			flowstep.From("PROCESSING"),
			flowstep.To("DONE"),
		).
		Build()
	if err != nil {
		t.Fatalf("buildHistoryWorkflow: %v", err)
	}
	return def
}

func TestHistory_ShallowResumesLastChild(t *testing.T) {
	te := testutil.NewTestEngine(t)
	if err := te.Engine.Register(buildHistoryWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Enter PROCESSING → lands at REVIEWING (initial child)
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-1", "enter", "actor", nil); err != nil {
		t.Fatalf("enter: %v", err)
	}
	testutil.AssertState(t, te, "TestAgg", "h-1", "REVIEWING")

	// Advance to SHIPPING
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-1", "advance", "actor", nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	testutil.AssertState(t, te, "TestAgg", "h-1", "SHIPPING")

	// Pause → IDLE (exits PROCESSING, records history)
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-1", "pause", "actor", nil); err != nil {
		t.Fatalf("pause: %v", err)
	}
	testutil.AssertState(t, te, "TestAgg", "h-1", "IDLE")

	// Resume with shallow history → should land at SHIPPING (last direct child)
	result, err := te.Engine.Transition(ctx, "TestAgg", "h-1", "resume_shallow", "actor", nil)
	if err != nil {
		t.Fatalf("resume_shallow: %v", err)
	}
	if result.NewState != "SHIPPING" {
		t.Errorf("resume_shallow: NewState = %q, want SHIPPING", result.NewState)
	}
}

func TestHistory_DeepResumesLastLeaf(t *testing.T) {
	te := testutil.NewTestEngine(t)
	if err := te.Engine.Register(buildHistoryWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Enter → REVIEWING, advance → SHIPPING, pause → IDLE
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-2", "enter", "actor", nil); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-2", "advance", "actor", nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-2", "pause", "actor", nil); err != nil {
		t.Fatalf("pause: %v", err)
	}

	// Resume with deep history → should land at SHIPPING (last leaf)
	result, err := te.Engine.Transition(ctx, "TestAgg", "h-2", "resume_deep", "actor", nil)
	if err != nil {
		t.Fatalf("resume_deep: %v", err)
	}
	if result.NewState != "SHIPPING" {
		t.Errorf("resume_deep: NewState = %q, want SHIPPING", result.NewState)
	}
}

func TestHistory_NoHistoryFallsBackToInitialChild(t *testing.T) {
	te := testutil.NewTestEngine(t)
	if err := te.Engine.Register(buildHistoryWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// First entry via resume_shallow (no history recorded yet) → should fall back to REVIEWING
	result, err := te.Engine.Transition(ctx, "TestAgg", "h-3", "resume_shallow", "actor", nil)
	if err != nil {
		t.Fatalf("resume_shallow: %v", err)
	}
	if result.NewState != "REVIEWING" {
		t.Errorf("no-history fallback: NewState = %q, want REVIEWING", result.NewState)
	}
}

func TestHistory_ForceStateClearsHistory(t *testing.T) {
	te := testutil.NewTestEngine(t)
	if err := te.Engine.Register(buildHistoryWorkflow(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Enter → REVIEWING, advance → SHIPPING, pause → IDLE (records history)
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-4", "enter", "actor", nil); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-4", "advance", "actor", nil); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "TestAgg", "h-4", "pause", "actor", nil); err != nil {
		t.Fatalf("pause: %v", err)
	}

	// ForceState to IDLE (clears history)
	if _, err := te.Engine.ForceState(ctx, "TestAgg", "h-4", "IDLE", "admin", "reset"); err != nil {
		t.Fatalf("ForceState: %v", err)
	}

	// Resume with shallow history → history was cleared, should fall back to REVIEWING
	result, err := te.Engine.Transition(ctx, "TestAgg", "h-4", "resume_shallow", "actor", nil)
	if err != nil {
		t.Fatalf("resume_shallow after ForceState: %v", err)
	}
	if result.NewState != "REVIEWING" {
		t.Errorf("ForceState history clear: NewState = %q, want REVIEWING", result.NewState)
	}
}

func TestHistory_MermaidAnnotation(t *testing.T) {
	def := buildHistoryWorkflow(t)
	diagram := types.Mermaid(def)

	checks := []string{
		"IDLE --> PROCESSING : resume_shallow [H]",
		"IDLE --> PROCESSING : resume_deep [H*]",
		"IDLE --> PROCESSING : enter",
	}
	for _, want := range checks {
		if !strings.Contains(diagram, want) {
			t.Errorf("Mermaid missing %q\ngot:\n%s", want, diagram)
		}
	}
	// "enter" must NOT have history annotation
	if strings.Contains(diagram, "enter [H]") || strings.Contains(diagram, "enter [H*]") {
		t.Errorf("non-history transition 'enter' must not have annotation\ngot:\n%s", diagram)
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func safeIdx(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "<missing>"
}
