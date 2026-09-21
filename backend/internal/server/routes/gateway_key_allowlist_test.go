package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayRoutesKeyAndGroupAllowlistBothApply(t *testing.T) {
	for _, tc := range []struct {
		name         string
		groupEnabled bool
		groupModels  []string
		keyModels    []string
		wantMessage  string
	}{
		{"key-only", false, nil, []string{"hy3"}, "not available for this API key"},
		{"key-restricts-group", true, []string{"hy*"}, []string{"hy3"}, "not available for this API key"},
		{"group-restricts-key", true, []string{"hy3"}, []string{"hy*"}, "not available for this group"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := newGatewayRoutesTestRouterWithGroup(
				allowlistGroup(service.PlatformCodeBuddy, tc.groupEnabled, tc.groupModels...), tc.keyModels...,
			)
			for _, path := range []string{
				"/v1/messages", "/v1/chat/completions", "/v1/responses",
				"/chat/completions", "/responses", "/backend-api/codex/responses",
				"/antigravity/v1/messages",
			} {
				t.Run(path, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"hy4-preview","messages":[]}`))
					req.Header.Set("Content-Type", "application/json")
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, req)
					require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
					require.Contains(t, rec.Body.String(), tc.wantMessage)
				})
			}
		})
	}
}
