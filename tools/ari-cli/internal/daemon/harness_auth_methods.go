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

type HarnessAuthStatusRequest struct {
	WorkspaceID string            `json:"workspace_id,omitempty"`
	Slots       []HarnessAuthSlot `json:"slots,omitempty"`
}

type HarnessAuthStatusResponse struct {
	Statuses []HarnessAuthStatus `json:"statuses"`
}

type HarnessAuthDiagnoseRequest struct {
	WorkspaceID             string `json:"workspace_id,omitempty"`
	DiscoverProviderMethods bool   `json:"discover_provider_methods,omitempty"`
}

type HarnessAuthDiagnoseResponse struct {
	Harnesses []HarnessAuthDiagnostic `json:"harnesses"`
}

type HarnessAuthDiagnostic struct {
	Harness         string                              `json:"harness"`
	Installed       bool                                `json:"installed"`
	Status          HarnessAuthState                    `json:"status"`
	Presentation    Presentation                        `json:"presentation"`
	DefaultSlot     HarnessAuthStatus                   `json:"default_slot"`
	NamedSlots      []AuthSlotResponse                  `json:"named_slots,omitempty"`
	Auth            HarnessAuthDescriptor               `json:"auth"`
	ProviderMethods HarnessAuthProviderMethodDiagnostic `json:"provider_methods"`
	NextStep        string                              `json:"next_step,omitempty"`
}

type HarnessAuthProviderMethodDiagnostic struct {
	Status    string                             `json:"status"`
	Connected []string                           `json:"connected,omitempty"`
	Providers map[string][]HarnessAuthMethodInfo `json:"providers,omitempty"`
}

type HarnessAuthProviderMethodsRequest struct {
	Harness     string `json:"harness"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type HarnessAuthProviderMethodsResponse struct {
	Status    string                             `json:"status"`
	Connected []string                           `json:"connected,omitempty"`
	Providers map[string][]HarnessAuthMethodInfo `json:"providers,omitempty"`
}

type HarnessAuthMethodInfo struct {
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

type HarnessAuthStartRequest struct {
	AuthSlotID  string `json:"auth_slot_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Method      string `json:"method,omitempty"`
}

type HarnessAuthStartResponse struct {
	Status HarnessAuthStatus `json:"status"`
}

type HarnessAuthCancelRequest struct {
	AuthSlotID  string `json:"auth_slot_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	FlowID      string `json:"flow_id"`
}

type HarnessAuthCancelResponse struct {
	Status HarnessAuthStatus `json:"status"`
}

type HarnessAuthLogoutRequest struct {
	AuthSlotID  string `json:"auth_slot_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

type HarnessAuthLogoutResponse struct {
	Status HarnessAuthStatus `json:"status"`
}

type AuthSlotListRequest struct {
	Harness string `json:"harness,omitempty"`
}

type AuthSlotGetRequest struct {
	AuthSlotID string `json:"auth_slot_id"`
}

type AuthSlotSaveRequest struct {
	AuthSlotID    string `json:"auth_slot_id"`
	Harness       string `json:"harness"`
	Label         string `json:"label"`
	ProviderLabel string `json:"provider_label,omitempty"`
}

type AuthSlotRemoveRequest struct {
	AuthSlotID string `json:"auth_slot_id"`
}

type AuthSlotRemoveResponse struct {
	Status     string `json:"status"`
	AuthSlotID string `json:"auth_slot_id"`
}

type AuthSlotResponse struct {
	AuthSlotID      string `json:"auth_slot_id"`
	Harness         string `json:"harness"`
	Label           string `json:"label"`
	ProviderLabel   string `json:"provider_label,omitempty"`
	CredentialOwner string `json:"credential_owner"`
	Status          string `json:"status"`
}

type AuthSlotListResponse struct {
	Slots []AuthSlotResponse `json:"slots"`
}

type harnessAuthStatuser interface {
	AuthStatus(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error)
}

type harnessAuthStarter interface {
	AuthStart(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
}

type harnessAuthCanceller interface {
	AuthCancel(context.Context, HarnessAuthSlot, string) (HarnessAuthStatus, error)
}

type harnessAuthLoggerOuter interface {
	AuthLogout(context.Context, HarnessAuthSlot) (HarnessAuthStatus, error)
}

type harnessAuthProviderMethodDiscoverer interface {
	AuthProviderMethods(context.Context) (HarnessAuthProviderMethodsResponse, error)
}

func (d *Daemon) harnessAuthStart(ctx context.Context, store *globaldb.Store, req HarnessAuthStartRequest) (HarnessAuthStartResponse, error) {
	stored, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return HarnessAuthStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth slot is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "start_invoked": false})
		}
		return HarnessAuthStartResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	slot := harnessAuthSlotFromGlobal(stored)
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthStartResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	factory, ok := d.harnessRegistry.Resolve(slot.Harness)
	if !ok {
		return HarnessAuthStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "unknown_harness", "start_invoked": false})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthStartResponse{}, mapHarnessRunError(err)
	}
	starter, ok := executor.(harnessAuthStarter)
	if !ok {
		return HarnessAuthStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth start is not supported", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "auth_start_unsupported", "start_invoked": false})
	}
	status, err := starter.AuthStart(ctx, slot, req.Method)
	if err != nil {
		return HarnessAuthStartResponse{}, mapHarnessRunError(err)
	}
	if status.Status != "" && status.Status != HarnessAuthUnknown {
		stored.Status = string(status.Status)
		if err := store.UpsertAuthSlot(ctx, stored); err != nil {
			return HarnessAuthStartResponse{}, err
		}
	}
	return HarnessAuthStartResponse{Status: presentAuthStatus(status)}, nil
}

