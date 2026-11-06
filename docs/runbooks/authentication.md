# Authentication runbook

The web preview fails closed unless its Neon Auth environment is explicitly configured. Account creation is restricted at the provider boundary by a signed blocking webhook. The browser also presents its short-lived Neon token to same-origin `GET /api/auth/authorize` before showing protected routes, and protected product APIs must use `hosted.RequireUser` so a valid session for a different identity cannot cross the application boundary.

Do not put real account identifiers, provider dashboard links, tokens, or endpoint IDs in this public repository. Store each environment's values only in its private provider settings. Preview and production must use distinct auth projects or branches and distinct allowlist values.

## Configuration

Configure these server-side values in the matching deployment environment:

- `NEON_AUTH_BASE_URL`: the HTTPS Neon Auth endpoint. The browser receives this URL from `GET /api/auth-config`; no credential is exposed.
- `AUTH_ALLOWED_EMAIL`: the single exact email allowed in that environment. This value remains server-only.
- `NEON_AUTH_ENDPOINT_ID`: required expected endpoint ID from webhook events. It binds signup decisions to the intended auth environment.

Bearer-token issuer and audience are always derived from the origin of `NEON_AUTH_BASE_URL`, matching Neon Auth's documented defaults. They are intentionally not configurable independently.

Keep `NEON_AUTH_BASE_URL` unset until the rest of the provider configuration is complete. The public config endpoint returns `enabled: false` unless the base URL, server-only allowlist, and endpoint binding are all present, so the sign-in form stays disabled for partial configurations.

## Provider setup order

Provider and deployment-protection changes require separate action-time approval. Perform them only after the code containing the webhook endpoint has been reviewed and deployed.

1. Configure Email OTP in the environment's Neon Auth project.
2. Set a blocking `user.before_create` webhook to the deployment's `POST /api/auth/before-create` endpoint.
3. Confirm the provider signs webhook events and that its JWKS endpoint is reachable at the auth base URL.
4. Add the server-only environment values above, using the environment's intended account and never copying preview values into production.
5. Make the webhook URL reachable by Neon. If Preview Deployment Protection blocks it, use a narrowly scoped public preview alias exception only after approval; never put an automation bypass secret in application code or provider configuration.
6. Enable `NEON_AUTH_BASE_URL` last, then test from a clean browser session.

## Release gate

Before considering login ready, verify all of the following in Preview:

- the allowed identity can request and verify a code, revisit a direct protected URL, and sign out;
- a different identity receives a generic denial and no account row is created;
- an unsigned, stale, wrong-event, wrong-endpoint, or incorrectly signed webhook is rejected;
- a protected API rejects a missing token, a valid token from another auth environment, an unverified identity, and the wrong email;
- browser history, refreshes, and direct links preserve the expected route;
- production remains disabled until its separate provider configuration and allowlist are reviewed.

Do not weaken the provider webhook to a client-side email check. The UI intentionally does not know the allowlisted account.
