# Private Preview producer: decision packet and non-runnable blueprint

Status: proposal only, based on develop `55fb7aaf1f260918e0803a3de47b97e88bab2b13`.
This document adds no workflow, upload command, credentials, activation or deployment.
Do not copy the phase sketch into `.github/workflows` as an operational workflow.

## Decision in plain terms

We have receipt consistency checks and separately reviewed archive/layout groundwork,
not a safe operational private-output producer. Existing reviewed CI produces clean
check evidence; it is not a private-output export channel. No main promotion or
security-setting change is authorized by this document.

Recommended design to evaluate: an owner-approved exact-source build in a disposable,
network-disabled sandbox, followed by a clean reporter, using a separately approved
private output store. Keep public GitHub artifacts limited to intentionally public,
bounded evidence. Do not upload raw output there, even temporarily.

Before implementation/activation, owner and service-owner decisions must identify:

1. **Trusted execution root:** immutable workflow/controller commit and protected
   execution mechanism. A workflow proposed on develop is not trusted merely because
   it names a protected environment. If the mechanism requires main promotion, stop
   for separate authorization; do not dispatch unreviewed develop code with secrets.
2. **Private SDK policy:** whether bundling the SDK into intended browser assets and
   server binaries is permitted, who may access those outputs, and acceptable residual
   risk from approved target code transforming or embedding package source. Shape,
   inventory and byte scans cannot prove non-disclosure. Offline execution stops some
   exfiltration routes, not malicious content in the eventual published app.
3. **Transfer boundary:** exact private store, tenant/project, reader/writer identities,
   object scope, retention/deletion policy and audit trail. No public repository artifact
   is an acceptable raw handoff. Any access/security change needs its own approval.
4. **Receipt authority:** authenticated reporter-output mechanism and separate receipt
   envelope identity, including issuer/audience, workflow/job/run/attempt attribution
   and tamper/replay resistance. A bare JSON file, timestamp or GitHub job name is not
   sufficient. Choose and review a supported attestation/signature mechanism before
   implementing a live adapter; this proposal does not assert one is configured.
5. **Release gate:** independently authenticated immutable readiness evidence for the
   exact source, migrations, Preview project/origin/app and policy; current required
   checks; target account isolation; and explicit protected Preview deployment authority.
   Invitation issuance and service registration are separate owner-controlled steps.

## Exact approved context

The independently reviewed plan must pin repository numeric ID/name, same-repository
source SHA, current develop base/ancestry, manifest and lock hashes, trusted workflow
path/SHA/ID, approved actor and rerun actor, run/attempt, Preview project ID and exact
HTTPS origin, app `meal-planner-preview`, enabled/screenshots flags, CLI/Go/builders,
route/schema policy hashes and readiness/migration identities. The origin candidate is
`https://athome-meals-dev.vercel.app`; live ownership/registration must be verified,
not inferred from this text. No token, key or user identity value belongs in the plan.

Use the existing receipt contract in
[`preview-receipt.md`](../../tools/framebriar-ci/preview-receipt.md).
Its `verifyReceipt` is policy consistency over **already authenticated** inputs,
not a GitHub API client, signature verifier, vulnerability scan or deploy permit.
The exact approved Go/toolchain pins require separate current security review.

## Non-runnable workflow blueprint

The following is deliberately a phase specification, not GitHub Actions YAML.
Each phase fails closed; no `continue-on-error`, branch fallback or cache fallback.

| Phase | Runs code from | Authority/data available | Required result |
| --- | --- | --- | --- |
| 0. Resolve and approve | Pinned trusted controller | Read-only GitHub metadata; independent plan | Verify repository, exact source/base, actor, workflow and current required checks; obtain protected approval |
| 1. Prefetch | Pinned trusted fetcher; no target scripts | Least-privilege package reader only during fetch | Verify reviewed manifests/lock/integrity; fetch exact dependencies/toolchain; lifecycle scripts disabled; no target checkout executed |
| 2. Build | Exact reviewed target in fresh sandbox | Explicit public build configuration; immutable dependency inputs; no credentials or network | Bounded build/tests produce output into private scratch; no public log/output upload |
| 3. Seal | Trusted host/controller after sandbox termination | Scratch output only; no target processes | Terminate all target descendants, detach scratch, create bounded independent copies and immutable handoff identity |
| 4. Inspect/report | Fresh trusted reporter, no target execution | Read-only sealed output; approved private-store writer | Independently validate full inventory, schemas, ELF architecture, public configuration and bounds; reject unknowns; upload only to approved private store |
| 5. Receipt | Trusted reporter/issuer | Authenticated output ID/digest and immutable readiness | Issue separate authenticated receipt after output creation; complete successful run; publish only explicitly approved evidence |
| 6. Consume | Pinned trusted consumer | Read-only private object access; authenticated GitHub/issuer metadata | Verify envelope, current plan/refs/latest run/attempt/readiness, downloaded digest and archive/layout; no target scripts executed |
| 7. Deploy | Separate protected Preview job | Deployment credential introduced only after all gates | Deploy validated prebuilt tree without package install/build hooks, verify exact deployed identity and smoke checks; never production |

