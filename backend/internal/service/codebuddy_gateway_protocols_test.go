//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codebuddy"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyGatewayProtocolsPreserveClientFormatAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The existing raw-chat regression covers non-streaming Chat Completions.
	// These cases exercise the public forwarding entry points and the remaining
	// protocol/stream combinations without contacting CodeBuddy.
	tests := []struct {
		name       string
		path       string
		body       string
		stream     bool
		jsonFields map[string]string
		sseMarkers []string
	}{
		{
			name: "messages_buffers_upstream_stream_as_json",
			path: "/v1/messages",
			body: `{"model":"hy3","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`,
			jsonFields: map[string]string{
				"type": "message", "role": "assistant", "content.0.text": "ok",
				"stop_reason": "end_turn", "usage.input_tokens": "3", "usage.output_tokens": "2",
			},
		},
		{
			name:   "messages_converts_upstream_stream_to_anthropic_events",
			path:   "/v1/messages",
			body:   `{"model":"hy3","max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":true}`,
			stream: true,
			sseMarkers: []string{
				"event: message_start", "event: content_block_delta", `"text":"ok"`,
				"event: message_delta", `"output_tokens":2`, "event: message_stop",
			},
		},
		{
			name: "responses_buffers_upstream_stream_as_json",
			path: "/v1/responses",
			body: `{"model":"hy3","input":"hello","stream":false}`,
			jsonFields: map[string]string{
				"object": "response", "status": "completed", "output.0.content.0.text": "ok",
				"usage.input_tokens": "3", "usage.output_tokens": "2",
			},
		},
		{
			name:   "responses_converts_upstream_stream_to_responses_events",
			path:   "/v1/responses",
			body:   `{"model":"hy3","input":"hello","stream":true}`,
			stream: true,
			sseMarkers: []string{
				"event: response.output_text.delta", `"delta":"ok"`, "event: response.completed",
				`"input_tokens":3`, `"output_tokens":2`, "data: [DONE]",
			},
		},
		{
			name:   "chat_completions_preserves_stream_and_usage_chunks",
			path:   "/v1/chat/completions",
			body:   `{"model":"hy3","messages":[{"role":"user","content":"hello"}],"stream":true}`,
			stream: true,
			sseMarkers: []string{
				`"object":"chat.completion.chunk"`, `"content":"ok"`,
				`"prompt_tokens":3`, `"completion_tokens":2`, "data: [DONE]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":          []string{"text/event-stream"},
					"X-Upstream-Request-Id": []string{"codebuddy-request-id"},
				},
				Body: io.NopCloser(strings.NewReader(codeBuddyChatCompletionsSSE("hy3", "chatcmpl_cb", "ok"))),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := codeBuddyRawChatCompletionsTestAccount()

			var result *OpenAIForwardResult
			var err error
			switch tt.path {
			case "/v1/messages":
				result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			case "/v1/responses":
				result, err = svc.Forward(context.Background(), c, account, body)
			case "/v1/chat/completions":
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "codebuddy-request-id", result.UpstreamHeaders.Get("X-Upstream-Request-ID"))
			require.Len(t, upstream.requests, 1)
			require.Equal(t, codebuddy.DefaultBaseURL+codebuddy.ChatCompletionsPath, upstream.lastReq.URL.String())
			require.Equal(t, "Bearer cb-at-test", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
			require.Equal(t, "hy3", gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "hello", gjson.GetBytes(upstream.lastBody, "messages.0.content").String())
			require.False(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
			require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
			require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())
			require.Equal(t, codebuddy.ChatCompletionsPath, GetActualOpenAIUpstreamEndpoint(c))
			require.Equal(t, codebuddy.ChatCompletionsPath, result.UpstreamEndpoint)
			require.Equal(t, 3, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.Equal(t, tt.stream, result.Stream)

			require.Equal(t, http.StatusOK, rec.Code)
			if tt.stream {
				require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
			} else {
				require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
				require.True(t, gjson.Valid(rec.Body.String()))
			}
			for path, value := range tt.jsonFields {
				require.Equal(t, value, gjson.Get(rec.Body.String(), path).String(), "response field %s", path)
			}
			for _, marker := range tt.sseMarkers {
				require.Contains(t, rec.Body.String(), marker)
			}
		})
	}
}
