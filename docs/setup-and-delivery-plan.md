# Setup and Delivery Plan

Status: planning baseline with a partially prepared provider connection scaffold, 2026-11-01

This document separates authorized project setup from future implementation. Completing a provider setup step does not authorize application code or deployment.

The operational CI/CD configuration guide is maintained in [CI/CD Configuration Plan](ci-cd-plan.md). The provider resources below are not a wired or validated delivery pipeline.

## Setup targets

### GitHub

- Create one public repository named `meal-planner` in the verified existing personal account.
- Publish only sanitized planning documentation.
- Use `main` as the default branch.
- Do not add CI workflows during this milestone.
- Verify repository visibility, remote URL, default branch, and the exact published commit.

### Neon

- Create one separate project named `meal-planner` in the existing account's free plan.
- Do not reuse or modify another application's project, branches, databases, roles, Auth configuration, or credentials.
- Do not create tables, migrations, seed data, Auth providers, OAuth credentials, API keys, or connection files.
- Record the dashboard link and non-secret project identifier only in the ignored local setup receipt.

Neon's current public materials describe Free projects as including Postgres and Managed Auth, with per-project limits. Limits and feature availability can change, so they must be rechecked before implementation.

### Vercel

- Create one separate project named `meal-planner` in the existing personal Hobby account.
- Do not reuse or modify another application's project or deployment.
- Do not deploy a placeholder application.
- Leave the Vercel Git integration unconnected. Future delivery is expected to use GitHub Actions with narrowly scoped Vercel credentials; the deploy workflow and credentials are deferred implementation work, while secret-free CI scaffolding starts ahead of Phase 3.
- Record the dashboard link and non-secret project identifier only in the ignored local setup receipt.

Vercel's Hobby plan is currently free for personal projects within published limits. Go HTTP functions are officially supported, but workload fit remains an implementation checkpoint.

## Setup verification checklist

### Repository

- [x] Local Git repository initialized with `main`.
- [x] Documentation reviewed for private data and account identifiers.
- [x] Local secret-pattern scan passes.
- [x] Initial documentation commit created.
- [x] Public remote created in the verified account.
- [x] `origin` points to the exact intended repository.
- [x] Initial commit pushed and visible on `main`.
- [x] Remote visibility independently verified as public.

### Neon project

- [x] Existing account and free-plan context verified.
- [x] No matching project exists.
- [x] Separate project created with the exact intended name.
- [x] A persistent `preview` child branch was created from the still-empty `production` branch so Preview never needs the production connection.
- [x] No schema, migration, seed data, Auth provider, OAuth grant, or API key added.
- [x] Non-secret project and branch identifiers and the dashboard link recorded locally, outside Git.

### Vercel project

- [x] Existing personal Hobby account verified.
- [x] No matching project exists.
- [x] Separate project created with the exact intended name.
- [x] No placeholder deployment created; the project has no production or preview deployments.
- [x] Git integration remains unconnected and no new persistent GitHub grant was accepted.
- [x] One project-scoped deployment token was created and installed as an encrypted GitHub Actions repository secret; no Vercel environment variable or delivery workflow was created.
- [x] Non-secret identifier and dashboard link recorded locally, outside Git.

### CI/CD connection scaffold

The following non-secret setup is complete:

- GitHub deployment environments named `preview` and `production` exist. They do not yet contain environment-scoped secrets, protection rules, or branch restrictions.
- The repository contains non-secret Actions variables named `VERCEL_ORG_ID`, `VERCEL_PROJECT_ID`, `NEON_PROJECT_ID`, `NEON_PRODUCTION_BRANCH_ID`, and `NEON_PREVIEW_BRANCH_ID`. Their values are managed in GitHub, not repeated in this public document.
- The encrypted repository Actions secret `VERCEL_TOKEN` exists. Its value is not readable from GitHub and is not stored in this repository or the local setup receipt.
- Neon has separate persistent `production` and `preview` branches. The preview branch was created while production was empty, contains no application data, does not expire automatically, and has a separate branch endpoint.
- The Vercel project remains disconnected from Git and has no environment variables or deployments.

These steps prepare identifiers and isolation boundaries only. No connection has been exercised because there is no workflow or application build yet.

Protected material status (prior verification; not revalidated by a workflow):

