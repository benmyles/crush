package dialog

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"charm.land/catwalk/pkg/catwalk"
	"github.com/charmbracelet/crush/internal/commands"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/oauth"
	"github.com/charmbracelet/crush/internal/permission"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/skills"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/util"
)

// ActionClose is a message to close the current dialog.
type ActionClose struct{}

// ActionQuit is a message to quit the application.
type ActionQuit = tea.QuitMsg

// ActionOpenDialog is a message to open a dialog.
type ActionOpenDialog struct {
	DialogID string
}

// ActionSelectSession is a message indicating a session has been selected.
type ActionSelectSession struct {
	Session session.Session
}

// ActionSelectModel is a message indicating a model has been selected.
type ActionSelectModel struct {
	Provider       catwalk.Provider
	Model          config.SelectedModel
	ModelType      config.SelectedModelType
	ReAuthenticate bool
	// FollowLarge clears an optional slot (the compaction model) so it
	// follows the large model again. Provider/Model are empty when set.
	FollowLarge bool
}

// ActionOpenCompactionModel opens the model picker on the compaction slot.
type ActionOpenCompactionModel struct{}

// ActionSetCompactionOption writes one options.compaction field. Key is the
// JSON key (e.g. "reserve_tokens"); Message is shown on success.
type ActionSetCompactionOption struct {
	Key     string
	Value   any
	Message string
}

// ActionResetCompactionOptions removes the whole options.compaction block so
// every setting returns to its default.
type ActionResetCompactionOptions struct{}

// ActionUpdateCompactionModel rewrites the compaction model selection (e.g.
// with a new reasoning effort or thinking flag).
type ActionUpdateCompactionModel struct {
	Model   config.SelectedModel
	Message string
}

// Messages for commands
type (
	ActionNewSession              struct{}
	ActionToggleHelp              struct{}
	ActionToggleCompactMode       struct{}
	ActionToggleThinking          struct{}
	ActionTogglePills             struct{}
	ActionQueueEdit               struct{}
	ActionQueueRemove             struct{}
	ActionExternalEditor          struct{}
	ActionToggleYoloMode          struct{}
	ActionToggleNotifications     struct{}
	ActionSelectNotificationStyle struct {
		Style string
	}
	ActionSelectWebBackend struct {
		Backend string
	}
	ActionToggleTransparentBackground struct{}
	ActionToggleStatusUpdates         struct{}
	ActionToggleSubagents             struct{}
	ActionInitializeProject           struct{}
	ActionSummarize                   struct {
		SessionID string
	}
	// ActionCompact is the engine-only compaction command (/compact).
	ActionCompact struct {
		SessionID string
	}
	// ActionRewind requests a rewind at the selected user message. The
	// UI shows a confirmation dialog before any messages are deleted.
	ActionRewind struct {
		SessionID string
		MessageID string
	}
	// ActionRewindConfirmed is sent by the rewind confirmation dialog
	// when the user approves the truncation.
	ActionRewindConfirmed struct {
		SessionID string
		MessageID string
	}
	// ActionPause latches the global pause fence.
	ActionPause struct{}
	// ActionResume lifts the global pause fence.
	ActionResume struct{}
	// ActionSetGoal sets the session goal and submits the goal text as
	// the next user prompt so the agent starts working toward it right
	// away.
	ActionSetGoal struct {
		SessionID string
		Text      string
	}
	// ActionOpenGoal opens the goal input dialog.
	ActionOpenGoal struct {
		SessionID string
	}
	// ActionGoalShow opens the goal status dialog (fetches state first).
	ActionGoalShow struct {
		SessionID string
	}
	// ActionGoalResume reactivates a blocked or stalled goal.
	ActionGoalResume struct {
		SessionID string
	}
	// ActionGoalClear deletes the session goal.
	ActionGoalClear struct {
		SessionID string
	}
	// ActionSelectReasoningEffort is a message indicating a reasoning effort
	// has been selected.
	ActionSelectReasoningEffort struct {
		Effort string
	}
	ActionPermissionResponse struct {
		Permission permission.PermissionRequest
		Action     PermissionAction
	}
	// ActionRunCustomCommand is a message to run a custom command.
	ActionRunCustomCommand struct {
		Content   string
		Arguments []commands.Argument
		Args      map[string]string // Actual argument values
		Skill     *skills.Skill     // Set when this is a skill command
	}
	// ActionAttachSkill is sent when a skill is selected from the commands
	// dialog to be attached to the conversation as a markdown attachment.
	ActionAttachSkill struct {
		ID   string
		Name string
	}
	// ActionInsertSkillReference is sent when a skill is selected from the
	// commands dialog to insert a skill reference into the composer.
	ActionInsertSkillReference struct {
		Reference string
	}
	// ActionRunMCPPrompt is a message to run a custom command.
	ActionRunMCPPrompt struct {
		Title       string
		Description string
		PromptID    string
		ClientID    string
		Arguments   []commands.Argument
		Args        map[string]string // Actual argument values
	}
	// ActionEnableDockerMCP is a message to enable Docker MCP.
	ActionEnableDockerMCP struct{}
	// ActionDisableDockerMCP is a message to disable Docker MCP.
	ActionDisableDockerMCP struct{}
)

// Messages for MCP OAuth authentication dialog.
type (
	// ActionMCPAuthStarted is sent when the user approves authentication
	// for an MCP server. The UI should initiate the actual auth flow
	// using the provided context, which the dialog will cancel if the
	// user closes it.
	ActionMCPAuthStarted struct {
		Name string
		Ctx  context.Context
	}

	// ActionMCPAuthComplete is sent when MCP authentication succeeds.
	ActionMCPAuthComplete struct {
		Name string
	}

	// ActionMCPAuthErrored is sent when MCP authentication fails.
	ActionMCPAuthErrored struct {
		Name  string
		Error error
	}
)

// Messages for API key input dialog.
type (
	ActionChangeAPIKeyState struct {
		State APIKeyInputState
	}
)

// Messages for OAuth2 device flow dialog.
type (
	// ActionInitiateOAuth is sent when the device auth is initiated
	// successfully.
	ActionInitiateOAuth struct {
		DeviceCode      string
		UserCode        string
		ExpiresIn       int
		VerificationURL string
		Interval        int
	}

	// ActionCompleteOAuth is sent when the device flow completes successfully.
	ActionCompleteOAuth struct {
		Token *oauth.Token
	}

	// ActionOAuthErrored is sent when the device flow encounters an error.
	ActionOAuthErrored struct {
		Error error
	}
)

// ActionCmd represents an action that carries a [tea.Cmd] to be passed to the
// Bubble Tea program loop.
type ActionCmd struct {
	Cmd tea.Cmd
}

// ActionFilePickerSelected is a message indicating a file has been selected in
// the file picker dialog.
type ActionFilePickerSelected struct {
	Path string
}

// Cmd returns a command that reads the file at path and sends a
// [message.Attachement] to the program.
func (a ActionFilePickerSelected) Cmd() tea.Cmd {
	path := a.Path
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		isFileLarge, err := common.IsFileTooBig(path, common.MaxAttachmentSize)
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("unable to read the image: %v", err),
			}
		}
		if isFileLarge {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "file too large, max 5MB",
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("unable to read the image: %v", err),
			}
		}

		mimeBufferSize := min(512, len(content))
		mimeType := http.DetectContentType(content[:mimeBufferSize])
		fileName := filepath.Base(path)

		return message.Attachment{
			FilePath: path,
			FileName: fileName,
			MimeType: mimeType,
			Content:  content,
		}
	}
}
