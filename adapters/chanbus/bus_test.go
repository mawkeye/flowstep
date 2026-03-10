package chanbus

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/mawkeye/flowstep/types"
)

func TestBusEmitToSubscribers(t *testing.T) {
	bus := New()
	var count1, count2 atomic.Int32

	bus.Subscribe(func(_ context.Context, _ types.DomainEvent) {
		count1.Add(1)
	})
	bus.Subscribe(func(_ context.Context, _ types.DomainEvent) {
		count2.Add(1)
	})

	ctx := context.Background()
	event := types.DomainEvent{ID: "evt-1", EventType: "OrderCreated"}

	if err := bus.Emit(ctx, event); err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	if count1.Load() != 1 {
		t.Errorf("expected handler 1 called once, got %d", count1.Load())
	}
	if count2.Load() != 1 {
		t.Errorf("expected handler 2 called once, got %d", count2.Load())
	}
}

func TestBusNoSubscribers(t *testing.T) {
	bus := New()
	ctx := context.Background()
	event := types.DomainEvent{ID: "evt-1"}

	if err := bus.Emit(ctx, event); err != nil {
		t.Fatalf("emit with no subscribers should not error: %v", err)
	}
}
