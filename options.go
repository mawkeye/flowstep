package flowstate

// Option configures the Engine.
type Option func(*engineConfig)

type engineConfig struct {
	eventStore    EventStore
	instanceStore InstanceStore
	taskStore     TaskStore
	childStore    ChildStore
	activityStore ActivityStore
	txProvider    TxProvider
	eventBus      EventBus
	activityRunner ActivityRunner
	clock         Clock
	hooks         Hooks
}

// WithEventStore sets the event store.
func WithEventStore(s EventStore) Option {
	return func(c *engineConfig) { c.eventStore = s }
}

// WithInstanceStore sets the instance store.
func WithInstanceStore(s InstanceStore) Option {
	return func(c *engineConfig) { c.instanceStore = s }
}

// WithTaskStore sets the task store.
func WithTaskStore(s TaskStore) Option {
	return func(c *engineConfig) { c.taskStore = s }
}

// WithChildStore sets the child store.
func WithChildStore(s ChildStore) Option {
	return func(c *engineConfig) { c.childStore = s }
}

// WithActivityStore sets the activity store.
func WithActivityStore(s ActivityStore) Option {
	return func(c *engineConfig) { c.activityStore = s }
}

// WithTxProvider sets the transaction provider.
func WithTxProvider(p TxProvider) Option {
	return func(c *engineConfig) { c.txProvider = p }
}

// WithEventBus sets the event bus.
func WithEventBus(b EventBus) Option {
	return func(c *engineConfig) { c.eventBus = b }
}

// WithActivityRunner sets the activity runner.
func WithActivityRunner(r ActivityRunner) Option {
	return func(c *engineConfig) { c.activityRunner = r }
}

// WithClock sets the clock.
func WithClock(c Clock) Option {
	return func(cfg *engineConfig) { cfg.clock = c }
}

// WithHooks sets the hooks.
func WithHooks(h Hooks) Option {
	return func(c *engineConfig) { c.hooks = h }
}
