# November 2026 UI/auth-disabled release scope

This document describes the web release promoted to main by PR #56. QA must approve the complete scope and evidence before an independent judge reviews the same head. Any later code change requires regressions and renewed sequential review. Required CI must pass on that head and on the resulting main commit before deployment.

## Included product surfaces

- Responsive browser UI and browser-local planning, shopping, prep and cooking flows, with per-account storage isolation and explicit unsaved/unavailable states.
- Restricted authentication code: exact identity policy, same-origin proxy, bearer verification, signed before-create webhook, password-code notice and controlled activation runbook. Production authentication stays disabled for this code release.
- Manual recipe HTTP API and reviewed Neon/Postgres schema files, ownership and revision checks, idempotent create receipts, bounded responses, and recipe detail/edit rewrites. This release does not apply migrations or install runtime credentials.
- Shared standard-library code under `internal/core/draft`, with its tests and fixtures. Hosted recipes use its strict JSON-string Unicode validation. Retaining this package does not expose its draft service or in-memory store through a command, MCP server or hosted draft endpoint.
- Existing main command scaffolds: `mealplanner version` and `mealplanner-mcp --version`. The MCP scaffold drains stdin without dispatching requests or writing drafts. These are unchanged from current main, not the local draft product.
- Current-main CI bootstrap and reviewed-evidence infrastructure (#65 and the CI-only #66 split), refreshed through `dfeda615d21cbe1ece12a39bd9378d44191f1c49`. Framebriar application integration (#54), the deferred private Preview exporter, and runtime activation are excluded. Private-dependency Preview targets remain blocked without a direct-build fallback.

## Deferred local-tool surface

The executable and filesystem portions of local recipe-draft epic #19 are excluded from this web release: `draft validate/list/show`, local draft persistence, the MCP SDK server, tool dispatch, and MCP create/update operations. `internal/localstore` and `internal/mcpadapter` are absent, and both commands retain only their main scaffold behavior. The MCP SDK, JSON Schema SDK and their unused transitive dependencies are removed from the release module.

The work remains in development history, including pre-split commit `ad556e9585971ee7409142eda56e332f43bb1cc9`; no shared history, user draft files, installed binaries or agent-host configuration are changed. That snapshot has unresolved duplicate MCP tool-parameter and in-flight ID findings. Exclusion is not approval or a claim that those bugs are fixed. Restoring the local product requires a separate reviewed change that addresses those findings and reruns its full protocol, persistence and host-compatibility acceptance checks.

The paused MCP-to-hosted draft bridge #55 remains excluded independently. The hosted manual recipe API remains included; it is not a local MCP endpoint.

### Separate #23 restore candidate

The candidate restores the retained CLI, local store, MCP adapter and pinned SDK dependencies for review into `develop`. The scope above describes what PR #56 shipped. The candidate's stdio gate rejects ambiguous tool-call parameters and IDs reused within a session before dispatch, while giving previously accepted work its normal shutdown grace.

Candidate acceptance requires independent review and renewed #25 host verification on the reviewed source. Caroline's personal Preview test is required before Production shipment. Only Caroline or Bagel can authorize cutting a release branch from reviewed `develop`. Promotion to `main` then requires a reviewed, CI-passing PR. [Local draft workflow](../local-workflow.md)

## Dependency and publication boundary

The release keeps the PostgreSQL driver `github.com/lib/pq v1.10.9`, YAML contract-test dependency `gopkg.in/yaml.v3 v3.0.1`, and only their required module checksums. Review the resulting `go.mod`/`go.sum` rather than assuming every prior dependency remains.

The standalone CLI publication workflow remains gated by `cli-v*` tags or explicit dispatch and the shared exact-main CI verifier. This hosted release uses only an `app-v*` tag; it does not publish command binaries or change any host's MCP configuration. Required CI, remaining shared-core and command tests, cross-builds, contract checks, disposable-Postgres integration and deployment safeguards remain active.

## Required review checklist

1. Verify the local-tool exclusion in source, imports, module dependencies and actual scaffold binaries. No draft command, MCP tool dispatch or filesystem mutation may remain. Preserve the complete shared-core validation tests and hosted Unicode checks; do not treat removal of an executable surface as permission to skip tests of retained code.
2. Confirm every runtime authentication entry point rejects incomplete or malformed provider and allowlist configuration, including session routes. Validate the full managed authority, including empty/explicit ports, Unicode/DNS boundaries, encoded paths, query/fragment delimiters and canonical issuer handling.
3. Confirm unambiguous auth JSON before typed decoding: duplicate decoded keys, security-field case aliases, malformed Unicode, null coercion and excessive nesting are rejected. JWT NumericDates require JSON numbers and exact fractional comparisons. Preserve documented verification spellings and distinct-key rotation.
4. Enforce context-specific JOSE profiles and the trusted signing-key source: bearer tokens accept omitted `typ` or `JWT`, while detached webhooks accept omitted `typ` or Neon's documented `JWS`. A type from the other context and any `cty` declaration must fail. Exercise the documented signed webhook envelope throughout the webhook regression matrix. JWKS redirects must not replace the configured provider endpoint, including same-origin redirects, HTTPS cross-origin redirects and HTTP downgrades. Test signed bearer and webhook paths with real HTTP redirect handling and prove no redirected destination is contacted.
5. Validate original email bytes before trimming. Unicode padding must be rejected by the bearer verifier, signed webhook and every proxy email route. Only outer ASCII SP/HT/CR/LF and ASCII case differences are accepted; the proxy forwards the exact configured lowercase identity. Rejected identities must not reach a protected handler or email provider.
6. Confirm malformed query encoding, unknown/duplicate/case-variant recipe JSON properties, malformed Unicode in every string/key, null or inappropriate revision fields and empty IDs cannot trigger storage calls. Verify POST/PUT, preserved valid Unicode, the full 256 KiB input limit and existing response-size budget.
7. Execute the runbook's token-safe alias recovery example with a fake curl, proving a synthetic token travels only through stdin and not process arguments. No live alias or credential change is part of that test.

## Remaining deployment and acceptance gates

- Review all retained application/core/scaffold regressions, full CI, workflow/shell checks and disposable-Postgres integration on the exact final head. The intentionally truncated core JSON fixture remains a disclosed full-diff whitespace exception.
- Use only the existing authorized owner review exception if needed; never bypass required CI or weaken protection rules. Preserve shared history and develop.
- Immediately before deployment, verify Production auth variables remain absent from project/shared configuration and deployment overrides. A green environment-validation job alone does not prove auth is disabled: it also permits a complete valid auth configuration.
- After deployment, verify the exact commit and public alias, `GET /api/auth-config` returning `enabled:false`, disabled sign-in, protected-API rejection, and direct UI routes. Prior old-production 404 responses do not prove these new behaviors.
- Provider/account provisioning, persistent credentials, runtime database role, migration execution and authentication activation remain separate approval-controlled work. Live sign-in/password recovery and cross-browser recipe durability require their own acceptance checks; no success is implied by code-only deployment.
