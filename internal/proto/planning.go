package proto

type PlanSubmission struct {
	ID         string `json:"id"`
	SessionID  string `json:"session_id"`
	ToolCallID string `json:"tool_call_id"`
	Markdown   string `json:"markdown"`
	Todos      []Todo `json:"todos"`
}
