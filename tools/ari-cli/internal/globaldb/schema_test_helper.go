package globaldb

import (
	"testing"

	"github.com/builtwithtofu/ari/tools/ari-cli/internal/testutil/dbtest"
)

func newGlobalDBTestStore(t *testing.T, prefix string) *Store {
	t.Helper()
	db := dbtest.NewDB(t, prefix)
	store, err := NewSQLStore(db)
	if err != nil {
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	return store
}
