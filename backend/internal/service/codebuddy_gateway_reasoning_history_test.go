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

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyMessagesPreservesOrdinaryAssistantReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, platform := range []string{PlatformCodeBuddy, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			body := []byte(`{"model":"hy3","max_tokens":256,"stream":false,"messages":[
				{"role":"user","content":"hello"},
				{"role":"assistant","content":[{"type":"thinking","thinking":"first step"},{"type":"text","text":"prior answer"},{"type":"thinking","thinking":"second step"}]},
				{"role":"user","content":"continue"}
			]}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			account := forceChatMessagesFallbackAccount()
			responseBody := `{"id":"chatcmpl_history","object":"chat.completion","model":"hy3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
			contentType := "application/json"
			wantReasoning := ""
			if platform == PlatformCodeBuddy {
				account = codeBuddyRawChatCompletionsTestAccount()
				account.Credentials["codebuddy_models_meta"] = `[{"id":"hy3","max_output_tokens":64000}]`
				responseBody = codeBuddyChatCompletionsSSE("hy3", "chatcmpl_history", "ok")
				contentType = "text/event-stream"
				wantReasoning = "first step\nsecond step"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{contentType}},
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, wantReasoning, gjson.GetBytes(upstream.lastBody, "messages.1.reasoning_content").String())
			require.Equal(t, "prior answer", gjson.GetBytes(upstream.lastBody, "messages.1.content").String())
			require.Equal(t, "continue", gjson.GetBytes(upstream.lastBody, "messages.2.content").String())
			require.False(t, gjson.GetBytes(upstream.lastBody, "messages.1.tool_calls").Exists())
			require.Equal(t, "ok", gjson.Get(rec.Body.String(), "content.0.text").String())
		})
	}
}
