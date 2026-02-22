package rpc

type RequestEnvelope[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  T      `json:"params,omitempty"`
}

type ResponseEnvelope[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  T      `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type BuildRequest struct {
	PlanID string `json:"plan_id"`
}

type BuildResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	Plan      Plan   `json:"plan"`
}

type InitializeRequest struct {
	WorldPath string `json:"world_path,omitempty"`
}

type InitializeResponse struct {
	SessionID    string            `json:"session_id"`
	Capabilities map[string]string `json:"capabilities,omitempty"`
}

type PlanRequest struct {
	Goal string `json:"goal"`
}

type PlanResponse struct {
	PlanID string `json:"plan_id"`
	Plan   Plan   `json:"plan"`
}

type ShutdownRequest struct{}

type ShutdownResponse struct {
	Status string `json:"status"`
}

type Plan struct {
	ID    string `json:"id"`
	Goal  string `json:"goal"`
	Steps []Step `json:"steps"`
}

type Step struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
