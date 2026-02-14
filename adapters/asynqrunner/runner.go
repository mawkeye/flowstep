// Package asynqrunner provides an ActivityRunner implementation using the asynq
// task queue. Activity invocations are enqueued as asynq tasks for background processing.
package asynqrunner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/mawkeye/flowstate/types"
)

const taskTypePrefix = "flowstate:activity:"

// Runner implements flowstate.ActivityRunner using asynq.
type Runner struct {
	client *asynq.Client
}

// New creates a new asynq-backed ActivityRunner.
func New(client *asynq.Client) *Runner {
	return &Runner{client: client}
}

// Dispatch enqueues an activity invocation as an asynq task.
func (r *Runner) Dispatch(_ context.Context, invocation types.ActivityInvocation) error {
	payload, err := json.Marshal(invocation)
	if err != nil {
		return fmt.Errorf("asynqrunner: marshal invocation: %w", err)
	}

	task := asynq.NewTask(taskTypePrefix+invocation.ActivityName, payload)

	var opts []asynq.Option
	if invocation.Timeout > 0 {
		opts = append(opts, asynq.Timeout(invocation.Timeout))
	}
	if invocation.RetryPolicy != nil && invocation.RetryPolicy.MaxAttempts > 0 {
		opts = append(opts, asynq.MaxRetry(invocation.RetryPolicy.MaxAttempts))
	}

	_, err = r.client.Enqueue(task, opts...)
	if err != nil {
		return fmt.Errorf("asynqrunner: enqueue: %w", err)
	}
	return nil
}

// HandleFunc returns an asynq.HandlerFunc for processing activity tasks.
// Register this with your asynq.ServeMux:
//
//	mux.HandleFunc(asynqrunner.TaskType("send_email"), asynqrunner.HandleFunc(sendEmailActivity))
func HandleFunc(activity interface {
	Execute(ctx context.Context, input types.ActivityInput) (*types.ActivityResult, error)
}) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, task *asynq.Task) error {
		var invocation types.ActivityInvocation
		if err := json.Unmarshal(task.Payload(), &invocation); err != nil {
			return fmt.Errorf("asynqrunner: unmarshal invocation: %w", err)
		}
		_, err := activity.Execute(ctx, invocation.Input)
		return err
	}
}

// TaskType returns the asynq task type string for a given activity name.
func TaskType(activityName string) string {
	return taskTypePrefix + activityName
}
