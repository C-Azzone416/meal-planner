# Hosted deploy runbook

Operator procedures for the Vercel-hosted web/API. The policy is in [ci-cd-plan.md](../ci-cd-plan.md); this page only says how to run the workflows. Nothing here is done automatically, and every Vercel-touching run needs explicit approval at the time.

Workflows: `.github/workflows/vercel-bootstrap.yml` (one time), `preview.yml` (called from `ci.yml` on `develop` pushes), `deploy-production.yml` (manual).

## Build/deploy split

In every workflow, `vercel build` (which runs repository install and build scripts) never runs in a job that has `VERCEL_TOKEN`. Each target runs as three jobs:

1. **pull**: has the token and no checkout during the token step, so no repository code is present next to `VERCEL_TOKEN`. Runs `vercel pull`, then checks out only `tools/deploy/check-pulled-env.sh` from the workflow's own commit (`github.workflow_sha`, never the SHA being built), checks and deletes the pulled env files (see the guard below), and uploads only `.vercel/project.json` (project/org IDs and the pulled project settings) as a one-day artifact for the build job.
2. **build**: no token. Checks out the exact SHA, restores `project.json`, writes a placeholder env file (see below), runs `vercel build`, tars `.vercel/output` and `.vercel/project.json`, and records the tarball's SHA-256 as a job output.
3. **deploy**: has the token, runs on a fresh runner with only `tools/deploy` checked out (from the commit that defines the workflow, not the built SHA). Downloads the tarball, verifies its checksum against the build job's output, then `tools/deploy/safe-extract.sh` validates it before anything is written: every entry must be a regular file or directory (symlinks, hardlinks and special files are rejected), no name may be absolute or contain `..`, and only `.vercel/output` and `.vercel/project.json` are allowed. It extracts into a fresh empty directory under `$RUNNER_TEMP` with `--no-same-owner --no-same-permissions`. Only then is the pinned Vercel CLI installed (into another `$RUNNER_TEMP` directory, not the npx cache, so the archive cannot touch it) and `vercel deploy --prebuilt` run from the extraction directory, followed by the smoke test. `tools/deploy/safe-extract_test.sh` covers the symlink, hardlink, `..`, absolute-path, fifo and clean-archive cases.

Production adds a final `tag` job (write-capable GitHub token, no Vercel token) that pushes the annotated tag after the smoke test passes.

**Two approval prompts per production deploy.** `pull` and `deploy` each reference the `production` environment, and both hold the token, so each waits for a required-reviewer approval: first `pull`, then (after `build`) `deploy`. Approving only `pull` does not release the deploy job. This is deliberate: environment secrets are released per job, so the second prompt is a separate chance to check the build output before the token meets it. The bootstrap's production-class half behaves the same way.

**Env files never leave the pull job.** `vercel pull` writes `.vercel/.env.<target>.local`, and that file includes `VERCEL_OIDC_TOKEN`, a live credential valid for about 12 hours. Artifacts on a public repository are readable by anyone, so the pull artifact contains only `.vercel/project.json`, and the env files are deleted from the runner right after the guard runs.

**Env guard (temporary).** `tools/deploy/check-pulled-env.sh` (tested by `check-pulled-env_test.sh` in CI) parses the pulled env files as dotenv and fails the `pull` job if any defines a name that is not a Vercel system variable: `VERCEL_*` or one of the exact names listed, with their Vercel docs source, in the script (`VERCEL_OIDC_TOKEN` itself passes, because it is a system variable, and is why the files are never shipped). Lines it cannot parse fail it too. It checks variable *names*, not whether a value is secret. It logs the unexpected names with line numbers, and on a pass the allowlisted non-`VERCEL_` names present; it never logs values or unparseable lines. A failure means build-time env vars now exist and this design must be revisited (for example, set non-secret build env in the build job from a trusted source, or build in a single trusted job); do not just loosen the guard. The guard fails the job loudly (exit 2) if its `find` cannot run or `.vercel` is missing. Add a name to the allowlist only with a Vercel docs citation.

**Placeholder env file (unverified).** `vercel build` looks for `.vercel/.env.<target>.local` and, with `--yes` and no file, would try to pull it itself, which needs a token the build job must not hold. So the build job writes a stripped file containing a comment and `VERCEL_ENV=<target>`, nothing else. This has not been verified against a live build, and the Vercel docs do not say what `vercel build` requires. The first bootstrap run is the test; if the build rejects it, revisit the design instead of shipping env values through an artifact.

