package slogadapter

import "log/slog"

// Option configures an Observer.
type Option func(*Observer)

// WithTransitionLevel overrides the log level for OnTransition events.
// Default: slog.LevelInfo.
func WithTransitionLevel(level slog.Level) Option {
	return func(o *Observer) { o.transitionLevel = level }
}

// WithGuardFailedLevel overrides the log level for OnGuardFailed events.
// Default: slog.LevelWarn.
func WithGuardFailedLevel(level slog.Level) Option {
	return func(o *Observer) { o.guardFailedLevel = level }
}

// WithActivityDispatchedLevel overrides the log level for OnActivityDispatched events.
// Default: slog.LevelInfo.
func WithActivityDispatchedLevel(level slog.Level) Option {
	return func(o *Observer) { o.activityDispatchedLevel = level }
}

// WithActivityCompletedLevel overrides the log level for OnActivityCompleted events.
// Default: slog.LevelInfo.
func WithActivityCompletedLevel(level slog.Level) Option {
	return func(o *Observer) { o.activityCompletedLevel = level }
}

// WithActivityFailedLevel overrides the log level for OnActivityFailed events.
// Default: slog.LevelError.
func WithActivityFailedLevel(level slog.Level) Option {
	return func(o *Observer) { o.activityFailedLevel = level }
}

// WithStuckLevel overrides the log level for OnStuck events.
// Default: slog.LevelError.
func WithStuckLevel(level slog.Level) Option {
	return func(o *Observer) { o.stuckLevel = level }
}

// WithPostCommitErrorLevel overrides the log level for OnPostCommitError events.
// Default: slog.LevelError.
func WithPostCommitErrorLevel(level slog.Level) Option {
	return func(o *Observer) { o.postCommitErrorLevel = level }
}
