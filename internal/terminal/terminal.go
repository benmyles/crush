// Package terminal provides a controller for interactive terminal sessions
// backed by a dedicated tmux server. Sessions live in tmux, not in the
// Crush process, so they survive Crush restarts and can be reconnected
// from a later run.
package terminal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	// SocketName is the tmux socket name Crush uses. A dedicated socket
	// keeps Crush-managed sessions fully isolated from any tmux server the
	// user runs themselves, so operations here can never touch their
	// sessions.
	SocketName = "crush"

	// SessionPrefix marks tmux sessions owned by Crush. Only names with
	// this prefix are ever targeted or listed.
	SessionPrefix = "crush-"

	// MaxTerminals caps the number of concurrent Crush terminals.
	MaxTerminals = 10

	// DefaultColumns and DefaultRows are the fallback pane sizes when a
	// caller does not specify dimensions.
	DefaultColumns = 120
	DefaultRows    = 40

	// capture history options.
	// maxCaptureWait bounds the time WaitFor polls for a match.
	maxCaptureWait = 30 * time.Second

	// maxCommandTime bounds a single tmux invocation so a hung tmux
	// server or wedged pane can never stall an agent tool.
	maxCommandTime = 30 * time.Second

	// fastFailDelay is how long Start waits before checking whether a
	// freshly spawned session survived its first moments.
	fastFailDelay = 400 * time.Millisecond
)

// ErrNoTmux indicates the tmux binary is not available on PATH.
var ErrNoTmux = errors.New("tmux is not installed; interactive terminals require tmux")

// ErrNotFound indicates the terminal does not exist (or no longer exists).
var ErrNotFound = errors.New("terminal not found")

// Session describes a live Crush terminal session discovered via List.
type Session struct {
	ID       string // Terminal ID (tmux session name), e.g. "crush-abc123".
	Command  string // Current foreground command running in the pane.
	PID      int    // Process ID of the pane's current program.
	Cols     int
	Rows     int
	Existing bool // False for newly created sessions, true for reconnects.
}

// Controller drives interactive terminals through a dedicated tmux
// server. It is safe for concurrent use.
type Controller struct {
	// TmuxDir is the TMUX_TMPDIR used for the socket. It must be on a
	// persistent volume so sessions survive reboots and tmp cleanup.
	TmuxDir string

	tmuxBin string

	// fastFailDelay mirrors the package constant so tests can skip the
	// settle wait. Never mutate it from outside this package.
	fastFailDelay time.Duration

	// commandTimeout mirrors maxCommandTime so tests can exercise the
	// hang path quickly. Never mutate it from outside this package.
	commandTimeout time.Duration
}

// NewController returns a Controller whose tmux socket lives under
// dataDir. dataDir should be Crush's persistent data directory. tmux is
// resolved on PATH per invocation, so tests may override PATH.
func NewController(dataDir string) *Controller {
	if dataDir == "" {
		dataDir = filepath.Join(os.TempDir(), "crush-terminal")
	}
	return &Controller{
		TmuxDir: filepath.Join(dataDir, "tmux"),
		// Never set tmuxBin to an absolute path: keeping it "tmux" lets
		// each exec.Command resolve PATH at call time.
		tmuxBin:        "tmux",
		fastFailDelay:  fastFailDelay,
		commandTimeout: maxCommandTime,
	}
}

// Available reports whether the tmux binary can be found.
func (c *Controller) Available() error {
	if _, err := exec.LookPath(c.tmuxBin); err != nil {
		return ErrNoTmux
	}
	return nil
}

// EnsureDir creates the socket directory if it does not exist yet.
func (c *Controller) EnsureDir() error {
	return os.MkdirAll(c.TmuxDir, 0o700)
}

// sessionName maps a user-provided session name to a Crush-owned tmux
// session name. Names are sanitized to tmux-safe characters; empty input
// yields "".
func sessionName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	re := regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)
	return SessionPrefix + strings.ToLower(re.ReplaceAllString(name, "-"))
}

// newID generates a fresh terminal ID (tmux session name).
func newID() (string, error) {
	var buf [3]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate terminal ID: %w", err)
	}
	return SessionPrefix + hex.EncodeToString(buf[:]), nil
}

