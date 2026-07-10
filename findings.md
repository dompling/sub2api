# Findings and Decisions

## Requirements
- Restore fork content lost during the Kiro/upstream merge.
- Preserve work from `jellynian@qq.com` and `gogoing1024` exactly.
- Audit all manual conflict resolutions, not only the confirmed i18n loss.
- Add regression protection and verify the full repository.

## Research Findings
- `origin/main` (`ec7b8443`) is an ancestor of current `HEAD` (`16a57987`), so commits remain reachable.
- The `c8895784` merge had 13 explicit remerge conflicts despite its commit message listing none.
- The `16a57987` merge had one conflict in `UsageFilters.vue`.
- The locale split dropped 124 keys in each language; all 124 were added after merge base `9d5f1b73`.
- 88 of those keys are still directly referenced by production UI code.
- Error-log cleanup code and endpoint remain present, but its three locale messages are missing.
- Existing Kiro and cleanup tests pass because they mock translations or do not assert these keys.
- The 124 keys fit into 14 current parent objects; no locale namespace redesign is needed.
- Exact fork source-text hashes are `96b142673b988b511c134d23473016c13e24393bd2afb3fb736c3b0bfbc5a4e5` (en) and `1b96e121e54c9dcfd4603678d6212214e9c2ddc89e66dd58382daac035e4db03` (zh).
- After migration, AST comparison reports 124/124 keys present in both locales and zero initializer differences from `ec7b8443`.
- Runtime semantic hashes used by the regression test are `2a496c2c86e420c035d4b808ff1fbd008ed89508aad6b613a4fbeee7ed88a8a1` (en) and `eca157af58e66d9cd040405dd30bb1702d1b059052e488a4db1e69659706b492` (zh).
- Merge audit found a second real loss: `CreateGroup` and `UpdateGroup` accept Kiro runtime fields but do not copy them into the persisted `Group` after the admin service split.
- The same split replaced `isOAuthOnlyRestrictedPlatform` with inline checks that omit Kiro in both create and update copy-account flows.
- The gateway split also dropped Kiro sticky hashing, runtime cooldown scheduling, SSE credit extraction/stripping, and gateway usage-log credit persistence.
- The admin account split dropped Kiro credit-unit-price validation from create and update flows.
- Two pre-existing unit assertions were stale rather than merge losses: HTTP header map casing for `TokenType`, and the External IdP redirect port after the explicit `3128` fix.
- Full unit verification found two more stale fixtures: the platform-quota success request omitted the now-allowed Kiro platform, and the public group API contract omitted `kiro_endpoint_mode`.
- Frontend lint exposed another real merge loss in `ReAuthAccountModal.vue`: the Kiro and Grok manual-input branches survived as consecutive returns, making the Grok union unreachable.
- Declaration-level comparison found zero missing non-test Go functions added on the fork side between `9d5f1b73` and `c8895784^1` after repairs.
- Final verification passed for backend normal/unit suites and all 953 frontend tests; Ent/Wire generation produced no drift.

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
| Extract locale data through the TypeScript AST | Preserves nested paths and exact source values without regex parsing |
| Place keys by current domain module | Matches the upstream locale architecture |
| Guard all 124 keys and their exact values | Prevents both deletion and silent wording changes |
| Regenerate Wire/Ent rather than hand-edit generated code | Keeps generator sources authoritative |

## Resources
- Fork locale source: `ec7b8443:frontend/src/i18n/locales/{en,zh}.ts`
- Merge base: `9d5f1b73`
- Merge commits: `c8895784`, `16a57987`
