package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type InitStateRequest struct{}

type InitStateResponse struct {
	Initialized          bool   `json:"initialized"`
	DefaultHarness       string `json:"default_harness"`
	SystemWorkspaceReady bool   `json:"system_workspace_ready"`
	SystemHelperReady    bool   `json:"system_helper_ready"`
}

type InitOptionsRequest struct{}

type InitHarnessOption struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type InitOptionsResponse struct {
	Harnesses []InitHarnessOption `json:"harnesses"`
}

type InitApplyRequest struct {
	Harness string `json:"harness"`
}

type InitApplyResponse struct {
	Initialized          bool   `json:"initialized"`
	DefaultHarness       string `json:"default_harness"`
	DefaultHarnessSet    bool   `json:"default_harness_set"`
	SystemWorkspaceReady bool   `json:"system_workspace_ready"`
	SystemHelperReady    bool   `json:"system_helper_ready"`
}

func (d *Daemon) registerInitMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[InitStateRequest, InitStateResponse]{
		Name:        "init.state",
		Description: "Report onboarding initialization state",
		Handler: func(ctx context.Context, req InitStateRequest) (InitStateResponse, error) {
			_ = req
			return d.initState(ctx, store)
		},
	}); err != nil {
		return fmt.Errorf("register init.state: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[InitOptionsRequest, InitOptionsResponse]{
		Name:        "init.options",
		Description: "List onboarding options",
		Handler: func(ctx context.Context, req InitOptionsRequest) (InitOptionsResponse, error) {
			_ = ctx
			_ = req
			return initOptions(), nil
		},
	}); err != nil {
		return fmt.Errorf("register init.options: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[InitApplyRequest, InitApplyResponse]{
		Name:        "init.apply",
		Description: "Apply onboarding choices",
		Handler: func(ctx context.Context, req InitApplyRequest) (InitApplyResponse, error) {
			return d.applyInit(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register init.apply: %w", err)
	}
	return nil
}

func (d *Daemon) initState(ctx context.Context, store *globaldb.Store) (InitStateResponse, error) {
	harness, err := d.readConfiguredDefaultHarness()
	if err != nil {
		return InitStateResponse{}, err
	}
	workspaceReady := false
	helperReady := false
	if store != nil {
		workspaceReady, err = store.HasSystemWorkspace(ctx)
		if err != nil {
			return InitStateResponse{}, err
		}
		if workspaceReady {
			system, err := store.EnsureSystemWorkspace(ctx, "init-state-system")
			if err != nil {
				return InitStateResponse{}, err
			}
			if _, err := store.GetDefaultHelperProfile(ctx, system.ID); err == nil {
				helperReady = true
			} else if !errors.Is(err, globaldb.ErrNotFound) {
				return InitStateResponse{}, err
			}
		}
	}
	return InitStateResponse{Initialized: harness != "" && workspaceReady && helperReady, DefaultHarness: harness, SystemWorkspaceReady: workspaceReady, SystemHelperReady: helperReady}, nil
}

func initOptions() InitOptionsResponse {
	names := SupportedHarnesses()
	options := make([]InitHarnessOption, 0, len(names))
	for _, name := range names {
		options = append(options, InitHarnessOption{Name: name, Label: name})
	}
	return InitOptionsResponse{Harnesses: options}
}

func (d *Daemon) applyInit(ctx context.Context, store *globaldb.Store, req InitApplyRequest) (InitApplyResponse, error) {
	harness := strings.TrimSpace(req.Harness)
	if !isSupportedHarness(harness) {
		return InitApplyResponse{}, fmt.Errorf("init apply: harness must be one of %s", strings.Join(SupportedHarnesses(), ", "))
	}
	if store == nil {
		return InitApplyResponse{}, fmt.Errorf("globaldb store is required")
	}
	workspaceID, err := newWorkspaceID()
	if err != nil {
		return InitApplyResponse{}, fmt.Errorf("generate workspace id: %w", err)
	}
	system, err := store.EnsureSystemWorkspace(ctx, workspaceID)
	if err != nil {
		return InitApplyResponse{}, err
	}
	if _, err := store.EnsureDefaultHelperProfile(ctx, system.ID, harness, systemHelperPrompt()); err != nil {
		return InitApplyResponse{}, err
	}
	if err := patchJSONConfigString(d.configPath, "default_harness", harness); err != nil {
		return InitApplyResponse{}, err
	}
	return InitApplyResponse{Initialized: true, DefaultHarness: harness, DefaultHarnessSet: true, SystemWorkspaceReady: true, SystemHelperReady: true}, nil
}

func (d *Daemon) readConfiguredDefaultHarness() (string, error) {
	values, err := readJSONConfig(d.configPath)
	if err != nil {
		return "", err
	}
	var harness string
	if raw, ok := values["default_harness"]; ok {
		if err := json.Unmarshal(raw, &harness); err != nil {
			return "", fmt.Errorf("read init state: parse default_harness: %w", err)
		}
	}
	harness = strings.TrimSpace(harness)
	if harness == "" {
		return "", nil
	}
	if !isSupportedHarness(harness) {
		return "", fmt.Errorf("read init state: unsupported default_harness %q", harness)
	}
	return harness, nil
}

func patchJSONConfigString(path, key, value string) error {
	return patchJSONConfigStrings(path, map[string]string{key: value})
}

func patchJSONConfigStrings(path string, updates map[string]string) error {
	values, err := readJSONConfig(path)
	if err != nil {
		return err
	}
	for key, value := range updates {
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("write init config: key is required")
		}
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			delete(values, key)
			continue
		}
		raw, err := json.Marshal(trimmed)
		if err != nil {
			return fmt.Errorf("write init config: marshal %s: %w", key, err)
		}
		values[key] = raw
	}
	encoded, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return fmt.Errorf("write init config: marshal config: %w", err)
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("write init config: mkdir config dir: %w", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return fmt.Errorf("write init config: write file: %w", err)
	}
	return nil
}

func readJSONConfig(path string) (map[string]json.RawMessage, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("config path is required")
	}
	values := map[string]json.RawMessage{}
	body, err := os.ReadFile(path)
	if err == nil {
		if len(body) == 0 {
			return values, nil
		}
		if err := json.Unmarshal(body, &values); err != nil {
			return nil, fmt.Errorf("read init config: parse config: %w", err)
		}
		return values, nil
	}
	if os.IsNotExist(err) {
		return values, nil
	}
	return nil, fmt.Errorf("read init config: %w", err)
}

func isSupportedHarness(harness string) bool {
	for _, name := range SupportedHarnesses() {
		if harness == name {
			return true
		}
	}
	return false
}
