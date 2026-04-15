package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type AgentSpawnRequest struct {
	WorkspaceID       string   `json:"workspace_id"`
	Name              string   `json:"name,omitempty"`
	Harness           string   `json:"harness,omitempty"`
	Command           string   `json:"command"`
	Args              []string `json:"args"`
	ExecutionRootPath string   `json:"execution_root_path,omitempty"`
}

type AgentSpawnResponse struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

type AgentListRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type AgentSummary struct {
	AgentID   string `json:"agent_id"`
	Name      string `json:"name,omitempty"`
	Command   string `json:"command"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
}

type AgentListResponse struct {
	Agents []AgentSummary `json:"agents"`
}

type AgentGetRequest struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
}

type AgentGetResponse struct {
	AgentID            string          `json:"agent_id"`
	WorkspaceID        string          `json:"workspace_id"`
	Name               string          `json:"name,omitempty"`
	Command            string          `json:"command"`
	Args               string          `json:"args"`
	Status             string          `json:"status"`
	ExitCode           *int            `json:"exit_code"`
	StartedAt          string          `json:"started_at"`
	StoppedAt          string          `json:"stopped_at,omitempty"`
	Harness            string          `json:"harness,omitempty"`
	HarnessResumableID string          `json:"harness_resumable_id,omitempty"`
	HarnessMetadata    json.RawMessage `json:"harness_metadata,omitempty"`
}

type AgentSendRequest struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	Input       string `json:"input"`
}

type AgentSendResponse struct {
	Status string `json:"status"`
}

type AgentOutputRequest struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
}

type AgentOutputResponse struct {
	Output string `json:"output"`
}

type AgentStopRequest struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
}

type AgentStopResponse struct {
	Status string `json:"status"`
}

const maxRetainedAgentLogs = 128

var stopAgentProcess = func(proc *process.Process) error {
	return proc.Stop()
}

var updateAgentStatus = func(store *globaldb.Store, ctx context.Context, params globaldb.UpdateAgentStatusParams) error {
	return store.UpdateAgentStatus(ctx, params)
}

var agentHarnessProjector HarnessProjector = defaultHarnessProjector{}

func (d *Daemon) registerAgentMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSpawnRequest, AgentSpawnResponse]{
		Name:        "agent.spawn",
		Description: "Spawn an agent process in a workspace",
		Handler: func(ctx context.Context, req AgentSpawnRequest) (AgentSpawnResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentSpawnResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}

			session, err := store.GetSession(ctx, sessionID)
			if err != nil {
				return AgentSpawnResponse{}, mapWorkspaceStoreError(err, sessionID)
			}
			if session.Status == "closed" {
				return AgentSpawnResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace is closed", sessionID)
			}

			primaryFolder, err := lookupPrimaryFolder(ctx, store, sessionID)
			if err != nil {
				if errors.Is(err, errNoPrimaryFolder) {
					return AgentSpawnResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace has no primary folder", sessionID)
				}
				return AgentSpawnResponse{}, mapAgentStoreError(err, sessionID)
			}
			executionRootPath, err := validateWorkspaceExecutionRootPath(ctx, store, sessionID, req.ExecutionRootPath)
			if err != nil {
				return AgentSpawnResponse{}, err
			}
			if executionRootPath == "" {
				executionRootPath = primaryFolder
			}

			launcher, err := resolveAgentLauncher(req.Harness)
			if err != nil {
				return AgentSpawnResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), sessionID)
			}

			launchSpec, err := launcher.prepare(req.Command, req.Args)
			if err != nil {
				return AgentSpawnResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), sessionID)
			}

			projector := agentHarnessProjector
			if projector == nil {
				projector = defaultHarnessProjector{}
			}

			harnessName := strings.TrimSpace(req.Harness)
			if harnessName == "" {
				harnessName = inferHarnessFromCommand(launchSpec.Command)
			}
			harnessIdentity := projector.Project(harnessName, launchSpec.Args)

			proc, err := process.New(launchSpec.Command, launchSpec.Args, process.Options{Dir: executionRootPath})
			if err != nil {
				return AgentSpawnResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), sessionID)
			}
			if err := proc.Start(); err != nil {
				return AgentSpawnResponse{}, fmt.Errorf("start agent process: %w", err)
			}

			agentID, err := newAgentID()
			if err != nil {
				_ = proc.Stop()
				_, _ = proc.Wait()
				return AgentSpawnResponse{}, fmt.Errorf("generate agent id: %w", err)
			}

			startedAt := time.Now().UTC().Format(time.RFC3339Nano)
			createParams := globaldb.CreateAgentParams{
				AgentID:            agentID,
				WorkspaceID:        sessionID,
				Command:            launchSpec.Command,
				Args:               encodeArgs(launchSpec.Args),
				Status:             "running",
				StartedAt:          startedAt,
				Harness:            harnessIdentity.Harness,
				HarnessResumableID: harnessIdentity.ResumableID,
				HarnessMetadata:    harnessIdentity.Metadata,
			}
			if name := strings.TrimSpace(req.Name); name != "" {
				createParams.Name = &name
			}
			if err := store.CreateAgent(ctx, createParams); err != nil {
				_ = proc.Stop()
				_, _ = proc.Wait()
				return AgentSpawnResponse{}, mapAgentStoreError(err, sessionID)
			}

			d.setAgentProcess(agentID, proc)
			d.agentWG.Add(1)
			go d.waitForAgentExit(context.Background(), agentID, sessionID, store, proc)

			return AgentSpawnResponse{AgentID: agentID, Status: "running"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.spawn: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentListRequest, AgentListResponse]{
		Name:        "agent.list",
		Description: "List agents for a workspace",
		Handler: func(ctx context.Context, req AgentListRequest) (AgentListResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return AgentListResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			agents, err := store.ListAgents(ctx, sessionID)
			if err != nil {
				return AgentListResponse{}, mapAgentStoreError(err, sessionID)
			}

			out := make([]AgentSummary, 0, len(agents))
			for _, agent := range agents {
				item := AgentSummary{
					AgentID:   agent.AgentID,
					Command:   agent.Command,
					Status:    agent.Status,
					StartedAt: agent.StartedAt,
				}
				if agent.Name != nil {
					item.Name = *agent.Name
				}
				out = append(out, item)
			}

			return AgentListResponse{Agents: out}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.list: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentGetRequest, AgentGetResponse]{
		Name:        "agent.get",
		Description: "Get agent details",
		Handler: func(ctx context.Context, req AgentGetRequest) (AgentGetResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentGetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			identifier := strings.TrimSpace(req.AgentID)
			if identifier == "" {
				return AgentGetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "agent_id is required", sessionID)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return AgentGetResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			agent, err := lookupAgentByIdentifier(ctx, store, sessionID, identifier)
			if err != nil {
				return AgentGetResponse{}, mapAgentStoreError(err, sessionID)
			}

			resp := AgentGetResponse{
				AgentID:     agent.AgentID,
				WorkspaceID: agent.WorkspaceID,
				Command:     agent.Command,
				Args:        agent.Args,
				Status:      agent.Status,
				ExitCode:    agent.ExitCode,
				StartedAt:   agent.StartedAt,
			}
			if agent.Name != nil {
				resp.Name = *agent.Name
			}
			if agent.StoppedAt != nil {
				resp.StoppedAt = *agent.StoppedAt
			}
			if agent.Harness != nil {
				resp.Harness = *agent.Harness
			}
			if agent.HarnessResumableID != nil {
				resp.HarnessResumableID = *agent.HarnessResumableID
			}
			if strings.TrimSpace(agent.HarnessMetadata) != "" && json.Valid([]byte(agent.HarnessMetadata)) {
				resp.HarnessMetadata = json.RawMessage(agent.HarnessMetadata)
			}

			return resp, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.get: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentSendRequest, AgentSendResponse]{
		Name:        "agent.send",
		Description: "Send input to a running agent",
		Handler: func(ctx context.Context, req AgentSendRequest) (AgentSendResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			identifier := strings.TrimSpace(req.AgentID)
			if identifier == "" {
				return AgentSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "agent_id is required", sessionID)
			}
			if req.Input == "" {
				return AgentSendResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "input is required", sessionID)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return AgentSendResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			agent, err := lookupAgentByIdentifier(ctx, store, sessionID, identifier)
			if err != nil {
				return AgentSendResponse{}, mapAgentStoreError(err, sessionID)
			}
			if d.hasActiveAttachForAgent(agent.AgentID) {
				return AgentSendResponse{}, rpc.NewHandlerError(rpc.AgentAlreadyAttached, "agent has an active attach session", sessionID)
			}

			proc, ok := d.getAgentProcess(agent.AgentID)
			if !ok {
				return AgentSendResponse{}, rpc.NewHandlerError(rpc.AgentNotRunning, "agent is not running", sessionID)
			}
			if err := writeAllBytes(proc, []byte(req.Input)); err != nil {
				return AgentSendResponse{}, rpc.NewHandlerError(rpc.AgentNotRunning, "agent is not running", sessionID)
			}

			return AgentSendResponse{Status: "sent"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.send: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentOutputRequest, AgentOutputResponse]{
		Name:        "agent.output",
		Description: "Get agent output snapshot",
		Handler: func(ctx context.Context, req AgentOutputRequest) (AgentOutputResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentOutputResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			identifier := strings.TrimSpace(req.AgentID)
			if identifier == "" {
				return AgentOutputResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "agent_id is required", sessionID)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return AgentOutputResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			agent, err := lookupAgentByIdentifier(ctx, store, sessionID, identifier)
			if err != nil {
				return AgentOutputResponse{}, mapAgentStoreError(err, sessionID)
			}
			if agent.Status != "running" {
				if output, ok := d.getAgentOutput(agent.AgentID); ok {
					return AgentOutputResponse{Output: output}, nil
				}
			}

			proc, ok := d.getAgentProcess(agent.AgentID)
			if !ok {
				if output, exists := d.getAgentOutput(agent.AgentID); exists {
					return AgentOutputResponse{Output: output}, nil
				}
				return AgentOutputResponse{Output: ""}, nil
			}

			return AgentOutputResponse{Output: string(proc.OutputSnapshot())}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.output: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentStopRequest, AgentStopResponse]{
		Name:        "agent.stop",
		Description: "Stop running agent process",
		Handler: func(ctx context.Context, req AgentStopRequest) (AgentStopResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentStopResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			identifier := strings.TrimSpace(req.AgentID)
			if identifier == "" {
				return AgentStopResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "agent_id is required", sessionID)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return AgentStopResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			agent, err := lookupAgentByIdentifier(ctx, store, sessionID, identifier)
			if err != nil {
				return AgentStopResponse{}, mapAgentStoreError(err, sessionID)
			}
			if agent.Status != "running" {
				return AgentStopResponse{Status: agent.Status}, nil
			}

			proc, ok := d.getAgentProcess(agent.AgentID)
			if !ok {
				return AgentStopResponse{Status: "lost"}, nil
			}

			d.markAgentStopRequested(agent.AgentID)
			go func() {
				_ = stopAgentProcess(proc)
			}()

			return AgentStopResponse{Status: "stopping"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.stop: %w", err)
	}

	return nil
}

func inferHarnessFromCommand(command string) string {
	base := strings.TrimSpace(filepath.Base(strings.TrimSpace(command)))
	if base == "" {
		return ""
	}
	base = strings.TrimSuffix(base, ".exe")
	if _, ok := harnessDefinitions[base]; ok {
		return base
	}
	return ""
}

func parseHarnessResumableID(harness string, args []string) string {
	if len(args) == 0 {
		return ""
	}
	flag := resumableFlagForHarness(harness)
	if flag == "" {
		return ""
	}
	for index := 0; index < len(args); index++ {
		argument := strings.TrimSpace(args[index])
		if argument == flag {
			if index+1 < len(args) {
				next := strings.TrimSpace(args[index+1])
				if next != "" && !strings.HasPrefix(next, "-") {
					return next
				}
			}
			return ""
		}
		if strings.HasPrefix(argument, flag+"=") {
			return strings.TrimSpace(strings.TrimPrefix(argument, flag+"="))
		}
	}
	return ""
}

func (d *Daemon) waitForAgentExit(ctx context.Context, agentID, sessionID string, store *globaldb.Store, proc *process.Process) {
	if ctx == nil {
		ctx = context.Background()
	}

	defer d.agentWG.Done()
	defer d.deleteAgentProcess(agentID)
	defer d.clearAttachForAgent(agentID)

	result, err := proc.Wait()
	stoppedAt := time.Now().UTC().Format(time.RFC3339Nano)
	d.setAgentOutput(agentID, string(proc.OutputSnapshot()))

	status := "exited"
	if d.consumeAgentStopRequested(agentID) && err == nil {
		status = "stopped"
	}
	if err != nil {
		status = "lost"
	}

	update := globaldb.UpdateAgentStatusParams{
		WorkspaceID: sessionID,
		AgentID:     agentID,
		Status:      status,
		StoppedAt:   &stoppedAt,
	}
	if err == nil {
		update.ExitCode = &result.ExitCode
	}

	if err := persistAgentStatusWithRetry(ctx, store, update, 5*time.Second); err != nil {
		fallback := globaldb.UpdateAgentStatusParams{
			WorkspaceID: sessionID,
			AgentID:     agentID,
			Status:      "lost",
			StoppedAt:   &stoppedAt,
		}
		_ = persistAgentStatusWithRetry(ctx, store, fallback, 5*time.Second)
	}
}

func (d *Daemon) setAgentProcess(agentID string, proc *process.Process) {
	d.agentMu.Lock()
	d.agents[agentID] = proc
	d.agentMu.Unlock()
}

func (d *Daemon) getAgentProcess(agentID string) (*process.Process, bool) {
	d.agentMu.RLock()
	proc, ok := d.agents[agentID]
	d.agentMu.RUnlock()
	return proc, ok
}

func (d *Daemon) deleteAgentProcess(agentID string) {
	d.agentMu.Lock()
	delete(d.agents, agentID)
	d.agentMu.Unlock()
}

func (d *Daemon) setAgentOutput(agentID, output string) {
	d.agentMu.Lock()
	if _, exists := d.agentLogs[agentID]; !exists {
		d.agentLogOrder = append(d.agentLogOrder, agentID)
	}
	d.agentLogs[agentID] = output
	for len(d.agentLogOrder) > maxRetainedAgentLogs {
		evictID := d.agentLogOrder[0]
		d.agentLogOrder = d.agentLogOrder[1:]
		delete(d.agentLogs, evictID)
	}
	d.agentMu.Unlock()
}

func (d *Daemon) getAgentOutput(agentID string) (string, bool) {
	d.agentMu.RLock()
	output, ok := d.agentLogs[agentID]
	d.agentMu.RUnlock()
	return output, ok
}

func (d *Daemon) markAgentStopRequested(agentID string) {
	d.agentMu.Lock()
	d.agentStops[agentID] = true
	d.agentMu.Unlock()
}

func (d *Daemon) consumeAgentStopRequested(agentID string) bool {
	d.agentMu.Lock()
	requested := d.agentStops[agentID]
	delete(d.agentStops, agentID)
	d.agentMu.Unlock()
	return requested
}

func (d *Daemon) stopAllAgents() {
	d.agentMu.RLock()
	procs := make([]*process.Process, 0, len(d.agents))
	for _, proc := range d.agents {
		procs = append(procs, proc)
	}
	d.agentMu.RUnlock()

	for _, proc := range procs {
		_ = proc.Stop()
	}
}

func mapAgentStoreError(err error, sessionID string) error {
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.AgentNotFound, "agent not found", sessionID)
	}
	if errors.Is(err, globaldb.ErrInvalidInput) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), sessionID)
	}
	return err
}

func lookupAgentByIdentifier(ctx context.Context, store *globaldb.Store, sessionID, identifier string) (*globaldb.Agent, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, fmt.Errorf("%w: agent id is required", globaldb.ErrInvalidInput)
	}

	agent, err := store.GetAgent(ctx, sessionID, identifier)
	if err == nil {
		return agent, nil
	}
	if !errors.Is(err, globaldb.ErrNotFound) {
		return nil, err
	}

	return store.GetAgentByName(ctx, sessionID, identifier)
}

func newAgentID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buf)

	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]), nil
}

func persistAgentStatusWithRetry(ctx context.Context, store *globaldb.Store, update globaldb.UpdateAgentStatusParams, maxDuration time.Duration) error {
	deadline := time.Now().Add(maxDuration)
	var lastErr error

	for {
		if err := updateAgentStatus(store, ctx, update); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if time.Now().After(deadline) {
			return lastErr
		}

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func writeAllBytes(writer io.Writer, data []byte) error {
	remaining := data
	for len(remaining) > 0 {
		written, err := writer.Write(remaining)
		if err != nil {
			return err
		}
		if written <= 0 {
			return fmt.Errorf("write all bytes: wrote zero bytes")
		}
		if written > len(remaining) {
			return fmt.Errorf("write all bytes: wrote more bytes than requested")
		}
		remaining = remaining[written:]
	}

	return nil
}
