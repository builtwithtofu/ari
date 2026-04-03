package process

import (
	"bytes"
	"sync"
	"testing"
)

func TestRingBufferWriteAndSnapshot(t *testing.T) {
	rb := NewRingBuffer(32)

	n, err := rb.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len("hello world") {
		t.Fatalf("Write n = %d, want %d", n, len("hello world"))
	}

	got := rb.Snapshot()
	if !bytes.Equal(got, []byte("hello world")) {
		t.Fatalf("Snapshot() = %q, want %q", string(got), "hello world")
	}
}

func TestRingBufferLines(t *testing.T) {
	rb := NewRingBuffer(64)

	_, err := rb.Write([]byte("alpha\nbeta\ngamma"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	got := rb.Lines()
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("Lines len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Lines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRingBufferWraparoundKeepsNewestBytes(t *testing.T) {
	rb := NewRingBuffer(8)

	if _, err := rb.Write([]byte("12345")); err != nil {
		t.Fatalf("first Write returned error: %v", err)
	}
	if _, err := rb.Write([]byte("6789")); err != nil {
		t.Fatalf("second Write returned error: %v", err)
	}

	got := rb.Snapshot()
	if !bytes.Equal(got, []byte("23456789")) {
		t.Fatalf("Snapshot() = %q, want %q", string(got), "23456789")
	}
}

func TestRingBufferConcurrentWriteAndSnapshot(t *testing.T) {
	rb := NewRingBuffer(256)

	const writers = 8
	const writesPerWriter = 200

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerWriter; j++ {
				if _, err := rb.Write([]byte("chunk\n")); err != nil {
					t.Errorf("Write returned error: %v", err)
					return
				}
				_ = rb.Snapshot()
			}
		}()
	}

	wg.Wait()

	got := rb.Snapshot()
	if len(got) > 256 {
		t.Fatalf("Snapshot length = %d, want <= 256", len(got))
	}
	if len(got) == 0 {
		t.Fatal("Snapshot is empty after concurrent writes")
	}
}
