package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

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

// ─── Event bubbling tests ──────────────────────────────────────────────────────

// bubblingDef builds a hierarchical definition where "approve" is defined on parent PROCESSING,
// not on the leaf VALIDATING. Tests event bubbling from VALIDATING → PROCESSING.
func bubblingDef() *types.Definition {
	return &types.Definition{
		WorkflowType:  "order",
		AggregateType: "TestAgg",
		Version:       1,
		InitialState:  "CREATED",
		TerminalStates: []string{"APPROVED"},
		States: map[string]types.StateDef{
			"CREATED":    {Name: "CREATED", IsInitial: true},
			"PROCESSING": {Name: "PROCESSING", IsCompound: true, InitialChild: "VALIDATING", Children: []string{"VALIDATING", "APPROVED"}},
			"VALIDATING": {Name: "VALIDATING", Parent: "PROCESSING"},
			"APPROVED":   {Name: "APPROVED", Parent: "PROCESSING", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"start":   {Name: "start", Sources: []string{"CREATED"}, Target: "PROCESSING"},
			"approve": {Name: "approve", Sources: []string{"PROCESSING"}, Target: "APPROVED"},
		},
	}
}

func TestValidateTransition_EventBubbling_ParentStateHandles(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, bubblingDef())
	// Instance is at leaf VALIDATING; "approve" is defined on parent PROCESSING
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"

	ct, target, err := e.validateTransition(context.Background(), cm, inst, "approve", nil)
	if err != nil {
		t.Fatalf("validateTransition with event bubbling failed: %v", err)
	}
	if ct.Def.Name != "approve" {
		t.Errorf("compiled transition name = %q, want approve", ct.Def.Name)
	}
	if target != "APPROVED" {
		t.Errorf("target = %q, want APPROVED", target)
	}
}

func TestValidateTransition_EventBubbling_LeafHandlesDirect(t *testing.T) {
	// When the leaf itself has the transition, it fires directly (no bubbling).
	def := &types.Definition{
		WorkflowType:  "order",
		AggregateType: "TestAgg",
		Version:       1,
		InitialState:  "CREATED",
		TerminalStates: []string{"APPROVED"},
		States: map[string]types.StateDef{
			"CREATED":    {Name: "CREATED", IsInitial: true},
			"PROCESSING": {Name: "PROCESSING", IsCompound: true, InitialChild: "VALIDATING", Children: []string{"VALIDATING", "APPROVED"}},
			"VALIDATING": {Name: "VALIDATING", Parent: "PROCESSING"},
			"APPROVED":   {Name: "APPROVED", Parent: "PROCESSING", IsTerminal: true},
		},
		Transitions: map[string]types.TransitionDef{
			"start":   {Name: "start", Sources: []string{"CREATED"}, Target: "PROCESSING"},
			// "approve" is on VALIDATING directly (leaf handles it)
			"approve": {Name: "approve", Sources: []string{"VALIDATING"}, Target: "APPROVED"},
		},
	}
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, def)
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"

	ct, target, err := e.validateTransition(context.Background(), cm, inst, "approve", nil)
	if err != nil {
		t.Fatalf("validateTransition direct (no bubble) failed: %v", err)
	}
	if ct.Def.Name != "approve" {
		t.Errorf("compiled transition name = %q, want approve", ct.Def.Name)
	}
	if target != "APPROVED" {
		t.Errorf("target = %q, want APPROVED", target)
	}
}

func TestValidateTransition_EventBubbling_NeitherLeafNorAncestor_Fails(t *testing.T) {
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, bubblingDef())
	// Instance is at leaf VALIDATING; "start" transition is only on CREATED (unrelated)
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"

	_, _, err := e.validateTransition(context.Background(), cm, inst, "start", nil)
	if !errors.Is(err, errInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition when no ancestor handles event, got %v", err)
	}
}

// ─── Task 7: exit/entry sequences + compound resolution tests ─────────────────

// recordingResolver implements ActivityRunner + ActivityResolver, recording call order.
type recordingResolver struct {
	order  []string
	failOn string
}

func (r *recordingResolver) Dispatch(_ context.Context, _ types.ActivityInvocation) error {
	return nil
}
func (r *recordingResolver) Resolve(name string) (types.Activity, bool) {
	return &callbackActivity{name: name, recorder: r}, true
}

