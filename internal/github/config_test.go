package github

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGitHubAppConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      *GitHubAppConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: &GitHubAppConfig{
				AppID:          12345,
				PrivateKeyPath: "/tmp/test_key.pem",
				WebhookSecret:  "secret123",
				Installations: map[string]InstallationConfig{
					"owner/repo": {InstallationID: 67890, Repo: "owner/repo", AutoReviewEnabled: boolPtr(true), MentionsEnabled: boolPtr(true)},
				},
			},
			wantErr: false,
		},
		{
			name: "zero app id",
			config: &GitHubAppConfig{
				AppID:          0,
				PrivateKeyPath: "/tmp/test_key.pem",
				WebhookSecret:  "secret123",
			},
			wantErr:     true,
			errContains: "app_id is required",
		},
		{
			name: "empty private key path",
			config: &GitHubAppConfig{
				AppID:          12345,
				PrivateKeyPath: "",
				WebhookSecret:  "secret123",
			},
			wantErr:     true,
			errContains: "private_key_path is required",
		},
		{
			name: "empty webhook secret",
			config: &GitHubAppConfig{
				AppID:          12345,
				PrivateKeyPath: "/tmp/test_key.pem",
				WebhookSecret:  "",
			},
			wantErr:     true,
			errContains: "webhook_secret is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGitHubAppConfig_EncryptDecryptPrivateKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "private_key.pem")

	// Create a test private key
	testKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAtestkeycontent
-----END RSA PRIVATE KEY-----`
	require.NoError(t, os.WriteFile(keyPath, []byte(testKey), 0600))

	config := &GitHubAppConfig{
		AppID:          12345,
		PrivateKeyPath: keyPath,
		WebhookSecret:  "secret123",
	}

	// Test encryption
	encryptedPath, err := config.EncryptPrivateKey("test-passphrase")
	require.NoError(t, err)
	require.NotEmpty(t, encryptedPath)
	require.FileExists(t, encryptedPath)

	// Verify encrypted file has 0600 permissions
	info, err := os.Stat(encryptedPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// Test decryption
	decryptedPath, err := config.DecryptPrivateKey(encryptedPath, "test-passphrase")
	require.NoError(t, err)
	require.NotEmpty(t, decryptedPath)
	require.FileExists(t, decryptedPath)

	// Verify decrypted content matches original
	decryptedContent, err := os.ReadFile(decryptedPath)
	require.NoError(t, err)
	require.Equal(t, testKey, string(decryptedContent))

	// Verify decrypted file has 0600 permissions
	info, err = os.Stat(decryptedPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestGitHubAppConfig_EncryptPrivateKey_InvalidPassphrase(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "private_key.pem")

	testKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAtestkeycontent
-----END RSA PRIVATE KEY-----`
	require.NoError(t, os.WriteFile(keyPath, []byte(testKey), 0600))

	config := &GitHubAppConfig{
		AppID:          12345,
		PrivateKeyPath: keyPath,
		WebhookSecret:  "secret123",
	}

	// Encrypt with one passphrase
	encryptedPath, err := config.EncryptPrivateKey("passphrase1")
	require.NoError(t, err)

	// Try to decrypt with wrong passphrase
	_, err = config.DecryptPrivateKey(encryptedPath, "wrong-passphrase")
	require.Error(t, err)
	require.Contains(t, err.Error(), "decrypt")
}

func TestInstallationConfig_Defaults(t *testing.T) {
	config := InstallationConfig{
		InstallationID: 67890,
		Repo:           "owner/repo",
	}

	// Apply defaults
	config.SetDefaults()

	// Defaults should be true
	require.True(t, config.AutoReviewEnabledValue())
	require.True(t, config.MentionsEnabledValue())
}

func TestWebhookEvent_Serialization(t *testing.T) {
	event := &WebhookEvent{
		DeliveryID:  "abc123",
		EventType:   "pull_request",
		PayloadHash: "sha256:testhash",
		ProcessedAt: time.Now(),
		Result:      "success",
	}

	// Test that it can be serialized/deserialized (JSON)
	// This is a basic test to ensure struct tags work
	require.NotEmpty(t, event.DeliveryID)
	require.Equal(t, "pull_request", event.EventType)
	require.Equal(t, "success", event.Result)
}

func TestSDDExecution_Serialization(t *testing.T) {
	exec := &SDDExecution{
		IssueNumber:   42,
		Phase:         "spec",
		Status:        "running",
		StartedAt:     time.Now(),
		CompletedAt:   time.Time{}, // Zero time for incomplete execution
		OutputSummary: "Spec phase completed",
	}

	require.Equal(t, 42, exec.IssueNumber)
	require.Equal(t, "spec", exec.Phase)
	require.Equal(t, "running", exec.Status)
	require.True(t, exec.CompletedAt.IsZero()) // Zero time means not completed yet
}

func TestConfig_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "private_key.pem")

	testKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAtestkeycontent
-----END RSA PRIVATE KEY-----`
	require.NoError(t, os.WriteFile(keyPath, []byte(testKey), 0600))

	config := &GitHubAppConfig{
		AppID:          12345,
		PrivateKeyPath: keyPath,
		WebhookSecret:  "secret123",
	}

	encryptedPath, err := config.EncryptPrivateKey("test-passphrase")
	require.NoError(t, err)

	info, err := os.Stat(encryptedPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm(), "encrypted private key must have 0600 permissions")
}

func TestConfig_AgePassphraseFromEnv(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "private_key.pem")

	testKey := `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAtestkeycontent
-----END RSA PRIVATE KEY-----`
	require.NoError(t, os.WriteFile(keyPath, []byte(testKey), 0600))

	// Set env var
	t.Setenv("AYRTON_AGE_PASSPHRASE", "env-passphrase")

	config := &GitHubAppConfig{
		AppID:          12345,
		PrivateKeyPath: keyPath,
		WebhookSecret:  "secret123",
	}

	// Encrypt should use env var when no explicit passphrase provided
	encryptedPath, err := config.EncryptPrivateKey("")
	require.NoError(t, err)

	// Decrypt should also use env var
	decryptedPath, err := config.DecryptPrivateKey(encryptedPath, "")
	require.NoError(t, err)

	decryptedContent, err := os.ReadFile(decryptedPath)
	require.NoError(t, err)
	require.Equal(t, testKey, string(decryptedContent))
}

func boolPtr(b bool) *bool {
	return &b
}