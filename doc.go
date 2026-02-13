// Package flowstate provides a declarative workflow engine for Go.
//
// Workflows are defined as state machines using a fluent builder API.
// The engine handles state transitions, guard evaluation, activity dispatch,
// and immutable event chain persistence.
//
// Core package has zero external dependencies (stdlib only).
// Persistence, event bus, and activity runner are pluggable via interfaces.
// Reference implementations are provided in sub-packages.
//
// Key concepts:
//   - Definition: immutable workflow structure (states, transitions)
//   - Guard: deterministic precondition check (no I/O)
//   - Activity: async non-deterministic work (API calls, I/O allowed)
//   - DomainEvent: immutable record of a state transition
//   - WorkflowInstance: live state tracker for a workflow
//
// Example:
//
//	def, _ := flowstate.Define("order", "standard_order").
//	    Version(1).
//	    States(
//	        flowstate.Initial("CREATED"),
//	        flowstate.State("PAID"),
//	        flowstate.Terminal("SHIPPED"),
//	    ).
//	    Transition("pay",
//	        flowstate.From("CREATED"),
//	        flowstate.To("PAID"),
//	        flowstate.Event("OrderPaid"),
//	    ).
//	    Transition("ship",
//	        flowstate.From("PAID"),
//	        flowstate.To("SHIPPED"),
//	        flowstate.Event("OrderShipped"),
//	    ).
//	    Build()
package flowstate
