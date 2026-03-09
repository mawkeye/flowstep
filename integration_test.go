package flowstate_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/testutil"
	"github.com/mawkeye/flowstate/types"
)

// --- Guards for the booking workflow ---

// withinTimeWindowGuard checks that current time is within a window before the booking.
type withinTimeWindowGuard struct {
	windowBefore time.Duration
}

func (g *withinTimeWindowGuard) Check(ctx context.Context, _ any, params map[string]any) error {
	bookingTime, ok := params["booking_time"].(time.Time)
	if !ok {
		return fmt.Errorf("booking_time required")
	}
	now, ok := params["now"].(time.Time)
	if !ok {
		return fmt.Errorf("now required")
	}
	earliest := bookingTime.Add(-g.windowBefore)
	if now.Before(earliest) {
		return fmt.Errorf("check-in not yet open (opens %v before booking)", g.windowBefore)
	}
	if now.After(bookingTime) {
		return fmt.Errorf("check-in window has passed")
	}
	return nil
}

// cancellationPolicyGuard blocks cancellation if too close to booking time.
type cancellationPolicyGuard struct {
	cutoff time.Duration
}

func (g *cancellationPolicyGuard) Check(_ context.Context, _ any, params map[string]any) error {
	bookingTime, ok := params["booking_time"].(time.Time)
	if !ok {
		return fmt.Errorf("booking_time required")
	}
	now, ok := params["now"].(time.Time)
	if !ok {
		return fmt.Errorf("now required")
	}
	if bookingTime.Sub(now) < g.cutoff {
		return fmt.Errorf("cancellation not allowed within %v of booking", g.cutoff)
	}
	return nil
}

// isProfessionalGuard checks that actor is a professional/trainer.
type isProfessionalGuard struct{}

func (g *isProfessionalGuard) Check(_ context.Context, _ any, params map[string]any) error {
	role, _ := params["role"].(string)
	if role != "trainer" {
		return fmt.Errorf("only trainers can initiate trainer cancellation")
	}
	return nil
}

// --- Activities ---

type noopActivity struct {
	name  string
	calls int
}

func (a *noopActivity) Name() string { return a.name }
func (a *noopActivity) Execute(_ context.Context, _ types.ActivityInput) (*types.ActivityResult, error) {
	a.calls++
	return &types.ActivityResult{Output: map[string]any{"ok": true}}, nil
}

// --- Booking workflow builder ---

