package tool

import (
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
)

func TestToolFromCommandRecordBuildsCommandSubtype(t *testing.T) {
	record := globaldb.Command{
		CommandID:   "cmd-1",
		WorkspaceID: "ws-1",
		Command:     "go",
		Args:        `["test","./..."]`,
		Status:      "running",
		StartedAt:   "2026-04-15T00:00:00Z",
	}

	tool, err := FromCommandRecord(record)
	if err != nil {
		t.Fatalf("FromCommandRecord returned error: %v", err)
	}
	if tool.ToolID != "cmd-1" {
		t.Fatalf("tool id = %q, want %q", tool.ToolID, "cmd-1")
	}
	if tool.Type != TypeCommand {
		t.Fatalf("tool type = %q, want %q", tool.Type, TypeCommand)
	}
	if tool.Command == nil {
		t.Fatal("tool command payload = nil, want non-nil")
	}
	if tool.Command.Command != "go" {
		t.Fatalf("tool command = %q, want %q", tool.Command.Command, "go")
	}
	if len(tool.Command.Args) != 2 || tool.Command.Args[0] != "test" || tool.Command.Args[1] != "./..." {
		t.Fatalf("tool args = %#v, want [test ./...]", tool.Command.Args)
	}
}

func TestToolFromCommandRecordRejectsInvalidArgsJSON(t *testing.T) {
	record := globaldb.Command{
		CommandID:   "cmd-1",
		WorkspaceID: "ws-1",
		Command:     "go",
		Args:        `{"bad":true}`,
		Status:      "running",
		StartedAt:   "2026-04-15T00:00:00Z",
	}

	_, err := FromCommandRecord(record)
	if err == nil {
		t.Fatal("FromCommandRecord returned nil error")
	}
	if err.Error() != "decode command args: json: cannot unmarshal object into Go value of type []string" {
		t.Fatalf("FromCommandRecord error = %q, want exact decode error", err.Error())
	}
}
