---
name: sub2api-update
description: Run the private sub2api-fork upstream update workflow end-to-end: fetch and merge upstream/main, preserve this fork's local gateway/custom OAuth behavior, verify backend/frontend checks, build a tagged local Docker image, update /opt/sub2api with an app-only container recreate, verify live health, and record rollback details. Use whenever the owner says 更新, update sub2api, sync upstream, redeploy sub2api, or asks to run the Sub2API update flow.
---

# Sub2API Update

Use this skill only in `/root/CODE/sub2api-fork`. This is the owner's private fork of `Wei-Shaw/sub2api`; local fork behavior wins over upstream when they conflict.

## Safety Contract

- Do not read, print, edit, copy, or rotate `/opt/sub2api/.env`.
- Do not push to `origin` or `upstream` unless the owner explicitly asks.
- Do not run `git reset --hard`, broad `git clean`, `docker compose down`, volume removal, image prune, or database destructive commands.
- Do not restart postgres, redis, proxy, or unrelated containers. The only allowed deploy action is app-only recreate of `sub2api`.
- If a conflict changes product behavior rather than code mechanics, ask one business-level question. Resolve mechanical conflicts yourself.
- Keep `.codegraph/`, `.omo/`, `.playwright-mcp/`, and temporary evidence out of commits.

## Fork Decisions To Preserve

- Haiku OAuth mimicry uses full Claude Code betas, including `context-management-2025-06-27`, on the mimic path.
- Cache control TTL default remains `5m` where this fork has patched it.
- Parrot-style tool-name obfuscation and message cache breakpoints remain intact.
- Invalid placeholder OpenAI tools are dropped for both non-passthrough and passthrough `/responses` paths.
- OpenAI SSE incomplete-frame suppression and multiline `data:` preservation remain intact.
- Signature-sensitive retry cleanup remains intact.
- Opus 4.8 model support remains intact.

## Workflow

1. Preflight the repo and live deployment:
   - `git status --short --branch`
   - `docker ps --filter "name=^/sub2api$" --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}'`
   - Verify `/opt/sub2api/docker-compose.yml` and `/opt/sub2api/docker-compose.override.yml` exist.
   - Record current `HEAD`, current `sub2api` image tag, current override file, and rollback image availability.
2. Create a reversible checkpoint before merging:
   - Create a checkpoint branch named `pre-sub2api-update-<YYYYMMDDHHMMSS>`.
   - If there are intentional local changes that must be preserved, make a local checkpoint commit before upstream merge.
3. Sync upstream:
   - `git fetch upstream --prune`
   - Record `upstream/main` commit and behind count from local `main`.
4. Merge upstream:
   - Use `git merge --no-commit --no-ff upstream/main`.
   - Resolve conflicts preserving every fork decision above.
   - For `gateway_service.go`, verify haiku full beta/context-management behavior is still asserted by tests.
   - For `openai_gateway_service.go`, verify placeholder-tool cleanup does not force full JSON decode on valid image-generation tools with huge numeric literals.
5. Regenerate only if needed:
   - If Ent schema changed, run `cd backend && go generate ./ent`.
   - If Wire providers changed, run `cd backend && go generate ./cmd/server`.
6. Verify before deploy:
   - `git diff --check`
   - LSP diagnostics on every changed hotspot file.
   - `cd backend && go test ./internal/service -run 'ContextManagement.*Haiku|OAuth.*Haiku|Placeholder|InvalidTool|SSE|IncompleteData|MultiLine|Signature|ThinkingCleanup|Retry|ImageToolBillingDoesNotForceFullDecode' -count=1 -v`
   - `cd backend && go test ./internal/service/ -count=1 -timeout=120s`
   - `cd backend && go build ./...`
   - `pnpm --dir frontend run typecheck`
   - Stop before deploy if any required check is red.
7. Commit locally after green verification:
   - Confirm `git diff --name-only --diff-filter=U` is empty.
   - Stage only intended tracked merge changes and generated files.
   - Create a local commit such as `merge upstream main`.
   - Do not push.
8. Build a fresh local image from the repo root `Dockerfile`:
   - Read version from `backend/cmd/server/VERSION`.
   - Tag as `sub2api-local:<version>-upstream-<YYYYMMDDHHMM>` or a similarly specific short description.
   - Use build args `GOPROXY=https://goproxy.cn,direct` and `GOSUMDB=sum.golang.google.cn`.
   - Do not use one-off minimal COPY-a-prebuilt-binary Dockerfiles for real updates.
9. Stage deployment with rollback:
   - Back up `/opt/sub2api/docker-compose.override.yml` to `/opt/sub2api/docker-compose.override.yml.bak-<YYYYMMDDHHMMSS>`.
   - Replace only `services.sub2api.image` with the new local image tag.
10. Recreate only the app container:
   - `docker compose -f /opt/sub2api/docker-compose.yml -f /opt/sub2api/docker-compose.override.yml up -d sub2api`
   - Confirm postgres and redis remain running/healthy and were not recreated intentionally.
11. Post-deploy verify through the live surface:
   - `docker ps --filter "name=^/sub2api$" --format '{{.Names}} {{.Image}} {{.Status}} {{.Ports}}'`
   - `docker inspect sub2api --format '{{.Config.Image}} {{.State.Status}} {{.State.Health.Status}}'`
   - `curl -fsS --max-time 10 http://127.0.0.1:18080/health`
   - `curl -fsS --max-time 10 http://127.0.0.1:18080/version`
   - `curl -fsS --max-time 10 -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18080/`
   - Load `http://127.0.0.1:18080/` in a browser and check page title/console if browser tools are available.
12. Record the run:
   - New image tag and image ID.
   - Override backup path.
   - Upstream commit integrated.
   - Local commit hash.
   - Verification commands and any warnings.

## Rollback

If the new app is unhealthy or behavior regresses:

1. Restore the latest known-good `/opt/sub2api/docker-compose.override.yml.bak-<timestamp>` over `/opt/sub2api/docker-compose.override.yml`.
2. Run only `docker compose -f /opt/sub2api/docker-compose.yml -f /opt/sub2api/docker-compose.override.yml up -d sub2api`.
3. Re-run the live health checks above.
4. Report the failed new image tag, restored image tag, and evidence.

## Final Report

Report in Chinese with:

- upstream commit/tag integrated;
- local commit hash, without claiming it was pushed;
- new Docker image tag and old rollback image tag;
- override backup path;
- verification commands that passed;
- live health evidence;
- any warnings that did not block deploy.
