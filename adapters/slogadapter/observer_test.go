package slogadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/chanbus"
	"github.com/mawkeye/flowstep/adapters/memstore"
	"github.com/mawkeye/flowstep/adapters/slogadapter"
	"github.com/mawkeye/flowstep/types"
)

// newTestLogger returns a logger writing JSON to buf with level Debug.
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// parseLog reads the JSON log line from buf.
func parseLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	if buf.Len() == 0 {
		t.Fatal("log buffer is empty — observer did not write output")
	}
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("failed to parse log JSON: %v\n%s", err, buf.String())
	}
	return record
}

func assertField(t *testing.T, record map[string]any, key string, want any) {
	t.Helper()
	got, ok := record[key]
	if !ok {
		t.Errorf("log record missing field %q", key)
		return
	}
	// JSON numbers decode as float64
	switch wantTyped := want.(type) {
	case int64:
		if int64(got.(float64)) != wantTyped {
			t.Errorf("field %q: want %v, got %v", key, want, got)
		}
	default:
		if got != want {
			t.Errorf("field %q: want %v, got %v", key, want, got)
		}
	}
}

// ─── Unit tests — one per observer method ────────────────────────────────────

func TestObserver_OnTransition(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))
	ctx := context.Background()

	obs.OnTransition(ctx, types.TransitionEvent{
		Result: types.TransitionResult{
			Instance:       types.WorkflowInstance{WorkflowType: "order", AggregateID: "o-1"},
			TransitionName: "complete",
		},
		Duration: 50 * time.Millisecond,
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "INFO")
	assertField(t, rec, "msg", "flowstep.transition")
	assertField(t, rec, "workflow_type", "order")
	assertField(t, rec, "aggregate_id", "o-1")
	assertField(t, rec, "transition_name", "complete")
	assertField(t, rec, "duration_ms", int64(50))
}

func TestObserver_OnGuardFailed(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))
	ctx := context.Background()

	obs.OnGuardFailed(ctx, types.GuardFailureEvent{
		WorkflowType:   "order",
		TransitionName: "complete",
		GuardName:      "stockAvailable",
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "WARN")
	assertField(t, rec, "msg", "flowstep.guard_failed")
	assertField(t, rec, "workflow_type", "order")
	assertField(t, rec, "transition_name", "complete")
	assertField(t, rec, "guard_name", "stockAvailable")
}

func TestObserver_OnActivityDispatched(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))
	ctx := context.Background()

	obs.OnActivityDispatched(ctx, types.ActivityDispatchedEvent{
		Invocation: types.ActivityInvocation{
			ID:           "inv-1",
			ActivityName: "sendEmail",
			WorkflowType: "order",
			AggregateID:  "o-1",
		},
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "INFO")
	assertField(t, rec, "msg", "flowstep.activity_dispatched")
	assertField(t, rec, "activity_name", "sendEmail")
	assertField(t, rec, "invocation_id", "inv-1")
	assertField(t, rec, "workflow_type", "order")
	assertField(t, rec, "aggregate_id", "o-1")
}

func TestObserver_OnActivityCompleted(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))
	ctx := context.Background()

	obs.OnActivityCompleted(ctx, types.ActivityCompletedEvent{
		Invocation: types.ActivityInvocation{
			ID:           "inv-1",
			ActivityName: "sendEmail",
			WorkflowType: "order",
			AggregateID:  "o-1",
		},
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "INFO")
	assertField(t, rec, "msg", "flowstep.activity_completed")
	assertField(t, rec, "activity_name", "sendEmail")
	assertField(t, rec, "invocation_id", "inv-1")
}

func TestObserver_OnActivityFailed(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))
	ctx := context.Background()

	obs.OnActivityFailed(ctx, types.ActivityFailedEvent{
		Invocation: types.ActivityInvocation{
			ID:           "inv-1",
			ActivityName: "sendEmail",
			WorkflowType: "order",
			AggregateID:  "o-1",
		},
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "ERROR")
	assertField(t, rec, "msg", "flowstep.activity_failed")
	assertField(t, rec, "activity_name", "sendEmail")
	assertField(t, rec, "invocation_id", "inv-1")
}

func TestObserver_OnStuck(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))
	ctx := context.Background()

	obs.OnStuck(ctx, types.StuckEvent{
		Instance: types.WorkflowInstance{WorkflowType: "order", AggregateID: "o-1"},
		Reason:   "no matching transition",
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "ERROR")
	assertField(t, rec, "msg", "flowstep.stuck")
	assertField(t, rec, "workflow_type", "order")
	assertField(t, rec, "aggregate_id", "o-1")
	assertField(t, rec, "reason", "no matching transition")
}

func TestObserver_OnPostCommitError(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))
	ctx := context.Background()

	obs.OnPostCommitError(ctx, types.PostCommitErrorEvent{
		Operation: "EventBus.Emit",
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "ERROR")
	assertField(t, rec, "msg", "flowstep.post_commit_error")
	assertField(t, rec, "operation", "EventBus.Emit")
}

// ─── Custom level override ─────────────────────────────────────────────────────

func TestObserver_CustomTransitionLevel(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(
		newTestLogger(&buf),
		slogadapter.WithTransitionLevel(slog.LevelDebug),
	)
	ctx := context.Background()

	obs.OnTransition(ctx, types.TransitionEvent{
		Result: types.TransitionResult{
			Instance:       types.WorkflowInstance{WorkflowType: "order", AggregateID: "o-1"},
			TransitionName: "complete",
		},
		Duration: 10 * time.Millisecond,
	})

	rec := parseLog(t, &buf)
	assertField(t, rec, "level", "DEBUG")
	assertField(t, rec, "msg", "flowstep.transition")
}

// ─── Integration test ─────────────────────────────────────────────────────────

func TestObserver_Integration_TransitionLogged(t *testing.T) {
	var buf bytes.Buffer
	obs := slogadapter.New(newTestLogger(&buf))

	es := memstore.NewEventStore()
	is := memstore.NewInstanceStore()
	engine, err := flowstep.NewEngine(
		flowstep.WithEventStore(es),
		flowstep.WithInstanceStore(is),
		flowstep.WithTxProvider(memstore.NewTxProvider()),
		flowstep.WithEventBus(chanbus.New()),
		flowstep.WithClock(flowstep.RealClock{}),
		flowstep.WithObservers(obs),
	)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	def, err := flowstep.Define("order", "simple").
		Version(1).
		States(
			flowstep.Initial("CREATED"),
			flowstep.Terminal("DONE"),
		).
		Transition("complete",
			flowstep.From("CREATED"),
			flowstep.To("DONE"),
			flowstep.Event("OrderCompleted"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build definition: %v", err)
	}

	if err := engine.Register(def); err != nil {
		t.Fatalf("failed to register definition: %v", err)
	}

	ctx := context.Background()
	if _, err := engine.Transition(ctx, "order", "o-1", "complete", "user-1", nil); err != nil {
		t.Fatalf("transition failed: %v", err)
	}

	// The transition log line is written synchronously; assert its content.
	rec := parseLog(t, &buf)
	assertField(t, rec, "msg", "flowstep.transition")
	assertField(t, rec, "workflow_type", "simple")
	assertField(t, rec, "transition_name", "complete")
}
