package fakeharness

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func deliveryClaudePTY(w *lineSink, stdin string) int {
	if strings.TrimSpace(stdin) == "" {
		w.line(`{"channel":"managed_pty","status":"failed","error":"empty_input"}`)
		return 2
	}
	inputHash := shortHash(stdin)
	w.linef(`{"channel":"managed_pty","status":"admitted","input_hash":"%s"}`, inputHash)
	w.linef(`{"channel":"managed_pty","status":"completed","input_hash":"%s","output":"fake claude pty response"}`, inputHash)
	return 0
}

func deliveryCodexAppServer(w *lineSink, stdin string) int {
	if !strings.Contains(stdin, "turn/start") {
		w.line(`{"jsonrpc":"2.0","error":{"code":-32600,"message":"expected turn/start"}}`)
		return 2
	}
	w.line(`{"jsonrpc":"2.0","id":1,"result":{"turn":{"id":"fake-codex-turn"}}}`)
	w.line(`{"method":"turn/completed","params":{"turn_id":"fake-codex-turn","status":"completed"}}`)
	return 0
}

// serveOpenCode runs the fake `opencode serve` HTTP server. It mounts both
// the provider-discovery endpoints used by auth-method discovery and the
// session delivery endpoints used by the daemon delivery loop, so dispatcher
// journeys can run against a real subprocess boundary.
func serveOpenCode(w *lineSink) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		w.linef("listen failed: %v", err)
		return 1
	}
	defer func() { _ = listener.Close() }()
	mux := http.NewServeMux()
	mux.HandleFunc("/provider", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"connected":["anthropic"]}`) })
	mux.HandleFunc("/provider/auth", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"anthropic":[{"type":"oauth","label":"browser"}]}`)
	})
	mux.Handle("/api/session/", OpenCodeDeliveryHandler(nil))
	server := &http.Server{Handler: mux}
	w.linef("http://%s", listener.Addr().String())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		w.linef("serve failed: %v", err)
		return 1
	}
	return 0
}

type OpenCodePromptDelivery struct {
	SessionID      string `json:"session_id"`
	PromptID       string `json:"prompt_id"`
	Delivery       string `json:"delivery"`
	IdempotencyKey string `json:"idempotency_key"`
	TextHash       string `json:"text_hash"`
}

func OpenCodeDeliveryHandler(record func(OpenCodePromptDelivery)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session/", func(w http.ResponseWriter, r *http.Request) {
		trimmed := strings.TrimPrefix(r.URL.Path, "/api/session/")
		parts := strings.Split(trimmed, "/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			http.NotFound(w, r)
			return
		}
		sessionID := parts[0]
		switch {
		case r.Method == http.MethodPost && parts[1] == "prompt":
			var body struct {
				Text           string `json:"text"`
				Delivery       string `json:"delivery"`
				IdempotencyKey string `json:"idempotency_key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if strings.TrimSpace(body.Delivery) == "" {
				body.Delivery = "queue"
			}
			delivery := OpenCodePromptDelivery{SessionID: sessionID, PromptID: "fake-opencode-prompt", Delivery: body.Delivery, IdempotencyKey: body.IdempotencyKey, TextHash: shortHash(body.Text)}
			if record != nil {
				record(delivery)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"session_id": delivery.SessionID, "prompt_id": delivery.PromptID, "status": "queued"})
		case r.Method == http.MethodGet && parts[1] == "events":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"events": []map[string]string{{"type": "prompt.completed", "session_id": sessionID, "prompt_id": "fake-opencode-prompt"}, {"type": "session.idle", "session_id": sessionID, "prompt_id": "fake-opencode-prompt"}}})
		default:
			http.NotFound(w, r)
		}
	})
	return mux
}
