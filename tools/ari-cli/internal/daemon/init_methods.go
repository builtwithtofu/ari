package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

const daemonOperationTypeInitApplied = "init_applied"

type InitStateRequest struct{}

type InitStateResponse struct {
	Initialized        bool   `json:"initialized"`
	DefaultHarness     string `json:"default_harness"`
	PreferredModel     string `json:"preferred_model"`
	DefaultRoot        string `json:"default_root"`
	HomeWorkspaceReady bool   `json:"home_workspace_ready"`
	HomeHelperReady    bool   `json:"home_helper_ready"`
}

type InitOptionsRequest struct{}

type InitHarnessOption struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type InitModelOption struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

type InitRootOption struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

type InitOptionsResponse struct {
	Harnesses []InitHarnessOption `json:"harnesses"`
	Models    []InitModelOption   `json:"models"`
	Roots     []InitRootOption    `json:"roots"`
}

type InitApplyRequest struct {
	Harness string `json:"harness"`
	Model   string `json:"model"`
	Root    string `json:"root"`
}

type InitApplyResponse struct {
	Initialized        bool   `json:"initialized"`
	DefaultHarness     string `json:"default_harness"`
	PreferredModel     string `json:"preferred_model"`
	DefaultRoot        string `json:"default_root"`
	DefaultHarnessSet  bool   `json:"default_harness_set"`
	HomeWorkspaceReady bool   `json:"home_workspace_ready"`
	HomeHelperReady    bool   `json:"home_helper_ready"`
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
	model, root, err := d.readConfiguredInitDefaults()
	if err != nil {
		return InitStateResponse{}, err
	}
	homeWorkspaceReady := false
	homeHelperReady := false
	if store != nil {
		homeWorkspaceReady, homeHelperReady, err = d.homeWorkspaceInitState(ctx, store, root)
		if err != nil {
			return InitStateResponse{}, err
		}
	}
	return InitStateResponse{Initialized: harness != "", DefaultHarness: harness, PreferredModel: model, DefaultRoot: root, HomeWorkspaceReady: homeWorkspaceReady, HomeHelperReady: homeHelperReady}, nil
}

func initOptions() InitOptionsResponse {
	names := SupportedHarnesses()
	options := make([]InitHarnessOption, 0, len(names))
	for _, name := range names {
		options = append(options, InitHarnessOption{Name: name, Label: name})
	}
	return InitOptionsResponse{Harnesses: options, Models: []InitModelOption{{Name: "", Label: "Manual/default model"}}, Roots: []InitRootOption{{Path: "~/", Label: "~/"}}}
}

func (d *Daemon) applyInit(ctx context.Context, store *globaldb.Store, req InitApplyRequest) (InitApplyResponse, error) {
	harness := strings.TrimSpace(req.Harness)
	if !isSupportedHarness(harness) {
		return InitApplyResponse{}, fmt.Errorf("init apply: harness must be one of %s", strings.Join(SupportedHarnesses(), ", "))
	}
	model := strings.TrimSpace(req.Model)
	root, err := normalizeInitRoot(req.Root)
	if err != nil {
		return InitApplyResponse{}, err
	}
	if store == nil {
		return InitApplyResponse{}, fmt.Errorf("globaldb store is required")
	}
	payload := map[string]string{"harness": harness, "model": model, "root": root, "step": "init.apply"}
	rollbackData := map[string]string{"scope": "ari_owned_state_only"}
	previousConfig, err := readJSONConfig(d.configPath)
	if err != nil {
		return InitApplyResponse{}, err
	}
	previousDefaultHarness, err := readJSONConfigString(previousConfig, "default_harness")
	if err != nil {
		return InitApplyResponse{}, err
	}
	previousPreferredModel, err := readJSONConfigString(previousConfig, "preferred_model")
	if err != nil {
		return InitApplyResponse{}, err
	}
	previousDefaultRoot, err := readJSONConfigString(previousConfig, "default_workspace_root")
	if err != nil {
		return InitApplyResponse{}, err
	}
	previousContext, err := readActiveWorkspaceContext(ctx, store)
	if err != nil {
		return InitApplyResponse{}, err
	}
	payload["previous_workspace_id"] = previousContext.WorkspaceID
	payload["previous_default_harness"] = previousDefaultHarness
	payload["previous_preferred_model"] = previousPreferredModel
	payload["previous_default_workspace_root"] = previousDefaultRoot
	rollbackData["previous_workspace_id"] = previousContext.WorkspaceID
	rollbackData["previous_default_harness"] = previousDefaultHarness
	rollbackData["previous_preferred_model"] = previousPreferredModel
	rollbackData["previous_default_workspace_root"] = previousDefaultRoot
	checkpoint, err := createDaemonOperationCheckpoint(ctx, store, daemonOperationCheckpointOptions{Actor: "user", Source: daemonOperationSourceDaemon, Scope: globaldb.OperationScopeGlobal, RequestSummary: "apply Ari init choices", PayloadSnapshot: payload})
	if err != nil {
		return InitApplyResponse{}, err
	}

	var response InitApplyResponse
	_, err = recordDaemonOperation(ctx, store, daemonOperationRecordOptions{OperationType: daemonOperationTypeInitApplied, Actor: "user", Source: daemonOperationSourceDaemon, Scope: globaldb.OperationScopeGlobal, RequestSummary: "apply Ari init choices", ParentOperationID: checkpoint.OperationID, CheckpointOperationID: checkpoint.OperationID, RollbackPointID: checkpoint.OperationID, RollbackData: rollbackData, PayloadSnapshot: payload}, func(ctx context.Context) error {
		if err := patchJSONConfigStrings(d.configPath, map[string]string{"default_harness": harness, "preferred_model": model, "default_workspace_root": root}); err != nil {
			return err
		}
		home, homeCreated, err := d.ensureHomeWorkspace(ctx, store, root)
		if err != nil {
			return err
		}
		homeHelperReady := false
		if home != nil {
			payload["home_workspace_id"] = home.ID
			payload["home_workspace_created"] = fmt.Sprintf("%t", homeCreated)
			rollbackData["home_workspace_id"] = home.ID
			rollbackData["home_workspace_created"] = fmt.Sprintf("%t", homeCreated)
			if err := d.ensureHomeHelperSession(ctx, store, home.ID, harness); err != nil {
				return err
			}
			if _, err := setActiveWorkspaceContext(ctx, store, ContextSetRequest{WorkspaceID: home.ID}); err != nil {
				return err
			}
			if err := patchJSONConfigStrings(d.configPath, map[string]string{"active_workspace": home.ID}); err != nil {
				return err
			}
			homeHelperReady = true
		}
		response = InitApplyResponse{Initialized: true, DefaultHarness: harness, PreferredModel: model, DefaultRoot: root, DefaultHarnessSet: true, HomeWorkspaceReady: home != nil, HomeHelperReady: homeHelperReady}
		return nil
	})
	if err != nil {
		return InitApplyResponse{}, err
	}
	return response, nil
}