func (d *Daemon) harnessAuthCancel(ctx context.Context, store *globaldb.Store, req HarnessAuthCancelRequest) (HarnessAuthCancelResponse, error) {
	stored, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth slot is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "cancel_invoked": false})
		}
		return HarnessAuthCancelResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	flowID := strings.TrimSpace(req.FlowID)
	if flowID == "" {
		return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "flow id is required", map[string]any{"reason": "missing_flow_id", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "cancel_invoked": false})
	}
	slot := harnessAuthSlotFromGlobal(stored)
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthCancelResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	factory, ok := d.harnessRegistry.Resolve(slot.Harness)
	if !ok {
		return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "unknown_harness", "cancel_invoked": false})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthCancelResponse{}, mapHarnessRunError(err)
	}
	canceller, ok := executor.(harnessAuthCanceller)
	if !ok {
		return HarnessAuthCancelResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth cancel is not supported", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "auth_cancel_unsupported", "cancel_invoked": false})
	}
	status, err := canceller.AuthCancel(ctx, slot, flowID)
	if err != nil {
		return HarnessAuthCancelResponse{}, mapHarnessRunError(err)
	}
	if status.Status != "" && status.Status != HarnessAuthUnknown {
		stored.Status = string(status.Status)
		if err := store.UpsertAuthSlot(ctx, stored); err != nil {
			return HarnessAuthCancelResponse{}, err
		}
	}
	return HarnessAuthCancelResponse{Status: presentAuthStatus(status)}, nil
}

func (d *Daemon) harnessAuthLogout(ctx context.Context, store *globaldb.Store, req HarnessAuthLogoutRequest) (HarnessAuthLogoutResponse, error) {
	stored, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		if errors.Is(err, globaldb.ErrNotFound) {
			return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth account is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(req.AuthSlotID), "logout_invoked": false})
		}
		return HarnessAuthLogoutResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	slot := harnessAuthSlotFromGlobal(stored)
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthLogoutResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	factory, ok := d.harnessRegistry.Resolve(slot.Harness)
	if !ok {
		return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "unknown_harness", "logout_invoked": false})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: slot.Harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthLogoutResponse{}, mapHarnessRunError(err)
	}
	loggerOuter, ok := executor.(harnessAuthLoggerOuter)
	if !ok {
		return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth logout is not supported", map[string]any{"harness": strings.TrimSpace(slot.Harness), "reason": "auth_logout_unsupported", "logout_invoked": false, "ari_secret_storage": string(HarnessAriSecretStorageNone)})
	}
	status, err := loggerOuter.AuthLogout(ctx, slot)
	if err != nil {
		return HarnessAuthLogoutResponse{}, mapHarnessRunError(err)
	}
	if status.Status != "" && status.Status != HarnessAuthUnknown {
		stored.Status = string(status.Status)
		if err := store.UpsertAuthSlot(ctx, stored); err != nil {
			return HarnessAuthLogoutResponse{}, rpc.NewHandlerError(rpc.InternalError, "provider logout completed but Ari could not persist auth status", map[string]any{"reason": "auth_logout_status_persist_failed", "auth_slot_id": strings.TrimSpace(slot.AuthSlotID), "harness": strings.TrimSpace(slot.Harness), "status": string(status.Status), "logout_invoked": true, "ari_secret_storage": string(HarnessAriSecretStorageNone)})
		}
	}
	return HarnessAuthLogoutResponse{Status: presentAuthStatus(status)}, nil
}

