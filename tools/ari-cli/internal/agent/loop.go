// Package agent implements the agent state machine and loop logic.
package agent

// Loop represents the agent state machine.
type Loop struct {
	// State management
	currentState State
	// Provider interface for LLM calls
	provider Provider
	// World manager for persistence
	worldManager WorldManager
	// Event emitter for protocol output
	emitter EventEmitter
}


// Provider interface for LLM completion.
// This will be implemented by the provider package.
type Provider interface {
	// TODO: Add provider interface methods
}

// WorldManager interface for world persistence.
// This will be implemented by the world package.
type WorldManager interface {
	// TODO: Add world manager interface methods
}

// EventEmitter interface for protocol events.
// This will be implemented by the protocol package.
type EventEmitter interface {
	// TODO: Add event emitter interface methods
}

// NewLoop creates a new agent loop.
func NewLoop(provider Provider, worldManager WorldManager, emitter EventEmitter) *Loop {
	return &Loop{
		currentState: StateIdle,
		provider:     provider,
		worldManager: worldManager,
		emitter:      emitter,
	}
}

// Start begins the agent loop.
func (l *Loop) Start() error {
	// TODO: Implement agent loop logic
	return nil
}

// Transition changes the agent state.
func (l *Loop) Transition(to State) {
	l.currentState = to
	// TODO: Emit state change event
}
