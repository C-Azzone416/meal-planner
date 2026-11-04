# CI/CD Configuration Plan

Status: approved policy with recommended implementation details, 2026-11-01

This is the configuration guide for the future GitHub Actions, Vercel, Neon, and local-tool release pipelines. It is documentation only: no workflow, application, migration, credential, provider setting, or deployment described here has been implemented by this document.

## How to read this plan

- **Approved** — a delivery behavior that future implementation must preserve.
- **Recommended** — the current implementation proposal; it may change after validation without changing the approved behavior.
- **Prior verified** — observed during provider setup before this document was written. It was not revalidated by a deployment or workflow and may drift.
- **Unresolved** — a decision or provider capability that must be closed before the affected configuration is treated as ready.

Preparation is not the same as a wired pipeline. The secret-free pull-request and branch CI (`.github/workflows/ci.yml`) and its classifier are implemented by the CI gate pull request. The repository still has no deployable application, database schema, migration runner, working Preview or Production workflow, or release process. The Preview, Production and bootstrap deploy workflows are written but not yet enabled or exercised.

## Approved delivery model

### Repository and release units

- The public monorepo will contain a reusable Go core, hosted Go API, Go CLI, Go MCP adapter, and a TypeScript/React web client.
- The hosted web/API release and local CLI/MCP releases are separate release units.
- The Vercel-hosted web/API remains subject to the early workload-fit checkpoint in the [architecture plan](architecture.md).
- A web-only release does not create a CLI/MCP version. A CLI/MCP release does not deploy the hosted application.
- The CLI and MCP must remain compatible with the documented minimum supported hosted API version.

### Branch and promotion flow

```text
feature/* ---- pull request ----> develop
                                      |
                                      v
                            temporary release/*
                                      |
                                pull request
                                      v
                                    main
                                      |
                         manual production action
```

- `main` and `develop` are long-lived and must not be deleted or force-pushed.
- Temporary feature and release branches may be deleted after merge.
- Normal feature pull requests target `develop`.
- A temporary release branch is cut from `develop` and proposed to `main`.
- Production is manually initiated only after CI succeeds for the exact final `main` commit to deploy.
- Automatic Preview is limited to pushes to `develop` that change hosted components and pass their selected checks. Pull requests run checks only and never deploy a Preview.
- Preview is **Approved in principle**. Automatic Preview stays disabled (the repository variable `PREVIEW_ENABLED` is unset) until the one-time Vercel bootstrap passes.
- Rulesets that block deletion and force push on `main` and `develop` are **Approved**. The remaining ruleset options (required pull requests, approvals, bypass, merge method) are **Recommended** or **Unresolved**, as listed in the checklist.

## Proposed workflow boundaries and triggers

The workflow filenames and exact path filters are **Recommended** until the source tree exists. Keep the responsibilities separate even if the implementation combines some jobs.

| Workflow responsibility | Trigger | Credentials | Result |
| --- | --- | --- | --- |
| Pull-request CI | `pull_request` targeting `develop` or `main` | None beyond read-only GitHub token | Detect changes, run selected checks, publish stable gate. Never deploys a Preview |
| Branch CI | `push` to `develop` and `main` | None beyond read-only GitHub token for checks; a `develop` push may hand off to Preview | Reconfirm the merged commit and publish CI evidence for its exact SHA |
| Preview | `push` to `develop`, hosted change, selected CI success | `preview` environment only | Build, deploy Preview, smoke-test URL, then move the stable alias (`PREVIEW_ALIAS`, if set) |
| Production | `workflow_dispatch` with an explicit `main` SHA | `production` environment only | Verify CI evidence, migrate, deploy, smoke-test exact SHA |
| Local-tool candidate | Local core, CLI, MCP, contract, or release-metadata change | None for ordinary CI | Test supported platforms and API/protocol compatibility |
| Local-tool release | Manual action or stable version tag after candidate CI | Release-only credentials if later required | Build, verify, checksum, and publish CLI/MCP artifacts |

Additional trigger rules:

