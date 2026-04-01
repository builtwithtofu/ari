package vcs

// noneBackend implements VCSBackend as a fallback when no VCS is detected.
type noneBackend struct {
	root string
}

// Name returns "none".
func (n *noneBackend) Name() string {
	return "none"
}

// IsAvailable always returns false.
func (n *noneBackend) IsAvailable() bool {
	return false
}

// CurrentBranch returns ErrNotSupported.
func (n *noneBackend) CurrentBranch() (string, error) {
	return "", ErrNotSupported
}

// RecentCommits returns ErrNotSupported.
func (n *noneBackend) RecentCommits(count int) ([]Commit, error) {
	return nil, ErrNotSupported
}

// ChangedFiles returns ErrNotSupported.
func (n *noneBackend) ChangedFiles() ([]string, error) {
	return nil, ErrNotSupported
}

// CreateCommit returns ErrNotSupported.
func (n *noneBackend) CreateCommit(message string) error {
	return ErrNotSupported
}

// CreateBranch returns ErrNotSupported.
func (n *noneBackend) CreateBranch(name string) error {
	return ErrNotSupported
}

// SetupAriIgnore returns ErrNotSupported for no VCS.
func (n *noneBackend) SetupAriIgnore(ariDir string) error {
	return ErrNotSupported
}