func (d *Daemon) harnessAuthDiagnose(ctx context.Context, store *globaldb.Store, req HarnessAuthDiagnoseRequest) (HarnessAuthDiagnoseResponse, error) {
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		var err error
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthDiagnoseResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	storedSlots, err := store.ListAuthSlots(ctx, "")
	if err != nil {
		return HarnessAuthDiagnoseResponse{}, err
	}
	storedByID := make(map[string]globaldb.AuthSlot, len(storedSlots))
	slotsByHarness := map[string][]globaldb.AuthSlot{}
	for _, stored := range storedSlots {
		storedByID[stored.AuthSlotID] = stored
		slotsByHarness[stored.Harness] = append(slotsByHarness[stored.Harness], stored)
	}
	resp := HarnessAuthDiagnoseResponse{Harnesses: make([]HarnessAuthDiagnostic, 0, len(providerAuthHarnesses()))}
	for _, harness := range providerAuthHarnesses() {
		auth := HarnessAuthDescriptor{}
		if descriptor, ok := d.harnessRegistry.ResolveDescriptor(harness); ok {
			auth = descriptor.Auth
		}
		factory, ok := d.harnessRegistry.Resolve(harness)
		if !ok {
			status := HarnessAuthStatus{Harness: harness, AuthSlotID: authSlotIDForName(harness, "default"), Name: "default", Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}
			resp.Harnesses = append(resp.Harnesses, HarnessAuthDiagnostic{Harness: harness, Installed: true, Status: HarnessAuthUnknown, DefaultSlot: status, Auth: auth, NextStep: d.authDiagnosticNextStep(status)})
			continue
		}
		executor, err := factory(HarnessSessionStartRequest{Executor: harness}, primaryFolder, d.appendExecutorItems)
		diagnostic := HarnessAuthDiagnostic{Harness: harness, Installed: true, Status: HarnessAuthUnknown, DefaultSlot: HarnessAuthStatus{Harness: harness, AuthSlotID: authSlotIDForName(harness, "default"), Name: "default", Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}, Auth: auth}
		if err != nil {
			diagnostic.DefaultSlot = NewHarnessAuthRequired(harness, diagnostic.DefaultSlot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, SecretOwnedBy: harness})
			diagnostic.DefaultSlot.Name = "default"
			diagnostic.Status = diagnostic.DefaultSlot.Status
			diagnostic.NextStep = d.authDiagnosticNextStep(diagnostic.DefaultSlot)
			resp.Harnesses = append(resp.Harnesses, diagnostic)
			continue
		}
		if describer, ok := executor.(HarnessDescriber); ok {
			diagnostic.Auth = describer.Descriptor().Auth
		}
		defaultSlot := harnessAuthSlotFromGlobal(globaldb.AuthSlot{AuthSlotID: authSlotIDForName(harness, "default"), Harness: harness, Label: "default", CredentialOwner: string(HarnessCredentialOwnerProvider), Status: string(HarnessAuthUnknown)})
		if stored, ok := storedByID[defaultSlot.AuthSlotID]; ok {
			defaultSlot = harnessAuthSlotFromGlobal(stored)
		}
		if statuser, ok := executor.(harnessAuthStatuser); ok {
			status, err := statuser.AuthStatus(ctx, defaultSlot)
			if err != nil {
				var unavailable *HarnessUnavailableError
				if errors.As(err, &unavailable) && unavailable.Reason == "missing_executable" {
					status = HarnessAuthStatus{Harness: harness, AuthSlotID: defaultSlot.AuthSlotID, Status: HarnessAuthNotInstalled, AriSecretStorage: HarnessAriSecretStorageNone}
					diagnostic.Installed = false
				} else {
					return HarnessAuthDiagnoseResponse{}, err
				}
			}
			status.Name = authStatusName(defaultSlot, harness)
			diagnostic.DefaultSlot = status
			diagnostic.Status = status.Status
			diagnostic.NextStep = d.authDiagnosticNextStep(status)
		}
		diagnostic.ProviderMethods = authProviderMethodDiagnostic(ctx, executor, req.DiscoverProviderMethods)
		for _, stored := range slotsByHarness[harness] {
			if stored.AuthSlotID == defaultSlot.AuthSlotID {
				continue
			}
			diagnostic.NamedSlots = append(diagnostic.NamedSlots, authSlotResponseFromGlobal(stored))
		}
		resp.Harnesses = append(resp.Harnesses, d.presentAuthDiagnostic(diagnostic))
	}
	return resp, nil
}

