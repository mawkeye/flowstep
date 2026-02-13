package types

import (
	"strings"
	"testing"
)

func TestMermaidExport(t *testing.T) {
	def := &Definition{
		AggregateType: "order",
		WorkflowType:  "standard",
		Version:       1,
		InitialState:  "CREATED",
		TerminalStates: []string{"SHIPPED"},
		States: map[string]StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true},
			"PAID":    {Name: "PAID"},
			"SHIPPED": {Name: "SHIPPED", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"pay":  {Name: "pay", Sources: []string{"CREATED"}, Target: "PAID", Event: "Paid"},
			"ship": {Name: "ship", Sources: []string{"PAID"}, Target: "SHIPPED", Event: "Shipped"},
		},
	}

	diagram := def.Mermaid()

	assertions := []string{
		"stateDiagram-v2",
		"[*] --> CREATED",
		"CREATED --> PAID : pay",
		"PAID --> SHIPPED : ship",
		"SHIPPED --> [*]",
	}

	for _, expected := range assertions {
		if !strings.Contains(diagram, expected) {
			t.Errorf("expected diagram to contain %q\ngot:\n%s", expected, diagram)
		}
	}
}

func TestMermaidExportMultiSource(t *testing.T) {
	def := &Definition{
		AggregateType:  "order",
		WorkflowType:   "multi",
		InitialState:   "A",
		TerminalStates: []string{"DONE"},
		States: map[string]StateDef{
			"A":    {Name: "A", IsInitial: true},
			"B":    {Name: "B"},
			"DONE": {Name: "DONE", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"ab":     {Name: "ab", Sources: []string{"A"}, Target: "B"},
			"finish": {Name: "finish", Sources: []string{"A", "B"}, Target: "DONE"},
		},
	}

	diagram := def.Mermaid()

	if !strings.Contains(diagram, "A --> DONE : finish") {
		t.Errorf("expected A --> DONE : finish in diagram\ngot:\n%s", diagram)
	}
	if !strings.Contains(diagram, "B --> DONE : finish") {
		t.Errorf("expected B --> DONE : finish in diagram\ngot:\n%s", diagram)
	}
}