// validID reports whether id is a Crush-owned terminal ID. All target
// operations gate on this so a caller-supplied ID can never touch a
// non-Crush session.
func validID(id string) bool {
	return strings.HasPrefix(id, SessionPrefix)
}

// run executes a tmux command on the dedicated socket. stderr is
// captured into the returned error for diagnostics.
func (c *Controller) run(ctx context.Context, args ...string) (string, error) {
	if err := c.EnsureDir(); err != nil {
		return "", err
	}

	// Bound every tmux invocation so a hung server or wedged pane can
	// never block an agent tool indefinitely.
	ctx, cancel := context.WithTimeout(ctx, c.commandTimeout)
	defer cancel()

	fullArgs := make([]string, 0, len(args)+2)
	fullArgs = append(fullArgs, "-L", SocketName)
	fullArgs = append(fullArgs, args...)

	cmd := exec.CommandContext(ctx, c.tmuxBin, fullArgs...)
	cmd.Env = append(os.Environ(), "TMUX_TMPDIR="+c.TmuxDir)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// Start launches command in a new detached tmux session and returns it.
// If name is provided and a Crush session with that name already exists,
// the existing session is returned instead of spawning a new one, which
// lets agents reconnect to terminals from previous runs.
//
// command is executed through tmux's default shell inside the pane, so
// shell syntax (pipes, redirections, env assignments) works.
func (c *Controller) Start(ctx context.Context, name, dir, cmd string, cols, rows int) (Session, error) {
	if err := c.Available(); err != nil {
		return Session{}, err
	}
	if cmd == "" {
		return Session{}, errors.New("start: command is required")
	}
	if cols <= 0 {
		cols = DefaultColumns
	}
	if rows <= 0 {
		rows = DefaultRows
	}

	id, existing := sessionName(name), false
	if id != "" {
		exists, err := c.Exists(ctx, id)
		if err != nil {
			return Session{}, err
		}
		if exists {
			existing = true
		}
	}
	if id == "" {
		fresh, err := newID()
		if err != nil {
			return Session{}, err
		}
		id = fresh
	}

	if !existing {
		live, err := c.List(ctx)
		if err != nil {
			return Session{}, err
		}
		if len(live) >= MaxTerminals {
			return Session{}, fmt.Errorf("too many terminals (%d max); use terminal_kill to clean up", MaxTerminals)
		}

		if dir == "" {
			dir = "/"
		}
		if _, err := c.run(ctx, "new-session", "-d", "-s", id, "-c", dir, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows), cmd); err != nil {
			return Session{}, err
		}
	}

	// Give a freshly spawned session a moment to fail fast (invalid
	// command, immediate crash), so agents learn right away instead of
	// polling a dead terminal.
	if !existing {
		select {
		case <-ctx.Done():
			return Session{}, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
		exists, err := c.Exists(ctx, id)
		if err != nil {
			return Session{}, err
		}
		if !exists {
			return Session{}, fmt.Errorf("terminal exited immediately after starting %q", cmd)
		}
	}

	return Session{ID: id, Cols: cols, Rows: rows, Existing: existing}, nil
}

// isNoServer reports whether tmux failed because the dedicated server
// has not been started yet (or has exited).
func isNoServer(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "no server running") || strings.Contains(err.Error(), "error connecting"))
}

// Exists reports whether the terminal is alive.
func (c *Controller) Exists(ctx context.Context, id string) (bool, error) {
	if !validID(id) {
		return false, fmt.Errorf("invalid terminal ID: %q", id)
	}
	_, err := c.run(ctx, "has-session", "-t", id)
	if err == nil {
		return true, nil
	}
	if isNoServer(err) || strings.Contains(err.Error(), "can't find session") {
		return false, nil
	}
	return false, err
}

