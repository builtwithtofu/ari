package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type decodedEvent struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
}

func splitJSONLLines(t *testing.T, output string) []string {
	t.Helper()

	trimmed := strings.TrimSuffix(output, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

func stripTimestamp(t *testing.T, line string) string {
	t.Helper()

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	delete(envelope, "timestamp")

	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope without timestamp: %v", err)
	}

	return string(data)
}

func TestEmitterJSONLFramingTypesAndAnswerReceivedPayload(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := &Emitter{output: buf}

	if err := emitter.EmitSessionStart("sess-1", "plan"); err != nil {
		t.Fatalf("emit session_start: %v", err)
	}

	if err := emitter.EmitToolCall("call-1", "read", map[string]interface{}{"b": 2, "a": 1}); err != nil {
		t.Fatalf("emit tool_call: %v", err)
	}

	if err := emitter.EmitAnswerReceived("q-1"); err != nil {
		t.Fatalf("emit answer_received: %v", err)
	}

	lines := splitJSONLLines(t, buf.String())
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", len(lines))
	}

	expectedTypes := []string{"session_start", "tool_call", "answer_received"}
	for i, line := range lines {
		var event decodedEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("unmarshal line %d: %v", i, err)
		}

		if event.Type != expectedTypes[i] {
			t.Fatalf("line %d: expected type %q, got %q", i, expectedTypes[i], event.Type)
		}

		if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
			t.Fatalf("line %d: invalid timestamp %q: %v", i, event.Timestamp, err)
		}
	}

	if !strings.Contains(lines[1], `"args":{"a":1,"b":2}`) {
		t.Fatalf("tool_call args are not encoded in stable sorted-key order: %s", lines[1])
	}

	var answerReceivedEnvelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lines[2]), &answerReceivedEnvelope); err != nil {
		t.Fatalf("unmarshal answer_received envelope: %v", err)
	}

	if _, ok := answerReceivedEnvelope["data"]; ok {
		t.Fatalf("answer_received should not include data payload, got: %s", lines[2])
	}
}

func TestEmitterToolCallEncodingDeterministicAcrossEmits(t *testing.T) {
	buf := &bytes.Buffer{}
	emitter := &Emitter{output: buf}

	args := map[string]interface{}{"z": 9, "a": 1, "m": 5}
	if err := emitter.EmitToolCall("call-1", "read", args); err != nil {
		t.Fatalf("first emit tool_call: %v", err)
	}
	if err := emitter.EmitToolCall("call-1", "read", args); err != nil {
		t.Fatalf("second emit tool_call: %v", err)
	}

	lines := splitJSONLLines(t, buf.String())
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}

	first := stripTimestamp(t, lines[0])
	second := stripTimestamp(t, lines[1])

	if first != second {
		t.Fatalf("expected deterministic encoding across emits; first=%s second=%s", first, second)
	}

	if !strings.Contains(first, `"args":{"a":1,"m":5,"z":9}`) {
		t.Fatalf("unexpected args encoding order: %s", first)
	}
}
