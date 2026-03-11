package builder

import (
	"fmt"

	"github.com/mawkeye/flowstep/types"
)

// transitionBuilder collects options for a single transition.
type transitionBuilder struct {
	name       string
	sources    []string
	target     string
	event      string
	guards     []types.Guard
	condition  types.Condition
	isDefault  bool
	routes     []types.Route
	activities []types.ActivityDef
	taskDef    *types.TaskDef
	childDef   *types.ChildDef
	childrenDef *types.ChildrenDef
	triggerType types.TriggerType
	triggerKey  string
	allowSelf  bool
}

// TransitionOption configures a transition.
type TransitionOption func(*transitionBuilder)

// From sets the source state(s) for a transition.
func From(states ...string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.sources = append(tb.sources, states...)
	}
}

// To sets the target state for a transition.
func To(state string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.target = state
	}
}

// Event sets the event type emitted by this transition.
func Event(eventType string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.event = eventType
	}
}

// Guards adds guard checks to the transition.
func Guards(guards ...types.Guard) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.guards = append(tb.guards, guards...)
	}
}

// OnSignal marks this transition as signal-triggered.
func OnSignal(signalName string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.triggerType = types.TriggerSignal
		tb.triggerKey = signalName
	}
}

// OnTaskCompleted marks this transition as task-completion-triggered.
func OnTaskCompleted(taskType string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.triggerType = types.TriggerTaskCompleted
		tb.triggerKey = taskType
	}
}

// OnChildCompleted marks this transition as child-completion-triggered.
func OnChildCompleted(childWorkflowType string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.triggerType = types.TriggerChildCompleted
		tb.triggerKey = childWorkflowType
	}
}

// OnChildrenJoined marks this transition as children-joined-triggered.
func OnChildrenJoined() TransitionOption {
	return func(tb *transitionBuilder) {
		tb.triggerType = types.TriggerChildrenJoined
	}
}

// OnTimeout marks this transition as timeout-triggered.
func OnTimeout() TransitionOption {
	return func(tb *transitionBuilder) {
		tb.triggerType = types.TriggerTimeout
	}
}

// EmitTask creates a pending task when the transition fires.
func EmitTask(taskDef types.TaskDef) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.taskDef = &taskDef
	}
}

// SpawnChild spawns a child workflow when the transition fires.
func SpawnChild(childDef types.ChildDef) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.childDef = &childDef
	}
}

// SpawnChildren spawns parallel child workflows when the transition fires.
func SpawnChildren(childrenDef types.ChildrenDef) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.childrenDef = &childrenDef
	}
}

// AllowSelfTransition permits a transition from a state to itself.
func AllowSelfTransition() TransitionOption {
	return func(tb *transitionBuilder) {
		tb.allowSelf = true
	}
}

// Route adds a conditional route to the transition.
// Usage: Route(When(cond), To("STATE")) or Route(Default(), To("STATE"))
func Route(opts ...TransitionOption) TransitionOption {
	return func(tb *transitionBuilder) {
		sub := &transitionBuilder{}
		for _, opt := range opts {
			opt(sub)
		}
		route := types.Route{
			Target:    sub.target,
			Condition: sub.condition,
			IsDefault: sub.isDefault,
		}
		tb.routes = append(tb.routes, route)
	}
}

// When sets a condition for route evaluation.
func When(cond types.Condition) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.condition = cond
	}
}

// Default marks a route as the default fallback.
func Default() TransitionOption {
	return func(tb *transitionBuilder) {
		tb.isDefault = true
	}
}

// Dispatch adds a fire-and-forget activity to the transition.
func Dispatch(name string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.activities = append(tb.activities, types.ActivityDef{
			Name: name,
			Mode: types.FireAndForget,
		})
	}
}

// DispatchAndWait adds an await-result activity to the transition.
func DispatchAndWait(name string) TransitionOption {
	return func(tb *transitionBuilder) {
		tb.activities = append(tb.activities, types.ActivityDef{
			Name: name,
			Mode: types.AwaitResult,
		})
	}
}

// StateModifier is a functional option that configures a state's properties.
// Used with State(), WaitState(), Terminal(), and CompoundState().
type StateModifier func(*types.StateDef)

// Parent sets the parent compound state for a child state.
func Parent(parentName string) StateModifier {
	return func(sd *types.StateDef) {
		sd.Parent = parentName
	}
}

// InitialChild sets the initial child state for a compound state.
func InitialChild(childName string) StateModifier {
	return func(sd *types.StateDef) {
		sd.InitialChild = childName
	}
}

// EntryActivityOpt sets the activity to execute synchronously when entering this state.
func EntryActivityOpt(name string) StateModifier {
	return func(sd *types.StateDef) {
		sd.EntryActivity = name
	}
}

// ExitActivityOpt sets the activity to execute synchronously when exiting this state.
func ExitActivityOpt(name string) StateModifier {
	return func(sd *types.StateDef) {
		sd.ExitActivity = name
	}
}

// StateOption configures a state.
type StateOption struct {
	def types.StateDef
}