func buildBookingWorkflow(t *testing.T) *types.Definition {
	t.Helper()

	checkInGuard := &withinTimeWindowGuard{windowBefore: 30 * time.Minute}
	cancelGuard := &cancellationPolicyGuard{cutoff: 2 * time.Hour}
	trainerGuard := &isProfessionalGuard{}

	def, err := flowstate.Define("booking", "fitness_booking").
		Version(1).
		States(
			flowstate.Initial("PENDING_PAYMENT"),
			flowstate.State("AWAITING_PAYMENT"),
			flowstate.State("CONFIRMED"),
			flowstate.State("CHECKED_IN"),
			flowstate.WaitState("AWAITING_TRAINER_DECISION"),
			flowstate.State("TRAINER_HOLD"),
			flowstate.Terminal("CANCELLED"),
			flowstate.Terminal("NO_SHOW"),
			flowstate.Terminal("COMPLETED"),
		).
		// Core payment flow
		Transition("initiate_payment",
			flowstate.From("PENDING_PAYMENT"),
			flowstate.To("AWAITING_PAYMENT"),
			flowstate.Event("PaymentInitiated"),
			flowstate.DispatchAndWait("process_stripe_charge"),
		).
		// Payment signals
		Transition("confirm",
			flowstate.From("AWAITING_PAYMENT"),
			flowstate.To("CONFIRMED"),
			flowstate.Event("BookingConfirmed"),
			flowstate.OnSignal("payment_succeeded"),
			flowstate.Dispatch("schedule_lighting"),
			flowstate.Dispatch("generate_credentials"),
			flowstate.Dispatch("send_confirmation_email"),
		).
		Transition("payment_failed",
			flowstate.From("AWAITING_PAYMENT"),
			flowstate.To("CANCELLED"),
			flowstate.Event("PaymentFailed"),
			flowstate.OnSignal("payment_failed"),
		).
		Transition("payment_timed_out",
			flowstate.From("AWAITING_PAYMENT"),
			flowstate.To("CANCELLED"),
			flowstate.Event("PaymentTimedOut"),
			flowstate.OnSignal("payment_timed_out"),
		).
		// Check-in
		Transition("check_in",
			flowstate.From("CONFIRMED"),
			flowstate.To("CHECKED_IN"),
			flowstate.Event("GuestCheckedIn"),
			flowstate.Guards(checkInGuard),
			flowstate.Dispatch("log_attendance"),
		).
		// Session completion
		Transition("complete_session",
			flowstate.From("CHECKED_IN"),
			flowstate.To("COMPLETED"),
			flowstate.Event("SessionCompleted"),
		).
		// User cancellation (from multiple states)
		Transition("cancel_user",
			flowstate.From("PENDING_PAYMENT", "CONFIRMED"),
			flowstate.To("CANCELLED"),
			flowstate.Event("BookingCancelledByUser"),
			flowstate.Guards(cancelGuard),
		).
		// Trainer cancellation initiation (creates wait state + task)
		Transition("trainer_cancel_init",
			flowstate.From("CONFIRMED"),
			flowstate.To("AWAITING_TRAINER_DECISION"),
			flowstate.Event("TrainerCancelInitiated"),
			flowstate.Guards(trainerGuard),
			flowstate.EmitTask(types.TaskDef{
				Type:        "trainer_release_decision",
				Description: "Decide whether to release the booking or hold for trainer",
				Options:     []string{"release_to_public", "hold_for_trainer"},
				Timeout:     60 * time.Minute,
			}),
		).
		// Trainer decision outcomes (task-triggered)
		Transition("release_to_public",
			flowstate.From("AWAITING_TRAINER_DECISION"),
			flowstate.To("CANCELLED"),
			flowstate.Event("BookingReleasedToPublic"),
			flowstate.OnTaskCompleted("trainer_release_decision"),
		).
		Transition("hold_for_trainer",
			flowstate.From("AWAITING_TRAINER_DECISION"),
			flowstate.To("TRAINER_HOLD"),
			flowstate.Event("BookingHeldForTrainer"),
			flowstate.OnTaskCompleted("trainer_release_decision"),
		).
		// Trainer hold completion
		Transition("trainer_confirm",
			flowstate.From("TRAINER_HOLD"),
			flowstate.To("CONFIRMED"),
			flowstate.Event("TrainerConfirmed"),
		).
		// No-show
		Transition("mark_no_show",
			flowstate.From("CONFIRMED"),
			flowstate.To("NO_SHOW"),
			flowstate.Event("MarkedNoShow"),
		).
		Build()
	if err != nil {
		t.Fatalf("failed to build booking workflow: %v", err)
	}
	return def
}

// --- Integration Tests ---