- **Approved:** documentation-only changes run the classifier and stable gate but do not build or deploy an application.
- **Approved:** CLI/MCP-only changes do not deploy the hosted application.
- **Recommended:** cancel stale pull-request CI using a concurrency key scoped to workflow and pull request.
- **Approved:** Preview deploys use concurrency group `preview-<sha>` with `cancel-in-progress: false`. Every `develop` commit deploys and none is cancelled, so no commit's `CI / required` is turned red by a cancelled Preview. Runs for different SHAs can overlap, so the stable alias moves forward only: it is skipped when the commit it currently serves is a descendant of the run's SHA on `develop`.
- **Approved:** serialize Production runs. Never cancel an in-progress production migration or deployment merely because a newer run was requested.
- **Conditional:** add `merge_group` only if a merge queue is enabled. GitHub requires that event for required Actions checks in a merge queue.
- **Recommended:** add scheduled dependency or security checks later, but keep them outside the merge-facing required check unless they produce deterministic per-commit results.

## Dependency DAG

```text
                         +------------------+
pull_request/push ------>| detect affected  |
                         +---------+--------+
                                   |
                 +-----------------+------------------+
                 |                 |                  |
                 v                 v                  v
           web checks       hosted Go checks   local Go checks
                 |                 |                  |
                 +-----------+-----+---------+--------+
                             |               |
                             v               v
                       contract checks   integration checks
                             |               |
                             +-------+-------+
                                     |
                 develop push? ------+-- no --> stable required gate
                         | yes
                         v
                 Preview build/deploy
                         |
                    Preview smoke
                         |
                    stable alias
                         |
                         v
                 stable required gate

manual Production(exact main SHA)
        -> verify SHA + successful branch CI evidence
        -> production build and immutable manifest
        -> forward-only compatible migration
        -> production deploy
        -> smoke verification
        -> record deployment evidence
```

The stable gate must use an unconditional evaluation step (for example, an `always()`-style dependency) so a failed dependency cannot silently skip the merge-facing result.

## Affected-component plan

### Logical components

The classifier should emit a machine-readable plan with at least these booleans:

- `docs`
- `web`
- `hosted_go`
- `local_go`
- `contracts`
- `integration`
- `hosted_preview`
- `release_metadata`

### Path map

**Recommended**, implemented in `.github/scripts/classify.sh` and tested by `.github/scripts/classify_test.sh`. Any path that matches no rule selects everything (fail safe).

| Path | Class | Selects |
| --- | --- | --- |
| `docs/**`, root `*.md` | docs | nothing (only the gate runs) |
| `web/**` | web | `web` |
| `api/**`, `hosted/**`, `vercel.json` | hosted Go | `hosted_go`, `local_go`, `contracts`, `integration` |
| `cmd/**` | CLI / MCP | `local_go`, `contracts`, `integration` |
| `internal/core/**` | core | `hosted_go`, `local_go`, `contracts`, `integration` |
| `contracts/**` | contracts | everything |
| `go.mod`, `go.sum`, `.github/**` | toolchain | everything |
| anything else | unknown | everything |

- Local Go compatibility runs on hosted changes, because the CLI consumes the hosted API. `cmd/**` also selects `contracts` and `integration` as compatibility checks, but never Preview.
- `integration` is selected with `hosted_go`, `cmd/**`, core, contracts, or toolchain (and so for unknown paths).
- `hosted_preview` = (`web` or `hosted_go` or core or contracts or toolchain) AND the event is a `push` to `refs/heads/develop` (`PREVIEW_TRUSTED`; pull requests, including same-repository and dependabot ones, and pushes to `main` are never trusted) AND `vars.PREVIEW_ENABLED == 'true'`. Because the variable is unset, Preview is off.
- Push events diff against `github.event.before`; pull requests diff against the base SHA. If the base cannot be found (new branch, force push) or the diff fails, everything is selected.
- `release_metadata` is reserved for the CLI release workflow and currently mirrors toolchain/unknown changes.

### Selection matrix

