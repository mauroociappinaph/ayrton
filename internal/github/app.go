package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v88/github"
)

// GitHubAppClient defines the interface for GitHub App operations
type GitHubAppClient interface {
	GetInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error)
	VerifyWebhookSignature(payload []byte, signature, secret string) error
	ParseWebhookPayload(payload []byte, eventType string) (*WebhookEvent, error)
	ParseMentionCommand(comment string) (command string, args []string, issueNumber int, err error)
	GetClient(installationID int64) (*github.Client, error)
}

// InstallationToken represents a GitHub installation token with expiration
type InstallationToken struct {
	Token     string
	ExpiresAt time.Time
}

// TokenCache provides thread-safe caching of installation tokens with TTL
type TokenCache struct {
	mu       sync.RWMutex
	cache    map[int64]*InstallationToken
	ttl      time.Duration
}

// NewTokenCache creates a new token cache with the given TTL
func NewTokenCache(ttl time.Duration) *TokenCache {
	return &TokenCache{
		cache: make(map[int64]*InstallationToken),
		ttl:   ttl,
	}
}

// Set stores a token in the cache
func (c *TokenCache) Set(installationID int64, token *InstallationToken) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[installationID] = token
}

// Get retrieves a token from the cache, returns false if expired or not found
func (c *TokenCache) Get(installationID int64) (*InstallationToken, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	token, ok := c.cache[installationID]
	if !ok {
		return nil, false
	}
	
	// Check if token is expired (with 5 min buffer)
	if time.Now().Add(5 * time.Minute).After(token.ExpiresAt) {
		return nil, false
	}
	
	return token, true
}

// InstallationTokenBroker manages installation tokens with caching
type InstallationTokenBroker struct {
	appID         int64
	privateKeyPath string
	tokenCache    *TokenCache
	mu            sync.Mutex
}

// NewInstallationTokenBroker creates a new token broker
func NewInstallationTokenBroker(privateKeyPath string, appID int64) *InstallationTokenBroker {
	return &InstallationTokenBroker{
		appID:          appID,
		privateKeyPath: privateKeyPath,
		tokenCache:     NewTokenCache(50 * time.Minute),
	}
}

// GetToken retrieves an installation token, using cache if available
func (b *InstallationTokenBroker) GetToken(ctx context.Context, installationID int64) (string, time.Time, error) {
	// Check cache first
	if token, ok := b.tokenCache.Get(installationID); ok {
		return token.Token, token.ExpiresAt, nil
	}

	// Create ghinstallation transport
	itr, err := ghinstallation.NewKeyFromFile(
		http.DefaultTransport,
		b.appID,
		installationID,
		b.privateKeyPath,
	)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create app transport: %w", err)
	}

	// Create GitHub client to get token
	client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: itr}))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create github client: %w", err)
	}
	
	// Get installation token
	token, _, err := client.Apps.CreateInstallationToken(ctx, installationID, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create installation token: %w", err)
	}

	// Cache the token
	installToken := &InstallationToken{
		Token:     token.GetToken(),
		ExpiresAt: token.GetExpiresAt().Time,
	}
	b.tokenCache.Set(installationID, installToken)

	return installToken.Token, installToken.ExpiresAt, nil
}

// GitHubAppClientImpl implements GitHubAppClient
type GitHubAppClientImpl struct {
	appID          int64
	privateKeyPath string
	webhookSecret  string
	tokenBroker    *InstallationTokenBroker
	clientCache    map[int64]*github.Client
	clientMu       sync.RWMutex
}

// NewGitHubAppClient creates a new GitHub App client
func NewGitHubAppClient(appID int64, privateKeyPath, webhookSecret string) *GitHubAppClientImpl {
	return &GitHubAppClientImpl{
		appID:          appID,
		privateKeyPath: privateKeyPath,
		webhookSecret:  webhookSecret,
		tokenBroker:    NewInstallationTokenBroker(privateKeyPath, appID),
		clientCache:    make(map[int64]*github.Client),
	}
}

// GetInstallationToken returns a valid installation token
func (c *GitHubAppClientImpl) GetInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error) {
	return c.tokenBroker.GetToken(ctx, installationID)
}

// VerifyWebhookSignature verifies the X-Hub-Signature-256 header
func (c *GitHubAppClientImpl) VerifyWebhookSignature(payload []byte, signature, secret string) error {
	if secret == "" {
		return errors.New("webhook secret not configured")
	}
	return VerifyWebhookSignature(payload, signature, secret)
}

// VerifyWebhookSignature is a standalone function for testing
func VerifyWebhookSignature(payload []byte, signature, secret string) error {
	if signature == "" {
		return errors.New("missing signature")
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return errors.New("unsupported algorithm: only sha256 supported")
	}

	expectedSig := signature[7:] // Remove "sha256=" prefix
	
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSig), []byte(actualSig)) {
		return errors.New("invalid signature")
	}

	return nil
}

// ParseWebhookPayload parses a webhook payload into a WebhookEvent
func (c *GitHubAppClientImpl) ParseWebhookPayload(payload []byte, eventType string) (*WebhookEvent, error) {
	return ParseWebhookPayload(payload, eventType)
}

