package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mawkeye/flowstate/types"
)

// ─── minimal stubs ────────────────────────────────────────────────────────────

type noopTx struct{}

func (n *noopTx) Begin(_ context.Context) (any, error)          { return struct{}{}, nil }
func (n *noopTx) Commit(_ context.Context, _ any) error          { return nil }
func (n *noopTx) Rollback(_ context.Context, _ any) error        { return nil }

type noopEventStore struct{}

func (n *noopEventStore) Append(_ context.Context, _ any, _ types.DomainEvent) error {
	return nil
}
func (n *noopEventStore) ListByCorrelation(_ context.Context, _ string) ([]types.DomainEvent, error) {
	return nil, nil
}
func (n *noopEventStore) ListByAggregate(_ context.Context, _, _ string) ([]types.DomainEvent, error) {
	return nil, nil
}

type memInstanceStore struct {
	instances map[string]*types.WorkflowInstance
	notFound  error
}

func newMemInstanceStore(notFound error) *memInstanceStore {
	return &memInstanceStore{instances: make(map[string]*types.WorkflowInstance), notFound: notFound}
}
func (m *memInstanceStore) key(aggType, aggID string) string { return aggType + "/" + aggID }
func (m *memInstanceStore) Get(_ context.Context, aggType, aggID string) (*types.WorkflowInstance, error) {
	inst, ok := m.instances[m.key(aggType, aggID)]
	if !ok {
		return nil, m.notFound
	}
	cp := *inst
	return &cp, nil
}
func (m *memInstanceStore) Create(_ context.Context, _ any, inst types.WorkflowInstance) error {
	m.instances[m.key(inst.AggregateType, inst.AggregateID)] = &inst
	return nil
}
func (m *memInstanceStore) Update(_ context.Context, _ any, inst types.WorkflowInstance) error {
	m.instances[m.key(inst.AggregateType, inst.AggregateID)] = &inst
	return nil
}
func (m *memInstanceStore) ListStuck(_ context.Context) ([]types.WorkflowInstance, error) {
	return nil, nil
}

type noopHooks struct{}

func (n *noopHooks) OnTransition(_ context.Context, _ types.TransitionResult, _ time.Duration) {}
func (n *noopHooks) OnGuardFailed(_ context.Context, _, _, _ string, _ error)                  {}
func (n *noopHooks) OnActivityDispatched(_ context.Context, _ types.ActivityInvocation)        {}
func (n *noopHooks) OnActivityCompleted(_ context.Context, _ types.ActivityInvocation, _ *types.ActivityResult) {
}
func (n *noopHooks) OnActivityFailed(_ context.Context, _ types.ActivityInvocation, _ error) {}
func (n *noopHooks) OnStuck(_ context.Context, _ types.WorkflowInstance, _ string)           {}
func (n *noopHooks) OnPostCommitError(_ context.Context, _ string, _ error)                  {}

// capturingHooks records OnGuardFailed calls for assertion in tests.
type capturingHooks struct {
	noopHooks
	guardFailedWorkflow   string
	guardFailedTransition string
	guardFailedGuardName  string
	guardFailedErr        error
}

func (c *capturingHooks) OnGuardFailed(_ context.Context, workflowType, transitionName, guardName string, err error) {
	c.guardFailedWorkflow = workflowType
	c.guardFailedTransition = transitionName
	c.guardFailedGuardName = guardName
	c.guardFailedErr = err
}

type fixedClock struct{ t time.Time }

func (f *fixedClock) Now() time.Time { return f.t }

// ─── sentinel errors ──────────────────────────────────────────────────────────

var (
	errInstanceNotFound  = errors.New("instance not found")
	errInvalidTransition = errors.New("invalid transition")
	errAlreadyTerminal   = errors.New("already terminal")
	errGuardFailed       = errors.New("guard failed")
	errNoMatchingRoute   = errors.New("no matching route")
	errEngineShutdown    = errors.New("engine shutdown")
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func newTestEngine(is types.InstanceStore) *Engine {
	return New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        is,
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		Hooks:                &noopHooks{},
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
	})
}

