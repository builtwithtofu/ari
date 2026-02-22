package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/headless"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/vcs"
	"github.com/builtwithtofu/ari/tools/ari-cli/internal/world"
	"github.com/spf13/cobra"
)

func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize a new Ari world",
		Long:  "Initialize a new Ari world in the specified directory (default: current directory).",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if isHeadless(cmd) {
				return headless.HeadlessUnsupportedError("init")
			}

			path := "."
			if len(args) > 0 {
				path = args[0]
			}

			absPath, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("resolve path: %w", err)
			}

			vcsBackend, err := vcs.Detect(absPath)
			if err != nil {
				return fmt.Errorf("detect vcs: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Detected VCS: %s\n", vcsBackend.Name())

			ariDir := filepath.Join(absPath, ".ari")
			if err := os.MkdirAll(ariDir, 0o755); err != nil {
				return fmt.Errorf("create .ari directory: %w", err)
			}

			worldPath := filepath.Join(ariDir, "world.db")
			db, err := world.Initialize(worldPath)
			if err != nil {
				return fmt.Errorf("initialize world: %w", err)
			}
			_ = db.Close()

			manifest := map[string]any{
				"name":       filepath.Base(absPath),
				"vcs":        vcsBackend.Name(),
				"created_at": time.Now().UTC().Format(time.RFC3339),
				"version":    "0.1",
			}

			manifestPath := filepath.Join(ariDir, "world.json")
			manifestData, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal manifest: %w", err)
			}
			if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
				return fmt.Errorf("write manifest: %w", err)
			}

			for _, dir := range []string{"decisions", "plans", "sessions"} {
				if err := os.MkdirAll(filepath.Join(ariDir, dir), 0o755); err != nil {
					return fmt.Errorf("create %s directory: %w", dir, err)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Initialized Ari world in %s\n", ariDir)
			fmt.Fprintf(cmd.OutOrStdout(), "  VCS: %s\n", vcsBackend.Name())
			fmt.Fprintf(cmd.OutOrStdout(), "  World: %s\n", worldPath)
			fmt.Fprintf(cmd.OutOrStdout(), "  Manifest: %s\n", manifestPath)

			return nil
		},
	}
}
