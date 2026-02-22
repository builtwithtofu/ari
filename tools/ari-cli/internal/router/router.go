package router

import (
	"context"
	"fmt"

	"github.com/aledsdavies/ari/tools/ari-cli/internal/config"
)

// TaskType represents the type of task being routed
type TaskType string

const (
	TaskQuickFix TaskType = "quick_fix"
	TaskRefactor TaskType = "refactor"
	TaskFeature  TaskType = "feature"
	TaskReview   TaskType = "review"
	TaskDebug    TaskType = "debug"
	TaskDocs     TaskType = "docs"
)

// Task represents a task to be routed to a model
type Task struct {
	Type      TaskType
	FileCount int
	LineCount int
	Prompt    string
}

// Router selects models for tasks based on configuration
type Router struct {
	config *config.Config
}

// New creates a new Router instance
func New(cfg *config.Config) *Router {
	return &Router{
		config: cfg,
	}
}

// SelectModel chooses an appropriate model for the given task
func (r *Router) SelectModel(ctx context.Context, task Task) (string, error) {
	if r.config == nil {
		return "", fmt.Errorf("router config is nil")
	}

	// Simple v0 routing: map task type to model preference
	switch task.Type {
	case TaskQuickFix:
		return r.config.Models.Edits, nil
	case TaskReview:
		return r.config.Models.Review, nil
	case TaskDebug:
		return r.config.Models.Default, nil
	case TaskDocs:
		return r.config.Models.Edits, nil
	default:
		// Default to the main model for everything else
		return r.config.Models.Default, nil
	}
}

// GetModelForTaskType returns the model for a specific task type
// This is useful for testing and debugging
func (r *Router) GetModelForTaskType(taskType TaskType) string {
	switch taskType {
	case TaskQuickFix:
		return r.config.Models.Edits
	case TaskReview:
		return r.config.Models.Review
	default:
		return r.config.Models.Default
	}
}
