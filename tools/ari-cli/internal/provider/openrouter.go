package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	openrouterAPIEndpoint  = "https://openrouter.ai/api/v1/chat/completions"
	openrouterProviderName = "openrouter"
	maxRetries             = 3
	initialBackoff         = 1 * time.Second
)

// Supported free tier models
const (
	ModelGeminiFlash15     = "google/gemini-flash-1.5"
	ModelLlama318BInstruct = "meta-llama/llama-3.1-8b-instruct"
)

// HTTPClient interface for testability
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// OpenRouterProvider implements the Provider interface for OpenRouter API
type OpenRouterProvider struct {
	apiKey     string
	model      string
	httpClient HTTPClient
	baseURL    string
}


type openrouterRequest struct {
	Model       string              `json:"model"`
	Messages    []openrouterMessage `json:"messages"`
	Tools       []openrouterTool    `json:"tools,omitempty"`
	ToolChoice  string              `json:"tool_choice,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
}

type openrouterMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content"`
	Name       string               `json:"name,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []openrouterToolCall `json:"tool_calls,omitempty"`
}

type openrouterTool struct {
	Type     string             `json:"type"`
	Function openrouterFunction `json:"function"`
}

type openrouterFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type openrouterToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}


type openrouterResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Role       string               `json:"role"`
			Content    string               `json:"content"`
			ToolCalls  []openrouterToolCall `json:"tool_calls,omitempty"`
			ToolCallID string               `json:"tool_call_id,omitempty"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// NewOpenRouterProvider creates a new OpenRouter provider with the given API key and model.
// If apiKey is empty, it reads from OPENROUTER_API_KEY environment variable.
func NewOpenRouterProvider(apiKey, model string) (*OpenRouterProvider, error) {
	if apiKey == "" {
		apiKey = os.Getenv("OPENROUTER_API_KEY")
		if apiKey == "" {
			return nil, errors.New("OPENROUTER_API_KEY environment variable not set")
		}
	}

	if model == "" {
		return nil, errors.New("model is required")
	}


	if !isSupportedModel(model) {
		return nil, fmt.Errorf("unsupported model %q: only free tier models are supported (gemini-flash-1.5, llama-3.1-8b-instruct)", model)
	}

	return &OpenRouterProvider{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    openrouterAPIEndpoint,
	}, nil
}


func isSupportedModel(model string) bool {
	switch model {
	case ModelGeminiFlash15, ModelLlama318BInstruct:
		return true
	default:
		return false
	}
}


func (p *OpenRouterProvider) Name() string {
	return openrouterProviderName
}


func (p *OpenRouterProvider) Complete(ctx context.Context, request CompletionRequest) (CompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return CompletionResponse{}, err
	}


	reqBody := p.buildRequestBody(request)

	var lastErr error
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return CompletionResponse{}, ctx.Err()
			case <-time.After(backoff):
				backoff *= 2
			}
		}

		resp, err := p.doRequest(ctx, reqBody)
		if err == nil {
			return resp, nil
		}

		lastErr = err

		var rateLimitErr *RateLimitError
		if !errors.As(err, &rateLimitErr) {
			return CompletionResponse{}, err
		}

		if attempt < maxRetries {
			continue
		}
	}

	return CompletionResponse{}, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// RateLimitError represents a 429 rate limit error
type RateLimitError struct {
	Message string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded: %s", e.Message)
}


func (p *OpenRouterProvider) doRequest(ctx context.Context, reqBody openrouterRequest) (CompletionResponse, error) {
	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to create request: %w", err)
	}


	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://ari-cli.dev")
	req.Header.Set("X-Title", "Ari CLI")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to read response body: %w", err)
	}


	if resp.StatusCode == http.StatusTooManyRequests {
		return CompletionResponse{}, &RateLimitError{Message: string(body)}
	}


	if resp.StatusCode != http.StatusOK {
		return CompletionResponse{}, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp openrouterResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to unmarshal response: %w", err)
	}


	if apiResp.Error != nil {
		return CompletionResponse{}, fmt.Errorf("API error: %s (code: %d)", apiResp.Error.Message, apiResp.Error.Code)
	}

	if len(apiResp.Choices) == 0 {
		return CompletionResponse{}, errors.New("no completion choices returned")
	}

	return p.parseResponse(apiResp), nil
}


func (p *OpenRouterProvider) buildRequestBody(request CompletionRequest) openrouterRequest {
	reqBody := openrouterRequest{
		Model:    p.model,
		Messages: make([]openrouterMessage, len(request.Messages)),
	}


	for i, msg := range request.Messages {
		reqBody.Messages[i] = openrouterMessage{
			Role:       string(msg.Role),
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}


		if len(msg.ToolCalls) > 0 {
			reqBody.Messages[i].ToolCalls = make([]openrouterToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				reqBody.Messages[i].ToolCalls[j] = openrouterToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Name,
						Arguments: string(argsJSON),
					},
				}
			}
		}
	}


	if len(request.Tools) > 0 {
		reqBody.Tools = make([]openrouterTool, len(request.Tools))
		for i, tool := range request.Tools {
			reqBody.Tools[i] = openrouterTool{
				Type: "function",
				Function: openrouterFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			}
		}
	}


	if request.ToolChoice != "" {
		reqBody.ToolChoice = string(request.ToolChoice)
	}


	if request.Temperature != nil {
		reqBody.Temperature = request.Temperature
	}
	if request.MaxTokens > 0 {
		reqBody.MaxTokens = request.MaxTokens
	}

	return reqBody
}


func (p *OpenRouterProvider) parseResponse(apiResp openrouterResponse) CompletionResponse {
	choice := apiResp.Choices[0]

	response := CompletionResponse{
		Model: apiResp.Model,
		Message: Message{
			Role:    MessageRole(choice.Message.Role),
			Content: choice.Message.Content,
		},
		FinishReason: FinishReason(choice.FinishReason),
		Usage: TokenUsage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
		},
	}


	if len(choice.Message.ToolCalls) > 0 {
		response.Message.ToolCalls = make([]ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			response.Message.ToolCalls[i] = ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			}
		}
	}

	return response
}

// SetHTTPClient allows setting a custom HTTP client (mainly for testing)
func (p *OpenRouterProvider) SetHTTPClient(client HTTPClient) {
	p.httpClient = client
}
