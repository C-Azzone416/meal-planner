# Synthetic-only draft Preview activation

Status: preparation only. This runbook records Caroline's November 16, 2026
decision to accept the one shared draft runtime role for a short, controlled
Preview trial containing synthetic data only. It does not authorize a live
database change, provider setting, credential, deployment, feature-gate change,
MCP registration or Production action.

The accepted residual is narrow: the shared `MEAL_DRAFTS_DATABASE_URL` role can
read all owners' active draft rows and perform the complete pairing/proposal/
review workflow at the SQL privilege layer. Application authentication,
same-origin checks and owner predicates provide tenant separation; PostgreSQL
RLS does not. This is acceptable only for the bounded synthetic trial. Real
recipes, personal content, persistent use or broader membership require split
roles/DSNs, RLS, or narrow owned procedures and a new security review.

## Pre-activation prerequisites

### Vercel Hobby throttle decision (November 16, 2026)

Caroline chose to remain on Vercel Hobby and accepted a deliberately bounded
availability risk for this fake-data-only trial. Hobby allows one rate-limit
rule per project and a maximum ten-minute fixed window. Its counters are per
region, not a global distributed request cap. This design must not be described
as sufficient for personal recipes, an open public service, paid workloads or
traffic whose availability matters.

Use the one Hobby rule as a shared budget for both unauthenticated pairing
entrypoints. It is Preview-environment, exact-host, POST-only, keyed by the
provider-derived source IP, and matches only `/api/draft-pairings` and
`/api/draft-pairings/poll`. The fixed-window limit is 150 requests per ten
minutes, with a 429 before function invocation and no bypass. A normal client
polling every five seconds for the full ten-minute pairing lifetime uses at
most 120 polls plus one create, leaving 29 requests for retry and timing
headroom. Multiple clients behind one address share that budget.

The shared rule intentionally cannot give creation and polling independent
budgets. A source can spend all 150 requests on creates, and distributed or
multi-region traffic can exceed one regional counter. Application-side pairing
creation therefore also fails closed, per exact Preview environment and issuer,
at 20 active open pairings, 60 pairings created in the last hour, or 500
retained pairing rows. Creation is serialized with a transaction advisory lock
before counting and inserting. The bounded 429 response does not disclose which
cap was reached. The private retention inspector reports all three cap states.

These caps bound persistent fake-data growth; they do not make the service
available under attack. An attacker able to fill a cap can deny new pairing for
at least the ten-minute expiry window, and a full retained cap requires an
explicit operator cleanup decision. Random well-formed poll attempts still
perform an indexed database lookup. Existing 8 KiB bodies, strict JSON and
field validation, ten-minute pairing expiry, five-second valid-pairing poll
slowdown, bounded handler/database deadlines, two database connections per warm
instance, high-entropy pairing credentials and the 15-second function ceiling
reduce work but are not a global distributed-abuse defense. Never raise a cap
or add a bypass reactively; keep the routes closed and investigate.

The setup receipt last verified this project in a personal Hobby account on
November 1. A read-only recheck on November 16 found no local
`.vercel/project.json`; the local Vercel CLI reported logged out, and the GitHub
CLI credential was not usable. No credential was requested or inspected, and
no provider call or setting change was made. A fresh authenticated read-only
observation must still prove the exact project is Hobby before publication.

`tools/deploy/draft-edge-plan-adapter.mjs` is a read-only, authenticated plan
probe for a future protected job. It verifies the exact Vercel project and
account, accepts only a full team response, emits only canonical bounded
Hobby/Pro/Enterprise capability evidence, and never emits the token or provider
response. Its CLI accepts the token only on standard input, never through argv,
an environment variable or a file. It has not been run against Vercel. A fresh
result less than 15 minutes old must confirm Hobby before publishing the chosen
edge policy.

The normalized verifier now accepts exactly this one-rule Hobby policy, a Hobby
plan observation, no bypass, no intersecting rule, no Production effect and the
two exact function limits. A future authenticated firewall adapter remains a
prerequisite: plan verification does not authorize publishing the rule,
deploying, enabling a source/runtime gate, cleaning retained rows or touching
Production.

The code-only function prerequisite is now prepared: `vercel.json` assigns a
15-second `maxDuration` to exactly the create and poll entrypoints, ahead of the
general Go fallback, and `inspectDraftFunctionConfig` rejects drift or broader
overrides. This is source evidence only. A future inert Preview deployment must
still be checked with authenticated `vercel inspect <deployment> --json` and
show `timeout: 15` for both built functions before activation.

The source items must land and pass QA and Security Judge, and the package item
must produce its own evidence, before requesting any live approval:

