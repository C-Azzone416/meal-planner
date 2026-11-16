# Preview draft consent and review slice

Status: source-complete, runtime-disabled. The projection editor is wired only
into the inert hosted-review page, while the native credential adapter is not
constructed by a shipped command. This document records code behavior, not live
Preview acceptance.

This source is intentionally inert. `draftWorkflowSourceEnabled` is `false`, and
the runtime additionally requires Preview plus `MEAL_DRAFT_WORKFLOW_ENABLED=true`
and the exact pinned Preview origin. Migration `006_draft_review_workflow.sql` is
additive but is not applied by the application. No live role, grant, secret,
migration, deployment, or Production route is created here.

The local client may only propose an immutable content snapshot. Its
`draft:propose` grant creates and polls an intent; it cannot approve the intent,
read another grant's intent, review a draft, create a recipe, or access database
credentials. A signed-in owner must see the complete canonical content and approve
the exact intent revision and SHA-256 content hash from the exact same-origin app.
Owner mutations require both the verified bearer identity and same-origin browser
request metadata. Missing or cross-site metadata fails closed.

Approval revalidates the original grant, including current membership, revocation,
expiry and scope, then uses the intent UUID as the idempotent submission key. A
commit followed by a lost response is recovered by retrying the same key and
snapshot. Polling returns only state, safe object IDs and acceptance metadata;
credentials and recipe content never appear in the grant response.

The owner list and detail APIs retain and return canonical schema-1 content. They
do not map it into the manual recipe model. Missing estimates remain absent and
display as **Unknown**; an explicit zero remains zero. Sources, basis records,
guidance, raw ingredient text and unclassified steps remain attached to the draft.
Review is bound to both draft revision and review revision.

Saving requires a complete owner-supplied `RecipeContent` projection and a draft
already in `ready` review state. It reuses `RecipeRepository.Create`; the hosted
draft UUID is the stable owner-scoped recipe idempotency key. This is a deliberate
retry-safe saga, not a cross-database transaction: if recipe creation commits and
the review link update fails, retry returns the same recipe receipt and finishes
the link. A different projection with the same key conflicts rather than creating
a second recipe. Canonical content and provenance remain in the hosted draft.

The Preview source can approve or deny incoming snapshots, mark an exact draft
revision reviewed, and then collect a complete explicit manual projection. Every
lossy field begins unresolved. Canonical title, ingredient and instruction text
stay read-only until the owner deliberately copies them; quantity, unit, aisle,
pantry, phase, time, timer, storage, keep-limit and tag decisions remain explicit.
Blank values are not coerced to zero or false. Save is bound to the exact draft
and review revisions; an uncertain response retains the identical projection for
retry, while stale revisions and duplicate clicks fail closed. The API remains the
authoritative validator and idempotent recipe creator.

The shipped MCP command still does not construct the sender or native Keychain
adapter, and every workflow gate remains closed. Runtime verification of the
signed package and Keychain ACL, Neon permissions and migrations, provider
membership behavior, authenticated Preview UI, and protected deployment is still
pending.
