package proto

type UserQuestionChoice struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type UserQuestionRequest struct {
	ID          string               `json:"id"`
	SessionID   string               `json:"session_id"`
	ToolCallID  string               `json:"tool_call_id"`
	Question    string               `json:"question"`
	Description string               `json:"description,omitempty"`
	Choices     []UserQuestionChoice `json:"choices"`
}

type UserQuestionResponse struct {
	RequestID   string `json:"request_id"`
	ChoiceID    string `json:"choice_id,omitempty"`
	ChoiceLabel string `json:"choice_label,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Dismissed   bool   `json:"dismissed,omitempty"`
}
