package tools

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/charmbracelet/crush/internal/shell"
)

type BashParams struct {
	Description         string `json:"description" description:"A brief description of what the command does, try to keep it under 30 characters or so"`
	Command             string `json:"command" description:"The command to execute"`
	WorkingDir          string `json:"working_dir,omitempty" description:"The working directory to execute the command in (defaults to current directory)"`
	RunInBackground     bool   `json:"run_in_background,omitempty" description:"Set to true (boolean) to run this command in the background. Use job_output to read the output later."`
	AutoBackgroundAfter int    `json:"auto_background_after,omitempty" description:"Seconds to wait before automatically moving the command to a background job (default: 60)"`
}

type BashPermissionsParams struct {
	Description         string `json:"description"`
	Command             string `json:"command"`
	WorkingDir          string `json:"working_dir"`
	RunInBackground     bool   `json:"run_in_background"`
	AutoBackgroundAfter int    `json:"auto_background_after"`
}

type BashResponseMetadata struct {
	StartTime        int64  `json:"start_time"`
	EndTime          int64  `json:"end_time"`
	Output           string `json:"output"`
	Description      string `json:"description"`
	WorkingDirectory string `json:"working_directory"`
	Background       bool   `json:"background,omitempty"`
	ShellID          string `json:"shell_id,omitempty"`
}

const (
	BashToolName = "bash"

	DefaultAutoBackgroundAfter = 60 // Commands taking longer automatically become background jobs
	MaxOutputLength            = 30000
	BashNoOutput               = "no output"

	commandOutputPublishInterval = 250 * time.Millisecond
)

//go:embed bash.tpl
var bashDescriptionTmpl []byte

var bashDescriptionTpl = template.Must(
	template.New("bashDescription").
		Parse(string(bashDescriptionTmpl)),
)

type bashDescriptionData struct {
	BannedCommands  string
	MaxOutputLength int
	Attribution     config.Attribution
	ModelName       string
}

var bannedCommands = []string{
	// Network/Download tools
	"alias",
	"aria2c",
	"axel",
	"chrome",
	"curl",
	"curlie",
	"firefox",
	"http-prompt",
	"httpie",
	"links",
	"lynx",
	"nc",
	"safari",
	"scp",
	"ssh",
	"telnet",
	"w3m",
	"wget",
	"xh",

	// System administration
	"doas",
	"su",
	"sudo",

	// Package managers
	"apk",
	"apt",
	"apt-cache",
	"apt-get",
	"dnf",
	"dpkg",
	"emerge",
	"home-manager",
	"makepkg",
	"opkg",
	"pacman",
	"paru",
	"pkg",
	"pkg_add",
	"pkg_delete",
	"portage",
	"rpm",
	"yay",
	"yum",
	"zypper",

	// System modification
	"at",
	"batch",
	"chkconfig",
	"crontab",
	"fdisk",
	"mkfs",
	"mount",
	"parted",
	"service",
	"systemctl",
	"umount",

	// Network configuration
	"firewall-cmd",
	"ifconfig",
	"ip",
	"iptables",
	"netstat",
	"pfctl",
	"route",
	"ufw",
}

func bashDescription(attribution *config.Attribution, modelName string) string {
	bannedCommandsStr := strings.Join(bannedCommands, ", ")
	var out bytes.Buffer
	if err := bashDescriptionTpl.Execute(&out, bashDescriptionData{
		BannedCommands:  bannedCommandsStr,
		MaxOutputLength: MaxOutputLength,
		Attribution:     *attribution,
		ModelName:       modelName,
	}); err != nil {
		// this should never happen.
		panic("failed to execute bash description template: " + err.Error())
	}
	return out.String()
}

func blockFuncs() []shell.BlockFunc {
	return []shell.BlockFunc{
		shell.CommandsBlocker(bannedCommands),

		// System package managers
		shell.ArgumentsBlocker("apk", []string{"add"}, nil),
		shell.ArgumentsBlocker("apt", []string{"install"}, nil),
		shell.ArgumentsBlocker("apt-get", []string{"install"}, nil),
		shell.ArgumentsBlocker("dnf", []string{"install"}, nil),
		shell.ArgumentsBlocker("pacman", nil, []string{"-S"}),
		shell.ArgumentsBlocker("pkg", []string{"install"}, nil),
		shell.ArgumentsBlocker("yum", []string{"install"}, nil),
		shell.ArgumentsBlocker("zypper", []string{"install"}, nil),

		// Language-specific package managers
		shell.ArgumentsBlocker("brew", []string{"install"}, nil),
		shell.ArgumentsBlocker("cargo", []string{"install"}, nil),
		shell.ArgumentsBlocker("gem", []string{"install"}, nil),
		shell.ArgumentsBlocker("go", []string{"install"}, nil),
		shell.ArgumentsBlocker("npm", []string{"install"}, []string{"--global"}),
		shell.ArgumentsBlocker("npm", []string{"install"}, []string{"-g"}),
		shell.ArgumentsBlocker("pip", []string{"install"}, []string{"--user"}),
		shell.ArgumentsBlocker("pip3", []string{"install"}, []string{"--user"}),
		shell.ArgumentsBlocker("pnpm", []string{"add"}, []string{"--global"}),
		shell.ArgumentsBlocker("pnpm", []string{"add"}, []string{"-g"}),
		shell.ArgumentsBlocker("yarn", []string{"global", "add"}, nil),

		// `go test -exec` can run arbitrary commands
		shell.ArgumentsBlocker("go", []string{"test"}, []string{"-exec"}),
	}
}

