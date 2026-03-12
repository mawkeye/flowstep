package graph

import (
	"fmt"
	"slices"

	"github.com/mawkeye/flowstep/types"
)

// ValidationError wraps a sentinel error with additional context.
type ValidationError struct {
	Sentinel error
	Detail   string
}

func (e *ValidationError) Error() string {
	return e.Detail
}

func (e *ValidationError) Is(target error) bool {
	return target == e.Sentinel
}

func (e *ValidationError) Unwrap() error {
	return e.Sentinel
}

// Validate performs structural and reachability checks on a Definition.
// It must be called with the sentinel errors from the root package.
func Validate(def *types.Definition, sentinels Sentinels) error {
	if err := checkInitialState(def, sentinels); err != nil {
		return err
	}
	if err := checkTerminalStates(def, sentinels); err != nil {
		return err
	}
	if err := checkUnknownStates(def, sentinels); err != nil {
		return err
	}
	if err := checkOrphanedChildren(def, sentinels); err != nil {
		return err
	}
	if err := checkCompoundStates(def, sentinels); err != nil {
		return err
	}
	if err := checkParallelStates(def, sentinels); err != nil {
		return err
	}
	if err := checkCompoundNotTerminal(def, sentinels); err != nil {
		return err
	}
	if err := checkInitialNotCompound(def, sentinels); err != nil {
		return err
	}
	if err := checkWaitLeafOnly(def, sentinels); err != nil {
		return err
	}
	if err := checkDeadEnds(def, sentinels); err != nil {
		return err
	}
	if err := checkReachability(def, sentinels); err != nil {
		return err
	}
	return nil
}

// checkOrphanedChildren verifies that every state with a Parent field references an existing state.
func checkOrphanedChildren(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if st.Parent == "" {
			continue
		}
		if _, ok := def.States[st.Parent]; !ok {
			return &ValidationError{
				Sentinel: s.ErrOrphanedChild,
				Detail:   fmt.Sprintf("flowstep: state %q has Parent %q which does not exist", name, st.Parent),
			}
		}
	}
	return nil
}

// checkParallelStates verifies that parallel states have valid structure:
// at least one region child, all children are compound states (regions),
// and no child is itself a parallel state (no nested parallels).
func checkParallelStates(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if !st.IsParallel {
			continue
		}
		// A parallel state must not itself be a child of another parallel state.
		// This check uses the Parent field so it fires regardless of map iteration order.
		if st.Parent != "" {
			if parent, ok := def.States[st.Parent]; ok && parent.IsParallel {
				return &ValidationError{
					Sentinel: s.ErrNestedParallelState,
					Detail:   fmt.Sprintf("flowstep: parallel state %q is nested inside parallel state %q (not supported)", name, st.Parent),
				}
			}
		}
		if len(st.Children) == 0 {
			return &ValidationError{
				Sentinel: s.ErrParallelStateNoRegions,
				Detail:   fmt.Sprintf("flowstep: parallel state %q has no regions (children)", name),
			}
		}
		for _, childName := range st.Children {
			child, ok := def.States[childName]
			if !ok {
				continue // orphan check catches this
			}
			if child.IsParallel {
				return &ValidationError{
					Sentinel: s.ErrNestedParallelState,
					Detail:   fmt.Sprintf("flowstep: parallel state %q has nested parallel child %q (not supported)", name, childName),
				}
			}
			if !child.IsCompound {
				return &ValidationError{
					Sentinel: s.ErrParallelRegionNotCompound,
					Detail:   fmt.Sprintf("flowstep: parallel state %q child %q is not a compound state (region)", name, childName),
				}
			}
		}
	}
	return nil
}

// checkCompoundStates verifies that compound states have a non-empty InitialChild
// and that InitialChild is one of their children.
// Parallel states are exempt — they activate all children and have no InitialChild.
func checkCompoundStates(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if !st.IsCompound {
			continue
		}
		// Parallel states are compound but do not use InitialChild.
		if st.IsParallel {
			continue
		}
		if st.InitialChild == "" {
			return &ValidationError{
				Sentinel: s.ErrCompoundStateNoInitialChild,
				Detail:   fmt.Sprintf("flowstep: compound state %q has no InitialChild", name),
			}
		}
		if !slices.Contains(st.Children, st.InitialChild) {
			return &ValidationError{
				Sentinel: s.ErrCompoundStateNoInitialChild,
				Detail:   fmt.Sprintf("flowstep: compound state %q InitialChild %q is not in Children", name, st.InitialChild),
			}
		}
	}
	return nil
}

// checkCompoundNotTerminal verifies that compound states are not marked terminal.
func checkCompoundNotTerminal(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if st.IsCompound && st.IsTerminal {
			return &ValidationError{
				Sentinel: s.ErrDeadEndState,
				Detail:   fmt.Sprintf("flowstep: compound state %q cannot be terminal", name),
			}
		}
	}
	return nil
}

// checkInitialNotCompound verifies that the initial state is not a compound state.
// Compound states cannot be the direct resting place of an instance — only leaf states can.
// If an InitialState is compound, it must be resolved to its initial leaf via InitialLeafMap
// at instance creation time, so having IsInitial+IsCompound is a configuration error.
func checkInitialNotCompound(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if st.IsInitial && st.IsCompound {
			return &ValidationError{
				Sentinel: s.ErrDeadEndState,
				Detail:   fmt.Sprintf("flowstep: initial state %q cannot be compound (initial state must be a leaf)", name),
			}
		}
	}
	return nil
}

// checkWaitLeafOnly verifies that IsWait is only set on leaf states (not compound states).
func checkWaitLeafOnly(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if st.IsWait && st.IsCompound {
			return &ValidationError{
				Sentinel: s.ErrDeadEndState,
				Detail:   fmt.Sprintf("flowstep: compound state %q cannot be a wait state (IsWait must be set on leaf states only)", name),
			}
		}
	}
	return nil
}