type callbackActivity struct {
	name     string
	recorder *recordingResolver
}

func (a *callbackActivity) Name() string { return a.name }
func (a *callbackActivity) Execute(_ context.Context, _ types.ActivityInput) (*types.ActivityResult, error) {
	a.recorder.order = append(a.recorder.order, a.name)
	if a.name == a.recorder.failOn {
		return nil, errors.New("activity failed: " + a.name)
	}
	return &types.ActivityResult{}, nil
}

// activityHierarchyDef: CREATED→PROCESSING(compound)→VALIDATING(leaf)|APPROVED(leaf,terminal)
// "approve" on PROCESSING bubbles from VALIDATING.
func activityHierarchyDef() *types.Definition {
	return &types.Definition{
		WorkflowType:   "order",
		AggregateType:  "TestAgg",
		Version:        1,
		InitialState:   "CREATED",
		TerminalStates: []string{"APPROVED"},
		States: map[string]types.StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true, ExitActivity: "on_exit_created"},
			"PROCESSING": {
				Name: "PROCESSING", IsCompound: true,
				InitialChild: "VALIDATING", Children: []string{"VALIDATING", "APPROVED"},
				EntryActivity: "on_enter_processing", ExitActivity: "on_exit_processing",
			},
			"VALIDATING": {Name: "VALIDATING", Parent: "PROCESSING",
				EntryActivity: "on_enter_validating", ExitActivity: "on_exit_validating"},
			"APPROVED": {Name: "APPROVED", Parent: "PROCESSING", IsTerminal: true,
				EntryActivity: "on_enter_approved"},
		},
		Transitions: map[string]types.TransitionDef{
			"start":   {Name: "start", Sources: []string{"CREATED"}, Target: "PROCESSING"},
			"approve": {Name: "approve", Sources: []string{"PROCESSING"}, Target: "APPROVED"},
		},
	}
}

func newHierarchyEngine(is types.InstanceStore, runner types.ActivityRunner) *Engine {
	return New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        is,
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		ActivityRunner:       runner,
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
		Sentinels: graph.Sentinels{
			ErrNoInitialState:              errors.New("no initial"),
			ErrNoTerminalStates:            errors.New("no terminal"),
			ErrDeadEndState:                errors.New("dead end"),
			ErrUnreachableState:            errors.New("unreachable"),
			ErrUnknownState:                errors.New("unknown"),
			ErrMissingDefault:              errors.New("missing default"),
			ErrDuplicateTransition:         errors.New("duplicate"),
			ErrSpawnCycle:                  errSpawnCycle,
			ErrCompoundStateNoInitialChild: errors.New("compound no initial"),
			ErrOrphanedChild:               errors.New("orphaned"),
			ErrCircularHierarchy:           errors.New("circular"),
		},
	})
}

func TestCreateInstance_CompoundInitialState_ResolvesToLeaf(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newHierarchyEngine(is, &recordingResolver{})

	// Build a CompiledMachine where InitialState is compound and InitialLeafMap maps it to a leaf.
	cm := &graph.CompiledMachine{
		Definition: &types.Definition{
			WorkflowType:  "order",
			AggregateType: "TestAgg",
			Version:       1,
			InitialState:  "PROCESSING", // compound — must resolve to VALIDATING
			States: map[string]types.StateDef{
				"PROCESSING": {Name: "PROCESSING", IsCompound: true, IsInitial: true,
					InitialChild: "VALIDATING", Children: []string{"VALIDATING"}},
				"VALIDATING": {Name: "VALIDATING", Parent: "PROCESSING"},
			},
		},
		InitialLeafMap: map[string]string{"PROCESSING": "VALIDATING"},
	}

	inst, err := e.createInstance(context.Background(), cm, "TestAgg", "agg-compound")
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if inst.CurrentState != "VALIDATING" {
		t.Errorf("CurrentState = %q, want VALIDATING (resolved from compound PROCESSING)", inst.CurrentState)
	}
}

