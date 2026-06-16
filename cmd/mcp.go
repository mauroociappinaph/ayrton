package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"

	"github.com/mauroociappinaph/ayrton/internal/engram"
)

// mcpCmd starts the MCP stdio server for AI agent memory access.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Inicia el servidor MCP stdio para acceso a memoria persistente",
	Long: `Inicia un servidor MCP (Model Context Protocol) sobre stdio que expone
tres herramientas para que cualquier agente de IA pueda leer y escribir
memoria persistente:

  mem_save    - Guarda una observación en la memoria
  mem_search  - Busca observaciones por texto completo
  mem_context - Lista observaciones recientes

Uso típico: ayrton mcp
(clientes MCP como Claude Code, Cline, etc. se conectan automáticamente vía stdio)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMCPServer()
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}

func runMCPServer() error {
	client, err := engram.NewClient()
	if err != nil {
		return fmt.Errorf("init engram: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			slog.Warn("closing engram client", "error", err)
		}
	}()

	srv := server.NewMCPServer(
		"ayrton-memory",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(`Engram memory server for AI agents.

Tools:
  mem_save    - Save an observation to persistent memory
  mem_search  - Search memory using full-text search (FTS5)
  mem_context - List recent observations

All tools work with the same engram database as the ayrton CLI.`),
	)

	// ── Tool 1: mem_save ──────────────────────────────────────────
	memSaveTool := mcp.NewTool("mem_save",
		mcp.WithDescription("Save an observation to persistent memory. Use topic_key to update an existing entry (upsert)."),
		mcp.WithString("title",
			mcp.Description("Short, searchable title (e.g. 'Fixed N+1 in UserList')"),
			mcp.Required(),
		),
		mcp.WithString("type",
			mcp.Description("Type of observation"),
			mcp.Required(),
			mcp.Enum("bugfix", "decision", "architecture", "discovery", "pattern", "config", "preference"),
		),
		mcp.WithString("content",
			mcp.Description("Full content of the observation (markdown supported)"),
			mcp.Required(),
		),
		mcp.WithString("scope",
			mcp.Description("Scope: 'project' or 'personal'"),
		),
		mcp.WithString("project",
			mcp.Description("Project name (required for project-scoped observations)"),
		),
		mcp.WithString("topic_key",
			mcp.Description("Stable key for upsert (reuse to update existing observation)"),
		),
	)

	srv.AddTool(memSaveTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		title, _ := args["title"].(string)
		typeField, _ := args["type"].(string)
		content, _ := args["content"].(string)
		scope, _ := args["scope"].(string)
		project, _ := args["project"].(string)
		topicKey, _ := args["topic_key"].(string)

		if typeField == "" {
			return mcp.NewToolResultError("'type' is required"), nil
		}
		if title == "" {
			return mcp.NewToolResultError("'title' is required"), nil
		}
		if content == "" {
			return mcp.NewToolResultError("'content' is required"), nil
		}
		if scope == "" {
			scope = "project"
		}

		id, err := client.SaveOrUpdate(ctx, &engram.Observation{
			Title:    title,
			Type:     typeField,
			Scope:    scope,
			Project:  project,
			TopicKey: topicKey,
			Content:  content,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("save failed: %v", err)), nil
		}

		data, _ := json.Marshal(map[string]any{
			"id":     id,
			"title":  title,
			"status": "saved",
		})
		return mcp.NewToolResultText(string(data)), nil
	})

	// ── Tool 2: mem_search ────────────────────────────────────────
	memSearchTool := mcp.NewTool("mem_search",
		mcp.WithDescription("Search observations using full-text search (FTS5). Supports multiple filters for precise queries."),
		mcp.WithString("query",
			mcp.Description("Search query (full-text search via FTS5)"),
			mcp.Required(),
		),
		mcp.WithString("type",
			mcp.Description("Filter by type: bugfix, decision, architecture, etc."),
		),
		mcp.WithString("scope",
			mcp.Description("Filter by scope: 'project' or 'personal'"),
		),
		mcp.WithString("project",
			mcp.Description("Filter by project name"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum results (default 20)"),
		),
	)

	srv.AddTool(memSearchTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		query, _ := args["query"].(string)
		typeFilter, _ := args["type"].(string)
		scope, _ := args["scope"].(string)
		project, _ := args["project"].(string)

		if query == "" {
			return mcp.NewToolResultError("'query' is required"), nil
		}

		limit := 20
		if rawLimit, ok := args["limit"]; ok {
			if f, ok := rawLimit.(float64); ok {
				limit = int(f)
			}
		}

		results, err := client.SearchWithOptions(ctx, query, engram.SearchOptions{
			Type:    typeFilter,
			Scope:   scope,
			Project: project,
			Limit:   limit,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
		}

		data, _ := json.Marshal(map[string]any{
			"results": results,
			"count":   len(results),
			"query":   query,
		})
		return mcp.NewToolResultText(string(data)), nil
	})

	// ── Tool 3: mem_context ───────────────────────────────────────
	memContextTool := mcp.NewTool("mem_context",
		mcp.WithDescription("List recent observations to provide session context. Useful for agent initialization and recovery."),
		mcp.WithString("scope",
			mcp.Description("Scope: 'project' or 'personal' (default 'project')"),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum observations to return (default 20)"),
		),
	)

	srv.AddTool(memContextTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()

		scope, _ := args["scope"].(string)
		if scope == "" {
			scope = "project"
		}

		limit := 20
		if rawLimit, ok := args["limit"]; ok {
			if f, ok := rawLimit.(float64); ok {
				limit = int(f)
			}
		}

		results, err := client.ListRecent(ctx, scope, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list recent failed: %v", err)), nil
		}

		data, _ := json.Marshal(map[string]any{
			"observations": results,
			"count":        len(results),
		})
		return mcp.NewToolResultText(string(data)), nil
	})

	return server.ServeStdio(srv)
}