## Token rule

`VERCEL_TOKEN` is currently a **repository** secret, so any workflow could request it. Until it is moved into the `preview` and `production` environments (a separate approved credential action):

- Pass it only through `env:` on the exact steps that run the Vercel CLI or call the Vercel API (`vercel pull`, `vercel deploy`, deployment ID lookup, production alias check, bootstrap record verification).
- Never give it to a job that checks out the repository or runs install or build scripts (`vercel build`), nor to third-party action, smoke, or artifact steps.
- Never echo it, put it on a command line, or write it to a summary, artifact or cache. The only artifact with pulled `.vercel` data is the one-day pull artifact described above.
- Add no new workflow that reads it without review.

## Deployment Protection bypass secret

`VERCEL_AUTOMATION_BYPASS_SECRET` lives **only in the `preview` GitHub environment**. It is not in the `production` environment, not a repository secret, and no production job reads it.

- **What it is.** Vercel's "Protection Bypass for Automation" secret, sent as the `x-vercel-protection-bypass` header. It bypasses Vercel *Deployment Protection*, not application authentication, and it works for **every deployment of the project**, not just previews. Treat it as sensitive.
- **Where to create it.** Generate it in Vercel under Settings > Deployment Protection > Protection Bypass for Automation, then store it with `gh secret set VERCEL_AUTOMATION_BYPASS_SECRET --env preview`. It is a separate Meal Planner secret. **Not created yet; pending the user's confirmation.** When unset, smoke tests send no header.
- **Preview.** The `deploy` job in `preview.yml` (environment `preview`) reads it only in the smoke step's `env:`. A caller cannot pass environment secrets into a reusable workflow, so `ci.yml` does not pass it. Preview deployments run merged code pushed to `develop` (never pull-request code) and that code receives the header, so this relies on trusting whoever can push to `develop`.
- **Bootstrap.** The production-class phase deploys with `--skip-domain`, so only its protected unique URL exists and its smoke test really needs the bypass. That smoke test is its own job, `smoke-production-class`, under `environment: preview`. It holds **no Vercel token**, has no checkout and no CLI, only `curl` with the header passed through `-K -` on stdin, and re-validates the URL (`^https://[a-z0-9-]+\.vercel\.app$`) it receives as a job output because it is a new trust boundary. It adds one more `preview` approval prompt if reviewers are ever set on `preview`. Its secret and the token never meet in one job.
- **Production.** `deploy-production.yml` has no bypass secret. After `vercel deploy --prod` it finds the project's public production alias from the Vercel API, polls until the API says the alias points at **this deployment's ID**, then smoke-tests `https://<alias>/api/health` with no bypass header, which proves normal public access. The alias and deployment ID are recorded in the job summary. If no alias is found or the ID never matches (3 minute timeout), the job fails closed and no tag is pushed. The ID check, not the 200, is what proves the alias serves the new build.
- Smoke-test response bodies written to the job summary are capped at 2 KB, stripped of non-printable characters and backticks, and fenced.

Follow-up (app change, not part of the deploy workflows): `/api/health` should report the build commit SHA so smoke can assert it directly.

## 1. One-time bootstrap

Vercel treats a new project's first CLI deployment as Production, even when Preview is requested. The bootstrap proves both targets before automatic Preview is enabled.

Dispatch from `main` with a SHA reachable from `origin/main`; the validate job rejects anything else. Prerequisites: a reviewed SHA of the disposable hosted slice (`api/health.go`, `web/`, `vercel.json`) with no database, Auth, migrations or env vars; Vercel Git integration still disconnected; `PREVIEW_ENABLED` unset; separate approval to run this.

