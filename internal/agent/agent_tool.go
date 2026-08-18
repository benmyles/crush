package agent

import (
	"context"
	_ "embed"
	"errors"
	"strings"

	"charm.land/fantasy"

	"github.com/charmbracelet/crush/internal/agent/prompt"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/config"
)

//go:embed templates/agent_tool.md
var agentToolDescription string

type AgentParams struct {
	Prompt string `json:"prompt" description:"The task for the agent to perform"`
	// DelegatedScope describes the specific slice of work being handed off to
	// the sub-agent. Required when a sub-agent spawns a further sub-agent, to
	// enforce the scope-reduction invariant (no infinite delegation).
	DelegatedScope string `json:"delegated_scope,omitempty" description:"The specific slice of work being handed off (required for nested delegation)"`
	// KeptWork describes the work the caller retains for itself. Required when
	// a sub-agent spawns a further sub-agent; if the caller would delegate its
	// entire responsibility, the call is rejected.
	KeptWork string `json:"kept_work,omitempty" description:"The work the caller retains for itself (required for nested delegation)"`
}

const (
	AgentToolName = "agent"
)

func (c *coordinator) agentTool(ctx context.Context) (fantasy.AgentTool, error) {
	agentCfg, ok := c.cfg.Config().Agents[config.AgentTask]
	if !ok {
		return nil, errors.New("task agent not configured")
	}
	prompt, err := taskPrompt(prompt.WithWorkingDir(c.cfg.WorkingDir()))
	if err != nil {
		return nil, err
	}

	agent, err := c.buildAgent(ctx, prompt, agentCfg, true)
	if err != nil {
		return nil, err
	}
	return fantasy.NewParallelAgentTool(
		AgentToolName,
		agentToolDescription,
		func(ctx context.Context, params AgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.Prompt == "" {
				return fantasy.NewTextErrorResponse("prompt is required"), nil
			}

			sessionID := tools.GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, errors.New("session id missing from context")
			}

			// Scope-reduction invariant (from LCM): when a sub-agent spawns a
			// further sub-agent, it must declare what it is delegating and what
			// it is keeping. If it would delegate its entire responsibility
			// (empty kept_work), reject the call and instruct it to do the work
			// directly. This gives well-founded recursion without a depth limit.
			if c.sessions.IsAgentToolSession(sessionID) {
				if strings.TrimSpace(params.DelegatedScope) == "" || strings.TrimSpace(params.KeptWork) == "" {
					return fantasy.NewTextErrorResponse("You are a sub-agent spawning another sub-agent. You must provide both delegated_scope (the specific slice of work you are handing off) and kept_work (the work you will still perform yourself). If you would delegate your entire responsibility, perform the work directly instead."), nil
				}
			}

			agentMessageID := tools.GetMessageFromContext(ctx)
			if agentMessageID == "" {
				return fantasy.ToolResponse{}, errors.New("agent message id missing from context")
			}

			return c.runSubAgent(ctx, subAgentParams{
				Agent:          agent,
				SessionID:      sessionID,
				AgentMessageID: agentMessageID,
				ToolCallID:     call.ID,
				Prompt:         params.Prompt,
				SessionTitle:   "New Agent Session",
			})
		},
	), nil
}
