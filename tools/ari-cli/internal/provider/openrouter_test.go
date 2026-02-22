package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type mockHTTPClient struct {
	responses []mockResponse
	callCount int
}

type mockResponse struct {
	statusCode int
	body       string
	err        error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.callCount >= len(m.responses) {
		return nil, errors.New("no more mock responses")
	}
	resp := m.responses[m.callCount]
	m.callCount++
	if resp.err != nil {
		return nil, resp.err
	}
	return &http.Response{
		StatusCode: resp.statusCode,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Header:     make(http.Header),
	}, nil
}

func TestOpenRouterProviderImplementsProvider(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: `{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`},
		},
	}
	p, err := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	p.SetHTTPClient(client)

	var provider Provider = p
	if provider.Name() != "openrouter" {
		t.Fatalf("provider name = %q, want %q", provider.Name(), "openrouter")
	}
}

func TestNewOpenRouterProvider_ValidModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		valid bool
	}{
		{"gemini flash", ModelGeminiFlash15, true},
		{"llama 3.1", ModelLlama318BInstruct, true},
		{"invalid model", "invalid/model", false},
		{"empty model", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOpenRouterProvider("test-key", tt.model)
			if tt.valid && err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestNewOpenRouterProvider_EnvVar(t *testing.T) {
	os.Setenv("OPENROUTER_API_KEY", "env-key")
	defer os.Unsetenv("OPENROUTER_API_KEY")

	p, err := NewOpenRouterProvider("", ModelGeminiFlash15)
	if err != nil {
		t.Fatalf("failed to create provider from env: %v", err)
	}
	if p == nil {
		t.Fatal("provider is nil")
	}
}

func TestNewOpenRouterProvider_MissingEnvVar(t *testing.T) {
	os.Unsetenv("OPENROUTER_API_KEY")

	_, err := NewOpenRouterProvider("", ModelGeminiFlash15)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
	if !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("error message = %q, want to contain OPENROUTER_API_KEY", err.Error())
	}
}

func TestOpenRouterProviderComplete_Success(t *testing.T) {
	mockResp := `{
		"id": "test-id",
		"model": "google/gemini-flash-1.5",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "Hello! How can I help you?"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30
		}
	}`

	client := &mockHTTPClient{
		responses: []mockResponse{{statusCode: 200, body: mockResp}},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{
			{Role: MessageRoleUser, Content: "Hello"},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}

	if resp.Message.Content != "Hello! How can I help you?" {
		t.Fatalf("content = %q, want %q", resp.Message.Content, "Hello! How can I help you?")
	}
	if resp.FinishReason != FinishReasonStop {
		t.Fatalf("finish reason = %q, want %q", resp.FinishReason, FinishReasonStop)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want %d", resp.Usage.TotalTokens, 30)
	}
}

func TestOpenRouterProviderComplete_ToolCalls(t *testing.T) {
	mockResp := `{
		"id": "test-id",
		"model": "google/gemini-flash-1.5",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [{
					"id": "call_123",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"location\": \"San Francisco\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {
			"prompt_tokens": 15,
			"completion_tokens": 25,
			"total_tokens": 40
		}
	}`

	client := &mockHTTPClient{
		responses: []mockResponse{{statusCode: 200, body: mockResp}},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{
			{Role: MessageRoleUser, Content: "What's the weather?"},
		},
		Tools: []ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get weather for a location",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}

	if resp.FinishReason != FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want %q", resp.FinishReason, FinishReasonToolCalls)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.Message.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("tool name = %q, want %q", resp.Message.ToolCalls[0].Name, "get_weather")
	}
}

func TestOpenRouterProviderComplete_RateLimitRetry(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{
			{statusCode: 429, body: "rate limited"},
			{statusCode: 429, body: "rate limited again"},
			{statusCode: 200, body: `{"choices":[{"message":{"role":"assistant","content":"success"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`},
		},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete returned error after retries: %v", err)
	}

	if resp.Message.Content != "success" {
		t.Fatalf("content = %q, want %q", resp.Message.Content, "success")
	}
	if client.callCount != 3 {
		t.Fatalf("expected 3 API calls (2 retries + 1 success), got %d", client.callCount)
	}
}

func TestOpenRouterProviderComplete_RateLimitMaxRetriesExceeded(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{
			{statusCode: 429, body: "rate limited 1"},
			{statusCode: 429, body: "rate limited 2"},
			{statusCode: 429, body: "rate limited 3"},
			{statusCode: 429, body: "rate limited 4"},
		},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after max retries exceeded")
	}
	if !strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("error = %q, want to contain 'max retries exceeded'", err.Error())
	}
}

