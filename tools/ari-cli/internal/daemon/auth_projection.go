package daemon

import (
	"os"
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
