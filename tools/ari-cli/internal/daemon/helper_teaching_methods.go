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

type HelperContextRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Question    string `json:"question,omitempty"`
}

type HelperContextResponse struct {
	Workspace         HelperWorkspaceSummary   `json:"workspace"`
	Defaults          map[string]string        `json:"defaults"`
	Profiles          []HelperProfileSummary   `json:"profiles"`
	Workspaces        []HelperWorkspaceSummary `json:"workspaces,omitempty"`
	FinalResponses    []HelperFinalResponse    `json:"final_responses"`
	Telemetry         []HelperTelemetrySummary `json:"telemetry"`
	Proofs            []ProofResultSummary     `json:"proofs"`
	Health            HelperHealthSummary      `json:"health"`
	WorkflowLearnings []WorkflowLearning       `json:"workflow_learnings"`
	Docs              []string                 `json:"docs"`
	Explanations      []string                 `json:"explanations"`
}

type HelperWorkspaceSummary struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Status      string `json:"status"`
	OriginRoot  string `json:"origin_root"`
	FolderCount int    `json:"folder_count"`
}

type HelperProfileSummary struct {
	ProfileID       string `json:"profile_id"`
	WorkspaceID     string `json:"workspace_id"`
	Name            string `json:"name"`
	Harness         string `json:"harness"`
	Model           string `json:"model,omitempty"`
	InvocationClass string `json:"invocation_class,omitempty"`
	PromptSummary   string `json:"prompt_summary,omitempty"`
}

type HelperFinalResponse struct {
	FinalResponseID string `json:"final_response_id"`
	RunID           string `json:"run_id"`
	Status          string `json:"status"`
	Summary         string `json:"summary"`
}

