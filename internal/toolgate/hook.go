package toolgate

import "encoding/json"

// HookInput is the JSON Claude Code sends to PreToolUse hooks.
type HookInput struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	CWD            string          `json:"cwd"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
}

// HookOutput is the JSON returned to Claude Code.
type HookOutput struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type HookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// NewHookOutput builds a PreToolUse response.
func NewHookOutput(decision, reason, context string) HookOutput {
	return HookOutput{
		HookSpecificOutput: &HookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       decision,
			PermissionDecisionReason: reason,
			AdditionalContext:        context,
		},
	}
}
