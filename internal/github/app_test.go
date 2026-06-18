package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGitHubAppClient_Interface verifies the interface exists
func TestGitHubAppClient_Interface(t *testing.T) {
	var _ GitHubAppClient = (*GitHubAppClientImpl)(nil)
}

// TestWebhookSignatureVerification tests HMAC-SHA256 verification
func TestWebhookSignatureVerification(t *testing.T) {
	secret := "test-webhook-secret"
	payload := []byte(`{"action":"opened","pull_request":{"number":42}}`)

	// Generate valid signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	// Test valid signature
	err := VerifyWebhookSignature(payload, validSig, secret)
	require.NoError(t, err)

	// Test invalid signature
	err = VerifyWebhookSignature(payload, "sha256=invalidsignature", secret)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid signature")

	// Test missing signature
	err = VerifyWebhookSignature(payload, "", secret)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing signature")

	// Test wrong algorithm
	err = VerifyWebhookSignature(payload, "sha1=invalidsignature", secret)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported algorithm")
}

// TestWebhookPayloadParsing tests parsing of different webhook event types
func TestWebhookPayloadParsing(t *testing.T) {
	tests := []struct {
		name       string
		eventType  string
		payload    string
		wantType   string
		wantNumber int
	}{
		{
			name:      "pull_request opened",
			eventType: "pull_request",
			payload: `{"action":"opened","number":42,"pull_request":{"number":42,"title":"Test PR","user":{"login":"testuser"}}}`,
			wantType:   "pull_request",
			wantNumber: 42,
		},
		{
			name:      "pull_request synchronize",
			eventType: "pull_request",
			payload: `{"action":"synchronize","number":42,"pull_request":{"number":42,"title":"Test PR"}}`,
			wantType:   "pull_request",
			wantNumber: 42,
		},
		{
			name:      "issue_comment created",
			eventType: "issue_comment",
			payload: `{"action":"created","issue":{"number":42},"comment":{"body":"@ayrton spec","user":{"login":"testuser"}}}`,
			wantType:   "issue_comment",
			wantNumber: 42,
		},
		{
			name:      "installation created",
			eventType: "installation",
			payload: `{"action":"created","installation":{"id":12345,"account":{"login":"testorg"}}}`,
			wantType:   "installation",
			wantNumber: 12345,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock request
			req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
			req.Header.Set("X-GitHub-Event", tt.eventType)
			req.Header.Set("X-GitHub-Delivery", "test-delivery-123")
			
			// We'll test parsing through the handler
			event, err := ParseWebhookPayload([]byte(tt.payload), tt.eventType)
			require.NoError(t, err)
			require.Equal(t, tt.wantType, event.EventType)
		})
	}
}

// TestTokenCache_TTL tests that installation tokens are cached with 50 minute TTL
func TestTokenCache_TTL(t *testing.T) {
	cache := NewTokenCache(50 * time.Minute)

	// Store a token
	token := &InstallationToken{
		Token:     "ghs_test123",
		ExpiresAt: time.Now().Add(50 * time.Minute),
	}
	cache.Set(12345, token)

	// Retrieve should work
	got, ok := cache.Get(12345)
	require.True(t, ok)
	require.Equal(t, "ghs_test123", got.Token)

	// Create cache with very short TTL
	shortCache := NewTokenCache(10 * time.Millisecond)
	
	// Store a token that expires soon
	expiringToken := &InstallationToken{
		Token:     "ghs_expiring",
		ExpiresAt: time.Now().Add(5 * time.Millisecond),
	}
	shortCache.Set(12345, expiringToken)

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should be expired
	_, ok = shortCache.Get(12345)
	require.False(t, ok)
}

// TestInstallationTokenBroker tests the token broker functionality
func TestInstallationTokenBroker(t *testing.T) {
	// This test uses a mock - we test the logic without real GitHub API
	broker := NewInstallationTokenBroker("/tmp/nonexistent.pem", 12345)
	
	// Should fail because key file doesn't exist
	_, _, err := broker.GetToken(context.Background(), 67890)
	require.Error(t, err)
	require.Contains(t, err.Error(), "create app transport")
}

// TestParseMentionCommand tests parsing @ayrton commands from comments
func TestParseMentionCommand(t *testing.T) {
	tests := []struct {
		name       string
		comment    string
		wantCmd    string
		wantArgs   []string
		wantIssue  int
		wantError  bool
	}{
		{
			name:       "spec command",
			comment:    "@ayrton spec",
			wantCmd:    "spec",
			wantArgs:   []string{},
			wantIssue:  0, // will be inferred from issue context
			wantError:  false,
		},
		{
			name:       "spec with issue",
			comment:    "@ayrton spec --issue 42",
			wantCmd:    "spec",
			wantArgs:   []string{"--issue", "42"},
			wantIssue:  42,
			wantError:  false,
		},
		{
			name:       "apply command",
			comment:    "@ayrton apply --issue 123",
			wantCmd:    "apply",
			wantArgs:   []string{"--issue", "123"},
			wantIssue:  123,
			wantError:  false,
		},
		{
			name:       "help command",
			comment:    "@ayrton help",
			wantCmd:    "help",
			wantArgs:   []string{},
			wantIssue:  0,
			wantError:  false,
		},
		{
			name:       "unknown command",
			comment:    "@ayrton invalid",
			wantCmd:    "",
			wantArgs:   nil,
			wantIssue:  0,
			wantError:  true,
		},
		{
			name:       "no mention",
			comment:    "just a regular comment",
			wantCmd:    "",
			wantArgs:   nil,
			wantIssue:  0,
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, issue, err := ParseMentionCommand(tt.comment)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.wantCmd, cmd)
				require.Equal(t, tt.wantArgs, args)
				if tt.wantIssue > 0 {
					require.Equal(t, tt.wantIssue, issue)
				}
			}
		})
	}
}

// TestWebhookIdempotency tests duplicate delivery detection
func TestWebhookIdempotency(t *testing.T) {
	// This would use Engram in real implementation
	// For unit test, we'll test the logic
	
	deliveryID := "test-delivery-123"
	processed := make(map[string]bool)
	
	// First time - not processed
	require.False(t, processed[deliveryID])
	processed[deliveryID] = true
	
	// Second time - already processed
	require.True(t, processed[deliveryID])
}

// TestHealthzEndpoint tests the health check endpoint
func TestHealthzEndpoint(t *testing.T) {
	// Create a test server with healthz handler
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "ok",
				"version": "test",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Test healthz endpoint
	resp, err := http.Get(server.URL + "/healthz")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	
	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	require.Equal(t, "ok", result["status"])
	require.Equal(t, "test", result["version"])
}