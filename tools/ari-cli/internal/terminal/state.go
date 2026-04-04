package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/term"
)

const (
	enterAltScreen = "\x1b[?1049h\x1b[?25l"
	exitAltScreen  = "\x1b[?25h\x1b[?1049l"
)

type stateHooks struct {
	makeRaw func(fd int) (*term.State, error)
	restore func(fd int, state *term.State) error
}

type StateManager struct {
	mu sync.Mutex

	fd  int
	out io.Writer

	hooks stateHooks

	rawEntered bool
	altEntered bool
	closed     bool
	savedState *term.State
}

func NewStateManager(fd int, out io.Writer) *StateManager {
	return newStateManager(fd, out, stateHooks{
		makeRaw: term.MakeRaw,
		restore: term.Restore,
	})
}

func newStateManager(fd int, out io.Writer, hooks stateHooks) *StateManager {
	if out == nil {
		out = io.Discard
	}
	if hooks.makeRaw == nil {
		hooks.makeRaw = term.MakeRaw
	}
	if hooks.restore == nil {
		hooks.restore = term.Restore
	}

	return &StateManager{fd: fd, out: out, hooks: hooks}
}

func (m *StateManager) EnterRaw() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("enter raw mode: terminal state manager is closed")
	}
	if m.rawEntered {
		return nil
	}

	saved, err := m.hooks.makeRaw(m.fd)
	if err != nil {
		return fmt.Errorf("enter raw mode: %w", err)
	}

	m.savedState = saved
	m.rawEntered = true

	return nil
}

func (m *StateManager) EnterAltScreen() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("enter alternate screen: terminal state manager is closed")
	}
	if m.altEntered {
		return nil
	}

	if _, err := io.WriteString(m.out, enterAltScreen); err != nil {
		return fmt.Errorf("enter alternate screen: %w", err)
	}

	m.altEntered = true

	return nil
}

func (m *StateManager) Restore() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	var restoreErr error

	if m.altEntered {
		if _, err := io.WriteString(m.out, exitAltScreen); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore alternate screen: %w", err))
		} else {
			m.altEntered = false
		}
	}

	if m.rawEntered {
		if err := m.hooks.restore(m.fd, m.savedState); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore terminal mode: %w", err))
		} else {
			m.rawEntered = false
			m.savedState = nil
		}
	}

	if restoreErr == nil {
		m.closed = true
	}

	return restoreErr
}

func (m *StateManager) Close() error {
	return m.Restore()
}

func (m *StateManager) RecoverPanic() {
	if recovered := recover(); recovered != nil {
		_ = m.Restore()
		panic(recovered)
	}
}

func (m *StateManager) StartSignalCleanup(ctx context.Context, signals <-chan os.Signal) func() {
	stopCh := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
			return
		case <-signals:
			_ = m.Restore()
		}
	}()

	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			close(stopCh)
		})
	}
}
