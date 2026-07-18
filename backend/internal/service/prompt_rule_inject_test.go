package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func promptRule(id int64, role, action, content string) *model.PromptRule {
	return &model.PromptRule{ID: id, Role: role, Action: action, Content: content}
}

func TestInjectPromptRulesOpenAIChatPreservesFinalUserAndToolPairs(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"messages":[
			{"role":"system","content":"existing system"},
			{"role":"user","content":"first user"},
			{"role":"assistant","content":"working","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result"},
			{"role":"assistant","content":"answer"},
			{"role":"user","content":[{"type":"text","text":"final user"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AA=="}}]}
		]
	}`)

	result := InjectPromptRulesOpenAIChat(body,
		[]*model.PromptRule{
			promptRule(1, model.PromptRoleSystem, model.PromptActionPrepend, "system before"),
			promptRule(2, model.PromptRoleUser, model.PromptActionPrepend, "user before"),
		},
		[]*model.PromptRule{
			promptRule(3, model.PromptRoleSystem, model.PromptActionAppend, "system after"),
			promptRule(4, model.PromptRoleUser, model.PromptActionAppend, "user after"),
			promptRule(5, model.PromptRoleAssistant, model.PromptActionAppend, "assistant after"),
		},
	)

	require.Empty(t, result.Skipped)
	require.Equal(t, "system", gjson.GetBytes(result.Body, "messages.0.role").String())
	require.Equal(t, "system before\n\nexisting system\n\nsystem after", gjson.GetBytes(result.Body, "messages.0.content").String())
	require.Equal(t, "user before\n\nfirst user", gjson.GetBytes(result.Body, "messages.1.content").String())
	require.Equal(t, "call_1", gjson.GetBytes(result.Body, "messages.2.tool_calls.0.id").String())
	require.Equal(t, "call_1", gjson.GetBytes(result.Body, "messages.3.tool_call_id").String())
	require.Equal(t, "answer\n\nassistant after", gjson.GetBytes(result.Body, "messages.4.content").String())
	require.Equal(t, "final user\n\nuser after", gjson.GetBytes(result.Body, "messages.5.content.0.text").String())
	require.Equal(t, "user", gjson.GetBytes(result.Body, "messages.5.role").String())
}

func TestInjectPromptRulesSkipsUnsafeAssistantAnchor(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"run"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1"}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`)
	rule := promptRule(8, model.PromptRoleAssistant, model.PromptActionAppend, "unsafe")

	result := InjectPromptRulesOpenAIChat(body, nil, []*model.PromptRule{rule})

	require.Equal(t, string(body), string(result.Body))
	require.Equal(t, []PromptRuleSkip{{RuleID: 8, Role: "assistant", Action: "append", Reason: "no safe assistant text anchor"}}, result.Skipped)
}

func TestInjectPromptRulesOpenAIChatCreatesSystemOnlyAtStart(t *testing.T) {
	body := []byte(`{"messages":[{"role":"developer","content":"developer"},{"role":"user","content":"question"}]}`)
	result := InjectPromptRulesOpenAIChat(body,
		[]*model.PromptRule{promptRule(1, "system", "prepend", "before")},
		[]*model.PromptRule{promptRule(2, "system", "append", "after")},
	)

	require.Equal(t, "system", gjson.GetBytes(result.Body, "messages.0.role").String())
	require.Equal(t, "before\n\nafter", gjson.GetBytes(result.Body, "messages.0.content").String())
	require.Equal(t, "developer", gjson.GetBytes(result.Body, "messages.1.role").String())
	require.Equal(t, "user", gjson.GetBytes(result.Body, "messages.2.role").String())
}

func TestInjectPromptRulesAnthropicUsesSystemAndTextAnchors(t *testing.T) {
	body := []byte(`{
		"system":"existing",
		"messages":[
			{"role":"user","content":[{"type":"text","text":"question"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"},{"type":"text","text":"continue"}]}
		]
	}`)
	result := InjectPromptRulesAnthropic(body,
		[]*model.PromptRule{promptRule(1, "system", "prepend", "before")},
		[]*model.PromptRule{
			promptRule(2, "system", "append", "after"),
			promptRule(3, "user", "append", "user rule"),
			promptRule(4, "assistant", "append", "assistant rule"),
		},
	)

	require.Equal(t, "before", gjson.GetBytes(result.Body, "system.0.text").String())
	require.Equal(t, "existing", gjson.GetBytes(result.Body, "system.1.text").String())
	require.Equal(t, "after", gjson.GetBytes(result.Body, "system.2.text").String())
	require.Equal(t, "continue\n\nuser rule", gjson.GetBytes(result.Body, "messages.2.content.1.text").String())
	require.Equal(t, "toolu_1", gjson.GetBytes(result.Body, "messages.1.content.0.id").String())
	require.Len(t, result.Skipped, 1)
	require.Equal(t, int64(4), result.Skipped[0].RuleID)
}

func TestInjectPromptRulesOpenAIResponsesSupportsStringAndArrayInput(t *testing.T) {
	t.Run("string input", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5","instructions":"existing","input":"question"}`)
		result := InjectPromptRulesOpenAIResponses(body,
			[]*model.PromptRule{promptRule(1, "system", "prepend", "before")},
			[]*model.PromptRule{
				promptRule(2, "system", "append", "after"),
				promptRule(3, "user", "append", "user rule"),
				promptRule(4, "assistant", "append", "assistant rule"),
			},
		)

		require.Equal(t, "before\n\nexisting\n\nafter", gjson.GetBytes(result.Body, "instructions").String())
		require.Equal(t, "question\n\nuser rule", gjson.GetBytes(result.Body, "input").String())
		require.Len(t, result.Skipped, 1)
		require.Equal(t, int64(4), result.Skipped[0].RuleID)
	})

	t.Run("array input preserves function pair", func(t *testing.T) {
		body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"question"}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"ok"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
		result := InjectPromptRulesOpenAIResponses(body, nil, []*model.PromptRule{promptRule(1, "user", "append", "rule")})

		require.Equal(t, "call_1", gjson.GetBytes(result.Body, "input.1.call_id").String())
		require.Equal(t, "call_1", gjson.GetBytes(result.Body, "input.2.call_id").String())
		require.Equal(t, "continue\n\nrule", gjson.GetBytes(result.Body, "input.3.content.0.text").String())
	})
}

