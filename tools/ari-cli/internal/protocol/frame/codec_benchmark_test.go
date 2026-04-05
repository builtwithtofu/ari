package frame

import (
	"bytes"
	"testing"
)

func BenchmarkWriteFrame(b *testing.B) {
	benchmarks := []struct {
		name    string
		payload []byte
	}{
		{name: "64B", payload: bytes.Repeat([]byte("a"), 64)},
		{name: "1KB", payload: bytes.Repeat([]byte("a"), 1024)},
		{name: "32KB", payload: bytes.Repeat([]byte("a"), 32*1024)},
	}

	for _, tc := range benchmarks {
		b.Run(tc.name, func(b *testing.B) {
			var out bytes.Buffer
			msg := Frame{Type: TypeDataClientToServer, Payload: tc.payload}
			b.SetBytes(int64(len(tc.payload) + 5))
			b.ReportAllocs()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out.Reset()
				if err := WriteFrame(&out, msg); err != nil {
					b.Fatalf("WriteFrame returned error: %v", err)
				}
			}
		})
	}
}

func BenchmarkReadFrame(b *testing.B) {
	benchmarks := []struct {
		name    string
		payload []byte
	}{
		{name: "64B", payload: bytes.Repeat([]byte("a"), 64)},
		{name: "1KB", payload: bytes.Repeat([]byte("a"), 1024)},
		{name: "32KB", payload: bytes.Repeat([]byte("a"), 32*1024)},
	}

	for _, tc := range benchmarks {
		b.Run(tc.name, func(b *testing.B) {
			var encoded bytes.Buffer
			if err := WriteFrame(&encoded, Frame{Type: TypeDataServerToClient, Payload: tc.payload}); err != nil {
				b.Fatalf("WriteFrame setup returned error: %v", err)
			}
			data := append([]byte(nil), encoded.Bytes()...)

			var reader bytes.Reader
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				reader.Reset(data)
				msg, err := ReadFrame(&reader)
				if err != nil {
					b.Fatalf("ReadFrame returned error: %v", err)
				}
				if len(msg.Payload) != len(tc.payload) {
					b.Fatalf("payload length = %d, want %d", len(msg.Payload), len(tc.payload))
				}
			}
		})
	}
}
