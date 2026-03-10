package flowstep

import "github.com/mawkeye/flowstep/types"

// EventStore persists and queries immutable domain events.
type EventStore = types.EventStore

// InstanceStore persists and queries workflow instances.
// Update MUST use optimistic locking (WHERE updated_at = $old).
// Returns ErrConcurrentModification if the row was changed since last read.
type InstanceStore = types.InstanceStore
