package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

type Workspace struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Updated time.Time `json:"updated"`
}

func ListWorkspaces(repoRoot string) ([]Workspace, error) {
	root := filepath.Join(repoRoot, ".sandbox", "workspaces")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []Workspace{}, nil
		}

		return nil, err
	}

	workspaces := make([]Workspace, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		path := filepath.Join(root, entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		workspaces = append(workspaces, Workspace{
			Name:    entry.Name(),
			Path:    path,
			Updated: info.ModTime(),
		})
	}

	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].Updated.Equal(workspaces[j].Updated) {
			return workspaces[i].Name > workspaces[j].Name
		}

		return workspaces[i].Updated.After(workspaces[j].Updated)
	})

	return workspaces, nil
}

func RunHarness(repoRoot string, args []string) error {
	allArgs := append([]string{"run", "--cwd", "tools/opencode-gaia-harness", "cli"}, args...)

	cmd := exec.Command("bun", allArgs...)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}
