package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type PromptRuleProtocol string

const (
	PromptRuleProtocolOpenAIChat      PromptRuleProtocol = "openai_chat"
	PromptRuleProtocolOpenAIResponses PromptRuleProtocol = "openai_responses"
	PromptRuleProtocolAnthropic       PromptRuleProtocol = "anthropic_messages"
	PromptRuleProtocolGemini          PromptRuleProtocol = "gemini_generate_content"
)

type PromptRuleSkip struct {
	RuleID int64
	Role   string
	Action string
	Reason string
}

type PromptRuleInjectionResult struct {
	Body    []byte
	Skipped []PromptRuleSkip
}

func InjectPromptRules(protocol PromptRuleProtocol, body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	switch protocol {
	case PromptRuleProtocolOpenAIChat:
		return InjectPromptRulesOpenAIChat(body, prepend, appendRules)
	case PromptRuleProtocolOpenAIResponses:
		return InjectPromptRulesOpenAIResponses(body, prepend, appendRules)
	case PromptRuleProtocolAnthropic:
		return InjectPromptRulesAnthropic(body, prepend, appendRules)
	case PromptRuleProtocolGemini:
		return InjectPromptRulesGemini(body, prepend, appendRules)
	default:
		return PromptRuleInjectionResult{Body: body}
	}
}

func InjectPromptRulesOpenAIChat(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	return injectMessageArrayProtocol(body, "messages", PromptRuleProtocolOpenAIChat, prepend, appendRules)
}

func InjectPromptRulesAnthropic(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := injectAnthropicSystemRules(body, prepend, appendRules)
	messageResult := injectMessageArrayProtocol(result.Body, "messages", PromptRuleProtocolAnthropic, prepend, appendRules)
	result.Body = messageResult.Body
	result.Skipped = append(result.Skipped, messageResult.Skipped...)
	return result
}

func InjectPromptRulesGemini(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := injectGeminiSystemRules(body, prepend, appendRules)
	messageResult := injectMessageArrayProtocol(result.Body, "contents", PromptRuleProtocolGemini, prepend, appendRules)
	result.Body = messageResult.Body
	result.Skipped = append(result.Skipped, messageResult.Skipped...)
	return result
}

func InjectPromptRulesOpenAIResponses(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := injectResponsesSystemRules(body, prepend, appendRules)
	prependByRole, appendByRole := nonSystemRulesByRole(prepend, appendRules)
	if len(prependByRole) == 0 && len(appendByRole) == 0 {
		return result
	}

	input := gjson.GetBytes(result.Body, "input")
	if input.Type == gjson.String {
		userPrepend := prependByRole[model.PromptRoleUser]
		userAppend := appendByRole[model.PromptRoleUser]
		if len(userPrepend) > 0 || len(userAppend) > 0 {
			merged := mergeText(input.String(), joinedRuleContent(userPrepend), joinedRuleContent(userAppend))
			updated, err := sjson.SetBytes(result.Body, "input", merged)
			if err == nil {
				result.Body = updated
			} else {
				result.Skipped = append(result.Skipped, skipsForRules(userPrepend, "failed to update input")...)
				result.Skipped = append(result.Skipped, skipsForRules(userAppend, "failed to update input")...)
			}
		}
		result.Skipped = append(result.Skipped, skipsForRules(prependByRole[model.PromptRoleAssistant], "no safe assistant text anchor")...)
		result.Skipped = append(result.Skipped, skipsForRules(appendByRole[model.PromptRoleAssistant], "no safe assistant text anchor")...)
		return result
	}

	messageResult := injectMessageArrayProtocol(result.Body, "input", PromptRuleProtocolOpenAIResponses, prepend, appendRules)
	result.Body = messageResult.Body
	result.Skipped = append(result.Skipped, messageResult.Skipped...)
	return result
}

