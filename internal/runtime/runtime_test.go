package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
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
