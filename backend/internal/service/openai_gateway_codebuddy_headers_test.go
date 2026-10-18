package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newCodeBuddyHeaderTestContext() *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	return c
}

func TestResolveCodeBuddyConversationIDPrefersExplicitClientSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet","prompt_cache_key":"body-session","messages":[{"role":"user","content":"hello"}]}`)
	c := newCodeBuddyHeaderTestContext()
	c.Request.Header.Set("X-Conversation-ID", " codebuddy-session-42 ")

	require.Equal(t, "codebuddy-session-42", resolveCodeBuddyConversationID(c, body))
}

func TestResolveCodeBuddyConversationIDUsesPromptCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet","prompt_cache_key":"body-session","messages":[{"role":"user","content":"hello"}]}`)
	c := newCodeBuddyHeaderTestContext()

	require.Equal(t, "body-session", resolveCodeBuddyConversationID(c, body))
}

func TestResolveCodeBuddyConversationIDDerivesStableUUIDFromConversationPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	firstTurn := []byte(`{"model":"claude-sonnet","messages":[{"role":"system","content":"Be concise"},{"role":"user","content":"first question"}]}`)
	laterTurn := []byte(`{"model":"claude-sonnet","messages":[{"role":"system","content":"Be concise"},{"role":"user","content":"first question"},{"role":"assistant","content":"first answer"},{"role":"user","content":"follow up"}]}`)

	firstID := resolveCodeBuddyConversationID(newCodeBuddyHeaderTestContext(), firstTurn)
	laterID := resolveCodeBuddyConversationID(newCodeBuddyHeaderTestContext(), laterTurn)

	require.Equal(t, firstID, laterID)
	parsed, err := uuid.Parse(firstID)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(5), parsed.Version())
}

func TestResolveCodeBuddyConversationIDUsesRandomUUIDWithoutContentAnchor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"claude-sonnet"}`)

	firstID := resolveCodeBuddyConversationID(newCodeBuddyHeaderTestContext(), body)
	secondID := resolveCodeBuddyConversationID(newCodeBuddyHeaderTestContext(), body)

	_, firstErr := uuid.Parse(firstID)
	_, secondErr := uuid.Parse(secondID)
	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	require.NotEqual(t, firstID, secondID)
}

func TestSendCCUpstreamRequestPreservesCodeBuddyClientHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeSessionHTTPUpstream{}
	svc := openCodeSessionTestService()
	svc.httpUpstream = upstream
	account := &Account{ID: 42, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth}
	body := []byte(`{"model":"claude-sonnet","messages":[{"role":"user","content":"hello"}]}`)
	c := newCodeBuddyHeaderTestContext()
	c.Request.Header.Set("X-Conversation-ID", "client-conversation")
	c.Request.Header.Set("X-Agent-Intent", "review")

	resp, err := svc.sendCCUpstreamRequest(
		context.Background(), c, account, "https://copilot.tencent.com/v2/chat/completions",
		body, true, "token", "", "",
	)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotNil(t, upstream.request)
	require.Equal(t, "client-conversation", upstream.request.Header.Get("X-Conversation-ID"))
	require.Equal(t, "review", upstream.request.Header.Get("X-Agent-Intent"))
}

func TestSendCCUpstreamRequestDefaultsCodeBuddyAgentIntent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeSessionHTTPUpstream{}
	svc := openCodeSessionTestService()
	svc.httpUpstream = upstream
	account := &Account{ID: 43, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth}
	body := []byte(`{"model":"claude-sonnet","prompt_cache_key":"stable-session","messages":[{"role":"user","content":"hello"}]}`)
	c := newCodeBuddyHeaderTestContext()

	resp, err := svc.sendCCUpstreamRequest(
		context.Background(), c, account, "https://copilot.tencent.com/v2/chat/completions",
		body, true, "token", "", "",
	)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.NotNil(t, upstream.request)
	require.Equal(t, "stable-session", upstream.request.Header.Get("X-Conversation-ID"))
	require.Equal(t, "craft", upstream.request.Header.Get("X-Agent-Intent"))
}
