package runtime

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/mawkeye/flowstep/internal/graph"
	"github.com/mawkeye/flowstep/types"
)

// parallelCandidate is a matching transition found in a specific parallel region.
type parallelCandidate struct {
	region       string
	leafState    string
	ct           *graph.CompiledTransition
	effectiveSrc string
	targetState  string
}

// parallelTransition dispatches a named transition when the workflow is inside a parallel state.
// It detects two cases:
//  1. The transition exits the parallel state (sources include the parallel state name).
//  2. The transition is intra-parallel (sources match active leaves or ancestors within boundary).
func (e *Engine) parallelTransition(
	ctx context.Context,
	cm *graph.CompiledMachine,
	instance *types.WorkflowInstance,
	transitionName, actorID string,
	params map[string]any,
	start time.Time,
) (*types.TransitionResult, error) {
	tr, ok := cm.Definition.Transitions[transitionName]
	if !ok {
		return nil, fmt.Errorf("flowstep: transition %q not found in workflow %q: %w",
			transitionName, cm.Definition.WorkflowType, e.deps.ErrInvalidTransition)
	}

	parallelState := instance.CurrentState
	previousState := parallelState

	if st, exists := cm.Definition.States[parallelState]; exists && st.IsTerminal {
		return nil, fmt.Errorf("flowstep: workflow %s/%s is in terminal state %q: %w",
			instance.AggregateType, instance.AggregateID, parallelState, e.deps.ErrAlreadyTerminal)
	}

	// Case 1: transition exits the parallel state (sourced from the parallel state name itself).
	if slices.Contains(tr.Sources, parallelState) {
		ct, targetState, err := e.validateTransition(ctx, cm, instance, transitionName, params)
		if err != nil {
			return nil, err
		}
		event, err := e.commitParallelExit(ctx, cm, instance, ct, targetState, transitionName, actorID, params)
		if err != nil {
			return nil, err
		}
		return e.runPostCommit(ctx, cm, instance, ct, event, previousState, params, start)
	}

	// Case 2: intra-parallel — check active leaves and their ancestors.
	candidates, err := e.collectParallelCandidates(ctx, cm, instance, parallelState, transitionName, params)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("flowstep: transition %q not valid from parallel state %q (no active leaf matches): %w",
			transitionName, parallelState, e.deps.ErrInvalidTransition)
	}

	event, err := e.commitParallelIntra(ctx, cm, instance, candidates, parallelState, transitionName, actorID, params)
	if err != nil {
		return nil, err
	}
	return e.runPostCommit(ctx, cm, instance, candidates[0].ct, event, previousState, params, start)
}

// collectParallelCandidates finds transitions matching transitionName across all active
// leaf states and their ancestors (bounded by the parallel state — does not bubble past it).
func (e *Engine) collectParallelCandidates(
	ctx context.Context,
	cm *graph.CompiledMachine,
	instance *types.WorkflowInstance,
	parallelState, transitionName string,
	params map[string]any,
) ([]parallelCandidate, error) {
	tr, ok := cm.Definition.Transitions[transitionName]
	if !ok {
		return nil, fmt.Errorf("flowstep: transition %q not found: %w", transitionName, e.deps.ErrInvalidTransition)
	}

	// Sort regions for deterministic processing order.
	regions := make([]string, 0, len(instance.ActiveInParallel))
	for region := range instance.ActiveInParallel {
		regions = append(regions, region)
	}
	sort.Strings(regions)

	var candidates []parallelCandidate

	for _, region := range regions {
		leafState := instance.ActiveInParallel[region]
		effectiveSrc := ""

		if slices.Contains(tr.Sources, leafState) {
			effectiveSrc = leafState
		} else {
			for _, ancestor := range cm.Ancestry[leafState] {
				if ancestor == parallelState {
					break // do not bubble past the parallel state boundary
				}
				if slices.Contains(tr.Sources, ancestor) {
					effectiveSrc = ancestor
					break
				}
			}
		}

		if effectiveSrc == "" {
			continue
		}

		var ct *graph.CompiledTransition
		for _, c := range cm.TransitionsByState[effectiveSrc] {
			if c.Def.Name == transitionName {
				ct = c
				break
			}
		}
		if ct == nil {
			continue
		}

		if err := e.runGuards(ctx, cm.Definition.WorkflowType, ct, instance, params); err != nil {
			return nil, err
		}

		targetState := tr.Target
		if len(tr.Routes) > 0 {
			resolved, err := e.resolveRoute(ctx, tr, instance, params)
			if err != nil {
				return nil, err
			}
			targetState = resolved
		}

		candidates = append(candidates, parallelCandidate{
			region:       region,
			leafState:    leafState,
			ct:           ct,
			effectiveSrc: effectiveSrc,
			targetState:  targetState,
		})
	}

	return candidates, nil
}

