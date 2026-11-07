# Proposed protected Framebriar CI

Design only. No environment, credential, permission, reviewer, workflow or required-check change is authorized or performed by this document. The existing implementation retains push-only reader scope. Do not expose the reader to pull-request workflows, use `pull_request_target`, or grant this public repository broad package access through `GITHUB_TOKEN` as a shortcut.

## Verified baseline and blockers

Read-only inspection found active rulesets rather than classic branch protection. Main requires the GitHub Actions `required` check with strict up-to-date checking. Main also requires one PR approval, dismisses stale approvals and resolves review threads, but does not require code-owner approval and permits an administrator PR-only bypass of the review rule. Develop requires a PR and resolved threads but zero approvals and no required status checks, with the same review bypass. Both branches block force-push and deletion. Existing deployment environments have no reviewer or branch restrictions. Workflow default token permissions are read-only; Actions is enabled.

GitHub Free supports the relevant environment protection features for this public repository. Only one repository collaborator was found, with no outstanding collaborator invitations. **An independent reviewer must be selected and given the minimum appropriate access before a prevent-self-review gate can operate.** Do not silently turn off self-review prevention or reuse an unprotected deployment environment.

The private package reader is absent. An unauthenticated package fetch returns 401. The proposed workflow cannot run until approved access exists. Package-owner permission to give the selected reader principal access has not been verified.

## Credential and destination

- Provider: GitHub Packages npm registry, `https://npm.pkg.github.com`.
- Package: `@masra91/framebriar`, exact version `0.4.1`, using the reviewed lockfile integrity.
- Credential: GitHub **personal access token (classic)** with `read:packages` only. Do not request `repo`, `write:packages`, `delete:packages`, workflow or administration scopes without separately verified necessity and approval. Fine-grained PAT support must not be assumed for this registry.
- Principal: an owner-approved existing package reader with Read permission on this package. Prefer a principal whose package access is narrowly limited. A classic PAT is not intrinsically restricted to one package: it can read other packages that its principal is entitled to read. State that scope before provisioning. Creating a dedicated account, granting access or creating the token is a separate approval, not part of design preparation.
- Destination: new GitHub Actions environment **`framebriar-ci`**, secret **`FRAMEBRIAR_PACKAGE_READ_TOKEN`**. Never a repository secret, Vercel variable, local persistent npmrc, artifact or cache. No public variable is required.
- Runtime transmission: map to `NODE_AUTH_TOKEN` only during the isolated install step. Do not place the token in an npm command argument. The registry receives the credential solely to download the approved package.
- Lifetime: choose an owner-approved short expiry, document rotation ownership and test replacement before revoking the previous token. Do not generate or transmit a credential until the exact principal, scope and destination are approved.

GitHub's npm registry documentation explicitly supports a classic PAT with at least `read:packages` for packages associated with other private repositories. npm supports granular package permissions; these docs do not establish a need for broad source-repository access. If the package's actual permission model prevents this narrow reader, stop and obtain evidence rather than adding `repo` scope.

## Protected environment and branch requirements

Create `framebriar-ci` only after approval, with:

1. One explicitly selected independent reviewer who has at least repository Read access; identify the actual person privately before configuring. If the owner dispatches a run, that same owner cannot approve it with self-review prevention enabled.
2. Prevent self-review enabled; administrator protection bypass disabled.
3. Selected deployment **branch** exactly `main`, not tags, wildcard names or merely “protected branches.” This binds environment use to trusted main workflow code. The input SHA may name a reviewed PR commit but is not the workflow execution ref.
4. No deployment credentials, runtime database/key secrets or invitations in this environment.

Before supplying the secret, strengthen main/develop review rules in an explicitly approved change: at least one independent approval, stale-review dismissal, code-owner review for `.github/**`, `tools/**` and deployment config, and no administrator bypass of these review requirements. Update CODEOWNERS with the approved available reviewer. Protect CODEOWNERS itself. Keep existing deletion/force-push rules and main strict status-check policy. The current single-collaborator arrangement cannot satisfy these independent review requirements; choose a reviewer first.

Environment branch restrictions are not enough if an author can merge altered workflow code to main without independent review. Conversely, approving a job does not prove the input code is safe: reviewers must explicitly inspect the exact SHA and dependency changes before releasing package access.

## Workflow and immutable evidence

Proposed file: `.github/workflows/framebriar-reviewed-ci.yml`, event `workflow_dispatch` only, executed from main. Inputs are a PR number and full 40-character lowercase head SHA, supplied through environment variables rather than interpolated shell code.

