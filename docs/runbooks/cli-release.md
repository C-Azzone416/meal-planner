# CLI and MCP release runbook

Releases of the `mealplanner` CLI and the `mealplanner-mcp` server are built by
`.github/workflows/release-cli.yml`. They are **unsigned**, distributed only as
GitHub Release archives, and the CLI and MCP share one version.

## Cutting a release

1. Decide the version bump with the user. Versions are semver and start at
   `0.y.z`; while the major version is `0`, the release is marked as a
   GitHub pre-release.
2. Make sure the commit to release is on `main` and its CI is green. The
   workflow refuses to publish otherwise: `tools/release/verify-ci.sh <sha>`
   requires a `ci.yml` push run on `main` for that SHA with a job named
   `required` that succeeded (needs `GH_TOKEN`/`GITHUB_REPOSITORY`; the UI's
   "CI / required" is only workflow/job display text).  See below for running it locally.
3. Tag that commit and push the tag:

   ```sh
   git checkout main && git pull --ff-only
   git tag cli-v0.1.0
   git push origin cli-v0.1.0
   ```

   The tag must match `cli-vX.Y.Z` exactly.
4. Watch the **Release CLI** run. Flow: draft → verify → publish.
   - `verify`: for manual runs, the workflow must be dispatched from
     `refs/heads/main`; then tag format, ancestry on `main`, green `required`
     job via `verify-ci.sh`.
   - `build` (five cross-compiled targets) and `smoke` (native macOS and
     Windows runs). The Windows smoke test extracts the zip with bsdtar
     (System32 `tar.exe`) only; there is no PowerShell fallback.
   - `publish`: re-checks the tag still points at the verified commit, then
     creates a **draft** release (pre-release for `0.x`) with all archives and
     `SHA256SUMS`, targeting the verified SHA. Nothing is public yet.
   - `verify-published`: downloads the draft's assets fresh, asserts exactly
     the five expected archives plus `SHA256SUMS`, runs `sha256sum -c`, and
     compares `SHA256SUMS` byte for byte with the one produced by the build.
   - `release`: only after all of the above, re-checks once more that the tag
     still resolves to the verified commit (failing and leaving the draft
     unpublished if not), then flips the draft to published.
5. On failure the release stays a draft and the `cleanup` job deletes the
   draft (never a published release; the tag is kept). Read the failing job's
   log, fix the cause, then re-run (below). Nothing was ever public.

To re-run for an existing tag, use **Actions → Release CLI → Run workflow**
with the `tag` input. A leftover draft for the tag is deleted and recreated.
If a **published** release already exists, `publish` fails with a clear
message; delete that release (not the tag) first.

If GitHub immutable releases are enabled for the repository, publishing the
draft locks the tag and the release assets. After that, neither can be changed
or deleted, so a bad release can only be superseded by a new version.

Running `tools/release/verify-ci.sh` locally needs:

```sh
export GITHUB_REPOSITORY=C-Azzone416/meal-planner
export GH_TOKEN=<token with actions: read>   # or `GH_TOKEN=$(gh auth token)`
tools/release/verify-ci.sh <sha>
```

### Tag protection (recommended, not yet configured)

Create a repository ruleset for `cli-v*` tags so only maintainers can create,
update, or delete them. This guards the tag-to-commit mapping that the
workflow verifies. It has not been set up yet and will be configured during
rollout.

The in-file checks (`workflow_dispatch` only from `main`, tag reachable from
`main`) are not a security boundary on their own: a workflow file edited on a
non-main tag runs the edited copy and could skip them. The `cli-v*` tag ruleset
is the real control.

### First live verification

Run once after the workflow lands on `main` and the tag ruleset is in place,
using a throwaway tag `cli-v0.0.1`. Tag the current green `main` commit unless
a step says otherwise. Delete the release and the tag afterwards
(`gh release delete cli-v0.0.1 --cleanup-tag --yes`, or delete the tag with
`git push origin :refs/tags/cli-v0.0.1`).

- [ ] **Happy path.** Push `cli-v0.0.1`. All jobs go green; a published
  pre-release (major `0`) has the five archives plus `SHA256SUMS`.
- [ ] **Draft invisible when logged out.** While `verify-published` is still
  running (or on a draft you keep by pausing), fetch
  `https://github.com/C-Azzone416/meal-planner/releases/tag/cli-v0.0.1` and the
  releases API unauthenticated (`curl -s .../releases`): the draft must not
  appear.
