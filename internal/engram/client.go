package engram

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Client provides access to Engram memory store
type Client struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
}

// Observation represents a memory observation
type Observation struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Type        string    `json:"type"`
	Scope       string    `json:"scope"`
	Project     string    `json:"project,omitempty"`
	TopicKey    string    `json:"topic_key,omitempty"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SearchResult represents a search result
type SearchResult struct {
	Observation
	Rank float64 `json:"rank"`
}

// SearchOptions options for search
type SearchOptions struct {
	Type    string
	Project string
	Scope   string
	Limit   int
}

// NewClient creates a new Engram client
func NewClient() (*Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	dbPath := filepath.Join(home, ".ayrton", "engram.db")
	return NewClientWithPath(dbPath)
}

// NewClientWithPath creates a new Engram client at the given database path.
// Useful for testing with temporary directories.
func NewClientWithPath(dbPath string) (*Client, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	c := &Client{db: db, dbPath: dbPath}
	if err := c.initSchema(); err != nil {
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return c, nil
}

// initSchema initializes the database schema with FTS5
func (c *Client) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS observations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		type TEXT NOT NULL,
		scope TEXT NOT NULL DEFAULT 'project',
		project TEXT NOT NULL DEFAULT '',
		topic_key TEXT,
		content TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(topic_key, scope)
	);

	CREATE INDEX IF NOT EXISTS idx_observations_topic ON observations(topic_key);
	CREATE INDEX IF NOT EXISTS idx_observations_type ON observations(type);
	CREATE INDEX IF NOT EXISTS idx_observations_scope ON observations(scope);
	CREATE INDEX IF NOT EXISTS idx_observations_project ON observations(project);
	CREATE INDEX IF NOT EXISTS idx_observations_created ON observations(created_at);

	-- FTS5 virtual table for full-text search
	CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts USING fts5(
		title, content, type, scope, project, topic_key,
		content='observations', content_rowid='id'
	);

	-- Triggers to keep FTS in sync
	CREATE TRIGGER IF NOT EXISTS observations_ai AFTER INSERT ON observations BEGIN
		INSERT INTO observations_fts(rowid, title, content, type, scope, project, topic_key)
		VALUES (new.id, new.title, new.content, new.type, new.scope, new.project, new.topic_key);
	END;

	CREATE TRIGGER IF NOT EXISTS observations_ad AFTER DELETE ON observations BEGIN
		INSERT INTO observations_fts(observations_fts, rowid) VALUES ('delete', old.id);
	END;

	CREATE TRIGGER IF NOT EXISTS observations_au AFTER UPDATE ON observations BEGIN
		INSERT INTO observations_fts(observations_fts, rowid, title, content, type, scope, project, topic_key)
		VALUES ('delete', old.id, old.title, old.content, old.type, old.scope, old.project, old.topic_key);
		INSERT INTO observations_fts(rowid, title, content, type, scope, project, topic_key)
		VALUES (new.id, new.title, new.content, new.type, new.scope, new.project, new.topic_key);
	END;
	`

	_, err := c.db.Exec(schema)
	return err
}

// Save saves an observation
func (c *Client) Save(ctx context.Context, obs *Observation) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked(ctx, obs)
}

