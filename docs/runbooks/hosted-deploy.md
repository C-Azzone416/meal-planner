# Hosted deploy runbook

Operator procedures for the Vercel-hosted web/API. The policy is in [ci-cd-plan.md](../ci-cd-plan.md); this page only says how to run the workflows. Nothing here is done automatically, and every Vercel-touching run needs explicit approval at the time.

Workflows: `.github/workflows/vercel-bootstrap.yml` (one time), `preview.yml` (called from `ci.yml`), `deploy-production.yml` (manual).

## Build/deploy split

In every workflow, `vercel build` (which runs repository install and build scripts) never runs in a job that has `VERCEL_TOKEN`. Each target runs as three jobs:

1. **pull**: has the token and no checkout, so no repository code is present. Runs `vercel pull` and uploads `.vercel` (project settings and pulled env values) as a one-day artifact for the build job.
2. **build**: no token. Checks out the exact SHA, runs `vercel build`, tars `.vercel/output` and `.vercel/project.json`, and records the tarball's SHA-256 as a job output.
3. **deploy**: has the token, runs on a fresh runner with no checkout. Downloads the tarball, verifies its checksum against the build job's output (and that it holds only `.vercel/output` and `.vercel/project.json`), installs the pinned Vercel CLI, and runs only `vercel deploy --prebuilt`, then the smoke test.

