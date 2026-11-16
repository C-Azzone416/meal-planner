# Local-to-hosted draft workflow (Preview candidate)

Status: source candidate only. Every source and runtime feature gate remains off.
This document does not authorize a live migration, grant, role, secret, Preview
enablement, persistent MCP registration, or Production deployment.

## Security boundary

The local client is outbound-only. It uses stdio for MCP and pinned HTTPS for
pairing and sends. It opens no TCP, UDP, HTTP, loopback, callback, tunnel, or
background-sync service. Redirects are rejected rather than followed. Preview
is the only compiled profile; there is no Production fallback.

A send grant is scoped to `draft:propose`. It cannot approve a send, approve a
recipe, read recipes, or obtain database credentials. Calling `draft_send`
creates an immutable pending intent. A signed-in owner must separately inspect
and approve that exact content hash in the browser. Only that browser action can
turn the intent into an unreviewed hosted draft and receipt. This is the
independent consent boundary even when a shell-capable agent launches the MCP
server directly.

Provider sign-out and password reset do not revoke app grants. The application
does not claim otherwise. Explicit grant revocation, absolute expiry and the
membership epoch are the supported revocation boundaries. The application has
no provider removal event: an operator must rotate the epoch whenever membership
is removed or re-added, and the workflow must stay disabled if that cannot be
guaranteed. A rotated epoch keeps every older grant invalid.

## Frozen Preview interfaces

All responses use `Cache-Control: no-store`; browser mutations require a verified
owner bearer token plus same-origin request metadata. Secrets are accepted only
inside bounded HTTPS bodies or the `Authorization` header and are never returned
by MCP tools, written to drafts or ledgers, placed in argv/environment variables,
or logged.

### Pairing

`POST /api/draft-pairings` accepts:

```json
{"code_challenge":"<S256>","code_challenge_method":"S256","client_label":"<bounded label>"}
```

It returns `api_version: local-client-v1`, an opaque pairing ID and poll secret,
an eight-character user code, a challenge fingerprint, a ten-minute lifetime, a
minimum five-second poll interval and the exact pinned `/connect` URL. The URL
contains no code, pairing ID, secret, query or fragment.

`POST /api/draft-pairings/poll` accepts the pairing ID, poll secret and PKCE
verifier in JSON. It returns pending/slow-down until browser approval. Successful
redemption atomically consumes the pairing and returns one opaque
`mp_lc1_pv_...` grant once. Replays never return the grant.

### Send intent

`POST /api/draft-send-intents` uses a send grant and a durable
`Idempotency-Key`. Its JSON body is exactly the shared schema-1 draft submission.
An exact replay returns the same intent; reuse with different canonical content
is a conflict. The grant can create and poll only its own intents.

`POST /api/draft-send-intents/poll` accepts only an intent ID in JSON. Pending,
denied and expired states contain no draft content. An approved response contains
only the receipt ID, hosted draft ID, accepted revision and submission time.
The safe browser path is `/incoming-drafts/{intent_id}`; it contains no grant,
poll secret, operation key or recipe content.

Browser approval binds the owner, intent revision and canonical content hash.
Approval is single-use and retry-safe. It creates an unreviewed hosted draft and
immutable receipt, never a recipe.

### Owner review and save

Owner APIs list and read only the verified `(issuer, subject)` account's hosted
drafts. Review writes bind both draft revision and review revision. Unsupported
content schema versions fail closed as `unsupported_content_schema`; they are not
decoded, reviewed, or saved. Unknown values remain absent/Unknown; present zero
remains zero. Canonical content and provenance remain attached after recipe save.

Saving requires an explicit, complete owner-supplied projection into the existing
manual recipe model. The owner API reuses the existing recipe repository and a
stable idempotency key. It never silently converts strings to numbers, assigns
phases, invents pantry/aisle/timer/storage values, or drops provenance. Stale
revisions conflict, and a double save resolves to the same recipe. The Preview
source now includes an explicit projection editor. Every incompatible or lossy
field starts unresolved; canonical text remains read-only context unless the
owner deliberately copies it, and null, empty, zero and false require distinct
choices. The editor binds the exact draft and review revisions and locks an
uncertain retry to the identical projection. This is source-complete UI behind
the closed workflow gates, not evidence of authenticated Preview operation.

## Local durability

The credential-store interface has no plaintext or environment fallback. An
unwired Darwin+cgo Security.framework adapter exists and non-cgo/non-macOS builds
fail closed. Automated tests inject a synthetic backend and make no real Keychain
calls; packaged-binary signing, ACL and other-process behavior remain final hookup
gates. The receipt ledger contains no recipe content or credentials. Its namespace
includes exact environment, API origin, account binding, local draft ID, local
revision and canonical content hash. The compiled Preview profile pins the web
origin to that same exact origin. An ambiguous retry reuses the same operation
key. A changed revision or account uses a different namespace.

## Tomorrow's live hookup checklist

1. Independently review the final exact commit and all additive SQL.
2. Prove real macOS Keychain Security-framework behavior, signing and ACL prompts;
   run argv/environment/process canaries. Do not add a plaintext fallback.
3. Create least-privilege Preview-only database roles and apply reviewed
   migrations manually to a disposable rehearsal before the protected Preview.
4. Add exact Preview origins and server secrets through the protected owner
   workflow; keep both source and environment feature gates off while verifying.
5. Verify membership-epoch rotation/removal, grant revocation and old deployment
   retirement. Do not claim provider password-reset revocation.
6. Enable only the protected Preview, issue only a synthetic test grant, and run
   Caroline's end-to-end pairing, direct-MCP consent, retry and account-switch
   acceptance with synthetic recipes.
7. Disable again and inspect receipts/logs/quotas before considering a longer
   Preview trial. Production and `main` require a separate decision and review.
