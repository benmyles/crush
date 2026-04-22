package proto

type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form"`
}

// Session represents a session in the proto layer.
type Session struct {
	ID               string  `json:"id"`
	ParentSessionID  string  `json:"parent_session_id"`
	Title            string  `json:"title"`
	MessageCount     int64   `json:"message_count"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	SummaryMessageID string  `json:"summary_message_id"`
	Cost             float64 `json:"cost"`
	Todos            []Todo  `json:"todos,omitempty"`
	CreatedAt        int64   `json:"created_at"`
	UpdatedAt        int64   `json:"updated_at"`
}

// ForkSessionRequest represents a request to fork a session at a message.
type ForkSessionRequest struct {
	MessageID string `json:"message_id"`
}

// ForkSessionResponse contains the forked session and any prompt prefill.
type ForkSessionResponse struct {
	Session Session `json:"session"`
	Prefill string  `json:"prefill,omitempty"`
}