func injectMessageArrayProtocol(body []byte, path string, protocol PromptRuleProtocol, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := PromptRuleInjectionResult{Body: body}
	prependByRole, appendByRole := nonSystemRulesByRole(prepend, appendRules)
	if len(prependByRole) == 0 && len(appendByRole) == 0 {
		if protocol == PromptRuleProtocolOpenAIChat {
			return injectChatSystemRules(body, prepend, appendRules)
		}
		return result
	}

	array := gjson.GetBytes(body, path)
	if !array.Exists() || !array.IsArray() {
		for _, rules := range prependByRole {
			result.Skipped = append(result.Skipped, skipsForRules(rules, "message array is missing")...)
		}
		for _, rules := range appendByRole {
			result.Skipped = append(result.Skipped, skipsForRules(rules, "message array is missing")...)
		}
		if protocol == PromptRuleProtocolOpenAIChat {
			systemResult := injectChatSystemRules(body, prepend, appendRules)
			result.Body = systemResult.Body
			result.Skipped = append(result.Skipped, systemResult.Skipped...)
		}
		return result
	}

	items := rawArray(array)
	changed := false
	for _, role := range []string{model.PromptRoleUser, model.PromptRoleAssistant} {
		rolePrepend := prependByRole[role]
		roleAppend := appendByRole[role]
		if len(rolePrepend) == 0 && len(roleAppend) == 0 {
			continue
		}

		first, last := findSafeTextAnchors(items, protocol, role)
		if first < 0 {
			reason := fmt.Sprintf("no safe %s text anchor", role)
			result.Skipped = append(result.Skipped, skipsForRules(rolePrepend, reason)...)
			result.Skipped = append(result.Skipped, skipsForRules(roleAppend, reason)...)
			continue
		}

		if len(rolePrepend) > 0 {
			updated, ok := mergeMessageText(items[first], protocol, joinedRuleContent(rolePrepend), "")
			if !ok {
				result.Skipped = append(result.Skipped, skipsForRules(rolePrepend, "failed to update text anchor")...)
			} else {
				items[first] = updated
				changed = true
			}
		}
		if len(roleAppend) > 0 {
			updated, ok := mergeMessageText(items[last], protocol, "", joinedRuleContent(roleAppend))
			if !ok {
				result.Skipped = append(result.Skipped, skipsForRules(roleAppend, "failed to update text anchor")...)
			} else {
				items[last] = updated
				changed = true
			}
		}
	}

	if changed {
		if encoded, err := json.Marshal(items); err == nil {
			if updated, err := sjson.SetRawBytes(result.Body, path, encoded); err == nil {
				result.Body = updated
			}
		}
	}

	if protocol == PromptRuleProtocolOpenAIChat {
		systemResult := injectChatSystemRules(result.Body, prepend, appendRules)
		result.Body = systemResult.Body
		result.Skipped = append(result.Skipped, systemResult.Skipped...)
	}
	return result
}

func injectChatSystemRules(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := PromptRuleInjectionResult{Body: body}
	prependSystem, appendSystem := rulesForRole(prepend, model.PromptRoleSystem), rulesForRole(appendRules, model.PromptRoleSystem)
	if len(prependSystem) == 0 && len(appendSystem) == 0 {
		return result
	}
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		result.Skipped = append(result.Skipped, skipsForRules(prependSystem, "message array is missing")...)
		result.Skipped = append(result.Skipped, skipsForRules(appendSystem, "message array is missing")...)
		return result
	}

	items := rawArray(messages)
	firstSystem, lastSystem := -1, -1
	for i, item := range items {
		role := gjson.GetBytes(item, "role").String()
		if role != model.PromptRoleSystem && role != "developer" {
			break
		}
		if role == model.PromptRoleSystem {
			if firstSystem < 0 {
				firstSystem = i
			}
			lastSystem = i
		}
	}
	if firstSystem < 0 {
		content := mergeText("", joinedRuleContent(prependSystem), joinedRuleContent(appendSystem))
		items = append([]json.RawMessage{rawTextMessage(model.PromptRoleSystem, content)}, items...)
	} else {
		if len(prependSystem) > 0 {
			updated, ok := mergeMessageText(items[firstSystem], PromptRuleProtocolOpenAIChat, joinedRuleContent(prependSystem), "")
			if !ok {
				result.Skipped = append(result.Skipped, skipsForRules(prependSystem, "failed to update system instruction")...)
			} else {
				items[firstSystem] = updated
			}
		}
		if len(appendSystem) > 0 {
			updated, ok := mergeMessageText(items[lastSystem], PromptRuleProtocolOpenAIChat, "", joinedRuleContent(appendSystem))
			if !ok {
				result.Skipped = append(result.Skipped, skipsForRules(appendSystem, "failed to update system instruction")...)
			} else {
				items[lastSystem] = updated
			}
		}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return result
	}
	updated, err := sjson.SetRawBytes(body, "messages", encoded)
	if err == nil {
		result.Body = updated
	}
	return result
}

