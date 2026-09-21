package migrations

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForkPlatformConstraintsSupersetMigration(t *testing.T) {
	const name = "239_fork_platform_constraints_superset.sql"
	content, err := FS.ReadFile(name)
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'kiro', 'minimax', 'adobe', 'opencode_go', 'codebuddy'))")
	require.Contains(t, sql,
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'kiro', 'minimax', 'adobe', 'opencode_go', 'codebuddy'))")
	require.Contains(t, sql,
		"ADD CONSTRAINT channel_monitors_provider_check CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kiro', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go'))")
	require.Contains(t, sql,
		"ADD CONSTRAINT channel_monitor_request_templates_provider_check CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kiro', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go'))")

	// CodeBuddy 修复必须排在 239 之后，兼容已应用上游 239 的数据库。
	entries, err := FS.ReadDir(".")
	require.NoError(t, err)
	var touching []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		body, err := FS.ReadFile(e.Name())
		require.NoError(t, err)
		if strings.Contains(string(body), "ADD CONSTRAINT user_platform_quotas_platform_check") ||
			strings.Contains(string(body), "ADD CONSTRAINT composite_model_routes_target_platform_check") ||
			strings.Contains(string(body), "ADD CONSTRAINT channel_monitors_provider_check") {
			touching = append(touching, e.Name())
		}
	}
	sort.Strings(touching)
	require.Equal(t, "240_codebuddy_platform_constraints_superset.sql", touching[len(touching)-1])
}
