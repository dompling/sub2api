//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestCodeBuddyPlatformMigrationsPreserveExistingRows(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()

	// Shadow the real tables on this transaction's connection so replaying old
	// constraints cannot narrow the shared integration database's platform set.
	_, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE user_platform_quotas (LIKE public.user_platform_quotas INCLUDING ALL) ON COMMIT DROP;
CREATE TEMP TABLE composite_model_routes (LIKE public.composite_model_routes INCLUDING ALL) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitors (provider TEXT) ON COMMIT DROP;
CREATE TEMP TABLE channel_monitor_request_templates (provider TEXT) ON COMMIT DROP;
`)
	require.NoError(t, err)

	for _, name := range []string{
		"233_user_platform_quotas_add_codebuddy.sql",
		"234_composite_routes_add_codebuddy.sql",
	} {
		body, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, name)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO user_platform_quotas (id, user_id, platform, daily_limit_usd, daily_usage_usd)
VALUES (1, 1, 'codebuddy', 10, 2.5), (2, 2, 'codebuddy', 0, 0);
INSERT INTO composite_model_routes (id, group_id, public_model, target_platform, upstream_model)
VALUES (1, 1, 'my-codebuddy-model', 'codebuddy', 'claude-sonnet-4-6');
`)
	require.NoError(t, err)

	for _, name := range []string{
		"237_add_minimax_platform.sql",
		"238_add_adobe_platform.sql",
		"238_opencode_go_platform.sql",
		"238_purge_unlimited_user_platform_quotas.sql",
		"239_fork_platform_constraints_superset.sql",
		"240_codebuddy_platform_constraints_superset.sql",
	} {
		body, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err, "%s must accept already stored CodeBuddy rows", name)

		var limit, used float64
		require.NoError(t, tx.QueryRowContext(ctx,
			"SELECT daily_limit_usd, daily_usage_usd FROM user_platform_quotas WHERE id = 1 AND platform = 'codebuddy'",
		).Scan(&limit, &used))
		require.Equal(t, 10.0, limit, name)
		require.Equal(t, 2.5, used, name)
		var disabled int
		require.NoError(t, tx.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM user_platform_quotas WHERE id = 2 AND daily_limit_usd = 0",
		).Scan(&disabled))
		require.Equal(t, 1, disabled, name)
		var target, model string
		require.NoError(t, tx.QueryRowContext(ctx,
			"SELECT target_platform, upstream_model FROM composite_model_routes WHERE id = 1",
		).Scan(&target, &model))
		require.Equal(t, "codebuddy", target, name)
		require.Equal(t, "claude-sonnet-4-6", model, name)
	}

	// The final repair is replayable and retains every upstream concrete platform.
	body, err := migrations.FS.ReadFile("240_codebuddy_platform_constraints_superset.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(body))
	require.NoError(t, err)
	for i, platform := range []string{
		"anthropic", "openai", "gemini", "antigravity", "kiro", "grok", "adobe",
		"kimi", "zhipu", "deepseek", "minimax", "opencode_go", "codebuddy",
	} {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO user_platform_quotas (id, user_id, platform, daily_limit_usd) VALUES ($1, $1, $2, 1)", i+10, platform)
		require.NoError(t, err, platform)
		_, err = tx.ExecContext(ctx,
			"INSERT INTO composite_model_routes (id, group_id, public_model, target_platform) VALUES ($1, 1, $2, $2)", i+10, platform)
		require.NoError(t, err, platform)
	}
}
