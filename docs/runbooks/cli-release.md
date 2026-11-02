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
   "CI / required" is only workflow/job display text). You can run it locally
   with `gh` authenticated.
3. Tag that commit and push the tag:

   ```sh
   git checkout main && git pull --ff-only
   git tag cli-v0.1.0
   git push origin cli-v0.1.0
   ```

   The tag must match `cli-vX.Y.Z` exactly.
4. Watch the **Release CLI** run. Jobs: `verify` (tag format, ancestry on
   `main`, green `required` job via `verify-ci.sh`), `build` (five cross-compiled targets),
   `smoke` (native macOS and Windows runs), `publish` (creates the release),
   and `verify-published` (re-downloads the assets and runs `sha256sum -c`).
5. If `verify-published` fails, the release is public but untrustworthy:
   delete the release (not the tag), investigate, and re-run.

To re-run for an existing tag, use **Actions → Release CLI → Run workflow**
with the `tag` input. `publish` fails if a release for the tag already exists,
so delete the release first.

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
