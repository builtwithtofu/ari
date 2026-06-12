package fakeharness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sessionStore persists fake session turns as JSONL files under
// `<root>/<harness>/sessions/<id>.jsonl`. A zero root disables state: every
// invocation then behaves as a fresh stateless run, matching the historical
// fake harness behavior.
type sessionStore struct {
	root string
}

type sessionTurn struct {
	Turn  int    `json:"turn"`
	Stdin string `json:"stdin,omitempty"`
}

func newSessionStore(root string) sessionStore {
	return sessionStore{root: strings.TrimSpace(root)}
}

func (s sessionStore) enabled() bool {
	return s.root != ""
}

func (s sessionStore) sessionPath(harness, sessionID string) string {
	return filepath.Join(s.root, harness, "sessions", sanitizeSessionID(sessionID)+".jsonl")
}

func sanitizeSessionID(sessionID string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(sessionID) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			out.WriteRune(r)
		default:
			out.WriteByte('-')
		}
	}
	return strings.Trim(out.String(), "-.")
}

// turnCount returns the number of turns recorded for a session, 0 when state
// is disabled or the session does not exist yet.
func (s sessionStore) turnCount(harness, sessionID string) int {
	if !s.enabled() {
		return 0
	}
	f, err := os.Open(s.sessionPath(harness, sessionID))
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			count++
		}
	}
	return count
}

// appendTurn records one turn for a session and returns its 1-based number.
// With state disabled it returns 1 without touching the filesystem.
func (s sessionStore) appendTurn(harness, sessionID, stdinHash string) int {
	if !s.enabled() {
		return 1
	}
	turn := s.turnCount(harness, sessionID) + 1
	path := s.sessionPath(harness, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return turn
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return turn
	}
	defer func() { _ = f.Close() }()
	_ = json.NewEncoder(f).Encode(sessionTurn{Turn: turn, Stdin: stdinHash})
	return turn
}

// latestSession returns the most recently modified session id for a harness,
// or "" when none exist. Used by continue-most-recent flags (-c).
func (s sessionStore) latestSession(harness string) string {
	if !s.enabled() {
		return ""
	}
	dir := filepath.Join(s.root, harness, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	type candidate struct {
		name string
		mod  int64
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{name: strings.TrimSuffix(entry.Name(), ".jsonl"), mod: info.ModTime().UnixNano()})
	}
	if len(candidates) == 0 {
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].mod == candidates[j].mod {
			return candidates[i].name > candidates[j].name
		}
		return candidates[i].mod > candidates[j].mod
	})
	return candidates[0].name
}
