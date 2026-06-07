# Fork Customizations Inventory

This file tracks the intentional differences in this private fork so future
`upstream/main` syncs can preserve owner-specific behavior without rediscovering
the same decisions.

Last audited: 2026-06-01

## Baseline

| Ref | Value |
| --- | --- |
| Local branch | `main` |
| Local HEAD | `bad3442d0b74816237aa33f310c3b8cf1f69480d` |
| `origin/main` | `bad3442d0b74816237aa33f310c3b8cf1f69480d` |
| `upstream/main` | `f18451e56f15b31ef602ab238037b56c3522b19f` |
| Merge base | `f18451e56f15b31ef602ab238037b56c3522b19f` |

Audit commands used:

```bash
GIT_MASTER=1 git status --short --branch
GIT_MASTER=1 git log --oneline --decorate --reverse upstream/main..HEAD
GIT_MASTER=1 git cherry -v upstream/main HEAD
GIT_MASTER=1 git diff --stat upstream/main...HEAD
GIT_MASTER=1 git diff --name-status upstream/main...HEAD
GIT_MASTER=1 git diff --numstat upstream/main...HEAD
GIT_MASTER=1 git ls-files --others --exclude-standard
```

Current worktree note: tracked files were clean during the audit. Untracked
local tool artifacts were present under `.codegraph/`, `.omo/`, and
`.playwright-mcp/`; do not include those in upstream sync commits.

## Current Effective Diff Against Upstream

`GIT_MASTER=1 git diff --stat upstream/main...HEAD` reports 11 changed files,
677 insertions, and 41 deletions. The current effective fork delta is concentrated
in backend gateway behavior, not frontend, migrations, Ent, Wire, or deploy files.

| File | Delta | Category | Keep During Sync |
| --- | ---: | --- | --- |
| `backend/internal/pkg/claude/constants.go` | `+2/-2` | Claude Code fingerprint, beta list, model list | Yes |
| `backend/internal/service/gateway_service.go` | `+43/-23` | Claude OAuth mimicry and beta/header policy | Yes |
| `backend/internal/service/gateway_context_management_test.go` | `+11/-10` | Regression tests for context-management/haiku behavior | Yes |
| `backend/internal/service/gateway_request.go` | `+100/-1` | Context-management sanitize and thinking retry cleanup | Yes |
| `backend/internal/service/gateway_request_test.go` | `+55/-0` | Tests for request cleanup | Yes |
| `backend/internal/service/gateway_service_signature_test.go` | `+15/-0` | Signature-sensitive retry coverage | Yes |
| `backend/internal/service/gateway_anthropic_apikey_passthrough_test.go` | `+152/-4` | Anthropic passthrough retry regression coverage | Yes |
| `backend/internal/service/openai_codex_transform.go` | `+50/-0` | OpenAI/Codex tool placeholder cleanup | Yes |
| `backend/internal/service/openai_codex_transform_test.go` | `+106/-0` | Tests for placeholder cleanup | Yes |
| `backend/internal/service/openai_gateway_service.go` | `+83/-1` | OpenAI gateway placeholder cleanup and SSE handling | Yes |
| `backend/internal/service/openai_gateway_service_test.go` | `+60/-0` | OpenAI gateway regression tests | Yes |

## Local-Only Functional Commits

`GIT_MASTER=1 git cherry -v upstream/main HEAD` reports 11 local functional
commits. `git log upstream/main..HEAD` also contains 5 upstream merge commits;
those are sync history, not fork behavior.

| Commit | Date | Topic | Current Status |
| --- | --- | --- | --- |
| `df662694` | 2026-04-28 | Remove haiku exceptions from system prompt rewrite | Preserve as fork contract |
| `ce600fae` | 2026-04-24 | Port Parrot tool-name obfuscation and message cache breakpoints | Preserve behavior; some files may now overlap upstream |
| `854b2f5f` | 2026-05-09 | Fix brace nesting in Parrot `tool_choice` block | Historical fixup for Parrot port |
| `260655bd` | 2026-04-24 | Add `cache_control.ttl`, defaulting generated cache blocks to `5m` | Preserve as fork contract |
| `b95e4fd4` | 2026-04-24 | Bump mimicked Claude CLI to `2.1.92` and extend beta list | Preserve as fork contract |
| `fb2de079` | 2026-05-10 | Remove haiku bypass from CC system rewrite and OAuth mimicry | Preserve as fork contract |
| `fc29ae92` | 2026-05-31 | Normalize trailing thinking blocks | Preserve until upstream has equivalent behavior |
| `65b45ee3` | 2026-05-31 | Suppress incomplete OpenAI Responses SSE data | Preserve until upstream has equivalent behavior |
| `8ba648e0` | 2026-05-31 | Retry trailing thinking cleanup reactively | Preserve until upstream has equivalent behavior |
| `4255831c` | 2026-05-31 | Drop invalid placeholder tools in `/v1/responses` non-passthrough path | Candidate upstreamable fix |
| `bad3442d` | 2026-05-31 | Drop invalid placeholder tools on passthrough path | Candidate upstreamable fix |

