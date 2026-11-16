# Preview local-client pairing (inert implementation)

This slice implements the server half of an outbound-only device-style pairing
protocol. It is **not enabled**. `draftPairingSourceEnabled` is `false`, runtime
also requires `MEAL_DRAFT_PAIRING_ENABLED=true`, and configuration accepts only
the pinned `https://athome-meals-dev.vercel.app` Preview origin. No Production
origin is compiled in. The current protected environment allowlist does not
authorize these variables. No migration, role, grant, credential, or setting was
applied by this change.

The client opens no TCP/UDP/HTTP listener and receives no callback. It makes
ordinary outbound HTTPS requests. Browser approval uses the existing verified
owner bearer token plus an exact same-origin check; cookies alone never approve,
list, or revoke a connection.

## Locked `local-client-v1` interface

`POST /api/draft-pairings` accepts exactly:

```json
{"code_challenge":"<43-char base64url SHA-256>","code_challenge_method":"S256","client_label":"Caroline's Mac"}
```

It returns HTTP 201 with `api_version`, UUID `pairing_id`, a 256-bit `poll_token`,
an eight-character code from `23456789ABCDEFGHJKLMNPQRSTUVWXYZ`,
`verification_uri` equal to the pinned web origin plus `/connect` (no query),
`expires_in: 600`, `interval: 5`, and an eight-lowercase-hex
`challenge_fingerprint`. Only the poll-token hash is stored.

`POST /api/draft-pairings/poll` accepts exactly:

```json
{"pairing_id":"<uuid>","poll_token":"<43-char base64url>","code_verifier":"<PKCE verifier>"}
```

It returns HTTP 202 with `state: pending` or `state: slow_down`. After owner
approval, one transaction creates a grant and consumes the pairing, returning
HTTP 200 once with `state: approved`, `environment: preview`, exact `api_origin`
and `web_origin`, opaque `binding_id`, `grant`, `expires_at`, `owner_label`, and
`scope: draft:propose`. A later poll returns 410 and never recovers the secret.
The grant is a 256-bit `mp_lc1_pv_…` value; only its SHA-256 hash is stored.
`draft:propose` cannot approve a draft, create a manual recipe, or obtain database
credentials. The future send-intent endpoint must authenticate through
`AuthenticateDraftProposal`; direct draft ingestion must not silently treat this
scope as `draft:create`.

`POST /api/draft-pairings/approve` accepts `{user_code,
challenge_fingerprint}` only after `RequireUser` establishes the owner. `GET
/api/draft-grants` lists owner-scoped metadata (never a token/hash); `POST
/api/draft-grants` with `{"grant_id":"<uuid>"}` idempotently revokes the owner's
grant. Cross-owner and absent grant IDs have the same 404 behavior.

## Security and lifecycle contracts

- Pairings expire after ten minutes and polling faster than five seconds slows
  down. Approval binds issuer, subject, verified email, client label, environment,
  and the configured membership epoch. Pairing redemption is single-use.
- At most five current-epoch active grants and 50 retained grant records are
  allowed per owner. Revocation is checked on every future proposal
  authentication. Re-pairing does not revive or reveal an older credential.
- `MEAL_DRAFT_MEMBERSHIP_EPOCH` is a required 16–64 character deployment value.
  Grant authentication compares it on every use, so a changed epoch invalidates
  all earlier grants. This is an **operator contract**, not automatic provider
  integration: every membership removal and re-add must rotate the value before
  rollout. The app cannot detect a remove/re-add cycle that reuses the same epoch.
- Provider password reset does **not** revoke these grants. No such claim is made
  until the provider supplies a verified event/session-generation integration.
- The additive migration is `hosted/migrations/005_draft_pairing.sql`. Application
  startup never applies it. Dedicated least-privilege roles and a retention job
  remain tomorrow's reviewed hookup. The unauthenticated create route additionally
  requires deployment-level request throttling/body deadlines before enablement;
  the database caps are not a global denial-of-service control.
- Keychain ACL/signing behavior and private credential storage remain unverified
  on the actual Mac. No plaintext fallback is authorized.
