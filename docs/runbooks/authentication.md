# Authentication runbook

The web application fails closed unless its Neon Auth environment is explicitly configured. Account creation is restricted at the provider boundary by a signed blocking webhook. The browser also presents its short-lived Neon token to same-origin `GET /api/auth/authorize` before showing protected routes, and protected product APIs must use `hosted.RequireUser` so a valid session for a different identity cannot cross the application boundary.

Do not put real account identifiers, provider dashboard links, tokens, or endpoint IDs in this public repository. Store each environment's values only in its private provider settings. Preview and production must use distinct auth projects or branches and distinct allowlist values.

## Code deployment and auth activation

A **code-only Production deployment** can ship the reviewed auth routes and UI while keeping `NEON_AUTH_BASE_URL` unset in Production. Verify that `GET /api/auth-config` returns `enabled: false`, sign-in stays disabled, and protected APIs fail closed. A merged release, successful deployment, or green CI does not establish operational Production authentication.

**Controlled Production activation** requires reviewed, independent Production provider setup, approved administrative provisioning of the intended single account, and separate approval for the configuration/security changes. Keep Production auth disabled until those prerequisites are in place. Then enable the complete configuration only for approved owner-only verification, with public email signup still disabled and the single-identity allowlist enforced. The complete environment configuration enables the sign-in form; there is no separate application activation switch. Do not copy Preview settings or activate auth merely because the code deployment succeeded.

**Operational Production auth is ready** only after the first-account acceptance gate and release checks below pass against the deployed Production release. The app's first-password flow needs the complete configuration, so those checks happen during controlled activation, not before configuration can be enabled. An enabled sign-in form alone is not evidence that login is ready.

## Configuration

Configure these server-side values in the matching deployment environment:

- `NEON_AUTH_BASE_URL`: the server-only HTTPS Neon Auth endpoint used by the signed webhook and same-origin auth proxy.
- `AUTH_ALLOWED_EMAIL`: the single exact email allowed in that environment. This value remains server-only.
- `NEON_AUTH_ENDPOINT_ID`: required expected endpoint ID from webhook events. It binds signup decisions to the intended auth environment.

Bearer-token issuer and audience are always derived from the origin of `NEON_AUTH_BASE_URL`, matching Neon Auth's documented defaults. They are intentionally not configurable independently.

Runtime validates the full managed-provider authority and rejects ports (including an empty `:`), userinfo, Unicode authorities, malformed DNS labels, encoded paths, queries and fragments. It canonicalizes ASCII host case, outer URL/endpoint whitespace and trailing slashes before deriving both proxy routing and the expected issuer. A custom ASCII database path is supported. The deployment pull guard is intentionally narrower: literal values must use lowercase `/neondb/auth`; redacted Secret values are validated by this runtime policy when used.

Authentication JSON is checked before typed decoding. Relevant claim/header/identity field names are case-sensitive, duplicate decoded keys are rejected at every level, and malformed UTF-8/unpaired surrogate escapes or null security fields are rejected. Nesting is bounded to 64 levels; ordinary unknown, noncritical provider extensions, including nullable extras, remain supported. Email identity comparison checks the original email bytes before normalization and rejects any non-ASCII byte, including Unicode whitespace, case-fold aliases and malformed UTF-8. It then permits ASCII case differences and trims only outer space, tab, carriage return and line feed (SP/HT/CR/LF); other control bytes, internal whitespace and Unicode padding never identify the allowed account. This applies to bearer claims, signed before-create webhook payloads and every email proxy route. The configured allowlist itself must remain exact lowercase ASCII with no padding; accepted proxy requests forward that configured identity. Both documented verification spellings (`emailVerified`, `email_verified`) remain supported and must both be true if both are present.

Both JOSE profiles require EdDSA and a nonempty `kid`, but their token types are context-specific:

- Bearer access tokens accept omitted `typ` or exactly `"JWT"` (case-sensitive), never `"JWS"`.
- Detached webhook signatures accept omitted `typ` for compatibility or exactly `"JWS"`, never `"JWT"`. Neon's [webhook request-header documentation](https://neon.com/docs/auth/guides/webhooks#request-headers) shows a protected header that decodes to `{"alg":"EdDSA","typ":"JWS","kid":"01dec52b"}`. That signature envelope applies to all webhook events, including `user.before_create`; the example key ID and abbreviated signature are illustrative, not usable credentials. Tests preserve the documented algorithm/type and substitute a synthetic signing key and matching ID. The default webhook fixture uses `"JWS"` so the full identity, timestamp, endpoint, event and signature matrix exercises this provider format.

Empty, null, differently cased or padded types, other media types and token kinds such as `"refresh+jwt"` are rejected. The `cty` header must be omitted: neither path implements nested JWTs (`cty: "JWT"`) or alternate declared payload formats. Explicit empty or null declarations are not equivalent to omission. No critical extensions or unencoded payloads are supported (`crit` must be omitted; `b64` may be omitted or true). Header checks do not normalize or reserialize the original signed bytes. Optional bearer `iat` must be a NumericDate and `jti` a string; `iat` is not a second activation time. Unsupported profiles fail closed. Synthetic conformance tests do not establish live provider compatibility, which remains part of controlled activation testing.

Signing keys come only from `NEON_AUTH_BASE_URL` plus `/.well-known/jwks.json` in Production. The injectable `JWKSURL` is for deterministic tests, not an environment override. All JWKS redirects fail closed, including same-origin, same-path and HTTPS-to-HTTPS redirects; the HTTP client does not contact their destinations. Injected `*http.Client` instances are copied to enforce this policy without changing the caller's client or its transport/TLS settings and timeout. A reported final response URL must also match the requested URL. Generic `HTTPDoer` test doubles are trusted in-process dependencies and may omit the response request. Direct successful JWKS responses remain size-bounded. Key rotation may contain distinct key IDs, but a selected key ID must be unique and its optional algorithm/use/operations metadata must permit EdDSA signature verification.


The provider URL remains server-only. `GET /api/auth-config` gives the browser the fixed same-origin `/api/auth-proxy` path. That narrow proxy forwards only password sign-in, password-reset email-code, session, session-revocation, and sign-out methods used by this app; it filters incoming cookies to Neon's auth-cookie prefix and rewrites provider cookies onto the application origin. It rejects the legacy passwordless sign-in routes. This avoids third-party-cookie failures without exposing an open reverse proxy or forwarding Vercel protection cookies.

Meal Planner uses email and password for routine sign-in. The intended flow is for an existing invited account to prove inbox control with a short-lived email code when setting its first password or recovering it. The reset operation must add or replace the credential on the same provider user without creating a second account. The first-account procedure remains **UNVERIFIED** until the acceptance gate below passes. Public email self-signup stays disabled.

Keep `NEON_AUTH_BASE_URL` unset until the rest of the provider configuration is complete. The public config endpoint returns `enabled: false` unless the base URL, server-only allowlist, and endpoint binding are all present, so the sign-in form stays disabled for partial configurations.

The current release intentionally permits one identity per environment. When invitations expand beyond that, replace `AUTH_ALLOWED_EMAIL` with a private server-side membership store and explicit `invited`, `active`, and `revoked` states. Both the blocking creation webhook and every protected product API must consult that same membership policy. Do not ship a client-side list or broaden access with an email-domain match.

## Preview provider setup order

Provider and deployment-protection changes require separate action-time approval. Perform them only after the code containing the webhook endpoint has been reviewed and deployed. This sequence is for a protected Preview; its results do not approve or configure Production.

Managed Better Auth creates a public branch auth endpoint as soon as it is enabled. Neon does not currently offer an atomic enable-and-configure operation, so treat signup as open until the blocking webhook has been saved. Keep the application auth URL unset throughout this bootstrap period.

