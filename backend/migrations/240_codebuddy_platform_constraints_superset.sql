-- Preserve CodeBuddy alongside all upstream quota and composite-route platforms.
-- 237/238/239 also retain CodeBuddy so their ADD CONSTRAINT statements can run
-- against existing CodeBuddy rows. Their exact historical checksums remain
-- accepted by the migration runner; databases that already applied those files
-- need this append-only migration to converge to the complete platform set.
-- Private migrations 232/233/234 remain unchanged for deployed CodeBuddy forks.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro', 'grok',
                        'adobe', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'codebuddy'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro', 'grok',
                               'adobe', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'codebuddy'));