func simpleDef() *types.Definition {
	return &types.Definition{
		WorkflowType:   "test",
		AggregateType:  "TestAgg",
		InitialState:   "pending",
		TerminalStates: []string{"done"},
		States: map[string]types.StateDef{
			"pending": {Name: "pending"},
			"done":    {Name: "done", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"complete": {
				Name:    "complete",
				Sources: []string{"pending"},
				Target:  "done",
				Event:   "Completed",
			},
		},
	}
}

func simpleInstance(state string) *types.WorkflowInstance {
	now := time.Now()
	return &types.WorkflowInstance{
		ID:            "test-id",
		WorkflowType:  "test",
		AggregateType: "TestAgg",
		AggregateID:   "agg-1",
		CurrentState:  state,
		StateData:     map[string]any{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// ─── Shutdown tests ───────────────────────────────────────────────────────────

func TestShutdown_respectsContextCancellation(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))

	// Hold wg open by incrementing without decrementing
	e.wg.Add(1)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := e.Shutdown(ctx)
	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
}

func TestShutdown_returnsNilWhenComplete(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	// wg is zero — Shutdown should return immediately
	err := e.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

// ─── nil aggregate tests ──────────────────────────────────────────────────────

type aggregateCapturingGuard struct {
	captured any
}

func (g *aggregateCapturingGuard) Check(_ context.Context, aggregate any, _ map[string]any) error {
	g.captured = aggregate
	return nil
}

func TestValidateTransition_passesInstanceToGuard(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	guard := &aggregateCapturingGuard{}
	def := &types.Definition{
		WorkflowType:   "test",
		AggregateType:  "TestAgg",
		InitialState:   "pending",
		TerminalStates: []string{"done"},
		States: map[string]types.StateDef{
			"pending": {Name: "pending"},
			"done":    {Name: "done", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"complete": {
				Name:    "complete",
				Sources: []string{"pending"},
				Target:  "done",
				Event:   "Completed",
				Guards:  []types.Guard{guard},
			},
		},
	}
	inst := simpleInstance("pending")

	_, _, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if guard.captured == nil {
		t.Error("guard received nil aggregate — expected *WorkflowInstance")
	}
	if _, ok := guard.captured.(*types.WorkflowInstance); !ok {
		t.Errorf("expected *WorkflowInstance, got %T", guard.captured)
	}
}

// ─── resolveRoute tests ───────────────────────────────────────────────────────

type alwaysTrue struct{}

func (a *alwaysTrue) Evaluate(_ context.Context, _ any, _ map[string]any) (bool, error) {
	return true, nil
}

type alwaysFalse struct{}

func (a *alwaysFalse) Evaluate(_ context.Context, _ any, _ map[string]any) (bool, error) {
	return false, nil
}

func TestResolveRoute_firstMatchWins(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	tr := types.TransitionDef{
		Routes: []types.Route{
			{Condition: &alwaysTrue{}, Target: "state-a"},
			{Condition: &alwaysTrue{}, Target: "state-b"},
		},
	}
	target, err := e.resolveRoute(context.Background(), tr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "state-a" {
		t.Errorf("expected state-a, got %s", target)
	}
}

func TestResolveRoute_fallsToDefault(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	tr := types.TransitionDef{
		Routes: []types.Route{
			{Condition: &alwaysFalse{}, Target: "state-a"},
			{IsDefault: true, Target: "default-state"},
		},
	}
	target, err := e.resolveRoute(context.Background(), tr, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "default-state" {
		t.Errorf("expected default-state, got %s", target)
	}
}

func TestResolveRoute_noMatchReturnsError(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	tr := types.TransitionDef{
		Routes: []types.Route{
			{Condition: &alwaysFalse{}, Target: "state-a"},
		},
	}
	_, err := e.resolveRoute(context.Background(), tr, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─── runGuards tests ──────────────────────────────────────────────────────────

type failingGuard struct{ msg string }

func (f *failingGuard) Check(_ context.Context, _ any, _ map[string]any) error {
	return errors.New(f.msg)
}

func TestRunGuards_allPass(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	tr := types.TransitionDef{Name: "go", Guards: []types.Guard{&aggregateCapturingGuard{}}}
	if err := e.runGuards(context.Background(), "wf", tr, nil, nil); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestRunGuards_firstFailureReturnsErrGuardFailed(t *testing.T) {
	hooks := &capturingHooks{}
	e := New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        newMemInstanceStore(errInstanceNotFound),
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		Hooks:                hooks,
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
	})
	tr := types.TransitionDef{
		Name:   "go",
		Guards: []types.Guard{&failingGuard{"blocked"}},
	}
	err := e.runGuards(context.Background(), "my-workflow", tr, nil, nil)
	if !errors.Is(err, types.ErrGuardFailed) {
		t.Errorf("expected types.ErrGuardFailed, got %v", err)
	}
	if hooks.guardFailedWorkflow != "my-workflow" {
		t.Errorf("expected OnGuardFailed called with workflow=my-workflow, got %q", hooks.guardFailedWorkflow)
	}
	if hooks.guardFailedTransition != "go" {
		t.Errorf("expected OnGuardFailed called with transition=go, got %q", hooks.guardFailedTransition)
	}
	if hooks.guardFailedErr == nil {
		t.Error("expected OnGuardFailed to receive the guard error, got nil")
	}
}

// ─── copyMap tests ────────────────────────────────────────────────────────────

func TestCopyMap_nil(t *testing.T) {
	if copyMap(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestCopyMap_shallow(t *testing.T) {
	orig := map[string]any{"a": 1, "b": "hello"}
	cp := copyMap(orig)
	if cp["a"] != 1 || cp["b"] != "hello" {
		t.Error("shallow copy values wrong")
	}
	// Modifying copy must not affect original
	cp["a"] = 99
	if orig["a"] != 1 {
		t.Error("copy shares reference with original")
	}
}

func TestCopyMap_deepNested(t *testing.T) {
	orig := map[string]any{"nested": map[string]any{"x": 1}}
	cp := copyMap(orig)
	nested := cp["nested"].(map[string]any)
	nested["x"] = 99
	origNested := orig["nested"].(map[string]any)
	if origNested["x"] != 1 {
		t.Error("nested map is not a deep copy")
	}
}

// ─── evaluateJoinPolicy tests ─────────────────────────────────────────────────

type memChildStoreForJoin struct {
	relations []types.ChildRelation
}

func (m *memChildStoreForJoin) Create(_ context.Context, _ any, r types.ChildRelation) error {
	m.relations = append(m.relations, r)
	return nil
}
func (m *memChildStoreForJoin) GetByChild(_ context.Context, childType, childID string) (*types.ChildRelation, error) {
	return nil, errors.New("not found")
}
func (m *memChildStoreForJoin) GetByParent(_ context.Context, _, _ string) ([]types.ChildRelation, error) {
	return nil, nil
}
func (m *memChildStoreForJoin) GetByGroup(_ context.Context, groupID string) ([]types.ChildRelation, error) {
	var result []types.ChildRelation
	for _, r := range m.relations {
		if r.GroupID == groupID {
			result = append(result, r)
		}
	}
	return result, nil
}
func (m *memChildStoreForJoin) Complete(_ context.Context, _ any, _, _, _ string) error {
	return nil
}

func buildEngineWithChildStore(cs types.ChildStore) *Engine {
	return New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        newMemInstanceStore(errInstanceNotFound),
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		Hooks:                &noopHooks{},
		ChildStore:           cs,
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrNoMatchingSignal:  errors.New("no matching signal"),
		ErrEngineShutdown:    errEngineShutdown,
	})
}

func makeGroupSiblings(groupID string, completed, total int) []types.ChildRelation {
	var siblings []types.ChildRelation
	for i := 0; i < total; i++ {
		status := types.ChildStatusActive
		if i < completed {
			status = types.ChildStatusCompleted
		}
		siblings = append(siblings, types.ChildRelation{
			GroupID: groupID,
			Status:  status,
		})
	}
	return siblings
}

func TestEvaluateJoinPolicy_allMode_notSatisfied(t *testing.T) {
	cs := &memChildStoreForJoin{relations: makeGroupSiblings("grp", 2, 3)}
	e := buildEngineWithChildStore(cs)
	def := simpleDef()
	inst := simpleInstance("pending")
	relation := &types.ChildRelation{GroupID: "grp", JoinPolicy: "ALL"}

	_, err := e.evaluateJoinPolicy(context.Background(), relation, def, inst)
	// Not all complete, so must return ErrNoMatchingSignal
	if err == nil {
		t.Fatal("expected error for unsatisfied ALL join, got nil")
	}
}

func TestEvaluateJoinPolicy_anyMode_satisfied(t *testing.T) {
	cs := &memChildStoreForJoin{relations: makeGroupSiblings("grp", 1, 3)}
	e := buildEngineWithChildStore(cs)

	def := &types.Definition{
		WorkflowType:   "test",
		AggregateType:  "TestAgg",
		InitialState:   "pending",
		TerminalStates: []string{"done"},
		States: map[string]types.StateDef{
			"pending": {Name: "pending"},
			"done":    {Name: "done", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"joined": {
				Name:        "joined",
				Sources:     []string{"pending"},
				Target:      "done",
				TriggerType: types.TriggerChildrenJoined,
			},
		},
	}
	inst := simpleInstance("pending")
	relation := &types.ChildRelation{GroupID: "grp", JoinPolicy: "ANY",
		ParentAggregateType: "TestAgg", ParentAggregateID: "agg-1"}

	// evaluateJoinPolicy will call Transition when satisfied — but we have no full engine wiring.
	// We just verify it doesn't return ErrNoMatchingSignal for the join policy check itself.
	// The actual Transition call will fail because InstanceStore doesn't have "agg-1" pre-created.
	_, err := e.evaluateJoinPolicy(context.Background(), relation, def, inst)
	// ANY with 1/3 complete is satisfied — error (if any) will be from Transition, not join check.
	// We expect no "join policy not yet satisfied" error.
	if err != nil && strings.Contains(err.Error(), "not yet satisfied") {
		t.Errorf("ANY policy with 1 completed should be satisfied, got: %v", err)
	}
}

func TestEvaluateJoinPolicy_allMode_satisfied(t *testing.T) {
	cs := &memChildStoreForJoin{relations: makeGroupSiblings("grp", 3, 3)} // all complete
	e := buildEngineWithChildStore(cs)

	joinDef := &types.Definition{
		WorkflowType:   "test",
		AggregateType:  "TestAgg",
		InitialState:   "pending",
		TerminalStates: []string{"done"},
		States: map[string]types.StateDef{
			"pending": {Name: "pending"},
			"done":    {Name: "done", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"joined": {
				Name:        "joined",
				Sources:     []string{"pending"},
				Target:      "done",
				TriggerType: types.TriggerChildrenJoined,
			},
		},
	}
	inst := simpleInstance("pending")
	relation := &types.ChildRelation{GroupID: "grp", JoinPolicy: "ALL",
		ParentAggregateType: "TestAgg", ParentAggregateID: "agg-1"}

	_, err := e.evaluateJoinPolicy(context.Background(), relation, joinDef, inst)
	// ALL with 3/3 is satisfied — any error is from Transition firing, not the join check
	if err != nil && strings.Contains(err.Error(), "not yet satisfied") {
		t.Errorf("ALL policy with all completed should be satisfied, got: %v", err)
	}
}

func TestEvaluateJoinPolicy_nMode_notSatisfied(t *testing.T) {
	cs := &memChildStoreForJoin{relations: makeGroupSiblings("grp", 1, 5)} // 1/5, need 3
	e := buildEngineWithChildStore(cs)

	nDef := &types.Definition{
		WorkflowType:   "test",
		AggregateType:  "TestAgg",
		InitialState:   "pending",
		TerminalStates: []string{"done"},
		States: map[string]types.StateDef{
			"pending": {Name: "pending"},
			"done":    {Name: "done", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"spawn": {
				Name:    "spawn",
				Sources: []string{"pending"},
				Target:  "pending",
				ChildrenDef: &types.ChildrenDef{
					WorkflowType: "child-wf",
					Join:         types.JoinPolicy{Mode: "N", Count: 3},
					InputsFn:     func(_ any) []map[string]any { return make([]map[string]any, 5) },
				},
			},
		},
	}
	inst := simpleInstance("pending")
	relation := &types.ChildRelation{GroupID: "grp", JoinPolicy: "N",
		ParentAggregateType: "TestAgg", ParentAggregateID: "agg-1"}

	_, err := e.evaluateJoinPolicy(context.Background(), relation, nDef, inst)
	if err == nil {
		t.Fatal("expected error for N mode with 1/5 completed (need 3), got nil")
	}
	if !strings.Contains(err.Error(), "not yet satisfied") {
		t.Errorf("expected 'not yet satisfied' error, got: %v", err)
	}
}

// ─── validateTransition tests ─────────────────────────────────────────────────

func TestValidateTransition_valid(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("pending")

	tr, target, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if tr.Name != "complete" {
		t.Errorf("expected tr.Name=complete, got %q", tr.Name)
	}
	if target != "done" {
		t.Errorf("expected target=done, got %q", target)
	}
}

func TestValidateTransition_unknownTransition(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("pending")

	_, _, err := e.validateTransition(context.Background(), def, inst, "nonexistent", nil)
	if !errors.Is(err, errInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestValidateTransition_wrongSourceState(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("done") // terminal — but also wrong source

	_, _, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if err == nil {
		t.Fatal("expected error for wrong source state, got nil")
	}
}

func TestValidateTransition_alreadyTerminal(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	inst := simpleInstance("done")

	_, _, err := e.validateTransition(context.Background(), def, inst, "complete", nil)
	if !errors.Is(err, errAlreadyTerminal) {
		t.Errorf("expected ErrAlreadyTerminal, got %v", err)
	}
}
