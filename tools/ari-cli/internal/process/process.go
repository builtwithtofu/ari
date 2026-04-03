package process

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateExited   State = "exited"
)

type Options struct {
	Dir         string
	StopTimeout time.Duration
}

type Result struct {
	ExitCode int
	Signaled bool
}

type Process struct {
	mu      sync.RWMutex
	command string
	args    []string
	opts    Options

	cmd     *exec.Cmd
	ptyFile interface {
		Read(p []byte) (n int, err error)
		Write(p []byte) (n int, err error)
		Close() error
		Fd() uintptr
	}

	outputBuffer *RingBuffer
	outputSubs   map[chan []byte]struct{}
	outputDone   chan struct{}

	state   State
	exitSet bool
	exit    Result

	done      chan struct{}
	closeOnce sync.Once

	waitCh   chan struct{}
	waitOnce sync.Once
	waitErr  error
	waitCmd  func() error
}

const defaultStopTimeout = 10 * time.Second

var processGroupKill = syscall.Kill

func New(command string, args []string, opts Options) (*Process, error) {
	if command == "" {
		return nil, errors.New("process command is required")
	}
	if opts.Dir == "" {
		return nil, errors.New("process dir is required")
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = defaultStopTimeout
	}

	return &Process{
		command:      command,
		args:         append([]string(nil), args...),
		opts:         opts,
		state:        StateStarting,
		done:         make(chan struct{}),
		outputDone:   make(chan struct{}),
		waitCh:       make(chan struct{}),
		outputBuffer: NewRingBuffer(1 << 20),
		outputSubs:   make(map[chan []byte]struct{}),
	}, nil
}

func (p *Process) Start() error {
	p.mu.Lock()

	if p.cmd != nil {
		p.mu.Unlock()
		return errors.New("process already started")
	}

	cmd := exec.Command(p.command, p.args...)
	cmd.Dir = p.opts.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	ptyFile, err := pty.Start(cmd)
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("start pty process: %w", err)
	}

	if err := unix.SetNonblock(int(ptyFile.Fd()), true); err != nil {
		_ = ptyFile.Close()
		p.mu.Unlock()
		return fmt.Errorf("set pty nonblocking: %w", err)
	}

	p.cmd = cmd
	p.ptyFile = ptyFile
	p.waitCmd = cmd.Wait
	p.state = StateRunning
	p.mu.Unlock()

	go p.pumpOutput()
	p.startWaiter()

	return nil
}

func (p *Process) Stop() error {
	p.mu.RLock()
	cmd := p.cmd
	p.mu.RUnlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	select {
	case <-p.waitCh:
		return nil
	default:
	}

	pid := cmd.Process.Pid
	if pid > 0 {
		if err := processGroupKill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("send sigterm to process group %d: %w", pid, err)
		}
	}

	select {
	case <-p.waitCh:
		return nil
	case <-time.After(p.opts.StopTimeout):
		if err := processGroupKill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("send sigkill to process group %d: %w", pid, err)
		}

		select {
		case <-p.waitCh:
			return nil
		case <-time.After(p.opts.StopTimeout):
			return fmt.Errorf("process %d did not exit after stop timeout", pid)
		}
	}
}

func (p *Process) Wait() (Result, error) {
	p.mu.RLock()
	started := p.cmd != nil || p.waitCmd != nil
	p.mu.RUnlock()
	if !started {
		return Result{}, errors.New("wait before start")
	}

	p.startWaiter()

	<-p.waitCh

	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exit, p.waitErr
}

func (p *Process) State() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

func (p *Process) ExitCode() (int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.exit.ExitCode, p.exitSet
}

func (p *Process) PID() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *Process) Write(input []byte) (int, error) {
	p.mu.RLock()
	ptyFile := p.ptyFile
	state := p.state
	p.mu.RUnlock()

	if state != StateRunning || ptyFile == nil {
		return 0, errors.New("process is not running")
	}

	n, err := ptyFile.Write(input)
	if err != nil {
		return n, fmt.Errorf("write process input: %w", err)
	}

	return n, nil
}

func (p *Process) OutputSnapshot() []byte {
	return p.outputBuffer.Snapshot()
}

func (p *Process) SubscribeOutput() (<-chan []byte, func()) {
	updates := make(chan []byte, 32)

	p.mu.Lock()
	p.outputSubs[updates] = struct{}{}
	p.mu.Unlock()

	unsubscribe := func() {
		p.mu.Lock()
		if _, ok := p.outputSubs[updates]; ok {
			delete(p.outputSubs, updates)
			close(updates)
		}
		p.mu.Unlock()
	}

	return updates, unsubscribe
}

func (p *Process) Resize(rows, cols uint16) error {
	if rows == 0 {
		return errors.New("resize rows must be greater than zero")
	}
	if cols == 0 {
		return errors.New("resize cols must be greater than zero")
	}

	p.mu.RLock()
	ptyFile := p.ptyFile
	state := p.state
	p.mu.RUnlock()

	if state != StateRunning || ptyFile == nil {
		return errors.New("process is not running")
	}

	if err := unix.IoctlSetWinsize(int(ptyFile.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols}); err != nil {
		return fmt.Errorf("set pty size rows=%d cols=%d: %w", rows, cols, err)
	}

	return nil
}

func (p *Process) waitInternal() error {
	p.mu.RLock()
	waitCmd := p.waitCmd
	outputDone := p.outputDone
	ptyFile := p.ptyFile
	p.mu.RUnlock()
	if waitCmd == nil {
		return errors.New("wait before start")
	}

	err := waitCmd()
	result := Result{}

	if err == nil {
		result.ExitCode = 0
		result.Signaled = false
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return fmt.Errorf("wait for process: %w", err)
		}

		waitStatus, ok := exitErr.Sys().(syscall.WaitStatus)
		if !ok {
			return fmt.Errorf("unexpected wait status type %T", exitErr.Sys())
		}

		if waitStatus.Signaled() {
			result.Signaled = true
			result.ExitCode = 128 + int(waitStatus.Signal())
		} else {
			result.Signaled = false
			result.ExitCode = waitStatus.ExitStatus()
		}
	}

	p.closeOnce.Do(func() {
		close(p.done)
	})

	if ptyFile != nil {
		_ = ptyFile.Close()
	}
	if outputDone != nil {
		<-outputDone
	}

	p.mu.Lock()
	p.exit = result
	p.exitSet = true
	p.state = StateExited
	p.mu.Unlock()

	return nil
}

func (p *Process) startWaiter() {
	p.waitOnce.Do(func() {
		go func() {
			p.waitErr = p.waitInternal()
			close(p.waitCh)
		}()
	})
}

func (p *Process) pumpOutput() {
	defer close(p.outputDone)

	buf := make([]byte, 4096)
	for {
		n, err := p.ptyFile.Read(buf)
		if err == nil {
			chunk := append([]byte(nil), buf[:n]...)
			_, _ = p.outputBuffer.Write(chunk)
			p.broadcastOutput(chunk)
			continue
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		return
	}
}

func (p *Process) broadcastOutput(chunk []byte) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for ch := range p.outputSubs {
		select {
		case ch <- chunk:
		default:
		}
	}
}