Merge commits in local history:

```text
32945caf Merge remote-tracking branch 'upstream/main'
0818b18e Merge remote-tracking branch 'upstream/main'
a08314bf Merge remote-tracking branch 'upstream/main'
57263b4c Merge remote-tracking branch 'upstream/main'
73085a9f Merge remote-tracking branch 'upstream/main'
```

## Fork Decisions To Preserve

### 1. Haiku OAuth mimicry uses full Claude Code mimicry

Owner contract: OAuth haiku requests on the mimic path must behave like
sonnet/opus. They must receive the full Claude Code beta set, including
`context-management-2025-06-27`, and full system rewrite. This intentionally
differs from upstream tests that exclude context-management for haiku.

Evidence:

- `backend/internal/pkg/claude/constants.go`: `FullClaudeCodeMimicryBetas()`
  includes `BetaContextManagement` and says OAuth accounts use the full set for
  all models, including haiku.
- `backend/internal/service/gateway_service.go`: `applyClaudeCodeOAuthMimicryToBody`
  removes the haiku bypass and always calls `rewriteSystemForNonClaudeCode`.
- `backend/internal/service/gateway_service.go`: `computeFinalAnthropicBeta`
  uses `claude.FullClaudeCodeMimicryBetas()` for OAuth mimicry and documents the
  fork decision.
- `backend/internal/service/gateway_context_management_test.go`: tests assert
  haiku keeps context-management beta on mimic paths.

Sync rule: if upstream reintroduces haiku-specific beta stripping, keep this
fork's behavior and update tests to assert `Includes/Preserves`, not
`Excludes/Strips`.

### 2. Claude Code fingerprint matches observed CLI traffic

Owner contract: mimicry should follow recent real Claude Code CLI traffic closely
enough to avoid third-party usage classification.

Evidence:

- `backend/internal/pkg/claude/constants.go`: `CLICurrentVersion = "2.1.92"`.
- `backend/internal/pkg/claude/constants.go`: default `User-Agent` is
  `claude-cli/2.1.92 (external, cli)`.
- `backend/internal/pkg/claude/constants.go`: full mimicry betas include
  `prompt-caching-scope-2026-01-05`, `effort-2025-11-24`,
  `context-management-2025-06-27`, and `extended-cache-ttl-2025-04-11`.

Sync rule: do not blindly accept upstream beta-list simplifications on OAuth
mimicry paths. If updating the mimicked CLI version, keep `CLICurrentVersion`,
`DefaultHeaders["User-Agent"]`, billing attribution text, and tests in sync.

### 3. Generated cache breakpoints default to `5m`, not `1h`

Owner contract: when the proxy creates cache breakpoints itself, it should default
to `ttl: "5m"`. User-supplied TTL must be preserved.

Evidence:

- `backend/internal/pkg/claude/constants.go`: `DefaultCacheControlTTL = "5m"`.
- `backend/internal/service/gateway_messages_cache.go`: message cache breakpoint
  injection uses `claude.DefaultCacheControlTTL`.
- `backend/internal/service/gateway_tool_rewrite.go`: tools-last cache breakpoint
  injection uses `claude.DefaultCacheControlTTL` and preserves existing TTL.

Sync rule: preserve `5m` as this fork's default even if upstream or Parrot uses
`1h`. This is quota/cost behavior, not a cosmetic detail.

### 4. Parrot-style tool-name mimicry and cache breakpoints

Owner contract: non-Claude-Code clients using Claude OAuth should more closely
mimic Claude Code/Parrot request shape by rewriting selected tool names and
restoring names in responses.

Evidence:

- Historical local commit: `ce600fae feat(gateway): port Parrot tool-name obfuscation + message cache breakpoints`.
- `backend/internal/service/gateway_tool_rewrite.go`: implements static and
  dynamic tool-name rewrite, `tool_choice.name` rewrite, historical `tool_use`
  rewrite, tools-last cache breakpoint, and response-side restoration.
- `backend/internal/service/gateway_messages_cache.go`: strips unstable message
  cache controls and re-injects stable breakpoints.
- `backend/internal/service/gateway_service.go`: `applyClaudeCodeOAuthMimicryToBody`
  runs message cache rewrite, tool rewrite, and tools-last breakpoint in order.

