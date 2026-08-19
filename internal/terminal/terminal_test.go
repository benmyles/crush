package terminal

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stubTmux is a POSIX sh script that emulates the subset of the tmux CLI
// the Controller uses. Session state lives in $CRUSH_STUB_DIR/sessions and
// every invocation is appended to $CRUSH_STUB_LOG for assertion.
const stubTmux = `#!/bin/sh
# args: -L crush <subcommand> ...
echo "$@" >> "${CRUSH_STUB_LOG:?}"

state="${CRUSH_STUB_DIR:?}/sessions"
sub="${3:-}"

case "$sub" in
new-session)
    last=""
    for a in "$@"; do last="$a"; done
    # "fail" as the command simulates a program that exits immediately.
    if [ "$last" = "fail" ]; then exit 0; fi
    name=""
    prev=""
    for a in "$@"; do
        if [ "$prev" = "-s" ]; then name="$a"; fi
        prev="$a"
    done
    echo "$name" >> "$state"
    ;;
has-session)
    name=""
    prev=""
    for a in "$@"; do
        if [ "$prev" = "-t" ]; then name="$a"; fi
        prev="$a"
    done
    if grep -qx "$name" "$state" 2>/dev/null; then exit 0; fi
    echo "can't find session: $name" >&2
    exit 1
    ;;
capture-pane)
    name=""
    prev=""
    for a in "$@"; do
        if [ "$prev" = "-t" ]; then name="$a"; fi
        prev="$a"
    done
    if ! grep -qx "$name" "$state" 2>/dev/null; then
        echo "can't find session: $name" >&2
        exit 1
    fi
    counter="${CRUSH_STUB_DIR:?}/captures"
    n=$(cat "$counter" 2>/dev/null || echo 0)
    n=$((n + 1))
    echo "$n" > "$counter"
    if [ "$n" -ge 3 ]; then
        printf 'READY\n'
    else
        printf 'busy %s\n' "$n"
    fi
    ;;
list-sessions)
    if [ ! -f "$state" ]; then
        echo "no server running on /tmp/stub" >&2
        exit 1
    fi
    while read -r name; do
        printf '%s\t120\t40\tssh\t111\n' "$name"
    done < "$state"
    ;;
kill-session)
    name=""
    prev=""
    for a in "$@"; do
        if [ "$prev" = "-t" ]; then name="$a"; fi
        prev="$a"
    done
    if ! grep -qx "$name" "$state" 2>/dev/null; then
        echo "can't find session: $name" >&2
        exit 1
    fi
    grep -vx "$name" "$state" > "$state.tmp" 2>/dev/null || true
    mv "$state.tmp" "$state" 2>/dev/null || true
    ;;
kill-server)
    rm -f "$state"
    ;;
send-keys)
    exit 0
    ;;
resize-window)
    exit 0
    ;;
*)
    echo "unexpected subcommand: $sub" >&2
    exit 1
    ;;
esac
`

// newTestController installs the stub tmux on PATH and returns a
// Controller pointing at a fresh socket dir.
func newTestController(t *testing.T) *Controller {
	t.Helper()

	stubDir := t.TempDir()
	stubPath := filepath.Join(stubDir, "tmux")
	require.NoError(t, os.WriteFile(stubPath, []byte(stubTmux), 0o755))

	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CRUSH_STUB_DIR", stubDir)
	t.Setenv("CRUSH_STUB_LOG", filepath.Join(stubDir, "calls.log"))

	c := NewController(filepath.Join(stubDir, "data"))
	c.fastFailDelay = 0
	return c
}

// stubLog returns the stub's invocation log, one command per line.
func stubLog(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(os.Getenv("CRUSH_STUB_LOG"))
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func stubLogContains(t *testing.T, substr string) bool {
	t.Helper()
	for _, line := range stubLog(t) {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

func TestController_Start(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)

	s, err := c.Start(ctx, "", "/work", "ssh example.com", 100, 30)
	require.NoError(t, err)
	require.True(t, validID(s.ID))
	require.False(t, s.Existing)
	require.True(t, stubLogContains(t, "new-session -d -s "+s.ID+" -c /work -x 100 -y 30 ssh example.com"))
}

func TestController_Start_ReconnectByName(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)

	first, err := c.Start(ctx, "Deploy", "/work", "ssh example.com", 0, 0)
	require.NoError(t, err)
	require.Equal(t, SessionPrefix+"deploy", first.ID)
	require.False(t, first.Existing)

	again, err := c.Start(ctx, "deploy", "/work", "ssh example.com", 0, 0)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	require.True(t, again.Existing)

	count := 0
	for _, line := range stubLog(t) {
		if strings.Contains(line, "new-session") {
			count++
		}
	}
	require.Equal(t, 1, count, "reconnect must not spawn a second session")
}

func TestController_Start_FastFailure(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)

	_, err := c.Start(ctx, "", "/work", "fail", 0, 0)
	require.ErrorContains(t, err, "exited immediately")
}

func TestController_Start_MaxTerminals(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)

	for range MaxTerminals {
		_, err := c.Start(ctx, "", "/work", "sh", 0, 0)
		require.NoError(t, err)
	}

	_, err := c.Start(ctx, "", "/work", "sh", 0, 0)
	require.ErrorContains(t, err, "too many terminals")
}