| Change class | Web | Hosted Go | Local Go | Contracts | Integration | Preview |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Documentation only | — | — | — | — | — | — |
| Web client only | ✓ | — | — | as imported | as required | trusted PR only |
| Hosted Go API only | — | ✓ | shared API contract | ✓ | ✓ | trusted PR only |
| Reusable Go core | as dependent | ✓ | ✓ | as dependent | ✓ | if hosted output changes |
| CLI only | — | — | ✓ | ✓ | compatibility | — |
| MCP only | — | — | ✓ | ✓ | protocol + compatibility | — |
| Shared contract | ✓ | ✓ | ✓ | ✓ | ✓ | trusted PR only |
| Persistence or migration | as dependent | ✓ | as dependent | ✓ | ✓ with temporary Postgres | trusted PR only |
| Dependency/toolchain/workflow | ✓ | ✓ | ✓ | ✓ | ✓ | only if hosted output changes and trusted |
| Unknown path | ✓ | ✓ | ✓ | ✓ | ✓ | only if hosted output may change and trusted |

`—` means intentionally not selected, not an absent or lost result. The detector must explain each intentional skip in its summary.

### Stable required check

Use exactly one required merge-facing check name:

```text
CI / required
```

Recommended unique job display names:

```text
CI / affected
CI / web
CI / hosted-go
CI / local-go
CI / contracts
CI / integration
Deploy / preview
CI / required
```

The workflow is named `CI` (file `.github/workflows/ci.yml`) and the gate job's `name:` is exactly `required`. GitHub displays that as `CI / required`, but `CI / required` is only the display form: the actual check context a ruleset requires, and the job that release and production workflows look up (workflow file `ci.yml`, job `required`, on a push to `main`), is `required`. Do not rename the workflow, file, or job. Other jobs likewise report as `CI / <job>`. `preview` is a call to the reusable `preview.yml`; its smoke test runs inside that workflow and `required` treats the `preview` job result as covering both Preview and Preview smoke. Pull requests cancel stale runs; push runs are keyed on the SHA and are never cancelled, so every `develop`/`main` commit keeps CI evidence.

Only `CI / required` is configured as a ruleset requirement. It runs unconditionally (`always()`), needs every job, and must:

1. read the classifier output;
2. inspect every selected job result;
3. accept an intentional skip only when the classifier did not select that job;
4. require Preview and Preview smoke success when `hosted_preview` is selected (it is never selected for a pull request, so `preview` is then skipped and accepted);
5. fail on cancellation, failure, missing output, unknown result, or an unexpectedly skipped selected job (and fail if `affected` itself did not succeed, or if a job ran although it was not selected); and
6. publish a human-readable selection/result summary.

Job display names must be unique across workflows. Renaming the stable job is a repository-settings migration: add the new check, prove it reports, then remove the old requirement.

## Pull-request and public-fork boundary

### Approved security rules