func TestCommitTransition_CompoundTarget_ResolvesToLeaf(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	recorder := &recordingResolver{}
	e := newHierarchyEngine(is, recorder)
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	inst := simpleInstance("CREATED")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	result, err := e.Transition(context.Background(), "TestAgg", "agg-1", "start", "actor", nil)
	if err != nil {
		t.Fatalf("Transition error: %v", err)
	}
	if result.NewState != "VALIDATING" {
		t.Errorf("NewState = %q, want VALIDATING (resolved leaf of PROCESSING)", result.NewState)
	}
	if result.Instance.CurrentState != "VALIDATING" {
		t.Errorf("Instance.CurrentState = %q, want VALIDATING", result.Instance.CurrentState)
	}
}

func TestCommitTransition_ExitEntry_ActivitiesFire_InOrder(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	recorder := &recordingResolver{}
	e := newHierarchyEngine(is, recorder)
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Instance already at VALIDATING (leaf of PROCESSING)
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	_, err := e.Transition(context.Background(), "TestAgg", "agg-1", "approve", "actor", nil)
	if err != nil {
		t.Fatalf("Transition error: %v", err)
	}
	// LCA(VALIDATING,APPROVED)=PROCESSING: exit [VALIDATING], entry [APPROVED]
	want := []string{"on_exit_validating", "on_enter_approved"}
	if len(recorder.order) != len(want) {
		t.Fatalf("activity order = %v, want %v", recorder.order, want)
	}
	for i, w := range want {
		if recorder.order[i] != w {
			t.Errorf("activity[%d] = %q, want %q", i, recorder.order[i], w)
		}
	}
}

func TestCommitTransition_StartTransition_EntryActivitiesFire_FullPath(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	recorder := &recordingResolver{}
	e := newHierarchyEngine(is, recorder)
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	inst := simpleInstance("CREATED")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	_, err := e.Transition(context.Background(), "TestAgg", "agg-1", "start", "actor", nil)
	if err != nil {
		t.Fatalf("Transition error: %v", err)
	}
	// LCA(CREATED,VALIDATING)="": exit [CREATED], entry [PROCESSING, VALIDATING]
	want := []string{"on_exit_created", "on_enter_processing", "on_enter_validating"}
	if len(recorder.order) != len(want) {
		t.Fatalf("activity order = %v, want %v", recorder.order, want)
	}
	for i, w := range want {
		if recorder.order[i] != w {
			t.Errorf("activity[%d] = %q, want %q", i, recorder.order[i], w)
		}
	}
}

func TestCommitTransition_ExitActivityFailure_AbortsTransition(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	recorder := &recordingResolver{failOn: "on_exit_validating"}
	e := newHierarchyEngine(is, recorder)
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	_, err := e.Transition(context.Background(), "TestAgg", "agg-1", "approve", "actor", nil)
	if err == nil {
		t.Fatal("expected error from exit activity failure, got nil")
	}
	// Instance state must remain at VALIDATING (source leaf)
	got, _ := is.Get(context.Background(), "TestAgg", "agg-1")
	if got.CurrentState != "VALIDATING" {
		t.Errorf("CurrentState = %q after exit failure, want VALIDATING", got.CurrentState)
	}
}

func TestCommitTransition_EntryActivityFailure_MarksStuck_SourceLeafPreserved(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	recorder := &recordingResolver{failOn: "on_enter_approved"}
	e := newHierarchyEngine(is, recorder)
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	_, err := e.Transition(context.Background(), "TestAgg", "agg-1", "approve", "actor", nil)
	if err == nil {
		t.Fatal("expected error from entry activity failure, got nil")
	}
	got, _ := is.Get(context.Background(), "TestAgg", "agg-1")
	if got.CurrentState != "VALIDATING" {
		t.Errorf("CurrentState = %q, want VALIDATING (source leaf preserved)", got.CurrentState)
	}
	if !got.IsStuck {
		t.Error("IsStuck should be true after entry activity failure")
	}
}

func TestForceState_CompoundTarget_ResolvesToLeaf(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newHierarchyEngine(is, nil)
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	inst := simpleInstance("APPROVED")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	result, err := e.ForceState(context.Background(), "TestAgg", "agg-1", "PROCESSING", "admin", "recovery")
	if err != nil {
		t.Fatalf("ForceState error: %v", err)
	}
	if result.NewState != "VALIDATING" {
		t.Errorf("ForceState NewState = %q, want VALIDATING (resolved leaf)", result.NewState)
	}
}

// ─── Task 8: dangling task cleanup tests ──────────────────────────────────────

