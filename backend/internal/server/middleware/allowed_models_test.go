//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// RequireAllowedModels 校验 Key 级模型白名单（api_keys.allowed_models）：
//   - 空白名单 = 不限制，直接放行（含无 model 的请求，如 Gemini model-in-path 由
//     后续 handler 校验）；
//   - 非空白名单：JSON body 的 model 字段必须命中；不命中返回 404 model_not_found
//     （与调度层 model_not_found 语义一致），并标记 ingress reject。
//
// 错误响应按协议分流：messages 走 Anthropic 格式，其余走 OpenAI 格式。
func newAllowedModelsRouter(apiKey *service.APIKey, cap *capture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Next()
		cap.reason, cap.rejected = GetIngressRejectReason(c)
	})
	router.Use(RequireAllowedModels())
	router.POST("/v1/messages", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	router.POST("/v1/chat/completions", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	router.POST("/v1/responses", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return router
}

type capture struct {
	rejected bool
	reason   IngressRejectReason
}

func do(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestRequireAllowedModelsEmptyWhitelistPasses(t *testing.T) {
	apiKey := &service.APIKey{ID: 1, Status: service.StatusActive}
	router := newAllowedModelsRouter(apiKey, &capture{})

	for _, body := range []string{"", `{"model":"gpt-5.2"}`, `{"model":""}`} {
		w := do(router, http.MethodPost, "/v1/messages", body)
		require.Equal(t, http.StatusOK, w.Code, "body: %s", body)
	}
}

func TestRequireAllowedModelsBlocksDisallowedModel(t *testing.T) {
	apiKey := &service.APIKey{
		ID:            1,
		Status:        service.StatusActive,
		AllowedModels: []string{"claude-sonnet-4-5", "claude-*"},
	}
	cap := &capture{}
	router := newAllowedModelsRouter(apiKey, cap)

	t.Run("anthropic protocol error shape and ingress reject", func(t *testing.T) {
		w := do(router, http.MethodPost, "/v1/messages", `{"model":"gpt-5.2"}`)
		require.Equal(t, http.StatusNotFound, w.Code)
		require.Contains(t, w.Body.String(), "model_not_found")
		require.Contains(t, w.Body.String(), "gpt-5.2")
		require.True(t, cap.rejected)
		require.Equal(t, IngressRejectModelNotAllowed, cap.reason)
	})

	t.Run("openai protocol error shape", func(t *testing.T) {
		w := do(router, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-5.2","messages":[]}`)
		require.Equal(t, http.StatusNotFound, w.Code)
		require.Contains(t, w.Body.String(), "model_not_found")
	})

	t.Run("whitelisted model passes", func(t *testing.T) {
		for _, body := range []string{`{"model":"claude-sonnet-4-5"}`, `{"model":"claude-opus-4"}`} {
			w := do(router, http.MethodPost, "/v1/messages", body)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", body)
		}
	})
}

func TestRequireAllowedModelsWildcardOnly(t *testing.T) {
	apiKey := &service.APIKey{
		ID:            1,
		Status:        service.StatusActive,
		AllowedModels: []string{"gpt-*"},
	}
	router := newAllowedModelsRouter(apiKey, &capture{})

	w := do(router, http.MethodPost, "/v1/chat/completions", `{"model":"gpt-5.2"}`)
	require.Equal(t, http.StatusOK, w.Code)

	w = do(router, http.MethodPost, "/v1/chat/completions", `{"model":"claude-sonnet-4-5"}`)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestRequireAllowedModelsMissingBodyOrModel(t *testing.T) {
	apiKey := &service.APIKey{
		ID:            1,
		Status:        service.StatusActive,
		AllowedModels: []string{"gpt-5.2"},
	}
	router := newAllowedModelsRouter(apiKey, &capture{})

	// body 缺失 / model 缺省：无法判定，交由后续 handler 的必填校验兜底，不在此拦截
	for _, body := range []string{"", `{"messages":[]}`} {
		w := do(router, http.MethodPost, "/v1/chat/completions", body)
		require.Equal(t, http.StatusOK, w.Code, "body: %s", body)
	}
}

func TestRequireAllowedModelsReusesBodyBudget(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, body := range []string{`{"model":"gpt-5.2","messages":[]}`, `{"messages":[]}`} {
			t.Run(path+"/"+body, func(t *testing.T) {
				// A short body uses its declared size plus one EOF-probe byte.
				// The budget fits one read but cannot accommodate a copied reread.
				limit := int64(len(body) + 1)
				budget := httputil.NewBodyMemoryBudget(limit)
				router := gin.New()
				router.Use(RequestBodyLimit(1024, budget), func(c *gin.Context) {
					c.Set(string(ContextKeyAPIKey), &service.APIKey{AllowedModels: []string{"gpt-5.2"}})
					c.Next()
				}, RequireAllowedModels())
				called := false
				router.POST(path, func(c *gin.Context) {
					called = true
					reread, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
					require.NoError(t, err)
					require.Equal(t, body, string(reread))
					require.Equal(t, int64(len(body)), c.Request.ContentLength)
					c.Status(http.StatusNoContent)
				})

				w := do(router, http.MethodPost, path, body)
				require.True(t, called)
				require.Equal(t, http.StatusNoContent, w.Code)
				ctx, release := httputil.WithRequestBodyMemoryBudget(context.Background(), budget)
				defer release()
				require.NoError(t, httputil.ReserveRequestBodyMemory(ctx, limit), "the completed request must release its entire reservation")
			})
		}
	}
}

func TestRequireAllowedModelsRejectsExhaustedBodyBudget(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			budget := httputil.NewBodyMemoryBudget(1024)
			ctx, release := httputil.WithRequestBodyMemoryBudget(context.Background(), budget)
			defer release()
			require.NoError(t, httputil.ReserveRequestBodyMemory(ctx, 1024))
			router := gin.New()
			router.Use(RequestBodyLimit(1024, budget), func(c *gin.Context) {
				c.Set(string(ContextKeyAPIKey), &service.APIKey{AllowedModels: []string{"gpt-5.2"}})
				c.Next()
			}, RequireAllowedModels())
			called := false
			router.POST(path, func(c *gin.Context) { called = true })
			w := do(router, http.MethodPost, path, `{"model":"gpt-5.2"}`)
			require.False(t, called)
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			require.Equal(t, "1", w.Header().Get("Retry-After"))
			var payload map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
			failure := payload["error"].(map[string]any)
			require.Contains(t, failure["message"], "buffering capacity is exhausted")
			if path == "/v1/messages" {
				require.Equal(t, "error", payload["type"])
				require.Equal(t, "api_error", failure["type"])
			} else {
				require.NotContains(t, payload, "type")
				require.Equal(t, "invalid_request_error", failure["type"])
			}
		})
	}
}

type allowedModelsFailingBody struct {
	err    error
	cancel context.CancelFunc
}

func (b allowedModelsFailingBody) Read([]byte) (int, error) {
	if b.cancel != nil {
		b.cancel()
	}
	return 0, b.err
}

func TestRequireAllowedModelsClassifiesBodyReadFailures(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses"} {
		for _, tc := range []struct {
			name         string
			err          error
			cancelBefore bool
			cancelOnRead bool
			status       int
			message      string
		}{
			{name: "cancelled", cancelBefore: true, status: 499},
			{name: "timeout", err: os.ErrDeadlineExceeded, status: http.StatusRequestTimeout, message: "Timed out reading request body"},
			{name: "timeout_also_cancels_context", err: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, cancelOnRead: true, status: http.StatusRequestTimeout, message: "Timed out reading request body"},
			{name: "truncated", err: io.ErrUnexpectedEOF, status: http.StatusBadRequest, message: "Failed to read request body"},
			{name: "oversized", status: http.StatusRequestEntityTooLarge, message: "Request body is too large"},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				router := gin.New()
				router.Use(RequestBodyLimit(8), func(c *gin.Context) {
					c.Set(string(ContextKeyAPIKey), &service.APIKey{AllowedModels: []string{"gpt-5.2"}})
					c.Next()
				}, RequireAllowedModels())
				called := false
				router.POST(path, func(c *gin.Context) { called = true })
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5.2"}`)).WithContext(ctx)
				if tc.cancelBefore {
					cancel()
				}
				if tc.err != nil {
					reader := allowedModelsFailingBody{err: tc.err}
					if tc.cancelOnRead {
						reader.cancel = cancel
					}
					req.Body = io.NopCloser(reader)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				require.False(t, called)
				require.Equal(t, tc.status, w.Code)
				require.Empty(t, w.Header().Get("Retry-After"))
				if tc.cancelBefore {
					require.Empty(t, w.Body.String())
				} else {
					expected, err := json.Marshal(map[string]any{"error": map[string]string{"type": "invalid_request_error", "message": tc.message}})
					require.NoError(t, err)
					require.JSONEq(t, string(expected), w.Body.String())
				}
			})
		}
	}
}
