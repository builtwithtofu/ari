package cmd

import (
	"os"
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
		session daemon.WorkspaceGetResponse
		want    bool
	}{
		{
			name: "matches origin root",
			cwd:  origin,
			session: daemon.WorkspaceGetResponse{
				OriginRoot: origin,
				Folders:    []daemon.WorkspaceFolderInfo{{Path: folder}},
			},
			want: true,
		},
		{
			name: "matches registered folder",
			cwd:  folder,
			session: daemon.WorkspaceGetResponse{
				OriginRoot: other,
				Folders:    []daemon.WorkspaceFolderInfo{{Path: folder}},
			},
			want: true,
		},
		{
			name: "rejects unrelated path",
			cwd:  other,
			session: daemon.WorkspaceGetResponse{
				OriginRoot: origin,
				Folders:    []daemon.WorkspaceFolderInfo{{Path: folder}},
			},
			want: false,
		},
		{
			name:    "rejects when roots absent",
			cwd:     origin,
			session: daemon.WorkspaceGetResponse{},
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

func TestWorkspaceMatchesSessionNormalizesSymlinks(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "workspace")
	repo := filepath.Join(origin, "repo-a")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	symlinkRoot := filepath.Join(root, "workspace-link")
	if err := os.Symlink(origin, symlinkRoot); err != nil {
		t.Skipf("os.Symlink unsupported in this environment: %v", err)
	}

	matched, err := workspaceMatchesSession(filepath.Join(symlinkRoot, "repo-a"), daemon.WorkspaceGetResponse{OriginRoot: origin})
	if err != nil {
		t.Fatalf("workspaceMatchesSession returned error: %v", err)
	}
	if !matched {
		t.Fatal("workspaceMatchesSession = false, want true for symlink-equivalent path")
	}
}

func TestWorkspaceMatchesSessionAcceptsRelativeSessionPaths(t *testing.T) {
	originAbs := t.TempDir()
	repoAbs := filepath.Join(originAbs, "repo-a")
	if err := os.MkdirAll(repoAbs, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd returned error: %v", err)
	}
	originRel, err := filepath.Rel(wd, originAbs)
	if err != nil {
		t.Skipf("filepath.Rel unsupported in this environment: %v", err)
	}

	matched, err := workspaceMatchesSession(repoAbs, daemon.WorkspaceGetResponse{OriginRoot: originRel})
	if err != nil {
		t.Fatalf("workspaceMatchesSession returned error: %v", err)
	}
	if !matched {
		t.Fatal("workspaceMatchesSession = false, want true for relative-equivalent session root")
	}
}

func TestWorkspaceMatchesSessionIgnoresBlankFolderEntries(t *testing.T) {
	origin := t.TempDir()
	repo := filepath.Join(origin, "repo-a")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	matched, err := workspaceMatchesSession(repo, daemon.WorkspaceGetResponse{
		Folders: []daemon.WorkspaceFolderInfo{
			{Path: ""},
			{Path: "   "},
			{Path: repo},
		},
	})
	if err != nil {
		t.Fatalf("workspaceMatchesSession returned error: %v", err)
	}
	if !matched {
		t.Fatal("workspaceMatchesSession = false, want true when one non-blank folder matches")
	}
}

func TestResolveWorkspaceByCWDSelectsMostSpecificMatch(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "projects")
	child := filepath.Join(parent, "app")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	workspaceID, err := resolveWorkspaceByCWD(child, []daemon.WorkspaceGetResponse{
		{WorkspaceID: "ws-parent", Name: "clay", OriginRoot: parent},
		{WorkspaceID: "ws-child", Name: "clay", OriginRoot: child},
	})
	if err != nil {
		t.Fatalf("resolveWorkspaceByCWD returned error: %v", err)
	}
	if workspaceID != "ws-child" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-child")
	}
}

func TestResolveWorkspaceByCWDHandlesBasenameCollisionsByPath(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "src", "clay")
	right := filepath.Join(root, "work", "clay")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatalf("os.MkdirAll left returned error: %v", err)
	}
	if err := os.MkdirAll(right, 0o755); err != nil {
		t.Fatalf("os.MkdirAll right returned error: %v", err)
	}

	workspaceID, err := resolveWorkspaceByCWD(right, []daemon.WorkspaceGetResponse{
		{WorkspaceID: "ws-left", Name: "clay", OriginRoot: left},
		{WorkspaceID: "ws-right", Name: "clay", OriginRoot: right},
	})
	if err != nil {
		t.Fatalf("resolveWorkspaceByCWD returned error: %v", err)
	}
	if workspaceID != "ws-right" {
		t.Fatalf("workspaceID = %q, want %q", workspaceID, "ws-right")
	}
}

func TestResolveWorkspaceByCWDErrorsWhenAmbiguous(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "repo")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("os.MkdirAll returned error: %v", err)
	}

	_, err := resolveWorkspaceByCWD(cwd, []daemon.WorkspaceGetResponse{
		{WorkspaceID: "ws-a", Name: "alpha", OriginRoot: cwd},
		{WorkspaceID: "ws-b", Name: "beta", OriginRoot: cwd},
	})
	if err == nil {
		t.Fatal("resolveWorkspaceByCWD returned nil error")
	}
	if err.Error() != "current directory matches multiple workspaces; use --workspace <id-or-name>" {
		t.Fatalf("resolveWorkspaceByCWD error = %q, want %q", err.Error(), "current directory matches multiple workspaces; use --workspace <id-or-name>")
	}
}
