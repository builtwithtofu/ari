package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type Profile struct {
	ProfileID       string                 `json:"profile_id,omitempty"`
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

type HarnessSessionDefaults struct {
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Settings        map[string]any         `json:"settings,omitempty"`
}

type ProfileRunRequest struct {
	Profile           string                 `json:"profile,omitempty"`
	Executor          string                 `json:"executor,omitempty"`
	ProfileDefinition *Profile               `json:"profile_definition,omitempty"`
	Defaults          HarnessSessionDefaults `json:"defaults,omitempty"`
	Packet            ContextPacket          `json:"packet"`
}

type ProfileRunResponse struct {
	Profile string         `json:"profile"`
	Harness string         `json:"harness"`
	Run     HarnessSession `json:"run"`
	Items   []TimelineItem `json:"items"`
}

type ProfileCreateRequest struct {
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

type ProfileGetRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	Name        string `json:"name"`
}

type ProfileListRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type ProfileListResponse struct {
	Profiles []ProfileResponse `json:"profiles"`
}

type DefaultHelperEnsureRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Harness     string `json:"harness,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
}

type DefaultHelperGetRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type ProfileResponse struct {
	ProfileID       string                 `json:"profile_id"`
	WorkspaceID     string                 `json:"workspace_id,omitempty"`
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	AuthSlotID      string                 `json:"auth_slot_id,omitempty"`
	AuthPool        HarnessAuthPool        `json:"auth_pool,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
	Defaults        map[string]any         `json:"defaults,omitempty"`
}

func defaultProfiles() map[string]Profile {
	return make(map[string]Profile)
}

func (d *Daemon) setProfileForTest(profile Profile) {
	if d == nil {
		return
	}
	if d.agentProfiles == nil {
		d.agentProfiles = make(map[string]Profile)
	}
	d.agentProfiles[strings.TrimSpace(profile.Name)] = profile
}

func agentSessionStartUsesProfile(req HarnessSessionStartRequest) bool {
	return strings.TrimSpace(req.Profile) != "" || req.ProfileDefinition != nil || agentSessionDefaultsSet(req.Defaults)
}

func agentSessionDefaultsSet(defaults HarnessSessionDefaults) bool {
	return strings.TrimSpace(defaults.Harness) != "" || strings.TrimSpace(defaults.Model) != "" || strings.TrimSpace(defaults.Prompt) != "" || strings.TrimSpace(defaults.AuthSlotID) != "" || len(defaults.AuthPool.SlotIDs) > 0 || defaults.InvocationClass != "" || len(defaults.Settings) > 0
}

func createStoredProfile(ctx context.Context, store *globaldb.Store, req ProfileCreateRequest) (ProfileResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile name is required", map[string]any{"reason": "missing_profile_name"})
	}
	if req.InvocationClass != "" && req.InvocationClass != HarnessInvocationSticky && req.InvocationClass != HarnessInvocationEphemeral {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "invocation class is invalid", map[string]any{"reason": "invalid_invocation_class"})
	}
	if key, ok := profileDefaultsForbiddenKey(req.Defaults); ok {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile defaults include a forbidden key", map[string]any{"reason": "forbidden_default_key", "key": key})
	}
	profileID, err := newAriULID()
	if err != nil {
		return ProfileResponse{}, err
	}
	defaultsJSON := "{}"
	if len(req.Defaults) > 0 {
		encoded, err := json.Marshal(req.Defaults)
		if err != nil {
			return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile defaults are invalid", map[string]any{"reason": "invalid_defaults"})
		}
		defaultsJSON = string(encoded)
	}
	authPoolJSON := "{}"
	if len(req.AuthPool.SlotIDs) > 0 || req.AuthPool.Strategy != "" {
		encoded, err := json.Marshal(req.AuthPool)
		if err != nil {
			return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile auth pool is invalid", map[string]any{"reason": "invalid_auth_pool"})
		}
		authPoolJSON = string(encoded)
	}
	stored := globaldb.Profile{ProfileID: "ap_" + profileID, WorkspaceID: strings.TrimSpace(req.WorkspaceID), Name: name, Harness: strings.TrimSpace(req.Harness), Model: strings.TrimSpace(req.Model), Prompt: strings.TrimSpace(req.Prompt), AuthSlotID: strings.TrimSpace(req.AuthSlotID), AuthPoolJSON: authPoolJSON, InvocationClass: string(req.InvocationClass), DefaultsJSON: defaultsJSON}
	if err := store.UpsertProfile(ctx, stored); err != nil {
		return ProfileResponse{}, err
	}
	persisted, err := store.GetProfile(ctx, stored.WorkspaceID, stored.Name)
	if err != nil {
		return ProfileResponse{}, err
	}
	return agentProfileResponseFromStore(persisted, req.Defaults), nil
}