func TestIntegration_BookingHappyPath(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	// Register activities
	paymentAct := &noopActivity{name: "process_stripe_charge"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	attendanceAct := &noopActivity{name: "log_attendance"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(attendanceAct)

	ctx := context.Background()
	bookingTime := te.Clock.Now().Add(3 * time.Hour)

	// Step 1: Initiate payment
	result, err := te.Engine.Transition(ctx, "booking", "b-1", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}
	if result.NewState != "AWAITING_PAYMENT" {
		t.Errorf("expected AWAITING_PAYMENT, got %s", result.NewState)
	}
	if paymentAct.calls != 1 {
		t.Errorf("expected payment activity called once, got %d", paymentAct.calls)
	}

	// Step 2: Payment succeeds (signal)
	result, err = te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-1",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-system",
	})
	if err != nil {
		t.Fatalf("payment_succeeded signal failed: %v", err)
	}
	if result.NewState != "CONFIRMED" {
		t.Errorf("expected CONFIRMED, got %s", result.NewState)
	}
	// Confirmation activities should have been dispatched
	if len(result.ActivitiesDispatched) != 3 {
		t.Errorf("expected 3 activities dispatched on confirm, got %d", len(result.ActivitiesDispatched))
	}

	// Step 3: Check in (within time window)
	result, err = te.Engine.Transition(ctx, "booking", "b-1", "check_in", "user-1", map[string]any{
		"booking_time": bookingTime,
		"now":          bookingTime.Add(-15 * time.Minute), // 15 min before, within 30-min window
	})
	if err != nil {
		t.Fatalf("check_in failed: %v", err)
	}
	if result.NewState != "CHECKED_IN" {
		t.Errorf("expected CHECKED_IN, got %s", result.NewState)
	}
	if attendanceAct.calls != 1 {
		t.Errorf("expected log_attendance called once, got %d", attendanceAct.calls)
	}

	// Step 4: Complete session
	result, err = te.Engine.Transition(ctx, "booking", "b-1", "complete_session", "system", nil)
	if err != nil {
		t.Fatalf("complete_session failed: %v", err)
	}
	if result.NewState != "COMPLETED" {
		t.Errorf("expected COMPLETED, got %s", result.NewState)
	}
	if !result.IsTerminal {
		t.Error("COMPLETED should be terminal")
	}

	// Verify event chain
	testutil.AssertState(t, te, "booking", "b-1", "COMPLETED")
	testutil.AssertEventCount(t, te, "booking", "b-1", 4)
	testutil.AssertEventChain(t, te, result.Event.CorrelationID,
		"PaymentInitiated", "BookingConfirmed", "GuestCheckedIn", "SessionCompleted")
}

func TestIntegration_BookingPaymentFailed(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	te.ActivityRunner.Register(paymentAct)

	ctx := context.Background()

	// Initiate payment
	_, err := te.Engine.Transition(ctx, "booking", "b-2", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}

	// Payment fails
	result, err := te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-2",
		SignalName:          "payment_failed",
		ActorID:             "payment-system",
	})
	if err != nil {
		t.Fatalf("payment_failed signal failed: %v", err)
	}
	if result.NewState != "CANCELLED" {
		t.Errorf("expected CANCELLED, got %s", result.NewState)
	}
	if !result.IsTerminal {
		t.Error("CANCELLED should be terminal")
	}

	testutil.AssertState(t, te, "booking", "b-2", "CANCELLED")
	testutil.AssertEventCount(t, te, "booking", "b-2", 2)
}

func TestIntegration_BookingPaymentTimedOut(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	te.ActivityRunner.Register(paymentAct)

	ctx := context.Background()

	_, err := te.Engine.Transition(ctx, "booking", "b-3", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}

	// Payment times out
	result, err := te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-3",
		SignalName:          "payment_timed_out",
		ActorID:             "timeout-scheduler",
	})
	if err != nil {
		t.Fatalf("payment_timed_out signal failed: %v", err)
	}
	if result.NewState != "CANCELLED" {
		t.Errorf("expected CANCELLED, got %s", result.NewState)
	}
	if result.Event.EventType != "PaymentTimedOut" {
		t.Errorf("expected PaymentTimedOut event, got %s", result.Event.EventType)
	}
}

