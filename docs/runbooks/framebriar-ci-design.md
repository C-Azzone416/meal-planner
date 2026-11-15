> Historical design from the feature branch. Current execution and trust boundaries are defined by [Framebriar CI wiring](framebriar-ci-wiring.md). Do not use historical direct-install workflow proposals to replace the current protected evidence flow.

# Proposed sole-owner protected Framebriar CI

Design only. No environment, credential, permission, workflow, launch or required-check change is performed or authorized by this document. Preserve the existing push-only reader boundary until the owner explicitly approves a replacement. Do not expose the reader directly to pull-request workflows, use `pull_request_target`, or grant this public repository broad package access through `GITHUB_TOKEN` as a shortcut.

## Actual rules versus this proposal

Read-only inspection confirms the existing owner/admin exemption is already configured: main and develop review rules allow administrators to bypass through a pull request. Develop requires zero approvals; main's normal one-approval requirement has the administrator PR-only exemption. Preserve that exemption. Main's strict `required` CI check has no bypass, and both branches prohibit deletion and force-push. These checks are separate from approval count.

The existing Preview/Production environments have no required-reviewer rules or branch restrictions. `framebriar-ci` does not yet exist. A previous draft proposed an independent reviewer and prevent-self-review; **that was an unimplemented design choice, not a GitHub requirement or a missing owner exemption**. The current sole-owner arrangement does not require inviting another contributor.

GitHub Free supports environment reviewers and deployment-branch restrictions for this public repository. The package reader remains absent and unauthenticated package installation returns 401. Package access and a trusted test path remain blockers distinct from review exemptions.

## Proposed owner approval boundary

Create a dedicated GitHub Actions environment named **`framebriar-ci`**, only after explicit approval:

- Required reviewer: the existing repository owner, Caroline, selected using the verified GitHub account in the actual setup operation. Do not publish personal account details or provider IDs in this document.
- `prevent_self_review=false`: the owner can approve a run she dispatched. This is intentional for a sole-owner repository, not an independent-human review claim.
- `can_admins_bypass=false`: require the explicit environment approval interaction rather than a bypass operation. This environment-only setting must not remove the existing branch ruleset owner exemption.
- Deployment branch policy: selected **branch exactly `main`**, no tags or wildcard branches. The workflow executes from main even when its separately validated input SHA identifies a feature PR commit.
- No Vercel deployment token, database credential, enrollment key or invitation in this environment.

The trusted main workflow, the exact source SHA and the package dependency changes must be inspected before approval. The owner is the trusted principal who can maintain main; this design does not attempt to protect the owner from her own deliberate edits. If additional contributors gain write access, revisit the trust model before granting them access to the gate.

## Exact package credential proposal

- Provider: GitHub Packages npm registry, `https://npm.pkg.github.com`.
- Package: `@masra91/framebriar`, pinned `0.4.1` and reviewed lockfile integrity.
- Credential: GitHub **personal access token (classic)**, scope **`read:packages` only**. No `repo`, `write:packages`, `delete:packages`, workflow or administration scope without separately verified necessity and approval. Fine-grained PAT compatibility must not be assumed for this registry.
- Principal: an owner-approved existing reader with Read permission on this package. Confirm the package owner's permission and entitlement privately. A classic PAT can read other packages accessible to its principal; it is not intrinsically limited to this one package. Prefer the narrowest existing principal. Creating an account or granting new access needs separate approval.
- Sole destination: environment `framebriar-ci`, secret **`FRAMEBRIAR_PACKAGE_READ_TOKEN`**. Not a repository secret, persistent local npmrc, Vercel setting, cache or artifact.
- Installation mapping: `NODE_AUTH_TOKEN` in only the isolated install step, never in command arguments. Registry credential transmission is only for the approved package download.
- Expiry: owner-approved short lifetime with named rotation responsibility. Confirm principal, scope, destination and expiry at credential creation/transmission time; this design is not that approval.

Official npm registry guidance supports a classic PAT with at least `read:packages` for packages associated with other private repositories. npm supports granular package permissions; the guidance does not justify broad source-repository `repo` scope. If the actual package permissions prevent a narrow reader, stop and collect evidence rather than broadening access.

## Trusted workflow outline

Proposed `.github/workflows/framebriar-reviewed-ci.yml` accepts `workflow_dispatch` only, with PR number and exact lowercase 40-character SHA. Never interpolate untrusted input into shell source. The trusted workflow lives on main; the target PR must not supply workflow steps, reusable actions or credential-bearing commands.

1. **Validate without the reader.** Require main dispatch. Resolve and record the immutable workflow revision (`github.workflow_sha`), verify it against the approved trusted main revision, and validate the target PR belongs to this repository with an allowed base. Require the supplied SHA to equal the PR's current head; capture base/head identity and the exact manifest hashes. Use only trusted validation code. Show the owner the target SHA, workflow revision and dependency diff before the environment approval.
2. **Approve explicitly.** The gated job references `framebriar-ci`. The owner approves that exact SHA and workflow revision, including running that source with the private SDK available. Recheck the PR head and source/manifests after approval; a changed head requires a new run/approval, never an automatic substitution.
3. **Install with fixed trusted code.** Checkout/download exactly the validated source without persisted checkout credentials. Copy only package.json and package-lock.json into a temporary install directory. Generate the fixed scoped registry npmrc, separate empty user/global npm configs, fresh HOME and `env -i`. Run only `npm ci --ignore-scripts` while authenticated, using a temporary cache deleted at exit. No repository-controlled action, lifecycle, build, test or shell script may run beside the reader. Transfer only node_modules back to the worktree, not logs, npmrc or caches.
4. **Test without credentials.** A fresh step with no reader executes the reviewed lifecycle validation, unit tests, lint/typecheck, disposable Go/PostgreSQL browser fixtures and target/capture artifact checks. Never contact live Framebriar or a live database. Do not upload SDK source, node_modules, npm caches or environment files. Code under test can still read the installed private SDK and potentially disclose it, so explicit exact-SHA approval is essential even after the token disappears. Token isolation alone does not approve arbitrary code.
5. **Report from a clean trusted runner.** No target checkout, no package-reader environment and no target artifact execution. Use the ephemeral workflow `GITHUB_TOKEN` with only `checks:write` and necessary Actions/PR reads. Verify the trusted workflow/run revision, successful required jobs, immutable head and current PR identity before creating `framebriar-reviewed-web` on that exact SHA. Link the trusted run. A dispatch run's native head SHA is main, not its input SHA, so a green dispatch badge alone cannot satisfy the feature PR gate.

