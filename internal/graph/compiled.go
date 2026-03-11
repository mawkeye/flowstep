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
// Returns an error only for structural problems detectable without validation context.
func Compile(def *types.Definition, _ Sentinels) (*CompiledMachine, error) {
	cm := &CompiledMachine{
		Definition:         def,
		Ancestry:           make(map[string][]string),
		DepthMap:           make(map[string]int),
		InitialLeafMap:     make(map[string]string),
		RegionIndex:        make(map[string][]string),
		TransitionsByState: make(map[string][]*CompiledTransition),
	}

	// Populate DepthMap: all states at depth 0 for flat workflows.
	// (Task 3 will populate Ancestry/DepthMap for hierarchical states when Parent field is added.)
	for name := range def.States {
		cm.DepthMap[name] = 0
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
		fmt.Fprintf(h, "state:%s|init:%v|term:%v|wait:%v\n",
			st.Name, st.IsInitial, st.IsTerminal, st.IsWait)
	}

	// Transitions (sorted by name)
	for _, name := range slices.Sorted(maps.Keys(def.Transitions)) {
		tr := def.Transitions[name]
		srcs := slices.Clone(tr.Sources)
		slices.Sort(srcs)
		fmt.Fprintf(h, "tr:%s|srcs:%v|target:%s|event:%s|trigger:%s|key:%s\n",
			tr.Name, srcs, tr.Target, tr.Event, tr.TriggerType, tr.TriggerKey)

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
