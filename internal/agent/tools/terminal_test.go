package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/terminal"
	"github.com/stretchr/testify/require"
)

// stubTmuxScript emulates the tmux CLI subset the terminal tools use.
// Session state lives in $CRUSH_STUB_DIR/sessions and every invocation is
// appended to $CRUSH_STUB_LOG.
const stubTmuxScript = `#!/bin/sh
echo "$@" >> "${CRUSH_STUB_LOG:?}"

state="${CRUSH_STUB_DIR:?}/sessions"
sub="${3:-}"

case "$sub" in
new-session)
    name=""
    prev=""
    for a in "$@"; do
        if [ "$prev" = "-s" ]; then name="$a"; fi
        prev="$a"
    done
    echo "$name" >> "$state"
    ;;
has-session|resize-window|send-keys)
    exit 0
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
    printf 'screen for %s\npassword:\n$ \n' "$name"
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
*)
    echo "unexpected subcommand: $sub" >&2
    exit 1
    ;;
esac
`

// newTerminalToolsTest installs the stub tmux on PATH and returns a
// controller plus the four permission-gated tools wired against it.
func newTerminalToolsTest(t *testing.T) (*terminal.Controller, fantasy.AgentTool /*start*/, fantasy.AgentTool /*input*/, fantasy.AgentTool /*kill*/, *recordingPermissionService) {
	t.Helper()

	stubDir := t.TempDir()
	stubPath := filepath.Join(stubDir, "tmux")
	require.NoError(t, os.WriteFile(stubPath, []byte(stubTmuxScript), 0o755))

	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CRUSH_STUB_DIR", stubDir)
	t.Setenv("CRUSH_STUB_LOG", filepath.Join(stubDir, "calls.log"))

	ctrl := terminal.NewController(filepath.Join(stubDir, "data"))

	perms := &recordingPermissionService{
		Broker: pubsub.NewBroker[permission.PermissionRequest](),
		allow:  true,
	}
	workingDir := filepath.Join(stubDir, "work")
	require.NoError(t, os.MkdirAll(workingDir, 0o755))

	return ctrl,
		NewTerminalStartTool(perms, workingDir, ctrl),
		NewTerminalInputTool(perms, workingDir, ctrl),
		NewTerminalKillTool(perms, workingDir, ctrl),
		perms
}

func runTerminalTool[T any](t *testing.T, tool fantasy.AgentTool, ctx context.Context, params T) fantasy.ToolResponse {
	t.Helper()
	input, err := json.Marshal(params)
	require.NoError(t, err)
	call := fantasy.ToolCall{
		ID:    "test-call",
		Name:  "terminal-test",
		Input: string(input),
	}
	resp, err := tool.Run(ctx, call)
	require.NoError(t, err)
	return resp
}

// startTerminal starts a terminal with the start tool and returns its ID.
func startTerminal(t *testing.T, ctx context.Context, start fantasy.AgentTool, name string) string {
	t.Helper()
	resp := runTerminalTool(t, start, ctx, TerminalStartParams{
		Command:     "ssh example.com",
		Description: "test terminal",
		Name:        name,
	})
	require.False(t, resp.IsError, "start failed: %s", resp.Content)
	var meta TerminalResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.NotEmpty(t, meta.TerminalID)
	return meta.TerminalID
}

func terminalCtx() context.Context {
	return context.WithValue(context.Background(), SessionIDContextKey, "test-session")
}

func TestTerminalStart_RequiresPermission(t *testing.T) {
	_, start, _, _, perms := newTerminalToolsTest(t)
	perms.allow = false

	resp := runTerminalTool(t, start, terminalCtx(), TerminalStartParams{
		Command:     "ssh example.com",
		Description: "test",
	})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "User denied permission")
	require.Equal(t, 1, perms.requestCount)
}

func TestTerminalStart_MissingCommand(t *testing.T) {
	_, start, _, _, _ := newTerminalToolsTest(t)
	resp := runTerminalTool(t, start, terminalCtx(), TerminalStartParams{})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "command is required")
}

