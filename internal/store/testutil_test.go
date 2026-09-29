package store

import (
	"database/sql"
	"testing"

	"github.com/qiansi/app/internal/testdb"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return testdb.New(t)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return New(newTestDB(t))
}