func providerAuthHarnesses() []string {
	return []string{HarnessNameClaude, HarnessNameCodex, HarnessNameOpenCode, HarnessNamePi, HarnessNameGrok}
}

func authProviderMethodDiagnostic(ctx context.Context, executor Executor, discover bool) HarnessAuthProviderMethodDiagnostic {
	if !discover {
		return HarnessAuthProviderMethodDiagnostic{Status: "skipped"}
	}
	discoverer, ok := executor.(harnessAuthProviderMethodDiscoverer)
	if !ok {
		return HarnessAuthProviderMethodDiagnostic{Status: "unsupported"}
	}
	methods, err := discoverer.AuthProviderMethods(ctx)
	if err != nil {
		return HarnessAuthProviderMethodDiagnostic{Status: "error"}
	}
	return HarnessAuthProviderMethodDiagnostic(methods)
}

func (d *Daemon) harnessAuthProviderMethods(ctx context.Context, store *globaldb.Store, req HarnessAuthProviderMethodsRequest) (HarnessAuthProviderMethodsResponse, error) {
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		var err error
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthProviderMethodsResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	harness := strings.TrimSpace(req.Harness)
	if harness == "" {
		return HarnessAuthProviderMethodsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is required", map[string]any{"reason": "harness_required"})
	}
	factory, ok := d.harnessRegistry.Resolve(harness)
	if !ok {
		return HarnessAuthProviderMethodsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is not registered", map[string]any{"harness": harness, "reason": "harness_not_registered"})
	}
	executor, err := factory(HarnessSessionStartRequest{Executor: harness}, primaryFolder, d.appendExecutorItems)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, mapHarnessRunError(err)
	}
	discoverer, ok := executor.(harnessAuthProviderMethodDiscoverer)
	if !ok {
		return HarnessAuthProviderMethodsResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "harness auth provider methods are not supported", map[string]any{"harness": harness, "reason": "auth_provider_methods_unsupported"})
	}
	methods, err := discoverer.AuthProviderMethods(ctx)
	if err != nil {
		return HarnessAuthProviderMethodsResponse{}, mapHarnessRunError(err)
	}
	return methods, nil
}

func authSlotIDForName(harness, name string) string {
	harness = strings.TrimSpace(harness)
	name = strings.TrimSpace(name)
	if name == "" || name == "default" {
		return harness + "-default"
	}
	return harness + "-" + name
}