Sync rule: check these files even when the final diff against upstream is small;
upstream may have partially absorbed similar code, but ordering and TTL policy
still matter for this fork.

### 5. Opus 4.8 is available in the fork model list

Owner contract: keep local support for `claude-opus-4-8` unless upstream has a
newer equivalent.

Evidence:

- `backend/internal/pkg/claude/constants.go`: `DefaultModels` contains
  `claude-opus-4-8` with display name `Claude Opus 4.8`.

Sync rule: when upstream refreshes model metadata, verify `claude-opus-4-8` is
not removed from this fork unless the owner explicitly asks.

### 6. Context-management body/header symmetry is a safety rule

Owner contract: `context_management` may only be forwarded when the final outgoing
`anthropic-beta` header includes `context-management-2025-06-27`. Body cleanup
must happen before CCH signing.

Evidence:

- `backend/internal/service/gateway_request.go`: `sanitizeAnthropicBodyForBetaTokens`
  strips `context_management` if the final beta header lacks the token.
- `backend/internal/service/gateway_request.go`: `removeThinkingDependentContextStrategies`
  removes `clear_thinking_20251015` when thinking is disabled.
- `backend/internal/service/gateway_context_management_test.go`: tests cover
  body/header symmetry and haiku behavior.

Sync rule: never move beta computation after body signing if it makes CCH hash
and final upstream body diverge.

### 7. Trailing thinking/tool cleanup retries are local resilience fixes

Owner contract: when Anthropic rejects a request due to final thinking blocks or
signature-sensitive tool/thinking structure, retry with a cleaned request rather
than failing the user immediately.

Evidence:

- Local commits: `fc29ae92` and `8ba648e0`.
- `backend/internal/service/gateway_request.go`: `FilterSignatureSensitiveBlocksForRetry`
  converts signature-sensitive `thinking`, `redacted_thinking`, `tool_use`, and
  `tool_result` blocks to safer text or removes them as needed.
- `backend/internal/service/gateway_anthropic_apikey_passthrough_test.go`,
  `gateway_request_test.go`, and `gateway_service_signature_test.go` provide
  regression coverage.

Sync rule: preserve tests first, then reapply implementation around upstream
refactors. This behavior touches model output quality and retry semantics.

### 8. OpenAI Responses placeholder tool cleanup

Owner contract: OpenAI/Codex request bodies may contain placeholder tools with
invalid types such as `"None"`, `"null"`, or empty string. The gateway should drop
those entries before forwarding, both on transformed and passthrough paths.

Evidence:

- Local commits: `4255831c` and `bad3442d`.
- `backend/internal/service/openai_codex_transform.go`: `isInvalidCodexToolPlaceholderType`
  and `dropInvalidPlaceholderTools` remove invalid placeholder tools.
- `backend/internal/service/openai_gateway_service.go`: calls the cleanup in both
  `/v1/responses` normalization and passthrough paths.
- `backend/internal/service/openai_codex_transform_test.go` and
  `openai_gateway_service_test.go` cover mixed valid/invalid tools and passthrough.

Sync rule: this is likely upstreamable, but until upstream has equivalent behavior,
keep both the non-passthrough and passthrough hooks.

### 9. Incomplete OpenAI Responses SSE data is suppressed before stream errors

Owner contract: if an OpenAI Responses stream errors after partial data, do not leak
known-broken incomplete SSE data as if the response completed cleanly.

Evidence:

- Local commit: `65b45ee3`.
- `backend/internal/service/openai_gateway_service.go`: logs suppression of
  incomplete SSE data before stream errors.
- `backend/internal/service/openai_gateway_service_test.go`: includes regression
  coverage for partial reasoning summary and incomplete stream handling.

Sync rule: preserve terminal-event and usage-completeness checks when upstream
changes streaming code.

## Generated, Vendor, And Noise Policy

Do not treat these as fork customizations unless a source change requires them:

| Path | Policy |
| --- | --- |
| `.codegraph/**` | Local generated CodeGraph index. Never commit. |
| `.omo/**` | Local planning/evidence artifacts. Useful for reference, not product code. Do not include in upstream PRs. |
| `.playwright-mcp/**` | Browser/session artifact. Never commit. |
| `backend/ent/**` | Generated Ent output. Commit only when `backend/ent/schema/**` changes and generation was rerun. |
| `backend/cmd/server/wire_gen.go` | Generated Wire output. Commit only after DI source changes and `go generate ./cmd/server`. |
| `backend/internal/web/dist/**` | Frontend build output. Commit only for release/embed workflow if intentionally regenerated. |
| `frontend/pnpm-lock.yaml` | Dependency lock. Commit only after intentional frontend dependency changes. |
| `backend/go.sum` | Go dependency lock. Commit only after intentional Go dependency changes. |
| `backend/migrations/**` | Forward-only migrations. Never edit applied migrations; add a new numbered migration. |

