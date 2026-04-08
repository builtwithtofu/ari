package cmd

import "strings"

func resolveWorkspaceReference(overrideWorkspace string, readActive func() (string, error)) (string, error) {
	if readActive == nil {
		panic("resolveWorkspaceReference: readActive must not be nil")
	}
	if strings.TrimSpace(overrideWorkspace) != "" {
		return strings.TrimSpace(overrideWorkspace), nil
	}
	activeWorkspace, err := readActive()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(activeWorkspace) == "" {
		return "", userFacingError{message: "No active workspace is set"}
	}
	return strings.TrimSpace(activeWorkspace), nil
}
