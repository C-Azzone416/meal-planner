# Explicit reviewed-source Preview build

This candidate extends the existing owner-approved workflow with `preview=true`
for `subject=develop` only. The default remains metadata-only frontend evidence.
No ordinary PR receives the package reader. Ordinary private-dependency Preview
requires authenticated exact-SHA protected deployment evidence; absent evidence fails. This is not yet live activation.

## Trust contract

The operator reviews the exact current develop source and locked manifests before
approving execution. The private SDK's intended compiled client bundle is an
approved publication, not an accidental leak. Raw package source, dependency
archives, source maps, caches, logs and diagnostics are not selected for upload.
The fixed output inventory is checked before upload and again on a clean deploy
runner. This is **not** a general confidentiality sandbox for arbitrary hostile
source. Source can execute with Actions job capabilities; neither step-scoped
credentials nor static inventory inspection prove transformed-source secrecy.

The package reader exists only in the manifest-only, scripts-disabled install
step, before target checkout. Target code receives no reader, Vercel token,
database connection or runtime encryption/cookie secrets. Vercel pull runs in a
separate job and sends only project metadata. Pulled environment files are guarded
and deleted in that job. Deployment runs in another fresh job with trusted
workflow-revision tooling, safe extraction and repeated closed-output validation.
The project metadata comes independently from pull, never from builder output.

The output upload uses an immutable artifact ID from this workflow run plus a
checksum. These establish same-run provenance under the reviewed-source trust
model, not independently signed arbitrary-builder attestation. The prior
cross-run `preview-receipt` proposal is not used or claimed operational.

## Changes and verification

- Explicit `preview` dispatch input, default false; deployment disallowed for PR
  subjects. Owner/main/environment/locked manifest checks remain unchanged.
- Credential-free Vercel 65.9.0 build; Go 1.26.11; each emitted function inspected
  for that compiler and scanned with govulncheck 1.9.0.
- Only the known 11 functions, index, provenance and hashed JS/CSS bundle are
  packaged. Unknown files, maps, unsafe metadata and mismatched SHA are rejected.
- Separate deploy job requires successful frontend and clean evidence jobs,
  repeats inventory/digest validation and current-develop checks before token use.
  Existing Preview health checks and forward-only stable alias behavior remain.
- Fixed provider/build tool dependencies remain trust dependencies. A CLI version
  pin alone does not pin every dynamically resolved builder dependency.

## Explicit remaining activation gates

1. Independently review this exact diff; green CI and project QA then fresh Judge.
2. Premerge frontend validation uses protected PR evidence. After merging,
   protected Preview independently runs full backend, contracts, integration,
   tool-security and frontend checks against the exact merged source before
   deployment. The initial ordinary develop aggregate may fail while that
   dispatch is pending. Rerun ordinary CI after the protected deployment succeeds:
   its private Preview branch verifies that exact protected run, backend and
   successful deploy jobs before completing the aggregate. No earlier green CI
   or successful deployment is claimed. Public-dependency deploys are unchanged.
3. The current migration guard still fails for migration-bearing sources. Obtain
   independently verified target-specific schema/least-privilege readiness and
   wire its approved immutable record before attempting runtime enablement.
4. The trusted workflow must exist at the owner-approved main revision and its
   explicit trust pin must be configured. No main promotion or setting change is
   performed by this candidate; those need the appropriate approval.
5. Verify the actual registry installation, full real Vercel output, runtime
   configuration, signed-in Preview and complete image receipt. Local synthetic
   packaging tests do not establish any of those live outcomes.
6. Invitations remain a separate user-led step. Production remains untouched.