- `VERCEL_TOKEN` is installed and was verified as restricted to the intended project with a recorded expiry. Its provider-side identifier and expiry are intentionally kept out of this public document. Vercel does not offer deployment-only permissions for this token type, so future workflow gates and protected branches remain part of the security boundary. No workflow has used or validated the token yet.
- The remaining database connections require explicit approval at the moment they are created or installed:
  - Preview database connection: proposed as a Neon connection whose endpoint targets the preview branch and whose role is limited to the application database, stored only in the Vercel Preview environment and in the GitHub `preview` environment if build or migration steps require it. Neon role isolation, password expiry, and the final rotation mechanism must be verified before approval; absent provider-enforced expiry, use a documented 90-day rotation.
  - Production database connection: the same model, with an endpoint targeting the production branch and storage only in the corresponding Production environments. Preview and CI must never receive this value.

A Neon API key is not part of the initial minimum. Add a project-scoped `NEON_API_KEY` only if later workflow design actually automates branch creation, reset, or deletion. Neon Auth session keys, application login configuration, release signing keys, and package registry credentials are separate concerns and must not be bundled into deployment credentials.

## Approved branch and release policy

The detailed trigger, check-name, artifact-integrity, migration, public-fork, and manual-verification plan is in [CI/CD Configuration Plan](ci-cd-plan.md). This section records the higher-level policy.

### Branch flow

`main` and `develop` are long-lived branches. Normal changes follow this flow:

```text
feature branch -> develop -> temporary release branch -> main
```

The temporary release branch isolates the final promotion into `main`; it is not a replacement for `develop`. Only disposable feature and release branches are cleanup candidates. Long-lived branches are never part of routine merge cleanup.

Recommended repository protection, not yet configured:

- block deletion of both `main` and `develop`;
- block force pushes to both `main` and `develop`;
- require the stable CI gate before merging into protected branches; and
- limit branch-cleanup automation to branches explicitly classified as disposable.

The exact rules, exceptions, administrator bypass policy, and release-branch naming convention remain implementation decisions.

### Preview and production deployment

- Pushes to `develop` that affect the hosted web client or API deploy an automatic Vercel Preview (and move the optional stable `PREVIEW_ALIAS`) only after their relevant CI checks pass. Pull requests run checks only.
- Documentation-only and local-tool-only changes do not deploy the hosted web application.
- Pull requests from public forks receive secret-free checks only. Untrusted code never runs with deployment credentials or other protected secrets.
- Production deployment is deliberate and manual after CI completes on `main`.
- Production deploys the exact final `main` commit whose `main` CI passed. A pre-merge release-branch SHA is not sufficient, even when its content appears equivalent.

The deployment workflows, deployment credentials, environment protections, and approval gates that enforce this policy are deferred implementation work; secret-free CI starts ahead of Phase 3 on a hello-world verification slice. The empty GitHub environments and non-secret identifiers already exist.

## Approved affected-component CI behavior

CI runs the checks required by the affected components rather than running every job for every change.

- Web changes run the relevant web checks.
- Hosted Go API changes run the relevant Go API checks.
- Local core, CLI, or MCP changes run the relevant local Go checks and API-compatibility checks.
- Contract or cross-component changes run the relevant contract and integration checks.
- Shared core, shared contracts, toolchain, dependency-management, or workflow changes fan out to every dependent component.
- Independent relevant checks run in parallel.
- Integration tests use temporary Postgres with synthetic data. Preview and CI never receive production recipes, production Auth state, or other production user data.

A stable required gate is the single merge-facing result. It must fail closed: it reads the affected-component decision, understands intentional skips, requires every selected check to report success, and fails on a missing, unknown, or unexpectedly skipped result.

### Proposed workflow topology

The exact workflow files and job count are not approved yet. A proposed shape is:

1. Detect affected components and publish a machine-readable check plan.
2. Run independent web, hosted Go, local Go, and contract or integration groups in parallel when selected.
3. Run an automatic Preview deployment for a hosted change pushed to `develop` after the relevant checks succeed.
4. Evaluate all selected and intentionally skipped work in the stable fail-closed required gate.
5. Offer a manual Production action only for the exact `main` commit that passed the required gate.

Implementation must preserve the behavior above even if the eventual workflow uses more or fewer jobs.

## Delivery phases

### Local draft tools (deferred from the web release)

The executable and file-store surface of local recipe-draft epic #19 is deferred from the November web release. Existing development remains in develop and pre-split release history for a separately reviewed local-tool release, including its protocol, storage and host-compatibility acceptance. The version-only CLI and MCP scaffold, archive builds and separate CLI publication gates remain unchanged in scope.