1. Add a separate protected readiness mode for `draft: true`. It must bind the
   exact Preview Vercel project, Neon branch, source SHA, PostgreSQL 18 baseline,
   `draftRuntimeRole`, current recipe role, all five migration hashes and all
   seven draft relation/grant snapshots. The existing closed mode must continue
   to reject every `MEAL_DRAFT_*` setting.
2. Add a clean authenticated provider adapter for the exact edge observation in
   `tools/deploy/draft-edge-readiness.mjs`. A JSON file or digest supplied by the
   target is not provider evidence.
3. The activation runtime verifier must accept exactly Preview plus
   `MEAL_DRAFT_PAIRING_ENABLED=true`, `MEAL_DRAFT_WORKFLOW_ENABLED=true`, the
   two exact origin settings
   `MEAL_DRAFT_PAIRING_ORIGIN=https://athome-meals-dev.vercel.app` and
   `MEAL_DRAFT_INGEST_ORIGIN=https://athome-meals-dev.vercel.app`, one bounded
   `MEAL_DRAFT_MEMBERSHIP_EPOCH`, and sensitive placeholders for
   `MEAL_DRAFTS_DATABASE_URL` and the existing `MEAL_RECIPES_DATABASE_URL`. The
   workflow reuses the legacy-named `MEAL_DRAFT_INGEST_ORIGIN`; that setting does
   not enable direct ingest. The verifier must reject
   `MEAL_DRAFT_INGEST_ENABLED`, Production, any other origin, literal secret
   values, unknown draft variables and a reused database role.
4. The activation source change may open `draftPairingSourceEnabled` and
   `draftWorkflowSourceEnabled` only. `draftIngestSourceEnabled` and the legacy
   direct-ingest route remain closed. Runtime flags remain absent during the
   first deployment of those source bytes.
5. The distribution-signed local package still needs Keychain store/retrieve,
   denial/cancellation/lock and other-binary evidence. The ad-hoc Deny canary is
   not final package acceptance and cannot be upgraded by documentation.

## Action-time approval packets

Each packet names the exact Preview target, reviewed source SHA and restoration
plan. Approval of one packet does not authorize the next.

### 1. Preview database

- Independently verify the Neon project, branch, database and PostgreSQL major;
  reject any Production or ambiguous target.
- Verify exact recipe migration 001 is already present and matches its reviewed
  hash before applying the additive draft migrations.
- Apply only exact reviewed migrations 004, 005 and 006 with the migration
  owner. Create the shared draft runtime login and apply only
  `tools/deploy/draft-runtime-grants.sql`. Create a distinct catalog inspector
  with LOGIN and CONNECT only.
- Place connection values only through protected secret input. Never put a DSN
  or password in argv, shell history, logs, repository files or public workflow
  artifacts.
- Keep every draft source and runtime gate closed. Run the clean catalog
  inspection; its LOGIN-and-CONNECT-only identity cannot run the retention
  query. Run `tools/deploy/inspect-draft-retention-readiness.sql` separately as
  the migration owner in an owner-approved clean session pinned to the reviewed
  file hash. Force `psql -X -qAt -v ON_ERROR_STOP=1`, supply `as_of` from the
  trusted runner clock, retain no shell history or connection value, and emit
  only the bounded aggregate. This is explicit high-impact use of the migration
  owner, not authority for arbitrary SQL. Require zero integrity blockers and
  zero owners at retained caps.
- Restoration: do not drop additive tables. Keep gates closed, rotate/revoke
  issued credentials, and disable the new logins if rollback is authorized.
  Destructive schema cleanup is a separate decision.

### 2. Preview edge and function settings

- Publish one Preview-only, exact-host, POST-only fixed-window rule matching
  only `/api/draft-pairings` and `/api/draft-pairings/poll`, at 150 requests per
  provider-derived source IP per ten minutes. It must return 429 before function
  invocation. Do not add a bypass or another intersecting rule.
- Set a 15-second ceiling only for the create and poll pairing functions. Verify
  no intersecting broad rule, bypass or Production effect.
- Retrieve the resulting state through the authenticated clean adapter and
  verify its observation less than 15 minutes before deployment.
- Restoration: remove only this exact Preview rule and the function overrides and
  re-observe the provider state. The function ceiling is not proof of an upload
  deadline; failure to verify request-body behavior blocks activation.

### 3. Preview secrets and inert deployment

