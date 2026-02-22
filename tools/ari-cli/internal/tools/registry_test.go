package tools

import (
	"errors"
	"testing"
)

func TestRegistryRegisterGetAndList(t *testing.T) {
	registry := NewRegistry()
	readTool := stubTool{name: "read_file", description: "Read file"}
	writeTool := stubTool{name: "write_file", description: "Write file"}

	if err := registry.Register(writeTool); err != nil {
		t.Fatalf("register write_file returned error: %v", err)
	}
	if err := registry.Register(readTool); err != nil {
		t.Fatalf("register read_file returned error: %v", err)
	}

	if err := registry.Register(readTool); err == nil {
		t.Fatal("register duplicate returned nil error")
	}

	resolved, err := registry.Get(" READ_FILE ")
	if err != nil {
		t.Fatalf("get returned error: %v", err)
	}
	if resolved.Name() != "read_file" {
		t.Fatalf("resolved.Name = %q, want %q", resolved.Name(), "read_file")
	}

	list := registry.List()
	if len(list) != 2 {
		t.Fatalf("list length = %d, want 2", len(list))
	}
	if list[0].Name() != "read_file" {
		t.Fatalf("list[0].Name = %q, want %q", list[0].Name(), "read_file")
	}
	if list[1].Name() != "write_file" {
		t.Fatalf("list[1].Name = %q, want %q", list[1].Name(), "write_file")
	}
}

func TestRegistryGetUnknownToolError(t *testing.T) {
	registry := NewRegistry()

	if _, err := registry.Get("missing"); err == nil {
		t.Fatal("get returned nil error for missing tool")
	}

	if err := registry.Register(stubTool{name: "read_file"}); err != nil {
		t.Fatalf("register read_file returned error: %v", err)
	}

	_, err := registry.Get("run_shell")
	if err == nil {
		t.Fatal("get returned nil error for unknown tool")
	}
	if !errors.Is(err, ErrUnknownTool) {
		t.Fatalf("get error = %v, want unknown tool", err)
	}

	want := "unknown tool: \"run_shell\" (available: read_file)"
	if err.Error() != want {
		t.Fatalf("error message = %q, want %q", err.Error(), want)
	}
}

func TestRegistryToToolDefinitions(t *testing.T) {
	registry := NewRegistry()
	customSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}

	if err := registry.Register(stubTool{name: "write_file", description: "Write file"}); err != nil {
		t.Fatalf("register write_file returned error: %v", err)
	}
	if err := registry.Register(stubTool{name: "read_file", description: "Read file", schema: customSchema}); err != nil {
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

func TestDefaultRegistryIncludesStandardTools(t *testing.T) {
	registry := DefaultRegistry()
	wantNames := []string{"ask_user", "read_file", "run_command", "write_file"}

	list := registry.List()
	if len(list) != len(wantNames) {
		t.Fatalf("list length = %d, want %d", len(list), len(wantNames))
	}

	for i, want := range wantNames {
		if list[i].Name() != want {
			t.Fatalf("list[%d].Name = %q, want %q", i, list[i].Name(), want)
		}
		if _, err := registry.Get(want); err != nil {
			t.Fatalf("get %q returned error: %v", want, err)
		}
	}
}
