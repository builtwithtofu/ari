package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type State string

const (
	StatePlanningInProgress  State = "planning_in_progress"
	StatePlanningNeedsInput  State = "planning_needs_input"
	StatePlanningReady       State = "planning_ready"
	StateExecuting           State = "executing"
	StatePaused              State = "paused"
	StateBlockedWaitingHuman State = "blocked_waiting_human"
	StateCompleted           State = "completed"
)

type CollaborationMode string

const (
	ModeSupervised CollaborationMode = "supervised"
	ModeCheckpoint CollaborationMode = "checkpoint"
	ModeAgentic    CollaborationMode = "agentic"
)

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

var (
	ErrApprovalRequired = errors.New("approval required for medium/high risk execution")
	ErrPlanNotExecuting = errors.New("plan is not in executable state")
)

type SessionPolicy struct {
	SessionID   string            `json:"session_id"`
	StreamID    string            `json:"stream_id"`
	Mode        CollaborationMode `json:"mode"`
	Risk        RiskLevel         `json:"risk"`
	Scope       string            `json:"scope"`
	State       State             `json:"state"`
	NextCommand string            `json:"next_command"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	Path        string            `json:"-"`
}

type StartPlanInput struct {
	SessionID string
	StreamID  string
	Mode      CollaborationMode
	Risk      RiskLevel
	Scope     string
}

func DefaultSessionID() string {
	return fmt.Sprintf("session-%s", time.Now().UTC().Format("20060102"))
}

func StartPlan(repoRoot string, input StartPlanInput) (SessionPolicy, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = DefaultSessionID()
	}

	streamID := strings.TrimSpace(input.StreamID)
	if streamID == "" {
		streamID = "default"
	}

	mode := input.Mode
	if mode == "" {
		mode = ModeSupervised
	}

	risk := input.Risk
	if risk == "" {
		risk = RiskLow
	}

	scope := strings.TrimSpace(input.Scope)
	now := time.Now().UTC().Format(time.RFC3339)
	policy := SessionPolicy{
		SessionID: sessionID,
		StreamID:  streamID,
		Mode:      mode,
		Risk:      risk,
		Scope:     scope,
		State:     StatePlanningInProgress,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if scope == "" {
		policy.State = StatePlanningNeedsInput
		policy.NextCommand = fmt.Sprintf("ari plan start --session %s --scope \"<scope>\"", sessionID)
	} else {
		policy.State = StatePlanningReady
		policy.NextCommand = fmt.Sprintf("ari plan execute --session %s", sessionID)
	}

	return savePolicy(repoRoot, policy)
}

func ExecutePlan(repoRoot, sessionID string, approved bool) (SessionPolicy, error) {
	policy, err := LoadPolicy(repoRoot, sessionID)
	if err != nil {
		return SessionPolicy{}, err
	}

	if policy.State == StatePlanningNeedsInput {
		return policy, ErrPlanNotExecuting
	}

	if policy.State != StatePlanningReady && policy.State != StatePaused && policy.State != StateBlockedWaitingHuman {
		return policy, ErrPlanNotExecuting
	}

	if (policy.Risk == RiskMedium || policy.Risk == RiskHigh) && !approved {
		policy.State = StateBlockedWaitingHuman
		policy.NextCommand = fmt.Sprintf("ari plan execute --session %s --approve", policy.SessionID)
		policy.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		saved, saveErr := savePolicy(repoRoot, policy)
		if saveErr != nil {
			return SessionPolicy{}, saveErr
		}
		return saved, ErrApprovalRequired
	}

	policy.State = StateExecuting
	policy.NextCommand = fmt.Sprintf("ari work continue --session %s", policy.SessionID)
	policy.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return savePolicy(repoRoot, policy)
}

func ContinueWork(repoRoot, sessionID string) (SessionPolicy, error) {
	policy, err := LoadPolicy(repoRoot, sessionID)
	if err != nil {
		return SessionPolicy{}, err
	}

	if policy.State != StateExecuting && policy.State != StatePaused {
		return policy, ErrPlanNotExecuting
	}

	policy.State = StateExecuting
	policy.NextCommand = fmt.Sprintf("ari status --session %s", policy.SessionID)
	policy.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return savePolicy(repoRoot, policy)
}

func LoadPolicy(repoRoot, sessionID string) (SessionPolicy, error) {
	path := policyPath(repoRoot, sessionID)
	raw, err := os.ReadFile(path)
	if err != nil {
		return SessionPolicy{}, err
	}

	var policy SessionPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return SessionPolicy{}, err
	}

	policy.Path = path
	return policy, nil
}

func savePolicy(repoRoot string, policy SessionPolicy) (SessionPolicy, error) {
	if policy.SessionID == "" {
		return SessionPolicy{}, errors.New("session id is required")
	}

	path := policyPath(repoRoot, policy.SessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return SessionPolicy{}, err
	}

	if policy.CreatedAt == "" {
		policy.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	policy.Path = path
	payload, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return SessionPolicy{}, err
	}

	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return SessionPolicy{}, err
	}

	return policy, nil
}

func policyPath(repoRoot, sessionID string) string {
	return filepath.Join(repoRoot, ".gaia", "lifecycle", fmt.Sprintf("%s.json", sessionID))
}
