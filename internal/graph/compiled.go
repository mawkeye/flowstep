package graph

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"slices"

	"github.com/mawkeye/flowstep/types"
)

// CompiledTransition wraps a TransitionDef with precomputed data.
type CompiledTransition struct {
	// Def is a pointer to a copy of the TransitionDef, not a pointer into the
	// Definition.Transitions map. It is immutable — callers must not mutate *Def,
	// as changes will not propagate back to the canonical definition.
	Def        *types.TransitionDef
	GuardNames []string // populated by Compile() with NamedGuard names or fmt.Sprintf("%T")
}

// CompiledMachine is the intermediate representation between a Definition and the runtime engine.
// It holds the original definition plus precomputed graph data for efficient runtime execution.
// For flat workflows (no hierarchy), Ancestry/InitialLeafMap/RegionIndex are empty.
type CompiledMachine struct {
	Definition *types.Definition

	// Ancestry maps state name → ordered list of ancestor state names (nearest first).
	// Empty for flat workflows (no Parent fields).
	Ancestry map[string][]string

	// DepthMap maps state name → hierarchy depth (0 for all states in flat workflows).
	DepthMap map[string]int

	// InitialLeafMap maps compound state → its deepest initial leaf state.
	// Empty for flat workflows.
	InitialLeafMap map[string]string

	// RegionIndex maps parallel state → slice of region IDs.
	// Empty for flat workflows.
	RegionIndex map[string][]string

	// DefinitionHash is a deterministic SHA-256 hash of the definition's canonical form.
	// Used to detect duplicate registrations of the same version.
	DefinitionHash string

	// TransitionsByState maps source state name → transitions originating from that state.
	TransitionsByState map[string][]*CompiledTransition
}

// LCA returns the lowest common ancestor of stateA and stateB, and whether one was found.
// For flat workflows (no hierarchy), always returns ("", false).
// The bool return distinguishes "no ancestor found" from a state that happens to be named "".
func (cm *CompiledMachine) LCA(stateA, stateB string) (string, bool) {
	ancestorsA := cm.Ancestry[stateA]
	if len(ancestorsA) == 0 {
		return "", false
	}
	ancestorsB := cm.Ancestry[stateB]
	if len(ancestorsB) == 0 {
		return "", false
	}
	bSet := make(map[string]bool, len(ancestorsB))
	for _, a := range ancestorsB {
		bSet[a] = true
	}
	for _, a := range ancestorsA {
		if bSet[a] {
			return a, true
		}
	}
	return "", false
}

// Compile builds a CompiledMachine from a Definition.
// It assumes the definition has already been validated with Validate().
// Returns an error if a circular parent-child hierarchy is detected.
func Compile(def *types.Definition, s Sentinels) (*CompiledMachine, error) {
	cm := &CompiledMachine{
		Definition:         def,
		Ancestry:           make(map[string][]string),
		DepthMap:           make(map[string]int),
		InitialLeafMap:     make(map[string]string),
		RegionIndex:        make(map[string][]string),
		TransitionsByState: make(map[string][]*CompiledTransition),
	}

	// Populate Ancestry and DepthMap.
	// For flat workflows (no Parent), Ancestry stays empty and DepthMap is 0 for all.
	ancestryMemo := make(map[string][]string)
	for name := range def.States {
		chain, err := buildAncestry(name, def, ancestryMemo, make(map[string]bool), s)
		if err != nil {
			return nil, err
		}
		if len(chain) > 0 {
			cm.Ancestry[name] = chain
		}
		cm.DepthMap[name] = len(chain)
	}

	// Populate RegionIndex for parallel states: maps parallel state → region child names.
	for name, st := range def.States {
		if st.IsParallel {
			cm.RegionIndex[name] = st.Children
		}
	}

	// Populate InitialLeafMap for compound states (recursive initial-child resolution).
	// Parallel states are excluded — they have no single initial leaf.
	leafMemo := make(map[string]string)
	for name, st := range def.States {
		if st.IsCompound && !st.IsParallel && st.InitialChild != "" {
			cm.InitialLeafMap[name] = resolveInitialLeaf(name, def, leafMemo)
		}
	}

	// Build TransitionsByState index with precomputed guard names.
	for i := range def.Transitions {
		tr := def.Transitions[i]
		ct := &CompiledTransition{
			Def:        &tr,
			GuardNames: resolveGuardNames(tr.Guards),
		}
		for _, src := range tr.Sources {
			cm.TransitionsByState[src] = append(cm.TransitionsByState[src], ct)
		}
	}
	// Ensure all states have an entry in TransitionsByState (even terminal ones with no outgoing).
	for name := range def.States {
		if _, ok := cm.TransitionsByState[name]; !ok {
			cm.TransitionsByState[name] = []*CompiledTransition{}
		}
	}

	// Compute deterministic hash.
	cm.DefinitionHash = computeHash(def)

	return cm, nil
}

