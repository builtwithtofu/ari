package rpc

import (
	"context"
	"errors"
	"reflect"
)

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
	}
}

func (r *MethodRegistry) Register(method MethodSpec) error {
	if method.Name == "" {
		return errors.New("method name is required")
	}

	if _, exists := r.methods[method.Name]; exists {
		return errors.New("method already registered")
	}

	r.methods[method.Name] = method

	return nil
}

func (r *MethodRegistry) Get(name string) (MethodSpec, bool) {
	method, ok := r.methods[name]
	return method, ok
}
