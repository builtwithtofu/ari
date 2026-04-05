package daemon

import (
	"bytes"
	"io"
	"testing"
)

func BenchmarkWriteAllBytes(b *testing.B) {
	benchmarks := []struct {
		name      string
		payload   []byte
		chunkSize int
	}{
		{name: "4KB-full-write", payload: bytes.Repeat([]byte("x"), 4096), chunkSize: 4096},
		{name: "4KB-short-write", payload: bytes.Repeat([]byte("x"), 4096), chunkSize: 128},
		{name: "32KB-short-write", payload: bytes.Repeat([]byte("x"), 32*1024), chunkSize: 256},
	}

	for _, tc := range benchmarks {
		b.Run(tc.name, func(b *testing.B) {
			writer := &benchmarkShortWriter{maxChunk: tc.chunkSize}
			b.SetBytes(int64(len(tc.payload)))
			b.ReportAllocs()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				writer.reset()
				if err := writeAllBytes(writer, tc.payload); err != nil {
					b.Fatalf("writeAllBytes returned error: %v", err)
				}
				if writer.total != len(tc.payload) {
					b.Fatalf("writer total = %d, want %d", writer.total, len(tc.payload))
				}
			}
		})
	}
}

type benchmarkShortWriter struct {
	maxChunk int
	total    int
}

func (w *benchmarkShortWriter) Write(p []byte) (int, error) {
	if w.maxChunk <= 0 {
		return 0, io.ErrShortWrite
	}
	if len(p) <= w.maxChunk {
		w.total += len(p)
		return len(p), nil
	}
	w.total += w.maxChunk
	return w.maxChunk, nil
}

func (w *benchmarkShortWriter) reset() {
	w.total = 0
}
