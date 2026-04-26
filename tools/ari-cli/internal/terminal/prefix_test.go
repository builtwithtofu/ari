package terminal

import (
	"bytes"
	"testing"
)

func TestPrefixScannerPassesThroughNormalBytes(t *testing.T) {
	t.Parallel()

	scanner := NewPrefixScanner()
	input := []byte("hello world\n")

	passthrough, detach := scanner.Scan(input)
	if detach {
		t.Fatal("detach = true, want false")
	}
	if !bytes.Equal(passthrough, input) {
		t.Fatalf("passthrough = %q, want %q", passthrough, input)
	}
}

func TestPrefixScannerTriggersDetachAndStripsSuffix(t *testing.T) {
	t.Parallel()

	scanner := NewPrefixScanner()
	input := []byte{'a', 'b', DefaultDetachPrefix, 'c', 'd'}

	passthrough, detach := scanner.Scan(input)
	if !detach {
		t.Fatal("detach = false, want true")
	}
	want := []byte{'a', 'b'}
	if !bytes.Equal(passthrough, want) {
		t.Fatalf("passthrough = %q, want %q", passthrough, want)
	}
}
