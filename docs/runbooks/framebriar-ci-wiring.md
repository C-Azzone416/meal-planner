# Connect reviewed frontend evidence to mandatory CI

This revision connects the protected bootstrap from #65 to ordinary CI. It does not implement private Preview artifact export or consumption. Those optional paths were removed from this PR after review, rather than retained behind a disabled switch. The last exploratory implementation is in commit `8677af41537c9766a3845cedb35dcd65ea4a175d`; that commit is not an approved exporter or deployment revision.

## Supported behavior

- Ordinary CI `web` preserves npm/lint/typecheck/unit/build checks for manifests without the private Framebriar dependency. A private dependency instead requires complete, reviewed exact-SHA frontend evidence. Ordinary CI receives no package reader. The `required` aggregate, hosted/local Go, contracts and integration checks remain intact. Backend changes also select the web gate.
- `FRAMEBRIAR_TRUSTED_WORKFLOW_SHA` must be an explicitly approved full SHA of current main containing this verifier/workflow. Missing or stale pins fail. Main movement requires review and a deliberate pin update; there is no automatic trust expansion.
- Trusted dispatch `subject=pr` requires an open same-repository PR, its exact current head and actual current base. A GitHub comparison must prove that the current base is an ancestor of the head, preserving ordinary synthetic-merge coverage. Merge or rebase the current base before a fresh approved run if needed.
- Trusted dispatch `subject=develop` uses the exact current develop head and an empty PR input. It never reuses a PR-head receipt for a merge commit. Dispatch and rerun chronology prevent using older success after a newer failed, pending or different-head attempt.
- Owner-approved installation uses inert locked manifests and isolated npm configuration/cache. Only the fixed install step has the package reader. Target checkout, lifecycle handling, frontend tests, Chromium/WebKit checks and target-artifact checks run without that reader.
- A separate clean reporter revalidates the source, base and manifest hashes after frontend execution, then publishes one bounded `evidence.json`. The verifier checks the digest, exact artifact identity, clean-reporter timing, strict fields and complete coverage. The evidence ZIP parser rejects duplicate JSON keys. The removed `prebuild` extension is rejected even when null or empty.
- No Vercel token, Vercel pull/build, deployment artifact upload, artifact sealer or private artifact consumer remains in the reviewed workflow. Stale callers requesting the removed exporter fail explicitly. There is no alternate upload path in this implementation.

## Preview fails closed for private dependencies

Preview first inspects the requested commit's manifests with the same fixed-workflow classifier used by CI. `web` must be a real directory; manifests must be committed regular `100644` blobs at their exact paths, and checkout bytes must match those blobs. Symlinks, missing/malformed files, untracked or changed manifests fail. Direct, development, optional, peer, alias and locked private entries select the private result.

Only the exact public result permits the existing Preview pipeline to proceed. A private or unknown result fails before the token-bearing pull job, repository build or deployment. It does not run a direct private npm install, skip deployment as success, consume an old artifact or bypass migration checks. Public dependencies, including rollback after removing the SDK, retain the existing pull/build/materialize/checksum/extract/deploy/smoke path.

A selected private Preview therefore makes develop's `required` aggregate fail. Successful frontend evidence is useful for PR validation, but is not successful deployment evidence. Private Preview remains blocked until a separately reviewed exporter and consumer exist. Production stays unchanged and off; private push-to-main evidence is not implemented and fails explicitly.

## Integration checkpoints

1. QA reviews the exact proposed head. Judge review follows only after QA approval on that same head; required CI must also pass. A changed head restarts that order.
2. After separately authorized publication/merge, explicitly approve and update the trusted main workflow pin. This code does not configure credentials, environments or pins.
3. Reconcile the feature branch's tests with reviewed evidence rather than direct-reader CI. Preserve its auth/privacy/storage work and include the current base before running the approved PR subject.
4. For a separately authorized develop merge, collect fresh evidence for that merge SHA. Expect private Preview to remain blocked; do not weaken `required` or use a direct-install fallback to make it green.
5. Complete the bounded exporter follow-up, migration readiness and separately approved runtime setup before enabling private Preview. None of these steps authorizes Production deployment.

The shared pulled-environment guard and its existing changes are not removed or relaxed by this split. They also serve public Preview, Production and bootstrap workflows and require their own deployment-policy review. This split does not edit the separate #56 branch or its worktree.

## Deferred bounded exporter

Treat the future exporter as a new, independently reviewed trust boundary, not a revival of the exploratory code by changing a flag. Its acceptance checklist must include:

- A closed, minimal supported output layout based on real output from the pinned CLI and this application's build. Reject unknown files/directories, including empty directories and `.vercel/output/routes/`, until their meaning and upload behavior are established. Do not infer that an unparsed directory is harmless data.
- Explicit supported metadata schemas. Reject duplicate JSON keys before parsing discards them, including nested and escaped-equivalent keys. Either disallow response mutations or require literal names and values with no capture, environment or other runtime substitution. Do not extend support by adding isolated blacklist cases.
- Whole-archive adversarial and positive fixtures: actual Vite assets and stripped/unstripped Go functions, every permitted metadata location, unknown layout, duplicate keys and substitution attempts. Any supported prerender/service behavior needs its own concrete compatibility evidence.
- Separate credential-bearing pull, credential-free build and clean reporting boundaries; no raw private dependency/cache/source-map export; exact project, target, SHA, artifact identity and checksum binding; immutable migration readiness. No target-controlled readiness assertion or cleanup of failed evidence to produce success.
- Explicit assessment of static-scanning limitations. A byte scanner cannot establish general transformed-source secrecy or predict arbitrary runtime output. Disclosures are not risk acceptance. Request an explicit decision for any proposed limitation that conflicts with the required contract.
- Live protected-build and cross-run-consumption verification, provider settings and migration readiness before activation. Local tests and synthetic CLI repros do not establish those results.

Relevant schema sources for that future review are [Build Output configuration](https://vercel.com/docs/build-output-api/configuration), [prerender primitives](https://vercel.com/docs/build-output-api/primitives), [service output layout](https://vercel.com/docs/build-output-api/services), and [transform substitution](https://vercel.com/docs/project-configuration/vercel-json#in-transforms). Reconcile them with the exact pinned CLI, not only current online documentation.

## Verification and remaining limits

Run `node --test tools/framebriar-ci/*.test.mjs`, the classifier/required-gate suites, deployment guard suites and `actionlint`. The split tests execute the actual Preview guard for public/private/missing/malformed classifier output, verify its dependency graph prevents credential use and deployment, reject removed prebuild evidence/callers, and assert that only clean evidence metadata is uploaded. Existing current-base, chronology, identity, digest, archive and credential-boundary regressions remain.

The historical 353-test exporter result is not the current test count: exporter tests were removed with the unsupported implementation, and split-boundary regressions were added. Private Preview, migration readiness, provider configuration and live deployment remain unfinished. No merge, pin, credential, migration or deployment change follows automatically from these checks.
