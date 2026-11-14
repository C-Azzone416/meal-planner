# Framebriar application contract

Issue #52 implements app feedback, separate from meal-outcome feedback. This document describes the browser/Go boundary. The [runbook](runbooks/framebriar.md) controls setup and rollout; the [CI design proposal](runbooks/framebriar-ci-design.md) describes the unresolved private-package approval gate.

## Authentication and environment

Every route uses `hosted.RequireUser` with the environment's existing verified Neon identity policy. A valid app session is not a Framebriar invitation. Ownership is derived from verified subject, provider endpoint, environment and app identity, never from a browser owner field. The enrollment record adds a durable random generation; the future account deletion coordinator must call `hosted.RevokeAndEraseFeedback` before deleting the identity.

All mutations require the configured exact Origin. Same-origin browser GETs may instead supply the exact-origin Referer plus `Sec-Fetch-Site: same-origin`. Responses are no-store. Methods, media types and bounded payloads are checked. Mutations share a durable 12-per-minute account limit across server instances. The upstream destination is compiled into the client and does not follow redirects.

Independent target identities are `meal-planner-preview` and `meal-planner`. Each has separate approved origin, registration, encrypted storage and key ring. Configuration is off by default; the current deployment workflows explicitly keep both targets off. Missing setup is unavailable, never implicit enrollment.

## Routes

- `GET /api/feedback/status`: no body. Returns `status`, an opaque `outboxKey`, and the public `environment`, `appId`, `buildSha`, `screenshots` configuration. `expiresAt` is an ISO date when an enrollment deadline exists. `redemption: "pending"` indicates an uncertain attempt. Status may be `connected`, `not-connected`, `expired`, `revoked`, `disabled`, `needs-reconnect`, or `rate-limited`. Missing configuration/storage returns an unavailable error. Disabled configuration returns only disabled status and capture false.
- `POST /api/feedback/connect`: `application/json` body containing only `invitation`. Success returns status, not credentials. The raw invitation is not persisted. A hashed invitation binding and stable attempt ID are committed before redemption; an uncertain answer must be retried with the same invitation within 15 minutes. A definitive refusal clears the attempt. An unresolved older attempt needs operator reconciliation rather than another silent redemption.
- `POST /api/feedback/disconnect`: `application/json` body `{}`. Success means upstream revocation (or already expired/revoked access) and local encrypted-credential erasure completed, then returns status. Lost keys, pending redemption, upstream uncertainty or storage failure blocks a success claim. Delivered reports are unaffected.
- `POST /api/feedback/reports`: multipart with exactly one `report` JSON file and at most one `media` PNG file. Success is `{id, gameId, status: "received"}` only after an upstream ready receipt with matching report/app identity. Saving to IndexedDB is never this receipt.

Errors contain only `{error: SAFE_CODE}` and an appropriate HTTP status. Existing authentication middleware uses its existing safe text 401/503 response. Browser callers must handle non-JSON auth errors. No raw upstream errors, invitations, credentials, identity or report content are logged or returned.

## Report and capture limits

The pinned SDK 0.4.1 prepares a v1 report. Allowed automatic context is deliberately narrower than the SDK: UUID report/session IDs (random per review, not identity), matching app identity, captured 40-character lowercase commit SHA, reviewed screen code, device/input class and ISO capture time. `seed` is null and `breadcrumbs` is empty. No traces, correlation IDs, account IDs, full URLs, query strings, console/network content or automatic recipe/plan data are accepted. Unknown fields are rejected, and validated data is re-encoded before forwarding.

Screen codes: week, recipes, prep, shopping, drafts, cook, house-rules, other. Device class: mobile, tablet, desktop. Input class: mouse, keyboard, touch, mixed, unknown. Reports saved before a deployment retain their captured SHA; they are not relabeled with the newer server SHA.

Limits are 2,000 note characters, 16 KiB JSON, 3 MiB PNG, and 3 MiB + 16 KiB + 4 KiB for the entire multipart request. PNG signature/dimensions/hash/declared size are checked. These limits are deliberately below the Vercel request envelope; the actual hosted envelope remains part of the approved rollout smoke check.

Capture is opt-in per review and independently removable at build time. The preview is a simplified sanitized drawing of visible content, not a pixel-perfect screenshot. It omits controls, media, navigation/header, marked private regions, sensitive/unknown routes and uncertain/clipped/hidden nodes before rasterization. It uses viewport coordinates rather than document-scroll transforms. An unsafe/failed capture retains text-only feedback. The user must review the result before Send.

## Durable delivery and cleanup

The SDK queue is explicitly scoped by verified opaque account generation and environment. It holds at most 10 reports and 32 MiB media, until receipt or clearing, with stable IDs and persisted retry delays. The UI distinguishes Saved on this device from Received. Ambiguous failed saves preserve immutable notes/ID for retry. Abort/await precedes cleanup so an in-progress save cannot recreate discarded content.

Expired/revoked/disconnected scopes are still inspected and can be cleared, but delivery remains stopped. Sign-out normally clears that account's unsent content after warning. If ownership/storage cannot be verified, Delete reports failure; sign-out may proceed only after a separate explicit warning that unsent feedback may remain and will not be sent while signed out. It never falsely claims deletion or clears another account's current queue.

## Synthetic verification

`npm run --prefix web test:browser` starts the test-only Go fixture and Vite server. PostgreSQL command-line tools and pinned Playwright Chromium/WebKit browsers must already be installed. The fixture uses a new private temporary PostgreSQL cluster, real JWT verification, real handlers/store/protocol client and synthetic-only provider/Framebriar transports. No production binary imports it. Shutdown is explicit/graceful and stops the temporary database.

The matrix covers desktop Chromium, phone Chromium and iPhone/WebKit: connect/cancel/reopen/Send/receipt/disconnect; failed delivery and discard confirmation; PNG positive/negative privacy controls including clipped content; shared account enrollment across devices and confirmed account switching. Browser HTTPS traffic is blocked during these tests. Unit tests cover additional storage, queue, expiry, malformed receipt, rate-limit, auth-outage, save/clear and scope races. Real SQL integration is opt-in with `FEEDBACK_TEST_DATABASE_URL` and `go test -tags=integration -race -run '^TestFeedbackPostgres$' ./hosted` against a disposable database only.

Passing synthetic tests does not establish private-registry access, real Vercel packaging, service registration, live provider setup or delivery receipt. Those remain explicit release gates.
