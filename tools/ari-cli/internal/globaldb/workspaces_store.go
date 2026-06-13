package globaldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/globaldb/dbsqlc"
)

type Workspace struct {
	ID            string
	Name          string
	Status        string
	VCSPreference string
	OriginRoot    string
	CleanupPolicy string
	CreatedAt     string
	UpdatedAt     string
}

type WorkspaceFolder struct {
	WorkspaceID string
	FolderPath  string
	VCSType     string
	IsPrimary   bool
	AddedAt     string
}

func workspaceFromSQLC(row dbsqlc.Workspace) Workspace {
	return Workspace{ID: row.WorkspaceID, Name: row.Name, Status: row.Status, VCSPreference: row.VcsPreference, OriginRoot: row.OriginRoot, CleanupPolicy: row.CleanupPolicy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func workspaceFolderFromSQLC(row dbsqlc.WorkspaceFolder) WorkspaceFolder {
	return WorkspaceFolder{WorkspaceID: row.WorkspaceID, FolderPath: row.FolderPath, VCSType: row.VcsType, IsPrimary: row.IsPrimary != 0, AddedAt: row.AddedAt}
}

func (s *Store) CreateWorkspace(ctx context.Context, id, name, originRoot, cleanupPolicy, vcsPreference string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if name = strings.TrimSpace(name); name == "" {
		return fmt.Errorf("%w: workspace name is required", ErrInvalidInput)
	}
	originRoot = strings.TrimSpace(originRoot)
	if err := validateCleanupPolicy(cleanupPolicy); err != nil {
		return err
	}
	if err := validateVCSPreference(vcsPreference); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.sqlcQueries().CreateWorkspace(ctx, dbsqlc.CreateWorkspaceParams{WorkspaceID: id, Name: name, Status: statusActive, VcsPreference: vcsPreference, OriginRoot: originRoot, CleanupPolicy: cleanupPolicy, CreatedAt: now, UpdatedAt: now}); err != nil {
		return fmt.Errorf("create workspace %q: %w", id, err)
	}

	return nil
}

func (s *Store) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	if id = strings.TrimSpace(id); id == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: id})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: workspace id %q", ErrNotFound, id)
		}
		return nil, err
	}
	workspace := workspaceFromSQLC(row)
	return &workspace, nil
}

func (s *Store) GetWorkspaceByName(ctx context.Context, name string) (*Workspace, error) {
	if name = strings.TrimSpace(name); name == "" {
		return nil, fmt.Errorf("%w: workspace name is required", ErrInvalidInput)
	}

	row, err := s.sqlcQueries().GetWorkspaceByName(ctx, dbsqlc.GetWorkspaceByNameParams{Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: workspace name %q", ErrNotFound, name)
		}
		return nil, err
	}
	workspace := workspaceFromSQLC(row)
	return &workspace, nil
}

func (s *Store) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	rows, err := s.sqlcQueries().ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Workspace, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceFromSQLC(row))
	}
	return out, nil
}

func (s *Store) UpdateWorkspaceStatus(ctx context.Context, id, status string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if status = strings.TrimSpace(status); status == "" {
		return fmt.Errorf("%w: workspace status is required", ErrInvalidInput)
	}
	if !isValidWorkspaceStatus(status) {
		return fmt.Errorf("%w: invalid status %q", ErrInvalidInput, status)
	}

	return s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		workspace, err := queries.GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: id})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: workspace id %q", ErrNotFound, id)
			}
			return err
		}
		if !canTransitionWorkspaceStatus(workspace.Status, status) {
			return fmt.Errorf("%w: invalid workspace transition %q -> %q", ErrInvalidInput, workspace.Status, status)
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		rowsAffected, err := queries.UpdateWorkspaceStatus(ctx, dbsqlc.UpdateWorkspaceStatusParams{Status: status, UpdatedAt: now, WorkspaceID: id})
		if err != nil {
			return fmt.Errorf("update workspace status %q: %w", id, err)
		}
		if rowsAffected == 0 {
			return fmt.Errorf("%w: workspace id %q", ErrNotFound, id)
		}
		return nil
	})
}