## High-Risk Sync Files

Always inspect these after `git merge upstream/main` or `git rebase upstream/main`:

```text
backend/internal/pkg/claude/constants.go
backend/internal/service/gateway_service.go
backend/internal/service/gateway_request.go
backend/internal/service/gateway_context_management_test.go
backend/internal/service/gateway_anthropic_apikey_passthrough_test.go
backend/internal/service/openai_codex_transform.go
backend/internal/service/openai_codex_transform_test.go
backend/internal/service/openai_gateway_service.go
backend/internal/service/openai_gateway_service_test.go
```

Also check these historical customization files if upstream touches cache/tool
mimicry logic:

```text
backend/internal/service/gateway_tool_rewrite.go
backend/internal/service/gateway_tool_rewrite_test.go
backend/internal/service/gateway_messages_cache.go
backend/internal/service/gateway_forward_as_chat_completions.go
backend/internal/service/gateway_forward_as_responses.go
```

## Future Upstream Sync Checklist

1. Refresh refs and record the baseline.

   ```bash
   GIT_MASTER=1 git fetch upstream --prune
   GIT_MASTER=1 git status --short --branch
   GIT_MASTER=1 git rev-parse HEAD
   GIT_MASTER=1 git rev-parse upstream/main
   GIT_MASTER=1 git merge-base HEAD upstream/main
   ```

2. Compare current fork delta.

   ```bash
   GIT_MASTER=1 git log --oneline --decorate --reverse upstream/main..HEAD
   GIT_MASTER=1 git cherry -v upstream/main HEAD
   GIT_MASTER=1 git diff --stat upstream/main...HEAD
   GIT_MASTER=1 git diff --name-status upstream/main...HEAD
   ```

3. Confirm the fork contracts still hold.

   ```bash
   rg -n "FullClaudeCodeMimicryBetas|context-management-2025-06-27|haiku patch" backend/internal/pkg/claude backend/internal/service
   rg -n "DefaultCacheControlTTL|cache_control|ttl" backend/internal/pkg/claude backend/internal/service/gateway_* backend/internal/service/openai_*
   rg -n "dropInvalidPlaceholderTools|Unsupported tool type: None|placeholder tools" backend/internal/service/openai_*
   rg -n "FilterSignatureSensitiveBlocksForRetry|trailing thinking|stream usage incomplete" backend/internal/service
   ```

4. Run targeted tests before deployment.

   ```bash
   cd backend && go test ./internal/service -run 'Test.*(ContextManagement|Haiku|Placeholder|Thinking|SSE|Signature)' -count=1
   cd backend && go test ./internal/service -run 'TestFullClaudeCodeMimicryBetas|Test.*OpenAI.*Placeholder|Test.*Passthrough' -count=1
   cd backend && go build ./...
   ```

5. After deployment, verify live behavior, not just local tests.

   ```bash
   curl -s http://127.0.0.1:18080/version
   curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18080/
   ```

For the haiku mimicry contract, use the live gateway debug log when enabled and
verify that a mimic-path haiku forward carries `context-management-2025-06-27`
and does not trigger the third-party usage warning.

## Upstream PR Candidate Notes

`.omo/plans/sub2api-upstream-pr-sequencing.md` recorded two possible upstream PR
candidates from earlier work. Treat `.omo/` as untracked local evidence, not a
source file.

Candidate 1, OpenAI Responses invalid placeholder tools, is relatively isolated:

```text
backend/internal/service/openai_codex_transform.go
backend/internal/service/openai_codex_transform_test.go
backend/internal/service/openai_gateway_service.go
```

Candidate 2, haiku OAuth beta/header behavior, is higher risk and intentionally
may conflict with upstream's stance:

```text
backend/internal/pkg/claude/constants.go
backend/internal/service/gateway_service.go
```

Do not open upstream PRs from this private `main` branch. If the owner wants to
submit upstream changes, create clean branches from `upstream/main` and apply only
the candidate whitelist.

## When This File Is Stale

Refresh this inventory whenever any of these happen:

- `upstream/main` is merged or rebased into this fork.
- Any file under `backend/internal/pkg/claude/` or `backend/internal/service/gateway*`
  changes.
- Any OpenAI/Codex compatibility file under `backend/internal/service/openai*`
  changes.
- Live deployment behavior contradicts local tests.

If a future audit finds a local-only commit with no current effective diff, keep it
in the history table but mark it as absorbed by upstream or obsolete. Do not delete
the context unless the owner confirms the behavior is no longer needed.
