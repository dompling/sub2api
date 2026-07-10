# Task Plan: Preserve Fork Changes Across Kiro Merge

## Goal
Restore all fork i18n content lost during the upstream merge and verify every manual merge resolution preserves both fork and upstream behavior.

## Current Phase
Complete

## Phases

### Phase 1: Baseline and Extraction
- [x] Record the confirmed merge losses and conflict paths
- [x] Extract the 124 exact en/zh key-value pairs from `ec7b8443`
- **Status:** complete

### Phase 2: i18n Restoration
- [x] Migrate fork values into the current locale modules
- [x] Add exact-value preservation tests
- **Status:** complete

### Phase 3: Merge Resolution Audit
- [x] Audit all paths emitted by remerge-diff for `c8895784` and `16a57987`
- [x] Repair any additional confirmed fork losses and add regression coverage
- **Status:** complete

### Phase 4: Verification
- [x] Run code generation consistency checks
- [x] Run targeted and full backend/frontend checks
- [x] Confirm a clean, intentional diff
- **Status:** complete

### Phase 5: Delivery
- [x] Summarize restored content, audit results, and residual risk
- **Status:** complete

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Use `ec7b8443` as the fork content authority | User requested exact preservation of jellynian/gogoing1024 work |
| Keep current split locale architecture | Avoid reverting upstream refactors |
| Preserve exact fork translations | User selected exact fork wording |
| Do not rewrite existing merge history | Current commits remain reachable; additive repair is safer |

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| Combined en/zh accounts patch used a zh anchor that did not exist | 1 | Split locale patches and use language-specific anchors; no business changes were applied |
| Merge audit assertion treated separate Wire symbols as one line | 1 | Replace with independent symbol assertions |
| Audit expected old normalizer name directly in `admin_group.go` | 1 | Traced current handler/service/repository flow and found actual missing assignments |
| Focused unit suite initially failed on additional losses and stale assertions | 1 | Restored missing data flow and updated assertions only where later intentional behavior superseded them |
| Frontend lint found consecutive Kiro/Grok returns | 1 | Combined both merge sides into one reachable manual-input condition |
| `make generate` was invoked from the repository root | 1 | Re-ran from `backend/`, where the target is defined; generation produced no diff |
