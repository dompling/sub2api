package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	wsBridgeTestUserID   = int64(501)
	wsBridgeTestAPIKeyID = int64(601)
	wsBridgeTestGroupID  = int64(701)
)

// wsBridgeTestServer 是 ws_bridge 测试用的上游 WS：读取 response.create 后等待 delay，
// 再依次写出 events。rejectStatus 非零时直接以该状态码拒绝握手。
type wsBridgeTestServer struct {
	*httptest.Server
	requests     atomic.Int32
	lastStream   atomic.Bool
	rejectStatus int
	delay        time.Duration
}

func newWSBridgeTestServer(t *testing.T, rejectStatus int, events ...string) *wsBridgeTestServer {
	t.Helper()
	return newWSBridgeTestServerWithDelay(t, rejectStatus, 0, events...)
}

func newWSBridgeTestServerWithDelay(t *testing.T, rejectStatus int, delay time.Duration, events ...string) *wsBridgeTestServer {
	t.Helper()
	srv := &wsBridgeTestServer{rejectStatus: rejectStatus, delay: delay}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if srv.rejectStatus != 0 {
			http.Error(w, "upstream unavailable", srv.rejectStatus)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket failed: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			var request map[string]any
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			srv.requests.Add(1)
			srv.lastStream.Store(gjson.Get(requestToJSONString(request), "stream").Bool())
			if srv.delay > 0 {
				time.Sleep(srv.delay)
			}
			for _, event := range events {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
					t.Errorf("write ws event failed: %v", err)
					return
				}
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newWSBridgeTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeCtxPool
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 30
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 10
	cfg.Gateway.OpenAIWS.RetryTotalBudgetMS = 1
	return cfg
}

func newWSBridgeTestAccount(baseURL, mode string) *Account {
	return &Account{
		ID:          8101,
		Name:        "openai-ws-bridge",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Credentials: map[string]any{
			"api_key":  "sk-test",
			"base_url": baseURL,
		},
		Extra: map[string]any{
			"openai_apikey_responses_websockets_v2_mode": mode,
		},
	}
}

func newWSBridgeTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("User-Agent", "custom-client/1.0")
	groupID := wsBridgeTestGroupID
	c.Set("api_key", &APIKey{ID: wsBridgeTestAPIKeyID, GroupID: &groupID})
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	SetOpenAIHTTPResponseOwner(c, wsBridgeTestUserID, wsBridgeTestAPIKeyID)
	return c, rec
}

func newWSBridgeTestService(cfg *config.Config, upstream HTTPUpstream) *OpenAIGatewayService {
	return &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     upstream,
		cache:            &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
	}
}

func newWSBridgeHTTPSSEUpstream() *httpUpstreamRecorder {
	return &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_http_1\",\"model\":\"gpt-5.1\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
			)),
		},
	}
}

const (
	wsBridgeTestCreatedEvent   = `{"type":"response.created","response":{"id":"resp_ws_1","model":"gpt-5.1"}}`
	wsBridgeTestItemDoneEvent  = `{"type":"response.output_item.done","output_index":0,"item":{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hi"}]}}`
	wsBridgeTestCompletedEvent = `{"type":"response.completed","response":{"id":"resp_ws_1","model":"gpt-5.1","output":[],"usage":{"input_tokens":12,"output_tokens":7}}}`
)

