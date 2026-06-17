package engram

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Client provides access to Engram memory store
type Client struct {
	mu              sync.RWMutex
	db              *sql.DB
	dbPath          string
	needsFTSRebuild bool // set during migration, triggers FTS content sync after Phase 3
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
	// Phase 1: Create base table (IF NOT EXISTS skips if already present)
	if _, err := c.db.Exec(`
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
	`); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	// v1.0 databases had no UNIQUE constraint so duplicates could accumulate.
	// The migration preserves the newest row per (topic_key, scope) when adding the constraint.
	//
	// Phase 2: Migration for pre-v1.1 databases.
	//
	// Old databases may be missing:
	//   a) the `project` column (added to CREATE TABLE after initial release)
	//   b) the UNIQUE(topic_key, scope) constraint (SQLite can't ALTER to add one)
	//
	// Detect case (b) by checking for the auto-generated unique index name.
	// If missing, recreate the table preserving all existing data.
	var hasUnique bool
	err := c.db.QueryRow(
		`SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='index' AND tbl_name='observations' AND name LIKE 'sqlite_autoindex_observations_%'`,
	).Scan(&hasUnique)
	if err == nil && !hasUnique {
		if _, err := c.db.Exec(`
			DROP TRIGGER IF EXISTS observations_ai;
			DROP TRIGGER IF EXISTS observations_ad;
			DROP TRIGGER IF EXISTS observations_au;
			DROP TABLE IF EXISTS observations_fts;
			ALTER TABLE observations RENAME TO observations_old;
		`); err != nil {
			return fmt.Errorf("migrate: rename old table: %w", err)
		}

		var duplicateCount int64
		if err := c.db.QueryRow(`SELECT COUNT(*) - COUNT(DISTINCT topic_key || '|' || scope) FROM observations_old`).Scan(&duplicateCount); err != nil {
			return fmt.Errorf("migrate: count duplicates: %w", err)
		}
		if duplicateCount > 0 {
			log.Printf("warning: migration: deduplicating %d old observations (keeping newest per topic_key+scope)", duplicateCount)
		}

		if _, err := c.db.Exec(`
			CREATE TABLE observations (
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

			INSERT INTO observations(id, title, type, scope, project, topic_key, content, created_at, updated_at)
			SELECT id, title, type, scope, '', topic_key, content, created_at, updated_at
			FROM observations_old
			WHERE id IN (SELECT MAX(id) FROM observations_old GROUP BY topic_key, scope);

			DROP TABLE observations_old;
		`); err != nil {
			return fmt.Errorf("migrate: recreate table: %w", err)
		}

		// Schedule FTS rebuild to backfill the FTS index for migrated rows
		// (the triggers weren't alive during the INSERT INTO above).
		c.needsFTSRebuild = true
	}

	// After the (b) migration, the table definitely has the project column.
	// Migration (a): add project column if still missing (catches edge cases).
	if _, err := c.db.Exec("ALTER TABLE observations ADD COLUMN project TEXT NOT NULL DEFAULT ''"); err != nil {
		// "duplicate column" is expected when the column already exists;
		// any other error indicates a real problem.
		if !strings.Contains(err.Error(), "duplicate column") {
			log.Printf("warning: add project column (may already exist): %v", err)
		}
	}

	// Phase 3: Indexes, FTS5, and triggers (all use IF NOT EXISTS)
	if _, err := c.db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_observations_topic ON observations(topic_key);
		CREATE INDEX IF NOT EXISTS idx_observations_type ON observations(type);
		CREATE INDEX IF NOT EXISTS idx_observations_scope ON observations(scope);
		CREATE INDEX IF NOT EXISTS idx_observations_project ON observations(project);
		CREATE INDEX IF NOT EXISTS idx_observations_created ON observations(created_at);

		CREATE VIRTUAL TABLE IF NOT EXISTS observations_fts USING fts5(
			title, content, type, scope, project, topic_key,
			content='observations', content_rowid='id'
		);

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
	`); err != nil {
		return fmt.Errorf("create indexes and FTS: %w", err)
	}

	// Rebuild FTS index if data was migrated before triggers existed (Phase 2 migration).
	if c.needsFTSRebuild {
		if _, err := c.db.Exec("INSERT INTO observations_fts(observations_fts) VALUES('rebuild')"); err != nil {
			log.Printf("warning: FTS rebuild: %v", err)
		}
	}

	return nil
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

	// Escape LIKE wildcards so `_` and `%` in topicKey are literal
	escaped := strings.NewReplacer("_", `\_`, "%", `\%`).Replace(topicKey)

	rows, err := c.db.QueryContext(ctx, `
		SELECT id, title, type, scope, project, topic_key, content, created_at, updated_at
		FROM observations WHERE topic_key LIKE ? ESCAPE '\' AND scope=?
		ORDER BY created_at DESC
	`, escaped+"%", scope)
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

	// Wrap in quotes for phrase search if multiple terms — this makes all
	// FTS5 operators (*, -, +, NEAR/, etc.) literal inside the phrase.
	if strings.Contains(strings.TrimSpace(result), " ") {
		return `"` + result + `"`
	}

	// For single-term queries: strip `*` that appears mid-word.
	// Trailing `*` is a valid FTS5 prefix operator (e.g. "test*") and is kept.
	if strings.Contains(result, "*") {
		var safe strings.Builder
		runes := []rune(strings.TrimSpace(result))
		for i, r := range runes {
			if r == '*' && i < len(runes)-1 {
				continue // strip mid-word wildcard
			}
			safe.WriteRune(r)
		}
		return safe.String()
	}

	return strings.TrimSpace(result)
}

// ToJSON serializes observation to JSON
func (o *Observation) ToJSON() (string, error) {
	b, err := json.MarshalIndent(o, "", "  ")
	return string(b), err
}