1. Make the webhook route reachable without removing Preview Deployment Protection. If Vercel protection blocks webhook delivery, create a dedicated Protection Bypass for Automation secret and place it only in Neon's private webhook URL as the `x-vercel-protection-bypass` query parameter. The secret bypasses protection for the whole Vercel project, not only this route, so never commit, log, screenshot, or reuse it. Preserve normal Vercel login protection for browser visits.
2. Enable Managed Better Auth only on the intended Preview branch. Capture the branch auth base URL and endpoint ID privately.
3. Immediately enable only the blocking `user.before_create` event and point it at `POST /api/auth/before-create`. Until the deployment values below are configured, the handler deliberately returns `503`, causing signup to fail closed.
4. Audit the branch auth user list before proceeding. Stop if an unexpected account was created during the brief enable-to-webhook interval. Disable provider **Sign-up with Email** and keep it disabled, including during first-account provisioning. Keep email sign-in enabled for existing accounts.
5. Configure Email OTP, the trusted Preview domain, and **Revoke sessions on password reset** before password-flow testing. Add the server-only environment values above to Preview, using the environment's intended account and never copying Preview values into Production. Redeploy the protected Preview for controlled verification.
6. Confirm the provider signs webhook events, the handler can load the branch JWKS, and unsigned, stale, wrong-event, and wrong-endpoint requests remain rejected.
7. Verify that the signed webhook accepts the intended identity and rejects another identity without creating an account row. Do not reopen public signup to produce this evidence; if it cannot be obtained safely, record the blocker.
8. Confirm the intended Preview user already exists through the first-account acceptance gate below. Password reset deliberately does not create a user and must not be used as an invitation mechanism.
9. Confirm provider **Sign-up with Email** remains disabled before testing the password UI.
10. Confirm **Revoke sessions on password reset** is enabled. Verify old cookie-session rejection separately from already-issued bearer-token expiry as described below.

If the automation-bypass secret is exposed, revoke it immediately and replace it before continuing. Do not reuse an existing CI bypass credential. A public-domain protection exception is a broader fallback because it exposes the entire Preview alias; use one only with separate approval.

## First-account acceptance gate: UNVERIFIED

Neon's [Auth management API documentation](https://neon.com/docs/auth/guides/manage-auth-api) lists administrative user creation under the selected branch's `/auth/users` endpoint. That documents an administrative capability, not a proven or approved Meal Planner invitation/bootstrap procedure. Do not assume it triggers the blocking webhook, sets `emailVerified`, or creates a usable first-password account. Public email signup must remain disabled; do not temporarily open it or weaken the webhook to create the first account.

Verify the following for the intended environment under an approved, controlled setup. Rehearse in protected Preview when a suitable approved test account exists; do not delete or recreate the existing Preview user or provision another account/environment merely to satisfy this gate. The intended single-account Production administrative bootstrap can itself be the controlled check, with public signup remaining disabled. Independently verify the Production results before calling login ready. Record results privately, with no account identifiers, cookies, codes, or tokens in repository artifacts:

- Review the exact administrative provisioning action and intended branch/account before executing it. Obtain the required approval; this runbook is not authorization to create an account or change security settings.
- Audit the branch's users before provisioning and immediately afterward. Confirm that only the intended account was added and stop on any unexpected user.
- Confirm the account exists before requesting a first-password code. Prove that the email-code password flow works for that administratively provisioned account while public signup remains disabled.
- Compare the provider user ID before and after password setup and subsequent sign-in. It must remain the same, with no duplicate account row.
- Inspect `emailVerified` in the provider user and the issued token. Verify inbox control through the approved provider flow and confirm the application accepts only a verified identity. Do not assume password reset sets this flag or manually mark an unverified account as verified to pass the gate.
- Audit users again after password setup and negative tests. An unexpected or duplicate user, unverified identity, failed first-password flow, or unsupported provisioning path blocks operational readiness.

These are acceptance criteria, not completed checks. Missing provider setup, approved account provisioning, or activation approval keeps Production disabled. If an acceptance check fails during controlled activation, stop, resolve the blocker, and keep or return Production auth to the disabled state with the appropriate approval. Do not call login ready.

