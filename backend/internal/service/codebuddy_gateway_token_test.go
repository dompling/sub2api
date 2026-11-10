//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodeBuddyGatewayAllProtocolsUseTokenProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/admin/test"} {
		for _, refreshFailure := range []bool{false, true} {
			name := path + "/cached_token"
			if refreshFailure {
				name = path + "/refresh_failure"
			}
			t.Run(name, func(t *testing.T) {
				account := codeBuddyTokenAccountForTest()
				repo := &tokenRefreshAccountRepo{}
				repo.accountsByID = map[int64]*Account{account.ID: account}
				cache := &grokTokenCacheForProviderTest{token: "fresh-cached-token", lockResult: true}
				if refreshFailure {
					cache.token = ""
				}
				provider := NewCodeBuddyTokenProvider(repo, cache)
				provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &refreshAPIExecutorStub{
					needsRefresh: true, err: errors.New("independent refresh failure"),
				})
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(codeBuddyChatCompletionsSSE("hy3", "chatcmpl_token", "ok"))),
				}}
				body := []byte(`{"model":"hy3","max_tokens":256,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
				if path == "/v1/responses" {
					body = []byte(`{"model":"hy3","input":"hello","stream":false}`)
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, codeBuddyTokenProvider: provider}
				var err error
				switch path {
				case "/v1/chat/completions":
					_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				case "/v1/responses":
					_, err = svc.Forward(context.Background(), c, account, body)
				case "/v1/messages":
					_, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				case "/admin/test":
					testSvc := &AccountTestService{httpUpstream: upstream, codeBuddyTokenProvider: provider, accountRepo: repo}
					err = testSvc.testCodeBuddyAccountConnection(c, account, "hy3", "hello")
				}
				if refreshFailure {
					require.Error(t, err)
					require.Empty(t, upstream.requests, "an unusable credential must not reach inference")
					require.Zero(t, repo.setErrorCalls, "a refresh error must not become a false inference 401")
				} else {
					require.NoError(t, err)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "Bearer fresh-cached-token", upstream.lastReq.Header.Get("Authorization"))
					require.Contains(t, rec.Body.String(), "ok")
				}
			})
		}
	}
}

func TestCodeBuddyManualProbeCanRecoverInactiveAccountWithValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, state := range []string{"valid", "expired", "unknown_expiry"} {
		t.Run(state, func(t *testing.T) {
			account := codeBuddyTokenAccountForTest()
			account.Status = StatusError
			if state == "valid" {
				account.Credentials["expires_at"] = time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
			} else if state == "unknown_expiry" {
				delete(account.Credentials, "expires_at")
			}
			repo := &tokenRefreshAccountRepo{}
			repo.accountsByID = map[int64]*Account{account.ID: account}
			cache := &grokTokenCacheForProviderTest{lockResult: true}
			executor := &refreshAPIExecutorStub{needsRefresh: true}
			provider := NewCodeBuddyTokenProvider(repo, cache)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(codeBuddyChatCompletionsSSE("hy3", "chatcmpl_manual", "ok"))),
			}}
			svc := &AccountTestService{httpUpstream: upstream, codeBuddyTokenProvider: provider, accountRepo: repo}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/admin/test", nil)
			err := svc.testCodeBuddyAccountConnection(c, account, "hy3", "hello")
			if state == "valid" {
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1)
				require.Equal(t, "Bearer "+account.GetCodeBuddyAccessToken(), upstream.lastReq.Header.Get("Authorization"))
				require.Contains(t, rec.Body.String(), `"success":true`)
			} else {
				require.Error(t, err)
				require.Empty(t, upstream.requests)
			}
			require.Zero(t, executor.refreshCalls, "manual probe must not fake an active state to rotate disabled credentials")
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.setTempUnschedCalls)
			require.Equal(t, StatusError, account.Status)
		})
	}
}

func TestCodeBuddyManualProbeRejectsMissingConfiguredProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := codeBuddyTokenAccountForTest()
	account.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	proxyID := int64(12)
	account.ProxyID = &proxyID
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(codeBuddyChatCompletionsSSE("hy3", "chatcmpl_proxy", "ok"))),
	}}
	svc := &AccountTestService{httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/test", nil)
	err := svc.testCodeBuddyAccountConnection(c, account, "hy3", "hello")
	require.Error(t, err)
	require.Empty(t, upstream.requests, "a configured but unavailable proxy must not silently use direct inference")
}