func (d *Daemon) authDiagnosticNextStep(status HarnessAuthStatus) string {
	if status.Status == HarnessAuthAuthenticated {
		return ""
	}
	if status.Status == HarnessAuthNotInstalled {
		return "Install " + d.harnessDisplayName(status.Harness) + ", then run `ari auth login --harness " + status.Harness + "`."
	}
	method := ""
	if status.Remediation != nil && strings.TrimSpace(status.Remediation.Method) != "" {
		method = strings.TrimSpace(status.Remediation.Method)
	}
	switch method {
	case "device_code":
		return "Run `ari auth login --harness " + status.Harness + "` and complete the provider's device-code login."
	case "opencode_interactive":
		return "Run `ari auth login --harness opencode` and complete OpenCode's provider login."
	case "browser":
		return "Run `ari auth login --harness " + status.Harness + "` and complete the provider browser login."
	case "api_key", "api_key_provider_setup":
		return "Run `ari auth login --harness " + status.Harness + "`; Ari will not store the provider API key."
	case "provider_config", "provider_login", "":
		return "Run `ari auth login --harness " + status.Harness + "` or check the provider's native auth setup."
	default:
		return "Resolve provider auth for " + status.Harness + ": " + method + "."
	}
}

func (d *Daemon) harnessDisplayName(harness string) string {
	if d != nil {
		if descriptor, ok := d.harnessRegistry.ResolveDescriptor(harness); ok && strings.TrimSpace(descriptor.DisplayName) != "" {
			return descriptor.DisplayName
		}
	}
	return harness
}

func (d *Daemon) harnessAuthProjectionStyle(harness string) HarnessAuthProjectionStyle {
	if d == nil {
		return HarnessAuthProjectionStyleNone
	}
	descriptor, ok := d.harnessRegistry.ResolveDescriptor(strings.TrimSpace(harness))
	if !ok {
		return HarnessAuthProjectionStyleNone
	}
	return descriptor.AuthProjection
}

func (d *Daemon) harnessAuthStatus(ctx context.Context, store *globaldb.Store, req HarnessAuthStatusRequest) (HarnessAuthStatusResponse, error) {
	primaryFolder := ""
	if strings.TrimSpace(req.WorkspaceID) != "" {
		var err error
		primaryFolder, err = lookupPrimaryFolder(ctx, store, req.WorkspaceID)
		if err != nil {
			return HarnessAuthStatusResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
		}
	}
	slots := req.Slots
	storedSlots, err := store.ListAuthSlots(ctx, "")
	if err != nil {
		return HarnessAuthStatusResponse{}, err
	}
	storedByID := make(map[string]globaldb.AuthSlot, len(storedSlots))
	for _, stored := range storedSlots {
		storedByID[stored.AuthSlotID] = stored
	}
	if len(slots) == 0 {
		for _, stored := range storedSlots {
			slots = append(slots, harnessAuthSlotFromGlobal(stored))
		}
	} else {
		validated := make([]HarnessAuthSlot, 0, len(slots))
		for _, requested := range slots {
			stored, ok := storedByID[strings.TrimSpace(requested.AuthSlotID)]
			if !ok {
				return HarnessAuthStatusResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth slot is not configured", map[string]any{"reason": "unknown_auth_slot", "auth_slot_id": strings.TrimSpace(requested.AuthSlotID)})
			}
			validated = append(validated, harnessAuthSlotFromGlobal(stored))
		}
		slots = validated
	}
	if len(slots) == 0 {
		return HarnessAuthStatusResponse{Statuses: []HarnessAuthStatus{}}, nil
	}
	statuses := make([]HarnessAuthStatus, 0, len(slots))
	for _, slot := range slots {
		harness := strings.TrimSpace(slot.Harness)
		factory, ok := d.harnessRegistry.Resolve(harness)
		if !ok {
			status := HarnessAuthStatus{Harness: harness, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}
			status.Name = authStatusName(slot, status.Harness)
			statuses = append(statuses, status)
			continue
		}
		executor, err := factory(HarnessSessionStartRequest{Executor: harness}, primaryFolder, d.appendExecutorItems)
		if err != nil {
			status := NewHarnessAuthRequired(harness, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, SecretOwnedBy: harness})
			status.Name = authStatusName(slot, harness)
			statuses = append(statuses, status)
			continue
		}
		statuser, ok := executor.(harnessAuthStatuser)
		if !ok {
			status := HarnessAuthStatus{Harness: harness, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthUnknown, AriSecretStorage: HarnessAriSecretStorageNone}
			status.Name = authStatusName(slot, harness)
			statuses = append(statuses, status)
			continue
		}
		status, err := statuser.AuthStatus(ctx, slot)
		if err != nil {
			var unavailable *HarnessUnavailableError
			if errors.As(err, &unavailable) && unavailable.Reason == "missing_executable" {
				status := HarnessAuthStatus{Harness: harness, AuthSlotID: strings.TrimSpace(slot.AuthSlotID), Status: HarnessAuthNotInstalled, AriSecretStorage: HarnessAriSecretStorageNone}
				status.Name = authStatusName(slot, harness)
				if err := storePersistAuthStatus(ctx, store, storedByID, slot.AuthSlotID, status.Status); err != nil {
					return HarnessAuthStatusResponse{}, err
				}
				statuses = append(statuses, presentAuthStatus(status))
				continue
			}
			return HarnessAuthStatusResponse{}, err
		}
		if status.Status == HarnessAuthAuthenticated && d.namedSlotMissingProjection(storedByID[slot.AuthSlotID]) {
			status = NewHarnessAuthRequired(harness, slot.AuthSlotID, HarnessAuthRemediation{Kind: HarnessAuthRemediationProviderAuthFlow, Method: "ari_secret_projection_required", SecretOwnedBy: harness})
		}
		if err := storePersistAuthStatus(ctx, store, storedByID, slot.AuthSlotID, status.Status); err != nil {
			return HarnessAuthStatusResponse{}, err
		}
		status.Name = authStatusName(slot, status.Harness)
		statuses = append(statuses, presentAuthStatus(status))
	}
	return HarnessAuthStatusResponse{Statuses: statuses}, nil
}