type commandOutputStreamer struct {
	mu        sync.Mutex
	publisher pubsub.Publisher[CommandOutputEvent]
	base      CommandOutputEvent
	last      CommandOutputEvent
	hasLast   bool
}

func newCommandOutputStreamer(
	ctx context.Context,
	publisher pubsub.Publisher[CommandOutputEvent],
	call fantasy.ToolCall,
	params BashParams,
	shellID string,
	workingDir string,
	startTime time.Time,
) *commandOutputStreamer {
	if publisher == nil {
		return nil
	}
	sessionID := GetSessionFromContext(ctx)
	messageID := GetMessageFromContext(ctx)
	if sessionID == "" || messageID == "" || call.ID == "" {
		return nil
	}
	return &commandOutputStreamer{
		publisher: publisher,
		base: CommandOutputEvent{
			SessionID:        sessionID,
			MessageID:        messageID,
			ToolCallID:       call.ID,
			ShellID:          shellID,
			Command:          params.Command,
			Description:      params.Description,
			WorkingDirectory: workingDir,
			StartTime:        startTime.UnixMilli(),
		},
	}
}

func (s *commandOutputStreamer) publish(eventType pubsub.EventType, stdout, stderr string, done bool, execErr error, background bool, force bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	output := formatOutputSnapshot(stdout, stderr, execErr, done)
	event := s.base
	event.Output = output
	event.Background = background
	event.Done = done
	event.ExitCode = shell.ExitCode(execErr)
	event.UpdatedAt = now.UnixMilli()
	if done {
		event.EndTime = now.UnixMilli()
	}
	if execErr != nil {
		event.Error = execErr.Error()
	}

	if !force && s.hasLast &&
		s.last.Output == event.Output &&
		s.last.Background == event.Background &&
		s.last.Done == event.Done &&
		s.last.ExitCode == event.ExitCode &&
		s.last.Error == event.Error {
		return
	}

	s.last = event
	s.hasLast = true
	s.publisher.Publish(eventType, event)
}

func (s *commandOutputStreamer) publishFromShell(eventType pubsub.EventType, bgShell *shell.BackgroundShell, background bool, force bool) {
	if s == nil {
		return
	}
	stdout, stderr, done, execErr := bgShell.GetOutput()
	s.publish(eventType, stdout, stderr, done, execErr, background, force || done)
}

func (s *commandOutputStreamer) monitor(bgShell *shell.BackgroundShell, background bool) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(commandOutputPublishInterval)
	defer ticker.Stop()
	for {
		s.publishFromShell(pubsub.UpdatedEvent, bgShell, background, false)
		_, _, done, _ := bgShell.GetOutput()
		if done {
			return
		}
		<-ticker.C
	}
}

func formatOutputSnapshot(stdout, stderr string, execErr error, done bool) string {
	if done {
		return formatOutput(stdout, stderr, execErr)
	}
	stdout = truncateOutput(stdout)
	stderr = truncateOutput(stderr)
	if stdout != "" && stderr != "" {
		return stdout + "\n" + stderr
	}
	return stdout + stderr
}

