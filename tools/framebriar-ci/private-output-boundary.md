# Private Preview archive boundary foundation (not activated)

`private-output-boundary.py` is an inert verification primitive. It does not
create receipts, extract files, upload artifacts, enable a workflow, or authorize
a deployment. The existing private Preview stop remains authoritative.

## Trust contract

The future caller must first authenticate a clean-reporter receipt using the
reviewed workflow/source/current-base/chronology and artifact identity rules.
It supplies that receipt and independently expected Preview project, source SHA,
trusted workflow SHA, and artifact ID. Never obtain the expected context or
inventory from the target checkout or the archive being checked.

The helper checks an exact inventory and digests against bounded, uncompressed
USTAR bytes, parses recognized JSON metadata with duplicate-key rejection, and
never extracts or executes their contents. The inventory includes every directory;
extra empty directories fail. PAX/GNU extension headers, prefixes, links, devices,
fifos, duplicate entries, trailing junk/concatenated archives, noncanonical paths
and unsafe modes fail. The exact byte digest binds the archive to the receipt.

**An authenticated inventory does not establish that executable content is safe.**
A hostile builder can supply self-consistent bytes and hashes. The clean reporter
must establish the closed schema and provenance before issuing its receipt.

## Gates intentionally not implemented

- A closed metadata schema and actual Vercel CLI 65.9.0 output fixture for this
  application's Vite assets and materialized Go functions. Synthetic test bytes
  are expressly not evidence of that compatibility.
- Literal-only response metadata or complete rejection of response mutation.
  Duplicate-key rejection alone does not validate Vercel metadata semantics.
- A reviewed clean-reporter receipt issuer with immutable target-specific
  migration-readiness evidence. No target-provided `approved` flag is accepted.
- Exporter and cross-run consumer workflow wiring with credential-bearing pull,
  credential-free build, clean reporting, identity/checksum binding, and live
  protected-build verification.
- A decision resolving the static-scanning limit: byte scans cannot prove that
  arbitrary executable assets contain no transformed private source or will
  never emit private information. This note is not risk acceptance.

Until those gates have been implemented and independently reviewed, this helper
must not be used to change the existing fail-closed private Preview behavior.
Production is outside this helper's supported context.

## Local verification

Run without Python bytecode output:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 tools/framebriar-ci/private-output-boundary_test.py
```

The 15 tests cover synthetic positive inventory, duplicate JSON at top-level,
nested/escaped-equivalent keys, invalid UTF-8/nonfinite/deep metadata, both known
metadata locations, target/project/SHA/artifact substitutions, digest/payload
changes, unknown fields/directories, duplicate/missing entries, paths, link and
special headers, concatenation/truncation, and unsafe modes. This is focused
foundation coverage, not full application, real CLI, QA/Judge or live acceptance.
