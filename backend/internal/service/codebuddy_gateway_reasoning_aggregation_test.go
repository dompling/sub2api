//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyGatewayBufferedReasoningAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	variants := []struct {
		name      string
		deltas    []string
		reasoning string
	}{
		{"reasoning_content", []string{`{"reasoning_content":"first "}`, `{"reasoning_content":"second"}`}, "first second"},
		{"reasoning_alias", []string{`{"reasoning":"first "}`, `{"reasoning":"second"}`}, "first second"},
		{"mixed_aliases", []string{`{"reasoning_content":"first "}`, `{"reasoning":"second"}`}, "first second"},
		{"canonical_precedence", []string{`{"reasoning_content":"preferred","reasoning":"ignored alias"}`}, "preferred"},
		{"empty_canonical_fallback", []string{`{"reasoning_content":"","reasoning":"fallback"}`}, "fallback"},
		{"null_canonical_fallback", []string{`{"reasoning_content":null,"reasoning":"fallback"}`}, "fallback"},
		{"no_reasoning", []string{`{}`}, ""},
	}
	protocols := []struct {
		name          string
		path          string
		body          string
		reasoningPath string
		textPath      string
		toolIDPath    string
		toolNamePath  string
		toolArgsPath  string
		stopPath      string
		stopReason    string
	}{
		{
			name: "chat_completions", path: "/v1/chat/completions",
			body:          `{"model":"hy3","messages":[{"role":"user","content":"hello"}],"stream":false}`,
			reasoningPath: "choices.0.message.reasoning_content", textPath: "choices.0.message.content",
			toolIDPath: "choices.0.message.tool_calls.0.id", toolNamePath: "choices.0.message.tool_calls.0.function.name",
			toolArgsPath: "choices.0.message.tool_calls.0.function.arguments",
			stopPath:     "choices.0.finish_reason", stopReason: "tool_calls",
		},
		{
			name: "responses", path: "/v1/responses",
			body:          `{"model":"hy3","input":"hello","stream":false}`,
			reasoningPath: `output.#(type=="reasoning").summary.0.text`, textPath: `output.#(type=="message").content.0.text`,
			toolIDPath: `output.#(type=="function_call").call_id`, toolNamePath: `output.#(type=="function_call").name`,
			toolArgsPath: `output.#(type=="function_call").arguments`,
			stopPath:     "status", stopReason: "completed",
		},
		{
			name: "messages", path: "/v1/messages",
			body:          `{"model":"hy3","max_tokens":256,"messages":[{"role":"user","content":"hello"}],"stream":false}`,
			reasoningPath: `content.#(type=="thinking").thinking`, textPath: `content.#(type=="text").text`,
			toolIDPath: `content.#(type=="tool_use").id`, toolNamePath: `content.#(type=="tool_use").name`,
			toolArgsPath: `content.#(type=="tool_use").input`,
			stopPath:     "stop_reason", stopReason: "tool_use",
		},
	}

	for _, protocol := range protocols {
		for _, variant := range variants {
			t.Run(protocol.name+"/"+variant.name, func(t *testing.T) {
				var stream strings.Builder
				deltas := append([]string{`{"role":"assistant"}`}, variant.deltas...)
				deltas = append(deltas,
					`{"content":"ok","tool_calls":[{"index":2,"id":"call_lookup","type":"function","function":{"name":"lookup","arguments":"{\"query\":"}}]}`,
					`{"tool_calls":[{"index":2,"function":{"arguments":"\"value\"}"}}]}`,
				)
				for _, delta := range deltas {
					fmt.Fprintf(&stream, "data: {\"id\":\"chatcmpl_reasoning\",\"object\":\"chat.completion.chunk\",\"model\":\"hy3\",\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", delta)
				}
				stream.WriteString("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
				stream.WriteString("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":7,\"total_tokens\":16,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\ndata: [DONE]\n\n")

				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				body := []byte(protocol.body)
				c.Request = httptest.NewRequest(http.MethodPost, protocol.path, bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(stream.String())),
				}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := codeBuddyRawChatCompletionsTestAccount()
				account.Credentials["codebuddy_models_meta"] = `[{"id":"hy3","max_output_tokens":64000}]`

				var result *OpenAIForwardResult
				var err error
				switch protocol.path {
				case "/v1/chat/completions":
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				case "/v1/responses":
					result, err = svc.Forward(context.Background(), c, account, body)
				case "/v1/messages":
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.False(t, result.Stream)
				require.Equal(t, 9, result.Usage.InputTokens)
				require.Equal(t, 7, result.Usage.OutputTokens)
				require.Equal(t, 4, result.Usage.CacheReadInputTokens)
				require.Len(t, upstream.requests, 1)
				require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
				require.Equal(t, http.StatusOK, rec.Code)
				require.Contains(t, rec.Header().Get("Content-Type"), "application/json")

				response := rec.Body.String()
				require.True(t, gjson.Valid(response))
				require.Equal(t, variant.reasoning, gjson.Get(response, protocol.reasoningPath).String())
				require.NotContains(t, response, "ignored alias")
				require.Equal(t, "ok", gjson.Get(response, protocol.textPath).String())
				require.Equal(t, "call_lookup", gjson.Get(response, protocol.toolIDPath).String())
				require.Equal(t, "lookup", gjson.Get(response, protocol.toolNamePath).String())
				require.JSONEq(t, `{"query":"value"}`, gjson.Get(response, protocol.toolArgsPath).String())
				require.Equal(t, protocol.stopReason, gjson.Get(response, protocol.stopPath).String())
			})
		}
	}
}
