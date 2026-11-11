# Local preparation: owner-reviewed Framebriar CI

This document describes the bootstrap baseline. The current local wiring revision and its explicit remaining blockers are described in [framebriar-ci-wiring.md](framebriar-ci-wiring.md); the current PR remains unmerged and unactivated pending exact-head QA and judge review.

This dependency-free bootstrap was prepared against the current main baseline, before the private SDK feature. It adds a manual workflow and offline policy tests; it does not provision anything, run a workflow, enable feedback, change owner exemptions, replace a mandatory check or unblock the feature PR by skipping tests.

## Implemented preparatory path

`.github/workflows/framebriar-reviewed-ci.yml` accepts an open same-repository PR number, its exact current head SHA, and the exact reviewed trusted main workflow SHA. It accepts only the personal repository owner as both initiating and rerunning actor. It checks the dispatch API metadata, main execution ref, workflow path/revision and PR head identity and the actual current base branch head from the branch API (not potentially stale PR.base.sha). Forks, closed/moved PRs, moved main and unknown workflow revisions fail closed.

Before any gated job can run, read-only validation requires an existing `framebriar-ci` environment with the owner as sole reviewer, self-review prevention **off**, administrator bypass **off**, and the single allowed deployment branch **main**. The owner can approve her own dispatch, preserving the practical sole-owner model. No second reviewer is needed, and existing PR-only owner/admin exemptions remain unchanged. Missing environment/configuration blocks validation before the environment job can create or use anything implicitly.

Following owner approval, trusted code revalidates the source and checks the immutable commit/tree file modes and retrieves only the two regular manifest blobs via the GitHub API. Truncated trees and symlinks are rejected before installation; the contents API is not used because it can follow symlinks. It checks their hashes, rejects linked dependencies/unreviewed registry hosts or ports, and pins the private package version/integrity. The target checkout is not present while the package reader is used.

The only credential-bearing step uses fixed workflow shell code, an isolated temporary project, a fresh HOME, fixed scoped npmrc, empty separate user/global npm configs, `env -i`, `npm ci --ignore-scripts`, disabled npm log files, and an ephemeral deleted cache. The only package reader is environment secret `FRAMEBRIAR_PACKAGE_READ_TOKEN` mapped to `NODE_AUTH_TOKEN` for this step. No package credential is passed to lifecycle checks, test/build scripts, browsers, reports or uploads.

Only after installation is the exact target checked out. Its HEAD and manifest hashes are checked again, installed dependencies are moved into place, and fresh credential-free steps run lifecycle validation, lint/typecheck/unit tests, full Chromium/WebKit disposable Go/PostgreSQL browser fixtures and target/capture artifact checks. This workflow expects the feature's checked-in test entrypoints; it is not a generic arbitrary-repository runner.

The owner must approve this exact code having access to the installed private SDK. Code can read or disclose a package without retaining its registry token; isolated installation alone does not make unreviewed source safe. No live Framebriar or provider database operation is authorized by this test run.

## Clean evidence, not a replacement check

A separate clean reporter runner has no target checkout, package reader or target-generated artifacts. It revalidates current head/main/manifest identity, requires both validation and frontend jobs to succeed, and writes a small metadata-only `evidence.json`. It uploads the immutable artifact `framebriar-evidence-<run ID>-<attempt>` with `overwrite: false`. A target attempt to pre-create that name must make the reporter upload and overall workflow fail, never cause an overwrite/acceptance.

Evidence includes repository/PR, head and base SHA, trusted workflow SHA, run/attempt, manifest hashes and exact frontend coverage. It is informational only. No check-run or commit-status write permission is requested. The dispatch's native SHA is main, not its target SHA. The ordinary PR `web`/`required` gates and release push-main verifier are unchanged and remain mandatory. A green similarly named check is not trusted evidence.

`tools/framebriar-ci/policy.mjs` includes an offline-tested **unwired** bridge verifier. It requires:

- The expected repository, owner, workflow path, main branch and deliberately trusted workflow SHA.
- A successful completed manual run and all three exact jobs successful on that same run attempt.
- The expected unexpired immutable artifact, bound to the run/workflow SHA, with creation during the clean reporter job, not the target job.
- Exact current PR head/base identity, manifest hashes and full frontend coverage.

