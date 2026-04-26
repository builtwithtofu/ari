package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type AgentRunStartRequest struct {
	Executor string        `json:"executor"`
	Packet   ContextPacket `json:"packet"`
	Command  string        `json:"command,omitempty"`
	Args     []string      `json:"args,omitempty"`
}

type AgentRunStartResponse struct {
	Run   AgentRun       `json:"run"`
	Items []TimelineItem `json:"items"`
}

type AgentRun struct {
	AgentRunID      string   `json:"agent_run_id"`
	WorkspaceID     string   `json:"workspace_id"`
	TaskID          string   `json:"task_id"`
	Executor        string   `json:"executor"`
	ProviderRunID   string   `json:"provider_run_id"`
	Status          string   `json:"status"`
	ContextPacketID string   `json:"context_packet_id"`
	StartedAt       string   `json:"started_at"`
	Capabilities    []string `json:"capabilities"`
}

const (
	HarnessNameCodex    = "codex"
	HarnessNameClaude   = "claude"
	HarnessNameOpenCode = "opencode"
	HarnessNamePTY      = "pty"
)

type HarnessFactory func(AgentRunStartRequest, string, func(string, []TimelineItem)) (Executor, error)

type AgentProfile struct {
	Name            string                 `json:"name"`
	Harness         string                 `json:"harness"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class"`
}

