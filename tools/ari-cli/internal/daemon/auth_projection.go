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
