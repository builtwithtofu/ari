package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// harnessRPCNotification is a provider-initiated message on a JSONL RPC
// stream (a message without a request id).
type harnessRPCNotification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type harnessRPCMessage struct {
	ID     *int64           `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params json.RawMessage  `json:"params,omitempty"`
	Result json.RawMessage  `json:"result,omitempty"`
	Error  *harnessRPCError `json:"error,omitempty"`
}

type harnessRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// jsonlRPCTransport speaks newline-delimited JSON-RPC over a child process's
// stdin/stdout. It is shared by harness adapters whose providers expose an
// app-server/RPC/ACP protocol (codex app-server, pi --mode rpc, grok agent
// stdio). Terminal notifications are classified per harness so a full
// notification buffer never drops the event that ends a turn.
type jsonlRPCTransport struct {
	harness       string
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	notifications chan harnessRPCNotification
	responses     map[int64]chan harnessRPCMessage
	// pending holds responses read before their caller registered a channel,
	// so a fast provider stream cannot drop an in-flight call's response.
	pending   map[int64]harnessRPCMessage
	terminal  func(harnessRPCNotification) bool
	mu        sync.Mutex
	nextID    int64
	closed    chan struct{}
	closeOnce sync.Once
}

func newJSONLRPCTransport(harness string, cmd *exec.Cmd, stdin io.WriteCloser, stdout io.Reader, stderr io.Reader, notificationCap int, terminal func(harnessRPCNotification) bool) *jsonlRPCTransport {
	if notificationCap <= 0 {
		notificationCap = 64
	}
	if terminal == nil {
		terminal = func(harnessRPCNotification) bool { return false }
	}
	transport := &jsonlRPCTransport{harness: strings.TrimSpace(harness), cmd: cmd, stdin: stdin, notifications: make(chan harnessRPCNotification, notificationCap), responses: make(map[int64]chan harnessRPCMessage), pending: make(map[int64]harnessRPCMessage), terminal: terminal, closed: make(chan struct{})}
	go transport.readMessages(stdout)
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	return transport
}

func (t *jsonlRPCTransport) PID() int {
	if t == nil || t.cmd == nil || t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

func (t *jsonlRPCTransport) ProcessSample(ctx context.Context) *ProcessMetricsSample {
	pid := t.PID()
	if pid <= 0 {
		return nil
	}
	sample := sampleLinuxProcessMetrics(ctx, HarnessSession{PID: pid})
	return &sample
}

func (t *jsonlRPCTransport) Call(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&t.nextID, 1)
	responseCh := make(chan harnessRPCMessage, 1)
	t.mu.Lock()
	if response, ok := t.pending[id]; ok {
		delete(t.pending, id)
		responseCh <- response
	}
	t.responses[id] = responseCh
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.responses, id)
		t.mu.Unlock()
	}()
	message := map[string]any{"id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode %s %s request: %w", t.harness, method, err)
	}
	if _, err := t.stdin.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write %s %s request: %w", t.harness, method, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closed:
		select {
		case response := <-responseCh:
			return t.decodeCallResponse(method, response, result)
		default:
		}
		return fmt.Errorf("%s transport closed before %s response", t.harness, method)
	case response := <-responseCh:
		return t.decodeCallResponse(method, response, result)
	}
}

func (t *jsonlRPCTransport) decodeCallResponse(method string, response harnessRPCMessage, result any) error {
	if response.Error != nil {
		return fmt.Errorf("%s %s error %d: %s", t.harness, method, response.Error.Code, response.Error.Message)
	}
	if result == nil || len(response.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("decode %s %s response: %w", t.harness, method, err)
	}
	return nil
}

func (t *jsonlRPCTransport) Notify(ctx context.Context, method string, params any) error {
	message := map[string]any{"method": method}
	if params != nil {
		message["params"] = params
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode %s %s notification: %w", t.harness, method, err)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closed:
		return fmt.Errorf("%s transport closed before %s notification", t.harness, method)
	default:
	}
	if _, err := t.stdin.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write %s %s notification: %w", t.harness, method, err)
	}
	return nil
}

func (t *jsonlRPCTransport) Notifications() <-chan harnessRPCNotification {
	return t.notifications
}

func (t *jsonlRPCTransport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		_ = t.stdin.Close()
		if t.cmd != nil && t.cmd.Process != nil {
			err = t.cmd.Process.Kill()
		}
		if t.cmd != nil {
			_, _ = waitWithTimeout(t.cmd, 2*time.Second)
		}
		close(t.closed)
	})
	return err
}

func waitWithTimeout(cmd *exec.Cmd, timeout time.Duration) (struct{}, error) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return struct{}{}, err
	case <-time.After(timeout):
		return struct{}{}, fmt.Errorf("process did not exit within %s", timeout)
	}
}

func (t *jsonlRPCTransport) readMessages(stdout io.Reader) {
	defer func() {
		close(t.notifications)
		t.closeOnce.Do(func() { close(t.closed) })
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var message harnessRPCMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		if message.ID != nil {
			t.mu.Lock()
			responseCh := t.responses[*message.ID]
			if responseCh == nil {
				t.pending[*message.ID] = message
			}
			t.mu.Unlock()
			if responseCh != nil {
				responseCh <- message
			}
			continue
		}
		if strings.TrimSpace(message.Method) != "" {
			t.deliverNotification(harnessRPCNotification{Method: message.Method, Params: message.Params})
		}
	}
}

func (t *jsonlRPCTransport) deliverNotification(notification harnessRPCNotification) {
	select {
	case t.notifications <- notification:
		return
	default:
	}
	if !t.terminal(notification) {
		return
	}
	select {
	case <-t.notifications:
	default:
	}
	select {
	case t.notifications <- notification:
	default:
	}
}