func injectAnthropicSystemRules(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := PromptRuleInjectionResult{Body: body}
	prependSystem, appendSystem := rulesForRole(prepend, model.PromptRoleSystem), rulesForRole(appendRules, model.PromptRoleSystem)
	if len(prependSystem) == 0 && len(appendSystem) == 0 {
		return result
	}
	blocks := make([]json.RawMessage, 0)
	if text := joinedRuleContent(prependSystem); text != "" {
		blocks = append(blocks, rawAnthropicTextBlock(text))
	}
	system := gjson.GetBytes(body, "system")
	if system.Exists() {
		if system.IsArray() {
			blocks = append(blocks, rawArray(system)...)
		} else if system.Type == gjson.String && system.String() != "" {
			blocks = append(blocks, rawAnthropicTextBlock(system.String()))
		}
	}
	if text := joinedRuleContent(appendSystem); text != "" {
		blocks = append(blocks, rawAnthropicTextBlock(text))
	}
	encoded, err := json.Marshal(blocks)
	if err != nil {
		return result
	}
	if updated, err := sjson.SetRawBytes(body, "system", encoded); err == nil {
		result.Body = updated
	}
	return result
}

func injectGeminiSystemRules(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := PromptRuleInjectionResult{Body: body}
	prependSystem, appendSystem := rulesForRole(prepend, model.PromptRoleSystem), rulesForRole(appendRules, model.PromptRoleSystem)
	if len(prependSystem) == 0 && len(appendSystem) == 0 {
		return result
	}
	parts := make([]json.RawMessage, 0)
	if text := joinedRuleContent(prependSystem); text != "" {
		parts = append(parts, rawGeminiTextPart(text))
	}
	existing := gjson.GetBytes(body, "systemInstruction.parts")
	if existing.Exists() && existing.IsArray() {
		parts = append(parts, rawArray(existing)...)
	}
	if text := joinedRuleContent(appendSystem); text != "" {
		parts = append(parts, rawGeminiTextPart(text))
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return result
	}
	updated, err := sjson.SetRawBytes(body, "systemInstruction.parts", encoded)
	if err != nil {
		return result
	}
	if !gjson.GetBytes(updated, "systemInstruction.role").Exists() {
		updated, _ = sjson.SetBytes(updated, "systemInstruction.role", "user")
	}
	result.Body = updated
	return result
}

func injectResponsesSystemRules(body []byte, prepend, appendRules []*model.PromptRule) PromptRuleInjectionResult {
	result := PromptRuleInjectionResult{Body: body}
	prependSystem, appendSystem := rulesForRole(prepend, model.PromptRoleSystem), rulesForRole(appendRules, model.PromptRoleSystem)
	if len(prependSystem) == 0 && len(appendSystem) == 0 {
		return result
	}
	existing := ""
	instructions := gjson.GetBytes(body, "instructions")
	if instructions.Type == gjson.String {
		existing = instructions.String()
	}
	merged := mergeText(existing, joinedRuleContent(prependSystem), joinedRuleContent(appendSystem))
	if updated, err := sjson.SetBytes(body, "instructions", merged); err == nil {
		result.Body = updated
	}
	return result
}

