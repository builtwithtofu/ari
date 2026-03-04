package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

var ErrInvalidMethodParams = errors.New("invalid method params")

type Method[Req any, Resp any] struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, req Req) (Resp, error)
}

type MethodSpec struct {
	Name         string
	Description  string
	RequestType  reflect.Type
	ResponseType reflect.Type
	Call         func(ctx context.Context, raw json.RawMessage) (any, error)
}

type MethodRegistry struct {
	methods map[string]MethodSpec
}

func NewMethodRegistry() *MethodRegistry {
	return &MethodRegistry{methods: make(map[string]MethodSpec)}
}

func MethodDefinition[Req any, Resp any](method Method[Req, Resp]) MethodSpec {
	var req Req
	var resp Resp

	return MethodSpec{
		Name:         method.Name,
		Description:  method.Description,
		RequestType:  reflect.TypeOf(req),
		ResponseType: reflect.TypeOf(resp),
		Call: func(ctx context.Context, raw json.RawMessage) (any, error) {
			if method.Handler == nil {
				return nil, fmt.Errorf("method %q handler is required", method.Name)
			}

			decoded := req
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &decoded); err != nil {
					return nil, fmt.Errorf("%w: %v", ErrInvalidMethodParams, err)
				}
			}

			result, err := method.Handler(ctx, decoded)
			if err != nil {
				return nil, err
			}

			return result, nil
		},
	}
}

func (r *MethodRegistry) Register(method MethodSpec) error {
	if method.Name == "" {
		return errors.New("method name is required")
	}

	if method.Call == nil {
		return errors.New("method handler is required")
	}

	if _, exists := r.methods[method.Name]; exists {
		return errors.New("method already registered")
	}

	r.methods[method.Name] = method

	return nil
}

func RegisterMethod[Req any, Resp any](r *MethodRegistry, method Method[Req, Resp]) error {
	if r == nil {
		return errors.New("method registry is required")
	}

	return r.Register(MethodDefinition(method))
}

func (r *MethodRegistry) Get(name string) (MethodSpec, bool) {
	method, ok := r.methods[name]
	return method, ok
}