// Def returns the StateDef for this state option.
func (so StateOption) Def() types.StateDef {
	return so.def
}

// Initial creates an initial state. Does not accept StateModifier — the
// top-level initial state must be a root-level leaf.
func Initial(name string) StateOption {
	return StateOption{def: types.StateDef{Name: name, IsInitial: true}}
}

// Terminal creates a terminal state.
func Terminal(name string, mods ...StateModifier) StateOption {
	sd := types.StateDef{Name: name, IsTerminal: true}
	for _, m := range mods {
		m(&sd)
	}
	return StateOption{def: sd}
}

// State creates an intermediate state.
func State(name string, mods ...StateModifier) StateOption {
	sd := types.StateDef{Name: name}
	for _, m := range mods {
		m(&sd)
	}
	return StateOption{def: sd}
}

// WaitState creates a wait state (for pending tasks).
func WaitState(name string, mods ...StateModifier) StateOption {
	sd := types.StateDef{Name: name, IsWait: true}
	for _, m := range mods {
		m(&sd)
	}
	return StateOption{def: sd}
}

// CompoundState creates a compound state (a state that contains child states).
// It auto-sets IsCompound = true. Use InitialChild() to specify which child
// state the workflow enters when this compound state is entered.
func CompoundState(name string, mods ...StateModifier) StateOption {
	sd := types.StateDef{Name: name, IsCompound: true}
	for _, m := range mods {
		m(&sd)
	}
	return StateOption{def: sd}
}

// ValidateFn is called by Build() to validate the assembled Definition.
// The root package provides this function to inject sentinel errors.
type ValidateFn func(def *types.Definition) error

// DefBuilder builds a workflow Definition.
type DefBuilder struct {
	aggregateType string
	workflowType  string
	version       int
	states        []StateOption
	transitions   []struct {
		name string
		opts []TransitionOption
	}
	validateFn       ValidateFn
	errDupTransition error
}

// New creates a new DefBuilder.
func New(aggregateType, workflowType string, validateFn ValidateFn, errDupTransition error) *DefBuilder {
	return &DefBuilder{
		aggregateType:    aggregateType,
		workflowType:     workflowType,
		version:          1,
		validateFn:       validateFn,
		errDupTransition: errDupTransition,
	}
}

// Version sets the workflow version.
func (b *DefBuilder) Version(v int) *DefBuilder {
	b.version = v
	return b
}

// States adds states to the workflow.
func (b *DefBuilder) States(states ...StateOption) *DefBuilder {
	b.states = append(b.states, states...)
	return b
}

// Transition adds a named transition to the workflow.
func (b *DefBuilder) Transition(name string, opts ...TransitionOption) *DefBuilder {
	b.transitions = append(b.transitions, struct {
		name string
		opts []TransitionOption
	}{name: name, opts: opts})
	return b
}

// Build validates and returns an immutable Definition.
func (b *DefBuilder) Build() (*types.Definition, error) {
	def := &types.Definition{
		AggregateType: b.aggregateType,
		WorkflowType:  b.workflowType,
		Version:       b.version,
		States:        make(map[string]types.StateDef),
		Transitions:   make(map[string]types.TransitionDef),
	}

	// Collect states
	for _, so := range b.states {
		def.States[so.def.Name] = so.def
		if so.def.IsInitial {
			def.InitialState = so.def.Name
		}
		if so.def.IsTerminal {
			def.TerminalStates = append(def.TerminalStates, so.def.Name)
		}
	}

	// Wire Children from Parent references and auto-set IsCompound.
	// Iterate all states: for each state with a Parent, append it to its parent's Children.
	for name, st := range def.States {
		if st.Parent == "" {
			continue
		}
		parent, ok := def.States[st.Parent]
		if !ok {
			continue // validation will catch orphaned children
		}
		parent.Children = append(parent.Children, name)
		parent.IsCompound = true
		def.States[st.Parent] = parent
	}

	// Collect transitions (detect duplicates)
	for _, t := range b.transitions {
		if _, exists := def.Transitions[t.name]; exists {
			return nil, fmt.Errorf("flowstep: duplicate transition %q: %w", t.name, b.errDupTransition)
		}

		tb := &transitionBuilder{name: t.name, triggerType: types.TriggerDirect}
		for _, opt := range t.opts {
			opt(tb)
		}

		td := types.TransitionDef{
			Name:                tb.name,
			Sources:             tb.sources,
			Target:              tb.target,
			Event:               tb.event,
			Guards:              tb.guards,
			Routes:              tb.routes,
			Activities:          tb.activities,
			TaskDef:             tb.taskDef,
			ChildDef:            tb.childDef,
			ChildrenDef:         tb.childrenDef,
			TriggerType:         tb.triggerType,
			TriggerKey:          tb.triggerKey,
			AllowSelfTransition: tb.allowSelf,
		}
		def.Transitions[tb.name] = td
	}

	// Run validation
	if b.validateFn != nil {
		if err := b.validateFn(def); err != nil {
			return nil, err
		}
	}

	return def, nil
}
