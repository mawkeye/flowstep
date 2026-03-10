package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
)

// ─── memChildStoreForJoin ─────────────────────────────────────────────────────

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
	for i := range total {
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

// ─── evaluateJoinPolicy tests ─────────────────────────────────────────────────

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
