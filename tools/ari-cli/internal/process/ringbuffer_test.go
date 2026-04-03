package process

import (
	"bytes"
	"sync"
	"testing"
)

func TestRingBufferWriteAndSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		writes   [][]byte
		want     []byte
	}{
		{name: "single write", capacity: 32, writes: [][]byte{[]byte("hello world")}, want: []byte("hello world")},
		{name: "multiple writes append", capacity: 32, writes: [][]byte{[]byte("hello "), []byte("world")}, want: []byte("hello world")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rb := NewRingBuffer(tc.capacity)

			written := 0
			for _, chunk := range tc.writes {
				n, err := rb.Write(chunk)
				if err != nil {
					t.Fatalf("Write returned error: %v", err)
				}
				written += n
			}

			if written != len(tc.want) {
				t.Fatalf("total bytes written = %d, want %d", written, len(tc.want))
			}

			got := rb.Snapshot()
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("Snapshot() = %q, want %q", string(got), string(tc.want))
			}
		})
	}
}

func TestRingBufferLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "multiple lines", input: "alpha\nbeta\ngamma", want: []string{"alpha", "beta", "gamma"}},
		{name: "trailing newline trimmed", input: "alpha\nbeta\n", want: []string{"alpha", "beta"}},
		{name: "empty buffer returns no lines", input: "", want: []string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rb := NewRingBuffer(64)

			if _, err := rb.Write([]byte(tc.input)); err != nil {
				t.Fatalf("Write returned error: %v", err)
			}

			got := rb.Lines()
			if len(got) != len(tc.want) {
				t.Fatalf("Lines len = %d, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("Lines[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRingBufferWraparoundKeepsNewestBytes(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
		writes   [][]byte
		want     []byte
	}{
		{name: "overflow by one chunk", capacity: 8, writes: [][]byte{[]byte("12345"), []byte("6789")}, want: []byte("23456789")},
		{name: "single oversized write keeps tail", capacity: 5, writes: [][]byte{[]byte("abcdefghi")}, want: []byte("efghi")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rb := NewRingBuffer(tc.capacity)

			for i, chunk := range tc.writes {
				if _, err := rb.Write(chunk); err != nil {
					t.Fatalf("Write #%d returned error: %v", i+1, err)
				}
			}

			got := rb.Snapshot()
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("Snapshot() = %q, want %q", string(got), string(tc.want))
			}
		})
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
