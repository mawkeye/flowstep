package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/mawkeye/flowstep/internal/graph"
	"github.com/mawkeye/flowstep/types"
)

// ─── SideEffect test stubs ────────────────────────────────────────────────────

// capturingEventStore records all appended events.
type capturingEventStore struct {
	noopEventStore
	appended []types.DomainEvent
}

func (c *capturingEventStore) Append(_ context.Context, _ any, event types.DomainEvent) error {
	c.appended = append(c.appended, event)
	return nil
}

// failingCommitTx succeeds on Begin but fails on Commit.
type failingCommitTx struct {
	commitErr error
}

func (f *failingCommitTx) Begin(_ context.Context) (any, error)   { return struct{}{}, nil }
func (f *failingCommitTx) Commit(_ context.Context, _ any) error  { return f.commitErr }
func (f *failingCommitTx) Rollback(_ context.Context, _ any) error { return nil }

func newSideEffectEngine(is types.InstanceStore, es types.EventStore, tx types.TxProvider) *Engine {
	return New(Deps{
		EventStore:          es,
		InstanceStore:       is,
		TxProvider:          tx,
		Clock:               &fixedClock{},
		ErrInstanceNotFound: errInstanceNotFound,
		ErrEngineShutdown:   errEngineShutdown,
	})
}

// ─── SideEffect tests ─────────────────────────────────────────────────────────

func TestSideEffect_persistsEventWithCorrectType(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	inst := simpleInstance("pending")
	inst.CorrelationID = "corr-42"
	inst.WorkflowType = "TestWorkflow"
	inst.WorkflowVersion = 2
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	cs := &capturingEventStore{}
	e := newSideEffectEngine(is, cs, &noopTx{})

	_, err := e.SideEffect(context.Background(), "TestAgg", "agg-1", "GenerateID", func() (any, error) {
		return "uuid-xyz", nil
	})
	if err != nil {
		t.Fatalf("SideEffect error: %v", err)
	}
	if len(cs.appended) != 1 {
		t.Fatalf("expected 1 appended event, got %d", len(cs.appended))
	}
	ev := cs.appended[0]
	if ev.EventType != types.EventTypeSideEffect {
		t.Errorf("EventType = %q, want %q", ev.EventType, types.EventTypeSideEffect)
	}
	if ev.AggregateType != "TestAgg" {
		t.Errorf("AggregateType = %q, want %q", ev.AggregateType, "TestAgg")
	}
	if ev.AggregateID != "agg-1" {
		t.Errorf("AggregateID = %q, want %q", ev.AggregateID, "agg-1")
	}
	if ev.CorrelationID != "corr-42" {
		t.Errorf("CorrelationID = %q, want %q", ev.CorrelationID, "corr-42")
	}
	if ev.WorkflowType != "TestWorkflow" {
		t.Errorf("WorkflowType = %q, want %q", ev.WorkflowType, "TestWorkflow")
	}
	if ev.WorkflowVersion != 2 {
		t.Errorf("WorkflowVersion = %d, want 2", ev.WorkflowVersion)
	}
	name, result, ok := types.ParseSideEffect(ev)
	if !ok {
		t.Fatal("ParseSideEffect returned ok=false")
	}
	if name != "GenerateID" {
		t.Errorf("name = %q, want %q", name, "GenerateID")
	}
	if result != "uuid-xyz" {
		t.Errorf("result = %v, want %q", result, "uuid-xyz")
	}
}

