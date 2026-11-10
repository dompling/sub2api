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

func codeBuddyMetadataAccount(id int64, meta string, mapping map[string]any) service.Account {
	return service.Account{
		ID: id, Platform: service.PlatformCodeBuddy, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{
			"models": []string{"native"}, "model_mapping": mapping,
			"codebuddy_models_meta": meta,
		},
	}
}

func requestCodeBuddyMetadata(t *testing.T, accounts []service.Account, keyModels []string, retrieve string) []map[string]any {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(447)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		groupID: accounts,
		448:     {codeBuddyMetadataAccount(99, `[{"id":"native","supports_reasoning":false}]`, nil)},
	}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	if retrieve != "" {
		c.Params = gin.Params{{Key: "model", Value: retrieve}}
	}
	c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
		AllowedModels: keyModels,
		Group: &service.Group{
			ID: groupID, Platform: service.PlatformCodeBuddy,
			ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"alias", "native", "missing"}},
		},
	})
	h.Models(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	if retrieve != "" {
		var model map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &model))
		return []map[string]any{model}
	}
	var result struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	return result.Data
}

func TestGatewayModels_CodeBuddyMetadataPreservesAliasesAndExplicitFalse(t *testing.T) {
	account := codeBuddyMetadataAccount(1, `[
		{"id":"native","supports_reasoning":true,"supports_images":true,"max_input_tokens":128000,"max_output_tokens":8192,"access_token":"must-not-leak"},
		{"id":"plain","supports_reasoning":false,"supports_images":false}
	]`, map[string]any{"alias": "plain", "native": "native"})
	models := requestCodeBuddyMetadata(t, []service.Account{account}, nil, "")
	require.Len(t, models, 3)
	require.Equal(t, "alias", models[0]["id"])
	require.Equal(t, "alias", models[0]["display_name"])
	require.Equal(t, false, models[0]["supports_reasoning"])
	require.Equal(t, false, models[0]["supports_images"])
	require.Equal(t, "native", models[1]["id"])
	require.Equal(t, true, models[1]["supports_reasoning"])
	require.Equal(t, true, models[1]["supports_images"])
	require.Equal(t, float64(128000), models[1]["max_input_tokens"])
	require.Equal(t, float64(8192), models[1]["max_output_tokens"])
	require.NotContains(t, models[1], "access_token")
	require.NotContains(t, models[2], "supports_reasoning")
	require.NotContains(t, models[2], "max_input_tokens")
	filtered := requestCodeBuddyMetadata(t, []service.Account{account}, []string{"alias"}, "")
	require.Len(t, filtered, 1)
	require.Equal(t, false, filtered[0]["supports_reasoning"])
	retrieved := requestCodeBuddyMetadata(t, []service.Account{account}, nil, "alias")
	require.Equal(t, models[0], retrieved[0])
}

func TestGatewayModels_CodeBuddyMetadataRequiresAgreementAcrossEligibleAccounts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		second    string
		want      any
		wantLimit any
	}{
		{"consistent", `[{"id":"native","supports_reasoning":true,"max_input_tokens":64000}]`, true, float64(64000)},
		{"conflicting", `[{"id":"native","supports_reasoning":false,"max_input_tokens":64000}]`, nil, float64(64000)},
		{"missing-flag", `[{"id":"native","max_input_tokens":64000}]`, nil, float64(64000)},
		{"missing-model", `[{"id":"another","supports_reasoning":true}]`, nil, nil},
		{"invalid-flag", `[{"id":"native","supports_reasoning":"false"}]`, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts := []service.Account{
				codeBuddyMetadataAccount(1, `[{"id":"native","supports_reasoning":true,"max_input_tokens":128000}]`, nil),
				codeBuddyMetadataAccount(2, tc.second, nil),
			}
			models := requestCodeBuddyMetadata(t, accounts, []string{"native"}, "")
			require.Len(t, models, 1)
			require.Equal(t, tc.want, models[0]["supports_reasoning"])
			require.Equal(t, tc.wantLimit, models[0]["max_input_tokens"])
		})
	}
}

func TestGatewayModels_CodeBuddyMetadataIgnoresIneligibleAccounts(t *testing.T) {
	active := codeBuddyMetadataAccount(1, `[{"id":"native","supports_reasoning":true}]`, nil)
	inactive := codeBuddyMetadataAccount(2, `[{"id":"native","supports_reasoning":false}]`, nil)
	inactive.Schedulable = false
	otherModel := codeBuddyMetadataAccount(3, `[{"id":"native","supports_reasoning":false}]`, map[string]any{"other": "native"})
	otherPlatform := codeBuddyMetadataAccount(4, `[{"id":"native","supports_reasoning":false}]`, nil)
	otherPlatform.Platform = service.PlatformAnthropic
	models := requestCodeBuddyMetadata(t, []service.Account{active, inactive, otherModel, otherPlatform}, []string{"native"}, "")
	require.Len(t, models, 1)
	require.Equal(t, true, models[0]["supports_reasoning"])
}

func TestGatewayModels_CodeBuddyMetadataFollowsWildcardAccountMappings(t *testing.T) {
	account := codeBuddyMetadataAccount(1, `[{"id":"native","supports_reasoning":true}]`, map[string]any{"ali*": "native"})
	models := requestCodeBuddyMetadata(t, []service.Account{account}, []string{"alias"}, "")
	require.Len(t, models, 1)
	require.Equal(t, "alias", models[0]["id"])
	require.Equal(t, true, models[0]["supports_reasoning"])
}

func TestGatewayModels_CodeBuddyMetadataDoesNotEnrichCompositeSources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(449)
	account := codeBuddyMetadataAccount(1, `[{"id":"native","supports_reasoning":true}]`, map[string]any{"native": "native"})
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{groupID: {account}}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
		Group: &service.Group{ID: groupID, Platform: service.PlatformComposite},
	})
	h.Models(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	for _, model := range got.Data {
		require.NotContains(t, model, "supports_reasoning")
	}
}
