package tools

import (
	"context"
	"errors"
	"testing"
)

type stubTool struct {
	name        string
	description string
	schema      map[string]any
}

func (s stubTool) Name() string {
	return s.name
}

func (s stubTool) Description() string {
	return s.description
}

func (s stubTool) Execute(context.Context, map[string]any) (any, error) {
	return nil, nil
}

func (s stubTool) InputSchema() map[string]any {
	return s.schema
}

func TestToolRegistryRegisterAndGetSuccess(t *testing.T) {
	registry := NewToolRegistry()
	tool := stubTool{name: "read_file", description: "Read a file"}

	if err := registry.Register("read_file", tool); err != nil {
		t.Fatalf("register returned error: %v", err)
	}

	resolved, err := registry.Get(" READ_FILE ")
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}

	if resolved.Name() != tool.Name() {
		t.Fatalf("resolved tool name = %q, want %q", resolved.Name(), tool.Name())
	}
}

func TestToolRegistryUnknownToolError(t *testing.T) {
	registry := NewToolRegistry()

	if err := registry.Register("read_file", stubTool{name: "read_file"}); err != nil {
		t.Fatalf("register read_file returned error: %v", err)
	}
	if err := registry.Register("write_file", stubTool{name: "write_file"}); err != nil {
		t.Fatalf("register write_file returned error: %v", err)
	}

	_, err := registry.Get("run_shell")
	if err == nil {
		t.Fatal("get returned nil error for unknown tool")
	}
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("get error = %v, want unknown tool error", err)
	}

	want := "unknown tool: \"run_shell\" (available: read_file, write_file)"
	if err.Error() != want {
		t.Fatalf("error message = %q, want %q", err.Error(), want)
	}
}

func TestToolRegistryListDeterministicOrder(t *testing.T) {
	registry := NewToolRegistry()

	if err := registry.Register("write_file", stubTool{name: "write_file"}); err != nil {
		t.Fatalf("register write_file returned error: %v", err)
	}
	if err := registry.Register("read_file", stubTool{name: "read_file"}); err != nil {
		t.Fatalf("register read_file returned error: %v", err)
	}

	list := registry.List()
	if len(list) != 2 {
		t.Fatalf("list length = %d, want 2", len(list))
	}
	if list[0].Name() != "read_file" {
		t.Fatalf("list[0] = %q, want %q", list[0].Name(), "read_file")
	}
	if list[1].Name() != "write_file" {
		t.Fatalf("list[1] = %q, want %q", list[1].Name(), "write_file")
	}
}

func TestToolRegistryToToolDefinitions(t *testing.T) {
	registry := NewToolRegistry()

	customSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}

	if err := registry.Register("write_file", stubTool{name: "write_file", description: "Write file"}); err != nil {
		t.Fatalf("register write_file returned error: %v", err)
	}
	if err := registry.Register("read_file", stubTool{name: "read_file", description: "Read file", schema: customSchema}); err != nil {
		t.Fatalf("register read_file returned error: %v", err)
	}

	definitions := registry.ToToolDefinitions()
	if len(definitions) != 2 {
		t.Fatalf("definitions length = %d, want 2", len(definitions))
	}

	if definitions[0].Name != "read_file" {
		t.Fatalf("definitions[0].Name = %q, want %q", definitions[0].Name, "read_file")
	}
	if definitions[0].Description != "Read file" {
		t.Fatalf("definitions[0].Description = %q, want %q", definitions[0].Description, "Read file")
	}
	if definitions[0].InputSchema["type"] != "object" {
		t.Fatalf("definitions[0].InputSchema[type] = %v, want object", definitions[0].InputSchema["type"])
	}

	if definitions[1].Name != "write_file" {
		t.Fatalf("definitions[1].Name = %q, want %q", definitions[1].Name, "write_file")
	}
	if definitions[1].Description != "Write file" {
		t.Fatalf("definitions[1].Description = %q, want %q", definitions[1].Description, "Write file")
	}
	if definitions[1].InputSchema["type"] != "object" {
		t.Fatalf("definitions[1].InputSchema[type] = %v, want object", definitions[1].InputSchema["type"])
	}
}