The read-only retrieval CLI `tools/framebriar-ci/bridge.mjs` now implements that verification, but remains **unwired from PR/mandatory CI**. Its required inputs are `GITHUB_REPOSITORY`, `PR_NUMBER`, `TARGET_SHA`, explicit `TRUSTED_WORKFLOW_SHA` and an ephemeral workflow `GH_TOKEN` with contents/Actions/PR read permissions. It never obtains package credentials or writes GitHub state. The trusted revision is a required input, not silently copied from current main.

It retrieves current PR/head, actual base-branch head, owner/repository, trusted dispatch, exact-attempt jobs and artifact metadata through the GitHub API. It requires one matching unexpired evidence artifact with matching repository/run/branch/attempt provenance and a SHA-256 digest. Signed storage requests receive no GitHub authorization header. Only HTTPS GitHub Actions/Azure artifact storage redirects are accepted, with no second redirect. Downloads are streamed and bounded: 1 MiB per metadata response, 128 KiB ZIP, 16 KiB decompressed JSON; 40 requests and a 90-second overall deadline, with at most 10 seconds per request. Inventories are bounded to 300 entries and reject truncation, duplicate IDs or changing counts; exceeding the bound is a clear failure requiring reviewed maintenance, not acceptance of partial history.

The Python-standard-library parser reads memory only and never extracts files. It permits exactly one regular `evidence.json`, rejects duplicate entries/JSON keys, symlinks, traversal, ZIP64/split/encrypted archives, prefix/trailing content, archive comments, extra unlisted local entries and decompression overflow. It verifies CRC and bounded local/central-directory consistency. The JSON schema and exact manifest hashes are then checked against regular Git blobs at the target SHA. Finally, PR/base/main, run attempt/latest selection and artifact identity/digest are rechecked to reject races.

Only public metadata field availability has been inspected live: artifact digest/repository binding and run/job attempt fields exist. No user/live artifact was downloaded and no real dispatch/private-package installation was exercised. ZIP compatibility was verified using the actual official writer from pinned upload-artifact v7.0.2: `@actions/artifact@6.3.1`, `archiver@7.0.1`, `zip-stream@6.0.1`, `compress-commons@6.0.2`. A synthetic single metadata file produced a 191-byte DEFLATE archive with streamed data descriptor (flags 8), no ZIP64; the strict parser accepted it. Its non-sensitive base64 fixture is checked in for regression. Live signed-download/upload-service compatibility still requires an approved clean reporter run; do not weaken checks silently if the service changes its format.

## Smallest next CI bridge decision

An ordinary PR web job can remain credential-free and require successful exact-head evidence from this trusted dispatch, then rerun ordinary CI after owner approval. This could retain the existing `required` context and branch rules without adding another required-check context. It must fail when evidence is absent, incomplete, expired, for a moved head/base, for an untrusted workflow revision or from a failed/colliding reporter run. Keep ordinary backend/contracts/integration gates and any frontend checks not included in the dispatch; never replace coverage with a success stub.

That bridge is implemented locally but not activated here. Before activation, choose how its trusted workflow revision is maintained and review/test the complete API/artifact provenance verifier with an approved clean reporter run. Actions check names alone are spoofable. PR workflow/verifier files are themselves editable in a PR, just as current CI files are, so the sole owner must inspect workflow changes and the trusted run before merging. Stronger independent check identity would need a separately approved restricted GitHub App or equivalent, not an invented trust claim.

### Main advancement and pin maintenance

Current validation deliberately requires the reviewed workflow SHA to equal current main. If main advances, even with identical workflow file content, old pins/runs are rejected. After main moves, review the new trusted revision, update the future bridge's approved pin through the normal owner-reviewed code path, and dispatch/reapprove the current target SHA. Main movement while approval or execution is pending also fails revalidation. This conservative behavior is intentional maintenance, not automatic setup. Do not silently accept any ancestor workflow or auto-update a pin from untrusted target code. A future verified workflow-content digest policy could reduce maintenance, but requires its own review/tests.

## Push/deploy access remains a separate decision

Main's current CI uses only public dependencies. The feature adds a private dependency and currently blocks PR installation without a reader. This bootstrap does not grant a reader to normal PR or push jobs. The Preview/deployment jobs also cannot access an environment-only reader merely because it exists.

Before merging the private feature, resolve both the credential-free PR evidence bridge and the approved trusted push/deploy access path. A separately approved manual owner gate for each exact pushed/deployed SHA is possible, but no package access, artifact reuse or deployment authorization is implied here. Keep the existing main `required` CI and exact-SHA release checks, and keep Preview/Production feature switches off. Do not create a green transition gap to land the feature.

