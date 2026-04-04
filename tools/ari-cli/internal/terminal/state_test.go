package terminal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"golang.org/x/term"
)

func TestStateManagerEnterRawAndRestore(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	makeRawCalled := false
	restoreCalled := false
	saved := &term.State{}

	m := newStateManager(9, &out, stateHooks{
		makeRaw: func(fd int) (*term.State, error) {
			makeRawCalled = true
			if fd != 9 {
				t.Fatalf("makeRaw fd = %d, want 9", fd)
			}
			return saved, nil
		},
		restore: func(fd int, state *term.State) error {
			restoreCalled = true
			if fd != 9 {
				t.Fatalf("restore fd = %d, want 9", fd)
			}
			if state != saved {
				t.Fatalf("restore state = %p, want %p", state, saved)
			}
			return nil
		},
	})

	if err := m.EnterRaw(); err != nil {
		t.Fatalf("EnterRaw returned error: %v", err)
	}
	if !makeRawCalled {
		t.Fatal("makeRaw called = false, want true")
	}

	if err := m.Restore(); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	if !restoreCalled {
		t.Fatal("restore called = false, want true")
	}
}

func TestStateManagerAltScreenEnterAndRestore(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	m := newStateManager(1, &out, stateHooks{
		makeRaw: func(fd int) (*term.State, error) { return &term.State{}, nil },
		restore: func(fd int, state *term.State) error { return nil },
	})

	if err := m.EnterAltScreen(); err != nil {
		t.Fatalf("EnterAltScreen returned error: %v", err)
	}
	if got, want := out.String(), "\x1b[?1049h\x1b[?25l"; got != want {
		t.Fatalf("alt-screen enter output = %q, want %q", got, want)
	}

	out.Reset()
	if err := m.Restore(); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	if got, want := out.String(), "\x1b[?25h\x1b[?1049l"; got != want {
		t.Fatalf("restore output = %q, want %q", got, want)
	}
}

func TestStateManagerCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	restoreCalls := 0
	m := newStateManager(7, &out, stateHooks{
		makeRaw: func(fd int) (*term.State, error) { return &term.State{}, nil },
		restore: func(fd int, state *term.State) error {
			restoreCalls++
			return nil
		},
	})

	if err := m.EnterRaw(); err != nil {
		t.Fatalf("EnterRaw returned error: %v", err)
	}
	if err := m.EnterAltScreen(); err != nil {
		t.Fatalf("EnterAltScreen returned error: %v", err)
	}

	if err := m.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("second Close returned error: %v", err)
	}

	if restoreCalls != 1 {
		t.Fatalf("restore call count = %d, want 1", restoreCalls)
	}
}

func TestStateManagerRecoverPanicRestoresAndRepanics(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	restoreCalls := 0
	m := newStateManager(4, &out, stateHooks{
		makeRaw: func(fd int) (*term.State, error) { return &term.State{}, nil },
		restore: func(fd int, state *term.State) error {
			restoreCalls++
			return nil
		},
	})

	if err := m.EnterRaw(); err != nil {
		t.Fatalf("EnterRaw returned error: %v", err)
	}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		defer m.RecoverPanic()
		panic("boom")
	}()

	if recovered != "boom" {
		t.Fatalf("recovered value = %v, want boom", recovered)
	}
	if restoreCalls != 1 {
		t.Fatalf("restore call count = %d, want 1", restoreCalls)
	}
}

func TestStateManagerSignalCleanupRestores(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	restoreCalls := 0
	m := newStateManager(5, &out, stateHooks{
		makeRaw: func(fd int) (*term.State, error) { return &term.State{}, nil },
		restore: func(fd int, state *term.State) error {
			restoreCalls++
			return nil
		},
	})

	if err := m.EnterRaw(); err != nil {
		t.Fatalf("EnterRaw returned error: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := m.StartSignalCleanup(ctx, sigCh)
	defer stop()

	sigCh <- syscall.SIGINT

	deadline := time.Now().Add(500 * time.Millisecond)
	for restoreCalls == 0 {
		if time.Now().After(deadline) {
			t.Fatal("signal cleanup did not restore terminal state")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStateManagerEnterRawPropagatesMakeRawError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("make raw failed")
	m := newStateManager(3, &bytes.Buffer{}, stateHooks{
		makeRaw: func(fd int) (*term.State, error) { return nil, wantErr },
		restore: func(fd int, state *term.State) error { return nil },
	})

	err := m.EnterRaw()
	if !errors.Is(err, wantErr) {
		t.Fatalf("EnterRaw error = %v, want wrapped %v", err, wantErr)
	}
}

func TestStateManagerRestoreCallsTerminalRestoreWhenAltScreenWriteFails(t *testing.T) {
	t.Parallel()

	restoreCalls := 0
	m := newStateManager(6, &failingWriter{}, stateHooks{
		makeRaw: func(fd int) (*term.State, error) { return &term.State{}, nil },
		restore: func(fd int, state *term.State) error {
			restoreCalls++
			return nil
		},
	})

	if err := m.EnterRaw(); err != nil {
		t.Fatalf("EnterRaw returned error: %v", err)
	}
	m.altEntered = true

	err := m.Restore()
	if err == nil {
		t.Fatal("Restore returned nil error, want non-nil")
	}
	if restoreCalls != 1 {
		t.Fatalf("restore call count = %d, want 1", restoreCalls)
	}
}

func TestStateManagerRestoreRetriesAfterRestoreHookFailure(t *testing.T) {
	t.Parallel()

	restoreCalls := 0
	m := newStateManager(8, &bytes.Buffer{}, stateHooks{
		makeRaw: func(fd int) (*term.State, error) { return &term.State{}, nil },
		restore: func(fd int, state *term.State) error {
			restoreCalls++
			if restoreCalls == 1 {
				return errors.New("restore failed")
			}
			return nil
		},
	})

	if err := m.EnterRaw(); err != nil {
		t.Fatalf("EnterRaw returned error: %v", err)
	}

	err := m.Restore()
	if err == nil {
		t.Fatal("first Restore returned nil error, want non-nil")
	}

	err = m.Restore()
	if err != nil {
		t.Fatalf("second Restore returned error: %v", err)
	}
	if restoreCalls != 2 {
		t.Fatalf("restore call count = %d, want 2", restoreCalls)
	}
}

type failingWriter struct{}

func (f *failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}
