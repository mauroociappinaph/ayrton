package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCommand() *cobra.Command {
	// Reset output on root and subcommands to avoid state leakage
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	greetCmd.SetOut(nil)
	greetCmd.SetErr(nil)
	versionCmd.SetOut(nil)
	versionCmd.SetErr(nil)
	learningCmd.SetOut(nil)
	learningCmd.SetErr(nil)
	learnCmd.SetOut(nil)
	learnCmd.SetErr(nil)
	recallCmd.SetOut(nil)
	recallCmd.SetErr(nil)
	recentCmd.SetOut(nil)
	recentCmd.SetErr(nil)
	return rootCmd
}

func TestRootCmd_Help(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := newTestCommand()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "ayrton")
	assert.Contains(t, output, "greet")
	assert.Contains(t, output, "version")
}

func TestRootCmd_VersionFlag(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := newTestCommand()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"version"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "version")
	assert.Contains(t, output, "commit")
	assert.Contains(t, output, "built")
}

func TestGreetCmd_DefaultName(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := newTestCommand()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"greet"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "¡Hola, mundo! 👋")
}

func TestGreetCmd_CustomName(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := newTestCommand()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"greet", "Mauro"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "¡Hola, Mauro! 👋")
}

func TestGreetCmd_JSONOutput(t *testing.T) {
	viper.Set("output", "json")
	defer viper.Set("output", "text")

	buf := new(bytes.Buffer)
	cmd := newTestCommand()
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"greet", "Test"})

	err := cmd.Execute()
	require.NoError(t, err)

	output := strings.TrimSpace(buf.String())
	assert.Contains(t, output, `{"greeting":"¡Hola, Test! 👋"}`)
	assert.True(t, strings.HasSuffix(output, "}"))
	assert.JSONEq(t, `{"greeting": "¡Hola, Test! 👋"}`, output)
}

func TestGreetCmd_InvalidArgs(t *testing.T) {
	buf := new(bytes.Buffer)
	cmd := newTestCommand()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"greet", "a", "b", "c"})

	err := cmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "accepts at most 1 arg")
}

func TestRootCmd_PersistentFlags(t *testing.T) {
	viper.Reset()
	// Re-bind flags after reset
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))

	cmd := newTestCommand()
	cmd.SetArgs([]string{"--verbose", "version"})

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	require.NoError(t, err)

	assert.True(t, viper.GetBool("verbose"), "verbose flag should be bound to viper")
}

func TestInitConfig_WithConfigFile(t *testing.T) {
	t.Skip("Requiere archivo de config temporal")
}

// Integration tests for SDD CLI commands
func TestSDD_ProposeCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	cmd := newTestCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"sdd", "propose", "--issue", "123", "--title", "Test Issue", "--body", "Test body"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Verify file was created
	proposalPath := filepath.Join(".atl", "proposals", "123.md")
	content, err := os.ReadFile(proposalPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Test Issue")
	assert.Contains(t, string(content), "#123")
	assert.Contains(t, string(content), "Test body")
}

func TestSDD_SpecCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	cmd := newTestCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"sdd", "spec", "--issue", "456"})

	err := cmd.Execute()
	require.NoError(t, err)

	specPath := filepath.Join(".atl", "specs", "456.md")
	content, err := os.ReadFile(specPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "#456")
	assert.Contains(t, string(content), "Requirements")
	assert.Contains(t, string(content), "Scenarios")
}

func TestSDD_DesignCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	cmd := newTestCommand()
	cmd.SetArgs([]string{"sdd", "design", "--issue", "789"})

	err := cmd.Execute()
	require.NoError(t, err)

	designPath := filepath.Join(".atl", "designs", "789.md")
	content, err := os.ReadFile(designPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "#789")
	assert.Contains(t, string(content), "Architecture Overview")
	assert.Contains(t, string(content), "Components")
}

func TestSDD_TasksCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	cmd := newTestCommand()
	cmd.SetArgs([]string{"sdd", "tasks", "--issue", "999"})

	err := cmd.Execute()
	require.NoError(t, err)

	tasksPath := filepath.Join(".atl", "tasks", "999.md")
	content, err := os.ReadFile(tasksPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "#999")
	assert.Contains(t, string(content), "Implementation Tasks")
	assert.Contains(t, string(content), "Task 1")
}

// Integration tests for Learning CLI commands
func TestLearn_AddCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	// Set HOME to temp dir so engram DB is created there
	t.Setenv("HOME", tmpDir)

	cmd := newTestCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"learn", "add", "Test pattern for integration test", "--category", "test", "--context", "Integration test context", "--confidence", "0.9"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Learned pattern")
}

func TestLearn_RecallCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	t.Setenv("HOME", tmpDir)

	// First add a pattern
	cmd := newTestCommand()
	cmd.SetArgs([]string{"learn", "add", "Recall test pattern", "--category", "recall-test"})
	err := cmd.Execute()
	require.NoError(t, err)

	// Then recall it
	buf := new(bytes.Buffer)
	cmd2 := newTestCommand()
	cmd2.SetOut(buf)
	cmd2.SetErr(buf)
	cmd2.SetArgs([]string{"learn", "recall", "Recall test"})

	err = cmd2.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Recall test pattern")
	assert.Contains(t, buf.String(), "recall-test")
}

func TestLearn_RecallByCategoryCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	t.Setenv("HOME", tmpDir)

	// Add pattern with specific category
	cmd := newTestCommand()
	cmd.SetArgs([]string{"learn", "add", "Category test pattern", "--category", "my-category"})
	err := cmd.Execute()
	require.NoError(t, err)

	// Recall by category
	buf := new(bytes.Buffer)
	cmd2 := newTestCommand()
	cmd2.SetOut(buf)
	cmd2.SetErr(buf)
	cmd2.SetArgs([]string{"learn", "recall", "anything", "--category", "my-category"})

	err = cmd2.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Category test pattern")
	assert.Contains(t, buf.String(), "my-category")
}

func TestLearn_RecentCmd(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	t.Setenv("HOME", tmpDir)

	// Add multiple patterns with different categories to avoid upsert
	for i := 0; i < 3; i++ {
		cmd := newTestCommand()
		cmd.SetArgs([]string{"learn", "add", "Recent pattern", "--category", fmt.Sprintf("recent-%d", i)})
		err := cmd.Execute()
		require.NoError(t, err)
	}

	// Get recent
	buf := new(bytes.Buffer)
	cmd2 := newTestCommand()
	cmd2.SetOut(buf)
	cmd2.SetErr(buf)
	cmd2.SetArgs([]string{"learn", "recent", "--limit", "2"})

	err := cmd2.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Recent pattern")
	// Should only show 2 due to limit
	lines := strings.Split(buf.String(), "\n")
	patternLines := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "1. ") || strings.HasPrefix(line, "2. ") {
			patternLines++
		}
	}
	assert.LessOrEqual(t, patternLines, 2)
}