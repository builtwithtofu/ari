package daemon

import (
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestDeliveryPolicyEngineRetryDelay(t *testing.T) {
	engine := NewDeliveryPolicyEngine()
	for _, tc := range []struct {
		name     string
		attempts int64
		policy   workspaceDeliveryPolicy
		want     time.Duration
	}{
		{name: "default linear preserves existing one-second-per-attempt cap", attempts: 3, policy: workspaceDeliveryPolicy{BackoffMode: workspaceDeliveryBackoffLinear}, want: 3 * time.Second},
		{name: "default linear caps at six seconds", attempts: 10, policy: workspaceDeliveryPolicy{BackoffMode: workspaceDeliveryBackoffLinear}, want: 6 * time.Second},
		{name: "fixed uses base", attempts: 4, policy: workspaceDeliveryPolicy{BackoffMode: workspaceDeliveryBackoffFixed, BackoffBaseMS: 250}, want: 250 * time.Millisecond},
		{name: "linear uses configured base and max", attempts: 4, policy: workspaceDeliveryPolicy{BackoffMode: workspaceDeliveryBackoffLinear, BackoffBaseMS: 250, BackoffMaxMS: 700}, want: 700 * time.Millisecond},
		{name: "exponential doubles from base", attempts: 4, policy: workspaceDeliveryPolicy{BackoffMode: workspaceDeliveryBackoffExponential, BackoffBaseMS: 100, BackoffMaxMS: 2_000}, want: 800 * time.Millisecond},
		{name: "exponential caps at max", attempts: 8, policy: workspaceDeliveryPolicy{BackoffMode: workspaceDeliveryBackoffExponential, BackoffBaseMS: 100, BackoffMaxMS: 500}, want: 500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := engine.RetryDelay(globaldb.PendingDelivery{Attempts: tc.attempts}, tc.policy)
			if got != tc.want {
				t.Fatalf("RetryDelay = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParseWorkspaceDeliveryPolicyValidatesAndDefaults(t *testing.T) {
	policy, err := parseWorkspaceDeliveryPolicy(`{"max_attempts":3,"backoff_mode":"exponential","backoff_base_ms":250,"backoff_max_ms":2000}`)
	if err != nil {
		t.Fatalf("parseWorkspaceDeliveryPolicy returned error: %v", err)
	}
	if policy.Channel != HarnessDeliveryVisiblePromptTurn || policy.MaxAttempts != 3 || policy.BackoffMode != workspaceDeliveryBackoffExponential || policy.BackoffBaseMS != 250 || policy.BackoffMaxMS != 2000 {
		t.Fatalf("policy = %#v, want defaults plus configured retry policy", policy)
	}

	for _, raw := range []string{
		`{"max_attempts":-1}`,
		`{"backoff_base_ms":-1}`,
		`{"backoff_max_ms":-1}`,
		`{"backoff_mode":"jitter"}`,
		`{"channel":"unsupported"}`,
		`{"channel":"not-a-channel"}`,
	} {
		if _, err := parseWorkspaceDeliveryPolicy(raw); err == nil {
			t.Fatalf("parseWorkspaceDeliveryPolicy(%s) returned nil error, want validation failure", raw)
		}
	}
}
