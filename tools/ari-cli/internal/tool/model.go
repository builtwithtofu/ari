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
	Name    string
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

	args, err := decodeCommandArgs(record.Args)
	if err != nil {
		return Tool{}, err
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

func FromWorkspaceCommandDefinition(record globaldb.WorkspaceCommandDefinition) (Tool, error) {
	if strings.TrimSpace(record.CommandID) == "" {
		return Tool{}, fmt.Errorf("command id is required")
	}
	if strings.TrimSpace(record.WorkspaceID) == "" {
		return Tool{}, fmt.Errorf("workspace id is required")
	}
	if strings.TrimSpace(record.Name) == "" {
		return Tool{}, fmt.Errorf("command name is required")
	}
	if strings.TrimSpace(record.Command) == "" {
		return Tool{}, fmt.Errorf("command is required")
	}

	args, err := decodeCommandArgs(record.Args)
	if err != nil {
		return Tool{}, err
	}

	return Tool{
		ToolID:      record.CommandID,
		WorkspaceID: record.WorkspaceID,
		Type:        TypeCommand,
		StartedAt:   record.CreatedAt,
		Command: &Command{
			Name:    record.Name,
			Command: record.Command,
			Args:    args,
		},
	}, nil
}

func decodeCommandArgs(rawArgs string) ([]string, error) {
	args := make([]string, 0)
	if strings.TrimSpace(rawArgs) == "" {
		return args, nil
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return nil, fmt.Errorf("decode command args: %w", err)
	}
	return args, nil
}
