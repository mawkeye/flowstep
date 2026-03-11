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

	diagram := Mermaid(def)

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

func TestMermaidExportRoutedTransitions(t *testing.T) {
	def := &Definition{
		AggregateType:  "order",
		WorkflowType:   "routed",
		InitialState:   "CREATED",
		TerminalStates: []string{"APPROVED", "REJECTED"},
		States: map[string]StateDef{
			"CREATED":  {Name: "CREATED", IsInitial: true},
			"APPROVED": {Name: "APPROVED", IsTerminal: true},
			"REJECTED": {Name: "REJECTED", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"review": {
				Name:    "review",
				Sources: []string{"CREATED"},
				Routes: []Route{
					{Target: "APPROVED"},
					{Target: "REJECTED"},
					{Target: ""}, // empty target should be skipped
				},
			},
		},
	}

	diagram := Mermaid(def)

	assertions := []string{
		"CREATED --> APPROVED : review",
		"CREATED --> REJECTED : review",
	}
	for _, expected := range assertions {
		if !strings.Contains(diagram, expected) {
			t.Errorf("expected diagram to contain %q\ngot:\n%s", expected, diagram)
		}
	}
}

func TestMermaidExport_CompoundState_TwoLevel(t *testing.T) {
	def := &Definition{
		AggregateType:  "order",
		WorkflowType:   "compound",
		InitialState:   "CREATED",
		TerminalStates: []string{"APPROVED"},
		States: map[string]StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true},
			"PROCESSING": {
				Name: "PROCESSING", IsCompound: true,
				InitialChild: "VALIDATING", Children: []string{"VALIDATING", "APPROVED"},
			},
			"VALIDATING": {Name: "VALIDATING", Parent: "PROCESSING"},
			"APPROVED":   {Name: "APPROVED", Parent: "PROCESSING", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"start":   {Name: "start", Sources: []string{"CREATED"}, Target: "PROCESSING"},
			"approve": {Name: "approve", Sources: []string{"PROCESSING"}, Target: "APPROVED"},
		},
	}

	diagram := Mermaid(def)

	checks := []string{
		"state PROCESSING {",
		"[*] --> VALIDATING",
		"PROCESSING --> APPROVED : approve",
		"CREATED --> PROCESSING : start",
		"APPROVED --> [*]",
	}
	for _, want := range checks {
		if !strings.Contains(diagram, want) {
			t.Errorf("expected diagram to contain %q\ngot:\n%s", want, diagram)
		}
	}
}

func TestMermaidExport_CompoundState_ThreeLevel(t *testing.T) {
	def := &Definition{
		AggregateType:  "order",
		WorkflowType:   "deep",
		InitialState:   "CREATED",
		TerminalStates: []string{"DONE"},
		States: map[string]StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true},
			"ROOT": {
				Name: "ROOT", IsCompound: true,
				InitialChild: "ORDER", Children: []string{"ORDER", "DONE"},
			},
			"ORDER": {
				Name: "ORDER", IsCompound: true, Parent: "ROOT",
				InitialChild: "VALIDATING", Children: []string{"VALIDATING"},
			},
			"VALIDATING": {Name: "VALIDATING", Parent: "ORDER"},
			"DONE":       {Name: "DONE", Parent: "ROOT", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"start":  {Name: "start", Sources: []string{"CREATED"}, Target: "ROOT"},
			"finish": {Name: "finish", Sources: []string{"ORDER"}, Target: "DONE"},
		},
	}

	diagram := Mermaid(def)

	checks := []string{
		"state ROOT {",
		"state ORDER {",
		"[*] --> ORDER",      // inside ROOT block
		"[*] --> VALIDATING", // inside ORDER block
		"CREATED --> ROOT : start",
	}
	for _, want := range checks {
		if !strings.Contains(diagram, want) {
			t.Errorf("expected diagram to contain %q\ngot:\n%s", want, diagram)
		}
	}
}