func (d *Daemon) namedSlotMissingProjection(slot globaldb.AuthSlot) bool {
	harness := strings.TrimSpace(slot.Harness)
	if d.harnessAuthProjectionStyle(harness) == HarnessAuthProjectionStyleNone {
		return false
	}
	if authSlotIsDefaultForHarness(harness, slot.AuthSlotID) {
		return false
	}
	_, err := slotProjectionSecretID(harness, slot.MetadataJSON)
	return err != nil
}

func authStatusName(slot HarnessAuthSlot, harness string) string {
	if strings.TrimSpace(slot.AuthSlotID) == strings.TrimSpace(harness)+"-default" {
		return "default"
	}
	return strings.TrimSpace(slot.Label)
}

func storePersistAuthStatus(ctx context.Context, store *globaldb.Store, storedByID map[string]globaldb.AuthSlot, authSlotID string, status HarnessAuthState) error {
	if status == "" || status == HarnessAuthUnknown {
		return nil
	}
	stored := storedByID[strings.TrimSpace(authSlotID)]
	stored.Status = string(status)
	return store.UpsertAuthSlot(ctx, stored)
}

func listAuthSlots(ctx context.Context, store *globaldb.Store, req AuthSlotListRequest) (AuthSlotListResponse, error) {
	slots, err := store.ListAuthSlots(ctx, req.Harness)
	if err != nil {
		return AuthSlotListResponse{}, err
	}
	resp := AuthSlotListResponse{Slots: make([]AuthSlotResponse, 0, len(slots))}
	for _, slot := range slots {
		resp.Slots = append(resp.Slots, authSlotResponseFromGlobal(slot))
	}
	return resp, nil
}

func getAuthSlot(ctx context.Context, store *globaldb.Store, req AuthSlotGetRequest) (AuthSlotResponse, error) {
	slot, err := store.GetAuthSlot(ctx, req.AuthSlotID)
	if err != nil {
		return AuthSlotResponse{}, mapWorkspaceStoreError(err, req.AuthSlotID)
	}
	return authSlotResponseFromGlobal(slot), nil
}

func saveAuthSlot(ctx context.Context, store *globaldb.Store, req AuthSlotSaveRequest) (AuthSlotResponse, error) {
	slot := globaldb.AuthSlot{AuthSlotID: strings.TrimSpace(req.AuthSlotID), Harness: strings.TrimSpace(req.Harness), Label: strings.TrimSpace(req.Label), ProviderLabel: strings.TrimSpace(req.ProviderLabel), CredentialOwner: "provider", Status: string(HarnessAuthUnknown), MetadataJSON: "{}"}
	if slot.Label == "" {
		slot.Label = authStatusName(HarnessAuthSlot{AuthSlotID: slot.AuthSlotID}, slot.Harness)
	}
	if err := store.UpsertAuthSlot(ctx, slot); err != nil {
		return AuthSlotResponse{}, mapWorkspaceStoreError(err, slot.AuthSlotID)
	}
	return authSlotResponseFromGlobal(slot), nil
}

