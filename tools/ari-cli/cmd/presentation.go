package cmd

import (
	"strings"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func presentationLabel(p daemon.Presentation, fallback string) string {
	if strings.TrimSpace(p.Label) != "" {
		return strings.TrimSpace(p.Label)
	}
	if strings.TrimSpace(p.Summary) != "" {
		return strings.TrimSpace(p.Summary)
	}
	return strings.TrimSpace(fallback)
}

func presentationStatusLabel(p daemon.Presentation, fallback string) string {
	if strings.TrimSpace(p.StatusLabel) != "" {
		return strings.TrimSpace(p.StatusLabel)
	}
	return strings.TrimSpace(fallback)
}

func presentationSummary(p daemon.Presentation, fallback string) string {
	if strings.TrimSpace(p.Summary) != "" {
		return strings.TrimSpace(p.Summary)
	}
	return presentationLabel(p, fallback)
}

func presentationSourceField(p daemon.Presentation, name string) string {
	if p.Source == nil || p.Source.Fields == nil {
		return ""
	}
	return strings.TrimSpace(p.Source.Fields[name])
}

func nativeSessionField(p daemon.Presentation, name, fallback string) string {
	if value := presentationSourceField(p, name); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}