func NewBashTool(
	permissions permission.Service,
	output pubsub.Publisher[CommandOutputEvent],
	workingDir string,
	attribution *config.Attribution,
	modelName string,
) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		BashToolName,
		string(bashDescription(attribution, modelName)),
		func(ctx context.Context, params BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Command == "" {
				return fantasy.NewTextErrorResponse("missing command"), nil
			}

			// Determine working directory
			execWorkingDir := cmp.Or(params.WorkingDir, workingDir)

			isSafeReadOnly := false
			cmdLower := strings.ToLower(params.Command)

			for _, safe := range safeCommands {
				if strings.HasPrefix(cmdLower, safe) {
					if len(cmdLower) == len(safe) || cmdLower[len(safe)] == ' ' || cmdLower[len(safe)] == '-' {
						isSafeReadOnly = true
						break
					}
				}
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for executing shell command")
			}
			if !isSafeReadOnly {
				p, err := permissions.Request(ctx,
					permission.CreatePermissionRequest{
						SessionID:   sessionID,
						Path:        execWorkingDir,
						ToolCallID:  call.ID,
						ToolName:    BashToolName,
						Action:      "execute",
						Description: fmt.Sprintf("Execute command: %s", params.Command),
						Params:      BashPermissionsParams(params),
					},
				)
				if err != nil {
					return fantasy.ToolResponse{}, err
				}
				if !p {
					return fantasy.ToolResponse{}, permission.ErrorPermissionDenied
				}
			}

			// If explicitly requested as background, start immediately with detached context
			if params.RunInBackground {
				startTime := time.Now()
				bgManager := shell.GetBackgroundShellManager()
				bgManager.Cleanup()
				var streamerRef atomic.Pointer[commandOutputStreamer]
				var outputBackground atomic.Bool
				outputBackground.Store(true)
				// Use background context so it continues after tool returns
				bgShell, err := bgManager.StartWithOutputCallback(context.Background(), execWorkingDir, blockFuncs(), params.Command, params.Description, func(bgShell *shell.BackgroundShell) {
					streamer := streamerRef.Load()
					if streamer == nil {
						return
					}
					streamer.publishFromShell(pubsub.UpdatedEvent, bgShell, outputBackground.Load(), false)
				})
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("error starting background shell: %w", err)
				}
				streamer := newCommandOutputStreamer(ctx, output, call, params, bgShell.ID, bgShell.WorkingDir, startTime)
				streamer.publish(pubsub.CreatedEvent, "", "", false, nil, true, true)
				if streamer != nil {
					streamerRef.Store(streamer)
				}

				// Wait a short time to detect fast failures (blocked commands, syntax errors, etc.)
				fastFailureTicker := time.NewTicker(100 * time.Millisecond)
				fastFailureTimeout := time.After(1 * time.Second)
				var stdout, stderr string
				var done bool
				var execErr error

			fastFailureLoop:
				for {
					select {
					case <-fastFailureTicker.C:
						stdout, stderr, done, execErr = bgShell.GetOutput()
						streamer.publish(pubsub.UpdatedEvent, stdout, stderr, done, execErr, true, done)
						if done {
							break fastFailureLoop
						}
					case <-fastFailureTimeout:
						stdout, stderr, done, execErr = bgShell.GetOutput()
						streamer.publish(pubsub.UpdatedEvent, stdout, stderr, done, execErr, true, done)
						break fastFailureLoop
					case <-ctx.Done():
						fastFailureTicker.Stop()
						bgManager.Kill(bgShell.ID)
						return fantasy.ToolResponse{}, ctx.Err()
					}
				}
				fastFailureTicker.Stop()

				if done {
					// Command failed or completed very quickly
					bgManager.Remove(bgShell.ID)

					interrupted := shell.IsInterrupt(execErr)
					exitCode := shell.ExitCode(execErr)
					if exitCode == 0 && !interrupted && execErr != nil {
						return fantasy.ToolResponse{}, fmt.Errorf("[Job %s] error executing command: %w", bgShell.ID, execErr)
					}

					stdout = formatOutput(stdout, stderr, execErr)

					metadata := BashResponseMetadata{
						StartTime:        startTime.UnixMilli(),
						EndTime:          time.Now().UnixMilli(),
						Output:           stdout,
						Description:      params.Description,
						Background:       params.RunInBackground,
						WorkingDirectory: bgShell.WorkingDir,
					}
					if stdout == "" {
						return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
					}
					stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
				}

				// Still running after fast-failure check - return as background job
				go streamer.monitor(bgShell, true)
				metadata := BashResponseMetadata{
					StartTime:        startTime.UnixMilli(),
					EndTime:          time.Now().UnixMilli(),
					Description:      params.Description,
					WorkingDirectory: bgShell.WorkingDir,
					Background:       true,
					ShellID:          bgShell.ID,
				}
				response := fmt.Sprintf("Background shell started with ID: %s\n\nUse job_output tool to view output or job_kill to terminate.", bgShell.ID)
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
			}

			// Start synchronous execution with auto-background support
			startTime := time.Now()

			// Start with detached context so it can survive if moved to background
			bgManager := shell.GetBackgroundShellManager()
			bgManager.Cleanup()
			var streamerRef atomic.Pointer[commandOutputStreamer]
			var outputBackground atomic.Bool
			bgShell, err := bgManager.StartWithOutputCallback(context.Background(), execWorkingDir, blockFuncs(), params.Command, params.Description, func(bgShell *shell.BackgroundShell) {
				streamer := streamerRef.Load()
				if streamer == nil {
					return
				}
				streamer.publishFromShell(pubsub.UpdatedEvent, bgShell, outputBackground.Load(), false)
			})
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("error starting shell: %w", err)
			}
			streamer := newCommandOutputStreamer(ctx, output, call, params, bgShell.ID, bgShell.WorkingDir, startTime)
			streamer.publish(pubsub.CreatedEvent, "", "", false, nil, false, true)
			if streamer != nil {
				streamerRef.Store(streamer)
			}

			// Wait for either completion, auto-background threshold, or context cancellation
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			autoBackgroundAfter := cmp.Or(params.AutoBackgroundAfter, DefaultAutoBackgroundAfter)
			autoBackgroundThreshold := time.Duration(autoBackgroundAfter) * time.Second
			timeout := time.After(autoBackgroundThreshold)

			var stdout, stderr string
			var done bool
			var execErr error

		waitLoop:
			for {
				select {
				case <-ticker.C:
					stdout, stderr, done, execErr = bgShell.GetOutput()
					streamer.publish(pubsub.UpdatedEvent, stdout, stderr, done, execErr, false, done)
					if done {
						break waitLoop
					}
				case <-timeout:
					stdout, stderr, done, execErr = bgShell.GetOutput()
					outputBackground.Store(true)
					streamer.publish(pubsub.UpdatedEvent, stdout, stderr, done, execErr, true, true)
					break waitLoop
				case <-ctx.Done():
					// Incoming context was cancelled before we moved to background
					// Kill the shell and return error
					bgManager.Kill(bgShell.ID)
					stdout, stderr, done, execErr = bgShell.GetOutput()
					streamer.publish(pubsub.UpdatedEvent, stdout, stderr, true, ctx.Err(), false, true)
					return fantasy.ToolResponse{}, ctx.Err()
				}
			}

			if done {
				// Command completed within threshold - return synchronously
				// Remove from background manager since we're returning directly
				// Don't call Kill() as it cancels the context and corrupts the exit code
				bgManager.Remove(bgShell.ID)

				interrupted := shell.IsInterrupt(execErr)
				exitCode := shell.ExitCode(execErr)
				if exitCode == 0 && !interrupted && execErr != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("[Job %s] error executing command: %w", bgShell.ID, execErr)
				}

				stdout = formatOutput(stdout, stderr, execErr)

				metadata := BashResponseMetadata{
					StartTime:        startTime.UnixMilli(),
					EndTime:          time.Now().UnixMilli(),
					Output:           stdout,
					Description:      params.Description,
					Background:       params.RunInBackground,
					WorkingDirectory: bgShell.WorkingDir,
				}
				if stdout == "" {
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse(BashNoOutput), metadata), nil
				}
				stdout += fmt.Sprintf("\n\n<cwd>%s</cwd>", normalizeWorkingDir(bgShell.WorkingDir))
				return fantasy.WithResponseMetadata(fantasy.NewTextResponse(stdout), metadata), nil
			}

			// Still running - keep as background job
			go streamer.monitor(bgShell, true)
			metadata := BashResponseMetadata{
				StartTime:        startTime.UnixMilli(),
				EndTime:          time.Now().UnixMilli(),
				Description:      params.Description,
				WorkingDirectory: bgShell.WorkingDir,
				Background:       true,
				ShellID:          bgShell.ID,
			}
			response := fmt.Sprintf("Command is taking longer than expected and has been moved to background.\n\nBackground shell ID: %s\n\nUse job_output tool to view output or job_kill to terminate.", bgShell.ID)
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(response), metadata), nil
		})
}

// formatOutput formats the output of a completed command with error handling
func formatOutput(stdout, stderr string, execErr error) string {
	interrupted := shell.IsInterrupt(execErr)
	exitCode := shell.ExitCode(execErr)

	stdout = truncateOutput(stdout)
	stderr = truncateOutput(stderr)

	errorMessage := stderr
	if errorMessage == "" && execErr != nil {
		errorMessage = execErr.Error()
	}

	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}

	return stdout
}

func truncateOutput(content string) string {
	if len(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]

	truncatedLinesCount := countLines(content[halfLength : len(content)-halfLength])
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLinesCount, end)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

func normalizeWorkingDir(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, fsext.WindowsWorkingDirDrive(), "")
	}
	return filepath.ToSlash(path)
}