func TestController_Start_NoTmux(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // Empty PATH directory: no tmux binary.
	c := NewController(t.TempDir())
	_, err := c.Start(context.Background(), "", "/work", "sh", 0, 0)
	require.ErrorIs(t, err, ErrNoTmux)
}

func TestController_SendKeys(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)
	s, err := c.Start(ctx, "", "/work", "ssh example.com", 0, 0)
	require.NoError(t, err)

	require.NoError(t, c.SendKeys(ctx, s.ID, "ls -la", []string{"C-c"}, true))
	require.True(t, stubLogContains(t, "send-keys -t "+s.ID+" -l ls -la"))
	require.True(t, stubLogContains(t, "send-keys -t "+s.ID+" C-c Enter"))
}

func TestController_SendKeys_InvalidID(t *testing.T) {
	c := newTestController(t)
	err := c.SendKeys(context.Background(), "other-session", "x", nil, false)
	require.ErrorContains(t, err, "invalid terminal ID")
}

func TestController_Capture(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)
	s, err := c.Start(ctx, "", "/work", "ssh example.com", 0, 0)
	require.NoError(t, err)

	screen, err := c.Capture(ctx, s.ID, false)
	require.NoError(t, err)
	require.Contains(t, screen, "busy")

	_, err = c.Capture(ctx, "crush-missing", false)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestController_WaitFor(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)
	s, err := c.Start(ctx, "", "/work", "ssh example.com", 0, 0)
	require.NoError(t, err)

	screen, ok, err := c.WaitFor(ctx, s.ID, "READY", 5*time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, screen, "READY")
}

func TestController_WaitFor_Timeout(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)
	s, err := c.Start(ctx, "", "/work", "ssh example.com", 0, 0)
	require.NoError(t, err)

	_, ok, err := c.WaitFor(ctx, s.ID, "NEVER", 200*time.Millisecond)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestController_List(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)

	sessions, err := c.List(ctx) // No server yet.
	require.NoError(t, err)
	require.Empty(t, sessions)

	first, err := c.Start(ctx, "alpha", "/work", "ssh example.com", 0, 0)
	require.NoError(t, err)
	second, err := c.Start(ctx, "beta", "/work", "sh", 0, 0)
	require.NoError(t, err)

	sessions, err = c.List(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	byID := map[string]Session{}
	for _, s := range sessions {
		byID[s.ID] = s
	}
	require.Equal(t, "ssh", byID[first.ID].Command)
	require.Equal(t, 111, byID[first.ID].PID)
	require.Equal(t, 120, byID[second.ID].Cols)
	require.Equal(t, 40, byID[second.ID].Rows)
}

func TestController_Resize(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)
	s, err := c.Start(ctx, "", "/work", "sh", 0, 0)
	require.NoError(t, err)

	require.NoError(t, c.Resize(ctx, s.ID, 200, 50))
	require.True(t, stubLogContains(t, "resize-window -t "+s.ID+" -x 200 -y 50"))

	require.Error(t, c.Resize(ctx, s.ID, 0, 50))
}

func TestController_Kill(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)
	s, err := c.Start(ctx, "", "/work", "sh", 0, 0)
	require.NoError(t, err)

	require.NoError(t, c.Kill(ctx, s.ID))
	require.ErrorIs(t, c.Kill(ctx, s.ID), ErrNotFound)

	exists, err := c.Exists(ctx, s.ID)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestController_KillAll(t *testing.T) {
	ctx := context.Background()
	c := newTestController(t)
	_, err := c.Start(ctx, "", "/work", "sh", 0, 0)
	require.NoError(t, err)
	_, err = c.Start(ctx, "", "/work", "sh", 0, 0)
	require.NoError(t, err)

	require.NoError(t, c.KillAll(ctx))

	sessions, err := c.List(ctx)
	require.NoError(t, err)
	require.Empty(t, sessions)
}

// TestController_RealTmuxRoundTrip exercises the controller against a
// real tmux server when one is installed.
func TestController_RealTmuxRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	ctx := context.Background()

	// macOS limits unix socket path lengths, so use a short path instead
	// of t.TempDir(), whose /var/folders prefix can overflow it.
	dir, err := os.MkdirTemp("/tmp", "crush-tmux-it-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	c := NewController(dir)
	t.Cleanup(func() {
		_ = c.KillAll(context.Background())
	})

	s, err := c.Start(ctx, "roundtrip", "", "sh", 80, 24)
	require.NoError(t, err)
	defer func() { _ = c.Kill(context.Background(), s.ID) }()

	require.NoError(t, c.SendKeys(ctx, s.ID, "echo crush-terminal-ok", nil, true))

	screen, ok, err := c.WaitFor(ctx, s.ID, "crush-terminal-ok", 10*time.Second)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, screen, "crush-terminal-ok")

	sessions, err := c.List(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	require.Equal(t, s.ID, sessions[0].ID)

	require.NoError(t, c.Resize(ctx, s.ID, 100, 30))
	exists, err := c.Exists(ctx, s.ID)
	require.NoError(t, err)
	require.True(t, exists)
}
