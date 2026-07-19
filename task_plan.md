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

---

# Task Plan: Merge official/main into main (2026-07-19)

## Goal
Resolve all current merge conflicts while preserving both fork and upstream behavior, verify the merged repository, and create the pending merge commit without pushing it.

## Current Phase
Complete

## Phases

### Phase 1: Backend Semantic Resolution
- [x] Merge gateway prompt rules with security audit and image-intent fixes
- [x] Merge group persistence normalization with duplicate/outbox behavior
- [x] Merge Ops sanitization fields and repair provider wiring
- **Status:** complete

### Phase 2: Generated Code
- [x] Regenerate Ent and Wire from authoritative sources
- [x] Confirm generated files contain both feature sets
- **Status:** complete

### Phase 3: Frontend Semantic Resolution
- [x] Merge step-up flows with existing app controls
- [x] Merge locale exports and navigation labels
- [x] Preserve both sides of conflicted frontend tests
- **Status:** complete

### Phase 4: Verification
- [x] Run conflict-marker, formatting, targeted test, full test, and build checks
- [x] Resolve any unmarked semantic conflicts exposed by verification
- **Status:** complete

### Phase 5: Merge Commit
- [x] Stage resolved/generated files and confirm no unmerged entries
- [x] Create the pending merge commit using the existing merge message
- **Status:** complete

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Preserve both fork and upstream features | User selected the feature-union strategy |
| Audit original input before prompt-rule injection | Prevent injected system rules from being treated as user input while retaining both controls |
| Regenerate Ent and Wire | Schema/provider sources are authoritative and both generated sides are incomplete |
| Keep application dialogs and add step-up around sensitive calls | Preserves established UX while adding upstream security gates |

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| Initial planning-file append used a stale progress anchor | 1 | No changes applied; retried with exact file-tail anchors |
| Ent generation could not load conflicted generated Go files | 1 | Apply minimal compile-only unions, then rerun the authoritative generator |
| Full frontend tests found native-control/dialog policy violations, a missing dependency install, and an Ops cleanup interaction failure | 1 | Inspect each failure and repair unmarked merge regressions before rerunning |
| Backend tests passed but golangci-lint reported 19 issues | 1 | Classify auto-merge regressions versus mechanical/static-analysis findings and fix in scope |
| Staticcheck identified an unreachable Grok quota-reset success branch | 1 | Preserve the unsupported endpoint contract and directly forward its required error from the handler |
| Cached whitespace check reported errors inside an upstream source-freeze patch | 1 | Preserve the archival patch byte-for-byte and verify all other staged paths with that evidence file excluded |
