package flow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adavies/opencode-gaia/tools/gaia-cli/internal/lifecycle"
)

type Command string

const (
	CommandStart    Command = "start"
	CommandIterate  Command = "iterate"
	CommandExecute  Command = "execute"
	CommandContinue Command = "continue"
)

var (
	ErrSessionRequired       = errors.New("session id is required")
	ErrIterateNotAllowed     = errors.New("flow iterate is not allowed while plan is executing or completed")
	ErrExecuteStateNotReady  = errors.New("flow execute requires a planning_ready, paused, or blocked_waiting_human state")
	ErrContinueStateNotReady = errors.New("flow continue requires an executing or paused state")
)

type StartInput struct {
	SessionID string
	StreamID  string
	Mode      lifecycle.CollaborationMode
	Risk      lifecycle.RiskLevel
	Scope     string
}

type IterateInput struct {
	SessionID string
	Scope     string
	Note      string
}

type ExecuteInput struct {
	SessionID string
	Approve   bool
}

type FlowSnapshot struct {
	SessionID     string                      `json:"session_id"`
	StreamID      string                      `json:"stream_id"`
	Mode          lifecycle.CollaborationMode `json:"mode"`
	Risk          lifecycle.RiskLevel         `json:"risk"`
	Scope         string                      `json:"scope"`
	State         lifecycle.State             `json:"state"`
	Iteration     int                         `json:"iteration"`
	LastSequence  int                         `json:"last_sequence"`
	LastCommand   Command                     `json:"last_command"`
	LastUpdatedAt string                      `json:"last_updated_at"`
	LifecyclePath string                      `json:"lifecycle_path"`
	FlowPath      string                      `json:"flow_path"`
	EventLogPath  string                      `json:"event_log_path"`
}

type TransitionResult struct {
	Policy      lifecycle.SessionPolicy `json:"policy"`
	Flow        FlowSnapshot            `json:"flow"`
	NextCommand string                  `json:"next_command"`
}

type flowEvent struct {
	Sequence  int             `json:"sequence"`
	Timestamp string          `json:"timestamp"`
	Command   Command         `json:"command"`
	State     lifecycle.State `json:"state"`
	Scope     string          `json:"scope,omitempty"`
	Note      string          `json:"note,omitempty"`
}

func Start(repoRoot string, input StartInput) (TransitionResult, error) {
	policy, err := lifecycle.StartPlan(repoRoot, lifecycle.StartPlanInput{
		SessionID: input.SessionID,
		StreamID:  input.StreamID,
		Mode:      input.Mode,
		Risk:      input.Risk,
		Scope:     input.Scope,
	})
	if err != nil {
		return TransitionResult{}, err
	}

	flowState, err := persistTransition(repoRoot, policy, CommandStart, persistOptions{
		ResetIteration: true,
		Note:           "planning session started",
	})
	if err != nil {
		return TransitionResult{}, err
	}

	return TransitionResult{
		Policy:      policy,
		Flow:        flowState,
		NextCommand: nextCommand(policy),
	}, nil
}

func Iterate(repoRoot string, input IterateInput) (TransitionResult, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return TransitionResult{}, ErrSessionRequired
	}

	basePolicy, err := lifecycle.LoadPolicy(repoRoot, sessionID)
	if err != nil {
		return TransitionResult{}, err
	}

	if basePolicy.State == lifecycle.StateExecuting || basePolicy.State == lifecycle.StateCompleted {
		return TransitionResult{}, ErrIterateNotAllowed
	}

	scope := strings.TrimSpace(input.Scope)
	if scope == "" {
		scope = strings.TrimSpace(basePolicy.Scope)
	}

	policy, err := lifecycle.StartPlan(repoRoot, lifecycle.StartPlanInput{
		SessionID: sessionID,
		StreamID:  basePolicy.StreamID,
		Mode:      basePolicy.Mode,
		Risk:      basePolicy.Risk,
		Scope:     scope,
	})
	if err != nil {
		return TransitionResult{}, err
	}

	flowState, err := persistTransition(repoRoot, policy, CommandIterate, persistOptions{
		IncrementIteration: true,
		Note:               input.Note,
	})
	if err != nil {
		return TransitionResult{}, err
	}

	return TransitionResult{
		Policy:      policy,
		Flow:        flowState,
		NextCommand: nextCommand(policy),
	}, nil
}