The transport-independent draft core, tests and synthetic fixtures remain because hosted manual recipes reuse the strict JSON-string validator. The [local draft contract](local-draft-contract.md) is retained as the shared-core specification and deferred design, not an installation guide. The local slice does not satisfy any numbered phase's exit gate and does not imply hosted draft save, source import, authentication activation, database setup or new provider credentials.

### Phase 0: planning and provider organization

Deliver the sanitized documents in this repository and create empty provider projects. Do not implement or deploy the application.

Exit gate:

- product decisions, proposals, and unresolved questions are clearly separated;
- architecture boundaries and early platform checkpoints are documented;
- public repository contains no sensitive material;
- provider resources are separate and free-tier; and
- any blocked external step has a precise owner and next action.

### Phase 1: decision closure

Resolve only the open rules needed for the first vertical slice: eligibility, review requirements, minimal feedback, success criteria, and change propagation. Produce an experience map and approve the initial contract resources.

Exit gate:

- launch scope is approved;
- safety and provenance policy is approved;
- critical domain rules have examples and acceptance criteria; and
- no implementation relies on an unanswered product decision.

### Phase 2: platform verification

Implement disposable verification slices for Vercel-hosted Go HTTP, Neon connectivity, Neon Auth token validation, and local CLI authentication. These checks should remain minimal and should not expand into feature work.

Exit gate:

- Vercel workload fit is accepted or an alternative host is selected;
- authenticated ownership isolation is demonstrated;
- secrets remain server-side and environment-separated; and
- a local client can authenticate without database credentials.

### Phase 3: contract and persistence foundation

Create the executable API contract, generated clients where appropriate, database model, migrations, and contract tests. CI scaffolding starts ahead of this phase on a hello-world verification slice; Phase 3 extends it to the contract and persistence checks once those artifacts exist.

Exit gate:

- contract review passes;
- migrations can apply and roll forward safely in isolated environments;
- database access and transaction tests pass; and
- CI enforces the agreed quality gates without exposing secrets.

### Phase 4: first vertical slice

Implement one narrow flow from reviewed saved recipes to an on-demand three-dinner plan. Include only the product rules approved for this slice.

Exit gate:

- end-to-end ownership and authorization tests pass;
- active and total time are represented correctly;
- plan generation is available without a live agent; and
- derived shopping and prep behavior is either implemented consistently or explicitly excluded.

### Phase 5: launch-capability expansion

Add approved shopping, prep, swaps, responsive cooking, editing, and feedback capabilities in small vertical slices. Validate data invalidation and progress behavior with every change.

Exit gate:

- launch acceptance criteria pass on phone and tablet layouts;
- privacy and authorization review passes;
- observability excludes sensitive content; and
- recovery and rollback procedures are documented and tested.

## Maintenance principles

- Prefer clear module ownership and standard HTTP contracts over clever provider coupling.
- Keep business rules transport-independent and covered by examples and tests.
- Treat generated clients and migrations as reviewed source artifacts.
- Add services only when a verified requirement justifies them.
- Revisit provider limits and documented behavior at each release gate.
- Record architectural decisions when a proposal becomes settled or a settled choice changes.

## Explicitly deferred setup

- Vercel deployment and production domain configuration.
- Production-grade CI beyond the hello-world verification slice (secret-free CI scaffolding starts ahead of Phase 3).
- Neon schema, Auth enablement or configuration, application database roles, connection installation, and API keys.
- Microsoft Graph access and source-note import.
- Object storage buckets or additional providers.
- Monitoring, analytics, email, and other third-party integrations.

## Official setup references

- Creating and managing Neon projects: https://neon.com/docs/manage/projects
- Current Neon Free-plan announcement: https://neon.com/blog/neon-free-plan-1-gb-per-project
- Vercel account plans: https://vercel.com/docs/plans
- Vercel Hobby plan: https://vercel.com/docs/plans/hobby
- Creating Vercel projects through the API: https://vercel.com/docs/rest-api/projects/create-a-new-project
- Vercel GitHub integration: https://vercel.com/docs/git/vercel-for-github
- Vercel access-token scopes: https://vercel.com/docs/accounts/access-tokens
- GitHub Actions variables: https://docs.github.com/actions/learn-github-actions/variables
- GitHub deployment environments: https://docs.github.com/actions/deployment/targeting-different-environments/using-environments-for-deployment
- Neon API keys: https://neon.com/docs/manage/api-keys
- Neon branch automation: https://neon.com/docs/reference/api/branches
