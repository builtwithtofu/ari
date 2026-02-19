package status

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

type RuntimeSummary struct {
	SessionID          string   `json:"session_id"`
	CurrentStreamID    string   `json:"current_stream_id"`
	ActiveWorkUnits    []string `json:"active_work_units"`
	CompletedWorkUnits []string `json:"completed_work_units"`
	BlockedWorkUnits   []string `json:"blocked_work_units"`
	ActiveCount        int      `json:"active_count"`
	CompletedCount     int      `json:"completed_count"`
	BlockedCount       int      `json:"blocked_count"`
}

type runtimeStateFile struct {
	SessionID          string   `json:"session_id"`
	CurrentStreamID    string   `json:"current_stream_id"`
	ActiveWorkUnits    []string `json:"active_work_units"`
	CompletedWorkUnits []string `json:"completed_work_units"`
	BlockedWorkUnits   []string `json:"blocked_work_units"`
}

func LoadRuntimeSummary(repoRoot, sessionID string) (RuntimeSummary, error) {
	statePath, err := resolveStatePath(repoRoot, sessionID)
	if err != nil {
		return RuntimeSummary{}, err
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		return RuntimeSummary{}, err
	}

	var state runtimeStateFile
	if err := json.Unmarshal(raw, &state); err != nil {
		return RuntimeSummary{}, err
	}

	return RuntimeSummary{
		SessionID:          state.SessionID,
		CurrentStreamID:    state.CurrentStreamID,
		ActiveWorkUnits:    state.ActiveWorkUnits,
		CompletedWorkUnits: state.CompletedWorkUnits,
		BlockedWorkUnits:   state.BlockedWorkUnits,
		ActiveCount:        len(state.ActiveWorkUnits),
		CompletedCount:     len(state.CompletedWorkUnits),
		BlockedCount:       len(state.BlockedWorkUnits),
	}, nil
}

func resolveStatePath(repoRoot, sessionID string) (string, error) {
	runtimeRoot := filepath.Join(repoRoot, ".gaia", "runtime")
	if sessionID != "" {
		return filepath.Join(runtimeRoot, sessionID, "state.json"), nil
	}

	entries, err := os.ReadDir(runtimeRoot)
	if err != nil {
		return "", err
	}

	type candidate struct {
		sessionID string
		statePath string
		modTime   int64
	}

	candidates := make([]candidate, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		statePath := filepath.Join(runtimeRoot, entry.Name(), "state.json")
		info, err := os.Stat(statePath)
		if err != nil {
			continue
		}

		candidates = append(candidates, candidate{
			sessionID: entry.Name(),
			statePath: statePath,
			modTime:   info.ModTime().UnixNano(),
		})
	}

	if len(candidates) == 0 {
		return "", errors.New("no runtime state files found")
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].modTime == candidates[j].modTime {
			return candidates[i].sessionID > candidates[j].sessionID
		}

		return candidates[i].modTime > candidates[j].modTime
	})

	return candidates[0].statePath, nil
}