func Execute(repoRoot string, input ExecuteInput) (TransitionResult, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		return TransitionResult{}, ErrSessionRequired
	}

	policy, err := lifecycle.ExecutePlan(repoRoot, sessionID, input.Approve)
	if err != nil {
		if !errors.Is(err, lifecycle.ErrApprovalRequired) && !errors.Is(err, lifecycle.ErrPlanNotExecuting) {
			return TransitionResult{}, err
		}
	}

	if policy.State != lifecycle.StateExecuting &&
		policy.State != lifecycle.StateBlockedWaitingHuman &&
		policy.State != lifecycle.StatePaused {
		if errors.Is(err, lifecycle.ErrPlanNotExecuting) {
			return TransitionResult{}, ErrExecuteStateNotReady
		}
	}

	flowState, persistErr := persistTransition(repoRoot, policy, CommandExecute, persistOptions{})
	if persistErr != nil {
		return TransitionResult{}, persistErr
	}

	result := TransitionResult{
		Policy:      policy,
		Flow:        flowState,
		NextCommand: nextCommand(policy),
	}

	if err != nil {
		return result, err
	}

	return result, nil
}

func Continue(repoRoot, sessionID string) (TransitionResult, error) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return TransitionResult{}, ErrSessionRequired
	}

	policy, err := lifecycle.ContinueWork(repoRoot, trimmed)
	if err != nil {
		if errors.Is(err, lifecycle.ErrPlanNotExecuting) {
			return TransitionResult{}, ErrContinueStateNotReady
		}

		return TransitionResult{}, err
	}

	flowState, persistErr := persistTransition(repoRoot, policy, CommandContinue, persistOptions{})
	if persistErr != nil {
		return TransitionResult{}, persistErr
	}

	return TransitionResult{
		Policy:      policy,
		Flow:        flowState,
		NextCommand: nextCommand(policy),
	}, nil
}

type persistOptions struct {
	ResetIteration     bool
	IncrementIteration bool
	Note               string
}

func persistTransition(repoRoot string, policy lifecycle.SessionPolicy, command Command, options persistOptions) (FlowSnapshot, error) {
	statePath := flowStatePath(repoRoot, policy.SessionID)
	eventPath := flowEventPath(repoRoot, policy.SessionID)

	existing, exists, err := readFlowSnapshot(statePath)
	if err != nil {
		return FlowSnapshot{}, err
	}

	iteration := 0
	if exists {
		iteration = existing.Iteration
	}

	if options.ResetIteration {
		iteration = 0
	}

	if options.IncrementIteration {
		iteration += 1
	}

	lastSequence := 1
	if exists {
		lastSequence = existing.LastSequence + 1
	}

	now := time.Now().UTC().Format(time.RFC3339)
	flowState := FlowSnapshot{
		SessionID:     policy.SessionID,
		StreamID:      policy.StreamID,
		Mode:          policy.Mode,
		Risk:          policy.Risk,
		Scope:         policy.Scope,
		State:         policy.State,
		Iteration:     iteration,
		LastSequence:  lastSequence,
		LastCommand:   command,
		LastUpdatedAt: now,
		LifecyclePath: policy.Path,
		FlowPath:      statePath,
		EventLogPath:  eventPath,
	}

	if err := writeFlowSnapshot(statePath, flowState); err != nil {
		return FlowSnapshot{}, err
	}

	event := flowEvent{
		Sequence:  lastSequence,
		Timestamp: now,
		Command:   command,
		State:     policy.State,
		Scope:     strings.TrimSpace(policy.Scope),
		Note:      strings.TrimSpace(options.Note),
	}

	if err := appendFlowEvent(eventPath, event); err != nil {
		return FlowSnapshot{}, err
	}

	return flowState, nil
}

func readFlowSnapshot(path string) (FlowSnapshot, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FlowSnapshot{}, false, nil
		}

		return FlowSnapshot{}, false, err
	}

	var parsed FlowSnapshot
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return FlowSnapshot{}, false, err
	}

	return parsed, true, nil
}

func writeFlowSnapshot(path string, payload FlowSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func appendFlowEvent(path string, event flowEvent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}

	return nil
}

func flowStatePath(repoRoot, sessionID string) string {
	return filepath.Join(repoRoot, ".gaia", "flows", fmt.Sprintf("%s.json", sessionID))
}

func flowEventPath(repoRoot, sessionID string) string {
	return filepath.Join(repoRoot, ".gaia", "flows", fmt.Sprintf("%s.events.ndjson", sessionID))
}

func nextCommand(policy lifecycle.SessionPolicy) string {
	sessionID := policy.SessionID

	switch policy.State {
	case lifecycle.StatePlanningNeedsInput, lifecycle.StatePlanningInProgress:
		return fmt.Sprintf("ari flow iterate --session %s --scope \"<scope>\"", sessionID)
	case lifecycle.StatePlanningReady:
		return fmt.Sprintf("ari flow execute --session %s", sessionID)
	case lifecycle.StateBlockedWaitingHuman:
		return fmt.Sprintf("ari flow execute --session %s --approve", sessionID)
	case lifecycle.StateExecuting, lifecycle.StatePaused:
		return fmt.Sprintf("ari flow continue --session %s", sessionID)
	case lifecycle.StateCompleted:
		return fmt.Sprintf("ari query all --session %s --json", sessionID)
	default:
		return fmt.Sprintf("ari query lifecycle --session %s --json", sessionID)
	}
}
