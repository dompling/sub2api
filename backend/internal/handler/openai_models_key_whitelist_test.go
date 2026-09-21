package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const pinnedKeyWhitelistAlphaModel = `{"id":"model-alpha","object":"model","owned_by":"catalog-owner","created":123,"extra":{"context_window":424242}}`

func newPinnedModelsKeyWhitelistFixture(t *testing.T) (*OpenAIGatewayHandler, *service.Group, *codexModelsPinnedHTTPUpstream) {
	t.Helper()
	upstream := &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{
		1: `{"data":[{"id":"scheduler-only"}]}`,
		2: `{"data":[` + pinnedKeyWhitelistAlphaModel + `,{"id":"model-beta"},{"id":"extra-exact"},{"id":"group-hidden"},{"id":"key-hidden"}]}`,
	}}
	codex := newPinnedCodexTestHandler([]service.Account{
		newPinnedCodexAccount(1, service.StatusActive, true, false),
		newPinnedCodexAccount(2, service.StatusActive, true, false),
	}, upstream, 3)
	t.Cleanup(codex.gatewayService.StopOpenAICodexTicketHarvester)
	group := &service.Group{
		ID: 96, Platform: service.PlatformOpenAI,
		ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"model-*", "extra-exact", "key-hidden"}},
		CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{
			Enabled: true, AccountIDs: []int64{2}, FallbackToScheduler: true,
		},
	}
	return codex, group, upstream
}

func requestPinnedModelsWithKeyForTest(t *testing.T, codex *OpenAIGatewayHandler, key *service.APIKey, modelID, etag string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	path := "/v1/models"
	if modelID != "" {
		path += "/" + modelID
		c.Params = gin.Params{{Key: "model", Value: modelID}}
	}
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	c.Request.Header.Set("If-None-Match", etag)
	c.Set(string(middleware2.ContextKeyAPIKey), key)
	h := &GatewayHandler{openAIGatewayService: codex.gatewayService, maxAccountSwitches: 3}
	h.Models(c)
	return rec
}

func TestOrdinaryPinnedModelsAppliesKeyWhitelistToListsAndRetrieval(t *testing.T) {
	codex, group, upstream := newPinnedModelsKeyWhitelistFixture(t)
	for _, tc := range []struct {
		name          string
		allowed       []string
		modelID       string
		wantStatus    int
		wantIDs       []string
		checkMetadata bool
	}{
		{
			name:    "wildcard_and_exact_models_intersect_group_allowlist",
			allowed: []string{"model-*", "extra-exact", "group-hidden"}, wantStatus: http.StatusOK,
			wantIDs: []string{"model-alpha", "model-beta", "extra-exact"}, checkMetadata: true,
		},
		{
			name: "exact_model_only", allowed: []string{"model-beta"}, wantStatus: http.StatusOK,
			wantIDs: []string{"model-beta"},
		},
		{
			name: "empty_result_remains_array_without_fallback", allowed: []string{"scheduler-only"},
			wantStatus: http.StatusOK, wantIDs: []string{},
		},
		{
			name: "retrieve_allowed_model_preserves_metadata", allowed: []string{"model-*"},
			modelID: "model-alpha", wantStatus: http.StatusOK, checkMetadata: true,
		},
		{
			name: "retrieve_model_blocked_by_key", allowed: []string{"model-alpha"},
			modelID: "model-beta", wantStatus: http.StatusNotFound,
		},
		{
			name: "retrieve_model_blocked_by_group", allowed: []string{"group-hidden"},
			modelID: "group-hidden", wantStatus: http.StatusNotFound,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := &service.APIKey{ID: 10, GroupID: &group.ID, Group: group, AllowedModels: tc.allowed}
			got := requestPinnedModelsWithKeyForTest(t, codex, key, tc.modelID, "")
			require.Equal(t, tc.wantStatus, got.Code, got.Body.String())
			if tc.wantStatus == http.StatusNotFound {
				require.Equal(t, "model_not_found", gjson.Get(got.Body.String(), "error.code").String())
				return
			}
			if tc.modelID == "" {
				require.Equal(t, tc.wantIDs, ordinaryPinnedModelIDs(t, got))
				if tc.checkMetadata {
					var catalog struct {
						Data []json.RawMessage `json:"data"`
					}
					require.NoError(t, json.Unmarshal(got.Body.Bytes(), &catalog))
					require.JSONEq(t, pinnedKeyWhitelistAlphaModel, string(catalog.Data[0]))
				}
			} else {
				require.JSONEq(t, pinnedKeyWhitelistAlphaModel, got.Body.String())
				require.Empty(t, got.Header().Get("ETag"), "single models must not reuse collection ETags")
			}
		})
	}
	require.Equal(t, []int64{2}, upstream.accountIDs(), "key filtering must use the cached pinned catalog without scheduler fallback")
}

