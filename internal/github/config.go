package github

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"filippo.io/age"
)

// GitHubAppConfig holds the GitHub App configuration
type GitHubAppConfig struct {
	AppID          int64                  `mapstructure:"app_id" yaml:"app_id"`
	PrivateKeyPath string                 `mapstructure:"private_key_path" yaml:"private_key_path"`
	WebhookSecret  string                 `mapstructure:"webhook_secret" yaml:"webhook_secret"`
	Installations  map[string]InstallationConfig `mapstructure:"installations" yaml:"installations"`
}

// InstallationConfig holds per-repository installation settings
// Using pointers to distinguish between "not set" and "explicitly false"
type InstallationConfig struct {
	InstallationID    int64  `mapstructure:"installation_id" yaml:"installation_id"`
	Repo              string `mapstructure:"repo" yaml:"repo"`
	AutoReviewEnabled *bool  `mapstructure:"auto_review_enabled" yaml:"auto_review_enabled"`
	MentionsEnabled   *bool  `mapstructure:"mentions_enabled" yaml:"mentions_enabled"`
}

// WebhookEvent represents a processed webhook delivery
type WebhookEvent struct {
	DeliveryID  string    `json:"delivery_id" yaml:"delivery_id"`
	EventType   string    `json:"event_type" yaml:"event_type"`
	PayloadHash string    `json:"payload_hash" yaml:"payload_hash"`
	ProcessedAt time.Time `json:"processed_at" yaml:"processed_at"`
	Result      string    `json:"result" yaml:"result"` // success, failed, skipped
}

// SDDExecution represents an SDD phase execution
type SDDExecution struct {
	IssueNumber   int       `json:"issue_number" yaml:"issue_number"`
	Phase         string    `json:"phase" yaml:"phase"` // propose, spec, design, tasks, apply, verify
	Status        string    `json:"status" yaml:"status"` // running, completed, failed, timeout
	StartedAt     time.Time `json:"started_at" yaml:"started_at"`
	CompletedAt   time.Time `json:"completed_at" yaml:"completed_at"`
	OutputSummary string    `json:"output_summary" yaml:"output_summary"`
}

// Validate validates the GitHubAppConfig
func (c *GitHubAppConfig) Validate() error {
	if c.AppID == 0 {
		return errors.New("app_id is required")
	}
	if c.PrivateKeyPath == "" {
		return errors.New("private_key_path is required")
	}
	if c.WebhookSecret == "" {
		return errors.New("webhook_secret is required")
	}
	return nil
}