func TestOpenRouterProviderComplete_NonRetryableError(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{
			{statusCode: 500, body: "internal server error"},
		},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
	if client.callCount != 1 {
		t.Fatalf("expected 1 API call (no retry for 500), got %d", client.callCount)
	}
}

func TestOpenRouterProviderComplete_ContextCancellation(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	_, err := p.Complete(ctx, req)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestOpenRouterProviderComplete_Timeout(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: `{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`},
		},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond)

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	_, err := p.Complete(ctx, req)
	if err == nil {
		t.Fatal("expected error for timed out context")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestOpenRouterProviderComplete_APIError(t *testing.T) {
	mockResp := `{
		"error": {
			"message": "Invalid API key",
			"code": 401,
			"type": "authentication_error"
		}
	}`

	client := &mockHTTPClient{
		responses: []mockResponse{{statusCode: 200, body: mockResp}},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("error = %q, want to contain 'Invalid API key'", err.Error())
	}
}

func TestOpenRouterProviderComplete_NoChoices(t *testing.T) {
	mockResp := `{
		"id": "test-id",
		"choices": [],
		"usage": {"prompt_tokens": 1, "completion_tokens": 0, "total_tokens": 1}
	}`

	client := &mockHTTPClient{
		responses: []mockResponse{{statusCode: 200, body: mockResp}},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for no choices")
	}
	if !strings.Contains(err.Error(), "no completion choices") {
		t.Fatalf("error = %q, want to contain 'no completion choices'", err.Error())
	}
}

func TestOpenRouterProviderComplete_NetworkError(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{{err: errors.New("connection refused")}},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	req := CompletionRequest{
		Messages: []Message{{Role: MessageRoleUser, Content: "test"}},
	}

	_, err := p.Complete(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for network failure")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("error = %q, want to contain 'connection refused'", err.Error())
	}
}

func TestOpenRouterProviderComplete_BuildRequestBody(t *testing.T) {
	client := &mockHTTPClient{
		responses: []mockResponse{
			{statusCode: 200, body: `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`},
		},
	}

	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)
	p.SetHTTPClient(client)

	temp := 0.7
	req := CompletionRequest{
		Model: ModelLlama318BInstruct,
		Messages: []Message{
			{Role: MessageRoleSystem, Content: "You are helpful"},
			{Role: MessageRoleUser, Content: "Hello"},
		},
		Tools: []ToolDefinition{
			{
				Name:        "search",
				Description: "Search docs",
				InputSchema: map[string]any{"type": "object"},
			},
		},
		ToolChoice:  ToolChoiceAuto,
		Temperature: &temp,
		MaxTokens:   100,
	}

	_, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete returned error: %v", err)
	}
}

func TestRateLimitError(t *testing.T) {
	err := &RateLimitError{Message: "too many requests"}
	if err.Error() != "rate limit exceeded: too many requests" {
		t.Fatalf("error message = %q, want %q", err.Error(), "rate limit exceeded: too many requests")
	}
}

func TestOpenRouterProvider_RequestBodyJSON(t *testing.T) {
	p, _ := NewOpenRouterProvider("test-key", ModelGeminiFlash15)

	req := CompletionRequest{
		Messages: []Message{
			{Role: MessageRoleUser, Content: "Hello"},
		},
	}

	body := p.buildRequestBody(req)
	if body.Model != ModelGeminiFlash15 {
		t.Fatalf("model = %q, want %q", body.Model, ModelGeminiFlash15)
	}
	if len(body.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(body.Messages))
	}

	jsonBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}

	if _, ok := decoded["model"]; !ok {
		t.Fatal("request body missing 'model' field")
	}
}
