package globaldb

import (
	"context"
	"fmt"
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

type Projection interface {
	Name() string
	EventTypes() []string
	ProjectWorkspaceEvent(context.Context, *dbsqlc.Queries, WorkspaceEvent) error
}

type PrefixProjection interface {
	Projection
	EventTypePrefixes() []string
}

type RebuildableProjection interface {
	Projection
	Rebuild(context.Context, *Store, string) error
}

type ProjectionRegistry struct {
	ordered  []Projection
	byType   map[string][]Projection
	prefixes []projectionPrefixRegistration
}

type projectionPrefixRegistration struct {
	prefix     string
	projection Projection
}

func NewProjectionRegistry() *ProjectionRegistry {
	return &ProjectionRegistry{byType: map[string][]Projection{}}
}

func DefaultProjectionRegistry() *ProjectionRegistry {
	registry := NewProjectionRegistry()
	mustRegisterProjection(registry, FanoutProjection{})
	mustRegisterProjection(registry, InboxProjection{})
	mustRegisterProjection(registry, TimelineProjection{})
	return registry
}

func mustRegisterProjection(registry *ProjectionRegistry, projection Projection) {
	if err := registry.Register(projection); err != nil {
		panic(err)
	}
}

func (r *ProjectionRegistry) Register(projection Projection) error {
	if r == nil {
		return fmt.Errorf("%w: projection registry is required", ErrInvalidInput)
	}
	if projection == nil {
		return fmt.Errorf("%w: projection is required", ErrInvalidInput)
	}
	name := strings.TrimSpace(projection.Name())
	if name == "" {
		return fmt.Errorf("%w: projection name is required", ErrInvalidInput)
	}
	for _, existing := range r.ordered {
		if strings.TrimSpace(existing.Name()) == name {
			return fmt.Errorf("%w: projection %q already registered", ErrInvalidInput, name)
		}
	}
	r.ordered = append(r.ordered, projection)
	for _, eventType := range projection.EventTypes() {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		r.byType[eventType] = append(r.byType[eventType], projection)
	}
	if prefixProjection, ok := projection.(PrefixProjection); ok {
		for _, prefix := range prefixProjection.EventTypePrefixes() {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			r.prefixes = append(r.prefixes, projectionPrefixRegistration{prefix: prefix, projection: projection})
		}
	}
	return nil
}

func (r *ProjectionRegistry) ProjectionsForEvent(event WorkspaceEvent) []Projection {
	if r == nil {
		return nil
	}
	eventType := strings.TrimSpace(event.EventType)
	if eventType == "" {
		return nil
	}
	projections := append([]Projection(nil), r.byType[eventType]...)
	for _, registered := range r.prefixes {
		if strings.HasPrefix(eventType, registered.prefix) && !projectionListContains(projections, registered.projection) {
			projections = append(projections, registered.projection)
		}
	}
	for _, projection := range r.byType[""] {
		if !projectionListContains(projections, projection) {
			projections = append(projections, projection)
		}
	}
	return projections
}

func projectionListContains(projections []Projection, candidate Projection) bool {
	if candidate == nil {
		return false
	}
	candidateName := strings.TrimSpace(candidate.Name())
	if candidateName == "" {
		return false
	}
	for _, projection := range projections {
		if projection != nil && strings.TrimSpace(projection.Name()) == candidateName {
			return true
		}
	}
	return false
}

func (r *ProjectionRegistry) All() []Projection {
	if r == nil {
		return nil
	}
	return append([]Projection(nil), r.ordered...)
}

func (r *ProjectionRegistry) Rebuildable() []RebuildableProjection {
	if r == nil {
		return nil
	}
	out := make([]RebuildableProjection, 0, len(r.ordered))
	for _, projection := range r.ordered {
		if rebuildable, ok := projection.(RebuildableProjection); ok {
			out = append(out, rebuildable)
		}
	}
	return out
}

type projectionContextKey struct{}

type transactionalProjectionFunc struct {
	name string
	fn   func(context.Context, *dbsqlc.Queries, WorkspaceEvent) error
}

func projectionRegistryFromContext(ctx context.Context) *ProjectionRegistry {
	if registry, ok := ctx.Value(projectionContextKey{}).(*ProjectionRegistry); ok {
		return registry
	}
	return DefaultProjectionRegistry()
}

func transactionalProjection(name string, fn func(context.Context, *dbsqlc.Queries, WorkspaceEvent) error) Projection {
	return transactionalProjectionFunc{name: name, fn: fn}
}

func (p transactionalProjectionFunc) Name() string { return p.name }

func (p transactionalProjectionFunc) EventTypes() []string { return nil }

func (p transactionalProjectionFunc) ProjectWorkspaceEvent(ctx context.Context, queries *dbsqlc.Queries, event WorkspaceEvent) error {
	if p.fn == nil {
		return nil
	}
	return p.fn(ctx, queries, event)
}