A private-store output locator will need a reviewed contract extension: current receipt
artifact fields describe GitHub-style artifact identity/digest. Do not force a private
object URI into those fields or claim the existing module authenticates another store.
The output and receipt are separate objects. Output creation precedes receipt issuance;
consumer waits for successful completed reporter/run. Never infer reporter outputs from
REST job metadata alone. Clean reporter must not evaluate JavaScript, shell, package
scripts or executable files from the build output.

## Proposed sandbox boundary, not yet verified

A possible implementation is a disposable Linux VM with a non-root build container:
network disabled, dropped capabilities, no host Docker socket, no host home/workspace
mount, no cloud metadata access, read-only dependency inputs, explicit writable scratch,
resource/time/process limits and no inherited host environment. Preinstall pinned
Node/Go/CLI/builders in a separate trusted fetch phase so offline execution cannot fall
back to downloads. Target lifecycle/build scripts run only here, never in the
credential-bearing prefetch/controller/reporter context.

`env -i` alone is not isolation. Containers share a kernel and require an explicit
accepted threat model; a disposable VM is preferable when package/source sensitivity
warrants it. Credentials must never be mounted in the target container, even if later
unset. Read-only package inputs still expose SDK bytes to target code. Build logs and
failure output can contain those bytes: suppress public raw stdout/stderr, keep bounded
logs inside the approved private boundary, and emit only trusted allowlisted summaries.
Do not persist shared caches, npm configuration, traces or build diagnostics publicly.

Terminating the target precedes trusted inspection. Do not run a privileged uploader
beside live target processes or let target-created links escape scratch. The staging
primitive is not race-proof isolation. The proposed private handoff must be demonstrated
on the actual runner; existing synthetic/local layout tests do not establish it.

## What is verified versus still absent

Verified at this design baseline: develop contains pure receipt parser/consistency
checks and their adversarial tests; the existing reviewed-CI workflow separates registry
installation and target checks but does not export private output. PR #85 separately
contains archive, metadata, full-layout and bounded staging groundwork. Those primitives
must be reconciled at the eventual integrated SHA, not described as wholly missing.

Still absent from the runnable system: approved producer isolation and log confinement,
private transfer mechanism, authenticated receipt envelope and GitHub/provider adapter,
independent immutable readiness producer, protected consumer/deploy wiring, and exact
integrated-source release evidence. Earlier diagnostic output included a temporary Go
patch and is not proof of any unchanged release commit. None of this document verifies
live Framebriar registration, invitation issuance or end-to-end Preview feedback.

## Next safe code slice and acceptance tests

After decisions 1–4, implement a pure adapter-normalization contract with recorded,
redacted synthetic API/attestation fixtures first. It must distinguish authenticated
transport from data validation and reject incomplete pagination, unknown issuer/audience,
wrong workflow/job attribution, stale refs, fork runs, failed newer attempts and untrusted
rerun actors. Do not introduce network access, credentials or an upload workflow merely
to test that contract. Existing receipt code should be reused, not duplicated.

Before any activation, independent QA then Judge must cover:
- Target tries network/DNS/metadata endpoints, reads host credentials, escapes mounts,
  leaves background processes, exhausts resources or emits SDK source in logs.
- Target embeds private bytes into valid JS/binaries: explicitly demonstrate that schema
  validation cannot detect all such disclosure; resolve through source review/policy.
- Unknown paths, links/special files, oversized archives, duplicate/escaped JSON keys,
  malformed metadata, architecture mismatch, changed assets and concurrent mutation.
- Same artifact name with wrong repository/run/attempt/workflow/job, expired/deleted or
  replaced object, wrong digest, forged reporter outputs or receipt envelope.
- Source/base/manifest/lock/pin/policy/readiness changes after approval; old receipt replay,
  newer failed/pending run, rerun actor change, incomplete API inventories or API failure.
- Upload denied/interrupted, receipt issuance interrupted, object retention expiry,
  consumer failure and deploy failure: no fallback to public upload or fresh target build.
- Exact integrated-source rebuild, clean consumer validation, Preview-only deployment and
  end-to-end sign-in/enrollment/reporting without exposing invitations or account data.

This design is ready for review, not activation. A follow-on operational change needs
all decisions recorded, exact-head QA/Judge, and separately authorized publication,
trusted workflow installation, access changes and deployment as applicable.
