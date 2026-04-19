package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lizhongxuan/aiops-codex/internal/bifrost"
)

// RegisterRequestUserInputTool registers the request_user_input tool.
func RegisterRequestUserInputTool(reg *ToolRegistry) {
	reg.Register(ToolEntry{
		Name:        "request_user_input",
		Description: "Request structured multi-question input from the user with support for different field types (text, secret, selection, id).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"questions": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id":         map[string]interface{}{"type": "string", "description": "Unique identifier for this question."},
							"text":       map[string]interface{}{"type": "string", "description": "The question text to display."},
							"field_type": map[string]interface{}{"type": "string", "enum": []string{"text", "secret", "selection", "id"}, "description": "Type of input field."},
							"options":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Options for selection field type."},
							"required":   map[string]interface{}{"type": "boolean", "description": "Whether this field is required."},
							"default":    map[string]interface{}{"type": "string", "description": "Default value for the field."},
						},
						"required": []string{"id", "text", "field_type"},
					},
					"minItems":    1,
					"description": "List of questions to present to the user.",
				},
				"title":       map[string]interface{}{"type": "string", "description": "Optional title for the input form."},
				"description": map[string]interface{}{"type": "string", "description": "Optional description for the input form."},
			},
			"required":             []string{"questions"},
			"additionalProperties": false,
		},
		Handler: handleRequestUserInput,
	})
}

// UserInputQuestion represents a single question in a user input request.
type UserInputQuestion struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	FieldType string   `json:"field_type"`
	Options   []string `json:"options,omitempty"`
	Required  bool     `json:"required,omitempty"`
	Default   string   `json:"default,omitempty"`
}

// UserInputRequest represents the full user input request.
type UserInputRequest struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Questions   []UserInputQuestion `json:"questions"`
}

func handleRequestUserInput(ctx context.Context, tc ToolContext, call bifrost.ToolCall, args map[string]interface{}) (string, error) {
	questionsRaw, ok := args["questions"]
	if !ok {
		return "", fmt.Errorf("request_user_input requires 'questions' argument")
	}

	data, err := json.Marshal(questionsRaw)
	if err != nil {
		return "", fmt.Errorf("request_user_input: invalid questions: %w", err)
	}

	var questions []UserInputQuestion
	if err := json.Unmarshal(data, &questions); err != nil {
		return "", fmt.Errorf("request_user_input: invalid questions format: %w", err)
	}

	if len(questions) == 0 {
		return "", fmt.Errorf("request_user_input: at least one question is required")
	}

	req := UserInputRequest{Questions: questions}
	if title, ok := args["title"].(string); ok {
		req.Title = title
	}
	if desc, ok := args["description"].(string); ok {
		req.Description = desc
	}

	out, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("request_user_input: %w", err)
	}

	return string(out), nil
}
