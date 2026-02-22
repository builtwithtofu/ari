package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
)

type Tool interface {
	Name() string
	Description() string
	Execute(ctx context.Context, args map[string]any) (any, error)
}

type inputSchemaProvider interface {
	InputSchema() map[string]any
}

var (
	ErrUnknownTool        = errors.New("unknown tool")
	ErrToolNameRequired   = errors.New("tool name is required")
	ErrToolNil            = errors.New("tool is nil")
	ErrToolAlreadyDefined = errors.New("tool already registered")
)

type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

func (r *ToolRegistry) Register(name string, tool Tool) error {
	canonical := canonicalToolName(name)
	if canonical == "" {
		return ErrToolNameRequired
	}
	if tool == nil {
		return ErrToolNil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[canonical]; exists {
		return fmt.Errorf("%w: %q", ErrToolAlreadyDefined, canonical)
	}

	r.tools[canonical] = tool
	return nil
}

func (r *ToolRegistry) Get(name string) (Tool, error) {
	canonical := canonicalToolName(name)
	if canonical == "" {
		return nil, ErrToolNameRequired
	}

	r.mu.RLock()
	tool, ok := r.tools[canonical]
	available := toolNamesLocked(r.tools)
	r.mu.RUnlock()

	if ok {
		return tool, nil
	}

	if len(available) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, canonical)
	}

	return nil, fmt.Errorf("%w: %q (available: %s)", ErrUnknownTool, canonical, strings.Join(available, ", "))
}

func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := toolNamesLocked(r.tools)
	list := make([]Tool, 0, len(names))
	for _, name := range names {
		list = append(list, r.tools[name])
	}

	return list
}

func (r *ToolRegistry) ToToolDefinitions() []provider.ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := toolNamesLocked(r.tools)
	definitions := make([]provider.ToolDefinition, 0, len(names))

	for _, name := range names {
		tool := r.tools[name]
		definitions = append(definitions, provider.ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: inputSchemaForTool(tool),
		})
	}

	return definitions
}

func canonicalToolName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func toolNamesLocked(tools map[string]Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func inputSchemaForTool(tool Tool) map[string]any {
	schemaProvider, ok := tool.(inputSchemaProvider)
	if ok {
		schema := schemaProvider.InputSchema()
		if schema != nil {
			return schema
		}
	}

	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": true,
	}
}