// commitParallelIntra commits an intra-parallel transition atomically, updating only the
// affected regions' active leaves and producing one DomainEvent with EventType="ParallelTransition".
func (e *Engine) commitParallelIntra(
	ctx context.Context,
	cm *graph.CompiledMachine,
	instance *types.WorkflowInstance,
	candidates []parallelCandidate,
	parallelState, transitionName, actorID string,
	params map[string]any,
) (types.DomainEvent, error) {
	now := e.deps.Clock.Now()

	actInput := types.ActivityInput{
		WorkflowType:  cm.Definition.WorkflowType,
		AggregateType: instance.AggregateType,
		AggregateID:   instance.AggregateID,
		CorrelationID: instance.CorrelationID,
		Transition:    transitionName,
		SourceState:   parallelState,
		Params:        params,
	}

	type regionUpdate struct {
		candidate parallelCandidate
		exitSeq   []string
		entrySeq  []string
	}
	updates := make([]regionUpdate, 0, len(candidates))
	for _, c := range candidates {
		lca, _ := cm.LCA(c.leafState, c.targetState)
		exitSeq := computeExitSequence(c.leafState, lca, cm.Ancestry)
		entrySeq := computeEntrySequence(lca, c.targetState, cm.Ancestry)
		updates = append(updates, regionUpdate{c, exitSeq, entrySeq})
	}

	// Run exit activities for all affected regions.
	for _, u := range updates {
		for _, stateName := range u.exitSeq {
			st := cm.Definition.States[stateName]
			if st.ExitActivity == "" {
				continue
			}
			if err := e.runSyncActivity(ctx, st.ExitActivity, actInput); err != nil {
				return types.DomainEvent{}, fmt.Errorf("flowstep: exit activity %q: %w", st.ExitActivity, err)
			}
		}
	}

	// Run entry activities for all affected regions.
	for _, u := range updates {
		for _, stateName := range u.entrySeq {
			st := cm.Definition.States[stateName]
			if st.EntryActivity == "" {
				continue
			}
			if err := e.runSyncActivity(ctx, st.EntryActivity, actInput); err != nil {
				return types.DomainEvent{}, fmt.Errorf("flowstep: entry activity %q: %w", st.EntryActivity, err)
			}
		}
	}

	// Build new ActiveInParallel (copy-on-write).
	newAIP := make(map[string]string, len(instance.ActiveInParallel))
	maps.Copy(newAIP, instance.ActiveInParallel)
	for _, u := range updates {
		newAIP[u.candidate.region] = u.candidate.targetState
	}
	newClock := instance.ParallelClock + 1

	// Build delta payload: only changed regions in _parallel_regions.
	parallelRegions := make(map[string]any, len(candidates))
	for _, u := range updates {
		parallelRegions[u.candidate.region] = map[string]any{
			"from": u.candidate.leafState,
			"to":   u.candidate.targetState,
		}
	}
	// _region_sequence_map: all regions → their last ParallelClock value.
	regionSeqMap := make(map[string]any, len(newAIP))
	for region := range instance.ActiveInParallel {
		if _, changed := parallelRegions[region]; changed {
			regionSeqMap[region] = newClock
		} else {
			regionSeqMap[region] = instance.ParallelClock
		}
	}

	payload := make(map[string]any, len(params)+2)
	maps.Copy(payload, params)
	payload["_parallel_regions"] = parallelRegions
	payload["_region_sequence_map"] = regionSeqMap

	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   instance.AggregateType,
		AggregateID:     instance.AggregateID,
		WorkflowType:    cm.Definition.WorkflowType,
		WorkflowVersion: cm.Definition.Version,
		EventType:       "ParallelTransition",
		CorrelationID:   instance.CorrelationID,
		ActorID:         actorID,
		TransitionName:  transitionName,
		StateBefore:     copyMap(instance.StateData),
		StateAfter:      copyMap(instance.StateData),
		Payload:         payload,
		CreatedAt:       now,
	}

	instance.LastReadUpdatedAt = instance.UpdatedAt
	instance.ActiveInParallel = newAIP
	instance.ParallelClock = newClock
	instance.UpdatedAt = now

	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstep: begin tx: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstep: append event: %w", err)
	}
	if err := e.deps.InstanceStore.Update(ctx, tx, *instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstep: update instance: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstep: commit tx: %w", err)
	}

	return event, nil
}

