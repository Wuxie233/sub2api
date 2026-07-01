---
name: sub2api-update
description: Run the private sub2api-fork upstream update workflow end-to-end: fetch and merge upstream/main, preserve this fork's local gateway/custom OAuth behavior, verify backend/frontend checks, build a tagged local Docker image, update /opt/sub2api with an app-only container recreate (handling the manual-container name conflict), verify live health, and record rollback details. Use whenever the owner says 更新, update sub2api, sync upstream, redeploy sub2api, or asks to run the Sub2API update flow.
---

# Sub2API Update

Use this skill only in `/root/CODE/sub2api-fork`. This is the owner's private fork of `Wei-Shaw/sub2api`; local fork behavior wins over upstream when they conflict.

## Safety Contract

- Do not read, print, edit, copy, or rotate `/opt/sub2api/.env`.
- Push to `origin` (`Wuxie233/sub2api`) by default after green verification and a healthy deploy — no need to ask. NEVER push to `upstream` (`Wei-Shaw/sub2api`) and NEVER force-push.
- Do not run `git reset --hard`, broad `git clean`, `docker compose down`, volume removal, image prune, or database destructive commands.
- Do not restart postgres, redis, proxy, or unrelated containers. The only allowed deploy action is app-only recreate of `sub2api` (which may require stopping/renaming the old `sub2api` container — that is still app-only).
- If a conflict changes product behavior rather than code mechanics, ask one business-level question. Resolve mechanical conflicts yourself.
- Keep `.codegraph/`, `.omo/`, `.playwright-mcp/`, and temporary evidence out of commits. Commit identity is `Wuxie233 <445714414@qq.com>` (override per-commit with `GIT_AUTHOR_*`/`GIT_COMMITTER_*`).

## Fork Decisions To Preserve

When these conflict with upstream, the fork wins. Each has a locking test where noted.

- **Haiku = full Claude Code mimicry** (no haiku bypass), including `context-management-2025-06-27`, on the OAuth mimic path. Drop any upstream `!strings.Contains(model,"haiku")` bypass; gate the full system rewrite only on the system-prompt-injection toggle. Locked by `TestComputeFinalAnthropicBeta_OAuthMimic_Haiku_IncludesContextManagement` and the haiku context-management end-to-end tests.
- **Reactive-only trailing-thinking cleanup**: the fork does NOT proactively pre-filter thinking blocks on the initial `Forward` send; cleanup happens only reactively on a 400 signature-error retry (preserves validly-signed historical thinking even when top-level thinking is disabled). Upstream periodically re-adds a proactive `FilterThinkingBlocks(body, reqModel)` call in `Forward` (after `StripEmptyTextBlocks`) — REMOVE it. Locked by `TestGatewayService_Forward_PreservesHistoricalTrailingThinkingOnInitialSend`.
- **CCH signing is RETIRED**: upstream removed `signBillingHeaderCCH`/`enableCCH`; `EnableCCHSigning` is now a no-op. Do NOT resurrect a `if enableCCH { body = signBillingHeaderCCH(body) }` block during conflict resolution — it will fail to compile (`undefined: enableCCH/signBillingHeaderCCH`). KEEP the local `ensureToolInputSchemas` guard (runs right before `http.NewRequestWithContext` in `buildUpstreamRequest`).
- **OpenAI SSE incomplete-frame gate** (`openAISSEFrameGate`) + multiline `data:` preservation. Keep the frame-gate framework (`writeBufferedLines` / `flushAfterSSEWrite` / `forwardStandaloneSSELine` / `processSSEFrame` / `processGateResult` / `frameGate.addLine` / `flushSSEFrameGate`). FOLD upstream's newer additions INTO the frame-gate `processSSEFrame`: cyber-policy `MarkOpsCyberPolicy` (parse usage FIRST, then Mark), `normalizeOpenAIResponsesFunctionCallArguments`, and `sanitizeOpenAIResponseFailedEventForClient`. There are TWO such functions (the passthrough one and the keepalive/streaming one) — patch both.
- **Invalid placeholder OpenAI tool cleanup** for both non-passthrough and passthrough `/responses` paths, plus the escaped-`\u` tools path.
- **Signature-sensitive retry cleanup** intact: adopt upstream `FilterThinkingBlocksForRetry(body, reqModel)` 2-arg signature but keep the local `isFinalBlockThinkingError → NormalizeHistoricalTrailingThinkingBlocks` branch on the retry paths.
- Cache control TTL default remains `5m`. Parrot-style tool-name obfuscation and message cache breakpoints intact.
- **Model support**: `claude-opus-4-8` and `claude-sonnet-5*` (fork-added Anthropic models) remain intact.

