package types

import (
	"sort"
	"strings"
)

// Mermaid generates a Mermaid stateDiagram-v2 representation of the workflow.
func (d *Definition) Mermaid() string {
	var b strings.Builder
	b.WriteString("stateDiagram-v2\n")

	// Initial state entry
	b.WriteString("    [*] --> ")
	b.WriteString(d.InitialState)
	b.WriteString("\n")

	// Sort transition names for deterministic output
	names := make([]string, 0, len(d.Transitions))
	for name := range d.Transitions {
		names = append(names, name)
	}
	sort.Strings(names)

	// Transitions
	for _, name := range names {
		tr := d.Transitions[name]
		for _, src := range tr.Sources {
			target := tr.Target
			if target == "" {
				continue
			}
			b.WriteString("    ")
			b.WriteString(src)
			b.WriteString(" --> ")
			b.WriteString(target)
			b.WriteString(" : ")
			b.WriteString(name)
			b.WriteString("\n")
		}
	}

	// Terminal state exits
	terminals := make([]string, len(d.TerminalStates))
	copy(terminals, d.TerminalStates)
	sort.Strings(terminals)
	for _, ts := range terminals {
		b.WriteString("    ")
		b.WriteString(ts)
		b.WriteString(" --> [*]\n")
	}

	return b.String()
}