func TestInjectPromptRulesGeminiUsesNativeRolesAndSystemInstruction(t *testing.T) {
	body := []byte(`{"systemInstruction":{"parts":[{"text":"existing"}]},"contents":[{"role":"user","parts":[{"text":"question"}]},{"role":"model","parts":[{"text":"answer"}]},{"role":"user","parts":[{"text":"continue"}]}]}`)
	result := InjectPromptRulesGemini(body,
		[]*model.PromptRule{promptRule(1, "system", "prepend", "before")},
		[]*model.PromptRule{
			promptRule(2, "system", "append", "after"),
			promptRule(3, "user", "append", "user rule"),
			promptRule(4, "assistant", "append", "assistant rule"),
		},
	)

	require.Empty(t, result.Skipped)
	require.Equal(t, "before", gjson.GetBytes(result.Body, "systemInstruction.parts.0.text").String())
	require.Equal(t, "existing", gjson.GetBytes(result.Body, "systemInstruction.parts.1.text").String())
	require.Equal(t, "after", gjson.GetBytes(result.Body, "systemInstruction.parts.2.text").String())
	require.Equal(t, "answer\n\nassistant rule", gjson.GetBytes(result.Body, "contents.1.parts.0.text").String())
	require.Equal(t, "continue\n\nuser rule", gjson.GetBytes(result.Body, "contents.2.parts.0.text").String())
	require.Equal(t, "user", gjson.GetBytes(result.Body, "contents.2.role").String())
}

func TestInjectPromptRulesNoRulesReturnsOriginalBytes(t *testing.T) {
	body := []byte("{ \"messages\" : [ { \"role\" : \"user\", \"content\" : \"hi\" } ] }")
	result := InjectPromptRulesOpenAIChat(body, nil, nil)
	require.Equal(t, string(body), string(result.Body))
}