// ParseWebhookPayload is a standalone function for testing
func ParseWebhookPayload(payload []byte, eventType string) (*WebhookEvent, error) {
	var generic map[string]interface{}
	if err := json.Unmarshal(payload, &generic); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	// Extract delivery ID if present
	deliveryID := ""
	if id, ok := generic["delivery_id"].(string); ok {
		deliveryID = id
	} else if id, ok := generic["X-GitHub-Delivery"].(string); ok {
		deliveryID = id
	}

	// Create payload hash for idempotency
	hash := sha256.Sum256(payload)
	payloadHash := "sha256:" + hex.EncodeToString(hash[:])

	event := &WebhookEvent{
		DeliveryID:  deliveryID,
		EventType:   eventType,
		PayloadHash: payloadHash,
		ProcessedAt: time.Now(),
		Result:      "success",
	}

	return event, nil
}

// ParseMentionCommand parses @ayrton commands from issue comments
func (c *GitHubAppClientImpl) ParseMentionCommand(comment string) (command string, args []string, issueNumber int, err error) {
	return ParseMentionCommand(comment)
}

// ParseMentionCommand is a standalone function for testing
func ParseMentionCommand(comment string) (command string, args []string, issueNumber int, err error) {
	const prefix = "@ayrton"
	
	if !strings.HasPrefix(strings.TrimSpace(comment), prefix) {
		return "", nil, 0, errors.New("not an @ayrton mention")
	}

	// Extract command part after @ayrton
	cmdPart := strings.TrimSpace(comment[len(prefix):])
	if cmdPart == "" {
		return "", nil, 0, errors.New("empty command")
	}

	// Split into parts
	parts := strings.Fields(cmdPart)
	if len(parts) == 0 {
		return "", nil, 0, errors.New("empty command")
	}

	cmd := parts[0]
	validCommands := map[string]bool{
		"propose": true, "spec": true, "design": true,
		"tasks": true, "apply": true, "verify": true, "help": true,
	}
	
	if !validCommands[cmd] {
		return "", nil, 0, fmt.Errorf("unknown command: %s", cmd)
	}

	// Parse --issue flag if present
	issueNum := 0
	args = []string{}
	for i := 1; i < len(parts); i++ {
		if parts[i] == "--issue" && i+1 < len(parts) {
			// Parse the issue number
			fmt.Sscanf(parts[i+1], "%d", &issueNum)
			args = append(args, parts[i], parts[i+1])
			i++
		} else {
			args = append(args, parts[i])
		}
	}

	return cmd, args, issueNum, nil
}

// GetClient returns a GitHub client for the given installation
func (c *GitHubAppClientImpl) GetClient(installationID int64) (*github.Client, error) {
	c.clientMu.RLock()
	if client, ok := c.clientCache[installationID]; ok {
		c.clientMu.RUnlock()
		return client, nil
	}
	c.clientMu.RUnlock()

	c.clientMu.Lock()
	defer c.clientMu.Unlock()

	// Double-check after acquiring write lock
	if client, ok := c.clientCache[installationID]; ok {
		return client, nil
	}

	// Create new client with ghinstallation transport
	itr, err := ghinstallation.NewKeyFromFile(
		http.DefaultTransport,
		c.appID,
		installationID,
		c.privateKeyPath,
	)
	if err != nil {
		return nil, fmt.Errorf("create app transport: %w", err)
	}

	client, err := github.NewClient(github.WithHTTPClient(&http.Client{Transport: itr}))
	if err != nil {
		return nil, fmt.Errorf("create github client: %w", err)
	}
	c.clientCache[installationID] = client

	return client, nil
}

// WebhookHandler handles incoming GitHub webhooks
type WebhookHandler struct {
	client      GitHubAppClient
	engramStore WebhookEventStore
}

// WebhookEventStore interface for storing webhook events (Engram in production)
type WebhookEventStore interface {
	HasDelivery(ctx context.Context, deliveryID string) (bool, error)
	SaveDelivery(ctx context.Context, event *WebhookEvent) error
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(client GitHubAppClient, store WebhookEventStore) *WebhookHandler {
	return &WebhookHandler{
		client:      client,
		engramStore: store,
	}
}

// ServeHTTP handles webhook HTTP requests
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read payload
	payload, err := readBody(r)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Verify signature
	signature := r.Header.Get("X-Hub-Signature-256")
	if err := h.client.VerifyWebhookSignature(payload, signature, ""); err != nil {
		// Note: secret should come from config
		http.Error(w, "Invalid signature verification failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	// Check idempotency
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if deliveryID == "" {
		deliveryID = r.Header.Get("X-GitHub-Delivery")
	}
	
	ctx := r.Context()
	if h.engramStore != nil && deliveryID != "" {
		processed, err := h.engramStore.HasDelivery(ctx, deliveryID)
		if err != nil {
			// Log error but continue
		} else if processed {
			// Duplicate - acknowledge quickly
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	// Parse event
	eventType := r.Header.Get("X-GitHub-Event")
	event, err := h.client.ParseWebhookPayload(payload, eventType)
	if err != nil {
		http.Error(w, "Failed to parse payload", http.StatusBadRequest)
		return
	}

	// Store delivery for idempotency
	if h.engramStore != nil && deliveryID != "" {
		event.DeliveryID = deliveryID
		_ = h.engramStore.SaveDelivery(ctx, event)
	}

	// Acknowledge quickly (async processing would happen here)
	w.WriteHeader(http.StatusOK)
}

// HealthzHandler handles health check requests
func HealthzHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"version": version,
		})
	}
}

func readBody(r *http.Request) ([]byte, error) {
	// Limit body size to 25MB (GitHub max)
	r.Body = http.MaxBytesReader(nil, r.Body, 25<<20)
	defer r.Body.Close()
	
	var buf []byte
	buf, err := io.ReadAll(r.Body)
	return buf, err
}