1. Actions > **Vercel bootstrap** > Run workflow. Enter the full 40-character `sha` and `confirm` = `bootstrap`.
2. Approve the `production` environment deployments when prompted (twice: pull and deploy, `--prod --skip-domain`).
3. The `smoke-production-class` job runs under `preview` (token-free, uses the bypass secret) and smoke-tests the unique URL. Then the preview phase deploys the same SHA with `--target=preview`.
4. Read the run summary. For each deployment it must show: project ID matches, `meta.sourceSha` matches, the expected target, `READY`, no production-domain alias (for the production-class deploy, no alias at all), and a passing smoke test on the unique URL. Also check the **project domains** line: if it shows a WARNING that the domains fetch failed or returned empty, the no-production-domain-alias check had nothing to compare against, so confirm in the Vercel dashboard that no production domain is attached before setting `PREVIEW_ENABLED`.
5. Only if both jobs pass, set repository variable `PREVIEW_ENABLED=true`. If either fails, leave it unset, do not retry blindly, and update the plan from current Vercel guidance.

The bootstrap creates no tag and touches no database or environment variables. If the unique URL returns 401/403, Vercel Deployment Protection is blocking it; resolve that as a project-setting decision before retrying.

## 2. Preview

Automatic Preview runs from `ci.yml` on every push to `develop` that changes hosted code, once `PREVIEW_ENABLED=true`. Pull requests run checks only and never deploy a Preview. Nothing to run by hand. The Preview summary carries the SHA, URL and manifest; the manifest is also an artifact named `preview-manifest-<sha>-<run attempt>`. A Preview failure blocks `CI / required`. Every develop commit deploys (`concurrency` group `preview-<sha>`, `cancel-in-progress: false`); nothing is cancelled. Overlapping runs are safe because the alias only moves forward (see below).

**Stable alias.** Set the repository variable `PREVIEW_ALIAS` (e.g. `gh variable set PREVIEW_ALIAS --body meal-planner-develop.vercel.app`) to have each successful develop Preview moved to that hostname after its smoke test. The run summary's "Preview alias" section shows the confirmed deployment ID. Unset skips the step with a notice. It fails if the value is a production domain of the project or does not point at the new deployment within 3 minutes. The alias hostname must be on a domain the Vercel token's scope can manage. The production-domain guard covers this project's domains only (it fails closed above 100 domains).

**Move-forward rule.** Before `vercel alias set`, the step reads which deployment the alias points at and that deployment's `meta.sourceSha`. A missing alias (404) or an unknown source SHA means move. The same SHA means nothing to move. Otherwise it fetches `develop` history (unauthenticated, blob-less, nothing checked out) and skips with a notice if this run's commit is an ancestor of the one the alias serves, so a late older deploy can't take the alias back and a commit that never deployed (docs-only, failed CI, preview off) never blocks a later one. Any other result (API error, commit not in `develop` history, fetch failure) fails the step. The fetch needs the repository to stay public; if it goes private the step fails closed.

**Red develop CI.** A red `CI / required` on a develop commit can mean "deployed, but the alias was not moved" (alias step failed). Check the run's alias step and summary before redeploying.

## 3. Production release

1. Merge the release branch to `main` and wait for push CI on the final `main` commit to finish green (`CI / required`).
2. Pick the SHA: the full 40-character SHA of that final `main` commit (`git rev-parse origin/main`).
3. Pick the version: `app-vMAJOR.MINOR.PATCH`, not already used as a tag (this is the hosted web/API version, separate from CLI/MCP versions).
4. Actions > **Deploy production** > Run workflow, **from `main`**, entering `sha` and `version`. Leave `allow_non_tip` false (default).
5. The verify job rejects the run unless the SHA is reachable from `origin/main` and is its current tip (an older ancestor is refused unless `allow_non_tip` is `true`, for a deliberate rollback-by-redeploy), the tag is unused (checked with `git tag -l` after a full-history checkout), and the newest `ci.yml` push run on `main` for that SHA has a successful `required` job. Fix the input rather than working around it.
6. A required reviewer approves the `production` environment (once for `pull`, once for `deploy`). Check the summary's SHA and version before approving; production secrets are released only after approval. Recommended: restrict the `production` environment's deployment branches to `main` (configured separately, not by these workflows).
7. The pull, build and deploy jobs (see the build/deploy split above) deploy that SHA with `--prod`, wait for the production alias to point at the new deployment ID, smoke-test `https://<alias>/api/health` without a bypass header, then the tag job pushes the annotated tag. The summary records the deployment URL, the production alias, the Vercel deployment ID and the manifest (SHA, lockfile hashes, tool versions, build-output checksum).

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
