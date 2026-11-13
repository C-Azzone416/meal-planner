# Preview draft ingress: endpoint-first slice

This implements a locked-down server endpoint, **not a connected MCP workflow**.
The local stdio MCP is unchanged and still cannot send. No listener is introduced.
No Production rollout, live migration, grant, role, secret, or configuration is
created by this change. The default response is 404 in every environment.

## Implemented boundary

`POST /api/draft-submissions` accepts only `content_schema_version: 1` plus the
canonical local `content`. It applies the shared strict decoder/validator,
rejects duplicate/case-aliased fields, unsupported versions, ownership/review
injection, invalid Unicode, unknown queries, compressed bodies, wrong media
and repeated headers. Input is capped at 256 KiB and canonical content at 128 KiB.
There is no network fetch of source URLs. Structural validation is not food-safety
review. Incomplete drafts, unknown values, explicit zero and provenance survive.

The response contains the immutable unreviewed draft and immutable receipt,
with server-generated IDs, owner, timestamps and `not_started` review state.
It never creates an approved recipe or planner item. HTTP 201 is a new snapshot;
200 is an exact semantic replay; same owner/key with different canonical input
is 409. Keep the same key and body after ambiguous failure. A new key creates a
new snapshot, not an update. Response has no UI links because read/review UI does
not exist yet; this is a **partial HD1 implementation**, not the complete #77 API.

## Authority

Browser cookies and Neon JWTs are NOT send credentials. Only an opaque
`mp_lc1_pv_` + canonical base64url encoding of 32 random bytes is accepted.
No code in this slice issues one. Only its SHA-256 digest is stored; no raw token
is logged, returned, persisted with draft content, or passed in a URL. There is
no token-accepting CLI argument or environment-variable fallback.

Every request checks the grant's exact issuer, Preview environment, owner,
`draft:create` scope, expiry, revocation and approved-email membership. Membership
is loaded anew per request. Submit repeats authentication inside its transaction,
locks the grant until commit (serialized against administrative revocation),
and rechecks expiry after owner-lock wait. A replay also requires a currently
valid grant. Different grants for the same owner share quota and operation-key namespaces,
but only the originating grant can recover a receipt. A different grant gets no
content from a reused key, including with an identical body. Re-pairing cannot
retrieve a prior grant's receipt; later owner UI recovery is separate.

This does **not** claim provider password resets revoke grants, that an email
change is detected, or that remove/re-add cannot revive an unexpired grant.
Before any issuance, the pairing slice must settle membership epochs/revocation,
active-grant caps, Keychain ACLs, private delivery, independent consent and the
no-listener polling design. Never transfer browser sessions or DB DSNs to Codex.
A host with shell access can directly invoke MCP, so host tool approvals alone
are not a per-send consent boundary.

The initial wrapper is locked to `VERCEL_ENV=preview`, explicit opt-in
`MEAL_DRAFT_INGEST_ENABLED=true`, and exact
`MEAL_DRAFT_INGEST_ORIGIN=https://athome-meals-dev.vercel.app`. It uses the existing
validated Neon issuer/endpoint and allowlist. Native calls may omit Origin;
browser calls need that exact origin. No CORS headers or preflight are exposed.
Configuration is not derived from Host or forwarded headers. Production cannot
be enabled through the feature flag alone.

## Storage, quotas and provisioning gates

Migration `004_draft_ingest.sql` is additive and explicitly qualifies `public`.
It must be separately reviewed and applied by the migration owner, never startup.
A new dedicated runtime role and server-only `MEAL_DRAFTS_DATABASE_URL` are needed;
**do not broaden or reuse the existing recipe runtime role**. URL requires
`sslmode=verify-full`; the existing strict parser rejects channel_binding and
other overrides. Pool max 2, database/context budget 8s, statement timeout 8s and lock
wait 3s. The context budget does not interrupt a slow body reader; verify the
platform's body-read/connection deadline before activation. The byte cap alone
does not bound slow upload duration. No migration/admin/recipe/auth-table rights are required.

Minimum table rights are SELECT on grants and UPDATE(last_used_at) only (needed
by PostgreSQL FOR SHARE locking), SELECT/INSERT on submissions, schema USAGE but
no CREATE. No grant-minting or authority-field updates, no DELETE/TRUNCATE,
no submission UPDATE. Effective PUBLIC/inherited rights and data API exposure
must be audited before enabling. Application SQL scopes every operation by
issuer + subject + environment. This is **not database-enforced tenant RLS**;
a compromised server credential can read the submission table. Keep it server-only.

An owner-serialized transaction permits at most 100 retained snapshots, 16 MiB
logical retained bytes and 20 new submissions per trailing hour across all grants.
Exact retries do not consume new capacity. The quota charges canonical request,
full response (including duplicated receipt snapshot, review structure and
server metadata), consumed key, namespace and hashes plus a conservative fixed
per-row/index reserve. Retained grant metadata (including revoked/expired grants)
also counts with a conservative per-grant reserve. This is not physical disk accounting. There are no
independent review/audit/retirement records or purge jobs in this slice; future
records MUST be added to the same accounting before introduction (#72). Quota
never silently deletes user work. All retained snapshots count, not just active
ones. Auth grant provisioning is not exposed, and must add its own caps.

Before exposure, verify platform body-read deadlines and request/connection abuse controls;
a per-owner new-submission cap is not a global unauthenticated request limiter.
The existing Preview env-pull allowlist deliberately does not authorize the new
configuration or secret yet. No existing deployment pipeline guard is weakened.

## Verification and remaining gates

Unit tests cover boundaries, credential type confusion, schema/version failures,
lossless full/incomplete fixtures and response/error secrecy. Disposable loopback
PostgreSQL integration tests cover persistence, retry/concurrency, namespace
isolation, grant revocation/expiry/membership and retained quotas. They do not
prove live Neon settings or Keychain behavior.

Before owner Preview acceptance: independent security review; green exact-head
CI; approved additive migration/runtime role setup; separate pairing and grant
issuance/delivery; explicit MCP send/ledger; protected Preview access; owner-only
read/review UI with unknown values and complete provenance. Unsupported stored
schema versions must disable edit/review/approval controls (#72); ingestion
already rejects unsupported versions. Approval, retention/purge, account deletion,
review invalidation and planner policy remain separate, not implicitly decided
by this ingress slice. Caroline's personal end-to-end Preview acceptance is still
required before any Production release.
