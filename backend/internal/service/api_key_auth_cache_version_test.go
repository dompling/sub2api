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

// Both forks used v26 for different additions; neither old schema is complete.
func TestAPIKeyService_RejectsV26AuthSnapshotFromEitherFork(t *testing.T) {
	svc := &APIKeyService{}
	for name, snapshot := range map[string]*APIKeyAuthSnapshot{
		"local_without_image_billing":    {Version: 26, AllowedModels: []string{"gpt-5"}},
		"upstream_without_key_allowlist": {Version: 26, Group: &APIKeyAuthGroupSnapshot{ImageCountBilling: true}},
	} {
		t.Run(name, func(t *testing.T) {
			apiKey, used, err := svc.applyAuthCacheEntry("sk-v26", &APIKeyAuthCacheEntry{Snapshot: snapshot})
			require.NoError(t, err)
			require.False(t, used)
			require.Nil(t, apiKey)
		})
	}
}

func TestAPIKeyAuthSnapshotPreservesKeyAllowlistAndImageBilling(t *testing.T) {
	svc := &APIKeyService{}
	apiKey := profitAuthTestAPIKey()
	apiKey.AllowedModels = []string{"gpt-5"}
	apiKey.Group.ImageCountBilling = true

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
}
