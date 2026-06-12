package daemon

import (
	"fmt"
	"strings"
)

type HarnessOption interface {
	harnessOption()
}

type HarnessInvocationMode string

const (
	HarnessInvocationModeHeadless   HarnessInvocationMode = "headless"
	HarnessInvocationModeBackground HarnessInvocationMode = "background"
	HarnessInvocationModeServer     HarnessInvocationMode = "server"
)

// invocationModeOption is the harness-neutral invocation mode request. Adapters
// consume it directly and the call envelope validates it against the adapter
// descriptor before Start is invoked.
type invocationModeOption struct {
	mode HarnessInvocationMode
}

func (invocationModeOption) harnessOption() {}

func WithInvocationMode(mode HarnessInvocationMode) HarnessOption {
	return invocationModeOption{mode: mode}
}

func requestedInvocationMode(options []HarnessOption) (HarnessInvocationMode, bool) {
	mode := HarnessInvocationMode("")
	found := false
	for _, option := range options {
		if typed, ok := option.(invocationModeOption); ok {
			mode = typed.mode
			found = true
		}
	}
	return mode, found
}

func harnessOptionsFromProfile(profile Profile) ([]HarnessOption, error) {
	harness := strings.TrimSpace(profile.Harness)
	switch harness {
	case HarnessNameClaude, HarnessNameCodex, HarnessNameOpenCode, HarnessNamePi, HarnessNameGrok:
		mode, ok, err := invocationModeFromSettings(profile.Defaults, harness)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return []HarnessOption{WithInvocationMode(mode)}, nil
	default:
		return nil, nil
	}
}

func invocationModeFromSettings(settings map[string]any, harness string) (HarnessInvocationMode, bool, error) {
	mode, modeSet, err := stringSetting(settings, "invocation_mode")
	if err != nil {
		return "", false, err
	}
	if native, ok, err := mapSetting(settings, harness); err != nil {
		return "", false, err
	} else if ok {
		if nativeMode, nativeOK, err := stringSetting(native, "invocation_mode"); err != nil {
			return "", false, err
		} else if nativeOK {
			mode = nativeMode
			modeSet = true
		}
	}
	if !modeSet || strings.TrimSpace(mode) == "" {
		return "", false, nil
	}
	switch HarnessInvocationMode(strings.TrimSpace(mode)) {
	case HarnessInvocationModeHeadless:
		return HarnessInvocationModeHeadless, true, nil
	case HarnessInvocationModeBackground:
		return HarnessInvocationModeBackground, true, nil
	case HarnessInvocationModeServer:
		return HarnessInvocationModeServer, true, nil
	default:
		return "", false, fmt.Errorf("unsupported invocation_mode %q", mode)
	}
}

func stringSetting(settings map[string]any, key string) (string, bool, error) {
	value, ok := settings[key]
	if !ok || value == nil {
		return "", false, nil
	}
	text, ok := value.(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(text), true, nil
}

func mapSetting(settings map[string]any, key string) (map[string]any, bool, error) {
	value, ok := settings[key]
	if !ok || value == nil {
		return nil, false, nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed, true, nil
	}
	return nil, false, fmt.Errorf("%s must be an object", key)
}