func TestIntegration_BookingUserCancellation(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)

	ctx := context.Background()
	bookingTime := te.Clock.Now().Add(5 * time.Hour) // well ahead of cutoff

	// Cancel from PENDING_PAYMENT
	t.Run("from_pending_payment", func(t *testing.T) {
		result, err := te.Engine.Transition(ctx, "booking", "b-cancel-1", "cancel_user", "user-1", map[string]any{
			"booking_time": bookingTime,
			"now":          te.Clock.Now(),
		})
		if err != nil {
			t.Fatalf("cancel from PENDING_PAYMENT failed: %v", err)
		}
		if result.NewState != "CANCELLED" {
			t.Errorf("expected CANCELLED, got %s", result.NewState)
		}
	})

	// Cancel from CONFIRMED
	t.Run("from_confirmed", func(t *testing.T) {
		// Move to CONFIRMED first
		_, err := te.Engine.Transition(ctx, "booking", "b-cancel-2", "initiate_payment", "user-1", nil)
		if err != nil {
			t.Fatalf("initiate_payment failed: %v", err)
		}
		_, err = te.Engine.Signal(ctx, types.SignalInput{
			TargetAggregateType: "booking",
			TargetAggregateID:   "b-cancel-2",
			SignalName:          "payment_succeeded",
			ActorID:             "payment-system",
		})
		if err != nil {
			t.Fatalf("payment_succeeded failed: %v", err)
		}

		result, err := te.Engine.Transition(ctx, "booking", "b-cancel-2", "cancel_user", "user-1", map[string]any{
			"booking_time": bookingTime,
			"now":          te.Clock.Now(),
		})
		if err != nil {
			t.Fatalf("cancel from CONFIRMED failed: %v", err)
		}
		if result.NewState != "CANCELLED" {
			t.Errorf("expected CANCELLED, got %s", result.NewState)
		}
	})
}

func TestIntegration_BookingCancellationGuardRejects(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	ctx := context.Background()
	// Booking is in 1 hour — within the 2-hour cutoff
	bookingTime := te.Clock.Now().Add(1 * time.Hour)

	_, err := te.Engine.Transition(ctx, "booking", "b-guard-1", "cancel_user", "user-1", map[string]any{
		"booking_time": bookingTime,
		"now":          te.Clock.Now(),
	})
	if !errors.Is(err, flowstate.ErrGuardFailed) {
		t.Errorf("expected ErrGuardFailed, got %v", err)
	}

	// Booking should still be in initial state (no event appended)
	events, _ := te.EventStore.ListByAggregate(ctx, "booking", "b-guard-1")
	if len(events) != 0 {
		t.Errorf("expected no events after guard rejection, got %d", len(events))
	}
}

func TestIntegration_BookingCheckInGuardRejects(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	attendanceAct := &noopActivity{name: "log_attendance"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)
	te.ActivityRunner.Register(attendanceAct)

	ctx := context.Background()
	bookingTime := te.Clock.Now().Add(3 * time.Hour)

	// Move to CONFIRMED
	_, err := te.Engine.Transition(ctx, "booking", "b-guard-2", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}
	_, err = te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-guard-2",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-system",
	})
	if err != nil {
		t.Fatalf("payment_succeeded failed: %v", err)
	}

	// Try check-in too early (2 hours before — outside 30-min window)
	_, err = te.Engine.Transition(ctx, "booking", "b-guard-2", "check_in", "user-1", map[string]any{
		"booking_time": bookingTime,
		"now":          bookingTime.Add(-2 * time.Hour),
	})
	if !errors.Is(err, flowstate.ErrGuardFailed) {
		t.Errorf("expected ErrGuardFailed for early check-in, got %v", err)
	}

	// Try check-in after booking time (too late)
	_, err = te.Engine.Transition(ctx, "booking", "b-guard-2", "check_in", "user-1", map[string]any{
		"booking_time": bookingTime,
		"now":          bookingTime.Add(5 * time.Minute),
	})
	if !errors.Is(err, flowstate.ErrGuardFailed) {
		t.Errorf("expected ErrGuardFailed for late check-in, got %v", err)
	}

	// State should still be CONFIRMED
	testutil.AssertState(t, te, "booking", "b-guard-2", "CONFIRMED")
}

