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
entrypoints across every hostname for this project's Preview environment. It
is POST-only, keyed by the provider-derived source IP, and matches only
`/api/draft-pairings` and
`/api/draft-pairings/poll`. The fixed-window limit is 150 requests per ten
minutes, with a 429 before function invocation and no bypass. A normal client
polling every five seconds for the full ten-minute pairing lifetime uses at
most 120 polls plus one create, leaving 29 requests for retry and timing
headroom. Multiple clients behind one address share that budget.

Vercel's rule reference supports `Environment`, `Hostname`, `Method` and
`Request Path` conditions, and the exact-value `Is any of` path operator. Use
that operator for the two paths, with Preview environment and POST method
combined by AND. Deliberately omit a Hostname condition so Vercel's generated
deployment URLs cannot bypass the only edge rule. Both paths and every Preview
hostname consequently consume the same threshold and counter key. The Go
handlers separately require the stable alias and exact unescaped route, so an
alternate hostname or normalized/trailing-slash path is rejected before any
repository call. That defense-in-depth rejection still incurs a function
invocation; it does not replace all-host edge coverage.

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

IP rotation evades an IP-keyed threshold, and traffic reaching more than one
Vercel region receives a separate counter in each region. At the configured
rate a single stable IP can be allowed up to 21,600 matching requests per day
per region; about 47 such IPs can collectively exceed Hobby's included one
million allowed requests. The WAF is therefore not a spend cap. The creation
caps bound pairing-row storage, but random poll traffic can still consume
function and database allowance without creating a row. For this fake-data
trial the accepted failure is temporary unavailability or exhausted free
allowance, not silent expansion to real data or a paid plan.

The setup receipt last verified this project in a personal Hobby account on
November 1. A read-only recheck on November 16 found no local
`.vercel/project.json`; the local Vercel CLI reported logged out, and the GitHub
CLI credential was not usable. No credential was requested or inspected, and
no provider call or setting change was made. A fresh authenticated read-only
observation must still prove the exact project is Hobby before publication.

`tools/deploy/draft-edge-plan-adapter.mjs` is a read-only, authenticated plan
probe for a future protected job. It verifies the exact Vercel project and
account, accepts only a full team response, emits only canonical bounded
Hobby/Pro/Enterprise capability evidence and whether a Git integration is
present, and never emits the token, repository metadata or provider response.
Its CLI accepts the token only on standard input, never through argv, an
environment variable or a file. It has not been run against Vercel. A fresh
result less than 15 minutes old must confirm Hobby before publishing the chosen
edge policy.

`tools/deploy/draft-edge-firewall-adapter.mjs` is the corresponding read-only
firewall-state adapter. It uses the exact project and team identities to read
the active firewall configuration (including IP rules and custom rulesets),
conditional bypasses and every page of project-scoped system bypasses. It
follows no redirects, bounds every response,
uses GET only, and reduces the provider response to the expected normalized
rule, coarse intersecting-rule actions and bypass kinds. It emits no provider
rule IDs, IPs, domains, notes, actors or raw responses. Unknown condition
semantics are treated as potentially intersecting. Its consumer accepts only
the exact active 429 rule, an enabled firewall, no other intersecting rule and
no bypass, with evidence less than 15 minutes old. Synthetic tests use the
current public Vercel REST/SDK response schema; the adapter has not been run
against this project and does not publish or change a rule.

The normalized verifier now accepts exactly this one-rule Hobby policy, a Hobby
plan observation, no bypass, no intersecting rule, no Production effect and the
two exact function limits. The provider adapters supply separate authenticated
plan/project and firewall observations; a later clean readiness handoff must
still bind those observations to the deployed source and function state. Plan
or firewall verification does not authorize publishing the rule, deploying,
enabling a source/runtime gate, cleaning retained rows or touching Production.

### No-deploy CI admission

The ordinary pull-request workflow is the source-only CI path. Its Preview job
requires both a `push` event and `refs/heads/develop`; the classifier independently
sets Preview trust only for that same event/ref pair. Pull requests therefore do
not call the deploy workflow or receive its inherited Vercel secrets. The
workflow now runs all `draft-*.test.mjs` contracts, including a fail-closed
source inspection of those selectors.

GitHub workflow selection cannot prevent Vercel's separate Git integration from
deploying the branch push. Before the first push of this branch, a fresh
owner-approved run of the plan adapter must report `gitIntegration` as
`disconnected`. `verifyDraftNoDeployAdmission` combines that authenticated,
less-than-15-minute observation with the exact local source SHA and the workflow
source contract. A connected, malformed, stale, altered or cross-project
observation blocks the push. The historical November 1 note that Git integration
was disconnected is not current evidence. Do not push merely to discover the
state, and do not disable or reconnect Git integration under this preparation.

Vercel recommends testing a custom rule with a Log action first. If Caroline
later authorizes that provider change, use the exact final conditions and
threshold in Log mode only while all source/runtime gates remain closed. Send a
bounded synthetic request sequence, observe one ten-minute window, then replace
the same rule's follow-up action with 429 and obtain fresh authenticated
evidence. Log mode does not block and is never activation evidence. A 429 is a
stop condition: the client must stop polling rather than retry aggressively,
clear that attempt's in-memory poll token and verifier, retain no new
credential, and begin a fresh pairing only after the fixed window has cleared.
Vercel's current rate-limit documentation does not promise a
`Retry-After` header, so the client and runbook must not depend on one.

Provider references verified November 16, 2026: [WAF rate limiting](https://vercel.com/docs/vercel-firewall/vercel-waf/rate-limiting),
[rule configuration](https://vercel.com/docs/vercel-firewall/vercel-waf/rule-configuration),
[custom-rule Log guidance](https://vercel.com/docs/vercel-firewall/vercel-waf/custom-rules),
and [generated deployment URLs](https://vercel.com/docs/deployments/generated-urls).

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
2. Run the clean authenticated provider adapters and carry their exact fresh
   receipts into the edge observation in `tools/deploy/draft-edge-readiness.mjs`.
   A JSON file or digest supplied by the target is not provider evidence, and
   the source-only synthetic adapter tests are not a live observation.
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

- With every draft gate closed, optionally publish the exact rule first with a
  non-blocking Log follow-up, run only the bounded synthetic match rehearsal,
  and review one ten-minute observation. This is a separate live-setting
  approval and does not authorize activation.
- Publish (or convert that same rule to) one Preview-environment, POST-only
  fixed-window rule, with no Hostname condition, matching
  only `/api/draft-pairings` and `/api/draft-pairings/poll`, at 150 requests per
  provider-derived source IP per ten minutes. It must return 429 before function
  invocation. Do not add a bypass or another intersecting rule.
- Exercise one controlled 429, verify the client stops without immediate retry,
  and wait for the window to clear before starting a new synthetic pairing.
- Probe the stable alias, one generated Preview deployment URL, trailing-slash
  and encoded-path variants, and a Production deployment. Provider evidence
  must show the edge counter covers both Preview hostnames, the Go handler
  rejects the generated hostname/path variants before repository access, and
  Production is unaffected.
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
