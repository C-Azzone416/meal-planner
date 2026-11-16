# Preview draft consent and review slice

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

The current Preview page can approve/deny incoming snapshots and mark an exact
draft revision reviewed. It intentionally does not invent the incompatible manual
projection. The save API exists and rejects incomplete projections; a product-
reviewed projection editor remains a final hookup. Runtime verification of Neon
permissions, migrations, provider membership behavior and protected deployment is
also pending.
