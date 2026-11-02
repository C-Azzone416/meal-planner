# Hosted deploy runbook

Operator procedures for the Vercel-hosted web/API. The policy is in [ci-cd-plan.md](../ci-cd-plan.md); this page only says how to run the workflows. Nothing here is done automatically, and every Vercel-touching run needs explicit approval at the time.

Workflows: `.github/workflows/vercel-bootstrap.yml` (one time), `preview.yml` (called from `ci.yml`), `deploy-production.yml` (manual).

## Token rule

`VERCEL_TOKEN` is currently a **repository** secret, so any workflow could request it. Until it is moved into the `preview` and `production` environments (a separate approved credential action):

- Pass it only through `env:` on the exact steps that run the Vercel CLI or call the Vercel API (`vercel pull`, `vercel deploy`, bootstrap record verification).
- Never pass it to checkout, install, `vercel build` (it runs the project's build commands), third-party action, smoke, or artifact steps.
- Never echo it, put it on a command line, or write it to a summary, artifact or cache. Upload only the manifest, never `.vercel/`.
- Add no new workflow that reads it without review.

## 1. One-time bootstrap

Vercel treats a new project's first CLI deployment as Production, even when Preview is requested. The bootstrap proves both targets before automatic Preview is enabled.

Prerequisites: a reviewed SHA of the disposable hosted slice (`api/health.go`, `web/`, `vercel.json`) with no database, Auth, migrations or env vars; Vercel Git integration still disconnected; `PREVIEW_ENABLED` unset; separate approval to run this.

1. Actions > **Vercel bootstrap** > Run workflow. Enter the full 40-character `sha` and `confirm` = `bootstrap`.
2. Approve the `production` environment deployment when prompted (job 1, `--prod --skip-domain`).
3. Job 2 runs under `preview` and deploys the same SHA with `--target=preview`.
4. Read the run summary. For each deployment it must show: project ID matches, `meta.sourceSha` matches, the expected target, `READY`, no production-domain alias (for the production-class deploy, no alias at all), and a passing smoke test on the unique URL.
5. Only if both jobs pass, set repository variable `PREVIEW_ENABLED=true`. If either fails, leave it unset, do not retry blindly, and update the plan from current Vercel guidance.

The bootstrap creates no tag and touches no database or environment variables. If the unique URL returns 401/403, Vercel Deployment Protection is blocking it; resolve that as a project-setting decision before retrying.

## 2. Preview

Automatic Preview runs from `ci.yml` for trusted same-repository hosted pull requests once `PREVIEW_ENABLED=true`. Nothing to run by hand. The Preview summary carries the SHA, URL and manifest; the manifest is also an artifact named `preview-manifest-<sha>`. A Preview failure blocks `CI / required`.

## 3. Production release

1. Merge the release branch to `main` and wait for push CI on the final `main` commit to finish green (`CI / required`).
2. Pick the SHA: the full 40-character SHA of that final `main` commit (`git rev-parse origin/main`).
3. Pick the version: `app-vMAJOR.MINOR.PATCH`, not already used as a tag (this is the hosted web/API version, separate from CLI/MCP versions).
4. Actions > **Deploy production** > Run workflow, **from `main`**, entering `sha` and `version`.
5. The verify job rejects the run unless the SHA is reachable from `origin/main`, the tag is unused, and the newest `ci.yml` push run on `main` for that SHA has a successful `required` job. Fix the input rather than working around it.
6. A required reviewer approves the `production` environment. Check the summary's SHA and version before approving; production secrets are released only after approval.
7. The job pulls, builds and deploys that SHA with `--prod`, smoke-tests `/api/health` on the new deployment URL, then pushes the annotated tag. The summary records the deployment URL and the manifest (SHA, lockfile hashes, tool versions, build-output checksum).

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
