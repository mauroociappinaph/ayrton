package engram

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// NewTestClient creates a new Engram client with a temporary database for testing.
func NewTestClient(t testing.TB) (*Client, error) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	return newTestClientWithPath(t, dbPath)
}

// NewTestClientWithPath creates a new Engram client with a specific database path for testing.
func NewTestClientWithPath(t testing.TB, dbPath string) (*Client, error) {
	t.Helper()
	return newTestClientWithPath(t, dbPath)
}

func newTestClientWithPath(t testing.TB, dbPath string) (*Client, error) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	c := &Client{db: db, dbPath: dbPath}
	if err := c.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	t.Cleanup(func() { _ = c.Close() })
	return c, nil
}