// Sentinels holds references to the root package's sentinel errors.
// This avoids the internal package importing the root package.
type Sentinels struct {
	ErrNoInitialState        error
	ErrMultipleInitialStates error
	ErrNoTerminalStates      error
	ErrUnreachableState      error
	ErrDeadEndState          error
	ErrUnknownState          error
	ErrMissingDefault        error
	ErrDuplicateTransition   error
	ErrSpawnCycle            error

	// Hierarchy sentinels
	ErrCompoundStateNoInitialChild error
	ErrOrphanedChild               error
	ErrCircularHierarchy           error

	// Parallel sentinels
	ErrParallelStateNoRegions    error
	ErrParallelRegionNotCompound error
	ErrNestedParallelState       error
}

func checkInitialState(def *types.Definition, s Sentinels) error {
	var initialCount int
	for _, st := range def.States {
		if st.IsInitial {
			initialCount++
		}
	}
	if initialCount == 0 {
		return &ValidationError{
			Sentinel: s.ErrNoInitialState,
			Detail:   "flowstep: no initial state defined",
		}
	}
	if initialCount > 1 {
		return &ValidationError{
			Sentinel: s.ErrMultipleInitialStates,
			Detail:   "flowstep: multiple initial states defined",
		}
	}
	return nil
}

func checkTerminalStates(def *types.Definition, s Sentinels) error {
	for _, st := range def.States {
		if st.IsTerminal {
			return nil
		}
	}
	return &ValidationError{
		Sentinel: s.ErrNoTerminalStates,
		Detail:   "flowstep: no terminal states defined",
	}
}

func checkUnknownStates(def *types.Definition, s Sentinels) error {
	for name, tr := range def.Transitions {
		for _, src := range tr.Sources {
			if _, ok := def.States[src]; !ok {
				return &ValidationError{
					Sentinel: s.ErrUnknownState,
					Detail:   fmt.Sprintf("flowstep: transition %q references unknown source state %q", name, src),
				}
			}
		}
		if tr.Target != "" {
			if _, ok := def.States[tr.Target]; !ok {
				return &ValidationError{
					Sentinel: s.ErrUnknownState,
					Detail:   fmt.Sprintf("flowstep: transition %q references unknown target state %q", name, tr.Target),
				}
			}
		}
		for _, route := range tr.Routes {
			if route.Target == "" {
				continue
			}
			if _, ok := def.States[route.Target]; !ok {
				return &ValidationError{
					Sentinel: s.ErrUnknownState,
					Detail:   fmt.Sprintf("flowstep: transition %q route references unknown target state %q", name, route.Target),
				}
			}
		}
	}
	return nil
}

// checkReachability does BFS from initial state to find unreachable states.
// For compound states, entering the state also reaches its InitialChild (recursively).
func checkReachability(def *types.Definition, s Sentinels) error {
	if def.InitialState == "" {
		return nil // already caught by checkInitialState
	}

	reachable := make(map[string]bool)
	queue := []string{}

	// enqueue marks a state as reachable and enqueues it (and its InitialChild chain)
	// for BFS traversal of outgoing transitions.
	var enqueue func(name string)
	enqueue = func(name string) {
		if reachable[name] {
			return
		}
		reachable[name] = true
		queue = append(queue, name)
		if st, ok := def.States[name]; ok && st.IsCompound {
			if st.IsParallel {
				// Parallel states activate ALL children (regions), not just InitialChild.
				for _, child := range st.Children {
					enqueue(child)
				}
			} else if st.InitialChild != "" {
				// Regular compound state: entering reaches its InitialChild.
				enqueue(st.InitialChild)
			}
		}
	}

	enqueue(def.InitialState)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, tr := range def.Transitions {
			if !slices.Contains(tr.Sources, current) {
				continue
			}
			// Direct target
			if tr.Target != "" {
				enqueue(tr.Target)
			}
			// Routed targets
			for _, route := range tr.Routes {
				enqueue(route.Target)
			}
		}
	}

	for name := range def.States {
		if !reachable[name] {
			return &ValidationError{
				Sentinel: s.ErrUnreachableState,
				Detail:   fmt.Sprintf("flowstep: state %q is unreachable from initial state", name),
			}
		}
	}
	return nil
}

// checkDeadEnds finds non-terminal states with no outgoing transitions.
// Compound states are excluded — their children provide the outgoing transitions.
// Leaf states with a parent that has outgoing transitions are also excluded —
// event bubbling allows child states to take parent-defined transitions.
func checkDeadEnds(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if st.IsTerminal {
			continue
		}
		// Compound states delegate transitions to children — not a dead end.
		if st.IsCompound {
			continue
		}
		if hasOutgoingOrAncestorHas(name, st.Parent, def) {
			continue
		}
		return &ValidationError{
			Sentinel: s.ErrDeadEndState,
			Detail:   fmt.Sprintf("flowstep: non-terminal state %q has no outgoing transitions", name),
		}
	}
	return nil
}

// hasOutgoingOrAncestorHas returns true if the state or any ancestor has an outgoing transition.
func hasOutgoingOrAncestorHas(name, parent string, def *types.Definition) bool {
	for _, tr := range def.Transitions {
		if slices.Contains(tr.Sources, name) {
			return true
		}
	}
	// Walk up the parent chain checking for ancestor transitions (event bubbling).
	current := parent
	for current != "" {
		for _, tr := range def.Transitions {
			if slices.Contains(tr.Sources, current) {
				return true
			}
		}
		if ancestor, ok := def.States[current]; ok {
			current = ancestor.Parent
		} else {
			break
		}
	}
	return false
}