// trackingTaskStore implements types.TaskStore + types.TaskInvalidator.
// Stores tasks in a map and records InvalidateByStates calls.
type trackingTaskStore struct {
	tasks           map[string]types.PendingTask
	invalidatedArgs []invalidateCall
}

type invalidateCall struct {
	aggType, aggID string
	states         []string
}

func newTrackingTaskStore() *trackingTaskStore {
	return &trackingTaskStore{tasks: make(map[string]types.PendingTask)}
}

func (s *trackingTaskStore) Create(_ context.Context, _ any, task types.PendingTask) error {
	s.tasks[task.ID] = task
	return nil
}
func (s *trackingTaskStore) Get(_ context.Context, id string) (*types.PendingTask, error) {
	t, ok := s.tasks[id]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := t
	return &cp, nil
}
func (s *trackingTaskStore) GetByAggregate(_ context.Context, _, _ string) ([]types.PendingTask, error) {
	return nil, nil
}
func (s *trackingTaskStore) Complete(_ context.Context, _ any, taskID, choice, actor string) error {
	t, ok := s.tasks[taskID]
	if !ok {
		return errors.New("not found")
	}
	t.Status = types.TaskStatusCompleted
	t.Choice = choice
	t.CompletedBy = actor
	s.tasks[taskID] = t
	return nil
}
func (s *trackingTaskStore) ListPending(_ context.Context) ([]types.PendingTask, error) {
	return nil, nil
}
func (s *trackingTaskStore) ListExpired(_ context.Context) ([]types.PendingTask, error) {
	return nil, nil
}

// InvalidateByStates — implements types.TaskInvalidator.
func (s *trackingTaskStore) InvalidateByStates(_ context.Context, _ any, aggType, aggID string, states []string) error {
	s.invalidatedArgs = append(s.invalidatedArgs, invalidateCall{aggType, aggID, states})
	stateSet := make(map[string]bool, len(states))
	for _, st := range states {
		stateSet[st] = true
	}
	for id, t := range s.tasks {
		if t.AggregateType == aggType && t.AggregateID == aggID &&
			t.Status == types.TaskStatusPending && stateSet[t.State] {
			t.Status = types.TaskStatusCancelled
			s.tasks[id] = t
		}
	}
	return nil
}

// noopTaskStore implements types.TaskStore but NOT types.TaskInvalidator.
type noopTaskStore struct{}

func (n *noopTaskStore) Create(_ context.Context, _ any, _ types.PendingTask) error { return nil }
func (n *noopTaskStore) Get(_ context.Context, _ string) (*types.PendingTask, error) {
	return nil, errors.New("not found")
}
func (n *noopTaskStore) GetByAggregate(_ context.Context, _, _ string) ([]types.PendingTask, error) {
	return nil, nil
}
func (n *noopTaskStore) Complete(_ context.Context, _ any, _, _, _ string) error { return nil }
func (n *noopTaskStore) ListPending(_ context.Context) ([]types.PendingTask, error) {
	return nil, nil
}
func (n *noopTaskStore) ListExpired(_ context.Context) ([]types.PendingTask, error) {
	return nil, nil
}

func newHierarchyEngineWithTasks(is types.InstanceStore, runner types.ActivityRunner, ts types.TaskStore) *Engine {
	return New(Deps{
		EventStore:           &noopEventStore{},
		InstanceStore:        is,
		TxProvider:           &noopTx{},
		Clock:                &fixedClock{t: time.Now()},
		ActivityRunner:       runner,
		TaskStore:            ts,
		ErrInstanceNotFound:  errInstanceNotFound,
		ErrInvalidTransition: errInvalidTransition,
		ErrAlreadyTerminal:   errAlreadyTerminal,
		ErrGuardFailed:       errGuardFailed,
		ErrNoMatchingRoute:   errNoMatchingRoute,
		ErrEngineShutdown:    errEngineShutdown,
		Sentinels: graph.Sentinels{
			ErrNoInitialState:              errors.New("no initial"),
			ErrNoTerminalStates:            errors.New("no terminal"),
			ErrDeadEndState:                errors.New("dead end"),
			ErrUnreachableState:            errors.New("unreachable"),
			ErrUnknownState:                errors.New("unknown"),
			ErrMissingDefault:              errors.New("missing default"),
			ErrDuplicateTransition:         errors.New("duplicate"),
			ErrSpawnCycle:                  errSpawnCycle,
			ErrCompoundStateNoInitialChild: errors.New("compound no initial"),
			ErrOrphanedChild:               errors.New("orphaned"),
			ErrCircularHierarchy:           errors.New("circular"),
		},
	})
}

