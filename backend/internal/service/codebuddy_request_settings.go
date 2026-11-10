package service

import (
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codebuddy"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// applyCodeBuddyRequestSettings uses the original ingress body to distinguish
// client preferences from defaults introduced by the generic protocol bridges.
// Only synced/cached metadata is used here: a chat request must not need another
// authenticated config request just to resolve an optional model default.
func (s *OpenAIGatewayService) applyCodeBuddyRequestSettings(body, original []byte, account *Account) []byte {
	metas := parseCodeBuddyModelMeta(account)
	if len(metas) == 0 {
		if cached, ok := codeBuddyMetaCache.Load(account.ID); ok {
			if entry, ok := cached.(codeBuddyMetaCacheEntry); ok && time.Now().Before(entry.exp) {
				metas = entry.metas
			}
		}
	}
	var model *codebuddy.ModelInfo
	modelID := gjson.GetBytes(body, "model").String()
	for i := range metas {
		if metas[i].ID == modelID {
			model = &metas[i]
			break
		}
	}
	if model != nil {
		body, _ = clampCodeBuddyOutputTokens(body, model.MaxOutputTokens)
	}

	effort := ""
	for _, path := range []string{"reasoning_effort", "reasoning.effort", "output_config.effort"} {
		if value := gjson.GetBytes(original, path); value.Type == gjson.String && strings.TrimSpace(value.Str) != "" {
			effort = value.Str
			if path == "output_config.effort" && effort == "max" && (model == nil || model.Reasoning == nil || len(model.Reasoning.SupportedEfforts) == 0) {
				effort = "xhigh"
			}
			break
		}
	}
	if effort == "" {
		_, derived, ok := splitOpenAICompatReasoningModel(gjson.GetBytes(original, "model").String())
		if ok {
			effort = derived
		}
	}
	disabled := gjson.GetBytes(original, "thinking.type").String() == "disabled" || effort == "none"
	if disabled {
		body, _ = sjson.SetBytes(body, "reasoning_effort", "none")
		body, _ = sjson.DeleteBytes(body, "reasoning_summary")
		body, _ = sjson.DeleteBytes(body, "max_thinking_tokens")
		return body
	}
	if effort == "" && model != nil && !model.SupportsReasoning && !OpenAIBodyHasThinkingEnabled(original) && gjson.GetBytes(original, "max_thinking_tokens").Int() <= 0 {
		// The Messages bridge supplies medium even when the client did not ask
		// for thinking. Do not enable it for a non-reasoning product model.
		body, _ = sjson.DeleteBytes(body, "reasoning_effort")
	}
	if effort == "" && model != nil && model.SupportsReasoning && model.Reasoning != nil {
		effort = strings.TrimSpace(model.Reasoning.DefaultEffort)
		if effort == "" {
			effort = strings.TrimSpace(model.Reasoning.Effort)
		}
	}
	if effort != "" {
		if model != nil && model.Reasoning != nil {
			effort = codeBuddySupportedEffort(effort, model.Reasoning.SupportedEfforts)
		}
		body, _ = sjson.SetBytes(body, "reasoning_effort", effort)
	}

	// WorkBuddy's SDK uses max_thinking_tokens; Messages clients express the
	// same explicit budget as thinking.budget_tokens.
	budget := gjson.GetBytes(original, "max_thinking_tokens")
	if !budget.Exists() {
		budget = gjson.GetBytes(original, "thinking.budget_tokens")
	}
	if budget.Type == gjson.Number && budget.Int() > 0 {
		body, _ = sjson.SetBytes(body, "max_thinking_tokens", budget.Int())
	}
	summary := gjson.GetBytes(original, "reasoning_summary")
	if !summary.Exists() {
		summary = gjson.GetBytes(original, "reasoning.summary")
	}
	if summary.Exists() {
		body, _ = sjson.SetBytes(body, "reasoning_summary", summary.Value())
	} else if gjson.GetBytes(body, "reasoning_effort").String() != "" || budget.Int() > 0 {
		body, _ = sjson.SetBytes(body, "reasoning_summary", "auto")
	}
	return body
}

// WorkBuddy 5.9.3 orders max above xhigh and clamps to the closest supported
// lower level (or the minimum supported level). Only an explicit model list is
// authoritative here; absent metadata must not downgrade a client's request.
func codeBuddySupportedEffort(effort string, supported []string) string {
	levels := []string{"minimal", "low", "medium", "high", "xhigh", "max"}
	requested := -1
	lowest, best := len(levels), -1
	for i, level := range levels {
		if level == effort {
			requested = i
		}
	}
	if requested < 0 {
		return effort
	}
	for i, level := range levels {
		for _, allowed := range supported {
			if allowed != level {
				continue
			}
			if i < lowest {
				lowest = i
			}
			if i <= requested && i > best {
				best = i
			}
		}
	}
	if best >= 0 {
		return levels[best]
	}
	if lowest < len(levels) {
		return levels[lowest]
	}
	return effort
}

func clampCodeBuddyOutputTokens(body []byte, limit int) ([]byte, bool) {
	if limit <= 0 {
		return body, false
	}
	changed := false
	for _, field := range []string{"max_tokens", "max_completion_tokens"} {
		value := gjson.GetBytes(body, field)
		if value.Type == gjson.Number && value.Int() > int64(limit) {
			if limited, err := sjson.SetBytes(body, field, limit); err == nil {
				body, changed = limited, true
			}
		}
	}
	return body, changed
}