func getStoredProfile(ctx context.Context, store *globaldb.Store, req ProfileGetRequest) (ProfileResponse, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "profile name is required", map[string]any{"reason": "missing_profile_name"})
	}
	stored, err := store.GetProfile(ctx, req.WorkspaceID, name)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ProfileResponse{}, unknownProfileError(name)
		}
		return ProfileResponse{}, err
	}
	defaults, err := decodeStoredDefaults(stored.DefaultsJSON)
	if err != nil {
		return ProfileResponse{}, err
	}
	return agentProfileResponseFromStore(stored, defaults), nil
}

func listStoredProfiles(ctx context.Context, store *globaldb.Store, req ProfileListRequest) (ProfileListResponse, error) {
	stored, err := store.ListProfiles(ctx, req.WorkspaceID)
	if err != nil {
		return ProfileListResponse{}, err
	}
	profiles := make([]ProfileResponse, 0, len(stored))
	for _, profile := range stored {
		defaults, err := decodeStoredDefaults(profile.DefaultsJSON)
		if err != nil {
			return ProfileListResponse{}, err
		}
		profiles = append(profiles, agentProfileResponseFromStore(profile, defaults))
	}
	return ProfileListResponse{Profiles: profiles}, nil
}

func agentProfileResponseFromStore(profile globaldb.Profile, defaults map[string]any) ProfileResponse {
	authPool := decodeStoredAuthPool(profile.AuthPoolJSON)
	return ProfileResponse{ProfileID: profile.ProfileID, WorkspaceID: profile.WorkspaceID, Name: profile.Name, Harness: profile.Harness, Model: profile.Model, Prompt: profile.Prompt, AuthSlotID: profile.AuthSlotID, AuthPool: authPool, InvocationClass: HarnessInvocationClass(profile.InvocationClass), Defaults: defaults}
}

func decodeStoredAuthPool(raw string) HarnessAuthPool {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return HarnessAuthPool{}
	}
	var pool HarnessAuthPool
	_ = json.Unmarshal([]byte(raw), &pool)
	return pool
}

func profileDefaultsForbiddenKey(defaults map[string]any) (string, bool) {
	for key, value := range defaults {
		if isForbiddenProfileDefaultKey(key) {
			return key, true
		}
		if nested, ok := profileDefaultValueForbiddenKey(value); ok {
			return key + "." + nested, true
		}
	}
	return "", false
}

func profileDefaultValueForbiddenKey(value any) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return profileDefaultsForbiddenKey(typed)
	case []any:
		for _, item := range typed {
			if key, ok := profileDefaultValueForbiddenKey(item); ok {
				return key, true
			}
		}
	}
	return "", false
}

func isForbiddenProfileDefaultKey(key string) bool {
	normalized := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, key))
	if normalized == "" {
		return false
	}
	for _, marker := range []string{"apikey", "bearer", "secret", "password"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return normalized == "token" || strings.HasSuffix(normalized, "token")
}

func ensureDefaultHelperProfile(ctx context.Context, store *globaldb.Store, req DefaultHelperEnsureRequest) (ProfileResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = helperPrompt()
	}
	stored, err := store.EnsureDefaultHelperProfile(ctx, workspaceID, req.Harness, prompt)
	if err != nil {
		return ProfileResponse{}, err
	}
	return agentProfileResponseFromStore(stored, map[string]any{}), nil
}

func getDefaultHelperProfile(ctx context.Context, store *globaldb.Store, req DefaultHelperGetRequest) (ProfileResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	stored, err := store.GetDefaultHelperProfile(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return ProfileResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "default helper profile is not set up for this workspace", map[string]any{"reason": "helper_setup_required", "workspace_id": workspaceID})
		}
		return ProfileResponse{}, err
	}
	return agentProfileResponseFromStore(stored, map[string]any{}), nil
}

