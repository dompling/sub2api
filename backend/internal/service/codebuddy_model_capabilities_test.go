package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codebuddy"
	"github.com/stretchr/testify/require"
)

func TestCodeBuddyModelCapabilitiesPreservesPresence(t *testing.T) {
	account := &Account{ID: 881, Credentials: map[string]any{
		"codebuddy_models_meta": `[
			{"id":"missing","reasoning":{"default_effort":"high"}},
			{"id":"false","supports_reasoning":false,"supports_images":false},
			{"id":"true","supports_reasoning":true,"max_input_tokens":128000,"max_output_tokens":8192},
			{"id":"null","supports_reasoning":null,"max_input_tokens":-1}
		]`,
	}}
	got := codeBuddyModelCapabilities(account)
	require.Nil(t, got["missing"].SupportsReasoning)
	require.Nil(t, got["null"].SupportsReasoning)
	require.Zero(t, got["null"].MaxInputTokens)
	require.NotNil(t, got["false"].SupportsReasoning)
	require.False(t, *got["false"].SupportsReasoning)
	require.NotNil(t, got["false"].SupportsImages)
	require.False(t, *got["false"].SupportsImages)
	require.True(t, *got["true"].SupportsReasoning)
	require.Equal(t, 8192, got["true"].MaxOutputTokens)
}

func TestCodeBuddyModelCapabilitiesInvalidPersistedMetadataIsUnknown(t *testing.T) {
	for _, raw := range []string{
		`not-json`,
		`[{"id":"native","supports_reasoning":"true"}]`,
		`[{"id":"native","supports_reasoning":true},{"id":"native","supports_reasoning":false}]`,
		`[{"supports_reasoning":true}]`,
	} {
		account := &Account{ID: 882, Credentials: map[string]any{"codebuddy_models_meta": raw}}
		require.Empty(t, codeBuddyModelCapabilities(account))
	}
}

func TestCodeBuddyModelCapabilitiesUsesOnlyExistingUnexpiredCache(t *testing.T) {
	account := &Account{ID: 883}
	t.Cleanup(func() { codeBuddyMetaCache.Delete(account.ID) })
	entry := codeBuddyMetaCacheEntry{
		exp: time.Now().Add(time.Minute),
		metas: []codebuddy.ModelInfo{
			{ID: "true", SupportsReasoning: true, SupportsImages: true, MaxOutputTokens: 8192},
			{ID: "unknown", SupportsReasoning: false},
		},
	}
	codeBuddyMetaCache.Store(account.ID, entry)
	got := codeBuddyModelCapabilities(account)
	require.True(t, *got["true"].SupportsReasoning)
	require.True(t, *got["true"].SupportsImages)
	require.Equal(t, 8192, got["true"].MaxOutputTokens)
	require.Nil(t, got["unknown"].SupportsReasoning)
	account.Credentials = map[string]any{"codebuddy_models_meta": `[{"id":"true","supports_reasoning":false}]`}
	require.False(t, *codeBuddyModelCapabilities(account)["true"].SupportsReasoning)
	account.Credentials = nil
	entry.exp = time.Now().Add(-time.Minute)
	codeBuddyMetaCache.Store(account.ID, entry)
	require.Empty(t, codeBuddyModelCapabilities(account))
}

func TestCodeBuddyModelCapabilitiesRequiresGroupScope(t *testing.T) {
	service := &GatewayService{}
	require.Nil(t, service.GetCodeBuddyModelCapabilities(context.Background(), nil, []string{"native"}))
	var missing *GatewayService
	require.Nil(t, missing.GetCodeBuddyModelCapabilities(context.Background(), nil, []string{"native"}))
}
