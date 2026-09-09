-- user_platform_quotas.platform CHECK 加入 codebuddy（CodeBuddy/WorkBuddy 上游）。
-- 保留此前已允许的全部平台（anthropic/openai/gemini/antigravity/kiro/grok/kimi/zhipu/deepseek）。
-- 重建约束时漏掉任一平台都会让对应平台的配额行插入失败。
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro', 'grok',
                        'kimi', 'zhipu', 'deepseek', 'codebuddy'));
