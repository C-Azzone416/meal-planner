# Closed full-output inventory groundwork (not an exporter)

This module performs local, inert inspection and optional independent staging.
It does not issue a receipt, create an archive, upload, deploy or authorize release.
The fixed application layout and trusted golden require review when changed.

## Accepted layout

- Exactly `config.json`, matching the reviewed route golden with no extra
  response-transform, header, daemon or cron behavior.
- Exactly ten observed Go function directories: auth-config, auth-proxy,
  auth/authorize, auth/before-create, feedback/connect, feedback/disconnect,
  feedback/reports, feedback/status, health and recipes. Every function has only
  `.vc-config.json` and executable `bootstrap`. Metadata uses the closed six-key
  x86_64 schema with empty environment; ELF headers must agree.
- Exactly `static/index.html`, `static/framebriar-build.json`, and one eight-character
  hashed `index-*.js` plus `index-*.css` under assets. The index must match the
  reviewed fixed HTML template with those two exact asset paths; no extra scripts,
  external URLs or other HTML are admitted.
- Public provenance must exactly match independently supplied Preview environment,
  app ID, source SHA, origin and feature/capture flags. JS provenance/capture
  marker checks are consistency checks only, not proof of actual behavior.
- Known nonpublished paths are **only** `builds.json`,
  `diagnostics/cli_traces.json`, `diagnostics/deploy-manifest.json` and
  `diagnostics/project-manifest.json`. Their type/size/mode is checked, but their
  contents are never read, hashed, copied or returned. The result reports names
  only. They are optional; any other file or empty directory fails rather than
  being silently omitted.

No source maps, unknown assets, extra functions, symlinks, special files, unsafe
modes or unknown directories are accepted. Traversal uses pinned directory file
handles and no-follow opens. Each bounded read compares identity, size, mode and
modification metadata before and after. Limits are 128 entries, 64 MiB per file,
and 128 MiB total including nonpublished files.

## Observed hardlinks and staging

The credential-free full diagnostic build produced static files with link count
2, linked to build-tree files outside `.vercel/output`. Direct publish inspection
rejects these aliases. `stage_layout` may inspect **static source** hardlinks,
then creates a new private staging directory and exclusive independent files for
only the accepted published inventory. It re-reads through pinned/no-follow
handles, checks byte hashes against inspection, and validates the independent
copy with the strict no-hardlink validator. Function/config hardlinks remain
rejected. The source is not modified. Failed staging may leave an incomplete
private temporary directory; that is not successful output or publication.

This does **not** make a hostile still-running build process trustworthy. The real
exporter requires a clean process/job/archive handoff. Stat checks are not a
replacement for that trust separation. No byte scanner can prove that arbitrary
compiled JavaScript/Go cannot contain or emit transformed private source/data.
The staged tree and inventory are not an authenticated authorization receipt.

## Verification recorded

On 2026-11-13, the diagnostic full app output with explicit x86_64 source config
and a disposable Go 1.26.11 build passed independent-copy layout inspection:
**25 published files** and the four known excluded paths above. Its public
configuration was synthetic `https://preview.example.test`, app
`meal-planner-preview`, source label `c41eddf638eed64bdda5083306d37aae8eff6d98`,
feedback/capture enabled. This source label does not attest the disposable Go
patch or make this a reviewed release candidate. Nothing was uploaded.

Run `node --test tools/framebriar-ci/private-output-layout.test.mjs` for 18 focused
cases: positive inventory, diagnostics exclusion, unknown/missing paths, empty
directories, links, FIFO, metadata/environment, ELF architecture, public provenance,
index/capture checks, modes, independent hardlink staging and changed-source
rejection. Fixtures use tiny synthetic ELF headers, not executable Go programs.
Independent QA/Judge, full Linux candidate and live workflow checks remain separate.

## Diagnostic evidence qualification

The local full-app probe used archived candidate `c41eddf` plus an experimental
`go.mod` minimum of `1.26.11`. Its declared SHA/public origin were diagnostic
inputs, not evidence of unchanged release-source identity. No release receipt
was created. Every eventual publish candidate must be rebuilt from its exact
reviewed commit, checked for the patched compiler in all function binaries,
scanned for vulnerabilities and verified through its protected workflow.