func TestShouldUseOpenAIWSBridgeForHTTPIngress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsDecision := OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2, Reason: "ws_v2_mode_ws_bridge"}
	streamBody := []byte(`{"model":"gpt-5.1","stream":true,"input":"hello"}`)

	tests := []struct {
		name          string
		mutateCfg     func(*config.Config)
		mutateAccount func(*Account)
		path          string
		setup         func(*gin.Context)
		decision      *OpenAIWSProtocolDecision
		body          []byte
		responsesLite bool
		want          bool
	}{
		{name: "streaming ws_bridge request", want: true},
		{name: "ws client transport", setup: func(c *gin.Context) { SetOpenAIClientTransport(c, OpenAIClientTransportWS) }},
		{name: "mode router disabled", mutateCfg: func(cfg *config.Config) { cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = false }},
		{name: "ctx_pool account", mutateAccount: func(a *Account) {
			a.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
		}},
		{name: "ws_bridge as ingress default", mutateCfg: func(cfg *config.Config) {
			cfg.Gateway.OpenAIWS.IngressModeDefault = OpenAIWSIngressModeWSBridge
		}, mutateAccount: func(a *Account) {
			delete(a.Extra, "openai_apikey_responses_websockets_v2_mode")
		}, want: true},
		{name: "resolver chose http", decision: &OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportHTTPSSE}},
		{name: "non-streaming request", body: []byte(`{"model":"gpt-5.1","stream":false,"input":"hello"}`)},
		{name: "stream omitted", body: []byte(`{"model":"gpt-5.1","input":"hello"}`)},
		{name: "background request", body: []byte(`{"model":"gpt-5.1","stream":true,"background":true,"input":"hello"}`)},
		{name: "compact subpath", path: "/openai/v1/responses/compact"},
		{name: "cancel subpath", path: "/openai/v1/responses/resp_1/cancel"},
		{name: "native compaction v2", setup: MarkOpenAINativeCompactionV2},
		{name: "responses lite", responsesLite: true},
		{name: "account passthrough", mutateAccount: func(a *Account) { a.Extra["openai_passthrough"] = true }},
		{name: "payload above http bridge threshold", mutateCfg: func(cfg *config.Config) {
			cfg.Gateway.OpenAIWS.HTTPBridgeEnabled = true
			cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes = 8
		}},
		{name: "continuation above threshold stays on ws", mutateCfg: func(cfg *config.Config) {
			cfg.Gateway.OpenAIWS.HTTPBridgeEnabled = true
			cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes = 8
		}, body: []byte(`{"model":"gpt-5.1","stream":true,"previous_response_id":"resp_prev","input":"hello"}`), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newWSBridgeTestConfig()
			if tt.mutateCfg != nil {
				tt.mutateCfg(cfg)
			}
			account := newWSBridgeTestAccount("https://api.openai.com", OpenAIWSIngressModeWSBridge)
			if tt.mutateAccount != nil {
				tt.mutateAccount(account)
			}
			path := tt.path
			if path == "" {
				path = "/openai/v1/responses"
			}
			c, _ := newWSBridgeTestContext(path)
			if tt.setup != nil {
				tt.setup(c)
			}
			decision := wsDecision
			if tt.decision != nil {
				decision = *tt.decision
			}
			body := tt.body
			if body == nil {
				body = streamBody
			}
			svc := newWSBridgeTestService(cfg, nil)
			got := svc.shouldUseOpenAIWSBridgeForHTTPIngress(context.Background(), c, account, decision, body, tt.responsesLite)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOpenAIWSBridge_Forward_StreamingHTTPIngressUsesUpstreamWS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsServer := newWSBridgeTestServer(t, 0, wsBridgeTestCreatedEvent, wsBridgeTestItemDoneEvent, wsBridgeTestCompletedEvent)
	cfg := newWSBridgeTestConfig()
	upstream := newWSBridgeHTTPSSEUpstream()
	svc := newWSBridgeTestService(cfg, upstream)
	account := newWSBridgeTestAccount(wsServer.URL, OpenAIWSIngressModeWSBridge)
	c, rec := newWSBridgeTestContext("/openai/v1/responses")

	body := []byte(`{"model":"gpt-5.1","stream":true,"input":[{"type":"input_text","text":"hello"}]}`)
	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.OpenAIWSMode)
	require.True(t, result.Stream)
	require.Equal(t, "resp_ws_1", result.ResponseID)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Nil(t, upstream.lastReq, "ws_bridge 流式 HTTP 请求不应命中 HTTP 上游")
	require.Equal(t, int32(1), wsServer.requests.Load())
	require.True(t, wsServer.lastStream.Load())

	reason, _ := c.Get("openai_ws_transport_reason")
	require.Equal(t, "ws_v2_mode_ws_bridge", reason)
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))

	var completed string
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if data, ok := strings.CutPrefix(line, "data: "); ok && gjson.Get(data, "type").String() == "response.completed" {
			completed = data
		}
	}
	require.NotEmpty(t, completed, "客户端应收到 response.completed")
	require.Equal(t, "msg_1", gjson.Get(completed, "response.output.0.id").String(), "终态事件的空 output 应按 output_item.done 重建")

	owned, ownerErr := svc.ValidateOpenAIHTTPResponseOwner(context.Background(), wsBridgeTestGroupID, "resp_ws_1", wsBridgeTestUserID, wsBridgeTestAPIKeyID)
	require.NoError(t, ownerErr)
	require.True(t, owned, "ws_bridge 响应应绑定 HTTP 续接归属")
}

func TestOpenAIWSBridge_Forward_CtxPoolHTTPIngressStaysHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsServer := newWSBridgeTestServer(t, 0, wsBridgeTestCreatedEvent, wsBridgeTestCompletedEvent)
	cfg := newWSBridgeTestConfig()
	upstream := newWSBridgeHTTPSSEUpstream()
	svc := newWSBridgeTestService(cfg, upstream)
	account := newWSBridgeTestAccount(wsServer.URL, OpenAIWSIngressModeCtxPool)
	c, _ := newWSBridgeTestContext("/openai/v1/responses")

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.OpenAIWSMode)
	require.NotNil(t, upstream.lastReq)
	require.Zero(t, wsServer.requests.Load())
	reason, _ := c.Get("openai_ws_transport_reason")
	require.Equal(t, "client_protocol_http", reason)
}