func TestCommitTransition_DanglingTaskCleanup_CancelsTaskInExitedState(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	ts := newTrackingTaskStore()
	e := newHierarchyEngineWithTasks(is, nil, ts)
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Instance is at VALIDATING (leaf of PROCESSING).
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	// Pre-create a pending task for the VALIDATING state.
	pendingTask := types.PendingTask{
		ID:            "task-1",
		WorkflowType:  "order",
		AggregateType: inst.AggregateType,
		AggregateID:   inst.AggregateID,
		State:         "VALIDATING",
		Status:        types.TaskStatusPending,
	}
	_ = ts.Create(context.Background(), nil, pendingTask)

	// Transition "approve" exits VALIDATING → enters APPROVED.
	_, err := e.Transition(context.Background(), "TestAgg", "agg-1", "approve", "actor", nil)
	if err != nil {
		t.Fatalf("Transition error: %v", err)
	}

	// The VALIDATING task should now be CANCELLED.
	got, _ := ts.Get(context.Background(), "task-1")
	if got.Status != types.TaskStatusCancelled {
		t.Errorf("task status = %q, want CANCELLED", got.Status)
	}
}

func TestCommitTransition_DanglingTaskCleanup_NoTaskInvalidator_DegradeGracefully(t *testing.T) {
	is := newMemInstanceStore(errInstanceNotFound)
	e := newHierarchyEngineWithTasks(is, nil, &noopTaskStore{})
	if err := e.Register(activityHierarchyDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"
	inst.WorkflowVersion = 1
	is.instances[is.key(inst.AggregateType, inst.AggregateID)] = inst

	// noopTaskStore does not implement TaskInvalidator; the transition must still succeed.
	result, err := e.Transition(context.Background(), "TestAgg", "agg-1", "approve", "actor", nil)
	if err != nil {
		t.Fatalf("Transition error (expected graceful degradation): %v", err)
	}
	if result.NewState != "APPROVED" {
		t.Errorf("NewState = %q, want APPROVED", result.NewState)
	}
}

func TestValidateTransition_EventBubbling_GrandparentHandles(t *testing.T) {
	// 3-level: ROOT (compound) → ORDER (compound) → VALIDATING (leaf)
	// "approve" is on ROOT; current state is VALIDATING
	def := &types.Definition{
		WorkflowType:  "order",
		AggregateType: "TestAgg",
		Version:       1,
		InitialState:  "CREATED",
		TerminalStates: []string{"APPROVED"},
		States: map[string]types.StateDef{
			"CREATED":    {Name: "CREATED", IsInitial: true},
			"ROOT":       {Name: "ROOT", IsCompound: true, InitialChild: "ORDER", Children: []string{"ORDER", "APPROVED"}},
			"ORDER":      {Name: "ORDER", IsCompound: true, InitialChild: "VALIDATING", Children: []string{"VALIDATING"}, Parent: "ROOT"},
			"VALIDATING": {Name: "VALIDATING", Parent: "ORDER"},
			"APPROVED":   {Name: "APPROVED", IsTerminal: true, Parent: "ROOT"},
		},
		Transitions: map[string]types.TransitionDef{
			"start":   {Name: "start", Sources: []string{"CREATED"}, Target: "ROOT"},
			// "approve" defined on ROOT (grandparent of VALIDATING)
			"approve": {Name: "approve", Sources: []string{"ROOT"}, Target: "APPROVED"},
		},
	}
	e := newTestEngine(newMemInstanceStore(errInstanceNotFound))
	cm := compileDef(t, def)
	inst := simpleInstance("VALIDATING")
	inst.WorkflowType = "order"

	ct, target, err := e.validateTransition(context.Background(), cm, inst, "approve", nil)
	if err != nil {
		t.Fatalf("validateTransition grandparent bubbling failed: %v", err)
	}
	if ct.Def.Name != "approve" {
		t.Errorf("transition name = %q, want approve", ct.Def.Name)
	}
	if target != "APPROVED" {
		t.Errorf("target = %q, want APPROVED", target)
	}
}
