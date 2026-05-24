package globaldb

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLegacyAgentsRuntimePersistencePathIsRemoved(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	globaldbDir := filepath.Dir(currentFile)

	for _, rel := range []string{"queries", "dbsqlc"} {
		dir := filepath.Join(globaldbDir, rel)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s) returned error: %v", rel, err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "agents.") {
				t.Fatalf("legacy agents persistence file %s exists; harness_sessions is the runtime session authority", filepath.Join(rel, entry.Name()))
			}
		}
	}

	for _, rel := range []string{"store.go", filepath.Join("queries", "agent_runtime.sql")} {
		path := filepath.Join(globaldbDir, rel)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s) returned error: %v", rel, err)
		}
		for _, forbidden := range []string{"CreateAgent(", "GetAgentByID", "ListAgentsByWorkspace", "UpdateAgentStatus", "MarkRunningAgentsLost", "FROM agents", "UPDATE agents", "INSERT INTO agents"} {
			if strings.Contains(string(contents), forbidden) {
				t.Fatalf("%s contains legacy agents runtime persistence token %q", rel, forbidden)
			}
		}
	}
}
