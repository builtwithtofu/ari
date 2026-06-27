package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/protocol/rpc"
)

func callMethod[T any](t *testing.T, registry *rpc.MethodRegistry, methodName string, params any) T {
	t.Helper()
	result, err := callMethodResult[T](registry, methodName, params)
	if err != nil {
		t.Fatalf("call %s returned error: %v", methodName, err)
	}
	return result
}

func callMethodResult[T any](registry *rpc.MethodRegistry, methodName string, params any) (T, error) {
	var zero T
	spec, ok := registry.Get(methodName)
	if !ok {
		return zero, rpc.NewHandlerError(rpc.MethodNotFound, "method not registered", map[string]any{"method": methodName})
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return zero, err
	}
	resultAny, err := spec.Call(context.Background(), raw)
	if err != nil {
		return zero, err
	}
	result, ok := resultAny.(T)
	if !ok {
		return zero, rpc.NewHandlerError(rpc.InternalError, "unexpected result type", map[string]any{"method": methodName})
	}
	return result, nil
}

func callMethodError(registry *rpc.MethodRegistry, methodName string, params any) error {
	_, err := callMethodResult[any](registry, methodName, params)
	return err
}
