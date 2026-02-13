package chanbus

import (
	"context"
	"sync"

	"github.com/mawkeye/flowstate/types"
)

// Handler is a function that receives domain events.
type Handler func(ctx context.Context, event types.DomainEvent)

// Bus is an in-memory EventBus using Go channels for fan-out.
type Bus struct {
	mu       sync.RWMutex
	handlers []Handler
}

// New creates a new in-memory EventBus.
func New() *Bus {
	return &Bus{}
}

// Subscribe registers a handler to receive all emitted events.
func (b *Bus) Subscribe(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, h)
}

// Emit sends the event to all registered handlers.
func (b *Bus) Emit(ctx context.Context, event types.DomainEvent) error {
	b.mu.RLock()
	handlers := make([]Handler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, h := range handlers {
		h(ctx, event)
	}
	return nil
}