type HelperTelemetrySummary struct {
	ProfileID   string `json:"profile_id,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
	Harness     string `json:"harness"`
	Model       string `json:"model"`
	Runs        int    `json:"runs"`
	Completed   int    `json:"completed"`
	Failed      int    `json:"failed"`
}

type WorkflowLearning struct {
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	Summary    string `json:"summary"`
}

type HelperHealthSummary struct {
	DaemonVersion      string `json:"daemon_version"`
	ConfigReadable     bool   `json:"config_readable"`
	WorkspaceAvailable bool   `json:"workspace_available"`
	WorkspaceKind      string `json:"workspace_kind"`
}

type HelperExplainRequest struct {
	WorkspaceID string `json:"workspace_id"`
	Topic       string `json:"topic"`
}

type HelperExplainResponse struct {
	Topic       string   `json:"topic"`
	Explanation string   `json:"explanation"`
	Anchors     []string `json:"anchors"`
	RunID       string   `json:"run_id,omitempty"`
}

func (d *Daemon) registerHelperTeachingMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if err := rpc.RegisterMethod(registry, rpc.Method[HelperContextRequest, HelperContextResponse]{
		Name:        "helper.context",
		Description: "Read-only helper context from Ari workspace state",
		Handler: func(ctx context.Context, req HelperContextRequest) (HelperContextResponse, error) {
			return d.helperContext(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register helper.context: %w", err)
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[HelperExplainRequest, HelperExplainResponse]{
		Name:        "helper.explain",
		Description: "Explain Ari concepts from workspace state",
		Handler: func(ctx context.Context, req HelperExplainRequest) (HelperExplainResponse, error) {
			return d.helperExplain(ctx, store, req)
		},
	}); err != nil {
		return fmt.Errorf("register helper.explain: %w", err)
	}
	return nil
}

func (d *Daemon) helperContext(ctx context.Context, store *globaldb.Store, req HelperContextRequest) (HelperContextResponse, error) {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return HelperContextResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	session, err := store.GetSession(ctx, workspaceID)
	if err != nil {
		return HelperContextResponse{}, mapWorkspaceStoreError(err, workspaceID)
	}
	defaults, err := d.helperDefaults()
	if err != nil {
		return HelperContextResponse{}, err
	}
	profiles, err := helperProfiles(ctx, store, session.ID)
	if err != nil {
		return HelperContextResponse{}, err
	}
	finalResponses, err := helperFinalResponses(ctx, store, session.ID)
	if err != nil {
		return HelperContextResponse{}, err
	}
	telemetry, err := helperTelemetry(ctx, store, session.ID)
	if err != nil {
		return HelperContextResponse{}, err
	}
	proofs, err := d.workspaceProofs(ctx, store, session.ID)
	if err != nil {
		return HelperContextResponse{}, err
	}
	learnings, err := helperWorkflowLearnings(ctx, store, session, proofs)
	if err != nil {
		return HelperContextResponse{}, err
	}
	resp := HelperContextResponse{Workspace: helperWorkspaceSummary(ctx, store, session), Defaults: defaults, Profiles: profiles, FinalResponses: finalResponses, Telemetry: telemetry, Proofs: proofs, Health: d.helperHealth(ctx, store, session), WorkflowLearnings: learnings, Docs: helperDocs(), Explanations: helperExplanationTopics()}
	if session.Kind == "system" {
		resp.Workspaces, err = helperWorkspaceSummaries(ctx, store)
		if err != nil {
			return HelperContextResponse{}, err
		}
	}
	return resp, nil
}

func (d *Daemon) helperHealth(ctx context.Context, store *globaldb.Store, session *globaldb.Session) HelperHealthSummary {
	_, cfgErr := readJSONConfig(d.configPath)
	_, wsErr := store.GetSession(ctx, session.ID)
	return HelperHealthSummary{DaemonVersion: d.version, ConfigReadable: cfgErr == nil, WorkspaceAvailable: wsErr == nil, WorkspaceKind: session.Kind}
}

func (d *Daemon) helperExplain(ctx context.Context, store *globaldb.Store, req HelperExplainRequest) (HelperExplainResponse, error) {
	topic := strings.ToLower(strings.TrimSpace(req.Topic))
	if topic == "" {
		return HelperExplainResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "topic is required", map[string]any{"reason": "missing_topic"})
	}
	if strings.TrimSpace(req.WorkspaceID) == "" {
		return HelperExplainResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", map[string]any{"reason": "missing_workspace_id"})
	}
	if _, err := store.GetSession(ctx, req.WorkspaceID); err != nil {
		return HelperExplainResponse{}, mapWorkspaceStoreError(err, req.WorkspaceID)
	}
	switch topic {
	case "profile", "profiles":
		profiles, err := helperProfiles(ctx, store, req.WorkspaceID)
		if err != nil {
			return HelperExplainResponse{}, err
		}
		return HelperExplainResponse{Topic: "profile", Explanation: fmt.Sprintf("Profiles are workspace-scoped run configurations. This workspace has %d configured profile(s); helper is the conventional default helper profile when present.", len(profiles)), Anchors: []string{"agent.profile.list", "agent.profile.helper.get", "helper profile convention"}}, nil
	case "harness":
		return HelperExplainResponse{Topic: "harness", Explanation: "A harness is the external agent runtime Ari launches, such as codex, opencode, or claude-code. Ari stores local state and passes scoped context to the harness.", Anchors: []string{"default_harness", "agent.spawn", "agent.profile.run"}}, nil
	case "workspace type", "workspace":
		return HelperExplainResponse{Topic: "workspace type", Explanation: "Workspace type controls scope: system is the folderless Ari/local-machine starter workspace, while project workspaces are folder-backed project scopes.", Anchors: []string{"workspace_kind", "workspace.system.ensure", "workspace.get"}}, nil
	case "telemetry":
		return HelperExplainResponse{Topic: "telemetry", Explanation: "Telemetry summarizes Ari-owned run evidence such as status, token/cost knowledge, exit code, process metrics, and orphan state when available.", Anchors: []string{"telemetry.rollup", "agent_run_telemetry"}}, nil
	case "final response":
		return HelperExplainResponse{Topic: "final response", Explanation: "A final response is the durable answer or outcome Ari records for a run, including status, text, and evidence links where available.", Anchors: []string{"final_response.get", "final_response.list"}}, nil
	case "tool grant", "tool grants":
		return HelperExplainResponse{Topic: "tool grant", Explanation: "Tool grants are Ari-owned capabilities exposed through scoped tool calls. Read tools are read-only; write tools require a pre-issued, single-use approval marker.", Anchors: []string{"ari.tool.list", "ari.tool.call", "approval marker"}}, nil
	case "restart-required setting", "restart required":
		return HelperExplainResponse{Topic: "restart-required setting", Explanation: "A restart-required setting was written to config but may not affect already-running daemon or harness processes until restart or reload support exists.", Anchors: []string{"ari.defaults.set", "restart_required"}}, nil
	case "latest failed run", "latest failure":
		return helperLatestFailedRunExplanation(ctx, store, req.WorkspaceID)
	default:
		return HelperExplainResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "unsupported explanation topic", map[string]any{"reason": "unsupported_topic", "topic": topic})
	}
}

func (d *Daemon) helperDefaults() (map[string]string, error) {
	values, err := readJSONConfig(d.configPath)
	if err != nil {
		return nil, err
	}
	return map[string]string{"default_harness": readConfigString(values, "default_harness"), "preferred_model": readConfigString(values, "preferred_model"), "default_invocation_class": readConfigString(values, "default_invocation_class")}, nil
}

func helperProfiles(ctx context.Context, store *globaldb.Store, workspaceID string) ([]HelperProfileSummary, error) {
	profiles, err := store.ListAgentProfiles(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]HelperProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, HelperProfileSummary{ProfileID: profile.ProfileID, WorkspaceID: profile.WorkspaceID, Name: profile.Name, Harness: profile.Harness, Model: profile.Model, InvocationClass: profile.InvocationClass, PromptSummary: firstOutputLine(profile.Prompt)})
	}
	return out, nil
}

func helperWorkspaceSummaries(ctx context.Context, store *globaldb.Store) ([]HelperWorkspaceSummary, error) {
	sessions, err := store.ListSessions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]HelperWorkspaceSummary, 0, len(sessions))
	for i := range sessions {
		out = append(out, helperWorkspaceSummary(ctx, store, &sessions[i]))
	}
	return out, nil
}

func helperWorkspaceSummary(ctx context.Context, store *globaldb.Store, session *globaldb.Session) HelperWorkspaceSummary {
	count := 0
	if folders, err := store.ListFolders(ctx, session.ID); err == nil {
		count = len(folders)
	}
	return HelperWorkspaceSummary{WorkspaceID: session.ID, Name: session.Name, Kind: session.Kind, Status: session.Status, OriginRoot: session.OriginRoot, FolderCount: count}
}

func helperFinalResponses(ctx context.Context, store *globaldb.Store, workspaceID string) ([]HelperFinalResponse, error) {
	responses, err := store.ListFinalResponses(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]HelperFinalResponse, 0, len(responses))
	for _, response := range responses {
		out = append(out, HelperFinalResponse{FinalResponseID: response.FinalResponseID, RunID: response.RunID, Status: response.Status, Summary: firstOutputLine(response.Text)})
	}
	return out, nil
}

func helperTelemetry(ctx context.Context, store *globaldb.Store, workspaceID string) ([]HelperTelemetrySummary, error) {
	rollups, err := store.RollupAgentRunTelemetry(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]HelperTelemetrySummary, 0, len(rollups))
	for _, rollup := range rollups {
		out = append(out, HelperTelemetrySummary{ProfileID: rollup.Group.ProfileID, ProfileName: rollup.Group.ProfileName, Harness: rollup.Group.Harness, Model: rollup.Group.Model, Runs: rollup.Runs, Completed: rollup.Completed, Failed: rollup.Failed})
	}
	return out, nil
}

func helperWorkflowLearnings(ctx context.Context, store *globaldb.Store, session *globaldb.Session, proofs []ProofResultSummary) ([]WorkflowLearning, error) {
	learnings := make([]WorkflowLearning, 0)
	responses, err := store.ListFinalResponses(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	for _, response := range responses {
		if strings.TrimSpace(response.Text) == "" {
			continue
		}
		learnings = append(learnings, WorkflowLearning{SourceKind: "final_response", SourceID: response.RunID, Summary: firstOutputLine(response.Text)})
	}
	for _, proof := range proofs {
		summary := strings.TrimSpace(proof.Command)
		if proof.Status != "" {
			summary = strings.TrimSpace(proof.Status + ": " + summary)
		}
		if proof.LogSummary != "" {
			summary = strings.TrimSpace(summary + " — " + proof.LogSummary)
		}
		learnings = append(learnings, WorkflowLearning{SourceKind: "proof", SourceID: proof.SourceID, Summary: summary})
	}
	if session.Kind == "project" && strings.TrimSpace(session.OriginRoot) != "" {
		learnings = append(learnings, helperAriWorkflowLearnings(session.OriginRoot)...)
	}
	return learnings, nil
}

func helperAriWorkflowLearnings(originRoot string) []WorkflowLearning {
	activePath := filepath.Join(originRoot, ".ari", "active")
	entries, err := os.ReadDir(activePath)
	if err != nil {
		return nil
	}
	learnings := make([]WorkflowLearning, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		statePath := filepath.Join(activePath, entry.Name(), "STATE.json")
		summary := ".ari active workflow artifacts are present for this project."
		if body, err := os.ReadFile(statePath); err == nil {
			var state struct {
				CurrentPhase string `json:"current_phase"`
				CurrentTask  string `json:"current_task"`
				Next         string `json:"next"`
				Status       string `json:"status"`
			}
			if json.Unmarshal(body, &state) == nil {
				parts := []string{}
				if state.CurrentPhase != "" {
					parts = append(parts, state.CurrentPhase)
				}
				if state.CurrentTask != "" {
					parts = append(parts, state.CurrentTask)
				}
				if state.Next != "" {
					parts = append(parts, "next: "+state.Next)
				}
				if state.Status != "" {
					parts = append(parts, "status: "+state.Status)
				}
				if len(parts) > 0 {
					summary = strings.Join(parts, "; ")
				}
			}
		}
		learnings = append(learnings, WorkflowLearning{SourceKind: "workflow_artifact", SourceID: ".ari active workflow " + entry.Name(), Summary: summary})
	}
	return learnings
}

func helperLatestFailedRunExplanation(ctx context.Context, store *globaldb.Store, workspaceID string) (HelperExplainResponse, error) {
	responses, err := store.ListFinalResponses(ctx, workspaceID)
	if err != nil {
		return HelperExplainResponse{}, err
	}
	for _, response := range responses {
		if response.Status != "failed" {
			continue
		}
		return HelperExplainResponse{Topic: "latest failed run", Explanation: firstOutputLine(response.Text), Anchors: []string{"final_response.list", "ari.run.explain_latest"}, RunID: response.RunID}, nil
	}
	return HelperExplainResponse{Topic: "latest failed run", Explanation: "No failed final response records are available for this workspace yet.", Anchors: []string{"final_response.list"}}, nil
}

func helperDocs() []string {
	return []string{"ari init: choose the default harness and create the system helper", "ari agent spawn --workspace system: start the system helper", "ari workspace show: inspect workspace kind, folders, and origin"}
}

func helperExplanationTopics() []string {
	return []string{"profile", "harness", "workspace type", "telemetry", "final response", "tool grant", "restart-required setting", "latest failed run"}
}