func TestOpenAIWSBridge_Forward_NonStreamingHTTPIngressStaysHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsServer := newWSBridgeTestServer(t, 0, wsBridgeTestCreatedEvent, wsBridgeTestCompletedEvent)
	cfg := newWSBridgeTestConfig()
	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_http_1","usage":{"input_tokens":1,"output_tokens":1}}`)),
		},
	}
	svc := newWSBridgeTestService(cfg, upstream)
	account := newWSBridgeTestAccount(wsServer.URL, OpenAIWSIngressModeWSBridge)
	c, _ := newWSBridgeTestContext("/openai/v1/responses")

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.1","stream":false,"input":"hello"}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.OpenAIWSMode)
	require.NotNil(t, upstream.lastReq)
	require.Zero(t, wsServer.requests.Load())
}

func TestOpenAIWSBridge_Forward_RateLimitEventBeforeOutputFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsServer := newWSBridgeTestServer(t, 0,
		wsBridgeTestCreatedEvent,
		`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Rate limit reached"}}`,
	)
	cfg := newWSBridgeTestConfig()
	upstream := newWSBridgeHTTPSSEUpstream()
	svc := newWSBridgeTestService(cfg, upstream)
	account := newWSBridgeTestAccount(wsServer.URL, OpenAIWSIngressModeWSBridge)
	c, rec := newWSBridgeTestContext("/openai/v1/responses")

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "首输出前的 429 应交给 handler 切号: %v", err)
	require.Equal(t, http.StatusTooManyRequests, failoverErr.StatusCode)
	require.False(t, c.Writer.Written(), "切号前不应写出任何响应")
	require.Empty(t, rec.Body.String())
	require.Empty(t, c.Writer.Header().Get("Content-Type"), "切号前应清除本次 attempt 的 SSE 头")
	require.Empty(t, c.Writer.Header().Get(openAICodexTurnStateHeader))
	require.Nil(t, upstream.lastReq)
}

func TestOpenAIWSBridge_Forward_DialFailureFailsOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsServer := newWSBridgeTestServer(t, http.StatusServiceUnavailable)
	cfg := newWSBridgeTestConfig()
	upstream := newWSBridgeHTTPSSEUpstream()
	svc := newWSBridgeTestService(cfg, upstream)
	account := newWSBridgeTestAccount(wsServer.URL, OpenAIWSIngressModeWSBridge)
	c, rec := newWSBridgeTestContext("/openai/v1/responses")

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "WS 拨号失败应交给 handler 切号: %v", err)
	require.GreaterOrEqual(t, failoverErr.StatusCode, http.StatusInternalServerError)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
	require.Nil(t, upstream.lastReq, "ws_bridge 不回退 HTTP 上游，由 handler 换号")
}

func TestOpenAIWSBridge_Forward_PreviousResponseNotFoundIsNotSilentlyDropped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsServer := newWSBridgeTestServer(t, 0,
		`{"type":"error","error":{"type":"invalid_request_error","code":"previous_response_not_found","message":"Previous response with id 'resp_prev' not found."}}`,
	)
	cfg := newWSBridgeTestConfig()
	upstream := newWSBridgeHTTPSSEUpstream()
	svc := newWSBridgeTestService(cfg, upstream)
	account := newWSBridgeTestAccount(wsServer.URL, OpenAIWSIngressModeWSBridge)
	c, rec := newWSBridgeTestContext("/openai/v1/responses")

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.1","stream":true,"previous_response_id":"resp_prev","input":"hello"}`))
	require.Error(t, err)
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr), "续链不存在是请求级错误，不应换号")
	require.Equal(t, int32(1), wsServer.requests.Load(), "HTTP 客户端不应静默去掉 previous_response_id 重放")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.Nil(t, upstream.lastReq)
}

func TestOpenAIWSBridge_Forward_KeepaliveBeforeOutputDoesNotBlockFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	wsServer := newWSBridgeTestServerWithDelay(t, 0, 1500*time.Millisecond,
		`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"Rate limit reached"}}`,
	)
	cfg := newWSBridgeTestConfig()
	cfg.Gateway.StreamKeepaliveInterval = 1
	upstream := newWSBridgeHTTPSSEUpstream()
	svc := newWSBridgeTestService(cfg, upstream)
	account := newWSBridgeTestAccount(wsServer.URL, OpenAIWSIngressModeWSBridge)
	c, rec := newWSBridgeTestContext("/openai/v1/responses")

	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.1","stream":true,"input":"hello"}`))
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "保活后、首输出前的 429 仍应切号: %v", err)
	require.True(t, strings.HasPrefix(rec.Body.String(), ":\n\n"), "首输出前应写出 SSE 保活注释")
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c), "保活字节不算语义输出，handler 仍可切号")
}