func TestOrdinaryPinnedModelsETagsFollowKeyFilteredRepresentation(t *testing.T) {
	codex, group, upstream := newPinnedModelsKeyWhitelistFixture(t)
	unrestricted := &service.APIKey{ID: 1, GroupID: &group.ID, Group: group}
	alphaKey := &service.APIKey{ID: 2, GroupID: &group.ID, Group: group, AllowedModels: []string{"model-alpha"}}
	betaKey := &service.APIKey{ID: 3, GroupID: &group.ID, Group: group, AllowedModels: []string{"model-beta"}}

	groupList := requestPinnedModelsWithKeyForTest(t, codex, unrestricted, "", "")
	require.Equal(t, http.StatusOK, groupList.Code, groupList.Body.String())
	require.Equal(t, []string{"model-alpha", "model-beta", "extra-exact", "key-hidden"}, ordinaryPinnedModelIDs(t, groupList))
	groupETag := groupList.Header().Get("ETag")
	require.NotEmpty(t, groupETag)

	alphaList := requestPinnedModelsWithKeyForTest(t, codex, alphaKey, "", groupETag)
	require.Equal(t, http.StatusOK, alphaList.Code, "an unrestricted ETag must not suppress a key-filtered response")
	require.Equal(t, []string{"model-alpha"}, ordinaryPinnedModelIDs(t, alphaList))
	alphaETag := alphaList.Header().Get("ETag")
	require.NotEmpty(t, alphaETag)
	require.NotEqual(t, groupETag, alphaETag)

	unchanged := requestPinnedModelsWithKeyForTest(t, codex, alphaKey, "", alphaETag)
	require.Equal(t, http.StatusNotModified, unchanged.Code)
	require.Equal(t, alphaETag, unchanged.Header().Get("ETag"))
	require.Empty(t, unchanged.Body.String())

	allowedModel := requestPinnedModelsWithKeyForTest(t, codex, alphaKey, "model-alpha", alphaETag)
	require.Equal(t, http.StatusOK, allowedModel.Code)
	require.JSONEq(t, pinnedKeyWhitelistAlphaModel, allowedModel.Body.String())
	require.Empty(t, allowedModel.Header().Get("ETag"))
	blockedModel := requestPinnedModelsWithKeyForTest(t, codex, alphaKey, "model-beta", alphaETag)
	require.Equal(t, http.StatusNotFound, blockedModel.Code)

	betaList := requestPinnedModelsWithKeyForTest(t, codex, betaKey, "", alphaETag)
	require.Equal(t, http.StatusOK, betaList.Code)
	require.Equal(t, []string{"model-beta"}, ordinaryPinnedModelIDs(t, betaList))
	require.NotEqual(t, alphaETag, betaList.Header().Get("ETag"))

	again := requestPinnedModelsWithKeyForTest(t, codex, unrestricted, "", alphaETag)
	require.Equal(t, http.StatusOK, again.Code)
	require.JSONEq(t, groupList.Body.String(), again.Body.String(), "key filtering must not mutate the cached group catalog")
	require.Equal(t, groupETag, again.Header().Get("ETag"))
	require.Equal(t, http.StatusNotModified, requestPinnedModelsWithKeyForTest(t, codex, unrestricted, "", groupETag).Code)
	require.Equal(t, []int64{2}, upstream.accountIDs())
}