func TestTerminalStart(t *testing.T) {
	ctx := terminalCtx()
	_, start, _, _, _ := newTerminalToolsTest(t)

	resp := runTerminalTool(t, start, ctx, TerminalStartParams{
		Command:     "ssh example.com",
		Description: "ssh to prod",
	})
	require.False(t, resp.IsError, resp.Content)

	var meta TerminalResponseMetadata
	require.NoError(t, json.Unmarshal([]byte(resp.Metadata), &meta))
	require.True(t, strings.HasPrefix(meta.TerminalID, "crush-"))
	require.True(t, meta.Running)
	require.False(t, meta.Existing)
	require.Contains(t, resp.Content, "started")
	require.Contains(t, resp.Content, meta.TerminalID)
}

func TestTerminalStart_ReconnectByName(t *testing.T) {
	ctx := terminalCtx()
	_, start, _, _, _ := newTerminalToolsTest(t)

	first := startTerminal(t, ctx, start, "deploy")
	second := startTerminal(t, ctx, start, "deploy")
	require.Equal(t, first, second)
}

func TestTerminalInput_SendsKeysAndReadsBack(t *testing.T) {
	ctx := terminalCtx()
	_, start, input, _, _ := newTerminalToolsTest(t)
	id := startTerminal(t, ctx, start, "")

	resp := runTerminalTool(t, input, ctx, TerminalInputParams{
		TerminalID: id,
		Text:       "ls -la",
		Enter:      true,
		SettleMS:   0,
	})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "Input sent")
	require.Contains(t, resp.Content, "screen for "+id)

	log, err := os.ReadFile(os.Getenv("CRUSH_STUB_LOG"))
	require.NoError(t, err)
	require.Contains(t, string(log), fmt.Sprintf("send-keys -t %s -l ls -la", id))
	require.Contains(t, string(log), fmt.Sprintf("send-keys -t %s Enter", id))
}

func TestTerminalInput_UnsupportedKey(t *testing.T) {
	ctx := terminalCtx()
	_, start, input, _, _ := newTerminalToolsTest(t)
	id := startTerminal(t, ctx, start, "")

	resp := runTerminalTool(t, input, ctx, TerminalInputParams{
		TerminalID: id,
		Keys:       []string{"hyper-x"},
	})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "unsupported key")
}

func TestTerminalInput_RequiresPermission(t *testing.T) {
	ctx := terminalCtx()
	_, start, input, _, perms := newTerminalToolsTest(t)
	id := startTerminal(t, ctx, start, "")

	perms.allow = false
	resp := runTerminalTool(t, input, ctx, TerminalInputParams{
		TerminalID: id,
		Text:       "rm -rf /",
		Enter:      true,
		ReadBack:   false,
	})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "User denied permission")
}

func TestTerminalOutput_List(t *testing.T) {
	ctx := terminalCtx()
	ctrl, start, _, _, _ := newTerminalToolsTest(t)
	output := NewTerminalOutputTool(ctrl)

	// No terminals yet.
	resp := runTerminalTool(t, output, ctx, TerminalOutputParams{})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "No active terminals")

	id := startTerminal(t, ctx, start, "")
	resp = runTerminalTool(t, output, ctx, TerminalOutputParams{})
	require.False(t, resp.IsError)
	require.Contains(t, resp.Content, "1 active terminal")
	require.Contains(t, resp.Content, id)
}

func TestTerminalOutput_Capture(t *testing.T) {
	ctx := terminalCtx()
	ctrl, start, _, _, _ := newTerminalToolsTest(t)
	output := NewTerminalOutputTool(ctrl)
	id := startTerminal(t, ctx, start, "")

	resp := runTerminalTool(t, output, ctx, TerminalOutputParams{TerminalID: id})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "current screen")
	require.Contains(t, resp.Content, "screen for "+id)
	require.Contains(t, resp.Content, "password:")
}

func TestTerminalOutput_WaitFor(t *testing.T) {
	ctx := terminalCtx()
	ctrl, start, _, _, _ := newTerminalToolsTest(t)
	output := NewTerminalOutputTool(ctrl)
	id := startTerminal(t, ctx, start, "")

	resp := runTerminalTool(t, output, ctx, TerminalOutputParams{
		TerminalID: id,
		WaitFor:    "password:",
		TimeoutMs:  5000,
	})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, `wait_for "password:" matched`)
}

