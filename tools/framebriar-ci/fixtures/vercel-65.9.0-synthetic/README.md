# Synthetic Vercel CLI 65.9.0 metadata fixtures

Observed 2026-11-13 using the locally installed official Vercel CLI **65.9.0**.
The synthetic project used public application rewrites from the current Meal
Planner `vercel.json`, a harmless static build command, and a standard-library
Go health handler. It did not build the real Meal Planner frontend or include
Framebriar's private SDK, real recipes, account data, runtime credentials or
provider settings. Execution used an empty HOME and no credentials.

The orchestration observed `@vercel/go` **27.0.1**, `@vercel/static-build`
**26.0.1**, Go **1.26.8**, and Go bridge revision **cfc76d26d554**. A pinned CLI
alone does not pin all builder/runtime dependencies; future reproduction must
record and verify the resolved dependencies too.

`config.json` is the actual generated output, preserving route order, regexes,
query capture expansion, `daemons: {}` and `crons: []`. Its SHA-256 is
`a84242b06ebf021c39154516da031edc683161b96c9878512c1c73915fce1ecf`.

`go-config.json` is actual metadata after the existing materializer removed
`filePathMap` and copied the synthetic binary into its function directory. Its
SHA-256 is `ea7f86841b5dba6afe0f9af8f216dc460a92d7f2afa57a243fbf5b571ec3dad6`.
No binary, raw build report, diagnostic trace or temporary path is committed.

## Important qualifications

- **Observed architecture mismatch:** metadata reports `arm64`, but `file` on
  the materialized bootstrap reported a stripped ELF x86-64 executable. This
  fixture establishes a metadata shape, **not a runnable function or deployment
  compatibility**. Resolve the builder/Go target mismatch and verify the Linux
  runner's actual output independently before deployment acceptance.
- CLI completed with a nonfatal `PATH TO REGEXP ERROR` involving `:path*`.
  Generated metadata is preserved verbatim. Route functionality, especially auth
  proxy and recipe deep links, still requires independent verification.
- An initial materialization attempt correctly rejected the Mac `/tmp` symlink
  alias. The successful run used physical `/private/tmp` via `TMPDIR`. The guard
  was not weakened.
- CLI also generated `builds.json` and `diagnostics/cli_traces.json`, which may
  contain local paths. They were deliberately **not copied here**. A future
  exporter needs a reviewed minimal publishing contract; these fixtures do not
  authorize silently deleting unknown output or ignoring unexpected directories.
- The fixture is not evidence of real Vite assets, private-package installation,
  unstripped/stripped Go coverage, Linux CI, live protected export/consumption,
  credential isolation, migration readiness, or runtime delivery.

## Schema groundwork

`private-output-schema.py` accepts config only when it exactly matches a trusted
reviewed golden fixture and uses the observed closed field set. Response headers,
transforms and extra semantic fields are rejected even if supplied as a golden.
Known request-route capture substitutions are permitted only through exact
fixture equality; this is not general substitution support.

The current publish schema accepts exactly six fields with empty environment
and explicit x86_64 architecture. The original default-ARM64 metadata is retained
as a rejected historical fixture, not an accepted publish configuration. This validates metadata shape only and
pairs with an ELF64 little-endian header check that rejects a mismatched machine. This necessary compatibility check does not establish full binary validity, safety or runtime behavior. It remains
unwired. The expected golden must not come from the target checkout or builder.

Run `node --test tools/framebriar-ci/private-output-schema.test.mjs` for nine
focused schema tests. They are not the full deployment acceptance gate.

### Observed architecture rejection

The synthetic bootstrap SHA-256 was
`bd9c0615150e42a04e3a74d10a6cae14ff7e79084214e9b51b617b1c80477177`.
`file` identified **ELF 64-bit LSB executable, x86-64, statically linked,
stripped**. `validate_function_binary` rejected that actual output with
`ELF architecture differs from function metadata`; its metadata was not repaired
to force a passing fixture.

The installed `@vercel/go` 27.0.1 legacy API build path hardcodes `GOARCH: amd64`
while a different standalone path honors Lambda architecture. This explains a
plausible cause of the newly generated Mac fixture's mismatch; it is not evidence
that an existing production deployment is broken. The check must be reproduced
with the actual Linux release build and explicitly selected architecture.

The ELF helper checks only the bounded ELF64 little-endian version/type/header
size and machine binding. It does not validate all program sections or prove that
a binary is runnable or safe. Passing header-only tests are synthetic; the current positive fixture uses x86_64.

## Supported source configuration follow-up

A separate credential-free synthetic build with `functions: {"api/**/*.go":
{"architecture":"x86_64"}}` in **source** vercel.json was accepted by CLI65.9.0.
Its untouched generated metadata is `go-config-x86_64.json`; the materialized
bootstrap is ELF x86-64, SHA256
`00bb49fa94b1b874ce80b05e5b08469b9cf9faa631b16a182fa5769e1d529412`.
Routes were byte-identical to config.json. The publish schema now permits only
this matching x86_64 metadata shape. The original go-config.json is retained as
a rejected regression, not silently rewritten. The saved generator emits the
explicit source override. This resolves the observed synthetic host-default
mismatch, not the independent Linux full-application/live verification gates.
