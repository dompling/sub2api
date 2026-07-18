package model

import "time"

const (
	PromptActionPrepend = "prepend"
	PromptActionAppend  = "append"

	PromptRoleSystem    = "system"
	PromptRoleUser      = "user"
	PromptRoleAssistant = "assistant"
)

// PromptRule 提示词注入规则
type PromptRule struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	Order       int       `json:"order"`
	Role        string    `json:"role"`
	Content     string    `json:"content"`
	Action      string    `json:"action"`
	GroupIDs    []int64   `json:"group_ids"`
	ModelIDs    []string  `json:"model_ids"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (r *PromptRule) Validate() error {
	if r.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	if r.Content == "" {
		return &ValidationError{Field: "content", Message: "content is required"}
	}
	if r.Role != PromptRoleSystem && r.Role != PromptRoleUser && r.Role != PromptRoleAssistant {
		return &ValidationError{Field: "role", Message: "role must be 'system', 'user', or 'assistant'"}
	}
	if r.Action != PromptActionPrepend && r.Action != PromptActionAppend {
		return &ValidationError{Field: "action", Message: "action must be 'prepend' or 'append'"}
	}
	return nil
}
