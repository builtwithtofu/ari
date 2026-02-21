// Package agent defines the agent state types and constants.
package agent

// State represents the current state of the agent.
type State string

const (
	StateIdle     State = "idle"
	StateThinking State = "thinking"
	StateActing   State = "acting"
	StateWaiting  State = "waiting"
)

// IsValidState returns true if the state is valid.
func IsValidState(s State) bool {
	switch s {
	case StateIdle, StateThinking, StateActing, StateWaiting:
		return true
	default:
		return false
	}
}
