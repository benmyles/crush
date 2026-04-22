package tools

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/shell"
)

//go:embed wait.md
var waitDescription []byte

const WaitToolName = "wait"

type WaitParams struct {
	Seconds float64 `json:"seconds,omitempty" description:"Number of seconds to wait. With shell_id, this is an optional timeout."`
	ShellID string  `json:"shell_id,omitempty" description:"Background shell ID to wait for completion"`
}

type WaitResponseMetadata struct {
	Seconds  float64 `json:"seconds,omitempty"`
	ShellID  string  `json:"shell_id,omitempty"`
	Done     bool    `json:"done"`
	TimedOut bool    `json:"timed_out,omitempty"`
	ExitCode int     `json:"exit_code,omitempty"`
}

func NewWaitTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		WaitToolName,
		FirstLineDescription(waitDescription),
		func(ctx context.Context, params WaitParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Seconds < 0 {
				return fantasy.NewTextErrorResponse("seconds must be non-negative"), nil
			}
			if params.Seconds > 3600 {
				return fantasy.NewTextErrorResponse("seconds must be 3600 or less"), nil
			}
			if params.ShellID == "" {
				return waitSeconds(ctx, params.Seconds)
			}
			return waitForShell(ctx, params)
		},
	)
}

func waitSeconds(ctx context.Context, seconds float64) (fantasy.ToolResponse, error) {
	if seconds == 0 {
		return fantasy.NewTextErrorResponse("seconds or shell_id is required"), nil
	}
	timer := time.NewTimer(durationFromSeconds(seconds))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return fantasy.ToolResponse{}, ctx.Err()
	case <-timer.C:
	}

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(fmt.Sprintf("Waited %.3g seconds.", seconds)),
		WaitResponseMetadata{Seconds: seconds, Done: true},
	), nil
}

func waitForShell(ctx context.Context, params WaitParams) (fantasy.ToolResponse, error) {
	bgShell, ok := shell.GetBackgroundShellManager().Get(params.ShellID)
	if !ok {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("background shell not found: %s", params.ShellID)), nil
	}

	waitCtx := ctx
	cancel := func() {}
	if params.Seconds > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, durationFromSeconds(params.Seconds))
	}
	defer cancel()

	done := bgShell.WaitContext(waitCtx)
	_, _, isDone, err := bgShell.GetOutput()
	exitCode := shell.ExitCode(err)
	timedOut := !done && params.Seconds > 0 && waitCtx.Err() != nil

	status := "completed"
	if !isDone {
		status = "still running"
	}
	response := fmt.Sprintf("Job %s is %s.", params.ShellID, status)
	if isDone && exitCode != 0 {
		response += fmt.Sprintf(" Exit code %d.", exitCode)
	}
	if timedOut {
		response += " Wait timed out."
	}

	return fantasy.WithResponseMetadata(
		fantasy.NewTextResponse(response),
		WaitResponseMetadata{
			Seconds:  params.Seconds,
			ShellID:  params.ShellID,
			Done:     isDone,
			TimedOut: timedOut,
			ExitCode: exitCode,
		},
	), nil
}

func durationFromSeconds(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}
