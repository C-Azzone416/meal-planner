# Framebriar feedback

Implementation reference for issue #52. The [HTTP/privacy contract](../framebriar-contract.md) and [protected CI proposal](framebriar-ci-design.md) provide the implementation and approval details. Neither this change nor this runbook authorizes credentials, registrations, migrations, deployments, invitations or live reports. Both deployment workflows currently build feedback **off**. Production must remain off until its own reviewed authentication/setup, privacy tests and separately approved synthetic receipt check pass.

## Settings and isolation

Use separate Preview and Production registrations, exact stable HTTPS origins, database roles/storage and encryption key rings. Never borrow travel-packing registrations or secrets. Arbitrary deployment URLs are not allowed origins. No wildcards, paths, query strings or credentials in an origin.

Public build/runtime setting names:

- `FRAMEBRIAR_ENABLED`: `0` (default) or `1`.
- `FRAMEBRIAR_ENVIRONMENT`: `preview` or `production`.
- `FRAMEBRIAR_ALLOWED_ORIGIN`: one exact HTTPS origin, independently approved per environment.
- `FRAMEBRIAR_APP_ID`: `meal-planner-preview` or `meal-planner`, matching the environment.
- `FRAMEBRIAR_BUILD_SHA`: exact 40-character lowercase deployed commit, explicitly supplied from workflow `inputs.sha`, never ambient Vercel metadata.
- `FRAMEBRIAR_SCREENSHOTS`: `0` by default, independent `1` opt-in capability. Users still select each screenshot explicitly.

Runtime-only secrets: `FRAMEBRIAR_DATABASE_URL` and `FRAMEBRIAR_CREDENTIAL_KEYS` (JSON object with `current` key ID and `keys` map from IDs to base64-encoded 32-byte keys; see backend validation). Store these as Vercel Secret type, separately in each environment. The pulled-env guard accepts only `[SENSITIVE]` placeholders for these names; literal values fail with names-only diagnostics. Pulled env files are always deleted and never uploaded. No prefix-wide Framebriar allowlist exists.

The Vite build reads only the six public properties in `tools/feedback/config.mjs`, disables automatic `VITE_*` exposure and emits `framebriar-build.json`. Runtime browser origin must equal the compiled origin. Incomplete/mismatched config disables feedback. `prepare-build.mjs` generates public Go provenance constants; the server checks the runtime SHA, environment and app against these immutable values. A default local Go build has empty constants and fails closed. Private database/key material is never generated into source or artifacts.

## Package-reader prerequisite and safe installation

The SDK is pinned to `@masra91/framebriar` **0.4.1** with lockfile integrity, fetched normally from GitHub Packages. No package archive or implementation is vendored here. Package metadata/integrity were confirmed against the existing reference integration. The fresh unauthenticated registry request returned **401**; repository/environment secret-name inventory also confirmed `FRAMEBRIAR_PACKAGE_READ_TOKEN` is absent. Authenticated clean installation is a pending owner-controlled gate, not a completed check. Local implementation tests may use an existing locally installed SDK; that does not prove clean CI access.

An owner must separately approve and provision read-only package access as `FRAMEBRIAR_PACKAGE_READ_TOKEN`. It is not a Vercel runtime setting. Never log or paste its value. Workflows pass it as `NODE_AUTH_TOKEN` only to the `npm ci --ignore-scripts` step, with a temporary npm cache deleted at step exit. Only package manifests are copied into a temporary install directory; a fixed GitHub scope registry config is generated there. `env -i` removes inherited npm configuration and HOME credentials, with empty user/global config files. Only installed node_modules are moved back to the checkout. No cache/upload action includes npm files. No lifecycle, Vite, Go, test, browser, artifact or deploy step receives it. Pull-request code receives no package-reader secret: until a separately reviewed untrusted-build dependency-access design exists, private-SDK PR checks fail explicitly rather than expose access or silently skip tests.

After installation, a fresh shell runs `bash tools/feedback/postinstall.sh`. It refuses any package/deploy token and validates lifecycle metadata. The current reviewed optional `fsevents@2.3.3` install script is not needed for Linux builds and is not run. No blanket rebuild is permitted. A new lifecycle package fails until its narrow necessity and command are reviewed. Vercel's install command runs only this token-free check, not a second network installation. For a local prebuilt test, first perform the same approved isolated `npm ci --prefix web --ignore-scripts` install with an ephemeral cache, exit that credential-bearing shell, and only then run Vercel/build commands. Missing access is a blocker, not permission to reuse a broader token.

## Offline gates