func TestIntegration_BookingTrainerHoldRelease(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)

	ctx := context.Background()

	// Move to CONFIRMED
	_, err := te.Engine.Transition(ctx, "booking", "b-trainer-1", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}
	_, err = te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-trainer-1",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-system",
	})
	if err != nil {
		t.Fatalf("payment_succeeded failed: %v", err)
	}

	// Non-trainer cannot initiate trainer cancellation
	_, err = te.Engine.Transition(ctx, "booking", "b-trainer-1", "trainer_cancel_init", "regular-user", map[string]any{
		"role": "member",
	})
	if !errors.Is(err, flowstate.ErrGuardFailed) {
		t.Errorf("expected ErrGuardFailed for non-trainer, got %v", err)
	}

	// Trainer initiates cancellation — creates wait state with task
	result, err := te.Engine.Transition(ctx, "booking", "b-trainer-1", "trainer_cancel_init", "trainer-1", map[string]any{
		"role": "trainer",
	})
	if err != nil {
		t.Fatalf("trainer_cancel_init failed: %v", err)
	}
	if result.NewState != "AWAITING_TRAINER_DECISION" {
		t.Errorf("expected AWAITING_TRAINER_DECISION, got %s", result.NewState)
	}
	if result.TaskCreated == nil {
		t.Fatal("expected a task to be created")
	}
	if result.TaskCreated.TaskType != "trainer_release_decision" {
		t.Errorf("expected trainer_release_decision task, got %s", result.TaskCreated.TaskType)
	}

	// Verify task in store
	tasks, err := te.TaskStore.GetByAggregate(ctx, "booking", "b-trainer-1")
	if err != nil {
		t.Fatalf("get tasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// Trainer decides to release
	releaseResult, err := te.Engine.CompleteTask(ctx, tasks[0].ID, "release_to_public", "trainer-1")
	if err != nil {
		t.Fatalf("complete task (release) failed: %v", err)
	}
	if releaseResult.NewState != "CANCELLED" {
		t.Errorf("expected CANCELLED after release, got %s", releaseResult.NewState)
	}

	testutil.AssertState(t, te, "booking", "b-trainer-1", "CANCELLED")
}

func TestIntegration_BookingTrainerHold(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)

	ctx := context.Background()

	// Move to CONFIRMED, then trainer cancel, then hold
	_, err := te.Engine.Transition(ctx, "booking", "b-trainer-2", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}
	_, err = te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-trainer-2",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-system",
	})
	if err != nil {
		t.Fatalf("payment_succeeded failed: %v", err)
	}

	result, err := te.Engine.Transition(ctx, "booking", "b-trainer-2", "trainer_cancel_init", "trainer-1", map[string]any{
		"role": "trainer",
	})
	if err != nil {
		t.Fatalf("trainer_cancel_init failed: %v", err)
	}

	tasks, _ := te.TaskStore.GetByAggregate(ctx, "booking", "b-trainer-2")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// Trainer decides to hold
	holdResult, err := te.Engine.CompleteTask(ctx, tasks[0].ID, "hold_for_trainer", "trainer-1")
	if err != nil {
		t.Fatalf("complete task (hold) failed: %v", err)
	}
	if holdResult.NewState != "TRAINER_HOLD" {
		t.Errorf("expected TRAINER_HOLD, got %s", holdResult.NewState)
	}

	// Trainer can then confirm the booking back
	confirmResult, err := te.Engine.Transition(ctx, "booking", "b-trainer-2", "trainer_confirm", "trainer-1", nil)
	if err != nil {
		t.Fatalf("trainer_confirm failed: %v", err)
	}
	if confirmResult.NewState != "CONFIRMED" {
		t.Errorf("expected CONFIRMED after trainer_confirm, got %s", confirmResult.NewState)
	}

	testutil.AssertState(t, te, "booking", "b-trainer-2", "CONFIRMED")

	// Verify the full event chain
	testutil.AssertEventCount(t, te, "booking", "b-trainer-2", 5)
	testutil.AssertEventChain(t, te, result.Event.CorrelationID,
		"PaymentInitiated", "BookingConfirmed", "TrainerCancelInitiated",
		"BookingHeldForTrainer", "TrainerConfirmed")
}