func (d *Daemon) resolveProfileRunRequest(ctx context.Context, store *globaldb.Store, req ProfileRunRequest) (Profile, error) {
	name := strings.TrimSpace(req.Profile)
	if name != "" && req.ProfileDefinition != nil {
		return Profile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile input is ambiguous", map[string]any{"profile": name, "profile_definition": strings.TrimSpace(req.ProfileDefinition.Name), "reason": "ambiguous_profile", "start_invoked": false})
	}
	var profile Profile
	if req.ProfileDefinition != nil {
		profile = *req.ProfileDefinition
		if strings.TrimSpace(profile.Name) == "" {
			profile.Name = name
		}
	} else if name != "" {
		resolved, err := d.resolveProfile(ctx, store, req.Packet.WorkspaceID, name)
		if err != nil {
			return Profile{}, err
		}
		profile = resolved
	} else if executor := strings.TrimSpace(req.Executor); executor != "" {
		profile = Profile{Name: executor, Harness: executor}
	}
	profile = applyHarnessSessionDefaults(profile, req.Defaults)
	if strings.TrimSpace(profile.Harness) == "" {
		return Profile{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is required", map[string]any{"profile": strings.TrimSpace(profile.Name), "reason": "missing_harness", "start_invoked": false})
	}
	return profile, nil
}

func applyHarnessSessionDefaults(profile Profile, defaults HarnessSessionDefaults) Profile {
	if strings.TrimSpace(profile.Harness) == "" {
		profile.Harness = strings.TrimSpace(defaults.Harness)
	}
	if strings.TrimSpace(profile.Model) == "" {
		profile.Model = strings.TrimSpace(defaults.Model)
	}
	if strings.TrimSpace(profile.Prompt) == "" {
		profile.Prompt = strings.TrimSpace(defaults.Prompt)
	}
	if strings.TrimSpace(profile.AuthSlotID) == "" {
		profile.AuthSlotID = strings.TrimSpace(defaults.AuthSlotID)
	}
	if len(profile.AuthPool.SlotIDs) == 0 {
		profile.AuthPool = defaults.AuthPool
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = defaults.InvocationClass
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = HarnessInvocationSticky
	}
	profile.Defaults = mergeSettings(profile.Defaults, defaults.Settings)
	return profile
}

func mergeSettings(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	merged := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range override {
		merged[key] = value
	}
	return merged
}

func decodeStoredDefaults(raw string) (map[string]any, error) {
	defaults := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &defaults); err != nil {
			return nil, fmt.Errorf("decode profile defaults: %w", err)
		}
	}
	return defaults, nil
}

func (d *Daemon) resolveProfile(ctx context.Context, store *globaldb.Store, workspaceID, name string) (Profile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile is required", map[string]any{"reason": "missing_profile", "start_invoked": false})
	}
	if d == nil || d.agentProfiles == nil {
		return resolveStoredProfile(ctx, store, workspaceID, name)
	}
	profile, ok := d.agentProfiles[name]
	if !ok {
		return resolveStoredProfile(ctx, store, workspaceID, name)
	}
	return profile, nil
}

func resolveStoredProfile(ctx context.Context, store *globaldb.Store, workspaceID, name string) (Profile, error) {
	if store == nil {
		return Profile{}, unknownProfileError(name)
	}
	stored, err := store.GetProfile(ctx, workspaceID, name)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return Profile{}, unknownProfileError(name)
		}
		return Profile{}, err
	}
	defaults, err := decodeStoredDefaults(stored.DefaultsJSON)
	if err != nil {
		return Profile{}, err
	}
	return Profile{ProfileID: stored.ProfileID, WorkspaceID: stored.WorkspaceID, Name: stored.Name, Harness: stored.Harness, Model: stored.Model, Prompt: stored.Prompt, AuthSlotID: stored.AuthSlotID, AuthPool: decodeStoredAuthPool(stored.AuthPoolJSON), InvocationClass: HarnessInvocationClass(stored.InvocationClass), Defaults: defaults}, nil
}

func unknownProfileError(profile string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, "profile is not available", map[string]any{"profile": strings.TrimSpace(profile), "reason": "unknown_profile", "start_invoked": false})
}

func isUnknownProfileError(err error) bool {
	handlerErr := &rpc.HandlerError{}
	if !errors.As(err, &handlerErr) {
		return false
	}
	data, ok := handlerErr.Data.(map[string]any)
	if !ok {
		return false
	}
	reason, _ := data["reason"].(string)
	return reason == "unknown_profile"
}