func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	if id = strings.TrimSpace(id); id == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}

	rowsAffected, err := s.sqlcQueries().DeleteWorkspace(ctx, dbsqlc.DeleteWorkspaceParams{WorkspaceID: id})
	if err != nil {
		return fmt.Errorf("delete workspace %q: %w", id, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: workspace id %q", ErrNotFound, id)
	}

	return nil
}

func (s *Store) AddFolder(ctx context.Context, workspaceID, folderPath, vcsType string, isPrimary bool) error {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if folderPath = strings.TrimSpace(folderPath); folderPath == "" {
		return fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}
	if vcsType = strings.TrimSpace(vcsType); vcsType == "" {
		return fmt.Errorf("%w: vcs type is required", ErrInvalidInput)
	}
	if !isValidVCSType(vcsType) {
		return fmt.Errorf("%w: invalid vcs type %q", ErrInvalidInput, vcsType)
	}

	return s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		return addFolderInTransaction(ctx, queries, workspaceID, folderPath, vcsType, isPrimary)
	})
}

func (s *Store) RemoveFolder(ctx context.Context, workspaceID, folderPath string) error {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}
	if folderPath = strings.TrimSpace(folderPath); folderPath == "" {
		return fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}

	return s.withImmediateQueries(ctx, func(queries *dbsqlc.Queries) error {
		return removeFolderInTransaction(ctx, queries, workspaceID, folderPath)
	})
}

func (s *Store) ListFolders(ctx context.Context, workspaceID string) ([]WorkspaceFolder, error) {
	if workspaceID = strings.TrimSpace(workspaceID); workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidInput)
	}

	if _, err := s.GetWorkspace(ctx, workspaceID); err != nil {
		return nil, err
	}

	rows, err := s.sqlcQueries().ListWorkspaceFolders(ctx, dbsqlc.ListWorkspaceFoldersParams{WorkspaceID: workspaceID})
	if err != nil {
		return nil, err
	}
	out := make([]WorkspaceFolder, 0, len(rows))
	for _, row := range rows {
		out = append(out, workspaceFolderFromSQLC(row))
	}

	return out, nil
}

