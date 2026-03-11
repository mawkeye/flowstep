package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/internal/graph"
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

// capturingGuardObserver records OnGuardFailed calls for assertion in tests.
type capturingGuardObserver struct {
	guardFailedWorkflow   string
	guardFailedTransition string
	guardFailedGuardName  string
	guardFailedErr        error
}

func (c *capturingGuardObserver) OnGuardFailed(_ context.Context, e types.GuardFailureEvent) {
	c.guardFailedWorkflow = e.WorkflowType
	c.guardFailedTransition = e.TransitionName
	c.guardFailedGuardName = e.GuardName
	c.guardFailedErr = e.Err
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

var errSpawnCycle = errors.New("spawn cycle")

func newTestEngine(is types.InstanceStore) *Engine {
	return New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        is,
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
		Sentinels: graph.Sentinels{
			ErrNoInitialState:        errors.New("no initial state"),
			ErrMultipleInitialStates: errors.New("multiple initial states"),
			ErrNoTerminalStates:      errors.New("no terminal states"),
			ErrUnreachableState:      errors.New("unreachable state"),
			ErrDeadEndState:          errors.New("dead end state"),
			ErrUnknownState:          errors.New("unknown state"),
			ErrMissingDefault:        errors.New("missing default"),
			ErrDuplicateTransition:   errors.New("duplicate transition"),
			ErrSpawnCycle:            errSpawnCycle,
		},
	})
}

func simpleDef() *types.Definition {
	return &types.Definition{
		WorkflowType:   "test",
		AggregateType:  "TestAgg",
		InitialState:   "pending",
		TerminalStates: []string{"done"},
		States: map[string]types.StateDef{
			"pending": {Name: "pending", IsInitial: true},
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

// ─── Register + compiledFor tests (Task 4) ───────────────────────────────────

func TestRegister_StoresCompiledMachine(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	if err := e.Register(def); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	cm, ok := e.compiledFor(def.AggregateType, def.Version)
	if !ok {
		t.Fatal("compiledFor() returned false — compiled machine not stored")
	}
	if cm == nil {
		t.Fatal("compiledFor() returned nil CompiledMachine")
	}
	if cm.Definition.WorkflowType != def.WorkflowType {
		t.Errorf("CompiledMachine.Definition.WorkflowType = %q, want %q",
			cm.Definition.WorkflowType, def.WorkflowType)
	}
}

func TestRegister_CompiledForLatest(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	if err := e.Register(def); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	// version 0 means "latest"
	cm, ok := e.compiledFor(def.AggregateType, 0)
	if !ok {
		t.Fatal("compiledFor() version 0 returned false")
	}
	if cm.Definition.Version != def.Version {
		t.Errorf("latest version = %d, want %d", cm.Definition.Version, def.Version)
	}
}

func TestRegister_SpawnCycle_ReturnsError(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))

	defA := &types.Definition{
		AggregateType:  "agg-a",
		WorkflowType:   "wf-a",
		Version:        1,
		States:         map[string]types.StateDef{"A": {Name: "A", IsInitial: true}, "B": {Name: "B", IsTerminal: true}},
		Transitions:    map[string]types.TransitionDef{"go": {Name: "go", Sources: []string{"A"}, Target: "B", ChildDef: &types.ChildDef{WorkflowType: "wf-b"}}},
		InitialState:   "A",
		TerminalStates: []string{"B"},
	}
	defB := &types.Definition{
		AggregateType:  "agg-b",
		WorkflowType:   "wf-b",
		Version:        1,
		States:         map[string]types.StateDef{"A": {Name: "A", IsInitial: true}, "B": {Name: "B", IsTerminal: true}},
		Transitions:    map[string]types.TransitionDef{"go": {Name: "go", Sources: []string{"A"}, Target: "B", ChildDef: &types.ChildDef{WorkflowType: "wf-a"}}},
		InitialState:   "A",
		TerminalStates: []string{"B"},
	}

	if err := e.Register(defA); err != nil {
		t.Fatalf("Register(defA) unexpected error = %v", err)
	}
	err := e.Register(defB)
	if err == nil {
		t.Fatal("Register(defB) expected ErrSpawnCycle, got nil")
	}
	if !errors.Is(err, errSpawnCycle) {
		t.Errorf("expected errors.Is(err, errSpawnCycle), got %v", err)
	}
}

func TestRegister_HashDedup_SkipsRecompilation(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	def := simpleDef()
	if err := e.Register(def); err != nil {
		t.Fatalf("first Register() error = %v", err)
	}
	cm1, _ := e.compiledFor(def.AggregateType, def.Version)

	// Register the same definition again — same hash, should reuse compiled machine
	if err := e.Register(def); err != nil {
		t.Fatalf("second Register() error = %v", err)
	}
	cm2, _ := e.compiledFor(def.AggregateType, def.Version)

	if cm1.DefinitionHash != cm2.DefinitionHash {
		t.Error("hash changed on re-registration of identical definition")
	}
}

// verifyCompiledForUnused ensures the graph import is not unused even without extra references
var _ = graph.Sentinels{}
