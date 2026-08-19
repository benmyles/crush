package model

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/stretchr/testify/require"
)

type titleWorkspace struct {
	workspace.Workspace
	dir string
}

func (w titleWorkspace) WorkingDir() string { return w.dir }
func (titleWorkspace) Config() *config.Config {
	return &config.Config{}
}

func TestWindowTitle(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	u := newTestUI()
	u.com.Workspace = titleWorkspace{dir: dir}
	u.workingPlaceholder = "Working!"

	t.Run("no session", func(t *testing.T) {
		require.Equal(t, "crush "+dir, u.windowTitle())
	})

	t.Run("session idle", func(t *testing.T) {
		u.session = &session.Session{ID: "s1", Title: "Fix the deploy"}
		require.Equal(t, "Fix the deploy · "+dir, u.windowTitle())
	})

	t.Run("session busy", func(t *testing.T) {
		u.agentBusyCache.set(true)
		require.Equal(t, "Working! · Fix the deploy · "+dir, u.windowTitle())
	})

	t.Run("untitled session idle", func(t *testing.T) {
		u.agentBusyCache.set(false)
		u.session.Title = agent.DefaultSessionName
		require.Equal(t, "crush "+dir, u.windowTitle())
	})

	t.Run("untitled session busy", func(t *testing.T) {
		u.agentBusyCache.set(true)
		require.Equal(t, "Working! · "+dir, u.windowTitle())
	})
}