// SendKeys sends literal text followed by any named control keys to the
// terminal. text is sent verbatim with key-name lookup disabled; keys
// must be tmux key names (Enter, Escape, Tab, BSpace, C-c, ...). enter
// can be used instead of listing "Enter" in keys.
func (c *Controller) SendKeys(ctx context.Context, id, text string, keys []string, enter bool) error {
	if !validID(id) {
		return fmt.Errorf("invalid terminal ID: %q", id)
	}
	if text != "" {
		if _, err := c.run(ctx, "send-keys", "-t", id, "-l", text); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(keys)+1)
	names = append(names, keys...)
	if enter {
		names = append(names, "Enter")
	}
	if len(names) > 0 {
		args := []string{"send-keys", "-t", id}
		args = append(args, names...)
		if _, err := c.run(ctx, args...); err != nil {
			return err
		}
	}
	return nil
}

// Capture returns the terminal's screen content with escape sequences
// stripped. When history is true the full scrollback is included instead
// of only the current visible pane.
func (c *Controller) Capture(ctx context.Context, id string, history bool) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("invalid terminal ID: %q", id)
	}
	args := []string{"capture-pane", "-p", "-t", id}
	if history {
		args = append(args, "-S", "-")
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		if strings.Contains(err.Error(), "can't find") {
			return "", ErrNotFound
		}
		return "", err
	}
	return ansi.Strip(out), nil
}

// WaitFor polls the terminal until match appears in its output, then
// returns the full history capture. It returns the last capture (and
// false) when the wait deadline passes without a match. A zero wait
// deadline derives from the context deadline, or defaults to 10 seconds.
func (c *Controller) WaitFor(ctx context.Context, id, match string, wait time.Duration) (string, bool, error) {
	if match == "" {
		return "", false, errors.New("wait_for: match is required")
	}
	if wait <= 0 {
		wait = 30 * time.Second
	}
	if wait > maxCaptureWait {
		wait = maxCaptureWait
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var last string
	for {
		screen, err := c.Capture(deadlineCtx, id, true)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return "", false, ErrNotFound
			}
			if errors.Is(deadlineCtx.Err(), context.DeadlineExceeded) {
				return last, false, nil
			}
			return "", false, err
		}
		last = screen
		if strings.Contains(last, match) {
			return last, true, nil
		}
		select {
		case <-deadlineCtx.Done():
			return last, false, nil
		case <-ticker.C:
		}
	}
}

// Resize changes the pane dimensions the programs inside the terminal
// see.
func (c *Controller) Resize(ctx context.Context, id string, cols, rows int) error {
	if !validID(id) {
		return fmt.Errorf("invalid terminal ID: %q", id)
	}
	if cols <= 0 || rows <= 0 {
		return errors.New("resize: cols and rows must be positive")
	}
	_, err := c.run(ctx, "resize-window", "-t", id, "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows))
	return err
}

// Kill terminates a single terminal session. May be called on sessions
// that have already exited.
func (c *Controller) Kill(ctx context.Context, id string) error {
	if !validID(id) {
		return fmt.Errorf("invalid terminal ID: %q", id)
	}
	_, err := c.run(ctx, "kill-session", "-t", id)
	if err != nil && (strings.Contains(err.Error(), "can't find session") || isNoServer(err)) {
		return ErrNotFound
	}
	return err
}

// KillAll stops the tmux server, terminating every Crush terminal. Only
// the dedicated socket is affected.
func (c *Controller) KillAll(ctx context.Context) error {
	_, err := c.run(ctx, "kill-server")
	return err
}

// List returns all live Crush terminal sessions, ordered by tmux.
// A not-yet-running tmux server yields an empty list, not an error.
func (c *Controller) List(ctx context.Context) ([]Session, error) {
	out, err := c.run(ctx, "list-sessions", "-F", "#{session_name}\t#{window_width}\t#{window_height}\t#{pane_current_command}\t#{pane_pid}")
	if err != nil {
		if isNoServer(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []Session
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			continue
		}
		if !validID(fields[0]) {
			continue
		}
		cols, _ := strconv.Atoi(fields[1])
		rows, _ := strconv.Atoi(fields[2])
		pid, _ := strconv.Atoi(fields[4])
		sessions = append(sessions, Session{
			ID:      fields[0],
			Cols:    cols,
			Rows:    rows,
			Command: fields[3],
			PID:     pid,
		})
	}
	return sessions, nil
}