func TestTerminalOutput_WaitForTimeout(t *testing.T) {
	ctx := terminalCtx()
	ctrl, start, _, _, _ := newTerminalToolsTest(t)
	output := NewTerminalOutputTool(ctrl)
	id := startTerminal(t, ctx, start, "")

	resp := runTerminalTool(t, output, ctx, TerminalOutputParams{
		TerminalID: id,
		WaitFor:    "never-appears",
		TimeoutMs:  200,
	})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, `wait_for "never-appears" not found within 200ms`)
	require.Contains(t, resp.Content, "is still running")
	require.Contains(t, resp.Content, "Call terminal_output again with the same wait_for")
}

func TestTerminalOutput_TimeoutMsValidation(t *testing.T) {
	ctx := terminalCtx()
	ctrl, start, _, _, _ := newTerminalToolsTest(t)
	output := NewTerminalOutputTool(ctrl)
	id := startTerminal(t, ctx, start, "")

	for _, timeout := range []int{0, -5, 30001} {
		resp := runTerminalTool(t, output, ctx, TerminalOutputParams{
			TerminalID: id,
			WaitFor:    "anything",
			TimeoutMs:  timeout,
		})
		require.True(t, resp.IsError)
		require.Contains(t, resp.Content, "timeout_ms must be between 1 and 30000", "timeout %d", timeout)
	}

	// A timeout value is only checked when wait_for is actually used.
	resp := runTerminalTool(t, output, ctx, TerminalOutputParams{TerminalID: id})
	require.False(t, resp.IsError, resp.Content)
}

func TestTerminalInput_SettleMsValidation(t *testing.T) {
	ctx := terminalCtx()
	_, start, input, _, _ := newTerminalToolsTest(t)
	id := startTerminal(t, ctx, start, "")

	for _, settle := range []int{-1, 30001} {
		resp := runTerminalTool(t, input, ctx, TerminalInputParams{
			TerminalID: id,
			Text:       "ls",
			SettleMS:   settle,
		})
		require.True(t, resp.IsError)
		require.Contains(t, resp.Content, "settle_ms must be between 0 and 30000", "settle %d", settle)
	}
}

func TestTerminalOutput_NotFound(t *testing.T) {
	ctx := terminalCtx()
	ctrl, _, _, _, _ := newTerminalToolsTest(t)
	output := NewTerminalOutputTool(ctrl)

	resp := runTerminalTool(t, output, ctx, TerminalOutputParams{TerminalID: "crush-missing"})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "not running")
}

func TestTerminalResize(t *testing.T) {
	ctx := terminalCtx()
	ctrl, start, _, _, _ := newTerminalToolsTest(t)
	resize := NewTerminalResizeTool(ctrl)
	id := startTerminal(t, ctx, start, "")

	resp := runTerminalTool(t, resize, ctx, TerminalResizeParams{TerminalID: id, Cols: 200, Rows: 50})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "resized to 200x50")

	resp = runTerminalTool(t, resize, ctx, TerminalResizeParams{TerminalID: id, Cols: 0, Rows: 50})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "between 1 and 500")
}

func TestTerminalKill(t *testing.T) {
	ctx := terminalCtx()
	_, start, _, kill, perms := newTerminalToolsTest(t)
	id := startTerminal(t, ctx, start, "")
	perms.requestCount = 0

	resp := runTerminalTool(t, kill, ctx, TerminalKillParams{TerminalID: id})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "terminated")

	// Killing again reports a clean error.
	resp = runTerminalTool(t, kill, ctx, TerminalKillParams{TerminalID: id})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "not running")
	require.Equal(t, 2, perms.requestCount)
}

func TestTerminalKill_All(t *testing.T) {
	ctx := terminalCtx()
	ctrl, start, _, kill, _ := newTerminalToolsTest(t)
	startTerminal(t, ctx, start, "one")
	startTerminal(t, ctx, start, "two")

	resp := runTerminalTool(t, kill, ctx, TerminalKillParams{All: true})
	require.False(t, resp.IsError, resp.Content)
	require.Contains(t, resp.Content, "All interactive terminals terminated")

	sessions, err := ctrl.List(ctx)
	require.NoError(t, err)
	require.Empty(t, sessions)
}

func TestTerminalKill_RequiresPermission(t *testing.T) {
	ctx := terminalCtx()
	_, start, _, kill, perms := newTerminalToolsTest(t)
	id := startTerminal(t, ctx, start, "")

	perms.allow = false
	resp := runTerminalTool(t, kill, ctx, TerminalKillParams{TerminalID: id})
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, "User denied permission")
}