## Workflow

1. Preflight the repo and live deployment:
   - `git status --short --branch`
   - `docker ps --filter "name=^/sub2api$" --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}'`
   - `docker inspect sub2api --format '{{.Config.Image}}'` — the ACTUAL running image. It can DRIFT from `docker-compose.override.yml` (past manual deploys); the true rollback target is the running image, not the override's.
   - Verify `/opt/sub2api/docker-compose.yml` and `/opt/sub2api/docker-compose.override.yml` exist.
   - Record current `HEAD`, running image tag, override image tag, and old-image availability (`docker images | rg sub2api-local`).
2. Create a reversible checkpoint before merging:
   - Checkpoint branch `pre-sub2api-update-<YYYYMMDDHHMMSS>` at current HEAD.
   - Save a dirty-diff patch (`git diff > /flyshop/opencode/tmp/sub2api-local-dirty-<ts>.patch`).
   - If there are intentional uncommitted local changes, commit them as atomic local commits (author `Wuxie233`) BEFORE the merge so nothing is lost.
3. Sync upstream: `git fetch upstream --prune`; record `upstream/main` commit, new tags, and `git rev-list --left-right --count HEAD...upstream/main`.
4. Merge upstream with `git merge --no-commit --no-ff upstream/main`, then resolve conflicts preserving every Fork Decision above. Conflicts almost always land in the same 4 files — see **Conflict Playbook** below. After resolving: `rg -n '<<<<<<<|>>>>>>>|^=======$'` must be empty everywhere and `git diff --check` clean.
5. Regenerate only if needed: Ent schema changed → `cd backend && go generate ./ent`; Wire providers changed → `cd backend && go generate ./cmd/server`. (Upstream usually commits generated Ent/Wire already; regenerate only if the merge left them inconsistent.)
6. Verify before deploy (stop on any red):
   - `cd backend && go build ./...` and `go vet ./internal/service/`.
   - `go test ./internal/service/ -run 'PreservesHistoricalTrailingThinking|Haiku|ContextManagement|Placeholder|InvalidTool|EnsureToolInputSchemas|SSE|Signature|Thinking' -count=1` (targeted fork-decision run).
   - `go test ./internal/service/ -count=1 -timeout=180s`, `go test ./internal/handler/... -count=1`, `go test ./internal/repository/ -count=1` (unit; integration tests are build-tagged and skipped).
   - `pnpm --dir frontend run typecheck`.
   - Never pipe `go test ... | tail` in a way that swallows the exit code before a `&&` deploy — check the real exit status.
7. Commit locally after green verification:
   - `git diff --name-only --diff-filter=U` must be empty. Stage resolved files explicitly (avoid `git add -A` so no `.codegraph`/`.omo`/temp leaks in). Confirm no untracked junk is staged.
   - Merge commit (author/committer `Wuxie233`): `merge upstream main (vX.Y.Z)` with a body listing the fork decisions re-applied. Do NOT push yet (push happens after a healthy deploy, step 13).
8. Build a fresh local image from the repo-root `Dockerfile`:
   - Version from `backend/cmd/server/VERSION`. Tag `sub2api-local:<version>-upstream-<YYYYMMDDHHMM>`.
   - Build args `GOPROXY=https://goproxy.cn,direct`, `GOSUMDB=sum.golang.google.cn`, `COMMIT=<merge-hash>`.
   - **Base-image pull drift**: upstream sometimes bumps the Dockerfile `POSTGRES_IMAGE`/`GOLANG_IMAGE` ARG to a tag the local daocloud registry mirror can't fetch (build fails on `load metadata ... EOF`). Fix: `docker pull` the needed `golang:<x>-alpine` / `alpine:<x>` tags directly first, and override `--build-arg POSTGRES_IMAGE=postgres:15-alpine` (matches the deployed Postgres 15 and only feeds pg_dump/psql into the final image).
   - Run the build in the background writing to a log file and poll it (`nohup docker build ... > LOG 2>&1 &`); a full multi-stage build takes ~7-10 min. Do NOT use one-off COPY-a-prebuilt-binary Dockerfiles.
