package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/csync"
	"github.com/creack/pty"
)

const (
	// MaxBackgroundJobs is the maximum number of concurrent background jobs allowed
	MaxBackgroundJobs = 50
	// CompletedJobRetentionMinutes is how long to keep completed jobs before auto-cleanup (8 hours)
	CompletedJobRetentionMinutes = 8 * 60
)

// syncBuffer is a thread-safe wrapper around bytes.Buffer.
type syncBuffer struct {
	buf     bytes.Buffer
	mu      sync.RWMutex
	onWrite func()
	wroteAt atomic.Int64
}

func (sb *syncBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	n, err = sb.buf.Write(p)
	onWrite := sb.onWrite
	sb.mu.Unlock()
	if n > 0 {
		sb.wroteAt.Store(time.Now().UnixNano())
	}
	if n > 0 && onWrite != nil {
		onWrite()
	}
	return n, err
}

func (sb *syncBuffer) WriteString(s string) (n int, err error) {
	sb.mu.Lock()
	n, err = sb.buf.WriteString(s)
	onWrite := sb.onWrite
	sb.mu.Unlock()
	if n > 0 {
		sb.wroteAt.Store(time.Now().UnixNano())
	}
	if n > 0 && onWrite != nil {
		onWrite()
	}
	return n, err
}

func (sb *syncBuffer) String() string {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.String()
}

func (sb *syncBuffer) Len() int {
	sb.mu.RLock()
	defer sb.mu.RUnlock()
	return sb.buf.Len()
}

func (sb *syncBuffer) LastWriteTime() time.Time {
	ns := sb.wroteAt.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (sb *syncBuffer) setOnWrite(onWrite func()) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.onWrite = onWrite
}

// BackgroundShell represents a shell running in the background.
type BackgroundShell struct {
	ID            string
	Command       string
	Description   string
	Shell         *Shell
	WorkingDir    string
	ctx           context.Context
	cancel        context.CancelFunc
	stdout        *syncBuffer
	stderr        *syncBuffer
	done          chan struct{}
	exitErr       error
	completedAt   atomic.Int64 // Unix timestamp when job completed (0 if still running)
	supportsInput bool
	ptyMaster     *os.File
	ptyMu         sync.Mutex
	closePTYOnce  sync.Once
}

// BackgroundShellManager manages background shell instances.
type BackgroundShellManager struct {
	shells *csync.Map[string, *BackgroundShell]
}

var (
	backgroundManager     *BackgroundShellManager
	backgroundManagerOnce sync.Once
	idCounter             atomic.Uint64
)

// newBackgroundShellManager creates a new BackgroundShellManager instance.
func newBackgroundShellManager() *BackgroundShellManager {
	return &BackgroundShellManager{
		shells: csync.NewMap[string, *BackgroundShell](),
	}
}

// GetBackgroundShellManager returns the singleton background shell manager.
func GetBackgroundShellManager() *BackgroundShellManager {
	backgroundManagerOnce.Do(func() {
		backgroundManager = newBackgroundShellManager()
	})
	return backgroundManager
}

// Start creates and starts a new background shell with the given command.
func (m *BackgroundShellManager) Start(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command string, description string) (*BackgroundShell, error) {
	return m.start(ctx, workingDir, blockFuncs, command, description, nil)
}

// StartWithOutputCallback creates and starts a background shell with a callback
// that fires whenever stdout or stderr receives bytes.
func (m *BackgroundShellManager) StartWithOutputCallback(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command string, description string, onOutput func(*BackgroundShell)) (*BackgroundShell, error) {
	return m.start(ctx, workingDir, blockFuncs, command, description, onOutput)
}

func (m *BackgroundShellManager) start(ctx context.Context, workingDir string, blockFuncs []BlockFunc, command string, description string, onOutput func(*BackgroundShell)) (*BackgroundShell, error) {
	// Check job limit
	if m.shells.Len() >= MaxBackgroundJobs {
		return nil, fmt.Errorf("maximum number of background jobs (%d) reached. Please terminate or wait for some jobs to complete", MaxBackgroundJobs)
	}

	id := fmt.Sprintf("%03X", idCounter.Add(1))

	shell := NewShell(&Options{
		WorkingDir: workingDir,
		BlockFuncs: blockFuncs,
	})

	shellCtx, cancel := context.WithCancel(ctx)

	bgShell := &BackgroundShell{
		ID:          id,
		Command:     command,
		Description: description,
		WorkingDir:  workingDir,
		Shell:       shell,
		ctx:         shellCtx,
		cancel:      cancel,
		stdout:      &syncBuffer{},
		stderr:      &syncBuffer{},
		done:        make(chan struct{}),
	}
	if onOutput != nil {
		notify := func() {
			onOutput(bgShell)
		}
		bgShell.stdout.setOnWrite(notify)
		bgShell.stderr.setOnWrite(notify)
	}

	m.shells.Set(id, bgShell)

	if err := bgShell.startPTY(command); err == nil {
		return bgShell, nil
	}

	go bgShell.runStream(command)

	return bgShell, nil
}

func (bs *BackgroundShell) startPTY(command string) error {
	master, tty, err := pty.Open()
	if err != nil {
		return err
	}

	bs.ptyMaster = master
	bs.supportsInput = true

	go func() {
		defer close(bs.done)
		defer tty.Close() //nolint:errcheck

		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			_, _ = io.Copy(bs.stdout, master)
		}()

		err := bs.Shell.ExecPTY(bs.ctx, command, tty)
		bs.exitErr = err
		bs.completedAt.Store(time.Now().Unix())
		_ = tty.Close()

		select {
		case <-readDone:
		case <-time.After(250 * time.Millisecond):
		}
		bs.closePTY()
	}()

	return nil
}

