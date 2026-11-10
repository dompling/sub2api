//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codebuddy"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodeBuddyRequestSettingsAcrossProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		model        string
		meta         string
		effort       string
		summary      string
		budget       int
		disabled     bool
		nonReasoning bool
		output       int
		wantEffort   string
		wantOutput   int64
	}{
		{name: "model_default", model: "glm-5.2", meta: `"defaultEffort":"high","effort":"medium"`, wantEffort: "high"},
		{name: "model_legacy_effort", model: "glm-5.1", meta: `"effort":"medium"`, wantEffort: "medium"},
		{name: "explicit_xhigh", model: "glm-5.2", meta: `"defaultEffort":"high"`, effort: "xhigh", wantEffort: "xhigh"},
		{name: "flash_low", model: "glm-5.3-flash", meta: `"defaultEffort":"high"`, effort: "low", wantEffort: "low"},
		{name: "disabled_overrides_default_and_effort", model: "glm-5.2", meta: `"defaultEffort":"high"`, effort: "xhigh", disabled: true, wantEffort: "none"},
		{name: "disabled_overrides_budget_and_summary", model: "glm-5.2", meta: `"defaultEffort":"high"`, effort: "xhigh", budget: 8192, summary: "auto", disabled: true, wantEffort: "none"},
		{name: "summary_override", model: "glm-5.2", meta: `"defaultEffort":"high"`, summary: "detailed", wantEffort: "high"},
		{name: "explicit_budget", model: "glm-5.2", meta: `"defaultEffort":"high"`, budget: 8192, wantEffort: "high"},
		{name: "output_limit", model: "glm-5.2", meta: `"defaultEffort":"high"`, output: 100000, wantOutput: 64000, wantEffort: "high"},
		{name: "smaller_output_preserved", model: "glm-5.2", meta: `"defaultEffort":"high"`, output: 1024, wantOutput: 1024, wantEffort: "high"},
		{name: "declared_xhigh", model: "glm-5.2", meta: `"defaultEffort":"high","supportedEfforts":["high","xhigh"]`, effort: "max", wantEffort: "xhigh"},
		{name: "declared_max", model: "glm-5.3-flash", meta: `"defaultEffort":"high","supportedEfforts":["low","high","max"]`, effort: "max", wantEffort: "max"},
		{name: "unsupported_medium", model: "glm-5.2", meta: `"defaultEffort":"high","supportedEfforts":["high","xhigh"]`, effort: "medium", wantEffort: "high"},
		{name: "nonreasoning_model", model: "plain-model", meta: `"defaultEffort":"high"`, nonReasoning: true},
	}
	for _, tt := range tests {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages"} {
			t.Run(tt.name+path, func(t *testing.T) {
				request := map[string]any{"model": tt.model, "stream": false}
				if path == "/v1/responses" {
					request["input"] = "hello"
					if tt.effort != "" || tt.summary != "" {
						reasoning := map[string]string{}
						if tt.effort != "" {
							reasoning["effort"] = tt.effort
						}
						if tt.summary != "" {
							reasoning["summary"] = tt.summary
						}
						request["reasoning"] = reasoning
					}
					if tt.output > 0 {
						request["max_output_tokens"] = tt.output
					}
				} else {
					request["messages"] = []map[string]string{{"role": "user", "content": "hello"}}
					if tt.output > 0 {
						request["max_tokens"] = tt.output
					}
					if tt.summary != "" {
						request["reasoning_summary"] = tt.summary
					}
					if tt.effort != "" {
						if path == "/v1/messages" {
							effort := tt.effort
							if effort == "xhigh" {
								effort = "max"
							}
							request["output_config"] = map[string]string{"effort": effort}
						} else {
							request["reasoning_effort"] = tt.effort
						}
					}
				}
				if path == "/v1/messages" && tt.output == 0 {
					request["max_tokens"] = 32
				}
				if tt.budget > 0 {
					if path == "/v1/messages" {
						request["thinking"] = map[string]any{"type": "enabled", "budget_tokens": tt.budget}
					} else {
						request["max_thinking_tokens"] = tt.budget
					}
				}
				if tt.disabled {
					request["thinking"] = map[string]string{"type": "disabled"}
				}
				body, err := json.Marshal(request)
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(codeBuddyChatCompletionsSSE(tt.model, "synthetic", "ok"))),
				}}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				account := codeBuddyRawChatCompletionsTestAccount()
				account.Credentials["codebuddy_models_meta"] = `[{"id":"` + tt.model + `","supports_reasoning":` + strconv.FormatBool(!tt.nonReasoning) + `,"max_output_tokens":64000,"reasoning":{` + tt.meta + `}}]`
				var result *OpenAIForwardResult
				switch path {
				case "/v1/messages":
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				case "/v1/responses":
					result, err = svc.Forward(context.Background(), c, account, body)
				default:
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				}
				require.NoError(t, err)
				require.Len(t, upstream.requests, 1)
				require.Equal(t, tt.wantEffort, gjson.GetBytes(upstream.lastBody, "reasoning_effort").String())
				if tt.wantEffort != "none" && tt.wantEffort != "" {
					summary := tt.summary
					if summary == "" {
						summary = "auto"
					}
					require.Equal(t, summary, gjson.GetBytes(upstream.lastBody, "reasoning_summary").String())
				} else {
					require.False(t, gjson.GetBytes(upstream.lastBody, "reasoning_summary").Exists())
				}
				if tt.budget > 0 && !tt.disabled {
					require.EqualValues(t, tt.budget, gjson.GetBytes(upstream.lastBody, "max_thinking_tokens").Int())
				} else if tt.disabled {
					require.False(t, gjson.GetBytes(upstream.lastBody, "max_thinking_tokens").Exists())
				}
				if tt.wantOutput > 0 {
					field := "max_tokens"
					if path != "/v1/chat/completions" {
						field = "max_completion_tokens"
					}
					require.Equal(t, tt.wantOutput, gjson.GetBytes(upstream.lastBody, field).Int())
				}
				if tt.wantEffort == "" {
					require.Nil(t, result.ReasoningEffort)
				} else {
					require.NotNil(t, result.ReasoningEffort)
					require.Equal(t, tt.wantEffort, *result.ReasoningEffort)
				}
			})
		}
	}
}

