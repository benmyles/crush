package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/hooks"
	"github.com/stretchr/testify/require"
)

// isolateConfigEnv points all config discovery at a temp root so tests
// cannot touch the developer's real crush config or data.
func isolateConfigEnv(t *testing.T) string {
	t.Helper()
	isolated := t.TempDir()
	t.Setenv("HOME", isolated)
	t.Setenv("USERPROFILE", isolated)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(isolated, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(isolated, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(isolated, ".cache"))
	t.Setenv("CRUSH_GLOBAL_DATA", "")
	t.Setenv("CRUSH_GLOBAL_CONFIG", "")
	return isolated
}

func fragmentDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "crush", "hooks")
}

func writeFragment(t *testing.T, dir, name, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestHookFragmentsAppendToConfig(t *testing.T) {
	_ = isolateConfigEnv(t)

	workDir := t.TempDir()
	dataDir := t.TempDir()

	// Project config defines a PreToolUse hook; a fragment defines a
	// SessionStart hook. Both must land in the loaded config, and the
	// fragment entry must append rather than replace anything.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "crush.json"), []byte(`{
		"hooks": { "PreToolUse": [ { "matcher": "^bash$", "command": "exit 0" } ] }
	}`), 0o600))

	fragments := fragmentDir(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Dir(fragments))
	fragmentPath := writeFragment(t, fragments, "10-herdr.json", `{
		"hooks": { "SessionStart": [ { "name": "herdr", "command": "echo start" } ] }
	}`)

	store, err := config.Load(workDir, dataDir, false)
	require.NoError(t, err)

	pre := store.Config().Hooks[hooks.EventPreToolUse]
	require.Len(t, pre, 1)
	require.Equal(t, "exit 0", pre[0].Command)

	sessionStart := store.Config().Hooks[hooks.EventSessionStart]
	require.Len(t, sessionStart, 1)
	require.Equal(t, "herdr", sessionStart[0].Name)
	require.Equal(t, "echo start", sessionStart[0].Command)

	require.Contains(t, store.LoadedPaths(), fragmentPath)
}

func TestHookFragmentEventNameNormalized(t *testing.T) {
	_ = isolateConfigEnv(t)

	fragments := fragmentDir(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Dir(fragments))
	writeFragment(t, fragments, "herdr.json", `{
		"hooks": { "session_start": [ { "command": "echo start" } ] }
	}`)

	store, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	require.Len(t, store.Config().Hooks[hooks.EventSessionStart], 1)
}

func TestHookFragmentInvalidSkipped(t *testing.T) {
	_ = isolateConfigEnv(t)

	fragments := fragmentDir(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Dir(fragments))
	writeFragment(t, fragments, "broken.json", `{ not even json`)
	writeFragment(t, fragments, "valid.json", `{
		"hooks": { "SessionStart": [ { "command": "echo start" } ] }
	}`)

	store, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	// The valid fragment loads; the broken one is skipped with a warning.
	require.Len(t, store.Config().Hooks[hooks.EventSessionStart], 1)
}

func TestHookFragmentMissingCommandRejected(t *testing.T) {
	_ = isolateConfigEnv(t)

	fragments := fragmentDir(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Dir(fragments))
	writeFragment(t, fragments, "herdr.json", `{
		"hooks": { "SessionStart": [ { "name": "herdr" } ] }
	}`)

	// Fragments are validated along with the rest of the config.
	_, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.Error(t, err)
}

func TestHookFragmentLoadedOnReload(t *testing.T) {
	_ = isolateConfigEnv(t)

	fragments := fragmentDir(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Dir(fragments))

	store, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	require.Empty(t, store.Config().Hooks)

	writeFragment(t, fragments, "herdr.json", `{
		"hooks": { "SessionStart": [ { "command": "echo start" } ] }
	}`)

	require.NoError(t, store.ReloadFromDisk(t.Context()))
	require.Len(t, store.Config().Hooks[hooks.EventSessionStart], 1)
}

func TestHookFragmentPathsDeterministic(t *testing.T) {
	_ = isolateConfigEnv(t)

	fragments := fragmentDir(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Dir(fragments))
	writeFragment(t, fragments, "b.json", `{}`)
	writeFragment(t, fragments, "a.json", `{}`)

	store, err := config.Load(t.TempDir(), t.TempDir(), false)
	require.NoError(t, err)
	loaded := slices.DeleteFunc(slices.Clone(store.LoadedPaths()), func(p string) bool {
		return filepath.Dir(p) != fragments
	})
	require.Equal(t, []string{
		filepath.Join(fragments, "a.json"),
		filepath.Join(fragments, "b.json"),
	}, loaded)
}