- Add the Preview-only `MEAL_DRAFTS_DATABASE_URL` Secret and retain the separate
  `MEAL_RECIPES_DATABASE_URL` Secret. Add
  `MEAL_DRAFT_PAIRING_ORIGIN=https://athome-meals-dev.vercel.app`,
  `MEAL_DRAFT_INGEST_ORIGIN=https://athome-meals-dev.vercel.app`, and a newly
  generated `MEAL_DRAFT_MEMBERSHIP_EPOCH` through protected configuration. The
  legacy-named ingest origin is required by the proposal workflow; it does not
  authorize `MEAL_DRAFT_INGEST_ENABLED`. Do not add any draft `*_ENABLED`
  variable yet, and do not reuse a Production, migration-owner,
  catalog-inspector or recipe credential.
- Deploy the reviewed activation-source SHA while runtime flags remain absent.
  Confirm every draft route remains disabled and the stable alias still serves
  the accepted non-draft application.
- Restoration: remove/rotate only the newly added Preview values and redeploy
  the last accepted closed-gate SHA. No persistent local MCP registration is
  part of this packet.

### 4. Short synthetic activation

- Add only `MEAL_DRAFT_PAIRING_ENABLED=true` and
  `MEAL_DRAFT_WORKFLOW_ENABLED=true`; keep legacy ingest absent. Deploy the exact
  reviewed SHA through the protected Preview workflow after fresh runtime,
  catalog and edge observations.
- Use only designated synthetic accounts and conspicuously synthetic recipe
  content. Do not paste personal recipes, credentials or other real data.
- Run one bounded acceptance window. Normal teardown is mandatory: revoke the
  test grant; delete the exact trial credential through the reviewed Keychain
  adapter and verify it is absent; and record that no persistent MCP registration
  was added. If an unexpected registration exists, remove that exact entry and
  verify absence. A Keychain access prompt is handed to Caroline and is never
  approved automatically.
- Remove the two runtime flags, the three remaining draft settings and the
  `MEAL_DRAFTS_DATABASE_URL` Preview Secret. Redeploy the pre-recorded exact last
  accepted SHA whose pairing, workflow and ingest source gates are all `false`.
  Verify the deployed SHA, all three closed source gates, disabled route behavior
  and absent runtime configuration through authenticated evidence.
- Remove the one exact Preview edge rule and the two pairing function-duration
  overrides. Obtain a fresh authenticated provider observation proving their
  absence and no Production effect.
- While the inspector still has LOGIN, rerun the closed runtime and catalog
  observations, verify through the authenticated provider configuration that no
  `MEAL_DRAFT_*` setting or `MEAL_DRAFTS_DATABASE_URL` Secret remains, and run
  the private aggregate inspection once more. Then use the exact verified
  Preview migration-owner session to disable LOGIN for the shared draft runtime
  and catalog-inspector roles, rotate their credentials, and verify both
  `rolcanlogin` values are false. The recipe credential is outside this trial and
  remains unchanged. Additive tables and grants remain durable; deleting them is
  not part of teardown.

## Required acceptance evidence

1. Account A can pair only after Caroline's browser approval; an unauthenticated
   caller and synthetic account B cannot read or change A's pairing, intent,
   hosted draft or saved recipe.
2. Explicit revocation rejects the old grant. Membership removal/re-add is tested
   only with the documented manual epoch rotation; sign-out or password reset is
   not reported as revocation.
3. Repeated and interrupted sends preserve one intent/draft. Account switching,
   a changed local revision and stale browser edits fail safely.
4. Caroline approves the exact pending intent, reviews the hosted draft, supplies
   the explicit projection, saves once, reloads and sees the same synthetic
   recipe. Sending alone never creates or approves a recipe.
5. The private post-run inspection reports no integrity blocker or owner at a
   retained cap. Logs, process arguments, public artifacts and local ledgers
   contain no credential or recipe content.
6. The grant is revoked; the Keychain trial item is deleted; and no persistent
   MCP registration remains. The exact previously accepted closed-source SHA is
   restored with all three source gates false. Every Preview `MEAL_DRAFT_*`
   setting and the draft DSN Secret are removed; the one edge rule and two
   function overrides are absent with no Production effect; and both
   draft-specific database logins are disabled/rotated. Authenticated source,
   runtime, edge and catalog observations plus the final private aggregate are
   retained as evidence. The additive schema and grants may remain, but no
   active credential, source gate, provider rule or runtime setting may silently
   extend the trial.

Stop immediately on an unexpected owner/Keychain prompt, target ambiguity,
Production effect, provider-evidence mismatch, role/schema drift, literal secret
exposure, missing edge rule, failed owner isolation, duplicate durable object or
inability to restore closed mode. Preserve evidence and request a new explicit
decision; do not weaken a check to finish the trial.