9. Stage deployment with rollback: back up `/opt/sub2api/docker-compose.override.yml` to `...bak-<YYYYMMDDHHMMSS>`, then set `services.sub2api.image` to the new tag (only that line).
10. Recreate ONLY the app container:
   - `docker compose -f /opt/sub2api/docker-compose.yml -f /opt/sub2api/docker-compose.override.yml up -d sub2api`.
   - **Manual-container name conflict (common here)**: the live `sub2api` container was often created by a manual `docker run` (no `com.docker.compose.*` labels), so compose cannot claim the `sub2api` name and errors `Conflict. The container name "/sub2api" is already in use`. Compose may also leave a stray `<hash>_sub2api` in `Created` state. Recover (still app-only, pg/redis/proxy untouched):
     1. `docker rm <stray-created-id>` (the `Created` leftover, if any).
     2. `docker stop sub2api` then `docker rename sub2api sub2api-manual-rollback-<ts>` (frees the name AND keeps the old container for instant rollback).
     3. Re-run `docker compose ... up -d sub2api`. The new container comes up under compose management on `sub2api-network`.
   - Confirm postgres/redis/proxy stayed `Running`/`Healthy` and were NOT recreated.
11. Post-deploy verify through the live surface:
   - `docker inspect sub2api --format '{{.Config.Image}} {{.State.Status}} {{.State.Health.Status}}'` → running + healthy (healthcheck start_period ~30s; wait for it).
   - `curl -fsS --max-time 10 -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18080/health` and `.../` → 200; public `https://sub.wuxie233.com/health` → 200.
   - `/version` returns the SPA **HTML**, NOT a version string — do NOT treat it as a version probe. Use the image tag + `backend/cmd/server/VERSION`.
   - If the merge added migrations: confirm they applied at startup — `psql ... "select filename, applied_at from schema_migrations order by applied_at desc limit 8;"` (columns are `filename`,`checksum`,`applied_at`).
   - Skim `docker logs sub2api` for `Server started on 0.0.0.0:8080`, no panic/fatal, and real gateway traffic. Environmental noise (`no available accounts`, a transient upstream `EOF`) is not a deploy regression.
12. Record the run in `~/.config/opencode/memory/ops-log-sub2api-opencode.md`: new image tag + ID, override backup path, upstream commit/tag, merge commit hash, rollback image + renamed rollback container, verification evidence, any warnings, and any fork decision re-applied.
13. Push to origin (default): confirm fast-forward (`git merge-base --is-ancestor origin/main main`), then `git push origin main`. Push only to `origin` (`Wuxie233/sub2api`); NEVER `upstream`; never force-push.

## Conflict Playbook (the recurring 4 files)

- `.dockerignore` / `Dockerfile`: keep `docs/legal` in the Docker build context (frontend AdminCompliance raw-imports `docs/legal/*.md`).
- `backend/internal/service/gateway_service.go`: apply the haiku, thinking-retry-signature, CCH-removal, and reactive-thinking (remove proactive `FilterThinkingBlocks`) decisions above.
- `backend/internal/service/openai_gateway_service.go`: keep the `openAISSEFrameGate` framework; fold upstream cyber-policy + function-args + failed-event-sanitize into BOTH `processSSEFrame` functions.
- For a large, tab-sensitive conflict hunk, resolve deterministically with a small "keep ours" Python script (walk lines, on `<<<<<<< HEAD` keep the ours block, drop through `>>>>>>> upstream/main`) rather than a fragile manual edit — then verify no markers remain and `git build`/tests pass. Fold in any genuinely-new upstream logic as a SEPARATE targeted edit afterward.

## Rollback

If the new app is unhealthy or behavior regresses (fastest first):

1. If you renamed the old container in step 10: `docker stop sub2api && docker rm sub2api && docker rename sub2api-manual-rollback-<ts> sub2api && docker start sub2api`.
2. Or restore the override backup / set `services.sub2api.image` back to the prior running tag (old image is still present) and `docker compose -f /opt/sub2api/docker-compose.yml -f /opt/sub2api/docker-compose.override.yml up -d sub2api`.
3. Re-run the step 11 health checks. Report the failed new tag, restored tag, and evidence.

## Final Report

Report in Chinese with: upstream commit/tag integrated; merge commit hash and push result; new image tag and rollback image tag; override backup path + renamed rollback container; verification commands that passed; live health + migration evidence; any non-blocking warnings.