func TestCodeBuddySupportedEffortUsesProductDeclaration(t *testing.T) {
	for _, tt := range []struct {
		effort    string
		supported []string
		want      string
	}{
		{"xhigh", []string{"low", "high", "max"}, "high"},
		{"max", []string{"high", "xhigh"}, "xhigh"},
		{"medium", []string{"high", "xhigh"}, "high"},
		{"low", []string{"low", "high", "max"}, "low"},
		{"xhigh", nil, "xhigh"},
		{"max", []string{"unknown"}, "max"},
	} {
		require.Equal(t, tt.want, codeBuddySupportedEffort(tt.effort, tt.supported))
	}
}

func TestCodeBuddyRequestSettingsWithoutReasoningMetadata(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := codeBuddyRawChatCompletionsTestAccount()
	account.ID = 99123456
	defer codeBuddyMetaCache.Delete(account.ID)
	var requests atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(proxyURL.Port())
	require.NoError(t, err)
	account.Proxy = &Proxy{Protocol: "http", Host: proxyURL.Hostname(), Port: port}
	for _, tt := range []struct{ name, meta, body, want string }{
		{"missing_metadata", "", `{"model":"glm-5.2"}`, ""},
		{"legacy_metadata", `[{"id":"glm-5.2","supports_reasoning":true}]`, `{"model":"glm-5.2"}`, ""},
		{"explicit_without_metadata", "", `{"model":"glm-5.2","reasoning_effort":"xhigh"}`, "xhigh"},
		{"unknown_model", `[{"id":"hy4","supports_reasoning":true,"reasoning":{"defaultEffort":"high"}}]`, `{"model":"glm-5.2"}`, ""},
		{"malformed_metadata", "{", `{"model":"glm-5.2","max_completion_tokens":100000}`, ""},
		{"nonreasoning_model", `[{"id":"glm-5.2","supports_reasoning":false,"reasoning":{"defaultEffort":"high"}}]`, `{"model":"glm-5.2"}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account.Credentials["codebuddy_models_meta"] = tt.meta
			result := svc.applyCodeBuddyRequestSettings([]byte(tt.body), []byte(tt.body), account)
			require.Equal(t, tt.want, gjson.GetBytes(result, "reasoning_effort").String())
		})
	}
	require.Zero(t, requests.Load(), "request normalization must not fetch config when optional metadata is missing")
}

func TestCodeBuddyRequestSettingsDoesNotAddThinkingForNonreasoningModels(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := codeBuddyRawChatCompletionsTestAccount()
	account.Credentials["codebuddy_models_meta"] = `[{"id":"plain-model","supports_reasoning":false}]`
	converted := []byte(`{"model":"plain-model","reasoning_effort":"medium"}`)
	body := svc.applyCodeBuddyRequestSettings(converted, []byte(`{"model":"plain-model","messages":[]}`), account)
	require.False(t, gjson.GetBytes(body, "reasoning_effort").Exists())
	require.False(t, gjson.GetBytes(body, "reasoning_summary").Exists())
	explicit := []byte(`{"model":"plain-model","output_config":{"effort":"low"}}`)
	body = svc.applyCodeBuddyRequestSettings(converted, explicit, account)
	require.Equal(t, "low", gjson.GetBytes(body, "reasoning_effort").String())
}

func TestCodeBuddyRequestSettingsUsesOnlyUnexpiredCachedMetadata(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := codeBuddyRawChatCompletionsTestAccount()
	account.ID = 99123457
	defer codeBuddyMetaCache.Delete(account.ID)
	body := []byte(`{"model":"hy4","max_completion_tokens":100000}`)
	meta := codebuddy.ModelInfo{ID: "hy4", MaxOutputTokens: 64000, SupportsReasoning: true, Reasoning: &codebuddy.ReasoningConfig{DefaultEffort: "high"}}
	codeBuddyMetaCache.Store(account.ID, codeBuddyMetaCacheEntry{metas: []codebuddy.ModelInfo{meta}, exp: time.Now().Add(time.Minute)})
	result := svc.applyCodeBuddyRequestSettings(body, body, account)
	require.Equal(t, "high", gjson.GetBytes(result, "reasoning_effort").String())
	require.EqualValues(t, 64000, gjson.GetBytes(result, "max_completion_tokens").Int())
	codeBuddyMetaCache.Store(account.ID, codeBuddyMetaCacheEntry{metas: []codebuddy.ModelInfo{meta}, exp: time.Now().Add(-time.Minute)})
	result = svc.applyCodeBuddyRequestSettings(body, body, account)
	require.Equal(t, string(body), string(result))
}

func TestCodeBuddyOutputLimitCoversBothTokenFields(t *testing.T) {
	body, changed := clampCodeBuddyOutputTokens([]byte(`{"max_tokens":100000,"max_completion_tokens":90000}`), 64000)
	require.True(t, changed)
	require.EqualValues(t, 64000, gjson.GetBytes(body, "max_tokens").Int())
	require.EqualValues(t, 64000, gjson.GetBytes(body, "max_completion_tokens").Int())
	svc := &OpenAIGatewayService{}
	account := codeBuddyRawChatCompletionsTestAccount()
	account.Credentials["codebuddy_models_meta"] = `[{"id":"glm-5.2","max_output_tokens":64000}]`
	body, changed = svc.applyCodeBuddyMaxTokensLimit(context.Background(), []byte(`{"model":"glm-5.2","max_completion_tokens":100000}`), account, "synthetic-token")
	require.True(t, changed)
	require.EqualValues(t, 64000, gjson.GetBytes(body, "max_completion_tokens").Int())
}
