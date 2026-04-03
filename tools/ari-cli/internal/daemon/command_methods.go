package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type CommandRunRequest struct {
	SessionID string   `json:"session_id"`
	Command   string   `json:"command"`
	Args      []string `json:"args"`
}

type CommandRunResponse struct {
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
}

type CommandListRequest struct {
	SessionID string `json:"session_id"`
}

type CommandSummary struct {
	CommandID string `json:"command_id"`
	Command   string `json:"command"`
	Status    string `json:"status"`
	StartedAt string `json:"started_at"`
}

type CommandListResponse struct {
	Commands []CommandSummary `json:"commands"`
}

type CommandGetRequest struct {
	SessionID string `json:"session_id"`
	CommandID string `json:"command_id"`
}

type CommandGetResponse struct {
	CommandID  string `json:"command_id"`
	SessionID  string `json:"session_id"`
	Command    string `json:"command"`
	Args       string `json:"args"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type CommandOutputRequest struct {
	SessionID string `json:"session_id"`
	CommandID string `json:"command_id"`
}

type CommandOutputResponse struct {
	Output string `json:"output"`
}

type CommandStopRequest struct {
	SessionID string `json:"session_id"`
	CommandID string `json:"command_id"`
}

type CommandStopResponse struct {
	Status string `json:"status"`
}

const maxRetainedCommandLogs = 128

var stopCommandProcess = func(proc *process.Process) error {
	return proc.Stop()
}

var updateCommandStatus = func(store *globaldb.Store, ctx context.Context, params globaldb.UpdateCommandStatusParams) error {
	return store.UpdateCommandStatus(ctx, params)
}

func (d *Daemon) registerCommandMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[CommandRunRequest, CommandRunResponse]{
		Name:        "command.run",
		Description: "Run a command in a session",
		Handler: func(ctx context.Context, req CommandRunRequest) (CommandRunResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return CommandRunResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			session, err := store.GetSession(ctx, sessionID)
			if err != nil {
				return CommandRunResponse{}, mapSessionStoreError(err, sessionID)
			}
			if session.Status == "closed" {
				return CommandRunResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session is closed", sessionID)
			}
			command := strings.TrimSpace(req.Command)
			if command == "" {
				return CommandRunResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "command is required", sessionID)
			}

			primaryFolder, err := lookupPrimaryFolder(ctx, store, sessionID)
			if err != nil {
				if errors.Is(err, errNoPrimaryFolder) {
					return CommandRunResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session has no primary folder", sessionID)
				}
				return CommandRunResponse{}, mapCommandStoreError(err, sessionID)
			}

			proc, err := process.New(command, req.Args, process.Options{Dir: primaryFolder})
			if err != nil {
				return CommandRunResponse{}, rpc.NewHandlerError(rpc.InvalidParams, err.Error(), sessionID)
			}
			if err := proc.Start(); err != nil {
				return CommandRunResponse{}, fmt.Errorf("start command process: %w", err)
			}

			commandID, err := newCommandID()
			if err != nil {
				_ = proc.Stop()
				_, _ = proc.Wait()
				return CommandRunResponse{}, fmt.Errorf("generate command id: %w", err)
			}

			startedAt := time.Now().UTC().Format(time.RFC3339Nano)
			if err := store.CreateCommand(ctx, globaldb.CreateCommandParams{
				CommandID: commandID,
				SessionID: sessionID,
				Command:   command,
				Args:      encodeArgs(req.Args),
				Status:    "running",
				StartedAt: startedAt,
			}); err != nil {
				_ = proc.Stop()
				_, _ = proc.Wait()
				return CommandRunResponse{}, mapCommandStoreError(err, sessionID)
			}

			d.setCommandProcess(commandID, proc)
			d.commandWG.Add(1)
			go d.waitForCommandExit(commandID, sessionID, store, proc)

			return CommandRunResponse{CommandID: commandID, Status: "running"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register command.run: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[CommandListRequest, CommandListResponse]{
		Name:        "command.list",
		Description: "List commands for a session",
		Handler: func(ctx context.Context, req CommandListRequest) (CommandListResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return CommandListResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return CommandListResponse{}, mapSessionStoreError(err, sessionID)
			}

			commands, err := store.ListCommands(ctx, sessionID)
			if err != nil {
				return CommandListResponse{}, mapCommandStoreError(err, sessionID)
			}

			out := make([]CommandSummary, 0, len(commands))
			for _, command := range commands {
				out = append(out, CommandSummary{
					CommandID: command.CommandID,
					Command:   command.Command,
					Status:    command.Status,
					StartedAt: command.StartedAt,
				})
			}

			return CommandListResponse{Commands: out}, nil
		},
	}); err != nil {
		return fmt.Errorf("register command.list: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[CommandGetRequest, CommandGetResponse]{
		Name:        "command.get",
		Description: "Get command details",
		Handler: func(ctx context.Context, req CommandGetRequest) (CommandGetResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return CommandGetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			commandID := strings.TrimSpace(req.CommandID)
			if commandID == "" {
				return CommandGetResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "command_id is required", sessionID)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return CommandGetResponse{}, mapSessionStoreError(err, sessionID)
			}

			command, err := store.GetCommand(ctx, sessionID, commandID)
			if err != nil {
				return CommandGetResponse{}, mapCommandStoreError(err, sessionID)
			}

			resp := CommandGetResponse{
				CommandID: command.CommandID,
				SessionID: command.SessionID,
				Command:   command.Command,
				Args:      command.Args,
				Status:    command.Status,
				ExitCode:  command.ExitCode,
				StartedAt: command.StartedAt,
			}
			if command.FinishedAt != nil {
				resp.FinishedAt = *command.FinishedAt
			}

			return resp, nil
		},
	}); err != nil {
		return fmt.Errorf("register command.get: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[CommandOutputRequest, CommandOutputResponse]{
		Name:        "command.output",
		Description: "Get command output snapshot",
		Handler: func(ctx context.Context, req CommandOutputRequest) (CommandOutputResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return CommandOutputResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			commandID := strings.TrimSpace(req.CommandID)
			if commandID == "" {
				return CommandOutputResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "command_id is required", sessionID)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return CommandOutputResponse{}, mapSessionStoreError(err, sessionID)
			}

			if _, err := store.GetCommand(ctx, sessionID, commandID); err != nil {
				return CommandOutputResponse{}, mapCommandStoreError(err, sessionID)
			}

			proc, ok := d.getCommandProcess(commandID)
			if !ok {
				if output, exists := d.getCommandOutput(commandID); exists {
					return CommandOutputResponse{Output: output}, nil
				}
				return CommandOutputResponse{Output: ""}, nil
			}

			return CommandOutputResponse{Output: string(proc.OutputSnapshot())}, nil
		},
	}); err != nil {
		return fmt.Errorf("register command.output: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[CommandStopRequest, CommandStopResponse]{
		Name:        "command.stop",
		Description: "Stop command process",
		Handler: func(ctx context.Context, req CommandStopRequest) (CommandStopResponse, error) {
			sessionID := strings.TrimSpace(req.SessionID)
			if sessionID == "" {
				return CommandStopResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "session_id is required", nil)
			}
			commandID := strings.TrimSpace(req.CommandID)
			if commandID == "" {
				return CommandStopResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "command_id is required", sessionID)
			}
			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return CommandStopResponse{}, mapSessionStoreError(err, sessionID)
			}

			commandRecord, err := store.GetCommand(ctx, sessionID, commandID)
			if err != nil {
				return CommandStopResponse{}, mapCommandStoreError(err, sessionID)
			}
			if commandRecord.Status != "running" {
				return CommandStopResponse{Status: commandRecord.Status}, nil
			}

			proc, ok := d.getCommandProcess(commandID)
			if !ok {
				return CommandStopResponse{Status: "lost"}, nil
			}

			go func() {
				_ = stopCommandProcess(proc)
			}()

			return CommandStopResponse{Status: "stopping"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register command.stop: %w", err)
	}

	return nil
}

func (d *Daemon) waitForCommandExit(commandID, sessionID string, store *globaldb.Store, proc *process.Process) {
	defer d.commandWG.Done()
	defer d.deleteCommandProcess(commandID)

	result, err := proc.Wait()
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	d.setCommandOutput(commandID, string(proc.OutputSnapshot()))

	status := "exited"
	if err != nil {
		status = "lost"
	}

	update := globaldb.UpdateCommandStatusParams{
		SessionID:  sessionID,
		CommandID:  commandID,
		Status:     status,
		FinishedAt: &finishedAt,
	}
	if err == nil {
		update.ExitCode = &result.ExitCode
	}

	if err := persistCommandStatusWithRetry(context.Background(), store, update, 5*time.Second); err != nil {
		fallback := globaldb.UpdateCommandStatusParams{
			SessionID:  sessionID,
			CommandID:  commandID,
			Status:     "lost",
			FinishedAt: &finishedAt,
		}
		_ = persistCommandStatusWithRetry(context.Background(), store, fallback, 5*time.Second)
	}
}

func (d *Daemon) setCommandProcess(commandID string, proc *process.Process) {
	d.commandMu.Lock()
	d.commands[commandID] = proc
	d.commandMu.Unlock()
}

func (d *Daemon) getCommandProcess(commandID string) (*process.Process, bool) {
	d.commandMu.RLock()
	proc, ok := d.commands[commandID]
	d.commandMu.RUnlock()
	return proc, ok
}

func (d *Daemon) deleteCommandProcess(commandID string) {
	d.commandMu.Lock()
	delete(d.commands, commandID)
	d.commandMu.Unlock()
}

func (d *Daemon) setCommandOutput(commandID, output string) {
	d.commandMu.Lock()
	if _, exists := d.commandLogs[commandID]; !exists {
		d.commandLogOrder = append(d.commandLogOrder, commandID)
	}
	d.commandLogs[commandID] = output
	for len(d.commandLogOrder) > maxRetainedCommandLogs {
		evictID := d.commandLogOrder[0]
		d.commandLogOrder = d.commandLogOrder[1:]
		delete(d.commandLogs, evictID)
	}
	d.commandMu.Unlock()
}

func (d *Daemon) getCommandOutput(commandID string) (string, bool) {
	d.commandMu.RLock()
	output, ok := d.commandLogs[commandID]
	d.commandMu.RUnlock()
	return output, ok
}

func (d *Daemon) stopAllCommands() {
	d.commandMu.RLock()
	procs := make([]*process.Process, 0, len(d.commands))
	for _, proc := range d.commands {
		procs = append(procs, proc)
	}
	d.commandMu.RUnlock()

	for _, proc := range procs {
		_ = proc.Stop()
	}
}

var errNoPrimaryFolder = errors.New("session has no primary folder")

func lookupPrimaryFolder(ctx context.Context, store *globaldb.Store, sessionID string) (string, error) {
	folders, err := store.ListFolders(ctx, sessionID)
	if err != nil {
		return "", err
	}

	for _, folder := range folders {
		if folder.IsPrimary {
			return folder.FolderPath, nil
		}
	}

	return "", errNoPrimaryFolder
}

func mapCommandStoreError(err error, sessionID string) error {
	if errors.Is(err, globaldb.ErrNotFound) {
		return rpc.NewHandlerError(rpc.CommandNotFound, "command not found", sessionID)
	}
	if errors.Is(err, globaldb.ErrInvalidInput) {
		return rpc.NewHandlerError(rpc.InvalidParams, err.Error(), sessionID)
	}
	return err
}

func encodeArgs(args []string) string {
	if len(args) == 0 {
		return "[]"
	}

	b := strings.Builder{}
	b.WriteString("[")
	for i, arg := range args {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf("%q", arg))
	}
	b.WriteString("]")

	return b.String()
}

func newCommandID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buf)

	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]), nil
}

func persistCommandStatusWithRetry(ctx context.Context, store *globaldb.Store, update globaldb.UpdateCommandStatusParams, maxDuration time.Duration) error {
	deadline := time.Now().Add(maxDuration)
	var lastErr error

	for {
		if err := updateCommandStatus(store, context.Background(), update); err == nil {
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
