package learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/mauroociappina/ayrton/internal/engram"
)

// Agent is the Learning Agent that persists patterns cross-session
type Agent struct {
	client *engram.Client
	scope  string
}

// Pattern represents a learned pattern
type Pattern struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	Context     string    `json:"context"`
	Outcome     string    `json:"outcome,omitempty"`
	Confidence  float64   `json:"confidence"`
	UsageCount  int       `json:"usage_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// NewAgent creates a new Learning Agent
func NewAgent(scope string) (*Agent, error) {
	client, err := engram.NewClient()
	if err != nil {
		return nil, fmt.Errorf("create engram client: %w", err)
	}
	return &Agent{client: client, scope: scope}, nil
}

// Close closes the agent
func (a *Agent) Close() error {
	return a.client.Close()
}

// Learn stores a new pattern or updates existing one
func (a *Agent) Learn(ctx context.Context, pattern *Pattern) error {
	// Compute unique key from content fields only (for upsert dedup)
	// Use \x00 separator so field boundaries don't collide with field content
	contentKey := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%f",
		pattern.Description, pattern.Category,
		pattern.Context, pattern.Outcome,
		pattern.Confidence)
	hash := sha256.Sum256([]byte(contentKey))
	patternID := hex.EncodeToString(hash[:8])

	// Hash-based deterministic ID
	pattern.ID = patternID

	// Increment UsageCount if this pattern already exists
	topicKey := fmt.Sprintf("learning/patterns/%s/%s/%s", pattern.Category, a.scope, patternID)
	existing, err := a.client.ListByTopic(ctx, topicKey, a.scope)
	if err != nil {
		log.Printf("warning: failed to check existing pattern (will save as new): %v", err)
	} else {
		for _, e := range existing {
			if p := a.parsePattern(e.Content); p != nil {
				pattern.UsageCount = p.UsageCount + 1
				break
			}
		}
	}

	// Now set timestamps
	now := time.Now()
	if pattern.CreatedAt.IsZero() {
		pattern.CreatedAt = now
	}
	pattern.UpdatedAt = now

	contentBytes, err := json.Marshal(pattern)
	if err != nil {
		return fmt.Errorf("marshal pattern: %w", err)
	}

	obs := &engram.Observation{
		Title:    fmt.Sprintf("Pattern: %s", pattern.Description),
		Type:     "learning-pattern",
		Scope:    a.scope,
		TopicKey: topicKey,
		Content:  string(contentBytes),
	}

	_, err = a.client.SaveOrUpdate(ctx, obs)
	return err
}

// Recall searches for relevant patterns
func (a *Agent) Recall(ctx context.Context, query string, limit int) ([]Pattern, error) {
	results, err := a.client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}

	var patterns []Pattern
	for _, r := range results {
		p := a.parsePattern(r.Content)
		if p != nil {
			p.ID = fmt.Sprintf("pattern-%d", r.ID)
			patterns = append(patterns, *p)
		}
	}
	return patterns, nil
}

// RecallByCategory retrieves patterns by category
func (a *Agent) RecallByCategory(ctx context.Context, category string, limit int) ([]Pattern, error) {
	results, err := a.client.ListByTopic(ctx, fmt.Sprintf("learning/patterns/%s", category), a.scope)
	if err != nil {
		return nil, err
	}

	var patterns []Pattern
	for _, r := range results {
		if len(patterns) >= limit {
			break
		}
		p := a.parsePattern(r.Content)
		if p != nil {
			p.ID = fmt.Sprintf("pattern-%d", r.ID)
			patterns = append(patterns, *p)
		}
	}
	return patterns, nil
}

// GetRecentPatterns retrieves recently learned patterns
func (a *Agent) GetRecentPatterns(ctx context.Context, limit int) ([]Pattern, error) {
	// Over-fetch generously since ListRecent queries all types and we filter
	// by "learning-pattern" type in Go. Without over-fetch, we could return
	// fewer results than requested if non-pattern observations push out real
	// patterns from the SQL LIMIT window.
	fetchLimit := limit * 10
	if fetchLimit < 100 {
		fetchLimit = 100
	}
	results, err := a.client.ListRecent(ctx, a.scope, fetchLimit)
	if err != nil {
		return nil, err
	}

	var patterns []Pattern
	for _, r := range results {
		if r.Type != "learning-pattern" {
			continue
		}
		p := a.parsePattern(r.Content)
		if p != nil {
			p.ID = fmt.Sprintf("pattern-%d", r.ID)
			patterns = append(patterns, *p)
			if len(patterns) >= limit {
				break
			}
		}
	}
	return patterns, nil
}

// parsePattern extracts pattern from observation content (JSON format)
func (a *Agent) parsePattern(content string) *Pattern {
	var p Pattern
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return nil
	}
	if p.Description == "" {
		return nil
	}
	return &p
}

// LearnFromSDD saves SDD decision/pattern automatically
func (a *Agent) LearnFromSDD(ctx context.Context, phase, decision, rationale, files string) error {
	pattern := &Pattern{
		Description: fmt.Sprintf("SDD %s: %s", phase, decision),
		Category:    "sdd-decision",
		Context:     fmt.Sprintf("Phase: %s\nRationale: %s\nFiles: %s", phase, rationale, files),
		Outcome:     "decision recorded",
		Confidence:  0.9,
		UsageCount:  1,
	}
	return a.Learn(ctx, pattern)
}

// LearnFromError saves error pattern for future reference
func (a *Agent) LearnFromError(ctx context.Context, errorMsg, solution, context string) error {
	pattern := &Pattern{
		Description: fmt.Sprintf("Error: %s", errorMsg),
		Category:    "error-resolution",
		Context:     fmt.Sprintf("Context: %s\nSolution: %s", context, solution),
		Outcome:     "resolved",
		Confidence:  0.85,
		UsageCount:  1,
	}
	return a.Learn(ctx, pattern)
}
