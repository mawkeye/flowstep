package types_test

import (
	"testing"

	"github.com/mawkeye/flowstep/types"
)

func TestHistoryMode_Constants(t *testing.T) {
	if types.HistoryShallow != "SHALLOW" {
		t.Errorf("HistoryShallow = %q, want %q", types.HistoryShallow, "SHALLOW")
	}
	if types.HistoryDeep != "DEEP" {
		t.Errorf("HistoryDeep = %q, want %q", types.HistoryDeep, "DEEP")
	}
	var zero types.HistoryMode
	if zero != "" {
		t.Errorf("zero HistoryMode = %q, want empty string", zero)
	}
}

func TestWorkflowInstance_HistoryFields(t *testing.T) {
	inst := types.WorkflowInstance{
		ShallowHistory: map[string]string{"PROCESSING": "REVIEWING"},
		DeepHistory:    map[string]string{"PROCESSING": "CAPTURE"},
	}
	if inst.ShallowHistory["PROCESSING"] != "REVIEWING" {
		t.Errorf("ShallowHistory[PROCESSING] = %q, want %q", inst.ShallowHistory["PROCESSING"], "REVIEWING")
	}
	if inst.DeepHistory["PROCESSING"] != "CAPTURE" {
		t.Errorf("DeepHistory[PROCESSING] = %q, want %q", inst.DeepHistory["PROCESSING"], "CAPTURE")
	}
}

func TestTransitionDef_HistoryMode(t *testing.T) {
	tr := types.TransitionDef{
		Name:        "resume",
		HistoryMode: types.HistoryShallow,
	}
	if tr.HistoryMode != types.HistoryShallow {
		t.Errorf("HistoryMode = %q, want %q", tr.HistoryMode, types.HistoryShallow)
	}
	var noHistory types.TransitionDef
	if noHistory.HistoryMode != "" {
		t.Errorf("zero HistoryMode = %q, want empty", noHistory.HistoryMode)
	}
}
