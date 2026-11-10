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
)

func TestCodeBuddyGatewayRejectsMissingConfiguredProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			body := []byte(`{"model":"hy3","max_tokens":32,"input":"hello","messages":[{"role":"user","content":"hello"}]}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusBadRequest,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"synthetic upstream error"}}`)),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := codeBuddyRawChatCompletionsTestAccount()
			account.Credentials["codebuddy_models_meta"] = `[{"id":"hy3","max_output_tokens":64000}]`
			proxyID := int64(91)
			account.ProxyID = &proxyID
			var err error
			switch path {
			case "/v1/chat/completions":
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			case "/v1/responses":
				_, err = svc.Forward(context.Background(), c, account, body)
			case "/v1/messages":
				_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
			}
			require.ErrorContains(t, err, "proxy")
			require.Empty(t, upstream.requests)
		})
	}
}