- Pull-request checks must be secret-free and run with least-privilege permissions, normally read-only repository contents.
- Fork pull requests never receive repository, environment, Vercel, Neon, package, signing, or production secrets.
- Do not use `pull_request_target` to check out or execute untrusted pull-request code with privileged context.
- No pull request deploys a Preview. Only a push to `develop` (event and ref verified, in the classifier and again in the `preview` job's `if:`) does, after selected CI passes.
- Production data, production Auth state, and production credentials never test pull-request code.
- Integration tests use disposable Postgres and synthetic data.
- Pin third-party Actions to full commit SHAs and record the human-readable release in a comment or dependency-update configuration.
- Set explicit `permissions` at workflow and job scope; add write permissions only to the exact deploy or release job that needs them.
- Do not restore write-capable or secret-bearing caches into untrusted jobs. Include lockfile/toolchain identity and trust boundary in cache keys; never cache credentials or provider state.
- Cancel stale pull-request runs, but not an in-progress production migration/deployment.

### Recommended review boundary

Changes to workflow files, release metadata, migration tooling, and dependency-update configuration should require explicit maintainer review. CODEOWNERS and the number of approvals remain unresolved.

## Hosted Preview pipeline

Preview is **Approved** only for a push to `develop` with hosted changes. Pull requests run checks only. Production is separate: manual `workflow_dispatch` from `main`.

Preview runs in a separate trusted job (`preview`, a reusable-workflow call), tied to the exact pushed commit SHA (`github.sha`). It starts only after `affected` and every selected check succeeded. Preview secrets (`VERCEL_TOKEN`) are referenced only by that job and never reach the `pull_request` check jobs.

**Stable alias.** If the repository variable `PREVIEW_ALIAS` is set (a hostname such as `meal-planner-develop.vercel.app`), the deploy job, after the smoke test passes, assigns it with Vercel's `POST /v2/deployments/{id}/aliases` API (only if it would move forward: skipped when the alias already serves a descendant commit on `develop`, found by an unauthenticated blob-less fetch of `develop`; a failed comparison fails the step), then polls `/v4/aliases/<alias>` until it points at this deployment ID (up to 3 minutes) and records it in the step summary. The REST path avoids the CLI's unsupported user lookup for project-scoped tokens; the token is sent only through curl config on stdin and the response must confirm the requested alias. Unset means the step is skipped with a notice. The step fails if the alias is one of the project's production domains; that guard covers this project's domains only and fails closed above 100 domains. The fetch requires the repository to stay public.

### One-time first-deployment bootstrap

This bootstrap is an explicit, separately approved exception to "Production is manual and release-only": it creates a Production-class deployment of a disposable slice, not a product release, and needs its own approval at execution time. It is not needed now. It does not block repository setup, secret-free CI, or CLI/MCP implementation and testing. It is required only before enabling the first automatic hosted Preview, after a minimal non-sensitive hosted verification slice exists. The procedure below is a plan, not authorization to deploy or change provider settings.

**Source-backed Vercel behavior:**

- Vercel documents the first CLI deployment of a new project as a Production deployment even when `--prod` is omitted.
- The deploy CLI supports an explicit `--target=preview`, but Vercel's current BYOC deployment guide states that a new project's first deployment is still assigned to Production when Preview is requested. Explicit Preview targeting does not remove the first-deployment bootstrap requirement.
- `--skip-domain` must be used with `--prod`; it prevents automatic Production-domain aliasing so the deployment can be verified at its unique deployment URL before any domain promotion.
- For a prebuilt deployment, the pull, build, and deploy environments must match. Vercel also warns that `--prebuilt` omits System Environment Variables at build time, so the platform-fit checkpoint must confirm that the selected framework and build do not depend on them.

**Recommended one-time procedure, pending explicit approval at execution time:**

Prerequisites are a reviewed exact commit SHA and a disposable hosted slice with no database, Auth, migrations, user data, or Production-only application secrets. Keep automatic Preview disabled and Vercel Git integration disconnected.

1. Record the full source SHA and build the disposable slice with the Production target:

   ```text
   vercel pull --yes --environment=production
   vercel build --target=production
   vercel deploy --prebuilt --prod --skip-domain
   ```

2. Inspect the returned unique deployment URL and deployment record. Verify the intended Vercel project, full source SHA, Production environment classification, and absence of a Production/custom-domain alias. Smoke-test only the unique deployment URL.
3. Exercise a separate explicit Preview using matching Preview inputs:

   ```text
   vercel pull --yes --environment=preview
   vercel build --target=preview
   vercel deploy --prebuilt --target=preview
   ```

4. Inspect the second deployment. Verify the intended project and SHA, Preview environment classification, absence of a Production-domain alias, and a successful smoke test at its unique Preview URL.
5. Preserve the deployment evidence in the workflow run summary. Only after both classifications and domain behavior are proven may the automatic `develop`-push Preview path be enabled.

This is a Production-class provider bootstrap, not a product Production release. Do not add database credentials or run migrations merely to perform it. If current CLI behavior differs from the cited documentation, stop without enabling automatic Preview and update this plan from current official guidance.

Recommended sequence:

1. Check out the pull-request commit and record its full SHA.
2. Run the affected-component checks.
3. Obtain Preview project settings and environment through the protected Preview job.
4. Build the hosted release for that SHA and emit a manifest containing the source SHA, lockfile hashes, tool versions, and artifact checksum.
5. If the change includes compatible migrations, apply them to the persistent Preview branch using the Preview migration credential.
6. Deploy the prebuilt output to Vercel Preview.
7. Smoke-test the returned deployment URL without exposing it to fork-originated scripts.
8. Make the stable required gate validate Preview and smoke results when Preview was selected.

Preview failure blocks the stable gate for a selected `develop` push. Pull requests, including forks, never reach Preview secrets; they get secret-free review and CI only.

## Hosted Production pipeline

Production is **Approved** as a manual, exact-SHA operation.

Recommended sequence:

1. The operator selects a full commit SHA that is the intended final `main` commit.
2. The workflow proves the SHA is on `main`, is the expected current release target, and has a successful `CI / required` result from branch CI. A release-branch or pre-merge SHA is rejected.
3. Check out by immutable SHA, not a movable branch name.
4. Build with production settings and emit an immutable manifest. Until environment-neutral artifact promotion is proven, promote the **source identity and CI evidence**, then rebuild the production artifact from that SHA; do not claim the Preview artifact is identical.
5. Pass any environment approval gate before environment secrets become available.
6. Apply only reviewed, forward-compatible migrations through the direct production migration connection.
7. Deploy the exact production build and record the Vercel deployment identifier plus manifest checksum.
8. Smoke-test the deployment and record the result against the source SHA.

Production concurrency uses one serialized group with no cancellation of an in-progress run. A newer request waits or is explicitly abandoned before database work starts.

### Migration and rollback policy

- Use expand-and-contract migrations. Expansion lands before code depends on it; destructive contraction occurs only after old hosted and supported local clients no longer use the old shape.
- The migration runner uses a direct, branch-specific, limited migrator role. Runtime uses a pooled, branch-specific, minimal-DML role.
- Never run automatic destructive down migrations in Production.
- Migration failure stops deployment. Diagnose and roll forward with a reviewed corrective migration.
- Deployment failure after an expansion leaves the previous application compatible with the expanded schema; keep or restore traffic to the last verified deployment.
- Smoke failure stops completion and triggers an explicit operator decision. Prefer traffic rollback to the prior verified deployment when the expanded schema is backward-compatible; do not reverse schema automatically.
- Backup/restore expectations and the migration ownership tool must be selected and rehearsed before the first meaningful Production data migration.

## CLI and MCP release plan

### Approved behavior

- Local tools have stable versions independent of hosted web releases.
- Candidate CI tests reusable core behavior, CLI behavior, MCP protocol behavior, and compatibility with every supported hosted API version.
- Publication includes checksums and verifies downloaded artifacts before marking the release complete.
- Installation and updates are explicit; there is no silent updater.
- The update check runs at most once per day on use, is cached, times out quickly, is nonblocking on failure, and has an opt-out.
- Human-terminal update notices go to an appropriate diagnostic channel. They never appear in JSON command output, scripted standard output, or MCP protocol messages.
- The MCP adapter may expose a status tool and a skill may notify at most once per session. Host behavior is not guaranteed.
- Updating an MCP executable requires an explicit MCP-host restart.

### Recommended candidate matrix

The initial candidate matrix is a proposal, not a distribution commitment:

| OS | Architecture | Candidate artifact |
| --- | --- | --- |
| macOS | arm64, amd64 | CLI and MCP binaries/archive |
| Linux | amd64, arm64 | CLI and MCP binaries/archive |
| Windows | amd64 | CLI and MCP binaries/archive |

Every candidate should run unit tests, adapter tests, protocol conformance tests, supported-API compatibility tests, clean-machine install/uninstall checks, and checksum verification where the runner supports the target.

### Unresolved release decisions

- semantic-version and tag namespace for CLI versus MCP;
- whether CLI and MCP share one version or release independently;
- archive formats, binary names, and installation paths;
- code signing, notarization, provenance, SBOM, and signature verification;
- package managers or registries, if any;
- exact GitHub Release manifest and update-feed format;
- minimum supported OS versions and final architecture list;
- API compatibility window and removal/deprecation schedule; and
- whether releases are tag-triggered, manually dispatched, or both.

Do not create a release workflow until these are decided.

## Configuration checklist

Statuses below are prior setup observations, not a live audit performed by this document change.

| Location | Name/setting | Kind and scope | Purpose | Dependency | Status |
| --- | --- | --- | --- | --- | --- |
| GitHub repository | Actions permissions | Setting | Default read-only token; selectively elevate per job | Workflow design | **Recommended; not configured/verified here** |
| GitHub repository | Actions allowed sources | Setting | Limit or pin Actions used by workflows | Selected Actions | **Unresolved** |
| GitHub repository | `main` ruleset | Branch ruleset | Block deletion/force push; require PR and `CI / required` | First reporting CI run | **Approved (block deletion and force push); other options Recommended; not configured** |
| GitHub repository | `develop` ruleset | Branch ruleset | Same long-lived-branch protections | First reporting CI run | **Approved (block deletion and force push); other options Recommended; not configured** |
| GitHub repository | Merge method, approvals, stale review, conversation resolution, bypass | Ruleset settings | Review policy | Collaboration model | **Unresolved** |
| GitHub repository | Merge queue | Ruleset setting | Optional serialized merge validation | `merge_group` trigger | **Unresolved; do not enable alone** |
| GitHub Actions | `VERCEL_ORG_ID` | Repository variable | Identify Vercel account/team without treating ID as a credential | Vercel CLI workflow | **Prior verified present** |
| GitHub Actions | `VERCEL_PROJECT_ID` | Repository variable | Identify Vercel project | Vercel CLI workflow | **Prior verified present** |
| GitHub Actions | `NEON_PROJECT_ID` | Repository variable | Identify isolated Neon project if management automation is later added | Neon management workflow | **Prior verified present; not currently needed by deploy** |
| GitHub Actions | `NEON_PREVIEW_BRANCH_ID` | Repository variable | Identify persistent Preview branch if needed | Preview database automation | **Prior verified present** |
| GitHub Actions | `NEON_PRODUCTION_BRANCH_ID` | Repository variable | Identify persistent Production branch if needed | Production database automation | **Prior verified present** |
| GitHub Actions | `VERCEL_TOKEN` | Currently repository secret; environment scope recommended | Vercel CLI authentication | Trusted Preview/Production jobs | **Prior verified installed and project-scoped; never validated by a workflow** |
| GitHub environment | `preview` | Environment | Isolate Preview approvals, branch policy, secrets, and deployment history | Preview workflow | **Prior verified exists; protections absent** |
| GitHub `preview` environment | `MIGRATION_DATABASE_URL` | Environment secret; direct Preview endpoint | Run reviewed Preview migrations as limited migrator | Schema and role design | **Deferred; do not create yet** |
| Vercel Preview | `DATABASE_URL` | Sensitive Preview environment variable; pooled Preview endpoint | Hosted runtime minimal DML | Schema and runtime role design | **Deferred; do not create yet** |
| GitHub environment | `production` | Environment | Protect Production credentials and manual deployment | Production workflow | **Prior verified exists; protections absent** |
| GitHub `production` environment | Required reviewers/branch policy | Protection settings | Hold credentials and deploy until explicit approval | Confirm current plan support | **Recommended; not configured** |
| GitHub `production` environment | `MIGRATION_DATABASE_URL` | Environment secret; direct Production endpoint | Run reviewed Production migrations as limited migrator | Schema and role design | **Deferred; do not create yet** |
| Vercel Production | `DATABASE_URL` | Sensitive Production environment variable; pooled Production endpoint | Hosted runtime minimal DML | Schema and runtime role design | **Deferred; do not create yet** |
| GitHub Actions | `NEON_API_KEY` | Secret, preferably environment-scoped | Only for automated branch lifecycle management | Decision to automate Neon branches | **Not needed for persistent branches; absent by design** |
| GitHub release environment | Signing/package credentials | Environment secrets | Publish or sign local tools | Distribution decisions | **Unresolved; absent** |
| Vercel project | Git integration | Provider setting | Automatic Vercel Git deploys | None | **Prior verified disconnected; keep disconnected for Actions-driven deploy** |
| Vercel project | Deployments/environment variables | Provider state | Hosted application delivery | Application and credentials | **Prior verified none** |
| Neon project | Persistent `preview` and `production` branches | Provider state | Data and credential separation | Database implementation | **Prior verified present and empty** |
| Neon project | Auth | Provider setting | Application identity | Auth checkpoint | **Prior verified disabled** |

Database roles remain deferred until schema ownership and migration tooling are selected. The intended design is SQL-created roles: a `NOLOGIN` schema owner, a login migrator limited to the application schema, and a login runtime role with minimal DML plus correct default privileges. Never use the database owner as the hosted runtime role. Do not duplicate runtime `DATABASE_URL` values into GitHub unless the build proves they are required there.

Before deployment workflows are enabled, decide whether to move `VERCEL_TOKEN` from repository scope into both protected deployment environments and remove the repository-scoped copy after validation. Environment scope is recommended so ordinary CI jobs cannot request the token. Any move, duplication, rotation, or deletion is a separate credential action requiring explicit approval.

## Ruleset configuration order

Do not require a check before it has reported successfully on the target branch; that can strand merges.

1. Implement secret-free CI with the exact `CI / required` display name.
2. Run it on test pull requests to `develop` and `main`; force selected-job failure, skip, and cancellation cases.
3. Configure `develop` rules to block force push/deletion and require pull requests plus `CI / required`.
4. Configure equivalent `main` rules after the release-branch flow is exercised.
5. Add the expected GitHub Actions app as the required check source where the UI supports it.
6. Configure Preview environment branch restrictions and secrets only after the Preview workflow is reviewable.
7. Configure Production reviewers/branch restrictions and secrets only after exact-SHA verification and serialization are reviewable.
8. Re-run the manual verification checklist before calling the pipeline wired.

GitHub documents environments and deployment protection rules as available to public repositories on current plans. Exact ruleset, reviewer, bypass, and merge-queue options must still be checked in this repository's UI before configuration; do not silently weaken the policy when an option is unavailable.

## Manual verification checklist

### Classifier and required gate

`[~]` means implemented and covered by the classifier/gate unit tests in CI, but not yet exercised by a live test pull request.

- [~] Docs-only pull request reports `CI / required` without app build or deploy.
- [~] Web-only pull request selects web checks but not unrelated local-tool checks.
- [~] CLI-only and MCP-only pull requests never request Vercel credentials or deploy Preview.
- [~] Contract/shared-core change fans out to all real consumers.
- [~] Unknown path fails safe by selecting broad checks.
- [~] A failed selected job makes `CI / required` fail.
- [~] A cancelled or unexpectedly skipped selected job makes `CI / required` fail.
- [~] An intentionally unselected job is explained and does not fail the gate.
- [ ] Check names are unique and the configured requirement matches exactly.

### Fork and credential isolation

- [ ] Fork pull request completes secret-free CI without environment access.
- [ ] Fork pull request cannot invoke a privileged `pull_request_target` path that executes its code.
- [ ] A hosted pull request never deploys Preview; a hosted `develop` push deploys Preview only after selected checks pass.
- [ ] Logs, summaries, caches, and artifacts contain no credentials or connection strings.
- [ ] Workflow permissions are read-only except narrowly scoped trusted jobs.

### Preview

- [ ] A minimal non-sensitive hosted verification slice exists before bootstrap is attempted.
- [ ] The first Production-class bootstrap deployment uses `--skip-domain` and has no Production/custom-domain alias.
- [ ] Its deployment record matches the intended project, exact source SHA, and Production classification.
- [ ] The separate explicit Preview pull/build/deploy uses matching Preview targets.
- [ ] The Preview deployment record matches the intended project, exact source SHA, and Preview classification, with no Production-domain alias.
- [ ] Prebuilt suitability, including System Environment Variable behavior, is proven for the selected framework.
- [ ] Hosted change builds an artifact/manifest for the pull-request SHA.
- [ ] Preview migration uses only the Preview direct migrator role.
- [ ] Preview runtime uses only the Preview pooled runtime role.
- [ ] Smoke tests target both the returned deployment URL and the stable Preview alias; each must return HTTP 200 with the expected health payload before the stable gate passes.
- [ ] Docs-only/local-tool-only changes create no deployment.

### Production

- [ ] Manual run rejects a SHA not on `main` or lacking successful branch CI evidence.
- [ ] Manifest, deployment, and smoke record the same full source SHA.
- [ ] Environment approval occurs before Production secrets are available.
- [ ] Two Production requests serialize; an in-progress migration is not cancelled.
- [ ] Migration failure prevents deploy and does not run an automatic down migration.
- [ ] Deploy or smoke failure follows the documented traffic/schema recovery decision.

### CLI/MCP release

- [ ] Candidate tests cover core, CLI, MCP protocol, and all supported API versions.
- [ ] Every published artifact has and verifies a checksum.
- [ ] JSON/stdout and MCP protocol fixtures contain no update notices.
- [ ] Update lookup cache, timeout, failure, and opt-out behavior are tested.
- [ ] Install/update instructions require explicit action and document MCP restart.
- [ ] Signing, packaging, versioning, target platforms, and compatibility policy are approved before release automation exists.

## Open decisions before implementation

1. ~~Final monorepo directories and the path-to-component map.~~ Proposed in the Path map; confirm once the skeleton merges.
2. Whether CI and Preview are one workflow or use a privilege-separated handoff that preserves exact-SHA identity.
3. Pull-request approval count, CODEOWNERS, bypass policy, merge method, stale-review behavior, and merge queue.
4. Whether Preview success is required for every hosted `develop` push or only for release-candidate classes (now: every `develop` push that selects Preview); the current recommendation requires it whenever selected.
5. Production environment reviewer and branch restrictions actually available on the current GitHub plan.
6. Vercel Go workload fit, framework/root settings, smoke endpoints, and whether web/API remain one hosted unit.
7. Execution-time validation of the documented Vercel bootstrap procedure and prebuilt-build suitability before automatic Preview is enabled.
8. Whether to relocate `VERCEL_TOKEN` from repository scope to both protected deployment environments.
9. Migration tool, schema ownership, backup/restore rehearsal, compatibility window, and recovery runbook.
10. Temporary Postgres provisioning mechanism for integration tests. It must use synthetic data and no Production credentials.
11. Hosted API versioning and minimum supported CLI/MCP compatibility policy.
12. CLI/MCP versioning, platforms, packaging, signing, provenance, release trigger, and update manifest.

## Current platform notes and official references

These capabilities were checked against official documentation on 2026-11-01. Provider behavior and plan availability can change; recheck immediately before applying settings.

- GitHub required checks require exact, unique check names, and skipped dependencies need deliberate aggregation: https://docs.github.com/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks
- GitHub ruleset options and required-check sources: https://docs.github.com/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets
- GitHub environments, secrets, and deployment protection rules: https://docs.github.com/actions/deployment/targeting-different-environments/using-environments-for-deployment
- GitHub fork and Actions settings: https://docs.github.com/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository
- GitHub Actions secure-use guidance, including third-party Action pinning: https://docs.github.com/actions/reference/security/secure-use
- GitHub `pull_request_target` security boundary: https://docs.github.com/actions/reference/security/securely-using-pull_request_target
- GitHub event behavior, including `merge_group`: https://docs.github.com/actions/reference/workflows-and-actions/events-that-trigger-workflows
- GitHub concurrency behavior: https://docs.github.com/actions/writing-workflows/choosing-what-your-workflow-does/control-the-concurrency-of-workflows-and-jobs
- Vercel's official GitHub Actions pattern for `vercel build` and `vercel deploy --prebuilt`: https://vercel.com/docs/git/vercel-for-github
- Vercel CLI deployment behavior: https://vercel.com/docs/cli/deploying-from-cli
- Vercel `pull`, `build`, and `deploy` target and prebuilt options: https://vercel.com/docs/cli/pull, https://vercel.com/docs/cli/build, https://vercel.com/docs/cli/deploy
- Vercel's explicit warning that a new project's first deployment is Production even when Preview is requested: https://vercel.com/docs/networking/bring-your-own-cloud/deploy-with-cli
- Neon recommends pooled application connections and direct connections for schema migrations: https://neon.com/docs/reference/compatibility

The references establish current provider capabilities, not proof that this repository is configured or that the final application fits provider limits.