func (d *Daemon) homeWorkspaceInitState(ctx context.Context, store *globaldb.Store, root string) (bool, bool, error) {
	root, err := normalizeInitRoot(root)
	if err != nil || strings.TrimSpace(root) == "" {
		return false, false, nil
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return false, false, err
	}
	for _, session := range sessions {
		if session.OriginRoot == root {
			profile, helperErr := store.GetDefaultHelperProfile(ctx, session.ID)
			if helperErr != nil {
				return true, false, nil
			}
			helpers, err := store.ListAgentSessions(ctx, session.ID)
			if err != nil {
				return false, false, err
			}
			return true, hasLiveHelperSession(helpers, profile.ProfileID), nil
		}
	}
	return false, false, nil
}

func (d *Daemon) ensureHomeHelperSession(ctx context.Context, store *globaldb.Store, workspaceID, harness string) error {
	profile, err := store.EnsureDefaultHelperProfile(ctx, workspaceID, harness, helperPrompt())
	if err != nil {
		return err
	}
	sessions, err := store.ListAgentSessions(ctx, workspaceID)
	if err != nil {
		return err
	}
	if hasLiveHelperSession(sessions, profile.ProfileID) {
		return nil
	}
	_, err = startProfileSession(d, ctx, store, AgentSessionStartRequest{WorkspaceID: workspaceID, Profile: globaldb.DefaultHelperProfileName})
	return err
}

func hasLiveHelperSession(sessions []globaldb.AgentSession, profileID string) bool {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return false
	}
	for _, session := range sessions {
		if strings.TrimSpace(session.AgentID) == profileID && strings.TrimSpace(session.Status) == "running" {
			return true
		}
	}
	return false
}

func (d *Daemon) ensureHomeWorkspace(ctx context.Context, store *globaldb.Store, root string) (*globaldb.Session, bool, error) {
	root, err := normalizeInitRoot(root)
	if err != nil || strings.TrimSpace(root) == "" {
		return nil, false, nil
	}
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, session := range sessions {
		if session.OriginRoot == root {
			return &session, false, nil
		}
	}
	workspaceID, err := newWorkspaceID()
	if err != nil {
		return nil, false, fmt.Errorf("generate workspace id: %w", err)
	}
	name := availableHomeWorkspaceName(sessions)
	if err := store.CreateSession(ctx, workspaceID, name, root, "manual", "auto"); err != nil {
		return nil, false, err
	}
	if err := store.AddFolder(ctx, workspaceID, root, "unknown", true); err != nil {
		_ = store.DeleteSession(ctx, workspaceID)
		return nil, false, err
	}
	session, err := store.GetSession(ctx, workspaceID)
	return session, true, err
}

func availableHomeWorkspaceName(sessions []globaldb.Session) string {
	used := map[string]bool{}
	for _, session := range sessions {
		used[session.Name] = true
	}
	if !used["home"] {
		return "home"
	}
	for index := 2; ; index++ {
		name := fmt.Sprintf("home-%d", index)
		if !used[name] {
			return name
		}
	}
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

func (d *Daemon) readConfiguredInitDefaults() (string, string, error) {
	values, err := readJSONConfig(d.configPath)
	if err != nil {
		return "", "", err
	}
	model, err := readJSONConfigString(values, "preferred_model")
	if err != nil {
		return "", "", fmt.Errorf("read init state: parse preferred_model: %w", err)
	}
	root, err := readJSONConfigString(values, "default_workspace_root")
	if err != nil {
		return "", "", fmt.Errorf("read init state: parse default_workspace_root: %w", err)
	}
	root, err = normalizeInitRoot(root)
	if err != nil {
		return "", "", fmt.Errorf("read init state: normalize default_workspace_root: %w", err)
	}
	return strings.TrimSpace(model), root, nil
}

func readJSONConfigString(values map[string]json.RawMessage, key string) (string, error) {
	var value string
	if raw, ok := values[key]; ok {
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(value), nil
}

func normalizeInitRoot(root string) (string, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" || trimmed == "~/" || trimmed == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("init apply: resolve home root: %w", err)
		}
		trimmed = home
	} else if strings.HasPrefix(trimmed, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("init apply: resolve home root: %w", err)
		}
		trimmed = filepath.Join(home, trimmed[2:])
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("init apply: resolve root: %w", err)
	}
	return abs, nil
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
