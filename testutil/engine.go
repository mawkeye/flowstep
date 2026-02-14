package testutil

import (
	"testing"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/adapters/chanbus"
	"github.com/mawkeye/flowstate/adapters/memrunner"
	"github.com/mawkeye/flowstate/adapters/memstore"
)

// TestEngine bundles a flowstate.Engine with all in-memory adapters for testing.
type TestEngine struct {
	Engine        *flowstate.Engine
	EventStore    *memstore.EventStore
	InstanceStore *memstore.InstanceStore
	TaskStore     *memstore.TaskStore
	ChildStore    *memstore.ChildStore
	ActivityStore *memstore.ActivityStore
	EventBus      *chanbus.Bus
	ActivityRunner *memrunner.Runner
	Clock         *FakeClock
}

// NewTestEngine creates a fully wired engine with in-memory adapters and a FakeClock.
func NewTestEngine(t *testing.T) *TestEngine {
	t.Helper()

	clock := NewFakeClock()
	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	ts := memstore.NewTaskStore()
	cs := memstore.NewChildStore()
	as := memstore.NewActivityStore()
	bus := chanbus.New()
	runner := memrunner.New()

	engine, err := flowstate.NewEngine(
		flowstate.WithEventStore(es),
		flowstate.WithInstanceStore(is),
		flowstate.WithTaskStore(ts),
		flowstate.WithChildStore(cs),
		flowstate.WithActivityStore(as),
		flowstate.WithTxProvider(memstore.NewTxProvider()),
		flowstate.WithEventBus(bus),
		flowstate.WithActivityRunner(runner),
		flowstate.WithClock(clock),
	)
	if err != nil {
		t.Fatalf("testutil: failed to create engine: %v", err)
	}

	return &TestEngine{
		Engine:         engine,
		EventStore:     es,
		InstanceStore:  is,
		TaskStore:      ts,
		ChildStore:     cs,
		ActivityStore:  as,
		EventBus:       bus,
		ActivityRunner: runner,
		Clock:          clock,
	}
}
