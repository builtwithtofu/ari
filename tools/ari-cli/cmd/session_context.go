package cmd

import "strings"

func resolveWorkspaceSessionReference(overrideSession string, readActive func() (string, error)) (string, error) {
	if readActive == nil {
		panic("resolveWorkspaceSessionReference: readActive must not be nil")
	}
	if strings.TrimSpace(overrideSession) != "" {
		return strings.TrimSpace(overrideSession), nil
	}
	activeSession, err := readActive()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(activeSession) == "" {
		return "", userFacingError{message: "No active workspace session is set"}
	}
	return strings.TrimSpace(activeSession), nil
}
