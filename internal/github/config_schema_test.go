package github

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_WithDefaults(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigName(".ayrton")
	v.AddConfigPath(".")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ayrton.yaml")
	v.SetConfigFile(configPath)

	// Create minimal config
	configContent := `
github_bot:
  app_id: 12345
  private_key_path: "/path/to/key.pem"
  webhook_secret: "secret123"
  installations:
    "owner/repo":
      installation_id: 67890
      repo: "owner/repo"
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	config, err := LoadConfig(v)
	require.NoError(t, err)
	require.Equal(t, int64(12345), config.AppID)
	require.Equal(t, "/path/to/key.pem", config.PrivateKeyPath)
	require.Equal(t, "secret123", config.WebhookSecret)
	require.Len(t, config.Installations, 1)
	
	inst := config.Installations["owner/repo"]
	require.Equal(t, int64(67890), inst.InstallationID)
	require.Equal(t, "owner/repo", inst.Repo)
	// Defaults should be applied
	require.True(t, inst.AutoReviewEnabledValue())
	require.True(t, inst.MentionsEnabledValue())
}

func TestLoadConfig_DefaultsWhenMissing(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ayrton.yaml")
	v.SetConfigFile(configPath)

	// Config without explicit auto_review_enabled/mentions_enabled
	configContent := `
github_bot:
  app_id: 12345
  private_key_path: "/path/to/key.pem"
  webhook_secret: "secret123"
  installations:
    "owner/repo":
      installation_id: 67890
      repo: "owner/repo"
      # auto_review_enabled and mentions_enabled not set
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	config, err := LoadConfig(v)
	require.NoError(t, err)

	inst := config.Installations["owner/repo"]
	// Should default to true
	require.True(t, inst.AutoReviewEnabledValue())
	require.True(t, inst.MentionsEnabledValue())
}

func TestLoadConfig_DisabledFeatures(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ayrton.yaml")
	v.SetConfigFile(configPath)

	// Config with features explicitly disabled
	configContent := `
github_bot:
  app_id: 12345
  private_key_path: "/path/to/key.pem"
  webhook_secret: "secret123"
  installations:
    "owner/repo":
      installation_id: 67890
      repo: "owner/repo"
      auto_review_enabled: false
      mentions_enabled: false
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	config, err := LoadConfig(v)
	require.NoError(t, err)

	inst := config.Installations["owner/repo"]
	require.False(t, inst.AutoReviewEnabledValue())
	require.False(t, inst.MentionsEnabledValue())
}

func TestSaveConfig(t *testing.T) {
	v := viper.New()

	trueVal := true
	falseVal := false
	config := &GitHubAppConfig{
		AppID:          12345,
		PrivateKeyPath: "/path/to/key.pem",
		WebhookSecret:  "secret123",
		Installations: map[string]InstallationConfig{
			"owner/repo": {
				InstallationID:    67890,
				Repo:              "owner/repo",
				AutoReviewEnabled: &trueVal,
				MentionsEnabled:   &falseVal,
			},
		},
	}

	err := SaveConfig(v, config)
	require.NoError(t, err)

	// Verify it was saved
	saved := v.Get("github_bot")
	require.NotNil(t, saved)

	// Reload and verify
	config2, err := LoadConfig(v)
	require.NoError(t, err)
	require.Equal(t, config.AppID, config2.AppID)
	require.Equal(t, config.PrivateKeyPath, config2.PrivateKeyPath)
	require.Equal(t, config.WebhookSecret, config2.WebhookSecret)
	require.Equal(t, config.Installations["owner/repo"].AutoReviewEnabledValue(), config2.Installations["owner/repo"].AutoReviewEnabledValue())
	require.Equal(t, config.Installations["owner/repo"].MentionsEnabledValue(), config2.Installations["owner/repo"].MentionsEnabledValue())
}

func TestConfig_MigrationFromOldFormat(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, ".ayrton.yaml")
	v.SetConfigFile(configPath)

	// Old format without github_bot section
	configContent := `
some_other_setting: value
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Should not error, just return empty config (zero values)
	config, err := LoadConfig(v)
	require.NoError(t, err)
	require.NotNil(t, config)
	require.Equal(t, int64(0), config.AppID)
	require.Equal(t, "", config.PrivateKeyPath)
	require.Equal(t, "", config.WebhookSecret)
	require.Nil(t, config.Installations)
}