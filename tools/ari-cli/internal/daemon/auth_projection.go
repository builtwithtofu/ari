package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type HarnessAuthProjectionOwner string

const (
	HarnessAuthProjectionOwnerNative HarnessAuthProjectionOwner = "native"
	HarnessAuthProjectionOwnerAri    HarnessAuthProjectionOwner = "ari"
)

type HarnessAuthProjectionKind string

const (
	HarnessAuthProjectionNone        HarnessAuthProjectionKind = "none"
	HarnessAuthProjectionConfigRoot  HarnessAuthProjectionKind = "config_root"
	HarnessAuthProjectionEnv         HarnessAuthProjectionKind = "env"
	HarnessAuthProjectionHelper      HarnessAuthProjectionKind = "helper"
	HarnessAuthProjectionStdin       HarnessAuthProjectionKind = "stdin"
	HarnessAuthProjectionFile        HarnessAuthProjectionKind = "file"
	HarnessAuthProjectionAuthContent HarnessAuthProjectionKind = "auth_content"
	HarnessAuthProjectionUnsupported HarnessAuthProjectionKind = "unsupported"
)

type HarnessAuthProjectionPlan struct {
	Owner      HarnessAuthProjectionOwner `json:"owner,omitempty"`
	Kind       HarnessAuthProjectionKind  `json:"kind,omitempty"`
	Env        map[string]string          `json:"-"`
	RiskLabels []string                   `json:"risk_labels,omitempty"`
}

type NativeAuthSlotProjectionRequest struct {
	Harness    string
	AuthSlotID string
	EnvKey     string
	Root       string
	RiskLabels []string
}

// ResolveNativeAuthSlotProjection maps a non-default named auth slot to the
// harness-native config root environment variable used by providers that own
// their credential state (Claude, Codex, Grok). Ari creates only an isolated
// per-slot config root and does not read provider credentials.
func ResolveNativeAuthSlotProjection(current HarnessAuthProjectionPlan, req NativeAuthSlotProjectionRequest) (HarnessAuthProjectionPlan, error) {
	harness := strings.TrimSpace(req.Harness)
	authSlotID := strings.TrimSpace(req.AuthSlotID)
	envKey := strings.TrimSpace(req.EnvKey)
	if authSlotIsDefaultForHarness(harness, authSlotID) {
		return current, nil
	}
	if current.Kind == HarnessAuthProjectionConfigRoot && strings.TrimSpace(current.Env[envKey]) != "" {
		return current, nil
	}
	home, err := harnessAuthSlotHome(harness, authSlotID, req.Root)
	if err != nil {
		return HarnessAuthProjectionPlan{}, err
	}
	riskLabels := append([]string(nil), req.RiskLabels...)
	if len(riskLabels) == 0 {
		riskLabels = []string{"provider_owned", "native_config_root_isolation"}
	}
	return HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerNative, Kind: HarnessAuthProjectionConfigRoot, Env: map[string]string{envKey: home}, RiskLabels: riskLabels}, nil
}

func RequireProjectedAuthSlot(harness, authSlotID string, projection HarnessAuthProjectionPlan, ready func(HarnessAuthProjectionPlan) bool) error {
	if authSlotIsDefaultForHarness(harness, authSlotID) || ready(projection) {
		return nil
	}
	return &HarnessUnavailableError{Harness: strings.TrimSpace(harness), Reason: "auth_slot_projection_required", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
}

// harnessAuthSlotHome resolves (and creates) the per-slot config root used to
// isolate a named auth slot's provider state. rootOverride replaces the
// default `<user-config>/ari/auth-slots/<harness>` root when set.
func harnessAuthSlotHome(harness, authSlotID, rootOverride string) (string, error) {
	harness = strings.TrimSpace(harness)
	root := strings.TrimSpace(rootOverride)
	if root == "" {
		configRoot, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve %s auth slot home root: %w", harness, err)
		}
		root = filepath.Join(configRoot, "ari", "auth-slots", harness)
	}
	safeSlotID := safeAuthSlotPathComponent(authSlotID)
	if safeSlotID == "" {
		return "", fmt.Errorf("%s auth slot id is required", harness)
	}
	home := filepath.Join(root, safeSlotID)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", fmt.Errorf("create %s auth slot home: %w", harness, err)
	}
	return home, nil
}

func commandEnvWithProjection(projection HarnessAuthProjectionPlan) []string {
	if len(projection.Env) == 0 {
		return nil
	}
	merged := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			merged[key] = value
		}
	}
	for key, value := range projection.Env {
		key = strings.TrimSpace(key)
		if key != "" {
			merged[key] = value
		}
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+merged[key])
	}
	return env
}
