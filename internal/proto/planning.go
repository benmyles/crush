package proto

type PlanSubmission struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	ToolCallID string `json:"tool_call_id"`
	Markdown   string `json:"markdown"`
	Todos      []Todo `json:"todos"`
}

type PlanResponse struct {
	SubmissionID   string `json:"submission_id"`
	Approved       bool   `json:"approved"`
	Comment        string `json:"comment,omitempty"`
	CompactHistory bool   `json:"compact_history,omitempty"`
}

type PlanMode string

const (
	PlanModeEnter PlanMode = "enter"
	PlanModeExit  PlanMode = "exit"
)

type PlanModeChangeRequest struct {
	ID         string   `json:"id"`
	SessionID  string   `json:"session_id"`
	ToolCallID string   `json:"tool_call_id"`
	Mode       PlanMode `json:"mode"`
	Prompt     string   `json:"prompt,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}
