package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	promptpkg "github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAgentPromptsUseSelectedInstructionsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, writePromptTestFile(filepath.Join(dir, "AGENTS.md"), "selected agents instructions"))
	require.NoError(t, writePromptTestFile(filepath.Join(dir, "CLAUDE.md"), "claude instructions"))
	require.NoError(t, writePromptTestFile(filepath.Join(dir, "docs", "extra.md"), "extra context"))

	contextPaths := append(config.DefaultContextPaths(), "docs/extra.md")
	store := config.NewTestStoreWithWorkingDir(&config.Config{
		Options: &config.Options{
			ContextPaths: contextPaths,
		},
	}, dir)

	fixedTime := func() time.Time {
		tm, _ := time.Parse("1/2/2006", "1/1/2025")
		return tm
	}

	prompts := map[string]*promptpkg.Prompt{}
	coder, err := coderPrompt(promptpkg.WithTimeFunc(fixedTime), promptpkg.WithPlatform("linux"))
	require.NoError(t, err)
	prompts["coder"] = coder

	task, err := taskPrompt(promptpkg.WithTimeFunc(fixedTime), promptpkg.WithPlatform("linux"))
	require.NoError(t, err)
	prompts["task"] = task

	fetch, err := promptpkg.NewPrompt(
		"agentic_fetch",
		string(agenticFetchPromptTmpl),
		promptpkg.WithTimeFunc(fixedTime),
		promptpkg.WithPlatform("linux"),
		promptpkg.WithWorkingDir(filepath.Join(dir, "tmp")),
	)
	require.NoError(t, err)
	prompts["agentic_fetch"] = fetch

	for name, tmpl := range prompts {
		t.Run(name, func(t *testing.T) {
			systemPrompt, err := tmpl.Build(context.Background(), "test-provider", "test-model", store)
			require.NoError(t, err)
			require.Contains(t, systemPrompt, `<file path="`+filepath.Join(dir, "AGENTS.md")+`">`)
			require.Contains(t, systemPrompt, "selected agents instructions")
			require.Contains(t, systemPrompt, `<file path="`+filepath.Join(dir, "docs", "extra.md")+`">`)
			require.Contains(t, systemPrompt, "extra context")
			require.NotContains(t, systemPrompt, "claude instructions")
		})
	}
}

func writePromptTestFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