func TestMermaidExport_FlatWorkflow_UnchangedOutput(t *testing.T) {
	def := &Definition{
		AggregateType:  "order",
		WorkflowType:   "flat",
		InitialState:   "A",
		TerminalStates: []string{"C"},
		States: map[string]StateDef{
			"A": {Name: "A", IsInitial: true},
			"B": {Name: "B"},
			"C": {Name: "C", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"ab": {Name: "ab", Sources: []string{"A"}, Target: "B"},
			"bc": {Name: "bc", Sources: []string{"B"}, Target: "C"},
		},
	}

	diagram := Mermaid(def)

	if strings.Contains(diagram, "state ") {
		t.Errorf("flat workflow should not contain 'state ' blocks, got:\n%s", diagram)
	}
	checks := []string{
		"[*] --> A",
		"A --> B : ab",
		"B --> C : bc",
		"C --> [*]",
	}
	for _, want := range checks {
		if !strings.Contains(diagram, want) {
			t.Errorf("expected diagram to contain %q\ngot:\n%s", want, diagram)
		}
	}
}

// ─── Task 5 (history states): Mermaid history annotation tests ───────────────

func TestMermaidExport_HistoryShallow_AnnotatesLabel(t *testing.T) {
	def := &Definition{
		AggregateType:  "order",
		WorkflowType:   "history-shallow",
		InitialState:   "IDLE",
		TerminalStates: []string{"DONE"},
		States: map[string]StateDef{
			"IDLE":       {Name: "IDLE", IsInitial: true},
			"PROCESSING": {Name: "PROCESSING", IsCompound: true, InitialChild: "DRAFT", Children: []string{"DRAFT"}},
			"DRAFT":      {Name: "DRAFT", Parent: "PROCESSING"},
			"DONE":       {Name: "DONE", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"start":  {Name: "start", Sources: []string{"IDLE"}, Target: "PROCESSING"},
			"resume": {Name: "resume", Sources: []string{"IDLE"}, Target: "PROCESSING", HistoryMode: HistoryShallow},
			"finish": {Name: "finish", Sources: []string{"PROCESSING"}, Target: "DONE"},
		},
	}

	diagram := Mermaid(def)

	if !strings.Contains(diagram, "IDLE --> PROCESSING : resume [H]") {
		t.Errorf("expected shallow history annotation [H]\ngot:\n%s", diagram)
	}
	// Non-history transition must NOT be annotated.
	if strings.Contains(diagram, "start [H]") || strings.Contains(diagram, "start [H*]") {
		t.Errorf("non-history transition 'start' must not have history annotation\ngot:\n%s", diagram)
	}
}

func TestMermaidExport_HistoryDeep_AnnotatesLabel(t *testing.T) {
	def := &Definition{
		AggregateType:  "order",
		WorkflowType:   "history-deep",
		InitialState:   "IDLE",
		TerminalStates: []string{"DONE"},
		States: map[string]StateDef{
			"IDLE":       {Name: "IDLE", IsInitial: true},
			"PROCESSING": {Name: "PROCESSING", IsCompound: true, InitialChild: "DRAFT", Children: []string{"DRAFT"}},
			"DRAFT":      {Name: "DRAFT", Parent: "PROCESSING"},
			"DONE":       {Name: "DONE", IsTerminal: true},
		},
		Transitions: map[string]TransitionDef{
			"start":  {Name: "start", Sources: []string{"IDLE"}, Target: "PROCESSING"},
			"resume": {Name: "resume", Sources: []string{"IDLE"}, Target: "PROCESSING", HistoryMode: HistoryDeep},
			"finish": {Name: "finish", Sources: []string{"PROCESSING"}, Target: "DONE"},
		},
	}

	diagram := Mermaid(def)

	if !strings.Contains(diagram, "IDLE --> PROCESSING : resume [H*]") {
		t.Errorf("expected deep history annotation [H*]\ngot:\n%s", diagram)
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

	diagram := Mermaid(def)

	if !strings.Contains(diagram, "A --> DONE : finish") {
		t.Errorf("expected A --> DONE : finish in diagram\ngot:\n%s", diagram)
	}
	if !strings.Contains(diagram, "B --> DONE : finish") {
		t.Errorf("expected B --> DONE : finish in diagram\ngot:\n%s", diagram)
	}
}
