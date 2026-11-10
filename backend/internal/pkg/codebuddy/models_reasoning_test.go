package codebuddy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseModelsPreservesReasoningConfiguration(t *testing.T) {
	models, err := ParseModels([]byte(`{"data":{"models":[{"id":"glm-5.2","supportsReasoning":true,"onlyReasoning":false,"reasoning":{"defaultEffort":"high","effort":"medium","supportedEfforts":["high","xhigh"],"canDisableThinking":true,"summary":"auto"}}]}}`))
	require.NoError(t, err)
	require.Len(t, models, 1)
	encoded, err := json.Marshal(models[0])
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal(encoded, &stored))
	require.Equal(t, map[string]any{"defaultEffort": "high", "effort": "medium", "supportedEfforts": []any{"high", "xhigh"}, "canDisableThinking": true, "summary": "auto"}, stored["reasoning"])
}