Record only safe evidence: source/workflow SHA, manifest hashes, test counts, target names and outcomes. Avoid logs that dump package files. Production enablement and deployments are not part of this CI gate.

## Required-check transition and honest limits

The existing private-package frontend PR job fails without access. Merely adding a dispatch workflow does not fix that failure. A separately reviewed CI change must split public checks from approved private-dependency frontend checks and wire the aggregate/branch requirements so **a missing approved frontend run cannot produce a mergeable green gate**. Keep main's existing `required` check non-bypassable and its strict up-to-date policy; preserve the owner's PR-only review exemption. Propose `framebriar-reviewed-web` as the additional required frontend check on both main and develop, with the exact final contexts verified before configuration. Do not weaken checks while transitioning.

GitHub Actions is a shared expected-source identity. Another workflow can emit the same check name, including a PR-controlled workflow; `framebriar-reviewed-web` plus expected source “GitHub Actions” is **not cryptographic proof that the trusted workflow ran**. The owner must verify the linked trusted-main dispatch run, workflow revision and input SHA before merging. A PR that edits workflows/CODEOWNERS or attempts a spoof must not be accepted merely because similarly named checks are green. Preserve trusted validation in main and never treat a claimed target artifact or target-generated status as authorization.

For a gate that must mechanically resist check-name spoofing without relying on the sole owner's final provenance review, use a separately approved dedicated GitHub App/restricted CI reporter or another supported required-workflow identity mechanism. That entails additional access/setup and must be designed and approved independently; it is not silently included here. Do not advertise the minimal Actions reporter as providing that stronger guarantee.

`tools/deploy/verify-ci.sh` must continue to require successful ci.yml push-main evidence for the exact release SHA. A dispatch run is not a substitute. Automatic push/deploy installations currently expect a reader separately; the new environment-only secret does not automatically reach those jobs. Before activation, explicitly design and approve how those trusted builds use the same protected path. Do not duplicate the token into repository scope or add package permissions to fix that gap by inference.

## Separate dependency-free bootstrap PR

Do not try to land the blocked feature PR by removing its failing checks. Prepare a small bootstrap PR against the actual current main baseline, with no new private dependency:

1. Add only the trusted dispatch/validation/reporter code, negative tests for provenance/head changes, and the intended public/private CI transition. Pin actions and the trusted workflow revision contract. Include failure cases for fork PRs, changed heads, missing approvals, forged job results and absent frontend evidence. Keep private execution unavailable until the protected environment exists.
2. Run the baseline's ordinary dependency-free CI and inspect the exact workflow diff. Merge through the existing owner PR exemption only after existing mandatory CI succeeds; no review-bypass change is needed. Do not activate a gate that cannot yet produce evidence.
3. In a separately approved configuration operation, create and verify the owner-reviewed environment, main-only branch policy, self-review allowance and disabled environment bypass. Only then approve/provision the exact reader credential.
4. Run one owner-approved exact feature SHA through the trusted path. Validate clean installation, frontend/browser/artifact results and reporter source/SHA correspondence.
5. Coordinate required-check activation and the CI split so neither an absent trusted frontend result nor the old permanently blocked private PR install is mistaken for a pass. Verify both an approved success and deliberately missing/mismatched evidence fail-closed behavior before relying on this route for merge.
6. Finish the separate trusted push/deploy access path and verify release evidence remains exact-SHA. Then resume the feature PR's normal review/merge process. None of these steps deploys or enables Preview implicitly.

This is an implementation outline, not an implemented bootstrap. The exact check transition must be reviewed against current baseline workflows before coding; do not use an unprotected merge window as the solution.

## Approvals still needed

- Approve the owner-reviewed environment/security configuration described above, without altering branch owner exemptions.
- Approve credential creation/grant/transmission with verified principal, `read:packages`, expiry and destination `framebriar-ci` / `FRAMEBRIAR_PACKAGE_READ_TOKEN`.
- Approve the concrete bootstrap workflow/required-check transition and any trusted push/deploy access adjustment after its exact diff and effects are known.
- Explicitly approve each reviewed SHA's access to the installed private SDK and verify the resulting trusted evidence before merge.

A general request to complete setup establishes intent, but is not a replacement for required action-time confirmation of new persistent credential access or security-sensitive configuration. No second approver/contributor is required by this proposal.

## Official references

- [Working with the npm registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-npm-registry)
- [About permissions for GitHub Packages](https://docs.github.com/en/packages/learn-github-packages/about-permissions-for-github-packages)
- [Deployments and environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
