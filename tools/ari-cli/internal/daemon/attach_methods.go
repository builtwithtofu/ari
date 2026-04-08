package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type AgentAttachRequest struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	InitialCols uint16 `json:"initial_cols"`
	InitialRows uint16 `json:"initial_rows"`
}

type AgentAttachResponse struct {
	Token  string `json:"token"`
	Status string `json:"status"`
}

type AgentDetachRequest struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
}

type AgentDetachResponse struct {
	Status string `json:"status"`
}

type attachSession struct {
	Token       string
	SessionID   string
	AgentID     string
	InitialCols uint16
	InitialRows uint16
	CreatedAt   time.Time
	Connected   bool
}

const (
	attachPendingSessionTTL    = 30 * time.Second
	attachTokenCleanupInterval = 10 * time.Second
)

var resizeAgentProcess = func(proc *process.Process, rows, cols uint16) error {
	return proc.Resize(rows, cols)
}

func (d *Daemon) registerAttachMethods(registry *rpc.MethodRegistry, store *globaldb.Store) error {
	if registry == nil {
		return fmt.Errorf("method registry is required")
	}
	if store == nil {
		return fmt.Errorf("globaldb store is required")
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentAttachRequest, AgentAttachResponse]{
		Name:        "agent.attach",
		Description: "Create an attach session for a running agent",
		Handler: func(ctx context.Context, req AgentAttachRequest) (AgentAttachResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentAttachResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			identifier := strings.TrimSpace(req.AgentID)
			if identifier == "" {
				return AgentAttachResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "agent_id is required", sessionID)
			}
			if req.InitialCols == 0 || req.InitialRows == 0 {
				return AgentAttachResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "initial_rows and initial_cols must be greater than zero", sessionID)
			}

			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return AgentAttachResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			agent, err := lookupAgentByIdentifier(ctx, store, sessionID, identifier)
			if err != nil {
				return AgentAttachResponse{}, mapAgentStoreError(err, sessionID)
			}
			if agent.Status != "running" {
				return AgentAttachResponse{}, rpc.NewHandlerError(rpc.AgentNotRunning, "agent is not running", sessionID)
			}

			_, ok := d.getAgentProcess(agent.AgentID)
			if !ok {
				return AgentAttachResponse{}, rpc.NewHandlerError(rpc.AgentNotRunning, "agent is not running", sessionID)
			}

			token, err := newAttachToken()
			if err != nil {
				return AgentAttachResponse{}, fmt.Errorf("generate attach token: %w", err)
			}

			session := attachSession{
				Token:       token,
				SessionID:   sessionID,
				AgentID:     agent.AgentID,
				InitialCols: req.InitialCols,
				InitialRows: req.InitialRows,
				CreatedAt:   time.Now().UTC(),
			}

			if !d.reserveAttachSession(session, time.Now().UTC()) {
				return AgentAttachResponse{}, rpc.NewHandlerError(rpc.AgentAlreadyAttached, "agent already has an active attach session", sessionID)
			}

			return AgentAttachResponse{Token: token, Status: "pending"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.attach: %w", err)
	}

	if err := rpc.RegisterMethod(registry, rpc.Method[AgentDetachRequest, AgentDetachResponse]{
		Name:        "agent.detach",
		Description: "Remove an active attach session for an agent",
		Handler: func(ctx context.Context, req AgentDetachRequest) (AgentDetachResponse, error) {
			sessionID := strings.TrimSpace(req.WorkspaceID)
			if sessionID == "" {
				return AgentDetachResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "workspace_id is required", nil)
			}
			identifier := strings.TrimSpace(req.AgentID)
			if identifier == "" {
				return AgentDetachResponse{}, rpc.NewHandlerError(rpc.InvalidParams, "agent_id is required", sessionID)
			}

			if _, err := store.GetSession(ctx, sessionID); err != nil {
				return AgentDetachResponse{}, mapWorkspaceStoreError(err, sessionID)
			}

			agent, err := lookupAgentByIdentifier(ctx, store, sessionID, identifier)
			if err != nil {
				return AgentDetachResponse{}, mapAgentStoreError(err, sessionID)
			}

			d.closeAttachConnection(agent.AgentID)
			d.clearAttachForAgent(agent.AgentID)
			return AgentDetachResponse{Status: "detached"}, nil
		},
	}); err != nil {
		return fmt.Errorf("register agent.detach: %w", err)
	}

	return nil
}

func newAttachToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (d *Daemon) reserveAttachSession(session attachSession, now time.Time) bool {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()

	if token, exists := d.attachByAgent[session.AgentID]; exists {
		existing, ok := d.attachByToken[token]
		if !ok {
			delete(d.attachByAgent, session.AgentID)
		} else {
			if !d.expirePendingAttachLocked(token, existing, now) {
				if existing.Connected {
					return false
				}
				if existing.InitialRows == session.InitialRows && existing.InitialCols == session.InitialCols {
					return false
				}
				delete(d.attachByToken, token)
				delete(d.attachByAgent, session.AgentID)
			}
		}
	}

	d.attachByToken[session.Token] = session
	d.attachByAgent[session.AgentID] = session.Token
	return true
}

func (d *Daemon) hasActiveAttachForAgent(agentID string) bool {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()

	token, ok := d.attachByAgent[agentID]
	if !ok {
		return false
	}

	session, exists := d.attachByToken[token]
	if !exists {
		delete(d.attachByAgent, agentID)
		return false
	}

	if d.expirePendingAttachLocked(token, session, time.Now().UTC()) {
		return false
	}

	return true
}

func (d *Daemon) clearAttachForAgent(agentID string) {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()

	token, ok := d.attachByAgent[agentID]
	if !ok {
		return
	}

	delete(d.attachByAgent, agentID)
	delete(d.attachByToken, token)
}

func (d *Daemon) clearAttachForToken(agentID, token string) {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()

	if activeToken, exists := d.attachByAgent[agentID]; exists && activeToken == token {
		delete(d.attachByAgent, agentID)
	}
	delete(d.attachByToken, token)
}

func (d *Daemon) markAttachSessionConnected(token string) (attachSession, bool) {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()

	session, ok := d.attachByToken[token]
	if !ok {
		return attachSession{}, false
	}
	if session.Connected {
		return attachSession{}, false
	}

	if d.expirePendingAttachLocked(token, session, time.Now().UTC()) {
		return attachSession{}, false
	}

	if activeToken, exists := d.attachByAgent[session.AgentID]; exists && activeToken != token {
		activeSession, activeExists := d.attachByToken[activeToken]
		if activeExists && activeSession.Connected {
			return attachSession{}, false
		}
	}

	session.Connected = true
	d.attachByToken[token] = session
	d.attachByAgent[session.AgentID] = token
	return session, true
}

func (d *Daemon) setAttachConnection(agentID string, conn net.Conn) {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()
	d.attachConnByAgent[agentID] = conn
}

func (d *Daemon) clearAttachConnectionIfCurrent(agentID string, conn net.Conn) {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()

	current, ok := d.attachConnByAgent[agentID]
	if ok && current == conn {
		delete(d.attachConnByAgent, agentID)
	}
}

func (d *Daemon) closeAttachConnection(agentID string) {
	d.attachMu.Lock()
	conn, ok := d.attachConnByAgent[agentID]
	if ok {
		delete(d.attachConnByAgent, agentID)
	}
	d.attachMu.Unlock()

	if ok {
		_ = conn.Close()
	}
}

func (d *Daemon) startAttachTokenCleanupLoop(ctx context.Context, interval time.Duration) {
	if ctx == nil {
		panic("attach cleanup context is required")
	}
	if interval <= 0 {
		panic("attach cleanup interval must be greater than zero")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			d.cleanupExpiredAttachSessions(now.UTC())
		}
	}
}

func (d *Daemon) cleanupExpiredAttachSessions(now time.Time) {
	d.attachMu.Lock()
	defer d.attachMu.Unlock()

	for token, session := range d.attachByToken {
		d.expirePendingAttachLocked(token, session, now)
	}
}

func (d *Daemon) expirePendingAttachLocked(token string, session attachSession, now time.Time) bool {
	if session.Connected {
		return false
	}
	if now.Sub(session.CreatedAt) <= attachPendingSessionTTL {
		return false
	}

	delete(d.attachByToken, token)
	if activeToken, exists := d.attachByAgent[session.AgentID]; exists && activeToken == token {
		delete(d.attachByAgent, session.AgentID)
	}

	return true
}
