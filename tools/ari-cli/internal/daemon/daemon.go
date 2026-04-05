package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/assert"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/process"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
	_ "modernc.org/sqlite"
)

type Daemon struct {
	mu                sync.RWMutex
	running           bool
	startedAt         time.Time
	socketPath        string
	dbPath            string
	configPath        string
	configSource      string
	pidPath           string
	signalCh          <-chan os.Signal
	version           string
	pid               int
	store             *globaldb.Store
	db                *sql.DB
	cancel            context.CancelFunc
	stopCh            chan struct{}
	transport         *rpc.UnixSocketTransport
	commandMu         sync.RWMutex
	commands          map[string]*process.Process
	commandLogs       map[string]string
	commandLogOrder   []string
	commandWG         sync.WaitGroup
	agentMu           sync.RWMutex
	agents            map[string]*process.Process
	agentLogs         map[string]string
	agentLogOrder     []string
	agentStops        map[string]bool
	agentWG           sync.WaitGroup
	attachMu          sync.Mutex
	attachByToken     map[string]attachSession
	attachByAgent     map[string]string
	attachConnByAgent map[string]net.Conn
}

var bootstrapDatabase = globaldb.Bootstrap

func NewDefault(version string) (*Daemon, error) {
	socketPath, err := DefaultSocketPath()
	if err != nil {
		return nil, err
	}
	dbPath, err := DefaultDBPath()
	if err != nil {
		return nil, err
	}
	pidPath, err := DefaultPIDFilePath()
	if err != nil {
		return nil, err
	}

	return New(socketPath, dbPath, pidPath, "defaults", "defaults", version), nil
}

func New(socketPath, dbPath, pidPath, configPath, configSource, version string) *Daemon {
	return NewWithSignalChannel(socketPath, dbPath, pidPath, configPath, configSource, version, nil)
}