func TestIntegration_BookingNoShow(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)

	ctx := context.Background()

	// Move to CONFIRMED
	_, err := te.Engine.Transition(ctx, "booking", "b-noshow", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}
	_, err = te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-noshow",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-system",
	})
	if err != nil {
		t.Fatalf("payment_succeeded failed: %v", err)
	}

	// Mark no-show
	result, err := te.Engine.Transition(ctx, "booking", "b-noshow", "mark_no_show", "admin-1", nil)
	if err != nil {
		t.Fatalf("mark_no_show failed: %v", err)
	}
	if result.NewState != "NO_SHOW" {
		t.Errorf("expected NO_SHOW, got %s", result.NewState)
	}
	if !result.IsTerminal {
		t.Error("NO_SHOW should be terminal")
	}

	testutil.AssertState(t, te, "booking", "b-noshow", "NO_SHOW")
}

func TestIntegration_BookingEventChainCorrelation(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	attendanceAct := &noopActivity{name: "log_attendance"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)
	te.ActivityRunner.Register(attendanceAct)

	ctx := context.Background()
	bookingTime := te.Clock.Now().Add(3 * time.Hour)

	// Walk through full happy path and verify correlation
	r1, _ := te.Engine.Transition(ctx, "booking", "b-corr", "initiate_payment", "user-1", nil)
	r2, _ := te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-corr",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-system",
	})
	r3, _ := te.Engine.Transition(ctx, "booking", "b-corr", "check_in", "user-1", map[string]any{
		"booking_time": bookingTime,
		"now":          bookingTime.Add(-10 * time.Minute),
	})

	// All events must share the same correlation ID
	corrID := r1.Event.CorrelationID
	if corrID == "" {
		t.Fatal("expected non-empty correlation ID")
	}
	if r2.Event.CorrelationID != corrID {
		t.Errorf("confirm event has different correlation: %s vs %s", r2.Event.CorrelationID, corrID)
	}
	if r3.Event.CorrelationID != corrID {
		t.Errorf("check_in event has different correlation: %s vs %s", r3.Event.CorrelationID, corrID)
	}

	// Query full event chain
	events, err := te.EventStore.ListByCorrelation(ctx, corrID)
	if err != nil {
		t.Fatalf("list by correlation failed: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events in chain, got %d", len(events))
	}

	expectedTypes := []string{"PaymentInitiated", "BookingConfirmed", "GuestCheckedIn"}
	for i, e := range events {
		if e.EventType != expectedTypes[i] {
			t.Errorf("event[%d]: expected %s, got %s", i, expectedTypes[i], e.EventType)
		}
	}
}

func TestIntegration_BookingMermaidExport(t *testing.T) {
	def := buildBookingWorkflow(t)

	mermaid := types.Mermaid(def)

	// Verify it starts with stateDiagram header
	if !strings.HasPrefix(mermaid, "stateDiagram-v2") {
		t.Error("expected Mermaid output to start with stateDiagram-v2")
	}

	// Verify key transitions are represented
	expectedEdges := []string{
		"PENDING_PAYMENT --> AWAITING_PAYMENT : initiate_payment",
		"AWAITING_PAYMENT --> CONFIRMED : confirm",
		"AWAITING_PAYMENT --> CANCELLED : payment_failed",
		"CONFIRMED --> CHECKED_IN : check_in",
		"CONFIRMED --> CANCELLED : cancel_user",
		"CONFIRMED --> AWAITING_TRAINER_DECISION : trainer_cancel_init",
		"AWAITING_TRAINER_DECISION --> CANCELLED : release_to_public",
		"AWAITING_TRAINER_DECISION --> TRAINER_HOLD : hold_for_trainer",
		"CONFIRMED --> NO_SHOW : mark_no_show",
	}

	for _, edge := range expectedEdges {
		if !strings.Contains(mermaid, edge) {
			t.Errorf("Mermaid output missing edge: %s", edge)
		}
	}

	// Verify terminal states exit
	if !strings.Contains(mermaid, "CANCELLED --> [*]") {
		t.Error("missing CANCELLED terminal exit")
	}
	if !strings.Contains(mermaid, "NO_SHOW --> [*]") {
		t.Error("missing NO_SHOW terminal exit")
	}
	if !strings.Contains(mermaid, "COMPLETED --> [*]") {
		t.Error("missing COMPLETED terminal exit")
	}

	// Verify initial state entry
	if !strings.Contains(mermaid, "[*] --> PENDING_PAYMENT") {
		t.Error("missing initial state entry")
	}
}

