package rpc

import (
	"encoding/json"
	"testing"
)

type echoParams struct {
	Message string `json:"message"`
}

func TestRequestEnvelopeMarshal(t *testing.T) {
	payload := RequestEnvelope[echoParams]{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "daemon.status",
		Params: echoParams{
			Message: "ok",
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request envelope: %v", err)
	}

	got := string(data)
	want := `{"jsonrpc":"2.0","id":1,"method":"daemon.status","params":{"message":"ok"}}`
	if got != want {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", want, got)
	}
}

func TestResponseEnvelopeMarshal(t *testing.T) {
	payload := ResponseEnvelope[map[string]string]{
		JSONRPC: "2.0",
		ID:      "req-1",
		Result: map[string]string{
			"status": "running",
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal response envelope: %v", err)
	}

	got := string(data)
	want := `{"jsonrpc":"2.0","id":"req-1","result":{"status":"running"}}`
	if got != want {
		t.Fatalf("unexpected JSON\nwant: %s\ngot:  %s", want, got)
	}
}
