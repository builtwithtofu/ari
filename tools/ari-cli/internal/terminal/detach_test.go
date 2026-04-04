package terminal

import (
	"bytes"
	"testing"
)

func TestDetachScannerPassesThroughNormalBytes(t *testing.T) {
	t.Parallel()

	scanner := NewDetachScanner()
	input := []byte("hello world\n")

	passthrough, detach := scanner.Scan(input)
	if detach {
		t.Fatal("detach = true, want false")
	}
	if !bytes.Equal(passthrough, input) {
		t.Fatalf("passthrough = %q, want %q", passthrough, input)
	}
}

func TestDetachScannerTriggersOnCtrlBackslash(t *testing.T) {
	t.Parallel()

	scanner := NewDetachScanner()
	input := []byte{0x1c}

	passthrough, detach := scanner.Scan(input)
	if !detach {
		t.Fatal("detach = false, want true")
	}
	if len(passthrough) != 0 {
		t.Fatalf("passthrough length = %d, want 0", len(passthrough))
	}
}

func TestDetachScannerSplitsSequenceContainingDetachByte(t *testing.T) {
	t.Parallel()

	scanner := NewDetachScanner()
	input := []byte{'a', 'b', 0x1c, 'c', 'd'}

	passthrough, detach := scanner.Scan(input)
	if !detach {
		t.Fatal("detach = false, want true")
	}

	want := []byte{'a', 'b'}
	if !bytes.Equal(passthrough, want) {
		t.Fatalf("passthrough = %q, want %q", passthrough, want)
	}
}

func TestDetachScannerAllowsCustomMatcher(t *testing.T) {
	t.Parallel()

	scanner := NewDetachScannerWithMatcher(func(b byte) bool { return b == '!' })
	input := []byte("ping!pong")

	passthrough, detach := scanner.Scan(input)
	if !detach {
		t.Fatal("detach = false, want true")
	}
	if got, want := string(passthrough), "ping"; got != want {
		t.Fatalf("passthrough = %q, want %q", got, want)
	}
}