func TestIntegration_BookingAdminForceState(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	te.ActivityRunner.Register(paymentAct)

	ctx := context.Background()

	// Move to AWAITING_PAYMENT
	_, err := te.Engine.Transition(ctx, "booking", "b-force", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("initiate_payment failed: %v", err)
	}

	// Admin force-cancels the stuck payment
	result, err := te.Engine.ForceState(ctx, "booking", "b-force", "CANCELLED", "admin-1", "Payment system is down")
	if err != nil {
		t.Fatalf("force state failed: %v", err)
	}
	if result.NewState != "CANCELLED" {
		t.Errorf("expected CANCELLED, got %s", result.NewState)
	}
	if result.Event.EventType != "StateForced" {
		t.Errorf("expected StateForced event, got %s", result.Event.EventType)
	}

	testutil.AssertState(t, te, "booking", "b-force", "CANCELLED")
}

func TestIntegration_BookingMultipleConcurrentWorkflows(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := buildBookingWorkflow(t)
	te.Engine.Register(def)

	paymentAct := &noopActivity{name: "process_stripe_charge"}
	emailAct := &noopActivity{name: "send_confirmation_email"}
	lightingAct := &noopActivity{name: "schedule_lighting"}
	credsAct := &noopActivity{name: "generate_credentials"}
	te.ActivityRunner.Register(paymentAct)
	te.ActivityRunner.Register(emailAct)
	te.ActivityRunner.Register(lightingAct)
	te.ActivityRunner.Register(credsAct)

	ctx := context.Background()

	// Create multiple bookings in different states
	// b-1: PENDING_PAYMENT
	// b-2: AWAITING_PAYMENT
	// b-3: CONFIRMED
	// b-4: CANCELLED

	// b-2: move to AWAITING_PAYMENT
	_, err := te.Engine.Transition(ctx, "booking", "b-m2", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("b-m2 initiate_payment failed: %v", err)
	}

	// b-3: move to CONFIRMED
	_, err = te.Engine.Transition(ctx, "booking", "b-m3", "initiate_payment", "user-1", nil)
	if err != nil {
		t.Fatalf("b-m3 initiate_payment failed: %v", err)
	}
	_, err = te.Engine.Signal(ctx, types.SignalInput{
		TargetAggregateType: "booking",
		TargetAggregateID:   "b-m3",
		SignalName:          "payment_succeeded",
		ActorID:             "payment-system",
	})
	if err != nil {
		t.Fatalf("b-m3 payment_succeeded failed: %v", err)
	}

	// b-4: CANCELLED
	bookingTime := te.Clock.Now().Add(5 * time.Hour)
	_, err = te.Engine.Transition(ctx, "booking", "b-m4", "cancel_user", "user-1", map[string]any{
		"booking_time": bookingTime,
		"now":          te.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("b-m4 cancel failed: %v", err)
	}

	// Verify each workflow is in its expected state independently
	testutil.AssertState(t, te, "booking", "b-m2", "AWAITING_PAYMENT")
	testutil.AssertState(t, te, "booking", "b-m3", "CONFIRMED")
	testutil.AssertState(t, te, "booking", "b-m4", "CANCELLED")
}

// Interface compliance checks for integration test guards.
var (
	_ types.Guard = (*withinTimeWindowGuard)(nil)
	_ types.Guard = (*cancellationPolicyGuard)(nil)
	_ types.Guard = (*isProfessionalGuard)(nil)
)
