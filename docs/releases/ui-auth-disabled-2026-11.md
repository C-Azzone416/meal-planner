# November 2026 UI/auth-disabled release scope

This document describes the full change being promoted to main by release PR #56. It is a review checklist, not acceptance of any remaining risk. QA must consolidate the full scope and evidence before an independent judge reviews the same head. Any later code change requires regressions and renewed sequential review. CI must be green on that head and on the resulting main commit before deployment.

## Included product surfaces

- Responsive browser UI and browser-local planning, shopping, prep and cooking flows, with per-account storage isolation and explicit unsaved/unavailable states.
- Restricted authentication code: exact identity policy, same-origin proxy, bearer verification, signed before-create webhook, password-code notice and controlled activation runbook. Production authentication stays disabled for this code release.
- Manual recipe HTTP API and reviewed Neon/Postgres schema files, ownership and revision checks, idempotent create receipts, bounded responses, and recipe detail/edit rewrites. The code release does not apply migrations or install runtime credentials.
- **Local recipe-draft epic #19**, including `cmd/mealplanner`, `cmd/mealplanner-mcp`, `internal/core/draft`, `internal/localstore`, `internal/mcpadapter`, their contracts, tests and local-setup documentation. This is a separate included product surface, not the paused hosted-draft bridge #55. The CLI provides read-only validation/list/show; the stdio MCP server supports local draft creation and revision-controlled updates in a private local directory (the default per-user configuration directory, or an explicit override). These tools have no hosted route and are not started by Vercel.
- Current-main CI bootstrap and reviewed-evidence infrastructure (#65 and the CI-only #66 split), refreshed through `dfeda615d21cbe1ece12a39bd9378d44191f1c49`. Framebriar application integration (#54), the deferred private Preview exporter, and runtime activation are excluded. Private-dependency Preview targets remain blocked without a direct-build fallback.

## Dependency and publication boundary

The full repository promotion includes the pinned Go MCP SDK `github.com/modelcontextprotocol/go-sdk v1.8.0`, `github.com/google/jsonschema-go v0.4.3`, `github.com/lib/pq v1.10.9`, and their `go.mod`/`go.sum` transitive dependencies. They must be reviewed as part of the complete release diff, even where they do not affect a deployed web route. The local tools' standalone release workflow is triggered only by `cli-v*` tags or an explicit dispatch. This hosted release creates only an `app-v*` tag; it does not publish CLI/MCP binaries or alter any host's MCP configuration.

## Required re-review of judge findings

1. Explicitly review the included local CLI/MCP surface and dependency changes above; do not mistake #55 exclusion for exclusion of all local draft tooling.
2. Confirm every runtime authentication entry point rejects incomplete/malformed provider and allowlist configuration, including session routes; reject provider URL query/fragment delimiters.
3. Confirm JWT NumericDate claims require JSON numbers and compare fractional boundaries without truncation or overflow. Review the explicit representation/size limits.
4. Confirm malformed query encoding, unknown/duplicate/case-variant JSON properties, null or inappropriate revision fields and empty IDs cannot trigger storage calls. Verify the entire 256 KiB request limit, including whitespace and extra data, and the existing response-size budget.
5. Confirm local store reads and writes remain pinned to the opened root/drafts directory across pathname substitution. Review the documented OS, advisory-lock, hard-link/mount and availability boundaries; documentation alone does not accept a risk.
6. Confirm exact supported JSON-RPC ID correlation, rejection of unsupported numeric representations and malformed-Unicode string IDs before SDK dispatch, no rejected/later tool execution, and bounded orderly shutdown. Review compatibility implications for configured MCP clients.
7. Execute the runbook's token-safe alias recovery example with a fake curl, proving a synthetic token travels only through stdin and not process arguments. No live alias or credential change is part of the test.

## Remaining deployment and acceptance gates

- Review all application and local-tool regressions, full CI, workflow/shell checks and disposable-Postgres integration on the exact final head. A clean incremental whitespace diff is not a claim about intentionally truncated historical JSON fixtures in the full release diff.
- Use only the existing authorized owner review exception if needed; never bypass required CI or weaken protection rules. Preserve shared history and develop.
- Immediately before deployment, verify Production auth variables remain absent from project/shared configuration and deployment overrides. A green environment-validation job alone does not prove auth is disabled: it also permits a complete valid auth configuration.
- After deployment, verify the exact commit and public alias, `GET /api/auth-config` returning `enabled:false`, disabled sign-in, protected-API rejection, and direct UI routes. Prior old-production 404 responses do not prove these new behaviors.
- Provider/account provisioning, persistent credentials, runtime database role, migration execution and authentication activation remain separate approval-controlled work. Live sign-in/password recovery and cross-browser recipe durability require their own acceptance checks; no success is implied by code-only deployment.
