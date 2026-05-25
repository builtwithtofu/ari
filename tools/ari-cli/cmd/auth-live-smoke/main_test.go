package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestMinimalEnvAllowlistFiltersHostEnv(t *testing.T) {
	t.Setenv("OPENCODE_AUTH_CONTENT", "secret")
	t.Setenv("ARI_AUTH_LIVE_SMOKE_TOKEN", "secret-token")
	for _, kv := range minimalEnv() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "PATH", "TERM", "NIX_SSL_CERT_FILE", "SSL_CERT_FILE", "USER", "LOGNAME":
		default:
			t.Fatalf("minimalEnv leaked %q", key)
		}
	}
}

func TestRedactRedactsTokenPatterns(t *testing.T) {
	t.Setenv("ARI_AUTH_LIVE_SMOKE_TOKEN", "exact-secret")
	cases := []struct {
		input string
		want  string
	}{
		{`access_token=abc123&state=xyz`, `access_token=[redacted]&state=[redacted]`},
		{`Bearer sk-ant-test123`, `Bearer [redacted]`},
		{`code: abcd-efgh-ijkl`, `code: [redacted]`},
		{`token exact-secret`, `token [redacted]`},
	}
	for _, tc := range cases {
		if got := redact(tc.input); got != tc.want {
			t.Fatalf("redact(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestHasAuthInitiationSignalPatterns(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"Visit https://auth.example.test/start", true},
		{"http://localhost:8080/callback", true},
		{"user code: abcd-efgh", true},
		{"provider auth methods observed", false},
	}
	for _, tc := range cases {
		if got := hasAuthInitiationSignal(strings.ToLower(tc.input)); got != tc.want {
			t.Fatalf("hasAuthInitiationSignal(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestClassifyDistinguishesFailedUnsupportedAndExercised(t *testing.T) {
	cases := []struct {
		name       string
		out        string
		err        error
		wantStatus string
	}{
		{"empty error", "", fmt.Errorf("boom"), "failed"},
		{"partial error", "partial output", fmt.Errorf("boom"), "failed"},
		{"explicit unsupported", "provider methods observed", unsupportedError{message: "not implemented"}, "unsupported"},
		{"empty unsupported", "", unsupportedError{message: "not implemented"}, "unsupported"},
		{"url signal", "See https://auth.example.test/start", nil, "exercised"},
		{"no signal", "provider methods observed", nil, "unsupported"},
	}
	for _, tc := range cases {
		got := classify("opencode", "pathway", tc.out, tc.err)
		if got.status != tc.wantStatus {
			t.Fatalf("%s: classify status = %q, want %q", tc.name, got.status, tc.wantStatus)
		}
	}
}

func TestClaudeStatusAuthenticatedParsesNegatives(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{`{"authenticated":true}`, true},
		{`{"authenticated":false}`, false},
		{"not authenticated", false},
		{"Authenticated", true},
	}
	for _, tc := range cases {
		if got := claudeStatusAuthenticated(tc.input); got != tc.want {
			t.Fatalf("claudeStatusAuthenticated(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