type AgentRunDefaults struct {
	Harness         string                 `json:"harness,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Prompt          string                 `json:"prompt,omitempty"`
	InvocationClass HarnessInvocationClass `json:"invocation_class,omitempty"`
}

type AgentProfileRunRequest struct {
	Profile           string           `json:"profile,omitempty"`
	ProfileDefinition *AgentProfile    `json:"profile_definition,omitempty"`
	Defaults          AgentRunDefaults `json:"defaults,omitempty"`
	Packet            ContextPacket    `json:"packet"`
}

type AgentProfileRunResponse struct {
	Profile string         `json:"profile"`
	Harness string         `json:"harness"`
	Run     AgentRun       `json:"run"`
	Items   []TimelineItem `json:"items"`
}

func defaultAgentProfiles() map[string]AgentProfile {
	return make(map[string]AgentProfile)
}

func (d *Daemon) resolveHarness(req AgentRunStartRequest, primaryFolder string) (Executor, error) {
	if d == nil {
		return nil, fmt.Errorf("daemon is required")
	}
	name := strings.TrimSpace(req.Executor)
	if name == "" {
		return nil, rpc.NewHandlerError(rpc.InvalidParams, "executor is required", nil)
	}
	factory, ok := d.harnessRegistry.Resolve(name)
	if !ok {
		return nil, unknownHarnessError(name)
	}
	return factory(req, primaryFolder, d.appendExecutorItems)
}

func (d *Daemon) setHarnessFactoryForTest(name string, factory HarnessFactory) {
	if d == nil {
		return
	}
	if d.harnessRegistry == nil {
		d.harnessRegistry = NewHarnessRegistry()
	}
	if err := d.harnessRegistry.ReplaceForTest(name, factory); err != nil {
		panic(fmt.Sprintf("set test harness factory: %v", err))
	}
}

func (d *Daemon) setAgentProfileForTest(profile AgentProfile) {
	if d == nil {
		return
	}
	if d.agentProfiles == nil {
		d.agentProfiles = make(map[string]AgentProfile)
	}
	d.agentProfiles[strings.TrimSpace(profile.Name)] = profile
}

func (d *Daemon) registerExecutorMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentRunStartRequest, AgentRunStartResponse]{
		Name:        "agent.run",
		Description: "Start an executor-backed agent run from a context packet",
		Handler: func(ctx context.Context, req AgentRunStartRequest) (AgentRunStartResponse, error) {
			return d.startAgentRun(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register agent.run: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[AgentProfileRunRequest, AgentProfileRunResponse]{
		Name:        "agent.profile.run",
		Description: "Start an agent run from a named Ari agent profile",
		Handler: func(ctx context.Context, req AgentProfileRunRequest) (AgentProfileRunResponse, error) {
			profile, err := d.resolveAgentProfileRunRequest(req)
			if err != nil {
				return AgentProfileRunResponse{}, err
			}
			resp, err := d.startAgentRun(ctx, store, AgentRunStartRequest{Executor: profile.Harness, Packet: req.Packet}, profile)
			if err != nil {
				return AgentProfileRunResponse{}, err
			}
			return AgentProfileRunResponse{Profile: profile.Name, Harness: profile.Harness, Run: resp.Run, Items: resp.Items}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.profile.run: %w", err)
	}
	return nil
}

func (d *Daemon) resolveAgentProfileRunRequest(req AgentProfileRunRequest) (AgentProfile, error) {
	name := strings.TrimSpace(req.Profile)
	if name != "" && req.ProfileDefinition != nil {
		return AgentProfile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile input is ambiguous", map[string]any{"profile": name, "profile_definition": strings.TrimSpace(req.ProfileDefinition.Name), "reason": "ambiguous_profile", "start_invoked": false})
	}
	var profile AgentProfile
	if req.ProfileDefinition != nil {
		profile = *req.ProfileDefinition
		if strings.TrimSpace(profile.Name) == "" {
			profile.Name = name
		}
	} else if name != "" {
		resolved, err := d.resolveAgentProfile(name)
		if err != nil {
			return AgentProfile{}, err
		}
		profile = resolved
	}
	profile = applyAgentRunDefaults(profile, req.Defaults)
	if strings.TrimSpace(profile.Harness) == "" {
		return AgentProfile{}, rpc.NewHandlerError(rpc.InvalidParams, "harness is required", map[string]any{"profile": strings.TrimSpace(profile.Name), "reason": "missing_harness", "start_invoked": false})
	}
	return profile, nil
}

func applyAgentRunDefaults(profile AgentProfile, defaults AgentRunDefaults) AgentProfile {
	if strings.TrimSpace(profile.Harness) == "" {
		profile.Harness = strings.TrimSpace(defaults.Harness)
	}
	if strings.TrimSpace(profile.Model) == "" {
		profile.Model = strings.TrimSpace(defaults.Model)
	}
	if strings.TrimSpace(profile.Prompt) == "" {
		profile.Prompt = strings.TrimSpace(defaults.Prompt)
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = defaults.InvocationClass
	}
	if profile.InvocationClass == "" {
		profile.InvocationClass = HarnessInvocationAgent
	}
	return profile
}

func (d *Daemon) resolveAgentProfile(name string) (AgentProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return AgentProfile{}, rpc.NewHandlerError(rpc.InvalidParams, "profile is required", map[string]any{"reason": "missing_profile", "start_invoked": false})
	}
	if d == nil || d.agentProfiles == nil {
		return AgentProfile{}, unknownProfileError(name)
	}
	profile, ok := d.agentProfiles[name]
	if !ok {
		return AgentProfile{}, unknownProfileError(name)
	}
	return profile, nil
}

func (d *Daemon) startAgentRun(ctx context.Context, store *globaldb.Store, req AgentRunStartRequest, profile ...AgentProfile) (AgentRunStartResponse, error) {
	executorName := strings.TrimSpace(req.Executor)
	if executorName == "" {
		return AgentRunStartResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "executor is required", nil)
	}
	primaryFolder, err := lookupPrimaryFolder(ctx, store, req.Packet.WorkspaceID)
	if err != nil {
		return AgentRunStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
	}
	executor, err := d.resolveHarness(req, primaryFolder)
	if err != nil {
		return AgentRunStartResponse{}, mapHarnessRunError(err)
	}
	if _, err := store.GetSession(ctx, req.Packet.WorkspaceID); err != nil {
		return AgentRunStartResponse{}, mapWorkspaceStoreError(err, req.Packet.WorkspaceID)
	}
	run, items, err := StartExecutorRun(ctx, executor, req.Packet, profile...)
	if err != nil {
		return AgentRunStartResponse{}, mapHarnessRunError(err)
	}
	d.recordExecutorRun(run, items)
	return AgentRunStartResponse{Run: run, Items: items}, nil
}

func mapHarnessRunError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*rpc.HandlerError); ok {
		return err
	}
	if unsupported, ok := err.(*UnsupportedHarnessCapabilitiesError); ok {
		return rpc.NewHandlerError(rpc.InvalidParams, unsupported.Error(), map[string]any{"unsupported_capabilities": harnessCapabilitiesToStrings(unsupported.Capabilities), "start_invoked": false})
	}
	if unavailable, ok := err.(*HarnessUnavailableError); ok {
		return rpc.NewHandlerError(rpc.InvalidParams, unavailable.Error(), unavailable.Data())
	}
	if validation, ok := err.(*HarnessValidationError); ok {
		return rpc.NewHandlerError(rpc.InvalidParams, validation.Error(), validation.Data())
	}
	return err
}

func unknownProfileError(profile string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, "profile is not available", map[string]any{"profile": strings.TrimSpace(profile), "reason": "unknown_profile", "start_invoked": false})
}

func unknownHarnessError(harness string) error {
	return rpc.NewHandlerError(rpc.InvalidParams, "harness is not available", map[string]any{"harness": strings.TrimSpace(harness), "reason": "unknown_harness", "start_invoked": false})
}

func (d *Daemon) appendExecutorItems(runID string, items []TimelineItem) {
	if d == nil || strings.TrimSpace(runID) == "" || len(items) == 0 {
		return
	}
	d.executorMu.Lock()
	d.executorItems[runID] = append(d.executorItems[runID], items...)
	d.updateExecutorRunStatusLocked(runID, items)
	d.executorMu.Unlock()
}

func StartExecutorRun(ctx context.Context, executor Executor, packet ContextPacket, profile ...AgentProfile) (AgentRun, []TimelineItem, error) {
	if ctx == nil {
		return AgentRun{}, nil, fmt.Errorf("context is required")
	}
	if executor == nil {
		return AgentRun{}, nil, fmt.Errorf("executor is required")
	}
	if strings.TrimSpace(packet.ID) == "" {
		return AgentRun{}, nil, &HarnessValidationError{Message: "context packet id is required", Field: "packet.id"}
	}
	if strings.TrimSpace(packet.WorkspaceID) == "" {
		return AgentRun{}, nil, &HarnessValidationError{Message: "workspace id is required", Field: "packet.workspace_id"}
	}
	if strings.TrimSpace(packet.TaskID) == "" {
		return AgentRun{}, nil, &HarnessValidationError{Message: "task id is required", Field: "packet.task_id"}
	}
	call, err := NewAgentRunHarnessCall(packet, nil)
	if err != nil {
		return AgentRun{}, nil, err
	}
	if len(profile) > 0 {
		call.SourceProfileID = strings.TrimSpace(profile[0].Name)
		call.Model = strings.TrimSpace(profile[0].Model)
		call.Prompt = strings.TrimSpace(profile[0].Prompt)
		if profile[0].InvocationClass != "" {
			call.InvocationClass = profile[0].InvocationClass
		}
	}
	call.Input = json.RawMessage(renderContextPacket(packet))
	return StartHarnessCall(ctx, executor, call)
}

func (d *Daemon) recordExecutorRun(run AgentRun, items []TimelineItem) {
	if d == nil || strings.TrimSpace(run.AgentRunID) == "" {
		return
	}
	d.executorMu.Lock()
	d.executorRuns[run.AgentRunID] = run
	buffered := append([]TimelineItem(nil), d.executorItems[run.AgentRunID]...)
	d.executorItems[run.AgentRunID] = mergeExecutorItems(items, buffered)
	d.updateExecutorRunStatusLocked(run.AgentRunID, d.executorItems[run.AgentRunID])
	d.executorMu.Unlock()
}

func mergeExecutorItems(primary, buffered []TimelineItem) []TimelineItem {
	out := make([]TimelineItem, 0, len(primary)+len(buffered))
	seen := make(map[string]bool, len(primary)+len(buffered))
	for _, item := range primary {
		out = append(out, item)
		if strings.TrimSpace(item.ID) != "" {
			seen[item.ID] = true
		}
	}
	for _, item := range buffered {
		if strings.TrimSpace(item.ID) != "" && seen[item.ID] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (d *Daemon) updateExecutorRunStatusLocked(runID string, items []TimelineItem) {
	run, ok := d.executorRuns[runID]
	if !ok {
		return
	}
	status := executorRunStatusFromItems(items)
	if status == "running" {
		return
	}
	run.Status = status
	d.executorRuns[runID] = run
}

func executorRunStatusFromItems(items []TimelineItem) string {
	if len(items) == 0 {
		return "running"
	}
	completed := false
	for _, item := range items {
		switch strings.TrimSpace(item.Status) {
		case "failed":
			return "failed"
		case "completed":
			completed = true
		}
	}
	if completed {
		return "completed"
	}
	return "running"
}

func renderContextPacket(packet ContextPacket) string {
	encoded, err := json.Marshal(packet)
	if err != nil {
		panic(fmt.Sprintf("render context packet: %v", err))
	}
	return string(encoded)
}
