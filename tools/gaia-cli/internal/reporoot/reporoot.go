package reporoot

import (
	"errors"
	"os"
	"path/filepath"
)

func Resolve(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}

	if env := os.Getenv("GAIA_REPO_ROOT"); env != "" {
		return filepath.Abs(env)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	current := cwd
	for {
		if hasMarker(current, ".jj") || hasMarker(current, ".git") {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return "", errors.New("could not resolve repository root")
}

func hasMarker(root, marker string) bool {
	info, err := os.Stat(filepath.Join(root, marker))
	if err != nil {
		return false
	}

	return info.IsDir()
}
