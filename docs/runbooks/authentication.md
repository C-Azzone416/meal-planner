# Authentication runbook

The web preview fails closed unless its Neon Auth environment is explicitly configured. Account creation is restricted at the provider boundary by a signed blocking webhook. The browser also presents its short-lived Neon token to same-origin `GET /api/auth/authorize` before showing protected routes, and protected product APIs must use `hosted.RequireUser` so a valid session for a different identity cannot cross the application boundary.

Do not put real account identifiers, provider dashboard links, tokens, or endpoint IDs in this public repository. Store each environment's values only in its private provider settings. Preview and production must use distinct auth projects or branches and distinct allowlist values.

## Configuration

Configure these server-side values in the matching deployment environment:

- `NEON_AUTH_BASE_URL`: the server-only HTTPS Neon Auth endpoint used by the signed webhook and same-origin auth proxy.
- `AUTH_ALLOWED_EMAIL`: the single exact email allowed in that environment. This value remains server-only.
- `NEON_AUTH_ENDPOINT_ID`: required expected endpoint ID from webhook events. It binds signup decisions to the intended auth environment.

Bearer-token issuer and audience are always derived from the origin of `NEON_AUTH_BASE_URL`, matching Neon Auth's documented defaults. They are intentionally not configurable independently.

The provider URL remains server-only. `GET /api/auth-config` gives the browser the fixed same-origin `/api/auth-proxy` path. That narrow proxy forwards only the email-code, session, token, and sign-out methods used by this app; it filters incoming cookies to Neon's auth-cookie prefix and rewrites provider cookies onto the application origin. This avoids third-party-cookie failures without exposing an open reverse proxy or forwarding Vercel protection cookies.

Meal Planner intentionally uses passwordless email OTP: the user proves inbox control with a fresh code on each sign-in. It does not share Packed Smart's separate code, setup email, password creation, and password recovery lifecycle.

Keep `NEON_AUTH_BASE_URL` unset until the rest of the provider configuration is complete. The public config endpoint returns `enabled: false` unless the base URL, server-only allowlist, and endpoint binding are all present, so the sign-in form stays disabled for partial configurations.

The current release intentionally permits one identity per environment. When invitations expand beyond that, replace `AUTH_ALLOWED_EMAIL` with a private server-side membership store and explicit `invited`, `active`, and `revoked` states. Both the blocking creation webhook and every protected product API must consult that same membership policy. Do not ship a client-side list or broaden access with an email-domain match.

## Provider setup order

Provider and deployment-protection changes require separate action-time approval. Perform them only after the code containing the webhook endpoint has been reviewed and deployed.

Managed Better Auth creates a public branch auth endpoint as soon as it is enabled. Neon does not currently offer an atomic enable-and-configure operation, so treat signup as open until the blocking webhook has been saved. Keep the application auth URL unset throughout this bootstrap period.

1. Make the webhook route reachable without removing Preview Deployment Protection. For Vercel, create a dedicated Protection Bypass for Automation secret and place it only in Neon's private webhook URL as the `x-vercel-protection-bypass` query parameter. The secret bypasses protection for the whole Vercel project, not only this route, so never commit, log, screenshot, or reuse it. Preserve normal Vercel login protection for browser visits.
2. Enable Managed Better Auth only on the intended Preview branch. Capture the branch auth base URL and endpoint ID privately.
3. Immediately enable only the blocking `user.before_create` event and point it at `POST /api/auth/before-create`. Until the deployment values below are configured, the handler deliberately returns `503`, causing signup to fail closed.
4. Audit the branch auth user list before proceeding. Stop if an unexpected account was created during the brief enable-to-webhook interval.
5. Add the server-only environment values above to Preview, using the environment's intended account and never copying Preview values into Production. Redeploy the protected Preview.
6. Confirm the provider signs webhook events, the handler can load the branch JWKS, and unsigned, stale, wrong-event, and wrong-endpoint requests remain rejected.
7. Configure Email OTP and the trusted Preview domain. Enable the application auth URL only after the signed webhook has accepted the intended identity and rejected another identity without creating an account row.

If the automation-bypass secret is exposed, revoke it immediately and replace it before continuing. Do not reuse an existing CI bypass credential. A public-domain protection exception is a broader fallback because it exposes the entire Preview alias; use one only with separate approval.

## Release gate

Before considering login ready, verify all of the following in Preview:

- the allowed identity can request and verify a code, revisit a direct protected URL, and sign out;
- a different identity receives a generic denial and no account row is created;
- an unsigned, stale, wrong-event, wrong-endpoint, or incorrectly signed webhook is rejected;
- a protected API rejects a missing token, a valid token from another auth environment, an unverified identity, and the wrong email;
- browser history, refreshes, and direct links preserve the expected route;
- production remains disabled until its separate provider configuration and allowlist are reviewed.

Do not weaken the provider webhook to a client-side email check. The UI intentionally does not know the allowlisted account.
