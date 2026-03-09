package graph

import (
	"fmt"
	"slices"

	"github.com/mawkeye/flowstate/types"
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
	if err := checkDeadEnds(def, sentinels); err != nil {
		return err
	}
	if err := checkReachability(def, sentinels); err != nil {
		return err
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
			Detail:   "flowstate: no initial state defined",
		}
	}
	if initialCount > 1 {
		return &ValidationError{
			Sentinel: s.ErrMultipleInitialStates,
			Detail:   "flowstate: multiple initial states defined",
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
		Detail:   "flowstate: no terminal states defined",
	}
}

func checkUnknownStates(def *types.Definition, s Sentinels) error {
	for name, tr := range def.Transitions {
		for _, src := range tr.Sources {
			if _, ok := def.States[src]; !ok {
				return &ValidationError{
					Sentinel: s.ErrUnknownState,
					Detail:   fmt.Sprintf("flowstate: transition %q references unknown source state %q", name, src),
				}
			}
		}
		if tr.Target != "" {
			if _, ok := def.States[tr.Target]; !ok {
				return &ValidationError{
					Sentinel: s.ErrUnknownState,
					Detail:   fmt.Sprintf("flowstate: transition %q references unknown target state %q", name, tr.Target),
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
					Detail:   fmt.Sprintf("flowstate: transition %q route references unknown target state %q", name, route.Target),
				}
			}
		}
	}
	return nil
}

// checkReachability does BFS from initial state to find unreachable states.
func checkReachability(def *types.Definition, s Sentinels) error {
	if def.InitialState == "" {
		return nil // already caught by checkInitialState
	}

	reachable := make(map[string]bool)
	queue := []string{def.InitialState}
	reachable[def.InitialState] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, tr := range def.Transitions {
			if !slices.Contains(tr.Sources, current) {
				continue
			}
			// Direct target
			if tr.Target != "" && !reachable[tr.Target] {
				reachable[tr.Target] = true
				queue = append(queue, tr.Target)
			}
			// Routed targets
			for _, route := range tr.Routes {
				if !reachable[route.Target] {
					reachable[route.Target] = true
					queue = append(queue, route.Target)
				}
			}
		}
	}

	for name := range def.States {
		if !reachable[name] {
			return &ValidationError{
				Sentinel: s.ErrUnreachableState,
				Detail:   fmt.Sprintf("flowstate: state %q is unreachable from initial state", name),
			}
		}
	}
	return nil
}

// checkDeadEnds finds non-terminal states with no outgoing transitions.
func checkDeadEnds(def *types.Definition, s Sentinels) error {
	for name, st := range def.States {
		if st.IsTerminal {
			continue
		}
		hasOutgoing := false
		for _, tr := range def.Transitions {
			if slices.Contains(tr.Sources, name) {
				hasOutgoing = true
				break
			}
		}
		if !hasOutgoing {
			return &ValidationError{
				Sentinel: s.ErrDeadEndState,
				Detail:   fmt.Sprintf("flowstate: non-terminal state %q has no outgoing transitions", name),
			}
		}
	}
	return nil
}
