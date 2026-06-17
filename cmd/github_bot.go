package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mauroociappinaph/ayrton/internal/github"
)

var githubBotCmd = &cobra.Command{
	Use:   "github-bot",
	Short: "GitHub App Bot for SDD workflow automation",
	Long: `GitHub App Bot runs a webhook server that listens for GitHub events
and automates SDD (Specification-Driven Development) workflows through
PR auto-reviews and @mention command routing.

The bot handles:
  - Pull request auto-reviews (runs 'ayrton sdd verify' on PR changes)
  - @mention command routing (@ayrton <phase> --issue N)
  - Per-repository configuration
  - Rate limiting with exponential backoff`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var githubBotServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the GitHub App webhook server",
	Long: `Start the HTTP server to receive GitHub webhooks.
The server listens on the configured port (default: 8080) and handles:
  - POST /webhook - GitHub webhook endpoint
  - GET /healthz - Health check endpoint`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		configPath, _ := cmd.Flags().GetString("config")

		// Load configuration
		if configPath != "" {
			viper.SetConfigFile(configPath)
		}

		config, err := github.LoadConfig(viper.GetViper())
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if err := config.Validate(); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		// Load private key (handles age decryption)
		keyPath, err := config.LoadPrivateKey("")
		if err != nil {
			return fmt.Errorf("load private key: %w", err)
		}
		defer func() {
			if keyPath != config.PrivateKeyPath {
				_ = os.Remove(keyPath)
			}
		}()

		// Create GitHub App client
		client := github.NewGitHubAppClient(config.AppID, keyPath, config.WebhookSecret)

		// Create webhook handler
		handler := github.NewWebhookHandler(client, nil)

		// Create HTTP server
		mux := http.NewServeMux()
		mux.Handle("/webhook", handler)
		mux.HandleFunc("/healthz", github.HealthzHandler("dev"))

		server := &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		}

		// Handle graceful shutdown
		go func() {
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
			fmt.Fprintln(os.Stderr, "\nShutting down...")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}()

		fmt.Fprintf(os.Stderr, "Starting GitHub App Bot server on port %d...\n", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}

		return nil
	},
}

var githubBotOnceCmd = &cobra.Command{
	Use:   "once",
	Short: "Process a single webhook payload from file (for testing)",
	Long: `Process a single webhook payload from a JSON file.
Useful for testing webhook handling without a running server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		payloadFile, _ := cmd.Flags().GetString("payload-file")
		eventType, _ := cmd.Flags().GetString("event-type")
		deliveryID, _ := cmd.Flags().GetString("delivery-id")

		if payloadFile == "" {
			return fmt.Errorf("--payload-file is required")
		}

		payload, err := os.ReadFile(payloadFile)
		if err != nil {
			return fmt.Errorf("read payload file: %w", err)
		}

		// Load configuration
		config, err := github.LoadConfig(viper.GetViper())
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if err := config.Validate(); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		keyPath, err := config.LoadPrivateKey("")
		if err != nil {
			return fmt.Errorf("load private key: %w", err)
		}
		defer func() {
			if keyPath != config.PrivateKeyPath {
				_ = os.Remove(keyPath)
			}
		}()

		client := github.NewGitHubAppClient(config.AppID, keyPath, config.WebhookSecret)

		// Parse and process
		event, err := client.ParseWebhookPayload(payload, eventType)
		if err != nil {
			return fmt.Errorf("parse payload: %w", err)
		}

		if deliveryID != "" {
			event.DeliveryID = deliveryID
		}

		fmt.Fprintf(os.Stderr, "Processing event: %s (delivery: %s)\n", event.EventType, event.DeliveryID)
		
		// Print parsed event
		output, _ := json.MarshalIndent(event, "", "  ")
		fmt.Println(string(output))

		return nil
	},
}

var githubBotConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure GitHub App interactively",
	Long: `Configure the GitHub App with interactive prompts.
This command guides you through:
  1. Creating a GitHub App manifest
  2. Authorizing the app installation
  3. Storing encrypted configuration`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runGitHubBotConfig(cmd)
	},
}

var githubBotInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install GitHub App on a repository",
	Long: `Install the configured GitHub App on a specific repository.
This adds the installation to the configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, _ := cmd.Flags().GetString("repo")
		if repo == "" {
			return fmt.Errorf("--repo is required (format: owner/repo)")
		}
		return runGitHubBotInstall(cmd, repo)
	},
}

func init() {
	// Serve command flags
	githubBotServeCmd.Flags().IntP("port", "p", 8080, "Port to listen on")
	githubBotServeCmd.Flags().String("config", "", "Config file path (default: ~/.ayrton.yaml)")

	// Once command flags
	githubBotOnceCmd.Flags().String("payload-file", "", "Path to webhook payload JSON file")
	githubBotOnceCmd.Flags().String("event-type", "", "GitHub event type (e.g., pull_request, issue_comment)")
	githubBotOnceCmd.Flags().String("delivery-id", "", "X-GitHub-Delivery header value")
	_ = githubBotOnceCmd.MarkFlagRequired("payload-file")

	// Config command flags
	githubBotConfigCmd.Flags().Bool("non-interactive", false, "Run without interactive prompts")

	// Install command flags
	githubBotInstallCmd.Flags().String("repo", "", "Repository to install on (owner/repo)")

	githubBotCmd.AddCommand(
		githubBotServeCmd,
		githubBotOnceCmd,
		githubBotConfigCmd,
		githubBotInstallCmd,
	)

	rootCmd.AddCommand(githubBotCmd)
}

func runGitHubBotConfig(cmd *cobra.Command) error {
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")

	fmt.Println("GitHub App Configuration")
	fmt.Println("========================")
	fmt.Println()

	if nonInteractive {
		fmt.Println("Non-interactive mode not yet implemented")
		return fmt.Errorf("non-interactive mode not implemented")
	}

	// TODO: Implement interactive configuration
	// This would:
	// 1. Prompt for App ID
	// 2. Prompt for private key path
	// 3. Generate manifest URL
	// 4. Handle OAuth callback
	// 5. Store encrypted config

	fmt.Println("Interactive configuration not yet implemented.")
	fmt.Println("Please manually configure ~/.ayrton.yaml with github_bot section.")
	return nil
}

func runGitHubBotInstall(cmd *cobra.Command, repo string) error {
	fmt.Printf("Installing GitHub App on %s...\n", repo)
	// TODO: Implement installation flow
	return fmt.Errorf("not implemented")
}