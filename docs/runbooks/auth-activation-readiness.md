# Auth activation readiness (#74)

Planning packet for verifying controlled auth activation. **Nothing here has been executed, and this document authorizes nothing.** It does not create accounts, credentials or provider/Vercel settings, deploy, migrate or activate auth. Every action in part 3 needs Caroline's approval first, at the time of the action. Procedures are in [authentication.md](authentication.md) and [hosted-deploy.md](hosted-deploy.md); this page only maps #74 onto them.

Public-doc rule: sanitized status only. No account IDs, endpoint IDs, provider URLs, emails, tokens, cookies or codes. Evidence marked **private** stays in the owner's private notes.

Preview and Production are separated throughout. A Preview result never counts as Production evidence, and Production stays **disabled** until its own approval (part 3B).

State read from GitHub on 2026-11-11/12 (`gh pr list` / `gh pr view`); re-read it before relying on it.

## 1. Source reconciliation (repo-only)

### 1.1 What is on develop and what is in flight

| Item | State | Head | Effect on Preview |
|---|---|---|---|
| develop | `ef7e561`, includes main `e73fab6` (#56 auth hardening) via #76 | `ef7e561` | Baseline. The 27-commit gap in #74's starting point is closed. |
| #78 multi-member membership (#53) | open, not draft. QA and judge APPROVE posted at `ad540ad`. Required CI green on the PR. | `ad540ad` | Adds `AUTH_ALLOWED_EMAILS` (hosted + `tools/deploy`). Hosted code, so a merge triggers a Preview deploy. |
| #75 local MCP restore (#23) | open, **draft**. QA and judge APPROVE at `f1b6455`; judge's conditional acceptance listed as a small round 2 before merge. Current head `91fd5c6` is not covered by either verdict. | `91fd5c6` | Also edits `hosted/auth_*`, `hosted/recipes.go`, `.github/workflows/*`, `tools/deploy`, `web/src`, `contracts/openapi.yaml`. Triggers Preview. Its body says it is based on main `e73fab6` (same tree as develop `ef7e561` per #77's description) and mentions an older 27-commit comparison; re-read its comparison against current develop before merge. |
| #77 hosted draft contract (#72) | open, not draft. QA REQUEST CHANGES at last read (round 3 pushed). | `f5c9e36` | Touches `contracts/` only, which is a Preview trigger (see 1.2). Design plus synthetic fixtures; no hosted behavior change. |
| #79 local sign-in design (#73) | open, draft | `d5bcbc8` | `docs/` only. No Preview. |
| #80 DB readiness runbook (#61) | open, draft | `c06eba3` | `docs/` only. No Preview. |

Re-check each head with `gh pr view <n> --json headRefOid,reviewDecision,isDraft` before use. A changed head returns to QA.

### 1.2 What a Preview deploy requires today

- `.github/scripts/classify.sh`: `hosted_preview=true` only if `web/*`, `api/*`, `hosted/*`, `vercel.json` (hosted_go) or `contracts/*` changed **and** `PREVIEW_TRUSTED=true` **and** `PREVIEW_ENABLED=true`. Docs-only changes never select Preview; CLI-only changes (`cmd/*`, `internal/localstore/*`, `internal/mcpadapter/*`) never select it either.
- `.github/workflows/ci.yml`: `PREVIEW_TRUSTED=true` only for `push` to `refs/heads/develop`. The `preview` job additionally requires `hosted_preview == 'true'` and no failed or cancelled needed job, and calls `preview.yml` with the pushed `github.sha`. Pull requests never deploy a Preview.
- [hosted-deploy.md](hosted-deploy.md) §2: "Pull requests run checks only and never deploy a Preview. Nothing to run by hand." The summary carries the SHA, URL and manifest (`preview-manifest-<sha>-<run attempt>`).
- **There is no documented manual Preview deploy.** `preview.yml` is `workflow_call` only, with no `workflow_dispatch`. Whether to add one, or to redeploy by pushing a hosted-code commit to develop, is an **owner decision that is pending**. This runbook does not choose it.

Consequence for env changes: Vercel environment changes apply to new deployments only (UNVERIFIED here; see §6). With no manual redeploy, the next qualifying develop push is the only documented way to pick up a new Preview Secret.

### 1.3 Which merges must happen first

The Preview owner test must run on a deployed Preview built from a develop SHA that contains, at minimum:

1. `ef7e561` (already on develop), reviewed #56 auth hardening.
2. **#78**, so the second test identity for isolation can be a configured member. Without it only one identity per environment exists (`AUTH_ALLOWED_EMAIL`), and the isolation check cannot pass.
3. **#75**, but only after the judge's round 2 is done, the comparison against current develop is re-read (rebase if needed), and QA then judge approve the final head with green CI. It is required for the local MCP half of the test (#23-remediated surface). Merge order #78 then #75 is recommended, since both edit `hosted/auth_*` and #75 should be rebased over #78's membership policy. If #75 lands first, #78 needs re-verification on the new base.
4. #77, #79 and #80 are **not** prerequisites for the auth readiness check. #77 is needed for the draft-flow half of Caroline's #55 test pack; #80 is needed for the recipe-persistence readiness (#61). Docs-only merges (#79, #80) do not redeploy Preview.

Each merge into develop needs the usual flow (QA first, then judge on the same head, then green CI) and is a PM/owner action, not part of this task.

### 1.4 Verifying the deployed Preview SHA

Do not assume the alias or a past deploy matches the candidate.

1. Choose the target SHA: `git rev-parse origin/develop` after the merges in 1.3. Confirm `git merge-base --is-ancestor <#78 merge> <sha>` and the same for #75.
2. Find the `ci.yml` push run for that SHA on develop. Its `preview` job must have succeeded, not been skipped. A skipped `preview` means the classifier did not select it or `PREVIEW_ENABLED` was unset.
3. Read the Preview summary: the deployed SHA, unique URL and, if `PREVIEW_ALIAS` is set, the "Preview alias" section confirming the deployment ID.
4. Cross-check with the manifest artifact `preview-manifest-<sha>-<run attempt>`.
5. Known gap: `/api/health` does not report the build SHA (hosted-deploy.md follow-up). The alias check compares deployment IDs; it does not prove the *code* beyond the run's own SHA record. Owner step: in the private Vercel project view, confirm the Preview deployment's source commit equals the target SHA (**live owner evidence**).
6. If the alias serves a different SHA than the target (see the "Alias stuck" and move-forward rules), stop and do not test.

## 2. Checklist: #74 acceptance boxes

Legend: **S** = covered by synthetic tests in this repo (not live proof); **L** = needs live owner evidence, kept private; **P** = Preview, **X** = Production (separate evidence, only after 3B approval). Test names are from `hosted/*_test.go` and `api/*/index_test.go` at ef7e561. Tests prefixed `#78` exist only after #78 merges.

| # | #74 acceptance box | Proving step | Synthetic coverage | Live owner evidence |
|---|---|---|---|---|
| A1 | Exact environment, release, provider configuration verified independently; Production stays disabled | §1.4 for the SHA; 3A.1–3A.8 for Preview config; part 3B for Production | `TestAuthConfigDisabledWithoutBaseURL`, `…WithoutIdentityPolicy`, `…WithoutEnvironmentBinding`, `TestAuthConfigRejectsProviderEndpointMismatch`, `TestAuthPolicyFailsClosedAcrossRuntimeEntryPoints`, `TestHandlerFailsClosedWithoutEnvironment` (api); #78: `TestAuthMembershipEnvironmentIsolation` | L: Preview settings exist (Secrets are write-only, so existence only; the guard reports `[SENSITIVE]` markers). L: `GET /api/auth-config` on Production returns `enabled:false`. |
| A2 | Intended account exists before first-password reset, same provider subject afterward, verified email via approved flow, no duplicate | authentication.md "First-account acceptance gate"; 3A.4 | None possible: needs a real provider. `TestAuthProxyForwardsCanonicalEmailAndSDKFields` and `TestVerifyBearerAcceptsConfiguredVerifiedIdentity`/`…NeonCamelCaseVerificationClaim` cover only our handling of claims. | L: user audit before/after, provider user ID unchanged (compare privately), `emailVerified` true in provider and token. Currently **UNVERIFIED**. |
| A3 | Sign-in, first password, recovery, sign-out, refresh and direct protected routes pass; unapproved identity gets generic response, no account, no email | 4. rehearsal steps R3–R7, R9 | `TestAuthProxyForwardsAllowedRouteAndRehomesCookie`, `TestAuthProxyReturnsGenericProviderErrorsForEmailOutsidePolicy`, `TestAuthProxySilentlyDropsBlockedEmailDelivery`, `TestAuthProxyRejectsLegacyPasswordlessRoutes`, `TestAuthorizeAcceptsOnlyConfiguredIdentity`, `TestAuthorizeRejectsMissingBearer`; web route tests for direct links (frontend suite) | L: real sign-in, code email, password set, recovery, sign-out, refresh, direct URL. L: unapproved identity: no email received, no new user row. |
| A4 | Signed webhook compatibility incl. protected/header/JWKS key-ID match; unsigned, stale, wrong-event, wrong-endpoint, wrongly signed fail | 3A.5; R8 | `TestAuthBeforeCreateAcceptsNeonDocumentedJWSHeader`, `TestAuthBeforeCreateJWSRetainsSignatureAndEnvelopeRestrictions`, `TestAuthBeforeCreateRejectsBadSignature`, `…RejectsStaleAndFutureTimestamps`, `…RejectsEnvelopeMismatch`, `…RejectsWrongEndpoint`, `…FailsClosedOnJWKSFailure`, `…AllowsOnlyConfiguredEmail`, `TestSigningKeyRejectsAmbiguousJWKS`, `TestAuthJWKSRefusesRedirectsBeforeDestinationContact`, `TestAuthJOSETypeProfile`; api: `TestHandler*` in `api/auth/before-create` | L: a real provider-signed event accepted for the approved identity and rejected for another, with the provider's `kid` equal to the JWKS `kid`. Replays of unsigned/stale/wrong-event/wrong-endpoint against Preview need a crafted request: owner step, private. |
| A5 | Old cookie sessions cannot mint tokens after reset; previously issued bearer tokens rejected by actual expiry; no immediate-revocation claim | authentication.md "Password-reset session and token checks"; R10 | `TestVerifyBearerNumericDateSemantics`, `TestNumericDateRepresentationBounds`. Cookie-session revocation is provider behavior: no synthetic test. | L: old session fails lookup right after reset; retained token's `exp` recorded (privately) and token rejected at/after `exp`. |
| A6 | Missing, expired, wrong-environment, unverified, unauthorized tokens fail on protected APIs | R11 | `TestRequireUserRejectsMissingBearer`, `TestVerifyBearerBindsIssuerAndAudienceToAuthOrigin`, `TestVerifyBearerRejectsIdentityPolicyFailures`, `TestVerifyBearerRejectsBadSignature`, `TestVerifyBearerNumericDateSemantics`, `TestVerifyBearerFailsClosedOnMissingPolicy`; recipes API tests in `api/recipes` and `hosted/recipes_test.go` | L: against deployed Preview, missing/expired/other-env/unverified/wrong-email tokens get 401/403. A genuine other-environment token needs a second auth environment (owner decision; else note the gap). |
| A7 | Preview acceptance does not substitute for Production; rollback/disable and exact blockers recorded | Parts 3B, 5 | n/a | L: separate Production evidence after 3B approval. |
| A8 | Private evidence stays private; public issue has sanitized status only | Evidence rules below | n/a | Owner keeps notes private; public comment lists only pass/fail, release SHA and PR numbers. |

Additional rows required by the PM brief:

| Topic | Detail |
|---|---|
| **`AUTH_ALLOWED_EMAILS` (#78)** | Private server-side JSON-array Secret, 1–100 entries, 32 KiB limit. Entries exact lowercase ASCII, no padding, local part ≤64 bytes, total ≤254 bytes. `AUTH_ALLOWED_EMAIL` (legacy) is used only when the new setting is absent; both set, or a malformed list, fails closed (generic 503/500, readiness disabled). `tools/deploy/check-pulled-env.sh` accepts it only as a redacted Secret with exactly one membership format. Synthetic coverage (from the #78 PR body): `TestAuthMultipleMembersAcrossAuthorizationPoints`, `TestAuthMembershipEnvironmentProxyAndWebhook`, `TestAuthMembershipConfigurationFailsClosedEverywhere`, `TestAuthMembershipSupportedInputs`, `TestAuthMembershipInvalidConfiguration`, `TestAuthMembershipDoesNotProvisionAccountsOrBroadenSignup`, `TestAuthMembershipLegacyEnvironmentMigrationAndRollback`. Verify names against the merged code. Live: both approved members sign in; an unlisted identity is denied. |
| **Removal bound (#78)** | A removed member is denied on the **next protected request evaluated with the updated deployment's configuration**, after the redeploy is serving and older deployment URLs/aliases carrying the old membership are retired. Not provider-wide revocation; the provider account and any already-issued token remain until expiry elsewhere. Test: `TestAuthMembershipRemovalDeniesPreviouslyIssuedTokensOnNextRequest`. Live: R12. Rolling back to the single-member setting with an obsolete list can restore a removed account, so record the current list privately before every change. |

Evidence rules: record pass/fail, date, deployed SHA (not full URLs) and the checking person. Never record emails, user IDs, endpoint IDs, codes, cookies, tokens, webhook URLs or bypass values in the repo, PRs or issues.

## 3. Approval packet

**Every action below needs Caroline's explicit approval before it is performed, at the time of the action.** Approval of this PR or of #74's planning scope is not approval of any action. Performer is the owner (Caroline) unless stated; agents never hold provider or Vercel credentials for these steps. "Verify" results are recorded privately.

### 3A. Preview (protected; nothing here touches Production)

| # | Action | Performer | Prerequisite | Rollback | Verification |
|---|---|---|---|---|---|
| 3A.1 | **Add the Preview membership Secret** (`AUTH_ALLOWED_EMAILS`, or keep `AUTH_ALLOWED_EMAIL` if #78 is not merged) to the Vercel **Preview** environment only, as a Secret (write-only). | Owner, in Vercel (private) | #78 merged and reviewed on the target SHA (1.3); the exact member list written privately; Preview project verified separate from Production | Restore the previous Secret value or remove the new one; redeploy (3A.7). Keep the prior list privately first; a stale list can re-admit a removed member. | `check-pulled-env.sh` in the Preview `pull` job passes with `[SENSITIVE]` markers and exactly one membership format; after redeploy `GET /api/auth-config` is `enabled:true`; allowed identity signs in, unlisted is denied (R6, R12). |
| 3A.2 | **Approve a second test identity** for isolation (a separate email the owner controls; no real user data). Provision it in the Preview auth branch only after 3A.4's procedure is approved. | Owner approves identity; owner provisions | Owner picks the identity; the identity is added to the Preview list (3A.1) | Remove it from the list (redeploy); delete the Preview-branch user only with separate approval. Do not delete or recreate the existing Preview user. | Provider user list shows exactly the approved accounts before/after; second identity sees none of the first's data (R9); both can sign in, an unlisted third cannot. |
| 3A.3 | **Confirm Preview and Production independence**: distinct auth branch, distinct `NEON_AUTH_BASE_URL`, endpoint ID, allowlist; local profile for the test holds Preview values only and has **no Production credentials and no fallback to Production** (#74/#25). | Owner | none | n/a (read-only check) | Compare the Preview and Production settings privately (never copy); `TestAuthMembershipEnvironmentIsolation` covers code isolation. Local profile inspected by the owner: only Preview URL and credentials present. |
| 3A.4 | **First-account provisioning and verified email** for each approved identity: audit users, create the account through the approved administrative path, request the first-password code, compare provider user IDs, check `emailVerified`. | Owner | Webhook live and blocking, **Sign-up with Email** disabled, Email OTP and trusted Preview domain set (authentication.md "Preview provider setup order"); exact provisioning action reviewed. The admin-create path is **UNVERIFIED** (does it fire the webhook, set `emailVerified`, support first-password?). | Disable or delete the created Preview user only with separate approval; re-audit. Never open public signup to recover. | authentication.md "First-account acceptance gate", items 1–6. Any unexpected or duplicate user, or unverified identity, blocks readiness. |
| 3A.5 | **Webhook signing-key and JWKS match**: confirm the provider signs `user.before_create`, the protected-header `kid` equals a `kid` in the branch JWKS, the endpoint ID matches `NEON_AUTH_ENDPOINT_ID`. | Owner (provider console) | Webhook saved and pointing at `POST /api/auth/before-create`; code deployed | If the key ID mismatches or the key rotated: leave the webhook as is (it fails closed with 503/401), fix on the provider side, retest. Do not relax verification. | R8. Handler loads branch JWKS (no 5xx on a signed event); unsigned/stale/wrong-event/wrong-endpoint/badly signed requests rejected. Compare `kid` values privately; only "match: yes/no" is public. |
| 3A.6 | **Keep deployment protection on** and **public email signup disabled** (two settings, checked together). If protection blocks webhook delivery, the only approved fallback is a dedicated Protection Bypass for Automation secret in Neon's private webhook URL (authentication.md step 1); it is a separate approval and never reuses the CI bypass. | Owner | n/a | If either setting is found changed: stop, restore it, audit users for unexpected accounts, then continue. | Anonymous browser request to the Preview URL is blocked by Vercel protection; provider **Sign-up with Email** shows disabled; an unlisted email gets a generic response with no user row (R7). |
| 3A.7 | **Redeploy Preview** for any env change. Env changes apply to new deployments, not running ones. | Owner decides the method (pending): push a hosted-code commit to develop, or add a reviewed manual trigger | Part 1.2 decision; env change saved | Redeploy the previous good develop SHA (also a develop push, subject to the alias move-forward rule) or restore the previous env and redeploy. | 1.4 steps, then `GET /api/auth-config` on the new deployment, then A3. Old deployment URLs holding the old membership are retired or confirmed unreachable (removal bound). |
| 3A.8 | **Enable auth in Preview**: set the complete trio (`NEON_AUTH_BASE_URL`, `NEON_AUTH_ENDPOINT_ID`, allowlist) last, after 3A.1–3A.6. There is no separate switch. | Owner | All of the above approved and done; user audit clean | Unset `NEON_AUTH_BASE_URL` and redeploy (see part 5). | `GET /api/auth-config` `enabled:true` on Preview only; Production still `enabled:false`. |

### 3B. Production (disabled by default; do not start)

Status: **DISABLED. No Production step is authorized or planned by this PR.** Production auth stays off until a separate, later approval for controlled owner-only activation, which also depends on #55's gates (Caroline's personal Preview test).

| # | Action | Performer | Prerequisite | Rollback | Verification |
|---|---|---|---|---|---|
| 3B.0 | Confirm Production is still disabled. | Owner | none | n/a | `GET /api/auth-config` on Production returns `enabled:false`; the three auth names are not set in Production. |
| 3B.1 | Independent Production provider setup (authentication.md "Production provider preparation"): enable on the Production branch, blocking webhook, JWKS, endpoint binding, Sign-up with Email off, Email OTP, reset-session revocation, Production checklist items. | Owner | Caroline's separate Production approval; reviewed code already deployed to Production; Preview results are *not* a substitute | Disable auth on the Production branch; unset the trio; audit users | Same checks as 3A.5, 3A.6, A4 but against Production only, with its own evidence. |
| 3B.2 | Provision the intended single Production account, then set the complete trio for owner-only verification. | Owner | 3B.1 done; user audit before/after; approval for the exact action | Unset `NEON_AUTH_BASE_URL`; redeploy via the Production release workflow (two environment approvals, see hosted-deploy.md §3) | Full release gate in authentication.md. Production is "operational" only when every row passes on the deployed Production release. |

Never copy Preview users, endpoint IDs, allowlists, webhook URLs or bypass secrets into Production.

## 4. Owner rehearsal plan (protected Preview, already-approved account)

This is a **readiness rehearsal of auth**, distinct from Caroline's personal #55 MCP test and does not replace it. #25 coordinates the hands-on local-MCP-to-Preview draft flow; #55 blocks Production shipment until Caroline completes that test and confirms the result. This rehearsal only shows that auth works well enough for that test to be meaningful. Use an account already approved and existing in Preview; do not create or reset anything outside an approved action.

Preconditions: target SHA deployed and verified (1.4); 3A approvals done; protection on; signup off.

| Step | Do | Pass criterion |
|---|---|---|
| R1 | Verify deployed Preview SHA (1.4). | SHA equals target; includes #78 (and #75 if the MCP half is tested). |
| R2 | Anonymous request to Preview URL. | Blocked by Vercel protection. |
| R3 | `GET /api/auth-config` | `enabled:true`; response shows only the same-origin proxy path, no provider URL. |
| R4 | Direct visit to a protected route while signed out. | Redirected/denied, no data. |
| R5 | Existing approved account: sign in (and first-password / recovery flow if that account has not set a password). | Success; provider user ID unchanged versus the pre-audit; no new user row. |
| R6 | Refresh, browser back/forward, direct protected URL. | Session persists; route preserved. |
| R7 | Unlisted identity: request sign-in/recovery. | Generic response, no email delivered, no user row. |
| R8 | Webhook: provider-signed event for approved identity accepted; owner-crafted unsigned/stale/wrong-event/wrong-endpoint/bad-signature requests rejected. | As stated; `kid` match recorded yes/no. |
| R9 | Second identity (3A.2) signs in; shows none of the first identity's data; first sees none of second's. | Isolation holds in both directions. Uses manual recipes API; the draft flow needs #77 and the hosted bridge, which are not part of this rehearsal. |
| R10 | Password reset on the rehearsal account: old cookie session cannot get a fresh token; retained pre-reset token recorded with `exp` and rejected at/after it. | Both outcomes recorded separately; no immediate bearer revocation claimed. |
| R11 | Protected API with missing, expired, other-environment (if available), unverified (if available), and wrong-email tokens. | All rejected (401/403). |
| R12 | Remove the second identity from the list, redeploy, retry. | Denied on the next protected request on the new deployment; old URLs retired. |
| R13 | Sign out. | Session ends; protected route denied. |

Cross-reference: after R1–R13 pass and are recorded privately, Caroline's #55 personal test (#25) can proceed on the same verified Preview. Nothing in this plan marks #55 complete.

## 5. Rollback and disable

Disable (any environment): remove or unset `NEON_AUTH_BASE_URL` in that environment and redeploy. `GET /api/auth-config` then returns `enabled:false`, the sign-in form stays disabled and protected APIs fail closed. This does not delete provider users.

Preview rollback, in order of least change:

1. Membership wrong or too broad: restore the previous list (recorded privately), redeploy. Removal takes effect on the next protected request on the new deployment (removal bound). Retire old deployment URLs/aliases that carry the old list.
2. Bad provider setting (webhook, key, endpoint): the handler already fails closed; unset `NEON_AUTH_BASE_URL`, redeploy, fix the provider side, retest from R1.
3. Unexpected or duplicate user found: stop. Unset `NEON_AUTH_BASE_URL`, confirm Sign-up with Email is disabled, audit, and ask the owner before any user deletion.
4. Exposed bypass secret: revoke and replace immediately; do not reuse a CI credential.
5. Bad code on the alias: redeploy the previous good develop SHA; the alias moves forward only (hosted-deploy.md). If it is stuck, use the token-safe alias repair in that runbook with approval.

Production: stays disabled by default. If ever activated and a check fails, unset `NEON_AUTH_BASE_URL`, redeploy through the Production workflow, then confirm `enabled:false` publicly. Do not roll the database back.

### Exact remaining blockers (as of this document)

1. #78 not merged. No second identity or multi-member membership on develop.
2. #75 is draft; judge's round 2 was required; the current head is not covered by a verdict and needs re-review (and a rebase if the comparison differs). The local MCP half of the test depends on it.
3. No documented manual Preview deploy; owner decision pending. Until then a Preview rebuild requires a hosted-code push to develop with `PREVIEW_ENABLED=true`.
4. `/api/health` does not report the build SHA, so the deployed SHA needs owner confirmation in Vercel.
5. First-account bootstrap is **UNVERIFIED** (admin create behavior, webhook firing, `emailVerified`, first-password on a provisioned account).
6. Live provider checks (A2–A6) have no evidence; synthetic tests are not provider compatibility.
7. No approved second test identity; no approved Preview membership Secret; Caroline's approvals for 3A not given.
8. The cross-environment token test (A6) needs a second auth environment, which is not planned. Record the gap or decide.
9. #77 (QA changes requested) and the hosted draft bridge are required for the draft half of #55; not auth blockers.
10. Production: everything in 3B; also gated by #55's owner test.

## 6. Provider claims and sources

Provider claims in this document come from the existing runbooks and were **not re-fetched during this task** (no web access was used). Treat them as unconfirmed until re-read on the access date recorded when the owner verifies.

| Claim | Source | Status |
|---|---|---|
| Webhook headers/JWS envelope (`alg` EdDSA, `typ` JWS, `kid`) | https://neon.com/docs/auth/guides/webhooks#request-headers | Cited in authentication.md; re-check before use: UNVERIFIED today |
| Admin user creation under `/auth/users`; auth config is per branch | https://neon.com/docs/auth/guides/manage-auth-api | Cited in authentication.md; UNVERIFIED today |
| Revoke sessions on password reset | https://neon.com/docs/auth/guides/password-reset | Cited in authentication.md; UNVERIFIED today |
| 15-minute access-token lifetime | https://neon.com/docs/auth/guides/plugins/jwt | Cited in authentication.md; UNVERIFIED today |
| Production checklist | https://neon.com/docs/auth/production-checklist | Cited in authentication.md; UNVERIFIED today |
| Vercel env changes apply to new deployments only; Secrets write-only | Vercel environment-variables docs | UNVERIFIED (not fetched). hosted-deploy.md records Secret behavior from run evidence. |
| Protection Bypass for Automation covers the whole project | hosted-deploy.md "Deployment Protection bypass secret" | Repo doc; provider docs UNVERIFIED today |
