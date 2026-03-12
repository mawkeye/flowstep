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

// computeExitSequence returns states to exit when leaving source, ordered leaf→LCA (exclusive).
// If lca is "" (no common ancestor), exits only the source state.
func computeExitSequence(source, lca string, ancestry map[string][]string) []string {
	seq := []string{source}
	for _, ancestor := range ancestry[source] {
		if ancestor == lca {
			break
		}
		seq = append(seq, ancestor)
	}
	return seq
}

// computeEntrySequence returns states to enter when reaching target, ordered LCA→target (exclusive of LCA).
// If lca is "" (no common ancestor), enters all states from root of target's hierarchy to target.
func computeEntrySequence(lca, target string, ancestry map[string][]string) []string {
	chain := ancestry[target] // [parent, grandparent, ...] nearest first
	// Find position of lca in chain; if not found, use full chain (flat-to-hierarchy entry).
	lcaIdx := len(chain)
	for i, a := range chain {
		if a == lca {
			lcaIdx = i
			break
		}
	}
	fromLCA := chain[:lcaIdx] // [parent_of_target, ..., child_of_lca] nearest first
	result := make([]string, len(fromLCA)+1)
	for i, s := range fromLCA {
		result[len(fromLCA)-1-i] = s // reverse to LCA→target order
	}
	result[len(fromLCA)] = target
	return result
}

// runSyncActivity resolves an activity by name via ActivityResolver and executes it synchronously.
// Returns nil if ActivityRunner is nil or does not implement ActivityResolver (graceful degradation).
func (e *Engine) runSyncActivity(ctx context.Context, activityName string, input types.ActivityInput) error {
	if e.deps.ActivityRunner == nil {
		return nil
	}
	resolver, ok := e.deps.ActivityRunner.(types.ActivityResolver)
	if !ok {
		return nil // ActivityRunner does not support sync resolution
	}
	act, ok := resolver.Resolve(activityName)
	if !ok {
		return nil // activity not registered by name
	}
	_, err := act.Execute(ctx, input)
	return err
}

// collectSubtreeStates returns the union of every state in the subtree rooted at each
// state in roots — including the root states themselves and all their descendants.
// For leaf states this is just {leaf}; for compound states it includes all children recursively.
func collectSubtreeStates(roots []string, def *types.Definition) []string {
	seen := make(map[string]bool)
	var visit func(name string)
	visit = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		st, ok := def.States[name]
		if !ok {
			return
		}
		for _, child := range st.Children {
			visit(child)
		}
	}
	for _, r := range roots {
		visit(r)
	}
	result := make([]string, 0, len(seen))
	for s := range seen {
		result = append(result, s)
	}
	return result
}

// recordHistory records the last-active child (ShallowHistory) and last leaf (DeepHistory)
// for every compound state in exitSeq. Uses copy-on-write to avoid aliasing the stored
// instance map when memstore returns a struct copy whose map header shares underlying data.
func recordHistory(instance *types.WorkflowInstance, sourceLeaf string, exitSeq []string, def *types.Definition) {
	for _, stateName := range exitSeq {
		st := def.States[stateName]
		if !st.IsCompound {
			continue
		}
		// Find the direct child of stateName that is also in the exit path.
		directChild := ""
		for _, s := range exitSeq {
			if def.States[s].Parent == stateName {
				directChild = s
				break
			}
		}
		if directChild == "" {
			continue
		}

		// ShallowHistory: copy-on-write — build a new map.
		newShallow := make(map[string]string, len(instance.ShallowHistory)+1)
		for k, v := range instance.ShallowHistory {
			newShallow[k] = v
		}
		newShallow[stateName] = directChild
		instance.ShallowHistory = newShallow

		// DeepHistory: copy-on-write — build a new map.
		newDeep := make(map[string]string, len(instance.DeepHistory)+1)
		for k, v := range instance.DeepHistory {
			newDeep[k] = v
		}
		newDeep[stateName] = sourceLeaf
		instance.DeepHistory = newDeep
	}
}

// enterParallelRegions populates instance.ActiveInParallel with each region's initial leaf.
// Uses copy-on-write to avoid aliasing stored maps.
func enterParallelRegions(instance *types.WorkflowInstance, cm *graph.CompiledMachine, parallelState string) {
	regions := cm.RegionIndex[parallelState]
	newAIP := make(map[string]string, len(regions))
	for _, region := range regions {
		leaf := region
		if initialLeaf, ok := cm.InitialLeafMap[region]; ok {
			leaf = initialLeaf
		}
		newAIP[region] = leaf
	}
	instance.ActiveInParallel = newAIP
}

// computeParallelEntrySequence returns the full entry sequence for entering a parallel state:
// [parallelState, region1, region1_initialLeaf, region2, region2_initialLeaf, ...]
// The LCA and ancestor path to parallelState is computed by the caller and prepended if needed.
func computeParallelEntrySequence(cm *graph.CompiledMachine, lca, parallelState string) []string {
	// Entry path to reach the parallel state itself (from LCA).
	parentSeq := computeEntrySequence(lca, parallelState, cm.Ancestry)

	// For each region: region → its initial leaf.
	regions := cm.RegionIndex[parallelState]
	extra := make([]string, 0, len(regions)*2)
	for _, region := range regions {
		extra = append(extra, region)
		if leaf, ok := cm.InitialLeafMap[region]; ok {
			extra = append(extra, leaf)
		}
	}
	return append(parentSeq, extra...)
}

// computeParallelExitSequence returns the exit sequence when leaving a parallel state.
// For each active leaf in ActiveInParallel: exit leaf → region → (stop before parallelState).
// Then exits the parallelState itself.
func computeParallelExitSequence(instance *types.WorkflowInstance, parallelState string) []string {
	var seq []string
	// Exit each region's active leaf and the region itself.
	for region, leaf := range instance.ActiveInParallel {
		seq = append(seq, leaf)
		if leaf != region {
			seq = append(seq, region)
		}
	}
	// Exit the parallel state itself.
	seq = append(seq, parallelState)
	return seq
}

// activeStates returns all states that should be checked when matching trigger transitions.
// In a normal state this is just [instance.CurrentState].
// In a parallel state it is [instance.CurrentState] plus all active leaf states.
func activeStates(instance *types.WorkflowInstance) []string {
	states := []string{instance.CurrentState}
	for _, leaf := range instance.ActiveInParallel {
		states = append(states, leaf)
	}
	return states
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
