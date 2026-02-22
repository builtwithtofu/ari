package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
)

var allowedCommands = []string{"ls", "cat", "echo", "go", "git", "mkdir", "cp", "mv", "rm"}

type ReadFileTool struct{}

func (t ReadFileTool) Name() string { return "read_file" }

func (t ReadFileTool) Description() string { return "Read contents of a file" }

func (t ReadFileTool) Execute(_ context.Context, args map[string]any) (any, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, errors.New("path is required")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return string(content), nil
}

type WriteFileTool struct{}

func (t WriteFileTool) Name() string { return "write_file" }

func (t WriteFileTool) Description() string { return "Write contents to a file" }

func (t WriteFileTool) Execute(_ context.Context, args map[string]any) (any, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return nil, errors.New("path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return nil, errors.New("content is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}

	return fmt.Sprintf("wrote file: %s", path), nil
}

type RunCommandTool struct{}

func (t RunCommandTool) Name() string { return "run_command" }

func (t RunCommandTool) Description() string { return "Run a shell command" }

func (t RunCommandTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	command, ok := args["command"].(string)
	if !ok || command == "" {
		return nil, errors.New("command is required")
	}

	if !slices.Contains(allowedCommands, command) {
		return nil, fmt.Errorf("command not allowed: %s", command)
	}

	rawArgs, ok := args["args"]
	if !ok {
		rawArgs = []string{}
	}

	commandArgs, err := toStringSlice(rawArgs)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, command, commandArgs...)
	if cwd, ok := args["cwd"].(string); ok && cwd != "" {
		cmd.Dir = cwd
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("command failed: %w\n%s", err, string(output))
	}

	return string(output), nil
}

type AskUserTool struct{}

func (t AskUserTool) Name() string { return "ask_user" }

func (t AskUserTool) Description() string { return "Ask the user a question" }

func (t AskUserTool) Execute(_ context.Context, args map[string]any) (any, error) {
	prompt, ok := args["prompt"].(string)
	if !ok || prompt == "" {
		return nil, errors.New("prompt is required")
	}

	return "user_input_placeholder", nil
}

func toStringSlice(raw any) ([]string, error) {
	if raw == nil {
		return []string{}, nil
	}

	if values, ok := raw.([]string); ok {
		return values, nil
	}

	values, ok := raw.([]any)
	if !ok {
		return nil, errors.New("args must be []string")
	}

	result := make([]string, 0, len(values))
	for _, value := range values {
		str, ok := value.(string)
		if !ok {
			return nil, errors.New("args must be []string")
		}
		result = append(result, str)
	}

	return result, nil
}
