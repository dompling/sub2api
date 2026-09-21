package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayModels_CodeBuddyCombinesGroupAndKeyAllowlists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name        string
		groupModels []string
		keyModels   []string
		want        []string
	}{
		{"live-model-and-wildcard", []string{"hy3-*", "hy4-preview"}, nil, []string{"hy3-lite", "hy4-preview"}},
		{"key-restricts-live-models", []string{"hy3-*", "hy4-preview"}, []string{"hy4-*"}, []string{"hy4-preview"}},
		{"key-with-no-group-list-match", []string{"hy3-*"}, []string{"unavailable"}, []string{}},
		{"key-with-no-default-match", nil, []string{"unavailable"}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(44)
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{
				byGroup: map[int64][]service.Account{
					groupID: {{
						ID: 1, Platform: service.PlatformCodeBuddy, Type: service.AccountTypeOAuth,
						Credentials: map[string]any{"model_mapping": map[string]any{"hy3-lite": "hy3-lite"}},
					}},
				},
			})
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				AllowedModels: tc.keyModels,
				Group: &service.Group{
					ID: groupID, Platform: service.PlatformCodeBuddy,
					ModelAllowlist: service.GroupModelAllowlist{Enabled: len(tc.groupModels) > 0, Models: tc.groupModels},
				},
			})
			h.Models(c)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var got gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			require.Equal(t, tc.want, modelIDsForTest(got.Data))
		})
	}
}