func NewWithSignalChannel(socketPath, dbPath, pidPath, configPath, configSource, version string, signalCh <-chan os.Signal) *Daemon {
	assert.Invariant(strings.TrimSpace(socketPath) != "", "daemon socket path is required")
	assert.Invariant(strings.TrimSpace(dbPath) != "", "daemon db path is required")
	assert.Invariant(strings.TrimSpace(pidPath) != "", "daemon pid path is required")
	assert.Invariant(strings.TrimSpace(configSource) != "", "daemon config source is required")

	if version == "" {
		version = "dev"
	}

	return &Daemon{
		socketPath:        socketPath,
		dbPath:            dbPath,
		pidPath:           pidPath,
		configPath:        configPath,
		configSource:      configSource,
		signalCh:          signalCh,
		version:           version,
		pid:               os.Getpid(),
		commands:          make(map[string]*process.Process),
		commandLogs:       make(map[string]string),
		commandLogOrder:   make([]string, 0),
		agents:            make(map[string]*process.Process),
		agentLogs:         make(map[string]string),
		agentLogOrder:     make([]string, 0),
		agentStops:        make(map[string]bool),
		attachByToken:     make(map[string]attachSession),
		attachByAgent:     make(map[string]string),
		attachConnByAgent: make(map[string]net.Conn),
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	if d == nil {
		return fmt.Errorf("daemon is required")
	}

	if ctx == nil {
		return fmt.Errorf("daemon context is required")
	}

	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("daemon is already running")
	}
	d.running = true
	d.mu.Unlock()

	startupSucceeded := false
	defer func() {
		if startupSucceeded {
			return
		}
		_ = RemovePIDFileIfOwned(d.pidPath, d.pid)
		d.mu.Lock()
		d.running = false
		d.startedAt = time.Time{}
		d.store = nil
		d.db = nil
		d.cancel = nil
		d.stopCh = nil
		d.transport = nil
		d.mu.Unlock()
	}()

	if err := bootstrapDatabase(ctx, d.dbPath); err != nil {
		return err
	}

	if err := WritePIDFile(d.pidPath, d.pid); err != nil {
		return fmt.Errorf("write daemon pid file: %w", err)
	}

	dbConn, err := sql.Open("sqlite", d.dbPath)
	if err != nil {
		return fmt.Errorf("open daemon database: %w", err)
	}

	store, err := globaldb.NewSQLStore(dbConn)
	if err != nil {
		_ = dbConn.Close()
		return fmt.Errorf("create daemon store: %w", err)
	}

	if err := store.SetMeta(ctx, "daemon.pid", strconv.Itoa(d.pid)); err != nil {
		_ = dbConn.Close()
		return fmt.Errorf("validate daemon database: %w", err)
	}
	if err := store.MarkRunningCommandsLost(ctx); err != nil {
		_ = dbConn.Close()
		return fmt.Errorf("reconcile running commands: %w", err)
	}
	if err := store.MarkRunningAgentsLost(ctx); err != nil {
		_ = dbConn.Close()
		return fmt.Errorf("reconcile running agents: %w", err)
	}

	registry := rpc.NewMethodRegistry()
	if err := d.registerMethods(registry, store); err != nil {
		_ = dbConn.Close()
		return err
	}

	server := rpc.NewServer(registry)
	transport := rpc.NewUnixSocketTransportWithFrameRouter(d.socketPath, server, d.routeFrameConnection)

	runCtx, cancel := context.WithCancel(ctx)
	stopCh := make(chan struct{}, 1)

	d.mu.Lock()
	d.startedAt = time.Now().UTC()
	d.store = store
	d.db = dbConn
	d.cancel = cancel
	d.stopCh = stopCh
	d.transport = transport
	d.mu.Unlock()

	go func() {
		select {
		case <-runCtx.Done():
			return
		case <-stopCh:
			cancel()
		case <-d.signalCh:
			cancel()
		}
	}()

	go d.startAttachTokenCleanupLoop(runCtx, attachTokenCleanupInterval)

	defer func() {
		startupSucceeded = true
		cancel()
		d.stopAllCommands()
		d.stopAllAgents()
		d.commandWG.Wait()
		d.agentWG.Wait()
		_ = RemovePIDFileIfOwned(d.pidPath, d.pid)
		if dbConn != nil {
			_ = dbConn.Close()
		}
		d.mu.Lock()
		d.running = false
		d.startedAt = time.Time{}
		d.store = nil
		d.db = nil
		d.cancel = nil
		d.stopCh = nil
		d.transport = nil
		d.mu.Unlock()
	}()

	return transport.Run(runCtx)
}

func (d *Daemon) Stop() {
	d.mu.RLock()
	stopCh := d.stopCh
	d.mu.RUnlock()

	if stopCh != nil {
		select {
		case stopCh <- struct{}{}:
		default:
		}
	}
}

func (d *Daemon) status() StatusResponse {
	d.mu.RLock()
	startedAt := d.startedAt
	socketPath := d.socketPath
	dbPath := d.dbPath
	configPath := d.configPath
	configSource := d.configSource
	version := d.version
	pid := d.pid
	store := d.store
	d.mu.RUnlock()

	uptime := int64(0)
	if !startedAt.IsZero() {
		uptime = int64(time.Since(startedAt).Seconds())
		if uptime < 0 {
			uptime = 0
		}
	}

	databaseState := "unhealthy"
	if store != nil {
		if _, err := store.GetMeta(context.Background(), "daemon.pid"); err == nil {
			databaseState = "healthy"
		}
	}

	return StatusResponse{
		Version:       version,
		PID:           pid,
		UptimeSeconds: uptime,
		SocketPath:    socketPath,
		DatabasePath:  dbPath,
		DatabaseState: databaseState,
		ConfigPath:    configPath,
		ConfigSource:  configSource,
	}
}

func DefaultSocketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ari", "daemon.sock"), nil
}

func DefaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ari", "ari.db"), nil
}
