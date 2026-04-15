package daemon

import (
	"encoding/json"
	"strings"
)

// HarnessProjection is Ari's canonical projected harness identity shape.
type HarnessProjection struct {
	Harness     *string
	ResumableID *string
	Metadata    string
}

// HarnessProjector projects Ari-owned runtime context into harness identity metadata.
type HarnessProjector interface {
	Project(harness string, args []string) HarnessProjection
}

type harnessProjectorFunc func(harness string, args []string) HarnessProjection

func (f harnessProjectorFunc) Project(harness string, args []string) HarnessProjection {
	return f(harness, args)
}

type defaultHarnessProjector struct{}

func (defaultHarnessProjector) Project(harness string, args []string) HarnessProjection {
	projection := HarnessProjection{Metadata: "{}"}
	harness = strings.TrimSpace(harness)
	if harness != "" {
		projection.Harness = &harness
	}

	resumableID := parseHarnessResumableID(harness, args)
	if resumableID == "" {
		return projection
	}

	projection.ResumableID = &resumableID
	metadata := map[string]string{"resume_source": "argv"}
	if encoded, err := json.Marshal(metadata); err == nil {
		projection.Metadata = string(encoded)
	}

	return projection
}
