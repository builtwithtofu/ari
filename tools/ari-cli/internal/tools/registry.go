package tools

import (
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/provider"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

func (r *Registry) Register(tool Tool) error {
	if tool == nil {
		return ErrToolNil
	}

	canonical := canonicalToolName(tool.Name())
	if canonical == "" {
		return ErrToolNameRequired
	}

	if _, exists := r.tools[canonical]; exists {
		return fmt.Errorf("%w: %q", ErrToolAlreadyDefined, canonical)
	}

	r.tools[canonical] = tool
	return nil
}

func (r *Registry) Get(name string) (Tool, error) {
	canonical := canonicalToolName(name)
	if canonical == "" {
		return nil, ErrToolNameRequired
	}

	tool, ok := r.tools[canonical]
	if ok {
		return tool, nil
	}

	available := toolNamesLocked(r.tools)
	if len(available) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTool, canonical)
	}

	return nil, fmt.Errorf("%w: %q (available: %s)", ErrUnknownTool, canonical, strings.Join(available, ", "))
}

func (r *Registry) List() []Tool {
	names := toolNamesLocked(r.tools)
	list := make([]Tool, 0, len(names))

	for _, name := range names {
		list = append(list, r.tools[name])
	}

	return list
}

func (r *Registry) ToToolDefinitions() []provider.ToolDefinition {
	tools := r.List()
	definitions := make([]provider.ToolDefinition, 0, len(tools))

	for _, tool := range tools {
		definitions = append(definitions, toolToDefinition(tool))
	}

	return definitions
}

func DefaultRegistry() *Registry {
	registry := NewRegistry()

	mustRegister(registry, ReadFileTool{})
	mustRegister(registry, WriteFileTool{})
	mustRegister(registry, RunCommandTool{})
	mustRegister(registry, AskUserTool{})

	return registry
}

func mustRegister(registry *Registry, tool Tool) {
	err := registry.Register(tool)
	if err != nil {
		panic(fmt.Sprintf("failed to register tool %q: %v", tool.Name(), err))
	}
}

func toolToDefinition(tool Tool) provider.ToolDefinition {
	return provider.ToolDefinition{
		Name:        tool.Name(),
		Description: tool.Description(),
		InputSchema: inputSchemaForTool(tool),
	}
}