1. **Validate** on a fresh runner with minimal read permissions. Require the dispatch ref to be main and verify the workflow revision is the reviewed main revision. Resolve the PR through GitHub, require the same base repository and approved base branch, match the exact current head SHA, and record the base/head and manifest hashes. No package reader, checkout scripts, code execution or deployment occurs. Put the exact reviewed SHA and dependency diff link in the approval summary. Revalidate after approval to catch a changed PR head.
2. **Approved build** references `framebriar-ci`. Download/check out the exact validated SHA with checkout credentials not persisted. Copy only package.json and package-lock.json into a temporary install directory. Use the fixed scoped registry npmrc, separate empty user/global npm configs, a new HOME and `env -i`. Authenticate only `npm ci --ignore-scripts`, use a temporary cache and delete it on exit. Transfer only node_modules into the worktree, never the token, npm log/cache or private dependency artifact. A fresh credential-free step performs the reviewed lifecycle validation, tests, lint, typecheck and eight target/capture artifact checks. Do not use repository-controlled actions or scripts while the reader is present. No live Framebriar report or deployment is part of this gate.
3. **Reporter** on a separate clean runner, no target checkout, no package-reader environment and no target artifacts executed. It has only narrowly scoped `checks: write` plus necessary Actions/PR reads using the ephemeral workflow `GITHUB_TOKEN`. Validate the trusted workflow run, successful required jobs, workflow revision, PR and immutable input SHA. Publish check **`framebriar-reviewed-web`** on that exact head SHA with a link to the trusted run. Reject changed heads, rerun failures and incomplete evidence. A dispatch run's native head SHA is main, so its native green status alone is not PR evidence.

No node_modules, SDK source, caches or environment files are uploaded. Review and avoid logs that dump private package files. Safe evidence consists of source/workflow SHA, manifest hashes, test counts, target names and pass/fail results. This private-package code becomes available to the approved input code during testing even after the token disappears; the approval is therefore also approval to run that reviewed code with the package present.

## Required-check transition, not a skip workaround

Propose adding required check `framebriar-reviewed-web` with expected source GitHub Actions to **both main and develop**. Preserve main's existing `required` check; add it to develop as part of the approved gate alignment. Keep strict up-to-date checking so a changed target base requires fresh evidence under the documented policy.

Separately refactor PR CI so its `required` aggregate covers all ordinary public-dependency Go/contract/integration/workflow checks while the private-dependency frontend checks are supplied exclusively by the mandatory reviewed check. Never simply skip frontend CI and leave the old green aggregate as the only required check. Push CI remains a distinct exact-main-SHA release gate; `tools/deploy/verify-ci.sh` must continue to require successful ci.yml push-main evidence and must not accept a dispatch run in its place.

Expected-source GitHub Actions is not unique to this reporter: another Actions workflow could emit the same check name. The required independent code-owner review for every workflow change is therefore an essential part of this proposal, not optional hardening. If a stronger isolated check identity is required, use a separately approved dedicated GitHub App/restricted CI reporter instead; that creates additional persistent access and is outside this minimal proposal. Do not claim the Actions check name alone proves trusted provenance.

Bootstrap this without a bypass shortcut: prepare a small dependency-free workflow/configuration PR first, run existing checks, review it independently, and coordinate the protected-environment and required-check activation in a recorded owner-approved window. Only then run the feature SHA through the new gate. Do not change required checks until the corresponding trusted check can actually run; do not merge the feature under an absent check. Existing automatic push/deployment installs also need a separately reviewed environment-scoped access plan before repository-scoped reader access is removed or provisioned; this proposal grants none by implication.

## Exact next decisions

1. Select an independent repository reviewer; approve their minimum access if none is available.
2. Approve implementing the trusted workflow and CI/check split, plus the explicit main/develop review and required-check changes above.
3. Identify the existing package-reader principal; confirm package-owner authorization and its Read access. Approve the classic `read:packages` token, expiry and sole destination `framebriar-ci` / `FRAMEBRIAR_PACKAGE_READ_TOKEN` at credential creation/transmission time.
4. Configure and verify protections before loading the secret, then approve one reviewed exact-SHA test run. Record results without private data.

Until those decisions, preserve the blocked checks and denied PR-secret boundary. Preparation is not a statement that protected CI, private installation or a rollout is complete.

## Official references

- [Working with the npm registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry)
- [About permissions for GitHub Packages](https://docs.github.com/en/packages/learn-github-packages/about-permissions-for-github-packages)
- [Deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