// buildAncestry returns the ordered ancestor chain for a state (nearest first).
// For root states (no Parent), returns an empty slice.
// Returns ErrCircularHierarchy if a cycle is detected in the parent chain.
func buildAncestry(
	name string,
	def *types.Definition,
	memo map[string][]string,
	visiting map[string]bool,
	s Sentinels,
) ([]string, error) {
	if chain, ok := memo[name]; ok {
		return chain, nil
	}
	st, ok := def.States[name]
	if !ok || st.Parent == "" {
		memo[name] = []string{}
		return []string{}, nil
	}
	if visiting[name] {
		return nil, &ValidationError{
			Sentinel: s.ErrCircularHierarchy,
			Detail:   fmt.Sprintf("flowstep: circular parent-child hierarchy detected at state %q", name),
		}
	}
	visiting[name] = true
	parentChain, err := buildAncestry(st.Parent, def, memo, visiting, s)
	if err != nil {
		return nil, err
	}
	delete(visiting, name)

	chain := make([]string, 0, 1+len(parentChain))
	chain = append(chain, st.Parent)
	chain = append(chain, parentChain...)
	memo[name] = chain
	return chain, nil
}

// resolveInitialLeaf recursively follows InitialChild links until a leaf state is reached.
func resolveInitialLeaf(name string, def *types.Definition, memo map[string]string) string {
	if leaf, ok := memo[name]; ok {
		return leaf
	}
	st := def.States[name]
	if !st.IsCompound || st.InitialChild == "" {
		return name
	}
	leaf := resolveInitialLeaf(st.InitialChild, def, memo)
	memo[name] = leaf
	return leaf
}

// resolveGuardNames returns the display name for each guard in order.
// Guards implementing types.NamedGuard use their Name(); others fall back to fmt.Sprintf("%T", guard).
func resolveGuardNames(guards []types.Guard) []string {
	names := make([]string, len(guards))
	for i, g := range guards {
		if ng, ok := g.(types.NamedGuard); ok {
			names[i] = ng.Name()
		} else {
			names[i] = fmt.Sprintf("%T", g)
		}
	}
	return names
}

// HashDefinition returns a deterministic SHA-256 hash of a Definition's canonical form.
// It is exported so that Register() can perform hash-based dedup without recompiling.
func HashDefinition(def *types.Definition) string {
	return computeHash(def)
}

// computeHash returns a deterministic SHA-256 hash of a Definition's canonical form.
// It sorts all map keys before iterating to ensure stable output regardless of Go's
// non-deterministic map iteration order.
func computeHash(def *types.Definition) string {
	h := sha256.New()

	// Top-level scalar fields
	fmt.Fprintf(h, "agg:%s|wf:%s|ver:%d|init:%s\n",
		def.AggregateType, def.WorkflowType, def.Version, def.InitialState)

	// Terminal states (sorted)
	terminals := slices.Clone(def.TerminalStates)
	slices.Sort(terminals)
	fmt.Fprintf(h, "terminals:%v\n", terminals)

	// States (sorted by name)
	for _, name := range slices.Sorted(maps.Keys(def.States)) {
		st := def.States[name]
		children := slices.Clone(st.Children)
		slices.Sort(children)
		fmt.Fprintf(h, "state:%s|init:%v|term:%v|wait:%v|parent:%s|children:%v|initialChild:%s|compound:%v|parallel:%v|entry:%s|exit:%s\n",
			st.Name, st.IsInitial, st.IsTerminal, st.IsWait,
			st.Parent, children, st.InitialChild, st.IsCompound, st.IsParallel,
			st.EntryActivity, st.ExitActivity)
	}

	// Transitions (sorted by name)
	for _, name := range slices.Sorted(maps.Keys(def.Transitions)) {
		tr := def.Transitions[name]
		srcs := slices.Clone(tr.Sources)
		slices.Sort(srcs)
		fmt.Fprintf(h, "tr:%s|srcs:%v|target:%s|event:%s|trigger:%s|key:%s|history:%s|priority:%d\n",
			tr.Name, srcs, tr.Target, tr.Event, tr.TriggerType, tr.TriggerKey, tr.HistoryMode, tr.Priority)

		// Activities (sorted by name)
		actNames := make([]string, len(tr.Activities))
		actMap := make(map[string]types.ActivityDef, len(tr.Activities))
		for i, a := range tr.Activities {
			actNames[i] = a.Name
			actMap[a.Name] = a
		}
		slices.Sort(actNames)
		for _, actName := range actNames {
			a := actMap[actName]
			fmt.Fprintf(h, "act:%s|mode:%v|timeout:%d\n", a.Name, a.Mode, a.Timeout)
			// ResultSignals nested map — sort keys
			for _, sigKey := range slices.Sorted(maps.Keys(a.ResultSignals)) {
				fmt.Fprintf(h, "sig:%s->%s\n", sigKey, a.ResultSignals[sigKey])
			}
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}
