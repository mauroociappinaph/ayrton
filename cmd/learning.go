package cmd

import (
	"context"
	"fmt"
	"math"

	"github.com/spf13/cobra"
	"github.com/mauroociappina/ayrton/internal/learning"
)

var learningCmd = &cobra.Command{
	Use:   "learn",
	Short: "Learning Agent commands",
	Long:  `Learn and recall patterns using the Learning Agent with Engram persistence.`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var learnCmd = &cobra.Command{
	Use:   "add [description]",
	Short: "Learn a new pattern",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		description := args[0]
		category, _ := cmd.Flags().GetString("category")
		contextStr, _ := cmd.Flags().GetString("context")
		outcome, _ := cmd.Flags().GetString("outcome")
		confidence, _ := cmd.Flags().GetFloat64("confidence")
		if math.IsNaN(confidence) || confidence < 0 || confidence > 1 {
			return fmt.Errorf("confidence must be between 0 and 1, got %v", confidence)
		}

		agent, err := learning.NewAgent("project")
		if err != nil {
			return fmt.Errorf("create agent: %w", err)
		}
		defer func() { _ = agent.Close() }()

		pattern := &learning.Pattern{
			Description: description,
			Category:    category,
			Context:     contextStr,
			Outcome:     outcome,
			Confidence:  confidence,
			UsageCount:  1,
		}

		ctx := context.Background()
		if err := agent.Learn(ctx, pattern); err != nil {
			return fmt.Errorf("learn pattern: %w", err)
		}

		cmd.Printf("✅ Learned pattern: %s\n", description)
		return nil
	},
}

var recallCmd = &cobra.Command{
	Use:   "recall [query]",
	Short: "Recall patterns matching query",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := args[0]
		limit, _ := cmd.Flags().GetInt("limit")
		category, _ := cmd.Flags().GetString("category")

		agent, err := learning.NewAgent("project")
		if err != nil {
			return fmt.Errorf("create agent: %w", err)
		}
		defer func() { _ = agent.Close() }()

		ctx := context.Background()
		var patterns []learning.Pattern

		if category != "" {
			patterns, err = agent.RecallByCategory(ctx, category, limit)
		} else {
			patterns, err = agent.Recall(ctx, query, limit)
		}

		if err != nil {
			return fmt.Errorf("recall patterns: %w", err)
		}

		if len(patterns) == 0 {
			cmd.Println("No patterns found")
			return nil
		}

		cmd.Printf("Found %d pattern(s):\n\n", len(patterns))
		for i, p := range patterns {
			cmd.Printf("%d. %s\n", i+1, p.Description)
			cmd.Printf("   Category: %s | Confidence: %.2f | Usage: %d\n", p.Category, p.Confidence, p.UsageCount)
			if p.Context != "" {
				cmd.Printf("   Context: %s\n", p.Context)
			}
			if p.Outcome != "" {
				cmd.Printf("   Outcome: %s\n", p.Outcome)
			}
			cmd.Println()
		}
		return nil
	},
}

var recentCmd = &cobra.Command{
	Use:   "recent",
	Short: "Show recently learned patterns",
	RunE: func(cmd *cobra.Command, args []string) error {
		limit, _ := cmd.Flags().GetInt("limit")

		agent, err := learning.NewAgent("project")
		if err != nil {
			return fmt.Errorf("create agent: %w", err)
		}
		defer func() { _ = agent.Close() }()

		ctx := context.Background()
		patterns, err := agent.GetRecentPatterns(ctx, limit)
		if err != nil {
			return fmt.Errorf("get recent: %w", err)
		}

		if len(patterns) == 0 {
			cmd.Println("No recent patterns")
			return nil
		}

		cmd.Printf("Recent %d pattern(s):\n\n", len(patterns))
		for i, p := range patterns {
			cmd.Printf("%d. %s\n", i+1, p.Description)
			cmd.Printf("   Category: %s | Confidence: %.2f\n", p.Category, p.Confidence)
			cmd.Println()
		}
		return nil
	},
}

func init() {
	learnCmd.Flags().String("category", "general", "Pattern category")
	learnCmd.Flags().String("context", "", "Context for the pattern")
	learnCmd.Flags().String("outcome", "", "Outcome/result")
	learnCmd.Flags().Float64("confidence", 0.8, "Confidence score (0-1)")

	recallCmd.Flags().Int("limit", 10, "Max results")
	recallCmd.Flags().String("category", "", "Filter by category")

	recentCmd.Flags().Int("limit", 10, "Max results")

	learningCmd.AddCommand(learnCmd, recallCmd, recentCmd)
	rootCmd.AddCommand(learningCmd)
}