func TestSideEffect_returnsFnResult(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	inst := simpleInstance("pending")
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	e := newSideEffectEngine(is, &noopEventStore{}, &noopTx{})

	got, err := e.SideEffect(context.Background(), "TestAgg", "agg-1", "GenTimestamp", func() (any, error) {
		return int64(12345678), nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != int64(12345678) {
		t.Errorf("result = %v, want 12345678", got)
	}
}

func TestSideEffect_fnError_noEventPersisted(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	inst := simpleInstance("pending")
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	cs := &capturingEventStore{}
	e := newSideEffectEngine(is, cs, &noopTx{})

	fnErr := errors.New("external service unavailable")
	_, err := e.SideEffect(context.Background(), "TestAgg", "agg-1", "GenID", func() (any, error) {
		return nil, fnErr
	})
	if !errors.Is(err, fnErr) {
		t.Errorf("expected fnErr, got %v", err)
	}
	if len(cs.appended) != 0 {
		t.Errorf("expected 0 appended events, got %d", len(cs.appended))
	}
}

func TestSideEffect_engineShutdown(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newSideEffectEngine(is, &noopEventStore{}, &noopTx{})
	e.shutdown.Store(true)

	_, err := e.SideEffect(context.Background(), "TestAgg", "agg-1", "GenID", func() (any, error) {
		return "x", nil
	})
	if !errors.Is(err, errEngineShutdown) {
		t.Errorf("expected ErrEngineShutdown, got %v", err)
	}
}

func TestSideEffect_instanceNotFound(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	// instance not seeded — Get returns errInstanceNotFound
	e := newSideEffectEngine(is, &noopEventStore{}, &noopTx{})

	fnCalled := false
	_, err := e.SideEffect(context.Background(), "TestAgg", "missing-id", "GenID", func() (any, error) {
		fnCalled = true
		return "x", nil
	})
	if !errors.Is(err, errInstanceNotFound) {
		t.Errorf("expected ErrInstanceNotFound, got %v", err)
	}
	if fnCalled {
		t.Error("fn was called for non-existent aggregate — expected fn NOT to execute before aggregate is verified")
	}
}

func TestSideEffect_commitFails_returnsError(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	inst := simpleInstance("pending")
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	commitErr := errors.New("db commit failed")
	failTx := &failingCommitTx{commitErr: commitErr}
	e := newSideEffectEngine(is, &noopEventStore{}, failTx)

	fnExecuted := false
	_, err := e.SideEffect(context.Background(), "TestAgg", "agg-1", "GenID", func() (any, error) {
		fnExecuted = true
		return "result", nil
	})
	if !fnExecuted {
		t.Error("fn was not executed before commit")
	}
	if !errors.Is(err, commitErr) {
		t.Errorf("expected commitErr, got %v", err)
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
			"pending": {Name: "pending", IsInitial: true},
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
	cm, err := graph.Compile(def, graph.Sentinels{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	inst := simpleInstance("pending")

	_, _, err = e.validateTransition(context.Background(), cm, inst, "complete", nil)
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

// compileDef compiles simpleDef() for use in white-box validateTransition tests.
func compileDef(t *testing.T, def *types.Definition) *graph.CompiledMachine {
	t.Helper()
	cm, err := graph.Compile(def, graph.Sentinels{})
	if err != nil {
		t.Fatalf("compileDef: %v", err)
	}
	return cm
}

// ─── validateTransition tests ─────────────────────────────────────────────────

func TestValidateTransition_valid(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, simpleDef())
	inst := simpleInstance("pending")

	ct, target, err := e.validateTransition(context.Background(), cm, inst, "complete", nil)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if ct.Def.Name != "complete" {
		t.Errorf("expected ct.Def.Name=complete, got %q", ct.Def.Name)
	}
	if target != "done" {
		t.Errorf("expected target=done, got %q", target)
	}
}

func TestValidateTransition_unknownTransition(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, simpleDef())
	inst := simpleInstance("pending")

	_, _, err := e.validateTransition(context.Background(), cm, inst, "nonexistent", nil)
	if !errors.Is(err, errInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestValidateTransition_wrongSourceState(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, simpleDef())
	inst := simpleInstance("done") // terminal — but also wrong source

	_, _, err := e.validateTransition(context.Background(), cm, inst, "complete", nil)
	if err == nil {
		t.Fatal("expected error for wrong source state, got nil")
	}
}

func TestValidateTransition_alreadyTerminal(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, simpleDef())
	inst := simpleInstance("done")

	_, _, err := e.validateTransition(context.Background(), cm, inst, "complete", nil)
	if !errors.Is(err, errAlreadyTerminal) {
		t.Errorf("expected ErrAlreadyTerminal, got %v", err)
	}
}
