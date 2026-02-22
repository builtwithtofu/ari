package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStubsImplementToolInterface(t *testing.T) {
	var _ Tool = ReadFileTool{}
	var _ Tool = WriteFileTool{}
	var _ Tool = RunCommandTool{}
	var _ Tool = AskUserTool{}
}

func TestReadFileToolExecute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}

	tool := ReadFileTool{}
	out, err := tool.Execute(context.Background(), map[string]any{"path": file})
	if err != nil {
		t.Fatalf("execute read tool: %v", err)
	}

	value, ok := out.(string)
	if !ok {
		t.Fatalf("output type = %T, want string", out)
	}
	if value != "hello" {
		t.Fatalf("output = %q, want %q", value, "hello")
	}
}

func TestReadFileToolExecuteErrors(t *testing.T) {
	t.Parallel()

	tool := ReadFileTool{}

	if _, err := tool.Execute(context.Background(), map[string]any{"path": ""}); err == nil {
		t.Fatal("expected error for empty path")
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"path": "/definitely/not/found"}); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestWriteFileToolExecute(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "nested", "hello.txt")
	tool := WriteFileTool{}

	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    file,
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("execute write tool: %v", err)
	}

	message, ok := out.(string)
	if !ok {
		t.Fatalf("output type = %T, want string", out)
	}
	if !strings.Contains(message, "wrote file") {
		t.Fatalf("output = %q, want confirmation message", message)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("written content = %q, want %q", string(data), "hello world")
	}
}

func TestWriteFileToolExecuteErrors(t *testing.T) {
	t.Parallel()

	tool := WriteFileTool{}

	if _, err := tool.Execute(context.Background(), map[string]any{"path": "", "content": "x"}); err == nil {
		t.Fatal("expected error for empty path")
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"path": "file.txt"}); err == nil {
		t.Fatal("expected error for missing content")
	}
}

func TestRunCommandToolExecute(t *testing.T) {
	t.Parallel()

	tool := RunCommandTool{}
	out, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo",
		"args":    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("execute command tool: %v", err)
	}

	value, ok := out.(string)
	if !ok {
		t.Fatalf("output type = %T, want string", out)
	}
	if strings.TrimSpace(value) != "hello" {
		t.Fatalf("output = %q, want %q", value, "hello")
	}
}

func TestRunCommandToolExecuteErrors(t *testing.T) {
	t.Parallel()

	tool := RunCommandTool{}

	if _, err := tool.Execute(context.Background(), map[string]any{"command": ""}); err == nil {
		t.Fatal("expected error for empty command")
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"command": "bash"}); err == nil {
		t.Fatal("expected error for non-whitelisted command")
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"command": "echo", "args": []any{"ok", 1}}); err == nil {
		t.Fatal("expected error for invalid args type")
	}
}

func TestAskUserToolExecute(t *testing.T) {
	t.Parallel()

	tool := AskUserTool{}
	out, err := tool.Execute(context.Background(), map[string]any{"prompt": "What now?"})
	if err != nil {
		t.Fatalf("execute ask tool: %v", err)
	}

	value, ok := out.(string)
	if !ok {
		t.Fatalf("output type = %T, want string", out)
	}
	if value != "user_input_placeholder" {
		t.Fatalf("output = %q, want %q", value, "user_input_placeholder")
	}
}

func TestAskUserToolExecuteErrors(t *testing.T) {
	t.Parallel()

	tool := AskUserTool{}
	if _, err := tool.Execute(context.Background(), map[string]any{"prompt": ""}); err == nil {
		t.Fatal("expected error for empty prompt")
	}
}
