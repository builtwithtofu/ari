package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type ContextProjectRequest struct {
	WorkspaceID string   `json:"workspace_id"`
	TaskID      string   `json:"task_id"`
	Goal        string   `json:"goal"`
	Constraints []string `json:"constraints"`
}

type ContextProjectResponse struct {
	Packet ContextPacket `json:"packet"`
}

type ContextPacket struct {
	ID                 string            `json:"id"`
	WorkspaceID        string            `json:"workspace_id"`
	TaskID             string            `json:"task_id"`
	PacketHash         string            `json:"packet_hash"`
	DiffHash           string            `json:"diff_hash"`
	Sections           []ContextSection  `json:"sections"`
	IncludedFilePaths  []string          `json:"included_file_paths"`
	IncludedCommandIDs []string          `json:"included_command_ids"`
	IncludedProofIDs   []string          `json:"included_proof_ids"`
	Omissions          []ContextOmission `json:"omissions"`
	CreatedAt          string            `json:"created_at"`
}

type ContextSection struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ContextOmission struct {
	Kind     string `json:"kind"`
	Reason   string `json:"reason"`
	SourceID string `json:"source_id"`
}

func (d *Daemon) registerContextMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}
	if err := rpc.RegisterMethod(registry, rpc.Method[ContextProjectRequest, ContextProjectResponse]{
		Name:        "context.project",
		Description: "Project an inspectable context packet from workspace facts",
		Handler: func(ctx context.Context, req ContextProjectRequest) (ContextProjectResponse, error) {
			packet, err := d.projectContextPacket(ctx, store, req)
			if err != nil {
				return ContextProjectResponse{}, err
			}
			return ContextProjectResponse{Packet: packet}, nil
		},
	}); err != nil {
		return fmt.Errorf("register context.project: %w", err)
	}
	return nil
}

func (d *Daemon) projectContextPacket(ctx context.Context, store *globaldb.Store, req ContextProjectRequest) (ContextPacket, error) {
	workspaceID, roots, err := requireWorkspaceRoots(ctx, store, req.WorkspaceID)
	if err != nil {
		return ContextPacket{}, err
	}
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return ContextPacket{}, rpc.NewHandlerError(rpc.InvalidParams, "task_id is required", workspaceID)
	}
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		return ContextPacket{}, rpc.NewHandlerError(rpc.InvalidParams, "goal is required", workspaceID)
	}
	diff := buildDiffSummary(roots)
	commands, err := store.ListWorkspaceCommandDefinitions(ctx, workspaceID)
	if err != nil {
		return ContextPacket{}, mapCommandStoreError(err, workspaceID)
	}
	proofs, err := d.workspaceProofs(ctx, store, workspaceID)
	if err != nil {
		return ContextPacket{}, err
	}

	includedCommandIDs := make([]string, 0, len(commands))
	commandLines := make([]string, 0, len(commands))
	for _, command := range commands {
		includedCommandIDs = append(includedCommandIDs, command.CommandID)
		commandLines = append(commandLines, command.Name+": "+commandLabel(command.Command, command.Args))
	}
	sort.Strings(includedCommandIDs)
	sort.Strings(commandLines)

	includedProofIDs := make([]string, 0, len(proofs))
	proofLines := make([]string, 0, len(proofs))
	omissions := make([]ContextOmission, 0)
	for _, proof := range proofs {
		includedProofIDs = append(includedProofIDs, proof.ID)
		proofLines = append(proofLines, proof.ID+": "+proof.Status+" "+proof.Command)
		if strings.TrimSpace(proof.LogSummary) != "" {
			omissions = append(omissions, ContextOmission{Kind: "logs", Reason: "summarized", SourceID: proof.SourceID})
		}
	}
	sort.Strings(includedProofIDs)
	sort.Strings(proofLines)

	constraints := append([]string(nil), req.Constraints...)
	for i := range constraints {
		constraints[i] = strings.TrimSpace(constraints[i])
	}
	constraints = compactStrings(constraints)

	sections := []ContextSection{
		{Name: "goal", Content: goal},
		{Name: "constraints", Content: strings.Join(constraints, "\n")},
		{Name: "workspace", Content: fmt.Sprintf("VCS: %s; changed files: %d", diff.Backend, diff.ChangedFiles)},
		{Name: "commands", Content: strings.Join(commandLines, "\n")},
		{Name: "latest_proof", Content: strings.Join(proofLines, "\n")},
		{Name: "omissions", Content: renderOmissions(omissions)},
	}
	diffHash := stableHash(diff)
	packet := ContextPacket{
		WorkspaceID:        workspaceID,
		TaskID:             taskID,
		DiffHash:           diffHash,
		Sections:           sections,
		IncludedFilePaths:  append([]string(nil), diff.Files...),
		IncludedCommandIDs: includedCommandIDs,
		IncludedProofIDs:   includedProofIDs,
		Omissions:          omissions,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	packet.PacketHash = stableHash(struct {
		WorkspaceID        string
		TaskID             string
		DiffHash           string
		Sections           []ContextSection
		IncludedFilePaths  []string
		IncludedCommandIDs []string
		IncludedProofIDs   []string
		Omissions          []ContextOmission
	}{packet.WorkspaceID, packet.TaskID, packet.DiffHash, packet.Sections, packet.IncludedFilePaths, packet.IncludedCommandIDs, packet.IncludedProofIDs, packet.Omissions})
	packet.ID = "ctx_" + strings.TrimPrefix(packet.PacketHash, "sha256:")[:12]
	return packet, nil
}

func compactStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			out = append(out, item)
		}
	}
	return out
}

func renderOmissions(omissions []ContextOmission) string {
	if len(omissions) == 0 {
		return "none"
	}
	lines := make([]string, 0, len(omissions))
	for _, omission := range omissions {
		lines = append(lines, omission.Kind+":"+omission.Reason+":"+omission.SourceID)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func stableHash(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("stable hash marshal failed: %v", err))
	}
	hash := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(hash[:])
}
