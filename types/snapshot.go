package types

import "time"

// Snapshot is a point-in-time export of a workflow instance's complete runtime state.
// It captures all fields required to reconstruct a WorkflowInstance, plus a DefinitionHash
// for validation on restore. Snapshot is a value type — callers may copy and modify fields
// (including AggregateID, CorrelationID) before passing to Engine.Restore for cross-aggregate
// clone scenarios.
//
// Excluded from Snapshot (not needed for restore):
//   - UpdatedAt: set to restoration time by Engine.Restore
//   - LastReadUpdatedAt: internal optimistic-locking field (json:"-")
type Snapshot struct {
	// InstanceID is the original workflow instance ID (maps to WorkflowInstance.ID).
	InstanceID string `json:"instance_id"`

	WorkflowType    string `json:"workflow_type"`
	WorkflowVersion int    `json:"workflow_version"`
	AggregateType   string `json:"aggregate_type"`
	AggregateID     string `json:"aggregate_id"`
	CurrentState    string `json:"current_state"`

	StateData map[string]any `json:"state_data"`

	CorrelationID string `json:"correlation_id"`

	IsStuck     bool       `json:"is_stuck"`
	StuckReason string     `json:"stuck_reason,omitempty"`
	StuckAt     *time.Time `json:"stuck_at,omitempty"`
	RetryCount  int        `json:"retry_count"`

	// CreatedAt is the original instance creation time, preserved across restore.
	CreatedAt time.Time `json:"created_at"`

	// ShallowHistory records the last direct child visited for each compound state.
	ShallowHistory map[string]string `json:"shallow_history"`
	// DeepHistory records the last leaf descendant visited for each compound state.
	DeepHistory map[string]string `json:"deep_history"`
	// ActiveInParallel is non-nil only when the workflow was inside a parallel state.
	ActiveInParallel map[string]string `json:"active_in_parallel,omitempty"`
	// ParallelClock is the per-instance logical counter for parallel transitions.
	ParallelClock int64 `json:"parallel_clock,omitempty"`

	// DefinitionHash is the SHA-256 hash of the workflow definition at capture time.
	// Engine.Restore validates this against the registered compiled machine's hash.
	// A mismatch returns ErrSnapshotDefinitionMismatch.
	DefinitionHash string `json:"definition_hash"`

	// CapturedAt is when this snapshot was taken (from the engine's Clock).
	CapturedAt time.Time `json:"captured_at"`
}
