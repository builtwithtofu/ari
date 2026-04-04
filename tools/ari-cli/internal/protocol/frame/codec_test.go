package frame

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
)

func TestWriteReadFrameRoundTripAllMessageTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		typ     Type
		payload []byte
	}{
		{name: "attach", typ: TypeAttach, payload: []byte(`{"token":"abc","cols":80,"rows":24}`)},
		{name: "data c2s", typ: TypeDataClientToServer, payload: []byte("echo hi\n")},
		{name: "data s2c", typ: TypeDataServerToClient, payload: []byte("output\n")},
		{name: "resize", typ: TypeResize, payload: []byte(`{"cols":100,"rows":40}`)},
		{name: "detach", typ: TypeDetach, payload: nil},
		{name: "snapshot", typ: TypeSnapshot, payload: []byte("snapshot-bytes")},
		{name: "error", typ: TypeError, payload: []byte("agent not running")},
		{name: "agent exited", typ: TypeAgentExited, payload: []byte(`{"exit_code":0}`)},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := WriteFrame(&buf, Frame{Type: tc.typ, Payload: tc.payload}); err != nil {
				t.Fatalf("WriteFrame returned error: %v", err)
			}

			got, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame returned error: %v", err)
			}
			if got.Type != tc.typ {
				t.Fatalf("ReadFrame type = %d, want %d", got.Type, tc.typ)
			}
			if !bytes.Equal(got.Payload, tc.payload) {
				t.Fatalf("ReadFrame payload = %q, want %q", got.Payload, tc.payload)
			}
		})
	}
}

func TestWriteReadFrameHandlesLargeAndEmptyPayload(t *testing.T) {
	t.Parallel()

	large := bytes.Repeat([]byte("x"), 64*1024)

	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "empty", payload: nil},
		{name: "large", payload: large},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := WriteFrame(&buf, Frame{Type: TypeDataServerToClient, Payload: tc.payload}); err != nil {
				t.Fatalf("WriteFrame returned error: %v", err)
			}

			got, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame returned error: %v", err)
			}
			if got.Type != TypeDataServerToClient {
				t.Fatalf("ReadFrame type = %d, want %d", got.Type, TypeDataServerToClient)
			}
			if !bytes.Equal(got.Payload, tc.payload) {
				t.Fatalf("ReadFrame payload length = %d, want %d", len(got.Payload), len(tc.payload))
			}
		})
	}
}

func TestWriteFrameRejectsInvalidType(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := WriteFrame(&buf, Frame{Type: Type(0xFF), Payload: []byte("bad")})
	if err == nil {
		t.Fatal("WriteFrame error = nil, want non-nil")
	}
}

func TestReadFrameRejectsInvalidType(t *testing.T) {
	t.Parallel()

	raw := []byte{0xFE, 0x01, 0x00, 0x00, 0x00, 'x'}
	_, err := ReadFrame(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("ReadFrame error = nil, want non-nil")
	}
}

func TestReadFrameReturnsEOFForTruncatedPayload(t *testing.T) {
	t.Parallel()

	raw := []byte{byte(TypeDataClientToServer), 0x04, 0x00, 0x00, 0x00, 'a', 'b'}
	_, err := ReadFrame(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("ReadFrame error = nil, want non-nil")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame error = %v, want EOF class error", err)
	}
}

func TestReadFrameRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	raw := []byte{byte(TypeDataClientToServer), 0x01, 0x00, 0x00, 0x01}
	_, err := ReadFrame(bytes.NewReader(raw))
	if err == nil {
		t.Fatal("ReadFrame error = nil, want non-nil")
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	oversized := bytes.Repeat([]byte("x"), int(MaxFramePayloadBytes)+1)
	err := WriteFrame(&bytes.Buffer{}, Frame{Type: TypeDataServerToClient, Payload: oversized})
	if err == nil {
		t.Fatal("WriteFrame error = nil, want non-nil")
	}
}

func TestWriteFrameHandlesShortWriter(t *testing.T) {
	t.Parallel()

	writer := &partialFrameWriter{maxBytesPerWrite: 2}

	err := WriteFrame(writer, Frame{Type: TypeDataServerToClient, Payload: []byte("abcdef")})
	if err != nil {
		t.Fatalf("WriteFrame returned error: %v", err)
	}

	got, err := ReadFrame(bytes.NewReader(writer.buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadFrame returned error: %v", err)
	}
	if got.Type != TypeDataServerToClient {
		t.Fatalf("frame type = %d, want %d", got.Type, TypeDataServerToClient)
	}
	if gotText := string(got.Payload); gotText != "abcdef" {
		t.Fatalf("frame payload = %q, want %q", gotText, "abcdef")
	}
}

func TestFrameCodecConcurrentReadWriteOnSingleConnection(t *testing.T) {
	t.Parallel()

	server, client := net.Pipe()
	defer func() {
		_ = server.Close()
	}()
	defer func() {
		_ = client.Close()
	}()

	want := Frame{Type: TypeDataClientToServer, Payload: []byte("ping")}

	var wg sync.WaitGroup
	wg.Add(2)

	errCh := make(chan error, 2)
	gotCh := make(chan Frame, 1)

	go func() {
		defer wg.Done()
		errCh <- WriteFrame(client, want)
	}()

	go func() {
		defer wg.Done()
		got, err := ReadFrame(server)
		if err != nil {
			errCh <- err
			return
		}
		gotCh <- got
		errCh <- nil
	}()

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent frame operation returned error: %v", err)
		}
	}

	got := <-gotCh
	if got.Type != want.Type {
		t.Fatalf("got type = %d, want %d", got.Type, want.Type)
	}
	if !bytes.Equal(got.Payload, want.Payload) {
		t.Fatalf("got payload = %q, want %q", got.Payload, want.Payload)
	}
}

type partialFrameWriter struct {
	buf              bytes.Buffer
	maxBytesPerWrite int
}

func (w *partialFrameWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	n := w.maxBytesPerWrite
	if n <= 0 {
		n = 1
	}
	if n > len(p) {
		n = len(p)
	}
	_, _ = w.buf.Write(p[:n])
	return n, nil
}