// EncryptPrivateKey encrypts the private key file using age
// If passphrase is empty, it reads from AYRTON_AGE_PASSPHRASE env var
func (c *GitHubAppConfig) EncryptPrivateKey(passphrase string) (string, error) {
	if passphrase == "" {
		passphrase = os.Getenv("AYRTON_AGE_PASSPHRASE")
	}
	if passphrase == "" {
		return "", errors.New("passphrase required (set AYRTON_AGE_PASSPHRASE or pass explicitly)")
	}

	// Read the private key
	keyContent, err := os.ReadFile(c.PrivateKeyPath)
	if err != nil {
		return "", fmt.Errorf("read private key: %w", err)
	}

	// Create age recipient from passphrase (using scrypt)
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return "", fmt.Errorf("create recipient: %w", err)
	}

	// Determine output path
	encryptedPath := c.PrivateKeyPath + ".age"

	// Create output file with 0600 permissions
	outFile, err := os.OpenFile(encryptedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("create encrypted file: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	// Encrypt the key content
	w, err := age.Encrypt(outFile, recipient)
	if err != nil {
		return "", fmt.Errorf("create encrypt writer: %w", err)
	}

	if _, err := w.Write(keyContent); err != nil {
		return "", fmt.Errorf("write encrypted content: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("close encrypt writer: %w", err)
	}

	return encryptedPath, nil
}

// DecryptPrivateKey decrypts an age-encrypted private key file
// If passphrase is empty, it reads from AYRTON_AGE_PASSPHRASE env var
func (c *GitHubAppConfig) DecryptPrivateKey(encryptedPath, passphrase string) (string, error) {
	if passphrase == "" {
		passphrase = os.Getenv("AYRTON_AGE_PASSPHRASE")
	}
	if passphrase == "" {
		return "", errors.New("passphrase required (set AYRTON_AGE_PASSPHRASE or pass explicitly)")
	}

	// Open encrypted file
	inFile, err := os.Open(encryptedPath)
	if err != nil {
		return "", fmt.Errorf("open encrypted file: %w", err)
	}
	defer func() { _ = inFile.Close() }()

	// Create identity from passphrase
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return "", fmt.Errorf("create identity: %w", err)
	}

	// Decrypt
	r, err := age.Decrypt(inFile, identity)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	// Read decrypted content
	decrypted := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			decrypted = append(decrypted, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read decrypted content: %w", err)
		}
	}

	// Write to temporary file with 0600 permissions
	tmpDir := os.TempDir()
	decryptedPath := filepath.Join(tmpDir, fmt.Sprintf("github-app-key-%d.pem", time.Now().UnixNano()))

	outFile, err := os.OpenFile(decryptedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("create decrypted file: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	if _, err := outFile.Write(decrypted); err != nil {
		return "", fmt.Errorf("write decrypted content: %w", err)
	}

	return decryptedPath, nil
}

// LoadPrivateKey loads and decrypts the private key for use
// Returns the path to the decrypted key file (caller must clean up)
func (c *GitHubAppConfig) LoadPrivateKey(passphrase string) (string, error) {
	if strings.HasSuffix(c.PrivateKeyPath, ".age") {
		return c.DecryptPrivateKey(c.PrivateKeyPath, passphrase)
	}
	// Already unencrypted
	return c.PrivateKeyPath, nil
}

// SetDefaults sets default values for InstallationConfig
func (i *InstallationConfig) SetDefaults() {
	// Default to true if not explicitly set
	if i.AutoReviewEnabled == nil {
		val := true
		i.AutoReviewEnabled = &val
	}
	if i.MentionsEnabled == nil {
		val := true
		i.MentionsEnabled = &val
	}
}

// AutoReviewEnabledValue returns the boolean value with default true
func (i InstallationConfig) AutoReviewEnabledValue() bool {
	if i.AutoReviewEnabled == nil {
		return true
	}
	return *i.AutoReviewEnabled
}

// MentionsEnabledValue returns the boolean value with default true
func (i InstallationConfig) MentionsEnabledValue() bool {
	if i.MentionsEnabled == nil {
		return true
	}
	return *i.MentionsEnabled
}
// LoadConfig loads GitHub App config from viper
func LoadConfig(v *viper.Viper) (*GitHubAppConfig, error) {
	// Read config file if not already read
	if !v.IsSet("github_bot") {
		if err := v.ReadInConfig(); err != nil {
			// Config file not found is OK - return empty config
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				return &GitHubAppConfig{}, nil
			}
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var config GitHubAppConfig
	if err := v.UnmarshalKey("github_bot", &config); err != nil {
		return nil, fmt.Errorf("unmarshal github_bot config: %w", err)
	}

	// If no github_bot section, return empty config
	if config.AppID == 0 && config.PrivateKeyPath == "" && config.WebhookSecret == "" && len(config.Installations) == 0 {
		return &GitHubAppConfig{}, nil
	}

	// Set defaults for installations
	for k, inst := range config.Installations {
		inst.SetDefaults()
		config.Installations[k] = inst
	}

	return &config, nil
}

// SaveConfig saves GitHub App config to viper
func SaveConfig(v *viper.Viper, config *GitHubAppConfig) error {
	v.Set("github_bot", config)
	return nil
}