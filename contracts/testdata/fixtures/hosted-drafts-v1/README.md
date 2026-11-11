# Hosted draft v1 synthetic fixtures

Contract: [docs/hosted-draft-contract.md](../../../../../docs/hosted-draft-contract.md).
Nothing in this pack is a live account, receipt, recipe, safety statement or secret.
All URLs use the reserved `.invalid` domain. UUIDs and owner subjects are synthetic.

For each accepted name (`incomplete`, `full`):

- `.local.json`: saved local schema-1 draft envelope, revision 7.
- `.submit.json`: proposed HTTP submission **body**, accepting only the content
  version selector and canonical content. Use a separately retained synthetic
  `Idempotency-Key`, such as `synthetic-incomplete-send-1` or
  `synthetic-full-send-1`; retries retain that key and captured body.
- `.hosted.json`: synthetic 201 acceptance response with new hosted IDs/revisions,
  unreviewed owner-scoped draft, immutable receipt, structural warnings and links.
- `.app.json`: lossless app edit-state content with read revision tokens, not the
  current web planner Draft type and not a valid submission body.

`full` exercises every content field family, all source kinds, all phases plus an
unclassified step, every estimate type at recipe and phase scope, all guidance
topics, both basis kinds, string fractions/zero/to-taste quantities, Unicode,
multiline notes and explicit zero minutes. A source-reference warning intentionally
remains. It is not ready for approval: numeric servings, planner classification,
projection and product policy are unresolved. `incomplete` has unknown estimates
and no ingredients/steps/sources; these are warnings, not invented values.

`preview-context.json` gives exact synthetic account/environment labels and fully
resolved receipt/draft links for the #25 dry run. Replace these only in the private
live execution record; never treat `.invalid` URLs as an environment setup.

`manifest.json` records accepted warning codes and rejected HTTP status/error code
expectations. `issue_code`, where present, is the nested core semantic issue code.
Rejections are raw request bodies; preserve raw bytes when checking duplicates or
malformed Unicode. Every named authoritative field is tested at both root and
content. Source-local IDs and references remain accepted in the full fixture.

Run from the repository root:

```sh
go test -count=1 ./contracts -run HostedDraft
```

The test-only submission decoder checks the specified wrapper and calls existing
core decode/validation. Typed round trips catch dropped content; no hosted handler,
store, migration, bridge, authentication or browser implementation is exercised.
Later integration and owner Preview acceptance are in contract sections 4–5 and 8.
Do not infer live acceptance from these synthetic checks.