// commitParallelExit handles a transition that exits the parallel state entirely.
// It runs exit activities for all active leaves, regions, and the parallel state itself,
// then opens a transaction to commit the event and updated instance atomically.
func (e *Engine) commitParallelExit(
	ctx context.Context,
	cm *graph.CompiledMachine,
	instance *types.WorkflowInstance,
	ct *graph.CompiledTransition,
	targetState, transitionName, actorID string,
	params map[string]any,
) (types.DomainEvent, error) {
	now := e.deps.Clock.Now()
	parallelState := instance.CurrentState

	// Full exit sequence: all active leaves → regions → parallel state.
	exitSeq := computeParallelExitSequence(instance, parallelState)

	// Record history per region: each region compound state gets its own active leaf
	// recorded as its deep-history target (not the parallel state name). Use the
	// per-region exit sub-sequence (leaf → region, stopping before parallelState) so
	// that recordHistory only processes the compound states within that region.
	regionNames := make([]string, 0, len(instance.ActiveInParallel))
	for r := range instance.ActiveInParallel {
		regionNames = append(regionNames, r)
	}
	sort.Strings(regionNames)
	for _, region := range regionNames {
		leaf := instance.ActiveInParallel[region]
		regionExitSeq := computeExitSequence(leaf, parallelState, cm.Ancestry)
		recordHistory(instance, leaf, regionExitSeq, cm.Definition)
	}

	actInput := types.ActivityInput{
		WorkflowType:  cm.Definition.WorkflowType,
		AggregateType: instance.AggregateType,
		AggregateID:   instance.AggregateID,
		CorrelationID: instance.CorrelationID,
		Transition:    transitionName,
		SourceState:   parallelState,
		Params:        params,
	}

	// Resolve target (compound states fall through to leaf via InitialLeafMap).
	resolvedTarget := targetState
	if leaf, ok := cm.InitialLeafMap[targetState]; ok {
		resolvedTarget = leaf
	}
	entrySeq := computeEntrySequence("", resolvedTarget, cm.Ancestry)

	// Run all exit and entry activities before opening the transaction so that a
	// failed activity does not leave an open (uncommitted) transaction behind.
	for _, stateName := range exitSeq {
		st := cm.Definition.States[stateName]
		if st.ExitActivity == "" {
			continue
		}
		if err := e.runSyncActivity(ctx, st.ExitActivity, actInput); err != nil {
			return types.DomainEvent{}, fmt.Errorf("flowstep: exit activity %q: %w", st.ExitActivity, err)
		}
	}

	for _, stateName := range entrySeq {
		st := cm.Definition.States[stateName]
		if st.EntryActivity == "" {
			continue
		}
		if err := e.runSyncActivity(ctx, st.EntryActivity, actInput); err != nil {
			return types.DomainEvent{}, fmt.Errorf("flowstep: entry activity %q: %w", st.EntryActivity, err)
		}
	}

	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   instance.AggregateType,
		AggregateID:     instance.AggregateID,
		WorkflowType:    cm.Definition.WorkflowType,
		WorkflowVersion: cm.Definition.Version,
		EventType:       ct.Def.Event,
		CorrelationID:   instance.CorrelationID,
		ActorID:         actorID,
		TransitionName:  transitionName,
		StateBefore:     copyMap(instance.StateData),
		StateAfter:      copyMap(instance.StateData),
		Payload:         params,
		CreatedAt:       now,
	}

	instance.LastReadUpdatedAt = instance.UpdatedAt
	instance.CurrentState = resolvedTarget
	instance.ActiveInParallel = nil
	instance.UpdatedAt = now

	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstep: begin tx: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstep: append event: %w", err)
	}
	if err := e.deps.InstanceStore.Update(ctx, tx, *instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstep: update instance: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstep: commit tx: %w", err)
	}

	return event, nil
}