Production adds a final `tag` job (write-capable GitHub token, no Vercel token) that pushes the annotated tag after the smoke test passes. Because `pull` and `deploy` both reference the environment, a Production run asks for approval twice (and the bootstrap's production-class half does too). Pulled env values are sent to the build job through a short-lived artifact, so anyone who can read this repository's Actions artifacts can read them; keep secrets out of Vercel build-time env vars where possible.

## Token rule

`VERCEL_TOKEN` is currently a **repository** secret, so any workflow could request it. Until it is moved into the `preview` and `production` environments (a separate approved credential action):

- Pass it only through `env:` on the exact steps that run the Vercel CLI or call the Vercel API (`vercel pull`, `vercel deploy`, deployment ID lookup, bootstrap record verification).
- Never give it to a job that checks out the repository or runs install or build scripts (`vercel build`), nor to third-party action, smoke, or artifact steps.
- Never echo it, put it on a command line, or write it to a summary, artifact or cache. The only artifact with pulled `.vercel` data is the one-day pull artifact described above.
- Add no new workflow that reads it without review.

## Deployment Protection bypass (optional)

If Vercel Deployment Protection blocks the unique deployment URLs, the smoke tests see 401/403 or a 3xx redirect to a login page. The workflows can send the `x-vercel-protection-bypass` header, but only if the optional secret `VERCEL_AUTOMATION_BYPASS_SECRET` is set. It is Vercel's "Protection Bypass for Automation" secret for the project, stored as a secret in the GitHub `preview` and `production` environments (and passed through by `ci.yml` to `preview.yml`). **Not created yet; pending approval.** When it is unset, the smoke tests send no header. It is handed to curl through a config on stdin, never echoed, and redirects are never followed.

## 1. One-time bootstrap

Vercel treats a new project's first CLI deployment as Production, even when Preview is requested. The bootstrap proves both targets before automatic Preview is enabled.

Dispatch from `main` with a SHA reachable from `origin/main`; the validate job rejects anything else. Prerequisites: a reviewed SHA of the disposable hosted slice (`api/health.go`, `web/`, `vercel.json`) with no database, Auth, migrations or env vars; Vercel Git integration still disconnected; `PREVIEW_ENABLED` unset; separate approval to run this.

1. Actions > **Vercel bootstrap** > Run workflow. Enter the full 40-character `sha` and `confirm` = `bootstrap`.
2. Approve the `production` environment deployments when prompted (twice: pull and deploy, `--prod --skip-domain`).
3. Job 2 runs under `preview` and deploys the same SHA with `--target=preview`.
4. Read the run summary. For each deployment it must show: project ID matches, `meta.sourceSha` matches, the expected target, `READY`, no production-domain alias (for the production-class deploy, no alias at all), and a passing smoke test on the unique URL. Also check the **project domains** line: if it shows a WARNING that the domains fetch failed or returned empty, the no-production-domain-alias check had nothing to compare against, so confirm in the Vercel dashboard that no production domain is attached before setting `PREVIEW_ENABLED`.
5. Only if both jobs pass, set repository variable `PREVIEW_ENABLED=true`. If either fails, leave it unset, do not retry blindly, and update the plan from current Vercel guidance.

The bootstrap creates no tag and touches no database or environment variables. If the unique URL returns 401/403, Vercel Deployment Protection is blocking it; resolve that as a project-setting decision before retrying.

## 2. Preview

Automatic Preview runs from `ci.yml` for trusted same-repository hosted pull requests once `PREVIEW_ENABLED=true`. Nothing to run by hand. The Preview summary carries the SHA, URL and manifest; the manifest is also an artifact named `preview-manifest-<sha>-<run attempt>`. A Preview failure blocks `CI / required`.

## 3. Production release

1. Merge the release branch to `main` and wait for push CI on the final `main` commit to finish green (`CI / required`).
2. Pick the SHA: the full 40-character SHA of that final `main` commit (`git rev-parse origin/main`).
3. Pick the version: `app-vMAJOR.MINOR.PATCH`, not already used as a tag (this is the hosted web/API version, separate from CLI/MCP versions).
4. Actions > **Deploy production** > Run workflow, **from `main`**, entering `sha` and `version`. Leave `allow_non_tip` false (default).
5. The verify job rejects the run unless the SHA is reachable from `origin/main` and is its current tip (an older ancestor is refused unless `allow_non_tip` is `true`, for a deliberate rollback-by-redeploy), the tag is unused (checked with `git tag -l` after a full-history checkout), and the newest `ci.yml` push run on `main` for that SHA has a successful `required` job. Fix the input rather than working around it.
6. A required reviewer approves the `production` environment (once for `pull`, once for `deploy`). Check the summary's SHA and version before approving; production secrets are released only after approval. Recommended: restrict the `production` environment's deployment branches to `main` (configured separately, not by these workflows).
7. The pull, build and deploy jobs (see the build/deploy split above) deploy that SHA with `--prod`, smoke-test `/api/health` on the new deployment URL, then the tag job pushes the annotated tag. The summary records the deployment URL, the Vercel deployment ID and the manifest (SHA, lockfile hashes, tool versions, build-output checksum).

Runs are serialized (`production` group, never cancelled). A third request queued while one waits replaces the older queued one, so re-dispatch if a request is dropped. Database migration steps are present but skipped (there is no schema yet).

## 4. Deploy or smoke failure

- Failure before `vercel deploy`: nothing was deployed. Fix and re-dispatch. No tag exists.
- Smoke failure after a production deploy: the new deployment may already serve the production domain, and no tag was created. Decide explicitly; the preferred action is to roll traffic back to the prior verified deployment:
  1. Find the last good deployment (Vercel dashboard > Deployments, or the previous release's summary URL).
  2. `vercel rollback <deployment-url-or-id>` (or **Instant Rollback** in the dashboard) using an operator credential. Do not add a rollback step that spreads the token.
  3. Confirm the production domain's `/api/health` returns 200 and the prior version.
- Schema: never roll a database back automatically. With expand-and-contract migrations the previous deployment stays compatible, so roll traffic back and roll forward with a reviewed corrective migration.
- After recovery, re-dispatch with the same `version` (no tag was created) or a new one, from a fixed `main` commit.
- Tag push failure after a successful deploy: the deployment is live and verified. Create the annotated tag manually on that SHA after checking the summary, or re-dispatch only if you accept a redeploy.
