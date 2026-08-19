package tools

import (
	"cmp"
	"context"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/shell"
)

const (
	JobOutputToolName = "job_output"

	jobOutputDefaultWaitMS = 30000
	jobOutputMaxWaitMS     = 30000
)

//go:embed job_output.md
var jobOutputDescription string

type JobOutputParams struct {
	ShellID   string `json:"shell_id" description:"The shell to retrieve output from"`
	Wait      bool   `json:"wait,omitempty" description:"If true, block until the shell completes OR timeout_ms elapses, then return the output collected so far (default false)."`
	TimeoutMs int    `json:"timeout_ms,omitempty" description:"How long to wait in milliseconds when wait=true before returning whatever output was collected (default 30000, max 30000)."`
}

type JobOutputResponseMetadata struct {
	ShellID          string `json:"shell_id"`
	Command          string `json:"command"`
	Description      string `json:"description"`
	Done             bool   `json:"done"`
	WorkingDirectory string `json:"working_directory"`
}

func NewJobOutputTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		JobOutputToolName,
		jobOutputDescription,
		func(ctx context.Context, params JobOutputParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.ShellID == "" {
				return fantasy.NewTextErrorResponse("missing shell_id"), nil
			}

			bgManager := shell.GetBackgroundShellManager()
			bgShell, ok := bgManager.Get(params.ShellID)
			if !ok {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
			}

			waitLog := ""
			if params.Wait {
				timeoutMS := cmp.Or(params.TimeoutMs, jobOutputDefaultWaitMS)
				if timeoutMS < 1 || timeoutMS > jobOutputMaxWaitMS {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("timeout_ms must be between 1 and %d (got %d)", jobOutputMaxWaitMS, timeoutMS)), nil
				}
				waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
				bgShell.WaitContext(waitCtx)
				cancel()
				waitLog = fmt.Sprintf(" (waited up to %dms)", timeoutMS)
			}

			stdout, stderr, done, err := bgShell.GetOutput()

			var outputParts []string
			if stdout != "" {
				outputParts = append(outputParts, stdout)
			}
			if stderr != "" {
				outputParts = append(outputParts, stderr)
			}

			status := "running"
			if done {
				status = "completed"
				if err != nil {
					exitCode := shell.ExitCode(err)
					if exitCode != 0 {
						outputParts = append(outputParts, fmt.Sprintf("Exit code %d", exitCode))
					}
				}
			}

			output := strings.Join(outputParts, "\n")
			output = TruncateOutput(output)

			metadata := JobOutputResponseMetadata{
				ShellID:          params.ShellID,
				Command:          bgShell.Command,
				Description:      bgShell.Description,
				Done:             done,
				WorkingDirectory: bgShell.WorkingDir,
			}

			if output == "" {
				output = BashNoOutput
			}

			result := fmt.Sprintf("Status: %s%s\n\n%s", status, waitLog, output)
			if status == "running" && params.Wait {
				result += fmt.Sprintf("\n\nThe shell is still running. The output above is everything captured so far. Call job_output again with wait=true to collect the next %d seconds.", jobOutputMaxWaitMS/1000)
			}
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(result), metadata), nil
		},
	)
}
