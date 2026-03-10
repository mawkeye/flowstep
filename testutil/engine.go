package testutil

import (
	"testing"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/chanbus"
	"github.com/mawkeye/flowstep/adapters/memrunner"
	"github.com/mawkeye/flowstep/adapters/memstore"
)

// TestEngine bundles a flowstep.Engine with all in-memory adapters for testing.
type TestEngine struct {
	Engine        *flowstep.Engine
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

	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTaskStore(ts),
		flowstep.WithChildStore(cs),
		flowstep.WithActivityStore(as),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(bus),
		flowstep.WithActivityRunner(runner),
		flowstep.WithClock(clock),
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
