package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/assert"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

type Daemon struct {
	mu         sync.RWMutex
	running    bool
	startedAt  time.Time
	socketPath string
	version    string
	pid        int
	cancel     context.CancelFunc
	stopCh     chan struct{}
	transport  *rpc.UnixSocketTransport
}

func NewDefault(version string) (*Daemon, error) {
	socketPath, err := DefaultSocketPath()
	if err != nil {
		return nil, err
	}

	return New(socketPath, version), nil
}

func New(socketPath, version string) *Daemon {
	assert.Invariant(strings.TrimSpace(socketPath) != "", "daemon socket path is required")

	if version == "" {
		version = "dev"
	}

	return &Daemon{
		socketPath: socketPath,
		version:    version,
		pid:        os.Getpid(),
	}
}

func (d *Daemon) Start(ctx context.Context) error {
	if d == nil {
		return fmt.Errorf("daemon is required")
	}

	if ctx == nil {
		return fmt.Errorf("daemon context is required")
	}

	registry := rpc.NewMethodRegistry()
	if err := d.registerMethods(registry); err != nil {
		return err
	}

	server := rpc.NewServer(registry)
	transport := rpc.NewUnixSocketTransport(d.socketPath, server)

	runCtx, cancel := context.WithCancel(ctx)
	stopCh := make(chan struct{}, 1)

	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		cancel()
		return fmt.Errorf("daemon is already running")
	}
	d.running = true
	d.startedAt = time.Now().UTC()
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
		}
	}()

	defer func() {
		cancel()
		d.mu.Lock()
		d.running = false
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
	version := d.version
	pid := d.pid
	d.mu.RUnlock()

	uptime := int64(0)
	if !startedAt.IsZero() {
		uptime = int64(time.Since(startedAt).Seconds())
		if uptime < 0 {
			uptime = 0
		}
	}

	return StatusResponse{
		Version:       version,
		PID:           pid,
		UptimeSeconds: uptime,
		SocketPath:    socketPath,
	}
}

func DefaultSocketPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".ari", "daemon.sock"), nil
}
