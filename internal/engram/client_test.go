package engram

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClient_SaveAndGet(t *testing.T) {
	// Create temp db
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	client := &Client{db: db, dbPath: dbPath}
	if err := client.initSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Save observation
	obs := &Observation{
		Title:    "Test Pattern",
		Type:     "test",
		Scope:    "project",
		TopicKey: "test/pattern",
		Content:  "This is a test pattern content",
	}

	id, err := client.Save(ctx, obs)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero ID")
	}

	// Get observation
	got, err := client.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected observation, got nil")
	}
	if got.Title != obs.Title {
		t.Errorf("title mismatch: got %q, want %q", got.Title, obs.Title)
	}
	if got.Content != obs.Content {
		t.Errorf("content mismatch")
	}
}

func TestClient_Search(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	client := &Client{db: db, dbPath: dbPath}
	if err := client.initSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Save multiple observations
	patterns := []Observation{
		{Title: "Go FTS5 pattern", Type: "learning", Scope: "project", TopicKey: "test/a", Content: "Use FTS5 for full text search in Go"},
		{Title: "SQLite pattern", Type: "learning", Scope: "project", TopicKey: "test/b", Content: "SQLite with modernc.org/sqlite driver"},
		{Title: "Error handling", Type: "error", Scope: "project", TopicKey: "test/c", Content: "Handle errors with proper wrapping"},
	}

	for _, p := range patterns {
		if _, err := client.Save(ctx, &p); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	// Search for FTS5
	results, err := client.Search(ctx, "FTS5", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	// Search for pattern
	results, err = client.Search(ctx, "pattern", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("expected at least 2 results, got %d", len(results))
	}
}

func TestClient_SaveOrUpdate_Upsert(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	client := &Client{db: db, dbPath: dbPath}
	if err := client.initSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// First save
	obs1 := &Observation{
		Title:    "Original",
		Type:     "test",
		Scope:    "project",
		TopicKey: "test/upsert",
		Content:  "Original content",
	}
	id1, err := client.SaveOrUpdate(ctx, obs1)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Update with same topic_key
	obs2 := &Observation{
		Title:    "Updated",
		Type:     "test",
		Scope:    "project",
		TopicKey: "test/upsert",
		Content:  "Updated content",
	}
	id2, err := client.SaveOrUpdate(ctx, obs2)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	// Should be same ID (upsert)
	if id1 != id2 {
		t.Errorf("expected same ID after upsert: %d vs %d", id1, id2)
	}

	// Verify content updated
	got, err := client.Get(ctx, id1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Updated" {
		t.Errorf("title not updated: %q", got.Title)
	}
	if got.Content != "Updated content" {
		t.Errorf("content not updated: %q", got.Content)
	}
}

func TestClient_ListByTopic(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	client := &Client{db: db, dbPath: dbPath}
	if err := client.initSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	// Save multiple with DIFFERENT topic_keys (unique constraint on topic_key+scope)
	for i := 0; i < 3; i++ {
		obs := &Observation{
			Title:    "Pattern",
			Type:     "learning",
			Scope:    "project",
			TopicKey: fmt.Sprintf("learning/patterns/test/%d", i),
			Content:  "Content",
		}
		if _, err := client.Save(ctx, obs); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	// ListByTopic with non-existent topic should return empty
	results, err := client.ListByTopic(ctx, "learning/patterns/nonexistent", "project")
	if err != nil {
		t.Fatalf("list by topic: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent topic, got %d", len(results))
	}

	// ListByTopic with one of the topic_keys
	results, err = client.ListByTopic(ctx, "learning/patterns/test/1", "project")
	if err != nil {
		t.Fatalf("list by topic: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestClient_ListRecent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := openTestDB(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	client := &Client{db: db, dbPath: dbPath}
	if err := client.initSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		obs := &Observation{
			Title:   "Recent",
			Type:    "learning",
			Scope:   "project",
			Content: "Content",
		}
		if _, err := client.Save(ctx, obs); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	results, err := client.ListRecent(ctx, "project", 3)
	if err != nil {
		t.Fatalf("list recent: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestSanitizeFTS5Query(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"hello world", "\"hello world\""},
		{"test-query", "test-query"},
		{"test+query", "test+query"},
		{"test*query", "testquery"},
		{"test*", "test*"},
		{"*test", "test"},
		{"test(query)", "test(query)"},
		{"test:query", "test:query"},
		{"a  b   c", "\"a  b   c\""},
		{"quote\"test", "quote\"\"test"},
	}

	for _, tc := range tests {
		result := sanitizeFTS5Query(tc.input)
		if result != tc.expected {
			t.Errorf("sanitize(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func openTestDB(path string) (*sql.DB, error) {
	return sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
}

func TestObservation_ToJSON(t *testing.T) {
	obs := &Observation{
		ID:        1,
		Title:     "Test",
		Type:      "learning",
		Scope:     "project",
		TopicKey:  "test/key",
		Content:   "Content",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	jsonStr, err := obs.ToJSON()
	if err != nil {
		t.Fatalf("to json: %v", err)
	}
	if jsonStr == "" {
		t.Fatal("expected non-empty JSON")
	}
}

func TestMain(m *testing.M) {
	// Ensure temp dir exists
	_ = os.MkdirAll("/tmp/engram_test", 0755)
	os.Exit(m.Run())
}