package types

import (
	"fmt"
	"sort"
	"strings"
)

// Mermaid generates a Mermaid stateDiagram-v2 representation of the workflow.
// Compound states render as nested `state X { ... }` blocks.
// Flat workflows produce the same output as before this feature was added.
func Mermaid(d *Definition) string {
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n")
	fmt.Fprintf(&b, "    [*] --> %s\n", d.InitialState)

	// Sort transition names for deterministic output.
	names := make([]string, 0, len(d.Transitions))
	for name := range d.Transitions {
		names = append(names, name)
	}
	sort.Strings(names)

	// Partition transitions by rendering scope: the tightest compound state
	// whose subtree contains all participant states (sources + targets), or
	// top-level ("") if no such compound state exists.
	compoundTransitions := make(map[string][]TransitionDef)
	var topLevelTransitions []TransitionDef
	for _, name := range names {
		tr := d.Transitions[name]
		scope := mermaidTransitionScope(tr, d)
		if scope == "" {
			topLevelTransitions = append(topLevelTransitions, tr)
		} else {
			compoundTransitions[scope] = append(compoundTransitions[scope], tr)
		}
	}

	// Render root compound state blocks (states with no parent that are compound).
	rootNames := mermaidRootStateNames(d)
	for _, name := range rootNames {
		if d.States[name].IsCompound {
			mermaidCompoundBlock(&b, name, d, compoundTransitions, 1)
		}
	}

	// Render top-level transitions.
	for _, tr := range topLevelTransitions {
		srcs := make([]string, len(tr.Sources))
		copy(srcs, tr.Sources)
		sort.Strings(srcs)
		label := mermaidTransitionLabel(tr)
		for _, src := range srcs {
			if tr.Target != "" {
				fmt.Fprintf(&b, "    %s --> %s : %s\n", src, tr.Target, label)
			} else {
				for _, route := range tr.Routes {
					if route.Target == "" {
						continue
					}
					fmt.Fprintf(&b, "    %s --> %s : %s\n", src, route.Target, label)
				}
			}
		}
	}

	// Terminal state exits.
	terminals := make([]string, len(d.TerminalStates))
	copy(terminals, d.TerminalStates)
	sort.Strings(terminals)
	for _, ts := range terminals {
		fmt.Fprintf(&b, "    %s --> [*]\n", ts)
	}

	return b.String()
}

// mermaidCompoundBlock writes a `state Name { ... }` block at the given indent depth.
func mermaidCompoundBlock(b *strings.Builder, name string, d *Definition, compoundTransitions map[string][]TransitionDef, depth int) {
	indent := strings.Repeat("    ", depth)
	st := d.States[name]
	fmt.Fprintf(b, "%sstate %s {\n", indent, name)

	if st.InitialChild != "" {
		fmt.Fprintf(b, "%s    [*] --> %s\n", indent, st.InitialChild)
	}

	// Render nested compound children (sorted for determinism).
	children := make([]string, len(st.Children))
	copy(children, st.Children)
	sort.Strings(children)
	for _, child := range children {
		if d.States[child].IsCompound {
			mermaidCompoundBlock(b, child, d, compoundTransitions, depth+1)
		}
	}

	// Render transitions scoped to this compound state.
	for _, tr := range compoundTransitions[name] {
		srcs := make([]string, len(tr.Sources))
		copy(srcs, tr.Sources)
		sort.Strings(srcs)
		label := mermaidTransitionLabel(tr)
		for _, src := range srcs {
			if tr.Target != "" {
				fmt.Fprintf(b, "%s    %s --> %s : %s\n", indent, src, tr.Target, label)
			} else {
				for _, route := range tr.Routes {
					if route.Target == "" {
						continue
					}
					fmt.Fprintf(b, "%s    %s --> %s : %s\n", indent, src, route.Target, label)
				}
			}
		}
	}

	fmt.Fprintf(b, "%s}\n", indent)
}

// mermaidTransitionScope returns the tightest compound state whose subtree contains all
// participant states of tr (sources + targets). Returns "" for top-level transitions.
func mermaidTransitionScope(tr TransitionDef, d *Definition) string {
	participants := make([]string, 0, len(tr.Sources)+1)
	participants = append(participants, tr.Sources...)
	if tr.Target != "" {
		participants = append(participants, tr.Target)
	} else {
		for _, route := range tr.Routes {
			if route.Target != "" {
				participants = append(participants, route.Target)
			}
		}
	}
	if len(participants) == 0 {
		return ""
	}

	bestScope := ""
	bestDepth := -1
	for compName, compSt := range d.States {
		if !compSt.IsCompound {
			continue
		}
		if mermaidAllInSubtree(participants, compName, d) {
			depth := mermaidStateDepth(compName, d)
			if depth > bestDepth {
				bestDepth = depth
				bestScope = compName
			}
		}
	}
	return bestScope
}

// mermaidAllInSubtree returns true if every state in states is within the subtree rooted at root.
func mermaidAllInSubtree(states []string, root string, d *Definition) bool {
	for _, s := range states {
		if !mermaidIsInSubtree(s, root, d) {
			return false
		}
	}
	return true
}

// mermaidIsInSubtree returns true if stateName is root or a descendant of root.
func mermaidIsInSubtree(stateName, root string, d *Definition) bool {
	if stateName == root {
		return true
	}
	rootSt, ok := d.States[root]
	if !ok || !rootSt.IsCompound {
		return false
	}
	for _, child := range rootSt.Children {
		if mermaidIsInSubtree(stateName, child, d) {
			return true
		}
	}
	return false
}

// mermaidStateDepth returns the number of ancestors (0 for root states).
func mermaidStateDepth(name string, d *Definition) int {
	depth := 0
	st, ok := d.States[name]
	for ok && st.Parent != "" {
		depth++
		st, ok = d.States[st.Parent]
	}
	return depth
}

// mermaidTransitionLabel returns the label for a transition arrow.
// History-aware transitions are annotated: [H] for shallow, [H*] for deep.
func mermaidTransitionLabel(tr TransitionDef) string {
	switch tr.HistoryMode {
	case HistoryShallow:
		return tr.Name + " [H]"
	case HistoryDeep:
		return tr.Name + " [H*]"
	default:
		return tr.Name
	}
}

// mermaidRootStateNames returns names of all root states (no Parent) sorted for determinism.
func mermaidRootStateNames(d *Definition) []string {
	var roots []string
	for name, st := range d.States {
		if st.Parent == "" {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)
	return roots
}