func (d *Daemon) removeAuthSlot(ctx context.Context, store *globaldb.Store, req AuthSlotRemoveRequest) (AuthSlotRemoveResponse, error) {
	authSlotID := strings.TrimSpace(req.AuthSlotID)
	if authSlotID == "" {
		return AuthSlotRemoveResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "auth_slot_id is required", map[string]any{"reason": "missing_auth_slot_id"})
	}
	if stored, err := store.GetAuthSlot(ctx, authSlotID); err == nil && d.harnessAuthProjectionStyle(stored.Harness) != HarnessAuthProjectionStyleNone {
		if secretID, err := slotProjectionSecretID(stored.Harness, stored.MetadataJSON); err == nil {
			if err := store.DeleteSecret(ctx, d.secretBackend, secretID); err != nil && !errors.Is(err, globaldb.ErrNotFound) {
				return AuthSlotRemoveResponse{}, mapWorkspaceStoreError(err, secretID)
			}
		}
	}
	if err := store.DeleteAuthSlot(ctx, authSlotID); err != nil {
		return AuthSlotRemoveResponse{}, mapWorkspaceStoreError(err, authSlotID)
	}
	return AuthSlotRemoveResponse{Status: "removed", AuthSlotID: authSlotID}, nil
}

func authSlotResponseFromGlobal(slot globaldb.AuthSlot) AuthSlotResponse {
	return AuthSlotResponse{AuthSlotID: slot.AuthSlotID, Harness: slot.Harness, Label: slot.Label, ProviderLabel: slot.ProviderLabel, CredentialOwner: slot.CredentialOwner, Status: slot.Status}
}

func harnessAuthSlotFromGlobal(slot globaldb.AuthSlot) HarnessAuthSlot {
	return HarnessAuthSlot{AuthSlotID: slot.AuthSlotID, Harness: slot.Harness, Label: slot.Label, ProviderLabel: slot.ProviderLabel, CredentialOwner: HarnessCredentialOwner(slot.CredentialOwner), Status: HarnessAuthState(slot.Status)}
}