- [ ] **Forced verify-published failure deletes the draft.** Temporarily make
  `verify-published` fail (for example a bogus expected asset name on a test
  branch run). The `cleanup` job deletes the draft, the tag stays, and no
  public release exists.
- [ ] **Re-run replaces a draft.** Leave a draft in place (fail a run after
  `publish`), then run via **Run workflow** with the same tag. The old draft is
  deleted and a new one is created.
- [ ] **Re-run against a published release is refused.** With the release
  published, run the workflow again for the tag. `publish` fails with the
  "published release already exists" error and nothing is deleted (the release
  and its assets are intact, and `cleanup` does not remove it).
- [ ] **Bad tag.** Push `cli-vfoo` and `cli-v1.2`: `verify` rejects both on
  the format check.
- [ ] **Tag not on main.** Tag a commit on a side branch: `verify` fails on
  the ancestry check. Also try **Run workflow** from a non-main branch: it
  fails the `refs/heads/main` guard.
- [ ] **No green `required`.** Tag a `main` commit with no successful
  `ci.yml` push run, or whose `required` job failed: `verify-ci.sh` fails.
- [ ] **Annotated tag.** Push an annotated tag (`git tag -a cli-v0.0.1 -m x`).
  `verify` resolves it to the commit via `refs/tags/<tag>^{commit}`, the draft
  targets that commit, and the release publishes.
- [ ] **Windows smoke test.** The `Smoke windows-amd64` job passes, extracting
  the zip with System32 `tar.exe` (bsdtar), and runs both `.exe` binaries.
- [ ] **Concurrency.** Push the tag and immediately dispatch the same tag.
  The second run queues behind the first (same `release-cli-<tag>` group,
  `cancel-in-progress: false`) rather than racing it.
- [ ] **`gh release edit` finds the draft.** In the `release` job log,
  `gh release edit cli-v0.0.1 --draft=false` succeeds on the draft by tag name.
  If it ever reports "release not found", switch to editing by release id.

### Dry run locally

```sh
tools/release/build-archives.sh 0.1.0 /tmp/dist          # all targets
tools/release/build-archives.sh 0.1.0 /tmp/dist linux/amd64
tools/release/make-checksums.sh /tmp/dist
tools/release/smoke-test.sh /tmp/dist/mealplanner_0.1.0_<os>_<arch>.tar.gz 0.1.0
```

## Artifacts

| Target | Archive |
| --- | --- |
| macOS arm64 / amd64 | `mealplanner_<version>_darwin_{arm64,amd64}.tar.gz` |
| Linux amd64 / arm64 | `mealplanner_<version>_linux_{amd64,arm64}.tar.gz` |
| Windows amd64 | `mealplanner_<version>_windows_amd64.zip` |

Each archive holds a directory with `mealplanner`, `mealplanner-mcp` (`.exe`
on Windows), and `README.md`/`LICENSE` when present. `SHA256SUMS` lists all
five archives.

## Installing, verifying, and updating (users)

There is no silent updater. Installing and updating are always manual.

1. Download the archive for your platform and `SHA256SUMS` from the
   [Releases page](https://github.com/C-Azzone416/meal-planner/releases).
2. Verify the checksum (the archives are unsigned, so this only detects
   corruption or a mismatch with the published list):

   ```sh
   sha256sum -c --ignore-missing SHA256SUMS     # Linux
   shasum -a 256 -c --ignore-missing SHA256SUMS # macOS
   ```

   On Windows PowerShell, compare `(Get-FileHash <archive>).Hash` with the
   matching line in `SHA256SUMS`.
3. Extract the archive and copy `mealplanner` and `mealplanner-mcp` onto your
   `PATH`.
4. Confirm: `mealplanner --version` and `mealplanner-mcp --version`.
5. macOS: unsigned and un-notarized binaries downloaded with a browser may be
   quarantined. Clear it only for files you verified:
   `xattr -d com.apple.quarantine mealplanner mealplanner-mcp`.

**Updating** is the same procedure: download the new release, verify, and
overwrite both binaries. **After updating `mealplanner-mcp`, restart the MCP
host** (or its MCP server process); a running host keeps using the old
executable until restarted.

## Follow-ups (unresolved)

- Code signing and signature verification for all platforms.
- macOS notarization.
- SBOM and build provenance attestations.
- Package managers (Homebrew, Scoop, etc.) and installer scripts.
- Update-feed metadata for the CLI's once-a-day update check.
- Minimum supported OS versions and API compatibility window.
- Running the full candidate CI matrix (protocol conformance, supported-API
  compatibility, clean-machine install checks) as a release gate beyond
  `CI / required`.