## Password-reset session and token checks

Neon's [password-reset guide](https://neon.com/docs/auth/guides/password-reset) describes **Revoke sessions on password reset** for both link and Email OTP resets. Immediately after a reset, every old cookie session must fail a fresh provider session lookup and must not mint a fresh JWT. Test an old session independently of the newly established password session; a cached browser view alone is not evidence of revocation.

Meal Planner's protected APIs validate stateless bearer tokens against JWKS and token claims; they do not look up the provider session on every request. An already-issued, otherwise-valid JWT can remain usable until its actual `exp`, even when its cookie session has been revoked. Neon's [JWT guide](https://neon.com/docs/auth/guides/plugins/jwt) currently documents a 15-minute access-token lifetime. Inspect the issued token's actual expiry privately rather than assuming a fixed delay from the reset. Do not promise immediate bearer-token revocation.

For acceptance, retain a pre-reset token securely, record its expiry without logging the token, reset the password, and verify both outcomes separately: the old cookie session cannot obtain a new token immediately, and the retained token is rejected by `GET /api/auth/authorize` and protected APIs at or after its `exp`. Remove sensitive test material afterward. Immediate rejection of all previously issued bearer tokens would require a separate design change and is not provided by the current stateless verifier.

## Production provider preparation

Repeat the provider review for the intended **Production branch**, independently of Preview. [Neon's management API](https://neon.com/docs/auth/guides/manage-auth-api) treats auth configuration as branch-specific. Verify the Production endpoint/branch binding, JWKS, signed blocking webhook, server-only allowlist, email-signup restriction, Email OTP, and reset-session revocation setting. Audit Production users before and after provisioning and verification. Never promote Preview users, endpoint IDs, allowlist values, webhook URLs, or bypass secrets by copying them into Production.

For a publicly reachable Production deployment, use the ordinary HTTPS webhook route after the reviewed code is deployed; no automation-bypass secret is needed. Preserve existing deployment protection wherever it applies. Only if protection blocks webhook delivery, obtain separate approval for the exact required change; Preview bypass approval does not cover Production. Keep the Production app auth URL unset during provider preparation and any enable-to-webhook interval, and stop if the user audit reveals an unexpected account.

Review Neon's [Production checklist](https://neon.com/docs/auth/production-checklist) for the Production branch: trusted Production domains, email delivery/provider configuration, the application name, email verification, and disabled localhost access. Do not change those settings or configure credentials without the appropriate approval. Document any unresolved delivery or verification requirement as a blocker.

Only add the complete Production configuration for approved, controlled owner-only verification once provider setup and approved administrative account provisioning are in place. Verify the first-password flow, same user ID, verified email, and all release checks during that controlled activation before describing login as ready.

## Release gate

Before considering login ready, verify all of the following against the intended environment and deployed release under the approved controlled setup. Protected Preview rehearsal does not replace independent Production verification or its separate approval:

- the first-account acceptance gate has passed for the environment, the intended user exists before requesting a password code, and no unexpected or duplicate account rows exist;
- provider email self-signup is disabled and session revocation on password reset is enabled;
- the allowed identity can request a password code, set a password, sign in with it, revisit a direct protected URL, and sign out;
- a different identity receives a generic response, no email is delivered, and no account row is created;
- every old cookie session fails a fresh lookup immediately after password reset and cannot mint a new JWT;
- a retained pre-reset bearer token is rejected at or after its actual `exp`, without claiming it was revoked immediately;
- an unsigned, stale, wrong-event, wrong-endpoint, or incorrectly signed webhook is rejected;
- a protected API rejects a missing token, a valid token from another auth environment, an unverified identity, and the wrong email;
- browser history, refreshes, and direct links preserve the expected route;
- Production remains disabled until its separate provider setup, allowlist, approved administrative account provisioning, and controlled-activation approval are in place; operational-ready status waits for all acceptance checks, and Preview success alone does not satisfy them.

Do not weaken the provider webhook to a client-side email check. The UI intentionally does not know the allowlisted account.