## Required setup approvals, not performed

1. Approve creation/configuration of `framebriar-ci` with the existing owner reviewer, self-review allowed, no admin bypass and main-only branch policy. Preserve current branch review exemptions and non-bypassable CI.
2. Confirm package-owner access for the selected principal. Approve GitHub classic PAT `read:packages` only, its expiry and storage solely as the named environment secret. Classic PAT access covers packages the principal can read; it is not inherently limited to one package. No `repo`/write/delete scopes are justified here.
3. Review/merge this dependency-free bootstrap through ordinary passing baseline CI, then implement and validate the exact evidence bridge and trusted push/deploy access plan without weakening gates.
4. Approve a specific target SHA and trusted workflow revision for a test run, then inspect clean-install and reporter evidence. A local test pass does not mean this setup or any live receipt is complete.

No credentials, provider IDs, private payloads or package source belong in this public preparation.

## Offline checks

Run `node --test tools/framebriar-ci/*.test.mjs`, `actionlint .github/workflows/framebriar-reviewed-ci.yml`, and the existing classifier/gate tests. The baseline CI affected job runs these dependency-free policy tests. Negative fixtures cover fork/moved-head/main, wrong actor/workflow provenance, missing environment protection, unsupported registry/link dependencies, absent frontend evidence, incomplete jobs/coverage, wrong run attempt, target-timed artifacts, reporter collision failure and pin maintenance.

Read-only API shape verification confirmed the jobs endpoint currently includes `run_id` and `run_attempt`. A future bridge must query the specific run attempt and fail closed if these binding fields are missing rather than inventing their values.

## Latest-run policy

For this trusted workflow and PR, the newest dispatch identity (greatest run ID, with consistent dispatch creation timestamps) wins, never the last completion or `updated_at`. If old R1 starts at 10:00 and finishes successfully at 10:06, it cannot override newer R2 that failed at 10:05. The newest dispatch must target the current head and its current attempt must pass; pending, failed, cancelled, missing-artifact and differently targeted newest runs block fallback to older success.

Older dispatch reruns receive a separate conservative check. For every older dispatch with attempt > 1, retrieve its exact latest-attempt API record and verify matching run ID/attempt. It must be completed and have an explicit `run_started_at` at or after its original creation, but strictly before the newest dispatch creation. A later/equal-start, pending, missing-start or mismatched older rerun blocks verification and requires a fresh owner-approved dispatch after that attempt. It never becomes replacement successful evidence itself. An older first-attempt completion is ignored for ordering. Recheck both dispatch selection and older-attempt chronology after download. This is intentionally stricter than selecting the latest success.

Identify the PR before enforcing the title contract. A malformed unrelated/unknown title cannot poison every PR. A malformed title with this PR's numeric prefix (or an API association with this PR) blocks it rather than being silently discarded. The current selected run attempt must complete successfully, including the clean reporter upload; expected-name collisions fail because upload overwrite is disabled.

Offline execution: `node --test tools/framebriar-ci/*.test.mjs`. The verifier itself can be invoked as `node tools/framebriar-ci/bridge.mjs` only after supplying the explicit inputs above through step-scoped environment; it is not currently called by a workflow. Do not pass credentials in arguments, print them or use a persistent package reader for this read-only role.

### Inventory bound and retention policy

The verifier requires complete inventories and rejects any runs/jobs/artifacts result above 300 entries or beyond its request/time budgets; it does not truncate to the newest page, skip unrelated entries to fit, or search older successes. Since the run inventory is workflow-wide, old unrelated dispatches can eventually reach this bound. Artifact retention is seven days; an expired/missing artifact always requires fresh evidence, not acceptance of logs or an older successful check.

There is no automatic cleanup in this bootstrap. Before the bound is reached, an owner must explicitly review a retention change or cleanup of obsolete closed-PR history, preserving an audit record. Preserve all relevant dispatch/attempt history for every open PR. Never delete a newer failed/pending run or later rerun to make an older result pass. If preserving necessary history cannot fit the current limits, keep verification blocked and review a bounded pagination/index/checkpoint design and its anti-rollback tests. Raising a bound or introducing a trusted cutoff is a reviewed code/policy change, not a runtime fallback. The verifier does not infer permission to delete any run or artifact.
