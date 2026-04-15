package tool

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

type Type string

const (
	TypeCommand Type = "command"
)

type Command struct {
	Command string
	Args    []string
}

type Tool struct {
	ToolID      string
	WorkspaceID string
	Type        Type
	Status      string
	StartedAt   string
	FinishedAt  *string
	ExitCode    *int
	Command     *Command
}

func FromCommandRecord(record globaldb.Command) (Tool, error) {
	if strings.TrimSpace(record.CommandID) == "" {
		return Tool{}, fmt.Errorf("command id is required")
	}
	if strings.TrimSpace(record.WorkspaceID) == "" {
		return Tool{}, fmt.Errorf("workspace id is required")
	}
	if strings.TrimSpace(record.Command) == "" {
		return Tool{}, fmt.Errorf("command is required")
	}

	args := make([]string, 0)
	if strings.TrimSpace(record.Args) != "" {
		if err := json.Unmarshal([]byte(record.Args), &args); err != nil {
			return Tool{}, fmt.Errorf("decode command args: %w", err)
		}
	}

	return Tool{
		ToolID:      record.CommandID,
		WorkspaceID: record.WorkspaceID,
		Type:        TypeCommand,
		Status:      record.Status,
		StartedAt:   record.StartedAt,
		FinishedAt:  record.FinishedAt,
		ExitCode:    record.ExitCode,
		Command: &Command{
			Command: record.Command,
			Args:    args,
		},
	}, nil
}
