# Step 09: Parallel States — Verification Fixes

Date: 2026-03-12
Plan: docs/plans/2026-03-12-parallel-states.md

## What Was Fixed

Post-implementation code review (spec-reviewer) identified 2 must_fix and 2 should_fix issues in the parallel states implementation. All four were addressed.

## Files Modified

- `internal/runtime/helpers.go` — sort/maps imports, `maps.Copy` in `recordHistory`
- `internal/runtime/parallel.go` — history recording and tx ordering in `commitParallelExit`
- `internal/runtime/parallel_engine_test.go` — new conflict resolution tests

## Issues Fixed

### must_fix 1: Non-deterministic exit activity ordering (computeParallelExitSequence)

`computeParallelExitSequence` iterated `instance.ActiveInParallel` (a Go map) directly. Go map iteration order is non-deterministic, so exit activities fired in different region orders on every call.

**Fix:** Collect region keys into a `[]string`, call `sort.Strings()`, then range the sorted slice. Added `"sort"` import to `helpers.go`. Also replaced manual `for k, v := range m { n[k] = v }` loops in `recordHistory` with `maps.Copy` (adds `"maps"` import).

### must_fix 2: No conflict resolution test coverage

The plan's Testing Strategy required table-driven tests for conflict resolution scenarios. The ancestor-bubbling and structural-exit-precedence code paths in `collectParallelCandidates` and `parallelTransition` were untested.

**Fix:** Added `parallelWorkflowForConflict()` helper and `TestParallelConflict` table-driven test in `parallel_engine_test.go` with two subtests:
- `ancestor_sourced_matches_active_leaf_via_bubbling` — dispatches a transition with `sources: ["bold_region"]` (compound ancestor, not leaf), verifying `collectParallelCandidates` walks ancestors correctly.
- `structural_exit_precedence_over_intra_region_candidate` — dispatches a transition with `sources: ["editing", "bold_off"]` (parallel state + leaf), verifying Case 1 structural check fires before intra-region candidate collection.

### should_fix 3: commitParallelExit opened transaction before running activities

`commitParallelExit` called `tx.Begin()` before exit and entry activity loops. A failed activity would roll back the tx but external side-effects from already-completed activities were not reversible.

**Fix:** Moved `tx.Begin()` to after all exit and entry activities complete, matching the `commitParallelIntra` pattern. Removed `Rollback` calls from activity failure paths (no tx is open at that point).

### should_fix 4: recordHistory used parallelState name as sourceLeaf

`commitParallelExit` called `recordHistory(instance, parallelState, exitSeq, cm.Definition)`. The `sourceLeaf` argument was the parallel state name, so every region compound state got `DeepHistory[region] = "editing"` instead of its actual active leaf.

**Fix:** For each region (sorted for determinism), compute a per-region exit sub-sequence using `computeExitSequence(leaf, parallelState, cm.Ancestry)` — this yields `[leaf, region]` stopping before the parallel state. Call `recordHistory(instance, leaf, regionExitSeq, cm.Definition)` once per region. This ensures `DeepHistory["bold_region"] = "bold_off"` and `DeepHistory["italic_region"] = "italic_off"` are recorded independently.

## Test Results

All 12 test packages pass, 0 failures.

## Deviations from Plan

None. All fixes were within the scope of the original parallel states plan.
