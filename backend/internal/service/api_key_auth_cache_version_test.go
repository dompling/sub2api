package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyService_RejectsV10AuthSnapshotWithoutModelAllowlist(t *testing.T) {
	groupID := int64(9)
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-models-list", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{
			Version:  10,
			APIKeyID: 1,
			UserID:   2,
			GroupID:  &groupID,
			Status:   StatusActive,
			User: APIKeyAuthUserSnapshot{
				ID:          2,
				Status:      StatusActive,
				Role:        RoleUser,
				Balance:     10,
				Concurrency: 3,
			},
			Group: &APIKeyAuthGroupSnapshot{
				ID:               groupID,
				Name:             "openai",
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   1,
			},
		},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatalf("expected v10 auth snapshot to be rejected after model_allowlist was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

func TestAPIKeyService_RejectsV15AuthSnapshotWithoutReasoningEffortPolicy(t *testing.T) {
	svc := &APIKeyService{}

	apiKey, ok, err := svc.applyAuthCacheEntry("k-legacy-reasoning-mappings", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 15},
	})

	if err != nil {
		t.Fatalf("expected stale snapshot to be ignored without error, got %v", err)
	}
	if ok {
		t.Fatal("expected v15 auth snapshot to be rejected after reasoning effort policy was added")
	}
	if apiKey != nil {
		t.Fatalf("expected no API key from stale snapshot, got %#v", apiKey)
	}
}

// Earlier snapshots from both forks lack fields required by the merged schema.
func TestAPIKeyService_RejectsLegacyAuthSnapshotsFromEitherFork(t *testing.T) {
	svc := &APIKeyService{}
	for name, snapshot := range map[string]*APIKeyAuthSnapshot{
		"local_without_image_billing":    {Version: 26, AllowedModels: []string{"gpt-5"}},
		"upstream_without_key_allowlist": {Version: 26, Group: &APIKeyAuthGroupSnapshot{ImageCountBilling: true}},
		"local_v27_without_privacy":      {Version: 27, AllowedModels: []string{"gpt-5"}, Group: &APIKeyAuthGroupSnapshot{ImageCountBilling: true}},
		"upstream_v28_without_key_allowlist": {Version: 28, Group: &APIKeyAuthGroupSnapshot{
			ImageCountBilling: true, RequirePrivacySet: true,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			apiKey, used, err := svc.applyAuthCacheEntry("sk-legacy", &APIKeyAuthCacheEntry{Snapshot: snapshot})
			require.NoError(t, err)
			require.False(t, used)
			require.Nil(t, apiKey)
		})
	}
}

func TestAPIKeyAuthSnapshotPreservesKeyAllowlistImageBillingAndPrivacy(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()
	apiKey.AllowedModels = []string{"gpt-5"}
	apiKey.Group.ImageCountBilling = true
	apiKey.Group.RequirePrivacySet = true

	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), apiKey)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))

	restored, used, err := svc.applyAuthCacheEntry("sk-merged", &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, restored)
	require.Equal(t, []string{"gpt-5"}, restored.AllowedModels)
	require.NotNil(t, restored.Group)
	require.True(t, restored.Group.ImageCountBilling)
	require.True(t, restored.Group.RequirePrivacySet)
}
