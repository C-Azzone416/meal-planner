# Local client sign-in (design)

Status: **proposed design LC1 (revision 2, 2026-11-11), issue #73.** Design only. It is not
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
  *disables* localhost. Neon's **Manage API and webhooks** document no session
  or token revoke endpoint and no session events. (The app's own same-origin
  auth proxy does forward `sign-out` and `revoke-other-sessions` to the
  provider; section 7 and D7 cover using that as a partial cascade.)
- Those limits mean a provider-issued token can neither express "send-only"
  nor be revoked faster than its lifetime. Send-only (#77) must be enforced by
  the application either way.
- **Recommendation (LC1):** an **app-mediated, device-style pairing** using
  existing Neon sign-in only for the *owner's approval step* (a bearer-header
  call, so the cookie session is not itself an approval authority, T13),
  followed by an
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
  password reset does **not** revoke grants unless the D7 proxy-cascade option
  is chosen, and even then only for calls that go through the app's proxy
  (section 7). Owners need a "revoke all connected tools" action after any
  compromise.
- Two #78 limits are stated plainly rather than hidden: a **provider-side email
  change is not detected** by the recommended design (T9, D11), and a member
  who is **removed and later re-added with the same email silently revives
  their unexpired grants** unless those grants were revoked at removal (T9,
  D12).

## 1. Threat model

### 1.1 Assets and trust

