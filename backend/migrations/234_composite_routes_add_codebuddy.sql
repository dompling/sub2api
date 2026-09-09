-- composite_model_routes.target_platform CHECK 加入 codebuddy。
-- 保留此前已允许的全部平台（anthropic/openai/gemini/antigravity/kiro/grok/kimi/zhipu/deepseek）。
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro', 'grok',
                               'kimi', 'zhipu', 'deepseek', 'codebuddy'));