// saveLocked saves an observation while already holding c.mu.Lock.
// Extracted to avoid deadlock when called from SaveOrUpdate.
func (c *Client) saveLocked(ctx context.Context, obs *Observation) (int64, error) {
	now := time.Now()
	obs.CreatedAt = now
	obs.UpdatedAt = now

	// Use NULL for empty topic_key to allow multiple rows without topic_key under UNIQUE constraint
	topicKey := nullIfEmpty(obs.TopicKey)

	result, err := c.db.ExecContext(ctx, `
		INSERT INTO observations (title, type, scope, project, topic_key, content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, obs.Title, obs.Type, obs.Scope, obs.Project, topicKey, obs.Content, obs.CreatedAt, obs.UpdatedAt)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	obs.ID = id
	return id, nil
}

// Update updates an existing observation
func (c *Client) Update(ctx context.Context, obs *Observation) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	obs.UpdatedAt = time.Now()
	_, err := c.db.ExecContext(ctx, `
		UPDATE observations SET title=?, type=?, scope=?, project=?, topic_key=?, content=?, updated_at=?
		WHERE id=?
	`, obs.Title, obs.Type, obs.Scope, obs.Project, nullIfEmpty(obs.TopicKey), obs.Content, obs.UpdatedAt, obs.ID)
	return err
}

// SaveOrUpdate saves or updates an observation by topic_key (upsert) - atomic
func (c *Client) SaveOrUpdate(ctx context.Context, obs *Observation) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if obs.TopicKey == "" {
		return c.saveLocked(ctx, obs)
	}

	now := time.Now()
	obs.UpdatedAt = now

	result, err := c.db.ExecContext(ctx, `
		INSERT INTO observations (title, type, scope, project, topic_key, content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(topic_key, scope) DO UPDATE SET
			title=excluded.title,
			type=excluded.type,
			project=excluded.project,
			content=excluded.content,
			updated_at=excluded.updated_at
	`, obs.Title, obs.Type, obs.Scope, obs.Project, obs.TopicKey, obs.Content, now, now)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if id == 0 {
		// On conflict, LastInsertId returns 0 in SQLite; fetch the actual ID
		err = c.db.QueryRowContext(ctx, `
			SELECT id FROM observations WHERE topic_key=? AND scope=?
		`, obs.TopicKey, obs.Scope).Scan(&id)
		if err != nil {
			return 0, err
		}
	}
	obs.ID = id
	return id, nil
}

// nullIfEmpty returns NULL for empty string, otherwise the string value
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Get retrieves an observation by ID
func (c *Client) Get(ctx context.Context, id int64) (*Observation, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	obs := &Observation{}
	var topicKey sql.NullString
	err := c.db.QueryRowContext(ctx, `
		SELECT id, title, type, scope, project, topic_key, content, created_at, updated_at
		FROM observations WHERE id=?
	`, id).Scan(&obs.ID, &obs.Title, &obs.Type, &obs.Scope, &obs.Project, &topicKey, &obs.Content, &obs.CreatedAt, &obs.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if topicKey.Valid {
		obs.TopicKey = topicKey.String
	}
	return obs, err
}

// Search performs full-text search using FTS5
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return c.SearchWithOptions(ctx, query, SearchOptions{Limit: limit})
}

// SearchWithOptions performs full-text search with options
func (c *Client) SearchWithOptions(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Sanitize query for FTS5 - escape special characters
	query = sanitizeFTS5Query(query)

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	whereClause := "WHERE observations_fts MATCH ?"
	args := []interface{}{query}

	if opts.Type != "" {
		whereClause += " AND o.type = ?"
		args = append(args, opts.Type)
	}

	if opts.Scope != "" {
		whereClause += " AND o.scope = ?"
		args = append(args, opts.Scope)
	}

	if opts.Project != "" {
		whereClause += " AND o.project = ?"
		args = append(args, opts.Project)
	}

	args = append(args, limit)

	rows, err := c.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT o.id, o.title, o.type, o.scope, o.project, o.topic_key, o.content, o.created_at, o.updated_at,
		       bm25(observations_fts) as rank
		FROM observations_fts
		JOIN observations o ON o.id = observations_fts.rowid
		%s
		ORDER BY rank
		LIMIT ?
	`, whereClause), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var topicKeyNull sql.NullString
		err := rows.Scan(&r.ID, &r.Title, &r.Type, &r.Scope, &r.Project, &topicKeyNull, &r.Content, &r.CreatedAt, &r.UpdatedAt, &r.Rank)
		if err != nil {
			return nil, err
		}
		if topicKeyNull.Valid {
			r.TopicKey = topicKeyNull.String
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ListByTopic retrieves observations by topic_key
func (c *Client) ListByTopic(ctx context.Context, topicKey, scope string) ([]Observation, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rows, err := c.db.QueryContext(ctx, `
		SELECT id, title, type, scope, project, topic_key, content, created_at, updated_at
		FROM observations WHERE topic_key LIKE ? AND scope=?
		ORDER BY created_at DESC
	`, topicKey+"%", scope)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []Observation
	for rows.Next() {
		var o Observation
		var topicKeyNull sql.NullString
		err := rows.Scan(&o.ID, &o.Title, &o.Type, &o.Scope, &o.Project, &topicKeyNull, &o.Content, &o.CreatedAt, &o.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if topicKeyNull.Valid {
			o.TopicKey = topicKeyNull.String
		}
		results = append(results, o)
	}
	return results, rows.Err()
}

// ListRecent retrieves recent observations
func (c *Client) ListRecent(ctx context.Context, scope string, limit int) ([]Observation, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rows, err := c.db.QueryContext(ctx, `
		SELECT id, title, type, scope, project, topic_key, content, created_at, updated_at
		FROM observations WHERE scope=?
		ORDER BY created_at DESC
		LIMIT ?
	`, scope, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []Observation
	for rows.Next() {
		var o Observation
		var topicKeyNull sql.NullString
		err := rows.Scan(&o.ID, &o.Title, &o.Type, &o.Scope, &o.Project, &topicKeyNull, &o.Content, &o.CreatedAt, &o.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if topicKeyNull.Valid {
			o.TopicKey = topicKeyNull.String
		}
		results = append(results, o)
	}
	return results, rows.Err()
}

// Close closes the database connection
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.db.Close()
}

// sanitizeFTS5Query sanitizes user input for FTS5 MATCH queries
func sanitizeFTS5Query(query string) string {
	// Escape double quotes by doubling them (FTS5 standard)
	result := strings.ReplaceAll(query, `"`, `""`)
	// Wrap in quotes for phrase search if multiple terms
	if strings.Contains(strings.TrimSpace(result), " ") {
		return `"` + result + `"`
	}
	return result
}

// ToJSON serializes observation to JSON
func (o *Observation) ToJSON() (string, error) {
	b, err := json.MarshalIndent(o, "", "  ")
	return string(b), err
}

// SaveLesson guarda una lección aprendida
func (c *Client) SaveLesson(ctx context.Context, trigger, action, outcome string, ctxMap map[string]interface{}, agents []string, confidence float64) (int64, error) {
	content := fmt.Sprintf(`**Trigger:** %s
**Action:** %s
**Outcome:** %s
**Context:** %v
**Agents:** %v
**Confidence:** %.2f
**Timestamp:** %s`, trigger, action, outcome, ctxMap, agents, confidence, time.Now().Format(time.RFC3339))

	return c.SaveOrUpdate(ctx, &Observation{
		Title:    fmt.Sprintf("Lesson: %s → %s", trigger, outcome),
		Type:     "learning",
		Scope:    "project",
		TopicKey: "patterns/" + slug(trigger),
		Content:  content,
	})
}

// SavePattern guarda/actualiza un patrón
func (c *Client) SavePattern(ctx context.Context, trigger string, pattern *Pattern) error {
	_, err := c.SaveOrUpdate(ctx, &Observation{
		Title:    fmt.Sprintf("Pattern: %s", trigger),
		Type:     "pattern",
		Scope:    "project",
		TopicKey: "patterns/" + slug(trigger),
		Content:  pattern.ToMarkdown(),
	})
	return err
}

// GetPatterns busca patrones
func (c *Client) GetPatterns(ctx context.Context, trigger string, limit int) ([]SearchResult, error) {
	return c.SearchWithOptions(ctx, trigger, SearchOptions{
		Type:    "pattern",
		Project: "project",
		Scope:   "project",
		Limit:   limit,
	})
}

// GetLessons busca lecciones
func (c *Client) GetLessons(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return c.SearchWithOptions(ctx, query, SearchOptions{
		Type:    "learning",
		Project: "project",
		Scope:   "project",
		Limit:   limit,
	})
}

func slug(s string) string {
	// Generate deterministic slug from SHA256 hash (truncated to 16 chars)
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// Pattern estructura para patrones aprendidos
type Pattern struct {
	ID          string
	Trigger     string
	Action      string
	Outcome     string
	Frequency   int
	Confidence  float64
	LastSeen    time.Time
	Agents      []string
	ContextKeys []string
}

func (p *Pattern) ToMarkdown() string {
	return fmt.Sprintf(`**Trigger:** %s
**Action:** %s
**Outcome:** %s
**Frequency:** %d
**Confidence:** %.2f
**Last Seen:** %s
**Agents:** %v
**Context Keys:** %v`, p.Trigger, p.Action, p.Outcome, p.Frequency, p.Confidence, p.LastSeen.Format(time.RFC3339), p.Agents, p.ContextKeys)
}