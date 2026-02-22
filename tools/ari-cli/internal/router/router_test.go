package router

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/config"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{}
	r := New(cfg)

	if r == nil {
		t.Fatal("New() = nil, want non-nil router")
	}
	if r.config != cfg {
		t.Fatal("New() router config pointer mismatch")
	}
}

func TestSelectModel(t *testing.T) {
	tests := []struct {
		name     string
		taskType TaskType
		want     string
	}{
		{name: "quick_fix uses edits model", taskType: TaskQuickFix, want: "model-edits"},
		{name: "review uses review model", taskType: TaskReview, want: "model-review"},
		{name: "debug uses default model", taskType: TaskDebug, want: "model-default"},
		{name: "docs uses edits model", taskType: TaskDocs, want: "model-edits"},
		{name: "refactor falls back to default", taskType: TaskRefactor, want: "model-default"},
		{name: "feature falls back to default", taskType: TaskFeature, want: "model-default"},
		{name: "explore_deep falls back to default", taskType: TaskType("explore_deep"), want: "model-default"},
		{name: "router_task falls back to default", taskType: TaskType("router_task"), want: "model-default"},
		{name: "invalid task type falls back to default", taskType: TaskType("invalid_task"), want: "model-default"},
	}

	r := New(&config.Config{
		Models: config.ModelConfig{
			Default: "model-default",
			Edits:   "model-edits",
			Review:  "model-review",
		},
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.SelectModel(context.Background(), Task{Type: tt.taskType})
			if err != nil {
				t.Fatalf("SelectModel() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("SelectModel() model = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSelectModelNilConfig(t *testing.T) {
	r := New(nil)

	_, err := r.SelectModel(context.Background(), Task{Type: TaskQuickFix})
	if err == nil {
		t.Fatal("SelectModel() error = nil, want non-nil")
	}
	if err.Error() != "router config is nil" {
		t.Fatalf("SelectModel() error = %q, want %q", err.Error(), "router config is nil")
	}
}

func TestSelectModelEmptyConfig(t *testing.T) {
	r := New(&config.Config{})

	got, err := r.SelectModel(context.Background(), Task{Type: TaskQuickFix})
	if err != nil {
		t.Fatalf("SelectModel() error = %v", err)
	}
	if got != "" {
		t.Fatalf("SelectModel() model = %q, want empty model for empty config", got)
	}
}

func TestGetModelForTaskType(t *testing.T) {
	r := New(&config.Config{
		Models: config.ModelConfig{
			Default: "model-default",
			Edits:   "model-edits",
			Review:  "model-review",
		},
	})

	tests := []struct {
		name     string
		taskType TaskType
		want     string
	}{
		{name: "quick fix returns edits model", taskType: TaskQuickFix, want: "model-edits"},
		{name: "review returns review model", taskType: TaskReview, want: "model-review"},
		{name: "explore_deep returns default model", taskType: TaskType("explore_deep"), want: "model-default"},
		{name: "router_task returns default model", taskType: TaskType("router_task"), want: "model-default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.GetModelForTaskType(tt.taskType)
			if got != tt.want {
				t.Fatalf("GetModelForTaskType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewHealthTracker(t *testing.T) {
	h := NewHealthTracker(nil)
	if h == nil {
		t.Fatal("NewHealthTracker() = nil, want non-nil")
	}
	if h.store != nil {
		t.Fatal("NewHealthTracker(nil) store = non-nil, want nil")
	}
}

func TestHealthTrackerMethods(t *testing.T) {
	h := NewHealthTracker(nil)

	if err := h.RecordSuccess(context.Background(), "model-a"); err != nil {
		t.Fatalf("RecordSuccess() error = %v", err)
	}

	if err := h.RecordFailure(context.Background(), "model-a"); err != nil {
		t.Fatalf("RecordFailure() error = %v", err)
	}

	healthy, err := h.IsHealthy(context.Background(), "model-a")
	if err != nil {
		t.Fatalf("IsHealthy() error = %v", err)
	}
	if !healthy {
		t.Fatal("IsHealthy() = false, want true")
	}
}

func TestLoadRouterConfigDefaults(t *testing.T) {
	homeDir := t.TempDir()
	workingDir := t.TempDir()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("Chdir(cleanup) error = %v", chdirErr)
		}
	})

	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Setenv("HOME", homeDir)

	got, err := LoadRouterConfig()
	if err != nil {
		t.Fatalf("LoadRouterConfig() error = %v", err)
	}
	if got == nil || got.Config == nil {
		t.Fatal("LoadRouterConfig() returned nil config")
	}
	if got.Strategy != got.Config.Router.Strategy {
		t.Fatalf("LoadRouterConfig() strategy = %q, want %q", got.Strategy, got.Config.Router.Strategy)
	}
	if got.Strategy == "" {
		t.Fatal("LoadRouterConfig() strategy is empty")
	}
}

func TestLoadRouterConfigMalformedConfigFile(t *testing.T) {
	homeDir := t.TempDir()
	workingDir := t.TempDir()

	err := os.WriteFile(filepath.Join(workingDir, "config.json"), []byte("{"), 0644)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(oldWD); chdirErr != nil {
			t.Fatalf("Chdir(cleanup) error = %v", chdirErr)
		}
	})

	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Setenv("HOME", homeDir)

	_, err = LoadRouterConfig()
	if err == nil {
		t.Fatal("LoadRouterConfig() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Fatalf("LoadRouterConfig() error = %q, want message containing %q", err.Error(), "read config")
	}
}
