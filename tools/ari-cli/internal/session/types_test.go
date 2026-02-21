package session

import "testing"

func TestSessionValidate_ValidRunningSession(t *testing.T) {
	s := validSession()

	errList := s.Validate()
	if len(errList) != 0 {
		t.Fatalf("expected no errors, got %d", len(errList))
	}
}

func TestSessionValidate_RequiredFields(t *testing.T) {
	s := Session{}

	errList := s.Validate()
	if len(errList) != 9 {
		t.Fatalf("expected 9 errors, got %d", len(errList))
	}

	assertErrEq(t, errList[0], "session.session_id is required")
	assertErrEq(t, errList[1], "session.project_id is required")
	assertErrEq(t, errList[2], "session.project_path is required")
	assertErrEq(t, errList[3], "session.op_id is required")
	assertErrEq(t, errList[4], "session.status is required")
	assertErrEq(t, errList[5], "session.started_at is required")
	assertErrEq(t, errList[6], "session.updated_at is required")
	assertErrEq(t, errList[7], "session.agent is required")
	assertErrEq(t, errList[8], "session.goal is required")
}

func TestSessionValidate_InvalidStatus(t *testing.T) {
	s := validSession()
	s.Status = SessionStatus("invalid")

	errList := s.Validate()
	if len(errList) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errList))
	}

	assertErrEq(t, errList[0], "session.status must be one of: running, waiting_approval, completed, rejected, failed, killed")
}

func TestSessionValidate_EndedAtRules(t *testing.T) {
	t.Run("ended_at requires terminal status", func(t *testing.T) {
		s := validSession()
		s.EndedAt = "2026-02-22T10:30:00Z"

		errList := s.Validate()
		if len(errList) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errList))
		}

		assertErrEq(t, errList[0], "session.ended_at requires terminal status")
	})

	t.Run("terminal status requires ended_at", func(t *testing.T) {
		s := validSession()
		s.Status = SessionStatusCompleted

		errList := s.Validate()
		if len(errList) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errList))
		}

		assertErrEq(t, errList[0], "session.ended_at is required for terminal status")
	})
}

func TestSessionStatusCanTransitionTo(t *testing.T) {
	tests := []struct {
		name string
		from SessionStatus
		to   SessionStatus
		want bool
	}{
		{name: "running to waiting_approval", from: SessionStatusRunning, to: SessionStatusWaitingApproval, want: true},
		{name: "running to completed", from: SessionStatusRunning, to: SessionStatusCompleted, want: true},
		{name: "running to failed", from: SessionStatusRunning, to: SessionStatusFailed, want: true},
		{name: "running to killed", from: SessionStatusRunning, to: SessionStatusKilled, want: true},
		{name: "running to rejected", from: SessionStatusRunning, to: SessionStatusRejected, want: false},
		{name: "waiting_approval to running", from: SessionStatusWaitingApproval, to: SessionStatusRunning, want: true},
		{name: "waiting_approval to rejected", from: SessionStatusWaitingApproval, to: SessionStatusRejected, want: true},
		{name: "failed to running", from: SessionStatusFailed, to: SessionStatusRunning, want: true},
		{name: "failed to killed", from: SessionStatusFailed, to: SessionStatusKilled, want: true},
		{name: "failed to completed", from: SessionStatusFailed, to: SessionStatusCompleted, want: false},
		{name: "completed terminal", from: SessionStatusCompleted, to: SessionStatusRunning, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.from.CanTransitionTo(tc.to)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func validSession() Session {
	return Session{
		SessionID:   "sess-7f3a9b2c",
		ProjectID:   "proj-auth-service",
		ProjectPath: "/home/user/projects/auth-service",
		OpID:        "op-e8d4f1a2",
		Status:      SessionStatusRunning,
		CurrentStep: "step-004",
		StartedAt:   "2026-02-22T10:00:00Z",
		UpdatedAt:   "2026-02-22T10:05:00Z",
		Agent:       "theseus",
		Goal:        "Add rate limiting to authentication endpoints",
	}
}

func assertErrEq(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %q, got nil", want)
	}
	if err.Error() != want {
		t.Fatalf("expected error %q, got %q", want, err.Error())
	}
}
