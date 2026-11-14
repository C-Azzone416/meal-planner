# Inert private Preview receipt binding contract

This is **pure validation groundwork**, independent of the Framebriar feature
branch. It does not implement a producer, upload, receipt publisher, downloader,
credential-bearing consumer or deployment workflow. The proposed workflow path
constant is an identifier only; no such runnable workflow is added here.

## API and prerequisite trust

1. A separately reviewed adapter authenticates the **receipt envelope** and its
   payload from the approved clean reporter. This module does not implement that
   envelope authentication. The receipt artifact is distinct from the output
   artifact named inside the receipt.
2. Decode UTF-8 strictly, then call `parseReceipt(rawText)`. It bounds input size
   and nesting and rejects duplicate JSON keys before they can be discarded,
   including nested and escaped-equivalent keys. Do not replace this with plain
   `JSON.parse` when receiving external bytes.
3. `verifyReceipt(receipt, plan, evidence, nowMs)` checks bindings against a plan
   from independent approval/configuration and evidence from authenticated
   GitHub/provider observations. **Never construct the plan, readiness evidence,
   reporter outputs or expected inventory by copying fields out of the receipt.**
4. A successful return contains immutable public identities only, not a deploy
   permit. Required checks, workflow environment protection, security approval,
   archive/layout validation, private-source policy and live readiness remain
   independent gates.

The caller could supply fabricated evidence to any pure function. This function
cannot distinguish that from authenticated metadata. No self-asserted `approved`
flag is accepted, and no claim of live authentication is made by these tests.

## Bindings

The closed receipt/plan binds repository ID/name, exact source and current develop
base, protected workflow path/SHA/ID, run/attempt and approved actor, manifests,
Preview project/origin/app, feature flags, resolved CLI/Go/builder versions,
immutable readiness identity/digest, migration digest and schema/routes policies.
The plan's explicit Go version is an independently reviewed pin, **not a
vulnerability scan or minimum-version policy implemented by this module**.

Additional receipt fields bind clean reporter job, output artifact ID/name/digest,
inner archive/inventory hashes, bounded file/byte counts and a short validity
window. Outer artifact digest and inner archive hash are distinct.

Evidence must include complete latest-run and attempt inventory, current refs,
source/base ancestry, exact successful producer identity and original/rerun actor,
successful clean reporter, output artifact, independently inspected downloaded
content and separately authenticated immutable migration readiness.

`reporter.artifactId`, `artifactDigest`, `archiveSha256`, and `inventorySha256`
are **trusted reporter outputs**, NOT fields available from ordinary GitHub job
REST responses. A future adapter needs an authenticated provenance mechanism for
this exact job's outputs. Job name plus timestamp overlap is insufficient.
The adapter must also attribute and authenticate the separate receipt envelope;
this slice intentionally leaves that mechanism unimplemented rather than accepting
an invented boolean or unsigned builder claim.

## Feasible chronology

Within the clean reporter: inspect and package output → upload output and obtain
its ID/digest → issue receipt binding that output → publish a separate receipt
artifact. The output creation time must be within the clean reporter's start and
receipt issuance; receipt issuance must precede reporter completion. The consumer
accepts only a completed successful run and unexpired receipt, and rejects any
newer run or attempt. Receipt-envelope publication/authentication is a separate
adapter responsibility, not conflated with the output artifact's timestamp.

## Disclosure boundary and remaining implementation

**Uploading raw private build output to a public repository before validation
already discloses it. A clean reporter cannot undo that exposure.** This slice
adds no upload path. A real handoff needs a separately reviewed private transfer
boundary or another explicitly approved design before any raw private output
leaves the isolated builder.

A digest/inventory and byte scans prove neither transformed-source secrecy nor
that executable output cannot emit private data. No private-source risk is
accepted here. Protected installation, credential-free target execution, clean
reporter isolation, safe raw-output handoff, immutable readiness production,
receipt-envelope authentication, live GitHub metadata adapter, download/digest
verification, archive/layout validation and protected Preview deployment are all
still missing from the runnable system.

The archive, metadata-schema and layout/staging primitives proposed separately
in PR #85 are relevant groundwork, not absent work. Earlier boundary documents
that say schema/layout validation is wholly missing should be reconciled after
those changes land; this receipt slice neither copies them nor depends on the
unmerged feature source.

## Verification

Run `node --test tools/framebriar-ci/preview-receipt.test.mjs`. Tests are included
by the existing `tools/framebriar-ci/*.test.mjs` CI invocation without workflow
changes. Synthetic cases cover approved-plan changes; same-name/wrong identity,
repository, workflow, run, attempt and reporter; fork metadata; latest failed or
pending runs; readiness target/source/policy changes; downloaded hashes/counts;
self-asserted approval; unknown keys/types; duplicate/oversize/deep JSON; malformed
timestamps; chronology, expiry and old-attempt replay.
