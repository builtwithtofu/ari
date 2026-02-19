package query

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrNoSessionState = errors.New("no runtime session state found")

type SessionSummary struct {
	SessionID      string    `json:"session_id"`
	CurrentStream  string    `json:"current_stream_id"`
	ActiveCount    int       `json:"active_count"`
	CompletedCount int       `json:"completed_count"`
	BlockedCount   int       `json:"blocked_count"`
	UpdatedAt      time.Time `json:"updated_at"`
	StatePath      string    `json:"state_path"`
}

type runtimeState struct {
	SessionID          string   `json:"session_id"`
	CurrentStreamID    string   `json:"current_stream_id"`
	ActiveWorkUnits    []string `json:"active_work_units"`
	CompletedWorkUnits []string `json:"completed_work_units"`
	BlockedWorkUnits   []string `json:"blocked_work_units"`
}

func ListSessions(repoRoot string) ([]SessionSummary, error) {
	runtimeRoot := filepath.Join(repoRoot, ".gaia", "runtime")
	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionSummary{}, nil
		}

		return nil, err
	}

	result := make([]SessionSummary, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		statePath := filepath.Join(runtimeRoot, entry.Name(), "state.json")
		info, err := os.Stat(statePath)
		if err != nil {
			continue
		}

		raw, err := os.ReadFile(statePath)
		if err != nil {
			continue
		}

		var state runtimeState
		if err := json.Unmarshal(raw, &state); err != nil {
			continue
		}

		sessionID := state.SessionID
		if sessionID == "" {
			sessionID = entry.Name()
		}

		result = append(result, SessionSummary{
			SessionID:      sessionID,
			CurrentStream:  state.CurrentStreamID,
			ActiveCount:    len(state.ActiveWorkUnits),
			CompletedCount: len(state.CompletedWorkUnits),
			BlockedCount:   len(state.BlockedWorkUnits),
			UpdatedAt:      info.ModTime().UTC(),
			StatePath:      statePath,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].SessionID > result[j].SessionID
		}

		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})

	return result, nil
}

func ResolveSessionID(repoRoot, sessionID string) (string, error) {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed != "" {
		return trimmed, nil
	}

	all, err := ListSessions(repoRoot)
	if err != nil {
		return "", err
	}

	if len(all) == 0 {
		return "", ErrNoSessionState
	}

	return all[0].SessionID, nil
}

func ReadSessionState(repoRoot, sessionID string) (map[string]any, string, error) {
	resolved, err := ResolveSessionID(repoRoot, sessionID)
	if err != nil {
		return nil, "", err
	}

	statePath := filepath.Join(repoRoot, ".gaia", "runtime", resolved, "state.json")
	return readJSON(statePath, resolved)
}

func ReadLifecycleState(repoRoot, sessionID string) (map[string]any, string, error) {
	resolved, err := ResolveSessionID(repoRoot, sessionID)
	if err != nil {
		return nil, "", err
	}

	path := filepath.Join(repoRoot, ".gaia", "lifecycle", resolved+".json")
	return readJSON(path, resolved)
}

func ReadSurfaceRegistry(repoRoot string) (map[string]any, error) {
	path := filepath.Join(repoRoot, ".gaia", "surfaces", "registry.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	return parsed, nil
}

func ReadFlowState(repoRoot, sessionID string) (map[string]any, string, error) {
	resolved, err := ResolveSessionID(repoRoot, sessionID)
	if err != nil {
		return nil, "", err
	}

	path := filepath.Join(repoRoot, ".gaia", "flows", resolved+".json")
	return readJSON(path, resolved)
}

func readJSON(path, resolvedSessionID string) (map[string]any, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, "", err
	}

	return parsed, resolvedSessionID, nil
}