- `node --test tools/feedback/config.test.mjs`: allowlist, bad/missing settings, target mismatch and fail-closed behavior.
- `bash tools/deploy/check-pulled-env_test.sh`: names-only validation including runtime-secret placeholders, lookalikes, malformed values and no secret logging.
- `bash tools/feedback/check-targets.sh`: real Preview/Production JavaScript builds, each enabled and disabled with capture on/off, exact SHA/app manifest and bundled provenance checks; synthetic secrets and fake-service markers must be absent. Screenshots-off uses an alternate module and excludes active capture code.
- Deployment workflows generate Go provenance before building, check the real static output and scan the materialized feedback server binaries before packaging. Build/deploy credentials remain in separate jobs.
- Frontend, Go, integration and privacy/phone suites remain independent required gates. Offline passing evidence is not a live receipt, clean private-package installation, production rollout or proof of browser-specific capture behavior.

## Owner-controlled setup and migration gate

Check existing service registrations before proposing new ones. Obtain explicit approval for each environment's registration/origin, least-privilege package reader, runtime storage/key setup and private invitation. Preserve all unrelated apps and settings. Apply only the reviewed forward-compatible `migrations/001_framebriar.sql` migration using the environment's owner-controlled migrator connection. Do not grant schema-management privileges to the runtime role. Migration is intentionally not run by the build job; verify it separately before any approved enablement/deployment. Keep schema changes backward compatible for rollback.

Enabling Preview requires a reviewed workflow/config change from its current hard-off state, matching runtime config, migration evidence and private enrollment. Production is a separate decision; Preview approval is not Production approval. Do not expand authentication membership as part of enrollment.

## Rotation, loss and revocation

Use different encryption keys per environment. Add a new current key while retaining old decrypt keys, re-encrypt existing records through an approved bounded maintenance procedure, verify no rows depend on the old key, then separately approve retirement. Never remove the old key first. On key loss, stop feedback in that environment, revoke affected upstream enrollment where possible and require fresh invitations after approved cleanup. Do not pretend encrypted data was recovered. A database restore must not resurrect disconnected/revoked enrollment; reconcile revocation before enabling delivery.

Disconnect invokes upstream revoke and erases enrollment; surface failures truthfully. A lost key or uncertain redemption older than the 15-minute retry window requires operator reconciliation and blocks a false disconnect/deletion success. Future account deletion must use the documented backend revoke/erase hook before deleting the identity and must prevent a recreated account inheriting prior enrollment. Neither operation erases delivered Framebriar reports.

## Unsent reports, privacy and retention

Reports are account- and environment-scoped on the sending device. Only an upstream receipt means **Received**; local persistence means **Saved on this device**. Retry uses a stable report ID. Delete unsent reports clears only that account's local outbox. Sign-out warns before discarding pending local content, stops delivery and clears that account's unsent material. Confirmed account changes stop delivery and clear the prior verified scope. If storage/ownership cannot be verified, explicit Delete fails truthfully; sign-out offers a separate retention warning and proceeds only after confirmation without claiming deletion. Storage failure keeps the visible draft rather than claiming it was saved. SDK 0.4.1 enforces at most 10 queued reports and 32 MiB queued media. It has no automatic age expiry (`queueDays: null`): unsent reports remain until delivered or explicitly cleared. This app additionally limits each report to 2,000 note characters, 16 KiB JSON and a 3 MiB PNG. Delivered-data retention must be confirmed with the service owner; SDK protocol retention hints are not evidence of deployed policy.

The image is a simplified sanitized viewport drawing, with controls/media/header/navigation and uncertain clipped regions omitted before rasterization. Screenshots start unselected and require a visible preview before Send. They may contain private recipes, plans or household preferences. No automatic recipe content, account identity, full URL/query, console or network history is attached. Auth/invitation regions must be excluded before rasterization. Text-only feedback remains available when capture is disabled or unsafe.

Delivered data lives in Framebriar under that service's retention/deletion policy; deleting local unsent reports, disconnecting or deleting this app's account does not delete delivered data. Confirm the service owner's retention/deletion process before onboarding users. Do not invent a retention duration or promise downstream erasure.

## Disable, rollback and synthetic delivery evidence

Disable only the affected environment with `FRAMEBRIAR_ENABLED=0` and deploy an approved matching disabled artifact. For capture-only incidents set `FRAMEBRIAR_SCREENSHOTS=0` and rebuild; changing only a runtime variable cannot remove already-published capture JavaScript. Server-side disable must stop submissions even for an old client. Preserve pending local drafts unless the user chooses deletion/sign-out; never mark them received. Roll back to a reviewed disabled artifact without destructively rolling back the compatible storage schema.

Only after separate approval, use a private invitation and synthetic non-sensitive text/image to connect, review, explicitly Send, receive an upstream receipt and disconnect on stable Preview desktop and phone. Record only commit, environment/app, timestamp, gate results and a non-sensitive receipt reference. Never publish payload, image, invitation, account identity or credentials. Test Production independently only after its separate approval. No live receipt or rollout has been performed by the implementation work.
