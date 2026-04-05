package client

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/frame"
)

func BenchmarkAttachSessionSendData(b *testing.B) {
	payload := bytes.Repeat([]byte("x"), 4096)
	serverConn, clientConn := net.Pipe()
	b.Cleanup(func() {
		_ = serverConn.Close()
		_ = clientConn.Close()
	})

	session := &AttachSession{conn: clientConn}

	ctx, cancel := context.WithCancel(context.Background())
	b.Cleanup(cancel)
	readErrCh := make(chan error, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			if err := serverConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
				return
			}
			msg, err := frame.ReadFrame(serverConn)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			if msg.Type != frame.TypeDataClientToServer {
				select {
				case readErrCh <- fmt.Errorf("frame type = %d, want %d", msg.Type, frame.TypeDataClientToServer):
				default:
				}
				return
			}
		}
	}()

	b.SetBytes(int64(len(payload) + 5))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		select {
		case err := <-readErrCh:
			b.Fatalf("background read failed: %v", err)
		default:
		}
		if err := session.SendData(payload); err != nil {
			b.Fatalf("SendData returned error: %v", err)
		}
	}
	b.StopTimer()

	cancel()
	_ = session.Close()
	wg.Wait()
}
