package learning

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mauroociappinaph/ayrton/internal/engram"
)

func newTestAgent(t testing.TB, scope string) (*Agent, error) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "engram.db")
	client, err := engram.NewClientWithPath(dbPath)
	if err != nil {
		return nil, fmt.Errorf("create engram client: %w", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return &Agent{client: client, scope: scope}, nil
}

func TestAgent_LearnAndRecall(t *testing.T) {
	agent, err := newTestAgent(t, "test")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ctx := context.Background()

	pattern := &Pattern{
		Description: "Test pattern for unit test",
		Category:    "test",
		Context:     "Unit test context",
		Outcome:     "Success",
		Confidence:  0.9,
		UsageCount:  1,
	}

	if err := agent.Learn(ctx, pattern); err != nil {
		t.Fatalf("learn: %v", err)
	}

	patterns, err := agent.Recall(ctx, "unit test", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("expected at least 1 pattern")
	}
	if patterns[0].Description != pattern.Description {
		t.Errorf("description mismatch: %q", patterns[0].Description)
	}
}

func TestAgent_RecallByCategory(t *testing.T) {
	agent, err := newTestAgent(t, "test")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ctx := context.Background()

	// Learn patterns with different descriptions but same category
	// Note: current implementation uses same topic_key per category, so only last one persists
	// This test verifies the RecallByCategory function works
	pattern := &Pattern{
		Description: "Category pattern unique",
		Category:    "test-category",
		Context:     "Test context",
		Confidence:  0.8,
	}
	if err := agent.Learn(ctx, pattern); err != nil {
		t.Fatalf("learn: %v", err)
	}

	patterns, err := agent.RecallByCategory(ctx, "test-category", 10)
	if err != nil {
		t.Fatalf("recall by category: %v", err)
	}
	if len(patterns) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(patterns))
	}
}

func TestAgent_GetRecentPatterns(t *testing.T) {
	agent, err := newTestAgent(t, "test")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ctx := context.Background()

	// Use different categories to avoid upsert collisions
	for i := 0; i < 5; i++ {
		pattern := &Pattern{
			Description: "Recent pattern " + string(rune('0'+i)),
			Category:    "recent-" + string(rune('0'+i)),
			Confidence:  0.7,
		}
		if err := agent.Learn(ctx, pattern); err != nil {
			t.Fatalf("learn: %v", err)
		}
	}

	patterns, err := agent.GetRecentPatterns(ctx, 3)
	if err != nil {
		t.Fatalf("get recent: %v", err)
	}
	if len(patterns) != 3 {
		t.Errorf("expected 3 recent patterns, got %d", len(patterns))
	}
}

func TestAgent_LearnFromSDD(t *testing.T) {
	agent, err := newTestAgent(t, "test")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ctx := context.Background()

	if err := agent.LearnFromSDD(ctx, "spec", "Use FTS5 for search", "Better performance", "internal/engram/"); err != nil {
		t.Fatalf("learn from SDD: %v", err)
	}

	patterns, err := agent.RecallByCategory(ctx, "sdd-decision", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("expected SDD decision pattern")
	}
	if patterns[0].Category != "sdd-decision" {
		t.Errorf("wrong category: %s", patterns[0].Category)
	}
}

func TestAgent_LearnFromError(t *testing.T) {
	agent, err := newTestAgent(t, "test")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ctx := context.Background()

	if err := agent.LearnFromError(ctx, "connection refused", "Check if service is running", "Database connection"); err != nil {
		t.Fatalf("learn from error: %v", err)
	}

	patterns, err := agent.RecallByCategory(ctx, "error-resolution", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("expected error resolution pattern")
	}
	if patterns[0].Category != "error-resolution" {
		t.Errorf("wrong category: %s", patterns[0].Category)
	}
}

func TestPattern_Persistence(t *testing.T) {
	// Use a shared temporary directory for both agents to share the database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	client1, err := engram.NewClientWithPath(dbPath)
	if err != nil {
		t.Fatalf("create client1: %v", err)
	}
	t.Cleanup(func() { _ = client1.Close() })
	agent1 := &Agent{client: client1, scope: "persistence-test"}

	ctx := context.Background()
	pattern := &Pattern{
		Description: "Persistent pattern",
		Category:    "persist",
		Context:     "Test cross-session",
		Confidence:  0.95,
	}
	if err := agent1.Learn(ctx, pattern); err != nil {
		t.Fatalf("learn: %v", err)
	}

	client2, err := engram.NewClientWithPath(dbPath)
	if err != nil {
		t.Fatalf("create client2: %v", err)
	}
	t.Cleanup(func() { _ = client2.Close() })
	agent2 := &Agent{client: client2, scope: "persistence-test"}

	patterns, err := agent2.Recall(ctx, "persistent", 10)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("pattern not persisted across sessions")
	}
	if patterns[0].Description != "Persistent pattern" {
		t.Errorf("wrong pattern: %q", patterns[0].Description)
	}
}

func TestAgent_ConcurrentAccess(t *testing.T) {
	agent, err := newTestAgent(t, "concurrent")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ctx := context.Background()

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			p := &Pattern{
				Description: "Concurrent pattern " + string(rune('0'+idx)),
				Category:    "concurrent",
				Context:     "Concurrent test",
				Confidence:  0.5,
			}
			done <- agent.Learn(ctx, p)
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Logf("concurrent learn error (expected under contention): %v", err)
		}
	}

	patterns, err := agent.RecallByCategory(ctx, "concurrent", 20)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(patterns) < 1 {
		t.Errorf("expected at least 1 pattern, got %d", len(patterns))
	}
}

func TestAgent_Close(t *testing.T) {
	agent, err := newTestAgent(t, "close-test")
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if err := agent.Close(); err != nil {
		t.Errorf("close error: %v", err)
	}

	if err := agent.Close(); err != nil {
		t.Errorf("double close error: %v", err)
	}
}

func TestPattern_Fields(t *testing.T) {
	p := &Pattern{
		Description: "Test",
		Category:    "test",
		Context:     "Context",
		Outcome:     "Outcome",
		Confidence:  0.8,
		UsageCount:  5,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if p.Description != "Test" {
		t.Errorf("description not set")
	}
	if p.Confidence != 0.8 {
		t.Errorf("confidence not set")
	}
	if p.UsageCount != 5 {
		t.Errorf("usage count not set")
	}
}