| Asset | Where | Should be visible to |
| --- | --- | --- |
| Send credential (`mp_lc1_…`) | OS keychain; in memory of the CLI/MCP *process*; `Authorization` header | Only the local client process. **Never** the model, tool arguments/results, draft files, logs, env vars, argv, crash dumps. For a shell-capable agent host this is a goal gated by the unverified keychain ACL, not a guarantee (T1). |
| Pairing `code_verifier` | memory of the CLI process for ≤10 min | CLI process only. Never written to disk. |
| Pairing `user_code` | terminal display; typed into the web app by the owner | The owner. Low value alone (useless without the verifier and the owner's session). It *may* appear on screen. Never in a URL. |
| `pairing_id` | CLI process memory; server; the redeem URL path | CLI and server only. **Never** returned to the approving browser (approval uses the separate `ref`, section 5.4). |
| Approval `ref` | returned to the approving session by lookup; valid 2 minutes | The approving owner session only. Useless without that session's bearer. |
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
| T1 | **Model/agent sees credentials or tokens** (prompt injection, verbose tool result, host transcript, draft text) | The credential never crosses MCP: the MCP server process reads it from the keychain and sets the header internally. No MCP tool accepts or returns a token, token path, or environment variable for one, and **there is no CLI `send` command** (section 8.1), so a shell cannot trigger a send. `connect` runs only in the CLI on a TTY, never as an MCP tool. Pairing never delivers the credential through the terminal or browser: it is returned over HTTPS to the polling CLI process (section 5.2). Tool results use an allowlisted schema (section 8.3). Redacting HTTP client; no header logging; redirects not followed. | **Shell-capable agent hosts (e.g. Claude Code) are not covered by the "never visible to the model" claim.** Such a host can run any `mealplanner` CLI command (`status`, `disconnect`, and `connect` under an allocated pty: a TTY check is not a boundary against it). **A pty-capable agent can also drive `connect`'s prompts, including the step 7 confirmation (section 5.2): step 7 is a human control, not a boundary.** The approval itself still requires the owner's signed-in browser session (T13), so the agent cannot approve its own pairing. The host can also read `profiles.json` (non-secret, but it contains the keychain account name `preview/<hash16>`) and run `security find-generic-password -s mealplanner.local-client.v1 -a preview/<hash16> -w`, which prints the secret to stdout, and so into the model's context, if the keychain releases it. **The only barrier is the keychain item ACL / macOS keychain prompt, and its behaviour for a different calling binary is UNVERIFIED** (section 2.5, LC-K3). Without a shell tool the guarantee holds by construction (no tool returns the secret). With a shell tool it reduces to the same-OS-user residual in T7. That is acceptable only for the Preview test with synthetic data; D5 and D10 gate anything beyond it. |
| T2 | **Pairing replay** (re-using a user code, polling secret or the issued credential) | Pairing is single-use: `pending → approved → consumed` (or `denied`/`expired`). The credential is created and returned in one transaction that sets `consumed`; a second redeem returns the same `410 pairing_consumed` with no credential. `code_verifier` is checked against the stored S256 challenge (RFC 7636 §4.6) and only the redeeming CLI holds it. 10-minute expiry. `user_code` is deleted at consumption. Polling ≥5 s, `slow_down` (RFC 8628 §3.5). | A lost redeem response orphans a grant; the owner revokes it from the app list (section 7); the CLI re-pairs. Accepted: simplicity over idempotent credential retrieval, which would weaken replay prevention. |
| T3 | **Callback/state substitution (login-CSRF)** and the **remote-phishing variant of pairing** (approval-page web attacks are T13) | There is **no callback** in the recommended flow, so no redirect URI, authorization code or `state` is exposed to interception. The CLI only accepts a credential from the exact pairing it created (pairing ID + verifier). The approval page shows client label, creation age and a short challenge fingerprint, and asks the owner to type the code they see in their own terminal. **That is a social control, not a technical defense.** It helps an owner who is at the CLI, but against remote phishing the attacker's CLI runs on the attacker's machine and shows the attacker the same code and fingerprint, so the owner cannot tell the difference (RFC 8628 §5.4: the confirmation "doesn't prevent a phisher from presenting a fresh token" when interacting in real time). The technical bound is that the credential is send-only (D6) and revocable. Any loopback variant (Flow A, section 3) must use `state` ≥128 bits, PKCE S256, `127.0.0.1`/`[::1]` only, exact path, one request, listener closed after first request, and reject mismatched `state`/`iss` (RFC 8252 §§7.3, 8.3, 8.9, 8.10; RFC 9207 §2.4). | Social engineering: the owner approves a code an attacker gave them. The approval-screen text and fingerprint do **not** stop a live remote phisher. Impact is bounded to send-only plus drafts reads (D6). |
| T4 | **Cross-environment grant reuse** (Preview credential used on Production or vice versa) | Credentials are issued, hashed and verified in the *environment's own database*; there is no shared table or key. Server also compares `grant.environment_id` with its configured environment ID and rejects mismatch. Token prefix carries the environment (`mp_lc1_pv_…`); the client compares prefix, profile, `whoami.environment` and receipt link origin before and after sending (section 4.2). No Production profile exists in the Preview build/config. | A client bug that ignores the preflight: tested by the matrix (LC-E1..E5). |
| T5 | **Cross-account grant reuse / account switching** | A grant is bound to `(environment, owner issuer, owner subject)` set at approval from the verified browser session, never from client input. Every `draft-v1` query already includes both owner fields (#77). Keychain items are keyed `env + hash(issuer,subject)`. Switching accounts is an explicit `mealplanner use` + new pairing; there is no implicit "most recent". Local send ledger and idempotency keys are keyed by environment *and* account (#77 §9). | Same person with two accounts mis-selects: confirmation text always shows `Preview · origin` and `Account · label` (#77 §8.1). |
| T6 | **Token theft from disk** (backups, Time Machine, dotfile sync, file fallback, shell history) | Credential lives only in the OS keychain. **No file fallback by default** (D4). No env-var or flag token inputs. Config file holds non-secret metadata only (0600). Credential is opaque, 256 bits, stored server-side only as SHA-256. Absolute expiry and server revocation. | Keychain item ACL strength depends on binary signing: **UNVERIFIED** (section 2.5). |
| T7 | **Malicious local process** (same user; other user; port squatter; reads argv/env; ptrace) | Other-user: keychain and 0600 files. Port squatting: the recommended flow opens no listener. Argv/env: the credential is never in either; keychain library must not pass the secret on a command line (**implementation acceptance test**, section 6). Same-user code execution: **cannot be fully prevented**. LC1 limits the blast radius: send-only (cannot approve, edit, read recipes or other routes), expiry, instant server revoke, last-used metadata and visible grant list. | A same-user attacker can read what the user can read. Sender-constrained tokens (e.g. DPoP, RFC 9449) would shrink this and are **not** designed here (not researched). |
| T8 | **Capability confusion** (send credential used for owner operations; Neon JWT used as a send credential; a provider-issued token treated as owner) | The credential is a different, *non-JWT* format. `hosted.RequireUser` (`hosted/bearer_auth.go`) requires a three-part JWT and rejects it structurally; owner routes need no change. A new `RequireLocalClient` accepts only the prefix and applies the capability matrix (section 8.2) **before** object lookup, matching #77's 403 rule. | A future change that makes a shared auth helper accept both types. Test: send credential on every owner route. |
| T9 | **Removed or ineligible member keeps access; provider email change; removal then re-add** | Each request re-runs the #78 membership policy against the **email recorded at approval** (`email_at_approval`) plus the grant's expiry/revocation. No cache. Bound in section 7. **Guarantee, stated honestly:** a grant works while `email_at_approval` is in the current #78 allowlist (`AUTH_ALLOWED_EMAILS`, evaluated per request). The credential is opaque, so the server never sees the account's *current* provider email, and **a provider-side email change is not detected**: the grant keeps working under the old email until it expires or is revoked. Optional hardening (D11 option b): a per-request lookup of the account's current email by `(issuer, subject)`. That needs a data source the app does not read today (a Neon Auth user table or API); its existence and shape are **UNVERIFIED**. **Removal then re-add:** #78 membership is an environment setting with no removal hook, so nothing revokes an email's grants when it is removed. If the same email is re-added while a grant is unexpired, the grant is **silently revived**. Bounds: the absolute expiry (D3, 7 days recommended) and an operator step "revoke all grants for that account before removal" (D12 lists options, including a code-enforced one). | (1) Provider email change: the old-email grant keeps working; the new email has no grant until a new pairing. (2) Revival on re-add, until `expires_at`. A recycled or reassigned email address is the realistic form of both. Neither exceeds send-only. |
| T10 | **Pairing spam and user-code brute force** (unauthenticated create route; guessing the owner's pending code from another member's session) | Unauthenticated create is limited per IP and by a pending-row cap (section 5.5). Codes: 8 characters from a 20-letter alphabet, 20⁸ ≈ 2.56×10¹⁰ values (≈34.5 bits). **A wrong code matches no pairing, so a per-pairing counter cannot count wrong guesses.** Guesses are counted against the *guesser*: per approving session, per account, per IP, plus a global cap, each with a window, a lockout and a typo rule (section 5.5, tests LC-P7a..j). Lookup is member-gated first (#78), so the attacker must hold a signed-in member session. Approval needs a separate opaque `ref` returned by lookup (the approver never learns `pairing_id`), a typed confirmation, and a POST with an owner bearer (T13). Recomputed attacker success: ≈3.9×10⁻¹⁰ per owner pairing for one attacker account, and ≤3.9×10⁻⁹ even at the global cap (section 5.5). A hit binds the owner's CLI to the **attacker's** account (RFC 8628 §5.1 makes the same point), so drafts the owner sends would land in the attacker's account. Residual mitigation: the CLI shows the bound account label and asks for confirmation before storing the credential (section 5.2 step 7). | Distributed abuse of the unauthenticated create route; shared-IP and global lockouts can inconvenience legitimate users (D9). |
| T11 | **Credential in logs, telemetry, error text, crash reports, draft files** | Redacting transport; errors never include headers or bodies; hosted error messages already exclude secrets (#77 §3.1); canary tests with a synthetic token search every output (section 6). Drafts and the local store never contain tokens (the send path reads drafts but writes only a non-secret ledger). | Third-party host telemetry of MCP *arguments/results*: none contain secrets by construction. |
| T12 | **Wrong destination** (profile edited, typosquat origin, redirect) | Profile pins exact HTTPS origins; the credential lookup key includes the origin, so an altered origin finds no credential. No redirects followed. Receipt/draft links must resolve to the profile web origin. | Same-user edit of config to a new origin forces a fresh pairing against it; the owner would see the unexpected origin at the pairing prompt. |
| T13 | **Approval page web surface**: CSRF on approve/deny, clickjacking and framing of the approval page, forged `Origin`, a `verification_url` that pre-fills the code | **Owner authentication for `/api/local-client-v1/approvals/*` is the `Authorization: Bearer <Neon JWT>` header verified by `RequireUser`, as the SPA already does for `/api/recipes` (`web/src/recipes/api.ts`, `web/src/auth/service.ts`).** These routes **ignore cookies entirely** (never read `Cookie`, never set one). A cross-site form post or image request carries the session cookie but the server does not look at it, and a cross-origin page has no access to the SPA's token; a cross-origin `fetch` with an `Authorization` header needs a CORS preflight and no CORS is granted. That is the CSRF defense, and it **deliberately differs from the auth proxy's**: `hosted/auth_proxy.go` is cookie-authenticated (provider cookies rewritten to `SameSite=Lax`) and its `authProxyRequestOrigin` **accepts a request with no `Origin` header**, so its posture is Lax cookie plus a best-effort Origin check and must not be copied for approvals. Defense in depth on the approval POST: POST only (GET/HEAD return 405 and never change state; no state-changing GET anywhere); `Content-Type: application/json` required; a mismatched `Origin`, or `Sec-Fetch-Site: cross-site`, returns 403 (OWASP's Fetch Metadata guidance with an Origin fallback; a *missing* `Origin` is accepted only with `Sec-Fetch-Site: same-origin`). **Framing:** the approval page and every `/api/local-client-v1/*` response send `Content-Security-Policy: frame-ancestors 'none'` and `X-Frame-Options: DENY` (the second for browsers without `frame-ancestors`; where both are understood, `frame-ancestors` wins). The repository sets neither today (searched `hosted/`, `api/`, `web/`, `vercel.json`), so this is a new deploy-config requirement (section 9). **Typed/fresh confirmation:** approving is not one click on a pre-filled page. The owner must retype the user code in the approval box, and the request must carry a JWT issued within the last 5 minutes (`iat`); an older token gets `401 reauth_required` and the SPA fetches a fresh one. **`verification_url` never embeds `user_code`** (`https://<web_origin>/connect`, no query string). RFC 8628 defines an optional `verification_uri_complete` that includes the code and is "designed for non-textual transmission" (QR/NFC, §3.2, §3.3.1); it removes the typing step, and §5.4 says that makes it "particularly important" to confirm the device is in the user's possession. LC1 omits it, so no link can pre-fill or auto-submit a code. | Clickjacking protection depends on the browser honouring either header (current browsers do; not tested per browser here). An XSS in the SPA could call the approval API with the owner's token; XSS defeats CSRF defenses (OWASP) and is an existing app-wide risk outside this design. A signed-in owner who types a code supplied by a phisher is T3. |

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
| Neon-side session/token revocation: Manage API lists **no session or token endpoints**; webhooks have **no session events** (7 events, none for sessions). Session lifetime: not documented on the pages read. | Documented absence | <https://neon.com/docs/auth/guides/manage-auth-api> (2026-10-09); <https://neon.com/docs/auth/guides/webhooks> (2026-09-16) |
| Sign-out / revoke other sessions / revoke on password reset: **these exist at the app level.** `hosted/auth_proxy.go` forwards `sign-out` and `revoke-other-sessions` (and the password-reset routes) to the provider, and `docs/runbooks/authentication.md` documents the Neon setting **Revoke sessions on password reset** (linking Neon's password-reset guide, not re-read here). They revoke *cookie sessions*; an already-issued JWT stays valid to its `exp` (runbook). | Repo + runbook (provider-side semantics beyond the runbook not re-verified) | `hosted/auth_proxy.go`, `docs/runbooks/authentication.md` |
| **OAuth Provider** (OAuth 2.1 authorization server): `authorization_code` + `refresh_token`; no `client_credentials`; public clients with PKCE for "single-page, mobile, desktop, and CLI apps"; MCP support | Documented | <https://neon.com/docs/auth/guides/plugins/oauth-provider> (2026-11-06) |
| OAuth Provider **scopes are fixed**: `openid`, `profile`, `email`, `offline_access`. "Scopes, token lifetimes, and other provider options aren't configurable." | Documented | OAuth Provider page |
| OAuth Provider access token 1 hour; refresh token 30 days, rotated on every use, only with `offline_access`; token carries `client_id` and `scope`; developer must host login and consent pages ("doesn't provide default pages") | Documented | OAuth Provider page |
| OAuth Provider **resources**: a list of resource identifiers (up to 10 configured); a requested resource "becomes the token's `aud` claim". If the authorize and token requests omit `resource`, an **opaque token** is issued instead of a JWT and "your resource server can't verify it". Per-token `client_id` and `scope` claims. **Client ID Metadata Documents (CIMD)** on by default once a server URL is set; legacy **Dynamic Client Registration** (`/oauth2/register`) off by default. **No revoke or introspection endpoint is documented**; "Deleting a client doesn't revoke tokens that were already issued to it." | Documented | OAuth Provider page (2026-11-06, read 2026-11-11) |
| Redirect URIs for managed clients: `https`, or `http` on a loopback host **when localhost access is enabled for the branch**; custom schemes such as `myapp://` fail | Documented | OAuth Provider page |
| Localhost toggle: Manage API endpoint `/allow_localhost` (GET, PATCH), "Toggle localhost access for development". The request-body field name is not shown on any page read. | Documented (endpoint only) | <https://neon.com/docs/auth/guides/manage-auth-api> (2026-10-09) |
| Production checklist item 6, **"Disable localhost access"**: 'Disable the "Allow Localhost" setting' in Settings → Better Auth; "enabled by default for development but should be disabled in production" | Documented | <https://neon.com/docs/auth/production-checklist> (2026-10-09) |
| Trusted domains: development origins such as `http://localhost:3000`, `http://localhost:5173` and "any localhost port" are described as pre-configured. **This page does not name the "Allow Localhost" setting**, and the pages differ on how localhost is gated (this page lists no toggle; the OAuth Provider page ties loopback `http` redirects to the branch's localhost-access setting). Do not assume loopback redirects work on any branch without checking the setting. | Documented | <https://neon.com/docs/auth/guides/configure-domains> (2026-11-05) |
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
Neon now documents resources and opaque access tokens (section 2.1). Only a
revoke endpoint remains undocumented (section 2.3, item 1).

### 2.3 UNVERIFIED provider behaviours (do not rely on them)

1. Whether Neon exposes a revocation endpoint for OAuth Provider tokens and
   whether its JWT access tokens are revocable. None is documented on the
   OAuth Provider page (2026-11-06); upstream Better Auth says JWT access
   tokens cannot be revoked.
2. Whether `client_id` appears in Neon-issued access-token claims in practice.
   It is now **documented** ("`client_id` identifies the client, `scope` lists
   the granted scopes"); not observed.
3. **Partly verified (2026-11-11).** Neon documents `resources` (up to 10, each
   becoming `aud` when requested), per-token `client_id`/`scope`, CIMD (default
   on) and DCR (default off); when `resource` is omitted the token is opaque
   and "your resource server can't verify it". Still **UNVERIFIED**: whether a
   token with `aud` set to a registered resource for our API would also pass,
   or fail, the existing owner verifier (`hosted/bearer_auth.go` requires `aud`
   to contain the issuer origin), and whether such a token could be told apart
   from an owner token safely. **Flow A's conclusion is unchanged**: scopes
   remain fixed (`openid`, `profile`, `email`, `offline_access`), so "send-only"
   still cannot be expressed in the token; it would have to be inferred from
   `client_id` plus a server allowlist, and with no documented revoke or
   introspection endpoint a leaked token stays valid for its lifetime (1 h
   access, 30 d rotating refresh).
4. Whether any device-code behaviour is available but undocumented.
5. Token field naming: the JWT page shows `session.token`, the
   authentication-flow page `session.access_token` (the pages disagree).
6. Whether Preview's branch already has localhost access ("Allow Localhost")
   enabled. Provider state was **not inspected** (no console access, by
   instruction).
7. Behaviour when a user's email changes at the provider (affects T9, D11).
   Also whether the app can read the account's *current* email per request
   (a Neon Auth user table or API); nothing in this repo reads one.
8. Whether the verified Neon JWT carries a session identifier claim. The JWT
   page documents `sub`, `iss`, `aud` only, and nothing in this repo reads one.
   **Treat C1 (section 5.5) as expected-absent**: the design must hold with C2
   (10 per hour per account) as the first limiter. A session claim, if ever
   found, only adds C1.
9. How the hosting platform exposes the real client IP (the per-IP counters of
   section 5.5 depend on a trustworthy source).
10. **`iat` for the 5-minute fresh-token rule (T13, section 5.4).** Neon documents
    `iat`, but `hosted/bearer_auth.go` treats it as optional and does not expose
    it to handlers, so the rule cannot be enforced today. **A missing `iat` fails
    closed: `401 reauth_required`.** The verifier must change to require and
    expose `iat` for these routes (section 9). **UNVERIFIED:** whether the SPA
    can obtain a freshly minted JWT on demand (the auth service may return a
    cached token). If it cannot, approval would always return `reauth_required`
    and the owner would have to sign in again; this degrades safely but may be
    unusable until verified.

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
| RFC 8628 §3.2, §3.3.1, §5.1, §5.4 (re-read 2026-11-11) | <https://www.rfc-editor.org/rfc/rfc8628> | `verification_uri_complete` is optional and "designed for non-textual transmission"; remote phishing (§5.4); the "8-character base 20 … 5 attempts" example is illustrative (§5.1) |
| MDN: `frame-ancestors`, `X-Frame-Options`, Clickjacking (accessed 2026-11-11) | <https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Content-Security-Policy/frame-ancestors>, <https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/X-Frame-Options>, <https://developer.mozilla.org/en-US/docs/Web/Security/Attacks/Clickjacking> | `frame-ancestors 'none'` is similar to `X-Frame-Options: deny`; where both are set, browsers that support `frame-ancestors` ignore `X-Frame-Options` (stated on the Clickjacking page) |
| OWASP CSRF Prevention Cheat Sheet (accessed 2026-11-11; part of the page was read through a tool summary) | <https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html> | Cookie sessions are sent automatically; custom request headers fall under the same-origin policy; Fetch Metadata (`Sec-Fetch-Site`) with an Origin fallback; "XSS can defeat all CSRF mitigation techniques". The page does not discuss the `Authorization` header by name, so that header's CSRF behaviour is argued from the browser model in T13, not cited |

Reading gaps (disclosed in the PR): the last ~6 KB of RFC 9700 §4.x and some
RFC 9700 §4 requirements were taken from summaries rather than verbatim; the
OAuth 2.1 draft body was not read; RFC 8693 was only skimmed and is not relied on.

### 2.5 Not researched

OS keychain behaviour (macOS Keychain access-control/ACL, code-signing
requirements, whether a different binary such as `/usr/bin/security` is prompted
or denied when reading an item another binary created, how popular Go keychain
libraries pass secrets) was **not checked against official Apple documentation**
(developer.apple.com pages need JavaScript and could not be read). Everything
about keychain strength is **UNVERIFIED** and is an implementation acceptance
gate (section 6, LC-K1..K3). One fact was read locally on 2026-11-11 from the
`security` tool's own help on the author's macOS (`security add-generic-password
-h`, `security find-generic-password -h`, `man security`): `add-generic-password
-w <password>` takes the secret **as a command-line argument** (visible in
process listings while the command runs), and `find-generic-password -w` /
`-g` print the secret to stdout. Any implementation that shells out to
`security` therefore exposes the secret on argv or in its output, and any
process that may run `security find-generic-password` is exactly the T1
shell-host path. This is local tool help, not an official web source.

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
scopes, (4) 1 h unrevocable access tokens, and (5) the remaining UNVERIFIED items in section 2.3 (revoke endpoint; our-API
resource handling).
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
| `name` | `preview` (exact; no default; `--env` is required on `connect`, `use`, `status`, `disconnect`. There is **no CLI `send`**, section 8.1) |
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
   `Account · <display label>`; the owner retypes the code and confirms there
   (T13). After redeem, the CLI prints the bound account label and asks for
   confirmation before storing the credential (section 5.2 step 7). Account switching
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
   (`BCDF-GHJK` style) and the plain verification URL (no code in it). Open the
   Preview web app, signed in, enter the code, check environment, account,
   client label, age and fingerprint, retype the code, approve. The CLI prints `Connected: Preview · <origin> ·
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
| `POST /approvals/lookup` | app-owner Neon JWT in `Authorization` (`RequireUser`); cookies ignored | Resolve a typed `user_code` to a summary (client label, age, fingerprint, environment) and an opaque approval `ref`. Never returns `pairing_id`. Subject to the attempt counters of section 5.5 |
| `POST /approvals/{ref}` | same; JWT `iat` within 5 minutes; JSON body only | `approve` or `deny`, with the retyped code (section 5.4) |
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
   `verification_url` is the bare page URL (`<web_origin>/connect`); it never
   contains the `user_code` and there is no `verification_uri_complete`
   (T13).
   `user_code`: 8 characters from `BCDFGHJKLMNPQRSTVWXZ`.
3. CLI prints the user code, `Preview · <origin>`, and the web verification URL.
   It opens nothing automatically.
4. Owner (signed in to the Preview web app) types the code into the approval
   page, which calls `POST /approvals/lookup`. The server first applies the
   member gate and the section 5.5 counters, then looks up the code. On a hit it
   mints the opaque `ref` (section 5.4) and the page shows: environment,
   account label (built as in step 7: it includes the verified normalized
   email), client label (marked "name chosen by the requesting
   program"), age ("created 40 s ago"), a 6-character fingerprint of the
   challenge (also printed by the CLI), and the text "Allows saving
   **unreviewed drafts only**. It cannot approve recipes, edit drafts or read
   your recipes." Approve requires retyping the code. The checks "type the
   code from your own terminal" and "compare the fingerprint" are a social
   control (T3), not a technical one.
5. On approve: the server records the owner `(issuer, subject)` and the
   normalized email from the **verified bearer** (never from request fields),
   plus the membership result, `state = approved`.
6. CLI polls `POST /pairings/{id}/redeem` `{code_verifier}` every ≥5 s (RFC 8628
   §3.5). Server checks `S256(verifier) == challenge`, state, expiry, then in one
   transaction creates the grant (`credential = "mp_lc1_pv_" + b64url(32 random
   bytes)`, stores only SHA-256), sets `consumed`, deletes the user code, and
   returns the credential in the HTTPS response body.
7. With the credential still only in process memory, the CLI calls `whoami`,
   prints `Account · <label> · Preview · <origin>` and asks the user to confirm
   it is their account (a wrong-account approval, T10, is visible here).
   **Label source (matches #77 §8.1's "authenticated display label").** The
   server builds `<label>` from the verified, allowlist-gated, **normalized
   email** recorded at approval (step 5), optionally followed by the JWT `name`
   for readability. **`name` alone is never the label**: it is user-controlled,
   so another #78 member could set theirs to the owner's name and make a
   wrong-account approval look right. The approval page (step 4) shows the same
   label. The prompt **defaults to "no"** (Enter or EOF means no) and proceeds
   only if the user types the label's email or `yes`. On
   "no" it calls `DELETE /grants/self` and exits. On "yes" it stores the
   credential in the keychain (item key from section 4.1), writes non-secret
   profile metadata and zeroes the verifier. If the keychain write fails, the
   CLI calls `DELETE /grants/self` and exits; it never falls back to a file.

### 5.3 Credential properties

| Property | Value |
| --- | --- |
| Format | `mp_lc1_<env>_<43 base64url chars>` (256 bits); opaque; not a JWT, so `RequireUser` rejects it |
| Server storage | SHA-256 hash only, unique index; no plaintext, no reversible storage |
| Bound to | `environment_id`, `owner_issuer`, `owner_subject`, `email_at_approval`, `scope = draft:send`, `grant_id` |
| Lifetime | Absolute `expires_at`; recommended 7 days (D3). No sliding extension, no refresh token in LC1 |
| Per-request checks | Hash lookup; `environment_id`; not revoked; not expired; #78 membership against `email_at_approval`; then capability (section 8.2) |

### 5.4 Approval authentication and the approval `ref`

- **Owner authentication** for `/approvals/*` is the Neon JWT in the
  `Authorization` header, verified by `RequireUser` (so #78 membership applies
  first), never a cookie. Rationale and the CSRF/framing defenses are in T13.
  Approve requires a JWT issued ≤5 minutes ago, JSON bodies only.
- **`ref`**: 128 random bits, minted by a *successful* lookup, stored hashed
  with the looking-up `(issuer, subject)` and bound to that pairing, valid 2
  minutes, at most 3 live refs per pairing. `POST /approvals/{ref}` succeeds
  only for the same `(issuer, subject)` that looked it up and only with the
  retyped code; 3 wrong confirmations invalidate the `ref` (not the pairing).
  An unknown, expired or foreign `ref` returns the same `404`.
- **Why a separate `ref` instead of `pairing_id`.** `pairing_id` is the handle of
  the *unauthenticated* `POST /pairings/{id}/redeem`, where a wrong verifier
  counts toward the per-pairing redeem limit (LC-P4: 5 wrong verifiers
  invalidate the pairing). If lookup or approval exposed `pairing_id`, any
  other member who learned a pending code could hammer `redeem` with bad
  verifiers and burn the owner's pairing (denial of service), or spam it with
  polling to force `slow_down`. With a separate `ref`, `pairing_id` exists only
  in the CLI process and the server, and a `ref` cannot be used on any
  unauthenticated route. Server logs record `pairing_id` and `ref` only as a
  short hash prefix.

### 5.5 Brute-force limits for the user code

**Why not "5 attempts per pairing".** A wrong code matches no pairing, so there
is nothing to attach a counter to (RFC 8628 §5.1's "5 attempts" is a rate-limit
*budget*, not a per-record counter). Wrong guesses are charged to the
*guesser*. The counters are stored in the database (the deployment is
serverless-style, `vercel.json`, so in-memory counters are per instance and
unusable) in a new attempts table (section 9), incremented atomically **before**
the code is evaluated, with **no refund and no reset on success**. Charging all
well-formed lookups (not only failures) closes two bypasses: parallel guesses
racing the counter, and an attacker interleaving lookups of their own valid
pairing to reset it.

| Counter | Key | Budget (well-formed lookups) | Window | Lockout |
| --- | --- | --- | --- | --- |
| C1 per approving session | `(issuer, subject, session id)` from the verified JWT (**expected absent**: Neon's JWT has no documented session-id claim; C1 is then dropped and C2 is the first limiter) | 5 | 10 min (one pairing lifetime) | 15 min of `429 locked` + `Retry-After`; correct codes are not evaluated while locked |
| C2 per account | `(issuer, subject)` | 10 | 60 min | until the window ends; after 3 lockouts in 24 h, 24 h, plus an operator alert (hashed identity only) |
| C3 per IP | hash of the client IP (**UNVERIFIED** trusted source) | 20 lookups; separately 10 `POST /pairings` and 3 pending pairings | 60 min | 60 min |
| C4 global (per environment) | environment | 100 | 60 min | until the window ends (an intentional availability trade-off) |

- **Typos.** The page validates format before submitting. The alphabet
  (`BCDFGHJKLMNPQRSTVWXZ`) has no vowels or digits, so there are no `0/O`, `1/I`
  confusions; input is normalised (case-insensitive, spaces/hyphens stripped). A
  malformed code (wrong length or alphabet) returns `400`, is **not** charged to
  C1/C2 (it cannot match, so it gains the guesser nothing) and is covered only by
  a coarse request-rate limit. A well-formed typo costs one attempt. **With C1
  expected-absent (section 2.3, item 8), the budget is C2's**: 10 well-formed
  lookups per account per hour, shared by every session and every pairing, so an
  owner survives 9 typos in an hour and the 10th lookup is the last one
  evaluated. If C1 exists, it adds a tighter 5-per-10-minutes limit per session
  (4 typos). On a C2 lockout the owner waits until the window ends (up to an
  hour; 24 h after 3 lockouts in 24 h); the page shows `Retry-After`, and
  signing in again or creating a new pairing does not help. An owner who burned the
  budget on typos cannot pair until it resets. **Creating a new pairing resets nothing.**
- **Uniform responses.** Nonexistent, expired, consumed, denied and
  other-environment codes return one identical `404 code_not_found`; lockouts
  return one identical `429`. Timing differences between those paths are not
  specified here (reviewer item).
- **Attacker success probability (recomputed).** Let N = 20⁸ = 2.56×10¹⁰. The
  attacker needs the *owner's* pending code (hitting their own pairing gains
  nothing) within its 10-minute life. Single attacker account (another #78
  member): C2 caps guesses at 10 per hour, so **p ≤ 10 / N ≈ 3.9×10⁻¹⁰ per
  owner pairing** (≈2⁻³¹·³, comparable to the RFC 8628 §5.1 example of 5 attempts
  for ≈2⁻³²). With several colluding members or IPs, C4 caps everyone at 100 per
  hour: **p ≤ 100 / N ≈ 3.9×10⁻⁹ per pairing**. If the owner pairs once per day,
  one account's cumulative chance over a year is ≲1.4×10⁻⁷. The previous text
  ("≤5 attempts per pairing") enforced none of this, because a wrong code never
  reached a pairing; its effective bound was only the generic request rate.
  These figures depend on N being uniform (a CSPRNG code) and on the counters
  being enforced in the database.
- **What a hit gives the attacker**: the owner's CLI is bound to the attacker's
  account (so the owner's drafts are sent into it). Residual handled by the CLI
  account-label confirmation (step 7), the owner-visible grant list and
  revocation, not by the counters.

## 6. Test matrix (synthetic and local only)

IDs are for the later implementation. All run against stubs, `httptest` servers
and in-memory fakes. None needs a provider, account or credential.

| ID | Case | Expected |
| --- | --- | --- |
| LC-P1 | Redeem before approval | `authorization_pending` with interval; never a credential |
| LC-P2 | Redeem after approval, correct verifier | One credential; pairing `consumed` |
| LC-P3 | Redeem again (replay) | `410 pairing_consumed`; no credential; no second grant |
| LC-P4 | Redeem with a wrong or plain-method verifier | `invalid_grant`; counts toward the **per-pairing redeem limit** (5 wrong verifiers invalidate the pairing; this works because `pairing_id` matched); no state change otherwise |
| LC-P5 | Poll faster than interval | `slow_down`, interval +5 s |
| LC-P6 | Redeem after 10 min | `expired`; user code unusable |
| LC-P7a | C1: 5 well-formed wrong codes from one session, then the 6th lookup with the **correct** code | 6th returns `429 locked` with `Retry-After`; the correct code is not evaluated and not consumed; after 15 min (fake clock) it works |
| LC-P7b | C2: 11 wrong codes spread across 3 sessions of one account | Locked at the 11th; each session's own C1 was not the limiter |
| LC-P7c | C3: 21 lookups from one IP using different accounts | Locked at the 21st; `POST /pairings` 11th in an hour refused; 4th pending pairing per IP refused |
| LC-P7d | C4: 101 lookups in an hour from many accounts/IPs | Locked for everyone until the window ends |
| LC-P7e | An attacker alternates lookups of their own **valid** pairing with wrong guesses | A successful lookup does **not** reset any counter; the limits still bite |
| LC-P7f | Parallel burst of 50 guesses | At most the budget is evaluated (atomic pre-increment); no counter race |
| LC-P7g | Typo handling: 4 well-formed typos, then the right code in one 10-min window | Succeeds; a 5th typo then a correct code is locked; a malformed code returns `400`, is not charged to C1/C2 |
| LC-P7h | Nonexistent / expired / consumed / denied / other-environment codes | Byte-identical `404 code_not_found`; lockouts byte-identical `429` |
| LC-P7i | `ref` hygiene: no response, header, log line or error from `/approvals/*` contains `pairing_id`; `ref` used by another account, after 2 min, after 3 wrong confirmations, or on `/pairings/{id}/redeem` | All rejected with the same `404`/`401`; `redeem` is unaffected by any approval call |
| LC-P7j | Non-member session calls `/approvals/lookup` | `403` before any counter or code evaluation (member gate first) |
| LC-P8 | Approve with a send credential, no session, or a cookie-only request | 401/403; no approval |
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
| LC-M2 | Provider-side email change (simulated: the same `(issuer, subject)` now has a different email; `email_at_approval` still in the allowlist) | Grant **keeps working** (documented limitation, T9); if D11 option (b) is chosen, `401` until re-pair (needs the UNVERIFIED current-email source) |
| LC-M3 | Member removed then re-added with the same email, unexpired grant, **no** revoke at removal | Grant is revived (documents the T9 risk). With the D12 chosen mitigation (operator revoke-by-email, or a membership stamp): `401` after re-add |
| LC-R1 | Revoke grant in the app; next request | 401 immediately; no cache |
| LC-R2 | `disconnect` with network failure | Local item deleted; message says the server grant remains until expiry or revocation |
| LC-R3 | Revoke all | Every grant for that account 401; other accounts unaffected |
| LC-R4 | Expiry at `expires_at` (fake clock) | 401; client reports `reconnect_required`; no auto re-pair |
| LC-R5 | Provider session revoked or password reset (simulated), calls made **directly** to the provider | Grant **still valid** (documented, section 7). If the D7 proxy-cascade option is chosen: the same calls made through the app proxy revoke that account's grants (LC-R6) |
| LC-R6 | (D7 proxy cascade only) `revoke-other-sessions` or a successful `email-otp/reset-password` through the proxy | The account's grants are revoked on the next request; a plain `sign-out` is left alone |
| LC-T1 | Canary: synthetic token string; run connect, status, send, errors, mismatch, panic paths | The canary is absent from stdout, stderr, MCP results, logs, local store, draft files, config, env and argv |
| LC-T2 | MCP `tools/list` and schemas | No tool accepts or returns token/URL/environment/credential fields |
| LC-T3 | Prompt-injection draft text asking for the token | Tool results contain no secret; there is no tool that could return it |
| LC-T4 | Redirect (301/302/307) from the API origin | Not followed; `Authorization` not forwarded |
| LC-K1 | Keychain write, read and delete through the real macOS Keychain on a developer machine, covering the `security -w` argv exposure | (a) Static: the credential package does not import `os/exec` and no code path runs `/usr/bin/security` (`add-generic-password -w <secret>` puts the secret on argv; `find-generic-password -w` prints it), the library calls the Security framework API directly. (b) Dynamic: with a canary secret, repeatedly sample `ps -ww -o pid,args` and `ps -wwE` (argv and environment) for the process tree during Put/Get/Delete, and run under process-exec tracing where available; the canary never appears. (c) The test fails if the implementation is switched to shelling out. **UNVERIFIED until run on a real machine** |
| LC-K2 | Keychain unavailable | `credential_store_unavailable`; no file, no env fallback |
| LC-K3 | Item read by a different binary, including `/usr/bin/security find-generic-password -w` run by the same OS user from a non-interactive shell (the T1 shell-host path) | Record whether it prompts, denies or returns the secret; if it returns the secret silently, T1/D10 must be upgraded to a blocker for anything beyond synthetic Preview data. **UNVERIFIED** |
| LC-D1 | `connect` without a TTY (as an MCP/agent subprocess) | Refuses; no pairing. Not a boundary against an agent that allocates a pty (T1) |
| LC-D2 | CLI surface: enumerate every subcommand and flag | No `send`, no command or flag that prints, exports or accepts a credential; adding one fails the test. If D5 option (2) is chosen, the only allowed variant is `send confirm`, which refuses without a TTY on stdin **and** stdout, reads confirmation from `/dev/tty`, and has its own test; it is documented as a human-typing control, not a boundary against a pty-capable shell |
| LC-W1 | Approve/deny with only the session cookie (no `Authorization`) | `401`; no state change; no `Set-Cookie` |
| LC-W2 | `GET`/`HEAD`/form-encoded request to approve/deny | `405`/`415`; no state change |
| LC-W3 | Approve with `Origin` of another site, or `Sec-Fetch-Site: cross-site`; `Origin` missing without `Sec-Fetch-Site: same-origin` | `403` |
| LC-W4 | Response headers on the approval page and on every `/api/local-client-v1/*` response | `Content-Security-Policy: frame-ancestors 'none'` and `X-Frame-Options: DENY` present |
| LC-W5 | `verification_url` in the pairing response; load `/connect?code=ABCD…` | URL has no query and no code; the page ignores a `code` query parameter and never pre-fills it |
| LC-W6 | Approve with a JWT older than 5 minutes; with a wrong or missing retyped code | `401 reauth_required` / rejection; no approval |

## 7. Revocation, expiry and the access-removal bound

| Event | Effect | Delay bound |
| --- | --- | --- |
| Owner revokes a grant or all grants in the app | Credential rejected | **Next request after the transaction commits** (no cache) |
| `mealplanner disconnect` | Server revoke then keychain delete; if offline, keychain delete only | Server grant ends at revoke, or at expiry if the revoke failed (the CLI says which) |
| Credential expiry | 401 | Exactly `expires_at` (server clock); decision D3 |
| Membership removal (#78) | Credential rejected through the shared policy | **Next protected request evaluated with the updated deployment configuration** (zero application cache), starting when the removal redeployment is fully serving and older deployments or aliases are retired or restricted, exactly the #78 bound. No fixed time from saving a setting. |
| Neon session revocation, sign-out, password reset | **Grants are unaffected by default.** Neon's Manage API and webhooks provide no session endpoint or event to hook (section 2.1), and a cookie-session revoke does not touch app-issued credentials. | Not revoked. Partial cascade is possible only through the app's own proxy (D7 option c): `hosted/auth_proxy.go` already forwards `sign-out` and `revoke-other-sessions`, and the runbook documents "Revoke sessions on password reset", so the proxy could revoke the account's grants after a successful forward. Calls made **directly to Neon** bypass the proxy and revoke nothing. Owners still need "revoke all" after a suspected compromise. |
| Provider account deletion | Membership removal or a missing account must be handled with the account lifecycle (#45) | Grants fail closed once the email is no longer a member; a deleted user is not detectable without a lookup, so deletion must be paired with "revoke all" |
| Provider-side email change | **Not detected.** The credential is opaque; the server sees only `email_at_approval` | Grant works until expiry/revocation while the old email stays in the allowlist (T9, D11) |
| Member removed, then the same email re-added | **Unexpired grants for that email revive silently** | Bounded only by `expires_at` unless D12 is adopted; removal itself revokes nothing because #78 membership is an environment setting with no hook (T9) |
| In-flight request | May finish | Already-accepted requests complete, as in #78 |
| Flow A, if chosen instead | Neon access token 1 h (non-configurable), refresh 30 d | Up to 1 h after provider-side change; revoke behaviour **UNVERIFIED** |

**Maximum removal delay:** zero cache, i.e. the next request, for grant
revocation, expiry and membership removal; the deployment-rollout caveat from
#78 applies to membership. There is **no** automatic removal on password reset
(unless D7 option c), **no** detection of a provider email change, and removal
is **not permanent** if the email is re-added before the grants expire.

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
**There is no CLI `send`** (decision for LC1, resolving the earlier mismatch
between sections 4.1 and 8.1): sending exists only as the MCP `draft_send` tool,
so a shell-capable agent cannot send through the CLI and bypass the host's
tool-approval prompt (D5). There is also no command that prints, exports or
accepts a credential (LC-D2). `status` prints profile, account label, scope,
expiry and connected state; never a secret. MCP adds exactly:

| Tool | Arguments | Result | Notes |
| --- | --- | --- | --- |
| `auth_status` | `{}` (no environment argument; uses the selected profile) | `{environment, web_origin, connected, account_label, scope, expires_at}` | read-only |
| `draft_send` | `{draft_id, expected_revision}` | #77 receipt subset and absolute links | Annotated destructive/open-world so hosts prompt. Does not accept a destination, environment, origin, header, credential, or idempotency key. The key is generated and retained in the local ledger (#77 §9). |

There is no MCP tool to connect, disconnect, or select accounts.

### 8.2 Capability matrix for the send credential (matches #77 §1)

| Operation | Send credential | Notes |
| --- | --- | --- |
| `POST /api/draft-v1/submissions` | Allow | |
| `GET draft`, `GET receipt`, `GET review` | Allow, owner-scoped; other owner or absent: same 404 | The credential can fetch only objects whose unguessable IDs it already holds; #77 gives it no way to enumerate them. **D6**: recommend narrowing to drafts created through *this grant* as a #77 follow-up |
| `GET /drafts` list | Deny `403 operation_not_allowed` | Not in #77's send-credential read list; the 403 is **inferred from omission** and is a **pending #77 clarification** (one sentence: "send credentials cannot call the list endpoint"). It does not block #77 |
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
| Reviewed migration: `local_client_pairings` (incl. hashed `ref` columns), `local_client_grants` (owner-composite keys, unique credential hash index) and `local_client_attempts` (counter key hash, window, count, `locked_until`) in **Preview** first | LC1 | Separate reviewed migration and persistent-data approval |
| New hosted routes and a per-environment enable flag / environment ID setting | LC1 | Deploy and configuration approval (not provider settings) |
| Web "Connect a tool" approval page and grants list | LC1 | UI review (P1 copy) |
| New server rate-limit counters (attempts table, section 5.5) and pending-row caps | LC1 | Reviewed migration + operational approval |
| Response headers `Content-Security-Policy: frame-ancestors 'none'` and `X-Frame-Options: DENY` for the approval page and `/api/local-client-v1/*`; SPA route for `/connect` (`vercel.json` rewrite). Neither header exists in the repo today | T13 | Deploy-config approval |
| Verifier change in `hosted/bearer_auth.go`: require `iat` and expose it, so the 5-minute fresh-token rule can be enforced; a missing `iat` returns `401 reauth_required` (section 2.3, item 10). Also verify the SPA can obtain a fresh JWT on demand (**UNVERIFIED**) | T13 | Code review + auth-surface approval |
| Operator "revoke grants by email" procedure (or a membership stamp in #78 config) | D12 | Runbook / #78 follow-up approval |
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
| D5 | Send confirmation UX | (1) MCP `draft_send` behind the host's tool-approval prompt; no CLI `send`; (2) MCP only prepares, and the human confirms in a TTY (`mealplanner send confirm`) showing destination, account and content hash | **(1) for the Preview test** (it is the flow Caroline will use); evaluate (2) for Production, because a host prompt shows arguments but not the destination. **State plainly:** with either option, a shell-capable agent host (e.g. Claude Code) can run CLI commands and attempt `security find-generic-password` itself; the only barrier is the keychain ACL/prompt, **UNVERIFIED** (LC-K3). Option (2)'s TTY check does not stop an agent that allocates a pty, so (2) adds a human typing step, not a boundary |
| D6 | Send-credential read scope | Owner-wide drafts/receipts reachable by ID; only drafts created by that grant | Current exposure is narrow: the credential can fetch only drafts/receipts whose unguessable IDs it already knows, and #77 gives it no listing. **Recommended #77 follow-up (additive, not a #77 blocker):** narrow reads to drafts created by the grant (record the grant ID on submissions), so a stolen credential cannot read drafts the owner submitted from elsewhere |
| D7 | Cascade on compromise | (a) Manual "revoke all" plus a visible grant list; (b) grant tied to the Neon session (impossible: no events); (c) **proxy cascade**: when `hosted/auth_proxy.go` successfully forwards `revoke-other-sessions`, or a successful `email-otp/reset-password` (identity is in the request body), also revoke that account's grants; optionally `sign-out` | **(a) now.** Add **(c)** for `revoke-other-sessions` and password reset only, as a follow-up after the Preview test; do **not** cascade on plain `sign-out` (it would disconnect tools every time the owner signs out). (c) limits: calls made directly to Neon bypass the proxy; the proxy sees a cookie, so it must resolve the account (for example through `get-session`, design not specified here); the app must still say password reset does not revoke tools unless (c) is on |
| D8 | Production profile | Not enabled until the #55 gate; enable C after; use Flow A (blocked) | Not enabled before the owner test; revisit after; Production needs its own approval |
| D9 | Pairing abuse limits and concurrent grants | (a) The section 5.5 numbers: C1 5/10 min, C2 10/h, C3 20/h and 10 creates/h and 3 pending per IP, C4 100/h, 10 pending per environment, 5 active grants per account; (b) tighter (3 / 5 / 10 / 50) at the cost of fewer typo retries; (c) a longer code (10 characters, 20¹⁰ ≈ 1.0×10¹³) with the same counters | **(a)**, tuned by ops after the Preview test. Store `user_code` as a keyed HMAC (unusable without the server key) with a 10 min TTL. Typo budget (C1 expected-absent): 9 well-formed typos per account per hour from C2, shared across sessions and pairings; 4 per 10 minutes per session only if a session claim exists (section 5.5). Attacker success ≈3.9×10⁻¹⁰ per owner pairing for one account, ≤3.9×10⁻⁹ at the global cap (section 5.5). Decide whether C4 lockout (a global availability lever) is acceptable. **Pending-pairing cap as an availability lever:** `POST /pairings` is unauthenticated, so an attacker with spoofed or rotating IPs (C3 is only as good as the IP source, section 2.3 item 9) can fill the 10-pending-per-environment cap and block the owner's pairing for up to 10 minutes (the pairing TTL), repeatedly. Mitigation options: (i) accept and document it (Preview is small; owner retries); (ii) reserve cap slots for requests that pass a cheap proof-of-work or a per-IP token; (iii) make the cap per client-supplied nonce plus IP and expire unredeemed rows faster (for example 2 minutes until first poll); (iv) require a signed-in member to start pairing (changes the flow: the web app issues the code). Recommend **(i) for the Preview test**, with an alert when the cap is hit, and revisit (iii) before Production |
| D10 | Same-user malicious-process and shell-capable-host stance | Accept; require signed binary and a keychain ACL; block real data until LC-K3 is run | Accept for Preview with synthetic data only; require LC-K3 evidence and signing before real data or Production |
| D11 | Provider email change (T9) | (a) State the limitation: the grant works while `email_at_approval` stays in the #78 allowlist, a provider-side email change is not detected, re-pair to update; (b) per-request lookup of the account's current email by `(issuer, subject)` and deny on mismatch (**UNVERIFIED** data source); (c) key #78 membership to `(issuer, subject)` instead of email (a #78 change) | **(a)** now, stated in the UI and runbook; revisit (b) only once a Neon user data source is verified. User-facing: an owner who changes email keeps tools connected until expiry or revocation |
| D12 | Removal then re-add of the same email | (a) State the risk plainly and require the operator runbook step "revoke that member's grants before or at removal"; (b) (a) plus an operator-only "revoke grants by email" procedure (the removed member can no longer sign in to revoke their own); (c) code-enforced: add a per-email added-at stamp to the #78 membership config and reject grants approved before it | **(a) + (b) for Preview**; consider (c) as a #78 follow-up before multi-member Production use. D3's 7-day cap bounds the revival window. User-facing: removing a member does not permanently end their tools if the email is re-added early |

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
| Flow prevents pairing replay, callback/state substitution, cross-environment/account reuse, credential exposure to model | Sections 1 (T1, T3, T10, T13), 5, 6 | Designed; tests specified. Model exposure holds by construction without a shell tool; with a shell-capable host it rests on the **UNVERIFIED** keychain ACL (T1) |
| Removed membership, expired/revoked credentials, account switching with testable outcomes and a bound | Sections 6, 7 | Designed. Provider email change is not detected and removal-then-re-add revives grants (T9, D11, D12) |
| Required new persistent grants/config and separate approvals identified | Section 9 | Done |
| Security review approves, or records a precise blocker and bounded alternatives | Section 10 plus reviewer list below | **Pending.** This document is the review input |
| No live auth changes, accounts, tokens, credentials or hosted submission | Whole document | Satisfied; only reading and synthetic design |

### Reviewer checklist (security review must decide)

1. Is an app-implemented pairing protocol acceptable, versus Flow A's delegated issuance?
2. Is "typed code from own terminal + signed-in approval + fingerprint" (a social control) plus send-only scope adequate against remote phishing?
3. Is a single opaque credential with absolute expiry (no refresh) acceptable?
4. Is the keychain-only policy sufficient without verified ACL behavior (section 2.5)?
5. D6: should a stolen credential be able to fetch any of the owner's drafts it can name (IDs are unguessable), or only those created by the grant?
6. Does the order of checks (401 before 403 before 404) match #77's 403-before-lookup rule?
7. Are the section 5.5 counters, windows, lockouts and the recomputed probabilities adequate, and is the global-cap availability trade-off acceptable?
8. Is the T13 approval-surface design (bearer-header auth with cookies ignored, fresh JWT, retyped code, `frame-ancestors 'none'`, no code in the URL) sufficient?
9. Is accepting the shell-capable-host path (T1) acceptable for Preview with synthetic data, pending LC-K3?
10. Are the stated T9 limits (undetected provider email change; removal-then-re-add revival) acceptable under D11/D12?