func (bs *BackgroundShell) runStream(command string) {
	defer close(bs.done)

	err := bs.Shell.ExecStream(bs.ctx, command, bs.stdout, bs.stderr)

	bs.exitErr = err
	bs.completedAt.Store(time.Now().Unix())
}

// Get retrieves a background shell by ID.
func (m *BackgroundShellManager) Get(id string) (*BackgroundShell, bool) {
	return m.shells.Get(id)
}

// Remove removes a background shell from the manager without terminating it.
// This is useful when a shell has already completed and you just want to clean up tracking.
func (m *BackgroundShellManager) Remove(id string) error {
	_, ok := m.shells.Take(id)
	if !ok {
		return fmt.Errorf("background shell not found: %s", id)
	}
	return nil
}

// Kill terminates a background shell by ID.
func (m *BackgroundShellManager) Kill(id string) error {
	shell, ok := m.shells.Take(id)
	if !ok {
		return fmt.Errorf("background shell not found: %s", id)
	}

	shell.cancel()
	shell.closePTY()
	<-shell.done
	return nil
}

// BackgroundShellInfo contains information about a background shell.
type BackgroundShellInfo struct {
	ID          string
	Command     string
	Description string
}

// List returns all background shell IDs.
func (m *BackgroundShellManager) List() []string {
	ids := make([]string, 0, m.shells.Len())
	for id := range m.shells.Seq2() {
		ids = append(ids, id)
	}
	return ids
}

// Cleanup removes completed jobs that have been finished for more than the retention period
func (m *BackgroundShellManager) Cleanup() int {
	now := time.Now().Unix()
	retentionSeconds := int64(CompletedJobRetentionMinutes * 60)

	var toRemove []string
	for shell := range m.shells.Seq() {
		completedAt := shell.completedAt.Load()
		if completedAt > 0 && now-completedAt > retentionSeconds {
			toRemove = append(toRemove, shell.ID)
		}
	}

	for _, id := range toRemove {
		m.Remove(id)
	}

	return len(toRemove)
}

// KillAll terminates all background shells. The provided context bounds how
// long the function waits for each shell to exit.
func (m *BackgroundShellManager) KillAll(ctx context.Context) {
	shells := slices.Collect(m.shells.Seq())
	m.shells.Reset(map[string]*BackgroundShell{})

	var wg sync.WaitGroup
	for _, shell := range shells {
		wg.Go(func() {
			shell.cancel()
			shell.closePTY()
			select {
			case <-shell.done:
			case <-ctx.Done():
			}
		})
	}
	wg.Wait()
}

// GetOutput returns the current output of a background shell.
func (bs *BackgroundShell) GetOutput() (stdout string, stderr string, done bool, err error) {
	select {
	case <-bs.done:
		return bs.stdout.String(), bs.stderr.String(), true, bs.exitErr
	default:
		return bs.stdout.String(), bs.stderr.String(), false, nil
	}
}

// SupportsInput reports whether the background shell accepts terminal input.
func (bs *BackgroundShell) SupportsInput() bool {
	return bs.supportsInput && bs.ptyMaster != nil
}

// HasOutput reports whether the shell has produced stdout or stderr.
func (bs *BackgroundShell) HasOutput() bool {
	return bs.stdout.Len() > 0 || bs.stderr.Len() > 0
}

// LastOutputTime returns the most recent time stdout or stderr received bytes.
func (bs *BackgroundShell) LastOutputTime() time.Time {
	stdoutAt := bs.stdout.LastWriteTime()
	stderrAt := bs.stderr.LastWriteTime()
	if stdoutAt.After(stderrAt) {
		return stdoutAt
	}
	return stderrAt
}

// WriteInput writes terminal input to the running shell.
func (bs *BackgroundShell) WriteInput(input string) error {
	if bs.IsDone() {
		return fmt.Errorf("background shell %s has completed", bs.ID)
	}
	if !bs.SupportsInput() {
		return fmt.Errorf("background shell %s does not support input", bs.ID)
	}

	bs.ptyMu.Lock()
	defer bs.ptyMu.Unlock()
	_, err := io.WriteString(bs.ptyMaster, input)
	if errors.Is(err, os.ErrClosed) {
		return fmt.Errorf("background shell %s has completed", bs.ID)
	}
	return err
}

// Resize resizes the shell terminal when a PTY is available.
func (bs *BackgroundShell) Resize(cols, rows int) error {
	if !bs.SupportsInput() {
		return fmt.Errorf("background shell %s does not support terminal resizing", bs.ID)
	}
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid terminal size %dx%d", cols, rows)
	}
	return pty.Setsize(bs.ptyMaster, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func (bs *BackgroundShell) closePTY() {
	bs.closePTYOnce.Do(func() {
		if bs.ptyMaster != nil {
			_ = bs.ptyMaster.Close()
		}
	})
}

// IsDone checks if the background shell has finished execution.
func (bs *BackgroundShell) IsDone() bool {
	select {
	case <-bs.done:
		return true
	default:
		return false
	}
}

// Wait blocks until the background shell completes.
func (bs *BackgroundShell) Wait() {
	<-bs.done
}

func (bs *BackgroundShell) WaitContext(ctx context.Context) bool {
	select {
	case <-bs.done:
		return true
	case <-ctx.Done():
		return false
	}
}
