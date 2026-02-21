package session

import "fmt"

type SessionStatus string

const (
	SessionStatusRunning         SessionStatus = "running"
	SessionStatusWaitingApproval SessionStatus = "waiting_approval"
	SessionStatusCompleted       SessionStatus = "completed"
	SessionStatusRejected        SessionStatus = "rejected"
	SessionStatusFailed          SessionStatus = "failed"
	SessionStatusKilled          SessionStatus = "killed"
)

type Session struct {
	SessionID   string        `json:"session_id"`
	ProjectID   string        `json:"project_id"`
	ProjectPath string        `json:"project_path"`
	OpID        string        `json:"op_id"`
	Status      SessionStatus `json:"status"`
	CurrentStep string        `json:"current_step,omitempty"`
	StartedAt   string        `json:"started_at"`
	UpdatedAt   string        `json:"updated_at"`
	EndedAt     string        `json:"ended_at,omitempty"`
	Agent       string        `json:"agent"`
	Goal        string        `json:"goal"`
}

func (s Session) Validate() []error {
	errList := make([]error, 0)

	if s.SessionID == "" {
		errList = append(errList, fmt.Errorf("session.session_id is required"))
	}
	if s.ProjectID == "" {
		errList = append(errList, fmt.Errorf("session.project_id is required"))
	}
	if s.ProjectPath == "" {
		errList = append(errList, fmt.Errorf("session.project_path is required"))
	}
	if s.OpID == "" {
		errList = append(errList, fmt.Errorf("session.op_id is required"))
	}
	if s.Status == "" {
		errList = append(errList, fmt.Errorf("session.status is required"))
	}
	if s.StartedAt == "" {
		errList = append(errList, fmt.Errorf("session.started_at is required"))
	}
	if s.UpdatedAt == "" {
		errList = append(errList, fmt.Errorf("session.updated_at is required"))
	}
	if s.Agent == "" {
		errList = append(errList, fmt.Errorf("session.agent is required"))
	}
	if s.Goal == "" {
		errList = append(errList, fmt.Errorf("session.goal is required"))
	}

	if s.Status != "" && !isValidStatus(s.Status) {
		errList = append(errList, fmt.Errorf("session.status must be one of: running, waiting_approval, completed, rejected, failed, killed"))
	}

	if s.EndedAt != "" && !s.Status.IsTerminal() {
		errList = append(errList, fmt.Errorf("session.ended_at requires terminal status"))
	}

	if s.Status.IsTerminal() && s.EndedAt == "" {
		errList = append(errList, fmt.Errorf("session.ended_at is required for terminal status"))
	}

	return errList
}

func (s SessionStatus) IsTerminal() bool {
	switch s {
	case SessionStatusCompleted, SessionStatusRejected, SessionStatusKilled:
		return true
	default:
		return false
	}
}

func (s SessionStatus) CanTransitionTo(next SessionStatus) bool {
	switch s {
	case SessionStatusRunning:
		return next == SessionStatusWaitingApproval ||
			next == SessionStatusCompleted ||
			next == SessionStatusFailed ||
			next == SessionStatusKilled
	case SessionStatusWaitingApproval:
		return next == SessionStatusRunning ||
			next == SessionStatusRejected
	case SessionStatusFailed:
		return next == SessionStatusRunning ||
			next == SessionStatusKilled
	case SessionStatusCompleted, SessionStatusRejected, SessionStatusKilled:
		return false
	default:
		return false
	}
}

func isValidStatus(status SessionStatus) bool {
	switch status {
	case SessionStatusRunning,
		SessionStatusWaitingApproval,
		SessionStatusCompleted,
		SessionStatusRejected,
		SessionStatusFailed,
		SessionStatusKilled:
		return true
	default:
		return false
	}
}
