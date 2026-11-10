package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// CodeBuddyModelCapabilities contains only public capability evidence. Pointers
// preserve explicit false without treating older, absent metadata as false.
type CodeBuddyModelCapabilities struct {
	SupportsReasoning *bool `json:"supports_reasoning,omitempty"`
	SupportsImages    *bool `json:"supports_images,omitempty"`
	MaxInputTokens    int   `json:"max_input_tokens,omitempty"`
	MaxOutputTokens   int   `json:"max_output_tokens,omitempty"`
}

// GetCodeBuddyModelCapabilities enriches an already-filtered ordinary CodeBuddy
// catalog. It does not add models, fetch upstream config, or inspect other
// groups. Composite routing is deliberately left to its own model resolver.
func (s *GatewayService) GetCodeBuddyModelCapabilities(ctx context.Context, groupID *int64, modelIDs []string) map[string]CodeBuddyModelCapabilities {
	if s == nil || s.accountRepo == nil || groupID == nil || len(modelIDs) == 0 {
		return nil
	}
	accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	if err != nil {
		return nil
	}
	metas := make(map[int]map[string]CodeBuddyModelCapabilities)
	for i := range accounts {
		if accounts[i].IsCodeBuddy() && accounts[i].IsSchedulable() {
			metas[i] = codeBuddyModelCapabilities(&accounts[i])
		}
	}
	result := make(map[string]CodeBuddyModelCapabilities)
	for _, modelID := range modelIDs {
		mapping, _ := s.ResolveChannelMappingAndRestrict(ctx, groupID, modelID)
		requested := mapping.MappedModel
		var merged CodeBuddyModelCapabilities
		found := false
		for i, metadata := range metas {
			account := &accounts[i]
			if !account.IsModelSupported(requested) || !account.IsSchedulableForModelWithContext(ctx, requested) {
				continue
			}
			upstreamModel := resolveOpenAIAccountUpstreamModelForRequest(account, requested, false)
			capabilities := metadata[upstreamModel]
			if !found {
				merged, found = capabilities, true
				continue
			}
			// A claim must hold for every eligible route. Missing or conflicting
			// booleans stay unknown; limits use the smallest fully known budget.
			merged.SupportsReasoning = agreedCapability(merged.SupportsReasoning, capabilities.SupportsReasoning)
			merged.SupportsImages = agreedCapability(merged.SupportsImages, capabilities.SupportsImages)
			merged.MaxInputTokens = min(merged.MaxInputTokens, capabilities.MaxInputTokens)
			merged.MaxOutputTokens = min(merged.MaxOutputTokens, capabilities.MaxOutputTokens)
		}
		if found {
			result[modelID] = merged
		}
	}
	return result
}

func agreedCapability(a, b *bool) *bool {
	if a == nil || b == nil || *a != *b {
		return nil
	}
	return a
}

func codeBuddyModelCapabilities(account *Account) map[string]CodeBuddyModelCapabilities {
	var entries []struct {
		ID string `json:"id"`
		CodeBuddyModelCapabilities
	}
	raw := account.GetCredential("codebuddy_models_meta")
	result := make(map[string]CodeBuddyModelCapabilities)
	if raw != "" && json.Unmarshal([]byte(raw), &entries) == nil && len(entries) > 0 {
		for _, entry := range entries {
			if strings.TrimSpace(entry.ID) == "" {
				return nil
			}
			if _, exists := result[entry.ID]; exists {
				return nil
			}
			entry.MaxInputTokens = max(0, entry.MaxInputTokens)
			entry.MaxOutputTokens = max(0, entry.MaxOutputTokens)
			result[entry.ID] = entry.CodeBuddyModelCapabilities
		}
		return result
	}
	// The existing live-config cache stores typed booleans without presence
	// information. True remains evidence; false cannot distinguish old missing
	// fields and is therefore omitted. Reading this cache never triggers a fetch.
	if cached, ok := codeBuddyMetaCache.Load(account.ID); ok {
		if entry, ok := cached.(codeBuddyMetaCacheEntry); ok && time.Now().Before(entry.exp) {
			for _, model := range entry.metas {
				capabilities := CodeBuddyModelCapabilities{
					MaxInputTokens: max(0, model.MaxInputTokens), MaxOutputTokens: max(0, model.MaxOutputTokens),
				}
				if model.SupportsReasoning {
					value := true
					capabilities.SupportsReasoning = &value
				}
				if model.SupportsImages {
					value := true
					capabilities.SupportsImages = &value
				}
				result[model.ID] = capabilities
			}
		}
	}
	return result
}