func addFolderInTransaction(ctx context.Context, queries *dbsqlc.Queries, workspaceID, folderPath, vcsType string, isPrimary bool) error {
	_, err := queries.GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: workspace id %q", ErrNotFound, workspaceID)
		}
		return err
	}
	owners, err := workspaceOwnersByFolderPath(ctx, queries, folderPath)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if owner.WorkspaceID != workspaceID {
			return fmt.Errorf("%w: folder %q already belongs to workspace %q", ErrInvalidInput, folderPath, owner.WorkspaceID)
		}
	}

	primary := 0
	existingFolders, err := queries.ListWorkspaceFolders(ctx, dbsqlc.ListWorkspaceFoldersParams{WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	if isPrimary || len(existingFolders) == 0 {
		primary = 1
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := queries.CreateWorkspaceFolder(ctx, dbsqlc.CreateWorkspaceFolderParams{WorkspaceID: workspaceID, FolderPath: folderPath, VcsType: vcsType, IsPrimary: int64(primary), AddedAt: now}); err != nil {
		return fmt.Errorf("add workspace folder %q: %w", folderPath, err)
	}

	if isPrimary && len(existingFolders) > 0 {
		if err := queries.PromotePrimaryWorkspaceFolder(ctx, dbsqlc.PromotePrimaryWorkspaceFolderParams{FolderPath: folderPath, WorkspaceID: workspaceID}); err != nil {
			return fmt.Errorf("promote workspace primary folder %q: %w", folderPath, err)
		}
	}

	return nil
}

func removeFolderInTransaction(ctx context.Context, queries *dbsqlc.Queries, workspaceID, folderPath string) error {
	rowsAffected, err := queries.DeleteWorkspaceFolder(ctx, dbsqlc.DeleteWorkspaceFolderParams{WorkspaceID: workspaceID, FolderPath: folderPath})
	if err != nil {
		return fmt.Errorf("remove workspace folder %q: %w", folderPath, err)
	}
	if rowsAffected == 0 {
		if _, err := queries.GetWorkspaceByID(ctx, dbsqlc.GetWorkspaceByIDParams{WorkspaceID: workspaceID}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: workspace id %q", ErrNotFound, workspaceID)
			}
			return err
		}

		return fmt.Errorf("%w: folder %q for workspace %q", ErrNotFound, folderPath, workspaceID)
	}

	folders, err := queries.ListWorkspaceFolders(ctx, dbsqlc.ListWorkspaceFoldersParams{WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	hasPrimary := false
	for _, folder := range folders {
		if folder.IsPrimary != 0 {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary && len(folders) > 0 {
		if err := queries.PromotePrimaryWorkspaceFolder(ctx, dbsqlc.PromotePrimaryWorkspaceFolderParams{FolderPath: folders[0].FolderPath, WorkspaceID: workspaceID}); err != nil {
			return fmt.Errorf("promote workspace primary folder %q: %w", folders[0].FolderPath, err)
		}
	}

	return nil
}

type workspaceFolderOwner struct {
	WorkspaceID string
	Status      string
}

func workspaceOwnersByFolderPath(ctx context.Context, queries *dbsqlc.Queries, folderPath string) ([]workspaceFolderOwner, error) {
	folderPath = strings.TrimSpace(folderPath)
	if folderPath == "" {
		return nil, fmt.Errorf("%w: folder path is required", ErrInvalidInput)
	}

	rows, err := queries.ListWorkspaceOwnersByFolderPath(ctx, dbsqlc.ListWorkspaceOwnersByFolderPathParams{FolderPath: folderPath})
	if err != nil {
		return nil, fmt.Errorf("lookup workspaces by folder path %q: %w", folderPath, err)
	}

	owners := make([]workspaceFolderOwner, 0, len(rows))
	for _, row := range rows {
		owner := workspaceFolderOwner{WorkspaceID: row.WorkspaceID, Status: row.Status}
		owner.WorkspaceID = strings.TrimSpace(owner.WorkspaceID)
		owner.Status = strings.TrimSpace(owner.Status)
		if owner.WorkspaceID == "" {
			return nil, fmt.Errorf("%w: folder %q has empty workspace id", ErrInvalidInput, folderPath)
		}
		if owner.Status == "" {
			return nil, fmt.Errorf("%w: folder %q owner %q has empty workspace status", ErrInvalidInput, folderPath, owner.WorkspaceID)
		}
		owners = append(owners, owner)
	}

	return owners, nil
}

func isValidWorkspaceStatus(status string) bool {
	switch status {
	case statusActive, statusSuspended:
		return true
	default:
		return false
	}
}

func canTransitionWorkspaceStatus(from, to string) bool {
	if from == to {
		return true
	}

	switch from {
	case statusActive:
		return to == statusSuspended
	case statusSuspended:
		return to == statusActive
	default:
		return false
	}
}

func validateCleanupPolicy(cleanupPolicy string) error {
	cleanupPolicy = strings.TrimSpace(cleanupPolicy)
	if cleanupPolicy == "" {
		return fmt.Errorf("%w: cleanup policy is required", ErrInvalidInput)
	}

	if cleanupPolicy != cleanupPolicyManual {
		return fmt.Errorf("%w: invalid cleanup policy %q", ErrInvalidInput, cleanupPolicy)
	}

	return nil
}

func validateVCSPreference(vcsPreference string) error {
	vcsPreference = strings.TrimSpace(vcsPreference)
	if vcsPreference == "" {
		return fmt.Errorf("%w: vcs preference is required", ErrInvalidInput)
	}

	if vcsPreference != "auto" && vcsPreference != "jj" && vcsPreference != "git" {
		return fmt.Errorf("%w: invalid vcs preference %q", ErrInvalidInput, vcsPreference)
	}

	return nil
}

func isValidVCSType(vcsType string) bool {
	switch vcsType {
	case vcsTypeGit, vcsTypeJJ, vcsTypeUnknown:
		return true
	default:
		return false
	}
}