func resolveProfileAuthSlot(ctx context.Context, store *globaldb.Store, executor Executor, harness string, profile Profile) (string, error) {
	if strings.TrimSpace(profile.AuthSlotID) == "" && len(profile.AuthPool.SlotIDs) == 0 {
		return "", nil
	}
	statuser, ok := executor.(harnessAuthStatuser)
	if !ok {
		return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_selection_unsupported", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	slotIDs := []string{}
	if strings.TrimSpace(profile.AuthSlotID) != "" {
		slotIDs = append(slotIDs, strings.TrimSpace(profile.AuthSlotID))
	} else {
		for _, slotID := range profile.AuthPool.SlotIDs {
			if strings.TrimSpace(slotID) != "" {
				slotIDs = append(slotIDs, strings.TrimSpace(slotID))
			}
		}
	}
	slots := make([]HarnessAuthSlot, 0, len(slotIDs))
	isPoolSelection := strings.TrimSpace(profile.AuthSlotID) == ""
	for _, slotID := range slotIDs {
		stored, err := store.GetAuthSlot(ctx, slotID)
		if err != nil {
			if errors.Is(err, globaldb.ErrNotFound) {
				if isPoolSelection {
					continue
				}
				return "", &HarnessUnavailableError{Harness: harness, Reason: "unknown_auth_slot", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
			}
			return "", err
		}
		slot := harnessAuthSlotFromGlobal(stored)
		if strings.TrimSpace(slot.Harness) != strings.TrimSpace(harness) {
			return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_harness_mismatch", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
		}
		status, err := statuser.AuthStatus(ctx, slot)
		if err != nil {
			return "", err
		}
		slot.Status = status.Status
		slots = append(slots, slot)
	}
	selected, _, err := ResolveHarnessAuthSlot(HarnessAuthSelection{ProfileSlotID: profile.AuthSlotID, ProfilePool: profile.AuthPool, Harness: harness}, slots)
	if err != nil {
		return "", &HarnessUnavailableError{Harness: harness, Reason: "auth_slot_not_ready", RequiredCapability: HarnessCapabilityHarnessSessionFromContext, StartInvoked: false}
	}
	return selected.AuthSlotID, nil
}

func authSlotIDFromProfiles(profile ...Profile) string {
	if len(profile) == 0 {
		return ""
	}
	return strings.TrimSpace(profile[0].AuthSlotID)
}

func (d *Daemon) authProjectionForStart(ctx context.Context, store *globaldb.Store, harness, workspaceID, authSlotID string) (HarnessAuthProjectionPlan, error) {
	harness = strings.TrimSpace(harness)
	authSlotID = strings.TrimSpace(authSlotID)
	style := d.harnessAuthProjectionStyle(harness)
	if style == HarnessAuthProjectionStyleNone {
		return HarnessAuthProjectionPlan{}, nil
	}
	if authSlotIsDefaultForHarness(harness, authSlotID) {
		return HarnessAuthProjectionPlan{}, nil
	}
	slot, err := store.GetAuthSlot(ctx, authSlotID)
	if err != nil {
		return HarnessAuthProjectionPlan{}, err
	}
	secretID, err := slotProjectionSecretID(harness, slot.MetadataJSON)
	if err != nil {
		return HarnessAuthProjectionPlan{}, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return HarnessAuthProjectionPlan{}, fmt.Errorf("%w: workspace_id is required for %s secret projection", globaldb.ErrPermissionDenied, harness)
	}
	value, err := store.ProjectSecretWithGrant(ctx, d.secretBackend, secretID, globaldb.SecretGrantSubjectWorkspace, workspaceID)
	if err != nil {
		return HarnessAuthProjectionPlan{}, err
	}
	if style == HarnessAuthProjectionStyleEnvKeys {
		// env-keys secrets are a JSON object of provider env keys projected
		// as an ari-owned env plan.
		env, err := piProjectionEnvFromSecret(value)
		if err != nil {
			return HarnessAuthProjectionPlan{}, err
		}
		return HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerAri, Kind: HarnessAuthProjectionEnv, Env: env, RiskLabels: []string{"provider_owned", "ari_projected_env_keys", "env_projection_downgrade_risk"}}, nil
	}
	return HarnessAuthProjectionPlan{Owner: HarnessAuthProjectionOwnerAri, Kind: HarnessAuthProjectionAuthContent, Env: map[string]string{"OPENCODE_AUTH_CONTENT": string(value)}, RiskLabels: []string{"provider_owned", "provider_hint_matching", "ari_projected_auth_content", "env_projection_downgrade_risk"}}, nil
}

func slotProjectionSecretID(harness, metadataJSON string) (string, error) {
	var metadata map[string]string
	if err := json.Unmarshal([]byte(defaultString(strings.TrimSpace(metadataJSON), "{}")), &metadata); err != nil {
		return "", fmt.Errorf("%w: auth slot metadata json is invalid", globaldb.ErrInvalidInput)
	}
	secretID := strings.TrimSpace(metadata["projection_ref"])
	if secretID == "" {
		return "", fmt.Errorf("%w: %s auth slot projection_ref is required", globaldb.ErrInvalidInput, harness)
	}
	return secretID, nil
}

func piProjectionEnvFromSecret(value []byte) (map[string]string, error) {
	var env map[string]string
	if err := json.Unmarshal(value, &env); err != nil {
		return nil, fmt.Errorf("%w: pi auth secret must be a JSON object of env keys", globaldb.ErrInvalidInput)
	}
	out := make(map[string]string, len(env))
	for key, secret := range env {
		key = strings.TrimSpace(key)
		if key == "" || strings.TrimSpace(secret) == "" {
			continue
		}
		out[key] = secret
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: pi auth secret has no env keys", globaldb.ErrInvalidInput)
	}
	return out, nil
}