func findSafeTextAnchors(items []json.RawMessage, protocol PromptRuleProtocol, role string) (int, int) {
	first, last := -1, -1
	for i, item := range items {
		if normalizedMessageRole(item, protocol) != role || !isSafeTextMessage(item, protocol, role) {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	return first, last
}

func normalizedMessageRole(item json.RawMessage, protocol PromptRuleProtocol) string {
	role := gjson.GetBytes(item, "role").String()
	if protocol == PromptRuleProtocolGemini && role == "model" {
		return model.PromptRoleAssistant
	}
	return role
}

func isSafeTextMessage(item json.RawMessage, protocol PromptRuleProtocol, role string) bool {
	if role == model.PromptRoleAssistant {
		if tools := gjson.GetBytes(item, "tool_calls"); tools.Exists() && tools.IsArray() && len(tools.Array()) > 0 {
			return false
		}
	}
	contentPath := "content"
	if protocol == PromptRuleProtocolGemini {
		contentPath = "parts"
	}
	content := gjson.GetBytes(item, contentPath)
	if content.Type == gjson.String {
		return content.String() != ""
	}
	if !content.IsArray() {
		return false
	}
	hasText := false
	unsafeAssistantBlock := false
	content.ForEach(func(_, part gjson.Result) bool {
		partType := part.Get("type").String()
		if role == model.PromptRoleAssistant && (partType == "tool_use" || partType == "server_tool_use" || part.Get("functionCall").Exists()) {
			unsafeAssistantBlock = true
		}
		if part.Get("text").Type == gjson.String {
			hasText = true
		}
		return true
	})
	return hasText && !unsafeAssistantBlock
}

func mergeMessageText(item json.RawMessage, protocol PromptRuleProtocol, prepend, appendText string) (json.RawMessage, bool) {
	contentPath := "content"
	if protocol == PromptRuleProtocolGemini {
		contentPath = "parts"
	}
	content := gjson.GetBytes(item, contentPath)
	if content.Type == gjson.String {
		updated, err := sjson.SetBytes(item, contentPath, mergeText(content.String(), prepend, appendText))
		return json.RawMessage(updated), err == nil
	}
	if !content.IsArray() {
		return item, false
	}
	parts := rawArray(content)
	first, last := -1, -1
	for i, part := range parts {
		if gjson.GetBytes(part, "text").Type == gjson.String {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return item, false
	}
	if prepend != "" {
		text := gjson.GetBytes(parts[first], "text").String()
		updated, err := sjson.SetBytes(parts[first], "text", mergeText(text, prepend, ""))
		if err != nil {
			return item, false
		}
		parts[first] = updated
	}
	if appendText != "" {
		text := gjson.GetBytes(parts[last], "text").String()
		updated, err := sjson.SetBytes(parts[last], "text", mergeText(text, "", appendText))
		if err != nil {
			return item, false
		}
		parts[last] = updated
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return item, false
	}
	updated, err := sjson.SetRawBytes(item, contentPath, encoded)
	return json.RawMessage(updated), err == nil
}

func nonSystemRulesByRole(prepend, appendRules []*model.PromptRule) (map[string][]*model.PromptRule, map[string][]*model.PromptRule) {
	prependByRole := make(map[string][]*model.PromptRule)
	appendByRole := make(map[string][]*model.PromptRule)
	for _, rule := range prepend {
		if rule != nil && rule.Role != model.PromptRoleSystem {
			prependByRole[rule.Role] = append(prependByRole[rule.Role], rule)
		}
	}
	for _, rule := range appendRules {
		if rule != nil && rule.Role != model.PromptRoleSystem {
			appendByRole[rule.Role] = append(appendByRole[rule.Role], rule)
		}
	}
	return prependByRole, appendByRole
}

func rulesForRole(rules []*model.PromptRule, role string) []*model.PromptRule {
	result := make([]*model.PromptRule, 0)
	for _, rule := range rules {
		if rule != nil && rule.Role == role {
			result = append(result, rule)
		}
	}
	return result
}

func joinedRuleContent(rules []*model.PromptRule) string {
	contents := make([]string, 0, len(rules))
	for _, rule := range rules {
		if rule != nil && rule.Content != "" {
			contents = append(contents, rule.Content)
		}
	}
	return strings.Join(contents, "\n\n")
}

func mergeText(existing, prepend, appendText string) string {
	parts := make([]string, 0, 3)
	if prepend != "" {
		parts = append(parts, prepend)
	}
	if existing != "" {
		parts = append(parts, existing)
	}
	if appendText != "" {
		parts = append(parts, appendText)
	}
	return strings.Join(parts, "\n\n")
}

func skipsForRules(rules []*model.PromptRule, reason string) []PromptRuleSkip {
	skips := make([]PromptRuleSkip, 0, len(rules))
	for _, rule := range rules {
		if rule != nil {
			skips = append(skips, PromptRuleSkip{RuleID: rule.ID, Role: rule.Role, Action: rule.Action, Reason: reason})
		}
	}
	return skips
}

func rawArray(value gjson.Result) []json.RawMessage {
	items := make([]json.RawMessage, 0)
	value.ForEach(func(_, item gjson.Result) bool {
		items = append(items, json.RawMessage(item.Raw))
		return true
	})
	return items
}

func rawTextMessage(role, content string) json.RawMessage {
	value, _ := json.Marshal(map[string]string{"role": role, "content": content})
	return value
}

func rawAnthropicTextBlock(content string) json.RawMessage {
	value, _ := json.Marshal(map[string]string{"type": "text", "text": content})
	return value
}

func rawGeminiTextPart(content string) json.RawMessage {
	value, _ := json.Marshal(map[string]string{"text": content})
	return value
}
