package cmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testBinName() string {
	name := "ayrton-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func TestMCPServer_ListTools(t *testing.T) {
	ctx := context.Background()
	mcpClient, cleanup := startTestMCPServer(t, ctx)
	defer cleanup()

	// List tools — verify all 3 exist
	tools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err)
	require.NotNil(t, tools)

	toolNames := make(map[string]bool)
	for _, tool := range tools.Tools {
		toolNames[tool.Name] = true
	}
	assert.True(t, toolNames["mem_save"], "expected mem_save tool")
	assert.True(t, toolNames["mem_search"], "expected mem_search tool")
	assert.True(t, toolNames["mem_context"], "expected mem_context tool")
}

func TestMCPServer_MemSaveAndSearch(t *testing.T) {
	ctx := context.Background()
	mcpClient, cleanup := startTestMCPServer(t, ctx)
	defer cleanup()

	// ── Save ───────────────────────────────────────────────────────
	saveReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "mem_save",
			Arguments: map[string]any{
				"title":     "Test observation from MCP",
				"type":      "discovery",
				"content":   "This is a test observation saved via the MCP server",
				"project":   "ayrton-test",
				"topic_key": "mcp-integration-test",
			},
		},
	}

	result, err := mcpClient.CallTool(ctx, saveReq)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.IsError, "mem_save should not return an error")

	// ── Search ─────────────────────────────────────────────────────
	searchReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "mem_search",
			Arguments: map[string]any{
				"query":   "test observation MCP",
				"project": "ayrton-test",
			},
		},
	}

	searchResult, err := mcpClient.CallTool(ctx, searchReq)
	require.NoError(t, err)
	require.NotNil(t, searchResult)
	require.NotEmpty(t, searchResult.Content, "search should return results")
	t.Logf("search result: %+v", searchResult.Content)
}

func TestMCPServer_MemContext(t *testing.T) {
	ctx := context.Background()
	mcpClient, cleanup := startTestMCPServer(t, ctx)
	defer cleanup()

	// Save something first
	_, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "mem_save",
			Arguments: map[string]any{
				"title":   "Context test entry",
				"type":    "discovery",
				"content": "Entry for context test",
				"project": "ayrton-test",
			},
		},
	})
	require.NoError(t, err)

	// List recent
	ctxReq := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "mem_context",
			Arguments: map[string]any{
				"scope": "project",
				"limit": float64(10),
			},
		},
	}

	ctxResult, err := mcpClient.CallTool(ctx, ctxReq)
	require.NoError(t, err)
	require.NotNil(t, ctxResult)
	require.NotEmpty(t, ctxResult.Content, "context should return observations")
	t.Logf("context result: %+v", ctxResult.Content)
}

// ── helpers ────────────────────────────────────────────────────────────────

func startTestMCPServer(t *testing.T, ctx context.Context) (*client.Client, func()) {
	t.Helper()

	// Build the binary to a temp location
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, testBinName())

	// Build the main binary (package main at project root, one level up from cmd/)
	buildCmd := exec.Command("go", "build", "-o", binPath, ".")
	buildCmd.Dir = ".."
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "build failed: %s", string(out))

	// Create temp HOME for isolated db
	dbDir := filepath.Join(tmpDir, "home")
	require.NoError(t, os.MkdirAll(dbDir, 0755))

	env := append(os.Environ(), "HOME="+dbDir)

	mcpClient, err := client.NewStdioMCPClient(binPath, env, "mcp")
	require.NoError(t, err)

	// Initialize handshake
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.Capabilities = mcp.ClientCapabilities{}
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "ayrton-mcp-test",
		Version: "0.1.0",
	}

	_, err = mcpClient.Initialize(ctx, initReq)
	require.NoError(t, err)

	cleanup := func() {
		mcpClient.Close()
	}

	return mcpClient, cleanup
}
