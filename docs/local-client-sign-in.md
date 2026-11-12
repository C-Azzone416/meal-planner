# Local client sign-in (design)

Status: **proposed design LC1, 2026-11-11, issue #73.** Design only. It is not
an implemented API and not permission to resume #55. It creates no accounts,
credentials, tokens, provider settings, persistent grants or hosted submissions.
Every example value is synthetic. **Independent security review has not
happened**; section 10 lists exactly what the reviewer must decide. Based on
`develop` `ef7e561`, the #77 hosted draft contract (`docs/hosted-draft-contract.md`,
HD1, branch `codex/72-hosted-draft-contract`), #78 membership
(`hosted/auth_membership.go`, branch `codex/53-multi-member`) and the local
MCP/CLI on `codex/23-mcp-remediation`.

#55 stays paused and Production stays closed until Caroline personally runs and
confirms the Preview test (#25). Nothing here lifts that.

## 0. Summary and recommendation

- **Neon Auth does not document a device authorization grant** (section 2). It
  does document an OAuth 2.1-style provider (authorization code + PKCE, public
  clients, loopback redirects) with **fixed scopes, fixed token lifetimes**, JWT
  access tokens that cannot be revoked, and a production checklist item that
  *disables* localhost. It also has no documented session-revocation or
  sign-out API, and no session webhooks.
- Those limits mean a provider-issued token can neither express "send-only"
  nor be revoked faster than its lifetime. Send-only (#77) must be enforced by
  the application either way.
- **Recommendation (LC1):** an **app-mediated, device-style pairing** using
  existing Neon sign-in only for the *owner's approval step*, followed by an
  **app-issued, opaque, send-only, environment-bound, revocable credential**
  stored in the OS keychain. No Neon setting changes. No credential ever enters
  the model's context. It copies the security properties of RFC 8628 and
  RFC 7636 but is an application protocol, not a provider feature. It needs new
  persistent storage and routes, so it **requires separate approval**
  (section 9).
- Documented-supported alternative (Flow A): Neon's OAuth Provider with
  loopback + PKCE. It needs a provider-side OAuth client, localhost access
  enabled on the branch, and app-hosted login and consent pages, and it cannot be
  used in Production while the production checklist's "disable localhost"
  stays in force. Compared in section 3.
- Access removal: membership removal and grant revocation take effect on the
  **next request** (zero cache), as in #78. Provider session revocation or
  password reset does **not** revoke grants (section 7). Owners need a
  "revoke all connected tools" action after any compromise.

## 1. Threat model

### 1.1 Assets and trust

| Asset | Where | Should be visible to |
| --- | --- | --- |
| Send credential (`mp_lc1_…`) | OS keychain; in memory of the CLI/MCP *process*; `Authorization` header | Only the local client process. **Never** the model, tool arguments/results, draft files, logs, env vars, argv, crash dumps. |
| Pairing `code_verifier` | memory of the CLI process for ≤10 min | CLI process only. Never written to disk. |
| Pairing `user_code` | terminal display; typed into the web app by the owner | The owner. Low value alone (useless without the verifier and the owner's session). It *may* appear on screen. |
| Owner's Neon session/JWT | browser | Browser only. Never sent to the local client. |
| Drafts (unreviewed) | local store; hosted Preview/Production | Owner; model sees local drafts by design (host sends them to its provider; see `docs/local-workflow.md` privacy boundary). |

Trust: the **model/agent and its tool results are untrusted** in both
directions (prompt injection can ask it to reveal or misuse anything it sees).
The local OS user account is *partially* trusted (section 1.2 T7). The hosted
app is trusted for authorization; the browser session is the only approval
authority.

### 1.2 Threats and required defenses

| ID | Threat | Defense in LC1 | Residual risk |
| --- | --- | --- | --- |
| T1 | **Model/agent sees credentials or tokens** (prompt injection, verbose tool result, host transcript, draft text) | The credential never crosses MCP: the MCP server process reads it from the keychain and sets the header internally. No MCP/CLI tool accepts or returns a token, token path, or environment variable for one. `device`/`user_code` flow runs only in the CLI on a TTY, never as an MCP tool. Pairing never delivers the credential through the terminal or browser: it is returned over HTTPS to the polling CLI process (section 5.2). Tool results use an allowlisted schema (section 8.3). Redacting HTTP client; no header logging; redirects not followed. | A host that logs the *process environment* or memory dumps: none used. A model with shell access can run `mealplanner status`, which prints no secret. A model with a shell running as the same OS user falls under T7. |
| T2 | **Pairing replay** (re-using a user code, polling secret or the issued credential) | Pairing is single-use: `pending → approved → consumed` (or `denied`/`expired`). The credential is created and returned in one transaction that sets `consumed`; a second redeem returns the same `410 pairing_consumed` with no credential. `code_verifier` is checked against the stored S256 challenge (RFC 7636 §4.6) and only the redeeming CLI holds it. 10-minute expiry. `user_code` is deleted at consumption. Polling ≥5 s, `slow_down` (RFC 8628 §3.5). | A lost redeem response orphans a grant; the owner revokes it from the app list (section 7); the CLI re-pairs. Accepted: simplicity over idempotent credential retrieval, which would weaken replay prevention. |
| T3 | **Callback/state substitution (login-CSRF)**: attacker induces the owner to authorize the *attacker's* client, or the CLI to accept the *attacker's* result | There is **no callback** in the recommended flow, so no redirect URI, authorization code or `state` is exposed to interception. The attacker-pairing variant: the owner must *type the code shown in their own terminal*, then see client label, creation age and short fingerprint of the challenge before approving. The CLI only accepts a credential from the exact pairing it created (pairing ID + verifier). Any loopback variant (Flow A, section 3) must use `state` ≥128 bits, PKCE S256, `127.0.0.1`/`[::1]` only, exact path, one request, listener closed after first request, and reject mismatched `state`/`iss` (RFC 8252 §§7.3, 8.3, 8.9, 8.10; RFC 9207 §2.4). | Social engineering: the owner approves a code an attacker gave them. The approval screen text and fingerprint are the mitigation (RFC 8628 §5.4). Impact is bounded to send-only plus drafts reads (D6). |
| T4 | **Cross-environment grant reuse** (Preview credential used on Production or vice versa) | Credentials are issued, hashed and verified in the *environment's own database*; there is no shared table or key. Server also compares `grant.environment_id` with its configured environment ID and rejects mismatch. Token prefix carries the environment (`mp_lc1_pv_…`); the client compares prefix, profile, `whoami.environment` and receipt link origin before and after sending (section 4.2). No Production profile exists in the Preview build/config. | A client bug that ignores the preflight: tested by the matrix (LC-E1..E5). |
| T5 | **Cross-account grant reuse / account switching** | A grant is bound to `(environment, owner issuer, owner subject)` set at approval from the verified browser session, never from client input. Every `draft-v1` query already includes both owner fields (#77). Keychain items are keyed `env + hash(issuer,subject)`. Switching accounts is an explicit `mealplanner use` + new pairing; there is no implicit "most recent". Local send ledger and idempotency keys are keyed by environment *and* account (#77 §9). | Same person with two accounts mis-selects: confirmation text always shows `Preview · origin` and `Account · label` (#77 §8.1). |
| T6 | **Token theft from disk** (backups, Time Machine, dotfile sync, file fallback, shell history) | Credential lives only in the OS keychain. **No file fallback by default** (D4). No env-var or flag token inputs. Config file holds non-secret metadata only (0600). Credential is opaque, 256 bits, stored server-side only as SHA-256. Absolute expiry and server revocation. | Keychain item ACL strength depends on binary signing: **UNVERIFIED** (section 2.5). |
| T7 | **Malicious local process** (same user; other user; port squatter; reads argv/env; ptrace) | Other-user: keychain and 0600 files. Port squatting: the recommended flow opens no listener. Argv/env: the credential is never in either; keychain library must not pass the secret on a command line (**implementation acceptance test**, section 6). Same-user code execution: **cannot be fully prevented**. LC1 limits the blast radius: send-only (cannot approve, edit, read recipes or other routes), expiry, instant server revoke, last-used metadata and visible grant list. | A same-user attacker can read what the user can read. Sender-constrained tokens (e.g. DPoP, RFC 9449) would shrink this and are **not** designed here (not researched). |
| T8 | **Capability confusion** (send credential used for owner operations; Neon JWT used as a send credential; a provider-issued token treated as owner) | The credential is a different, *non-JWT* format. `hosted.RequireUser` (`hosted/bearer_auth.go`) requires a three-part JWT and rejects it structurally; owner routes need no change. A new `RequireLocalClient` accepts only the prefix and applies the capability matrix (section 8.2) **before** object lookup, matching #77's 403 rule. | A future change that makes a shared auth helper accept both types. Test: send credential on every owner route. |
| T9 | **Removed or ineligible member keeps access** | Each request re-runs the #78 membership policy against the email recorded at approval, and the grant's expiry/revocation. No cache. Bound in section 7. | Email change at the provider: the grant email snapshot no longer matches the account's email; it is **denied** (fails closed) until re-pair. |
| T10 | **Remote phishing / pairing spam** (unauthenticated endpoints create rows, brute-force codes) | Unauthenticated create is rate-limited per IP/global, with a cap on pending rows. Codes: 8 characters from a 20-letter alphabet, ≈2.6×10¹⁰ values, ≤5 attempts per pairing (RFC 8628 §5.1 example). Approval requires the owner's signed-in session *and* the typed code, so brute force alone cannot approve. | Distributed abuse of the unauthenticated create route; an operational limit decision (D9). |
| T11 | **Credential in logs, telemetry, error text, crash reports, draft files** | Redacting transport; errors never include headers or bodies; hosted error messages already exclude secrets (#77 §3.1); canary tests with a synthetic token search every output (section 6). Drafts and the local store never contain tokens (the send path reads drafts but writes only a non-secret ledger). | Third-party host telemetry of MCP *arguments/results*: none contain secrets by construction. |
| T12 | **Wrong destination** (profile edited, typosquat origin, redirect) | Profile pins exact HTTPS origins; the credential lookup key includes the origin, so an altered origin finds no credential. No redirects followed. Receipt/draft links must resolve to the profile web origin. | Same-user edit of config to a new origin forces a fresh pairing against it; the owner would see the unexpected origin at the pairing prompt. |

## 2. Provider capabilities (official sources)

Provider: **Neon Auth (Managed Better Auth)**, per `docs/runbooks/authentication.md`.
Access date for every row: **2026-11-11**. "Unsupported/undocumented" means the
page does not list it. Where Neon says plugins are not user-configurable, a
Better Auth feature page alone does **not** establish that Neon supports it.

### 2.1 What Neon documents

| Capability | Status | Source (page last-updated) |
| --- | --- | --- |
| Supported plugin list: Admin, Email OTP, JWT, Magic Link, OAuth Provider, Organization (partial), Open API, Phone Number; "You don't install or configure Better Auth plugins directly." | Documented | <https://neon.com/docs/auth/guides/plugins> (2026-10-30) |
| **Device authorization plugin / RFC 8628 grant** | **Not in the supported list; not documented anywhere read. Treated as unavailable.** | same; <https://neon.com/docs/auth/roadmap> (2026-09-17) |
| Bearer plugin, API key plugin | Not documented for Neon (JWT page only mentions raw tokens for "CLI tools" and microservices) | <https://neon.com/docs/auth/guides/plugins/jwt> (2026-11-05) |
| JWT access token: EdDSA, **15 minutes**, JWKS at `<auth URL>/.well-known/jwks.json`, `iss`/`aud` = origin of the auth URL, `sub` = user ID; **custom claims not supported** | Documented | JWT page above |
| Session/JWT revocation API, sign-out API, session lifetime | **Not documented** on pages read. Manage API lists no session/token endpoints. Webhooks have no session events. | <https://neon.com/docs/auth/guides/manage-auth-api> (2026-10-09); <https://neon.com/docs/auth/guides/webhooks> (2026-09-16) |
| **OAuth Provider** (OAuth 2.1 authorization server): `authorization_code` + `refresh_token`; no `client_credentials`; public clients with PKCE for "single-page, mobile, desktop, and CLI apps"; MCP support | Documented | <https://neon.com/docs/auth/guides/plugins/oauth-provider> (2026-11-06) |
| OAuth Provider **scopes are fixed**: `openid`, `profile`, `email`, `offline_access`. "Scopes, token lifetimes, and other provider options aren't configurable." | Documented | OAuth Provider page |
| OAuth Provider access token 1 hour; refresh token 30 days, rotated on every use, only with `offline_access`; token carries `client_id` and `scope`; developer must host login and consent pages ("doesn't provide default pages") | Documented | OAuth Provider page |
| Redirect URIs for managed clients: `https`, or `http` on a loopback host **when localhost access is enabled for the branch**; custom schemes such as `myapp://` fail | Documented | OAuth Provider page |
| Trusted domains; localhost ports pre-approved; "Allow Localhost" setting exists in the management API (`allow_localhost`) | Documented | <https://neon.com/docs/auth/guides/configure-domains> (2026-11-05); manage-auth-api page |
| Production checklist: **disable "Allow Localhost"** in production ("enabled by default for development but should be disabled in production") | Documented | <https://neon.com/docs/auth/production-checklist> (2026-10-09) |
| Branch isolation: each branch has its own auth config and issuer; all branches share signing keys, only `iss` differs | Documented | OAuth Provider page |
| Applications minting their own tokens with Neon's keys | **Not documented** (only public JWKS is documented) | JWT, OAuth Provider pages |

### 2.2 What Better Auth documents (upstream, not necessarily enabled on Neon)

| Feature | Upstream documentation | Source |
| --- | --- | --- |
| Device authorization plugin | RFC 8628 flow; server plugin config and a DB table; device/user code default 30 min, minimum polling interval default 5 s; docs call the CLI login example "a demo feature" | <https://www.better-auth.com/docs/plugins/device-authorization> |
| Bearer plugin | Session token as bearer; "Use this cautiously"; lifetime, scope, revocation not documented | <https://www.better-auth.com/docs/plugins/bearer> |
| JWT plugin | 15 min default, configurable upstream; "not meant as a replacement for the session"; no scoping/revocation documented | <https://www.better-auth.com/docs/plugins/jwt> |
| OAuth Provider | `/oauth2/revoke` (RFC 7009); **JWT access tokens cannot be revoked** (opaque ones can); refresh rotation with replay detection; resource/audience binding (RFC 8707); `scopeExpirations`/`allowedScopes` configurable upstream | <https://www.better-auth.com/docs/plugins/oauth-provider>, `…/oauth-provider/tokens`, `…/oauth-provider/reference` |
| API key plugin | Server-side permissions, expiry, disable/delete | <https://www.better-auth.com/docs/plugins/api-key> |

Better Auth upstream options (configurable scopes, token lifetimes, revoke) do
**not** transfer to Neon: Neon states scopes and lifetimes are not configurable.
Whether Neon exposes `/oauth2/revoke`, resource registration or opaque access
tokens is **UNVERIFIED**.

### 2.3 UNVERIFIED provider behaviours (do not rely on them)

1. Whether Neon exposes a revocation endpoint for OAuth Provider tokens and
   whether JWT access tokens there are revocable. (Upstream says no for JWTs.)
2. Whether `client_id` appears in Neon-issued access-token claims in practice
   (documented on the OAuth Provider page; not observed).
3. Whether Neon OAuth Provider supports registering a resource/audience for our
   API (RFC 8707) or `aud` is fixed to something the existing verifier
   (`hosted/bearer_auth.go`, `aud` must contain the issuer origin) would accept as
   an *owner* token.
4. Whether any device-code behaviour is available but undocumented.
5. Token field naming: the JWT page shows `session.token`, the
   authentication-flow page `session.access_token` (the pages disagree).
6. Whether Preview's branch already has "Allow Localhost" enabled. Provider
   state was **not inspected** (no console access, by instruction).
7. Behaviour when a user's email changes at the provider (affects T9).

### 2.4 Standards used (accessed 2026-11-11)

| Standard | URL | Used for |
| --- | --- | --- |
| RFC 8252 (BCP 212) native apps | <https://www.rfc-editor.org/rfc/rfc8252> | External user agent (§5), PKCE (§6), loopback (§7.3, §8.3), exact redirect matching (§8.4), `state` (§8.9), mix-up (§8.10), no embedded webviews (§8.12) |
| RFC 7636 PKCE | <https://www.rfc-editor.org/rfc/rfc7636> | S256 only, verifier 43–128 chars, ≥256 bits entropy (§7.1), no downgrade (§7.2) |
| RFC 8628 device grant | <https://www.rfc-editor.org/rfc/rfc8628> | user-code brute force (§5.1), device code entropy (§5.2), remote phishing (§5.4), polling/`slow_down` (§3.5) |
| RFC 9700 (BCP 240) OAuth security BCP | <https://www.rfc-editor.org/rfc/rfc9700> | Public clients MUST use PKCE (§2.1.1); audience-restricted tokens (§2.3); refresh rotation for public clients (§2.2.2); exact redirect matching (§2.1) |
| OAuth 2.1 draft | <https://datatracker.ietf.org/doc/draft-ietf-oauth-v2-1/> (draft-17, last updated 2026-10-30) | Background only; **only the introduction was read**; no normative claim here relies on it |
| RFC 9207 issuer identification | <https://www.rfc-editor.org/rfc/rfc9207> | `iss` check if a redirect flow is used |
| RFC 8707 resource indicators | <https://www.rfc-editor.org/rfc/rfc8707> | audience restriction in Flow A |
| RFC 7009 token revocation | <https://www.rfc-editor.org/rfc/rfc7009> | Revocation semantics |

Reading gaps (disclosed in the PR): the last ~6 KB of RFC 9700 §4.x and some
RFC 9700 §4 requirements were taken from summaries rather than verbatim; the
OAuth 2.1 draft body was not read; RFC 8693 was only skimmed and is not relied on.

### 2.5 Not researched

OS keychain behaviour (macOS Keychain access-control/ACL, code-signing
requirements, how popular Go keychain libraries pass secrets) was **not checked
against official Apple documentation**. Everything about keychain strength is
**UNVERIFIED** and is an implementation acceptance gate (section 6, LC-K1..K3).

## 3. Candidate flows

| | **A. Neon OAuth Provider, loopback + PKCE** | **B. Pasted one-time code (out-of-band auth code + PKCE)** | **C. App-mediated device-style pairing (recommended)** |
| --- | --- | --- | --- |
| Provider support | Documented | Not a provider feature; app-implemented | Not a provider feature; app-implemented. Neon's *device* grant is undocumented. |
| Provider changes | OAuth client registration; "Allow Localhost" on; login + consent pages | None | None |
| New app persistence | Optional (grant ledger for fast revoke) | Pairing + grant tables | Pairing + grant tables |
| Send-only scope | Scopes fixed; must be inferred from `client_id` + a server allowlist (**UNVERIFIED**) | Native | Native |
| Credential visibility | Access/refresh tokens on loopback/back-channel; code in browser URL | Code appears in browser, clipboard, terminal scrollback, possibly model if the agent shares the terminal | **Only a user code is visible; the credential never leaves the HTTPS response to the CLI** |
| Revocation | JWT unrevocable; ≤1 h access, 30 d refresh; revoke endpoint **UNVERIFIED** | Immediate (server lookup) | Immediate (server lookup) |
| Production | Blocked by the "disable localhost" checklist item | Possible | Possible |
| Local attack surface | Loopback listener (port squat, local process race) | None (no listener) | None (no listener) |
| Standard coverage | RFC 8252, 7636, 9700 directly | RFC 7636 (+ manual transfer) | RFC 8628 properties + 7636 S256 for the polling secret |
| Weakness | Provider configuration dependence; tokens cannot be narrowed or revoked on our terms | Secret passes through human channels | Custom protocol in our code: needs focused security review |

**Recommendation:** C for Preview and, after owner decision D8, for Production.
It is the only candidate that provides send-only scope, immediate revocation,
no secret on screen or in the browser, no listener and no provider change.
Flow A remains the fallback if the owner prefers delegating credential
issuance to Neon. Its precise blockers are: (1) provider OAuth client creation,
(2) localhost enablement conflicting with the Production checklist, (3) fixed
scopes, (4) 1 h unrevocable access tokens, and (5) UNVERIFIED items 1–3 above.
Flow B is rejected because the long-lived secret is relayed through channels the
agent may observe.

## 4. Preview-only profile (the owner's #55/#25 test)

### 4.1 Profile record

The CLI and MCP server load exactly one *profile* per environment from a
non-secret file (`profiles.json`, mode 0600, user-owned). The first and only
shipped profile is `preview`. Production has **no profile**, and
`connect --env production` returns `profile_not_enabled` until a later,
separately approved change (D8).

| Field | Value / rule |
| --- | --- |
| `name` | `preview` (exact; no default; `--env` is required on `connect`, `use`, `status`, `disconnect`, `send`) |
| `web_origin`, `api_origin` | Exact HTTPS origins entered by the owner and shown on every confirmation; compared by exact string; no path, query, userinfo, port rewriting, wildcard or redirects |
| `environment` | `preview`; must equal the server's `whoami.environment` |
| `environment_id` | Server-returned opaque ID recorded on first connect and compared thereafter |
| `scope` | `draft:send` only |
| `account` | Selected explicitly; stored as `{label, hash16(issuer,subject)}`; never inferred |
| `credential_ref` | Keychain service `mealplanner.local-client.v1`, account `preview/<hash16>`; the value is the credential |

### 4.2 Required behaviours

1. **Explicit account and environment selection.** `mealplanner connect --env preview`
   prints `Preview · <web origin>` and requires typing `preview` to continue. The
   approval page in the web app prints `Preview · <web origin>` and
   `Account · <display label>`; the owner confirms there. Account switching
   uses `mealplanner use --env preview --account <label>`.
2. **Grant bound to Preview.** Section 5 stores `environment_id` in the grant,
   and the server rejects a mismatch. The token prefix is `mp_lc1_pv_`.
3. **Receipt and app link.** After a send, the tool result contains only the
   #77 receipt fields: `receipt.id`, `draft.id`, `revision`, absolute links
   built from the *server-returned relative* `receipt.links.*` resolved against
   `web_origin`, and the label `Saved to Preview as an unreviewed draft` (P1
   pending). The client never constructs a link from an ID.
4. **Fail-closed environment mismatch.** Before the draft body leaves the
   machine, the client performs `GET /api/local-client-v1/whoami` and aborts
   with `environment_mismatch` (nonzero exit / MCP error, **no retry, no
   fallback, no body sent**) if any of these differ: profile environment,
   token prefix, `whoami.environment`, `whoami.environment_id`, `whoami.web_origin`,
   or selected account hash. After the response, a link whose origin differs from
   `web_origin` is rejected and reported as a server contract violation (the
   draft was saved; the client says so rather than hiding it).
5. **No Production credential, no silent fallback.** No Production keychain
   item exists or is read. A missing or expired Preview credential produces
   `not_connected` / `reconnect_required`. It never tries another environment, another
   account, another origin, an environment variable or a file.
6. **Local-only creation stays local.** `draft_create/update/validate` never
   contact the network (unchanged, `docs/local-workflow.md`); only the explicit
   send path loads the credential.

### 4.3 Owner Preview walkthrough (maps to #77 §8.2 Preview-1/8/9)

Run by Caroline herself; this is not run by this issue.

1. Build and register the MCP server as in `docs/local-workflow.md`.
2. `mealplanner connect --env preview` → terminal shows the code
   (`BCDF-GHJK` style) and verification URL. Open the Preview web app,
   signed in, enter the code, check environment, account, client label, age and
   fingerprint, approve. The CLI prints `Connected: Preview · <origin> ·
   Account · <label> · expires <time>` (no secret).
3. `mealplanner status --env preview` → connected, scope, expiry.
4. In the agent host: create the synthetic incomplete draft, then `draft_send`.
   Approve the host tool prompt. Capture the receipt and open the app link.
5. Negative checks (synthetic): point a test profile at a stub that reports
   `environment: production`; expect `environment_mismatch` and no body sent.
6. `mealplanner disconnect --env preview`; send again → `not_connected`; the
   grant shows revoked in the app.

## 5. Protocol (LC1)

### 5.1 Hosted routes (proposed, not registered here)

All under `/api/local-client-v1`, `Cache-Control: no-store`, `api_version:
"local-client-v1"` in bodies, errors in the #77 envelope. Names are proposals.

| Route | Caller / auth | Purpose |
| --- | --- | --- |
| `POST /pairings` | none; rate limited | Create a pairing for `environment` with a `code_challenge` (S256 only) |
| `POST /pairings/{id}/redeem` | `code_verifier` in body | Poll; returns `authorization_pending`, `slow_down`, denied/expired, or the credential **once** |
| `POST /approvals/lookup` | app-owner Neon JWT (`RequireUser`) | Resolve a typed `user_code` to a summary (client label, age, fingerprint, environment) |
| `POST /approvals/{ref}` | app-owner JWT | `approve` or `deny` |
| `GET /whoami` | send credential | Environment, web origin, scope, expiry, account label |
| `DELETE /grants/self` | send credential | Disconnect (revoke this grant) |
| `GET /grants`, `DELETE /grants/{id}`, `DELETE /grants` | app-owner JWT | List, revoke one, revoke all for this account |

### 5.2 Sequence

1. CLI (`connect`) generates `code_verifier` (32 random bytes, base64url, 43
   chars; RFC 7636 §4.1, §7.1), `code_challenge = S256(verifier)`. It never
   sends the verifier, never stores it on disk.
2. `POST /pairings` `{environment:"preview", code_challenge, code_challenge_method:"S256", client:{name,version}, label}`.
   `label` is a user-chosen ≤64-char printable ASCII host name, shown *untrusted*.
   Server stores `pairing_id` (128-bit), `user_code_hash` (see D9 note), challenge,
   `environment_id`, `expires_at = now + 10 min`, `state = pending`, and returns
   `{pairing_id, user_code, verification_url, expires_in:600, interval:5}`.
   `user_code`: 8 characters from `BCDFGHJKLMNPQRSTVWXZ`.
3. CLI prints the user code, `Preview · <origin>`, and the web verification URL.
   It opens nothing automatically.
4. Owner (signed in to the Preview web app) enters the code. Server finds the
   pairing, increments attempts (≤5 then invalid), and the page shows: environment,
   account label, client label (marked "name chosen by the requesting program"),
   age ("created 40 s ago"), a 6-character fingerprint of the challenge (also
   printed by the CLI), and the text "Allows saving **unreviewed drafts only**.
   It cannot approve recipes, edit drafts or read your recipes." Approve or Deny.
5. On approve: the server records the owner `(issuer, subject)` and the
   normalized email from the **verified bearer** (never from request fields),
   plus the membership result, `state = approved`.
6. CLI polls `POST /pairings/{id}/redeem` `{code_verifier}` every ≥5 s (RFC 8628
   §3.5). Server checks `S256(verifier) == challenge`, state, expiry, then in one
   transaction creates the grant (`credential = "mp_lc1_pv_" + b64url(32 random
   bytes)`, stores only SHA-256), sets `consumed`, deletes the user code, and
   returns the credential in the HTTPS response body.
7. CLI stores the credential in the keychain (item key from section 4.1), writes
   non-secret profile metadata, and calls `whoami`. It zeroes the verifier.
   If the keychain write fails, the CLI calls `DELETE /grants/self` and exits;
   it never falls back to a file.

### 5.3 Credential properties

| Property | Value |
| --- | --- |
| Format | `mp_lc1_<env>_<43 base64url chars>` (256 bits); opaque; not a JWT, so `RequireUser` rejects it |
| Server storage | SHA-256 hash only, unique index; no plaintext, no reversible storage |
| Bound to | `environment_id`, `owner_issuer`, `owner_subject`, `email_at_approval`, `scope = draft:send`, `grant_id` |
| Lifetime | Absolute `expires_at`; recommended 7 days (D3). No sliding extension, no refresh token in LC1 |
| Per-request checks | Hash lookup; `environment_id`; not revoked; not expired; #78 membership against `email_at_approval`; then capability (section 8.2) |

## 6. Test matrix (synthetic and local only)

IDs are for the later implementation. All run against stubs, `httptest` servers
and in-memory fakes. None needs a provider, account or credential.

| ID | Case | Expected |
| --- | --- | --- |
| LC-P1 | Redeem before approval | `authorization_pending` with interval; never a credential |
| LC-P2 | Redeem after approval, correct verifier | One credential; pairing `consumed` |
| LC-P3 | Redeem again (replay) | `410 pairing_consumed`; no credential; no second grant |
| LC-P4 | Redeem with a wrong or plain-method verifier | `invalid_grant`; counts toward attempts; no state change |
| LC-P5 | Poll faster than interval | `slow_down`, interval +5 s |
| LC-P6 | Redeem after 10 min | `expired`; user code unusable |
| LC-P7 | Sixth wrong user code | Pairing invalidated |
| LC-P8 | Approve with a send credential or no session | 403/401; no approval |
| LC-P9 | Concurrent approve/deny/redeem | Exactly one terminal state; at most one grant |
| LC-S1 | Attacker pairing code typed by owner but CLI holds a different pairing | CLI never receives a credential for the other pairing |
| LC-S2 | `code_challenge_method=plain`, short or missing challenge | 400; no pairing |
| LC-E1 | Preview credential presented to a server configured as Production | 401 (no matching hash / environment mismatch) |
| LC-E2 | Client profile `preview`, `whoami.environment=production` | `environment_mismatch`; zero body bytes sent |
| LC-E3 | Token prefix `pv`, profile `production` or reverse | Abort before network |
| LC-E4 | Link in response with a different origin | Rejected; reported; not opened |
| LC-E5 | Missing Preview credential with a Production credential present | `not_connected`; Production item never read |
| LC-A1 | Account A grant, request for account B's draft, receipt, review | Same 404 as absent; no B metadata |
| LC-A2 | Switch account without a second pairing | Refused; no credential reuse; local ledger remains per account |
| LC-A3 | Same subject, different issuer | Treated as a different owner (#77) |
| LC-C1 | Send credential on PUT/review POST/approve/discard/archive, own/foreign/absent IDs | 403 `operation_not_allowed` before lookup for all |
| LC-C2 | Send credential on `/api/recipes*` and `/api/auth/authorize` | 401 (not a JWT) |
| LC-C3 | Neon owner JWT on `/whoami`, `/grants/self` | 401; credential type required |
| LC-C4 | Send credential on `POST /submissions` and the three GETs | Allowed per section 8.2 |
| LC-M1 | Remove member via #78 redeploy; reuse an unexpired credential | 401 on the next request; other member's grant works |
| LC-M2 | Account email changed from the email snapshot | 401 until re-pair (fail closed) |
| LC-R1 | Revoke grant in the app; next request | 401 immediately; no cache |
| LC-R2 | `disconnect` with network failure | Local item deleted; message says the server grant remains until expiry or revocation |
| LC-R3 | Revoke all | Every grant for that account 401; other accounts unaffected |
| LC-R4 | Expiry at `expires_at` (fake clock) | 401; client reports `reconnect_required`; no auto re-pair |
| LC-R5 | Provider session revoked or password reset (simulated) | Grant **still valid** (documented, section 7) |
| LC-T1 | Canary: synthetic token string; run connect, status, send, errors, mismatch, panic paths | The canary is absent from stdout, stderr, MCP results, logs, local store, draft files, config, env and argv |
| LC-T2 | MCP `tools/list` and schemas | No tool accepts or returns token/URL/environment/credential fields |
| LC-T3 | Prompt-injection draft text asking for the token | Tool results contain no secret; there is no tool that could return it |
| LC-T4 | Redirect (301/302/307) from the API origin | Not followed; `Authorization` not forwarded |
| LC-K1 | Keychain write, read and delete through the real macOS Keychain on a developer machine | Secret never in argv or process environment (checked with `ps`/`lsof`-style test); **UNVERIFIED until run** |
| LC-K2 | Keychain unavailable | `credential_store_unavailable`; no file, no env fallback |
| LC-K3 | Item read by a different binary | ACL behaviour recorded; **UNVERIFIED** |
| LC-D1 | `connect` without a TTY (as an MCP/agent subprocess) | Refuses; no pairing |

## 7. Revocation, expiry and the access-removal bound

| Event | Effect | Delay bound |
| --- | --- | --- |
| Owner revokes a grant or all grants in the app | Credential rejected | **Next request after the transaction commits** (no cache) |
| `mealplanner disconnect` | Server revoke then keychain delete; if offline, keychain delete only | Server grant ends at revoke, or at expiry if the revoke failed (the CLI says which) |
| Credential expiry | 401 | Exactly `expires_at` (server clock); decision D3 |
| Membership removal (#78) | Credential rejected through the shared policy | **Next protected request evaluated with the updated deployment configuration** (zero application cache), starting when the removal redeployment is fully serving and older deployments or aliases are retired or restricted, exactly the #78 bound. No fixed time from saving a setting. |
| Neon session revocation, sign-out, password reset | **Grants are unaffected.** | Not revoked. Neon documents no session-event webhook and no session revoke API, so automatic cascade is impossible. Owner must "revoke all" after a suspected compromise (D7). |
| Provider account deletion | Membership removal or a missing account must be handled with the account lifecycle (#45) | Grants fail closed once the email is no longer a member; a deleted user is not detectable without a lookup, so deletion must be paired with "revoke all" |
| In-flight request | May finish | Already-accepted requests complete, as in #78 |
| Flow A, if chosen instead | Neon access token 1 h (non-configurable), refresh 30 d | Up to 1 h after provider-side change; revoke behaviour **UNVERIFIED** |

**Maximum removal delay:** zero cache, i.e. the next request, for grant
revocation, expiry and membership removal; the deployment-rollout caveat from
#78 applies to membership. There is **no** automatic removal on password reset.

**Account switching:** a new account needs a new pairing; the old grant
remains valid until disconnected, revoked or expired. `mealplanner use`
changes only local selection. The send confirmation always names the account.

## 8. Interfaces

### 8.1 Local components (Go, proposed)

```go
// package localclient (no network in tests; all fakes)
type Profile struct {
    Name, WebOrigin, APIOrigin, EnvironmentID string // Name == "preview" in LC1
    AccountLabel string; AccountHash string
}
type CredentialStore interface { // keychain implementation + in-memory fake
    Get(ctx context.Context, p Profile) (Credential, error)   // Credential has no String()/MarshalJSON/format verb
    Put(ctx context.Context, p Profile, c Credential) error
    Delete(ctx context.Context, p Profile) error
}
type Pairer interface{ Pair(ctx context.Context, p Profile, show func(UserPrompt)) (Grant, error) }
type Sender interface{ Send(ctx context.Context, p Profile, in SendInput) (Receipt, error) }
```

`Credential` is an opaque type with redaction (`String()` and `MarshalJSON`
return `[redacted]`), consumed only by the HTTP transport. There is no `Token()`
accessor outside the transport package.

CLI (TTY required for `connect`): `mealplanner connect|status|use|accounts|disconnect --env preview`.
`status` prints profile, account label, scope, expiry and connected state; never
a secret. MCP adds exactly:

| Tool | Arguments | Result | Notes |
| --- | --- | --- | --- |
| `auth_status` | `{}` (no environment argument; uses the selected profile) | `{environment, web_origin, connected, account_label, scope, expires_at}` | read-only |
| `draft_send` | `{draft_id, expected_revision}` | #77 receipt subset and absolute links | Annotated destructive/open-world so hosts prompt. Does not accept a destination, environment, origin, header, credential, or idempotency key. The key is generated and retained in the local ledger (#77 §9). |

There is no MCP tool to connect, disconnect, or select accounts.

### 8.2 Capability matrix for the send credential (matches #77 §1)

| Operation | Send credential | Notes |
| --- | --- | --- |
| `POST /api/draft-v1/submissions` | Allow | |
| `GET draft`, `GET receipt`, `GET review` | Allow, owner-scoped; other owner or absent: same 404 | **D6**: #77 says "own drafts". Recommend narrowing to drafts created through *this grant* |
| `GET /drafts` list | Deny `403 operation_not_allowed` | Not in #77's send-credential read list; **confirm in #77** |
| `PUT`, review `POST`, approve, discard, archive | `403 operation_not_allowed` before lookup, own/foreign/absent alike | |
| `/api/recipes*`, `/api/auth/*` | 401 | not a JWT |
| `/api/local-client-v1/grants*` (owner) | 401 | app-owner JWT only |
| App-owner JWT on `submissions` | Deferred to #77 (recommend deny until needed) | |

Order for every credential request: parse the format → hash lookup → environment
→ expiry/revocation → membership → capability (403) → object lookup (404).
Failures before capability return a single generic `401 unauthorized` (the
client treats it as `reconnect_required`).

### 8.3 No credential in model-visible output

- MCP result schemas are allowlists (receipt IDs, revisions, labels, absolute
  links, status booleans, error codes and fixed messages). Hosted `message`
  fields are never forwarded; only the code is mapped to fixed local text.
- No MCP argument or result, `draft_*` file, local ledger, `profiles.json`,
  stderr line or log may contain a credential, `Authorization` header,
  verifier or user code. LC-T1/LC-T2 enforce this.
- No environment variable, flag, or config key can supply a credential.
- The send path never echoes request bodies in errors.

### 8.4 Keychain and file fallback policy

- macOS Keychain (the first platform, per the project decision) is the only
  credential store. Linux Secret Service and Windows Credential Manager are
  out of scope.
- **No file fallback** (recommended, D4). If the keychain is unavailable,
  `connect` and `send` fail with `credential_store_unavailable`.
- If the owner chooses a fallback (D4 options), it must be an explicit flag,
  Preview-only, mode 0600 in a 0700 directory, with absolute expiry ≤ 24 h, and
  a printed warning. No default.
- Metadata in `profiles.json` is non-secret and never trusted to select an
  origin without a matching keychain item.

## 9. What needs separate approval (nothing here is authorized)

| Item | Needed by | Approval |
| --- | --- | --- |
| Reviewed migration: `local_client_pairings`, `local_client_grants` (owner-composite keys, unique credential hash index) in **Preview** first | LC1 | Separate reviewed migration and persistent-data approval |
| New hosted routes and a per-environment enable flag / environment ID setting | LC1 | Deploy and configuration approval (not provider settings) |
| Web "Connect a tool" approval page and grants list | LC1 | UI review (P1 copy) |
| New server rate-limit and pending-row cap | LC1 | Operational approval |
| macOS release signing or notarization for the MCP/CLI binaries | T6/T7 | Release owner (**UNVERIFIED** whether it exists) |
| Provider changes: **none for LC1** | | |
| Flow A only: Neon OAuth client creation, "Allow Localhost", consent/login pages, `client_id` handling | Flow A | Provider/security-setting approval (explicitly out of scope here) |
| Creating any account or credential for the owner test | #25 | Caroline, at test time |

Persistent grants created by the flow are themselves "persistent access":
enabling the routes in an environment is a separate decision from merging code.

## 10. Open owner decisions

None of these is silently chosen. Recommendations are design input.

| ID | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| D1 | Flow | C app-mediated device-style; A Neon OAuth loopback; B pasted code | **C** (section 3) |
| D2 | Approve the new-persistence path (section 9) for Preview, or take Flow A's provider path | Persistence in Preview first; provider path | Persistence in Preview first, via a reviewed migration |
| D3 | Credential lifetime | 24 h; 7 days; 30 days; short credential + rotating refresh | **7 days absolute**, no refresh (revocation is instant, so a refresh adds complexity without a bound benefit) |
| D4 | File fallback | None; explicit Preview-only 0600 file ≤24 h; none until signed binaries | **None** |
| D5 | Send confirmation UX | (1) MCP `draft_send` behind the host's tool-approval prompt; (2) MCP only prepares, and the human confirms in a TTY (`mealplanner send confirm`) showing destination, account and content hash | **(1) for the Preview test** (it is the flow Caroline will use); evaluate (2) for Production, because a host prompt shows arguments but not the destination |
| D6 | Send-credential read scope | Owner-wide drafts/receipts; only drafts created by that grant | **Only those created by the grant**; limits what a stolen credential reads. Needs a #77 amendment (recorded owner grant ID on submissions) |
| D7 | Cascade on compromise | Manual "revoke all"; grant tied to the Neon session (impossible: no events) | **Manual "revoke all" plus a visible grant list**; the app must say password reset does not revoke tools |
| D8 | Production profile | Not enabled until the #55 gate; enable C after; use Flow A (blocked) | Not enabled before the owner test; revisit after; Production needs its own approval |
| D9 | Pairing abuse limits and concurrent grants | e.g. 5 active grants per account, 10 pending pairings, per-IP limits | 5 active / 10 pending, tuned by ops. Also decide whether `user_code` is stored hashed (unusable for lookup unless a keyed HMAC is used) or plain with a 10 min TTL; recommend keyed HMAC |
| D10 | Same-user malicious-process stance | Accept; require signed binary and a keychain ACL | Accept for Preview; require signing before Production |
| D11 | Email-change behaviour (T9) | Fail closed until re-pair; follow subject | **Fail closed** |

## 11. Out of scope

- Implementation of any route, table, tool, CLI command or UI.
- Creating accounts, credentials, tokens or grants; changing membership,
  persistent access, Neon settings, Vercel settings or any security setting.
- Hosted submission itself (#72/#77), the review UI, approval or Production.
- Replacing `NEON_AUTH_*` configuration or the JOSE profiles.
- Sender-constrained tokens and non-macOS keystores.

## 12. Acceptance mapping (issue #73)

| Acceptance item | Where | Status |
| --- | --- | --- |
| Supported vs unsupported capabilities backed by official sources; unresolved marked unverified | Section 2 | Done; items listed as **UNVERIFIED** |
| Flow prevents pairing replay, callback/state substitution, cross-environment/account reuse, credential exposure to model | Sections 1, 5, 6 | Designed; tests specified |
| Removed membership, expired/revoked credentials, account switching with testable outcomes and a bound | Sections 6, 7 | Designed |
| Required new persistent grants/config and separate approvals identified | Section 9 | Done |
| Security review approves, or records a precise blocker and bounded alternatives | Section 10 plus reviewer list below | **Pending.** This document is the review input |
| No live auth changes, accounts, tokens, credentials or hosted submission | Whole document | Satisfied; only reading and synthetic design |

### Reviewer checklist (security review must decide)

1. Is an app-implemented pairing protocol acceptable, versus Flow A's delegated issuance?
2. Is "typed code from own terminal + signed-in approval + fingerprint" adequate against remote phishing for a send-only credential?
3. Is a single opaque credential with absolute expiry (no refresh) acceptable?
4. Is the keychain-only policy sufficient without verified ACL behavior (section 2.5)?
5. D6: should a stolen credential be able to read all of an owner's drafts?
6. Does the order of checks (401 before 403 before 404) match #77's 403-before-lookup rule?
