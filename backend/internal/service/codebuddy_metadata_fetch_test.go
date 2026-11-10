//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyMetadataFetchUsesAcquiredTokenAndConfiguredEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"fresh", "credential_canceled", "credential_failed", "config_failed"} {
		t.Run(scenario, func(t *testing.T) {
			var requests atomic.Int64
			observations := make(chan [4]string, 4)
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				observations <- [4]string{r.Header.Get("Authorization"), r.Header.Get("X-User-Id"), r.URL.Host, r.URL.Path}
				if r.Method == http.MethodConnect || scenario == "config_failed" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"code":0,"data":{"models":[{"id":"hy3","maxOutputTokens":64000,"supportsReasoning":true,"reasoning":{"defaultEffort":"high"}}]}}`)
			}))
			defer proxy.Close()
			proxyURL, err := url.Parse(proxy.URL)
			require.NoError(t, err)
			port, err := strconv.Atoi(proxyURL.Port())
			require.NoError(t, err)
			account := codeBuddyTokenAccountForTest()
			account.ID = 8812001
			defer codeBuddyMetaCache.Delete(account.ID)
			delete(account.Credentials, "codebuddy_models_meta")
			account.Credentials["base_url"] = "http://workbuddy-config.invalid"
			account.Credentials["uid"] = "synthetic-user"
			account.Proxy = &Proxy{Protocol: "http", Host: proxyURL.Hostname(), Port: port}
			repo := &tokenRefreshAccountRepo{}
			repo.accountsByID = map[int64]*Account{account.ID: account}
			cache := &grokTokenCacheForProviderTest{token: "fresh-cached-token", lockResult: true}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			executor := &refreshAPIExecutorStub{needsRefresh: true, err: errors.New("refresh failed")}
			if scenario == "credential_canceled" {
				cache.token = ""
				executor.onRefresh = cancel
			} else if scenario == "credential_failed" {
				cache.token = ""
			}
			provider := NewCodeBuddyTokenProvider(repo, cache)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), codeBuddyTokenProvider: provider}
			attempts := 1
			if scenario == "config_failed" {
				attempts = 2
			}
			for i := 0; i < attempts; i++ {
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(codeBuddyChatCompletionsSSE("hy3", "metadata-test", "ok")))}}
				svc.httpUpstream = upstream
				body := []byte(`{"model":"hy3","max_tokens":100000,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
				result, err := svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
				if strings.HasPrefix(scenario, "credential_") {
					require.Error(t, err)
					require.Empty(t, upstream.requests)
					require.Zero(t, requests.Load(), "unavailable credentials must prevent the optional config request")
				} else {
					require.NoError(t, err)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "Bearer fresh-cached-token", upstream.lastReq.Header.Get("Authorization"))
					if scenario == "fresh" {
						require.EqualValues(t, 64000, gjson.GetBytes(upstream.lastBody, "max_tokens").Int())
						require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
						require.NotNil(t, result.ReasoningEffort)
						require.Equal(t, "high", *result.ReasoningEffort)
					}
				}
			}
			if !strings.HasPrefix(scenario, "credential_") {
				require.EqualValues(t, 1, requests.Load(), "failed metadata fetches need a short cooldown")
				observation := <-observations
				require.Equal(t, [4]string{"Bearer fresh-cached-token", "synthetic-user", "workbuddy-config.invalid", "/v3/config"}, observation)
			}
		})
	}
}

func TestCodeBuddyMetadataFetchCancellationDoesNotPoisonCache(t *testing.T) {
	var requests atomic.Int64
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"models":[{"id":"hy3","maxOutputTokens":64000}]}}`)
	}))
	defer server.Close()
	account := codeBuddyRawChatCompletionsTestAccount()
	account.ID = 8812002
	account.Credentials["base_url"] = server.URL
	defer codeBuddyMetaCache.Delete(account.ID)
	svc := &OpenAIGatewayService{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		select {
		case <-entered:
			cancel()
		case <-ctx.Done():
		}
	}()
	require.Empty(t, svc.getCodeBuddyModelMeta(ctx, account, "fresh-token"))
	_, exists := codeBuddyMetaCache.Load(account.ID)
	require.False(t, exists, "caller cancellation must not suppress a later valid request")
	require.Len(t, svc.getCodeBuddyModelMeta(context.Background(), account, "fresh-token"), 1)
	require.EqualValues(t, 2, requests.Load())
}

func TestCodeBuddyMetadataFetchNegativeCacheExpires(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"code":0,"data":{"models":[{"id":"hy3","maxOutputTokens":64000}]}}`)
	}))
	defer server.Close()
	account := codeBuddyRawChatCompletionsTestAccount()
	account.ID = 8812003
	account.Credentials["base_url"] = server.URL
	defer codeBuddyMetaCache.Delete(account.ID)
	svc := &OpenAIGatewayService{}
	require.Empty(t, svc.getCodeBuddyModelMeta(context.Background(), account, "fresh-token"))
	cached, exists := codeBuddyMetaCache.Load(account.ID)
	require.True(t, exists)
	entry := cached.(codeBuddyMetaCacheEntry)
	require.Greater(t, time.Until(entry.exp), time.Duration(0))
	require.LessOrEqual(t, time.Until(entry.exp), 30*time.Second)
	require.Empty(t, svc.getCodeBuddyModelMeta(context.Background(), account, "fresh-token"))
	require.EqualValues(t, 1, requests.Load())
	entry.exp = time.Now().Add(-time.Second)
	codeBuddyMetaCache.Store(account.ID, entry)
	require.Len(t, svc.getCodeBuddyModelMeta(context.Background(), account, "fresh-token"), 1)
	require.EqualValues(t, 2, requests.Load())
}
