package cmd

import (
	"path/filepath"
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/daemon"
)

func TestWorkspaceMatchesSession(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "workspace")
	folder := filepath.Join(origin, "repo-a")
	other := filepath.Join(root, "other")

	tests := []struct {
		name    string
		cwd     string
		session daemon.SessionGetResponse
		want    bool
	}{
		{
			name: "matches origin root",
			cwd:  origin,
			session: daemon.SessionGetResponse{
				OriginRoot: origin,
				Folders:    []daemon.SessionFolderInfo{{Path: folder}},
			},
			want: true,
		},
		{
			name: "matches registered folder",
			cwd:  folder,
			session: daemon.SessionGetResponse{
				OriginRoot: other,
				Folders:    []daemon.SessionFolderInfo{{Path: folder}},
			},
			want: true,
		},
		{
			name: "rejects unrelated path",
			cwd:  other,
			session: daemon.SessionGetResponse{
				OriginRoot: origin,
				Folders:    []daemon.SessionFolderInfo{{Path: folder}},
			},
			want: false,
		},
		{
			name:    "rejects when roots absent",
			cwd:     origin,
			session: daemon.SessionGetResponse{},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workspaceMatchesSession(tc.cwd, tc.session)
			if err != nil {
				t.Fatalf("workspaceMatchesSession returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("workspaceMatchesSession = %v, want %v", got, tc.want)
			}
		})
	}
}
