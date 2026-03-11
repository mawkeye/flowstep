package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/mawkeye/flowstep/internal/graph"
	"github.com/mawkeye/flowstep/types"
)

func (e *Engine) resolveRoute(ctx context.Context, tr types.TransitionDef, aggregate any, params map[string]any) (string, error) {
	var defaultTarget string
	for _, route := range tr.Routes {
		if route.IsDefault {
			defaultTarget = route.Target
			continue
		}
		if route.Condition != nil {
			matched, err := route.Condition.Evaluate(ctx, aggregate, params)
			if err != nil {
				return "", fmt.Errorf("flowstep: condition evaluation failed: %w", err)
			}
			if matched {
				return route.Target, nil
			}
		}
	}
	if defaultTarget != "" {
		return defaultTarget, nil
	}
	return "", fmt.Errorf("flowstep: no condition matched and no default route: %w", e.deps.ErrNoMatchingRoute)
}

func (e *Engine) runGuards(ctx context.Context, workflowType string, ct *graph.CompiledTransition, aggregate any, params map[string]any) error {
	for i, guard := range ct.Def.Guards {
		if err := guard.Check(ctx, aggregate, params); err != nil {
			guardName := ct.GuardNames[i]
			e.deps.Observers.NotifyGuardFailed(ctx, types.GuardFailureEvent{WorkflowType: workflowType, TransitionName: ct.Def.Name, GuardName: guardName, Err: err})
			return &types.GuardError{GuardName: guardName, Reason: err}
		}
	}
	return nil
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		// Recursively deep-copy nested maps so event state snapshots are independent.
		if nested, ok := v.(map[string]any); ok {
			cp[k] = copyMap(nested)
		} else {
			cp[k] = v
		}
	}
	return cp
}
