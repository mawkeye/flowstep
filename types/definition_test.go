package types

import "testing"

func TestTriggerTypeConstants(t *testing.T) {
	triggers := []TriggerType{
		TriggerDirect,
		TriggerSignal,
		TriggerTaskCompleted,
		TriggerChildCompleted,
		TriggerChildrenJoined,
		TriggerTimeout,
	}
	seen := make(map[TriggerType]bool)
	for _, tt := range triggers {
		if seen[tt] {
			t.Errorf("duplicate trigger type: %s", tt)
		}
		seen[tt] = true
	}
}

func TestDefinitionFields(t *testing.T) {
	def := &Definition{
		AggregateType: "order",
		WorkflowType:  "standard",
		States: map[string]StateDef{
			"CREATED": {Name: "CREATED", IsInitial: true},
		},
	}
	if def.AggregateType != "order" {
		t.Error("unexpected aggregate type")
	}
	if def.WorkflowType != "standard" {
		t.Error("unexpected workflow type")
	}
}
