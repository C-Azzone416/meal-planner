# Synthetic local handoff demonstration (not an operational exporter)

Run `node --test tools/framebriar-ci/synthetic-handoff.test.mjs`. Existing CI's
`*.test.mjs` pattern includes these tests. Only public fake SDK bytes, synthetic
identities and disposable local directories are used. No real SDK, credentials,
GitHub/provider requests, upload, workflow or deployment is involved.

The test controller starts a fixed Node fixture without a shell, inherited environment
or Node exec arguments, in a new process group. It waits for a bounded readiness message,
sends TERM then KILL, waits for leader close and confirms the group has disappeared
before reading output. A TERM-resistant background child tries repeated writes; the
group is terminated and its unexpected file is rejected. No uncontrolled escaped-child
fixture is launched. A malicious process that creates another session could escape
this lifecycle mechanism: it is NOT a sandbox or sufficient for untrusted builds.

A two-file allowlist, size limits, regular independent files, O_NOFOLLOW opens and
before/after checks constrain the synthetic copy. New private staging receives exclusive
independent copies, then is inspected again. A subsequent original-file mutation does
not alter the copy. Symlinks, hardlinks, FIFO, extra/missing/oversized files and a hung
producer fail. One explicitly named fake diagnostics file is checked for type/size and
excluded, not published. This miniature copier does not replace the production archive,
metadata/layout or descriptor-pinned staging primitives in PR #85.

The reporter builds a deterministic JSON package digest and inventory digest; these are
NOT production USTAR or GitHub artifact digests. The receipt, plan and evidence are
explicit in-memory authority fixtures with synthetic dates/IDs. The existing receipt
parser and verifyReceipt consume them after independent staged-byte reinspection.
Tampered receipt/context or output fails. There is no signature, authenticated envelope,
GitHub adapter or readiness authority here. A passing test proves fixture consistency,
not authorization to publish or deploy.

The encoded fake-source test intentionally passes shape/digest checks while retaining
a recoverable fake private-source canary. This demonstrates why those checks cannot
prove secrecy. Nothing sensitive is used. No target output is printed or uploaded.

## OS capability limitation: visible skipped test

Read-only inventory found Darwin sandbox-exec and a Docker executable. Docker was not
started; no service or security settings changed. The parent's default-context sandbox
probe failed before execution: `sandbox_apply: Operation not permitted`. An initial
scoped per-process canary probe in this test author's execution context ran but did not
block the synthetic canary read (exit 2); its network probe was not reached. No isolation
success is inferred from either result, and no bypass or further sandbox retry followed.

The suite records one explicit skipped capability test rather than passing off these
probes as protection. The ordinary lifecycle demo is intentionally unrestricted and
never advertised as sandbox mode. It attempts no network traffic. Real filesystem and
network denial, host-credential isolation, kernel/VM trust, safe logging and hostile
process containment still need a separately reviewed implementation and evidence.

The fixture supports POSIX process groups and a fixed `/usr/bin/mkfifo`; current target
CI uses Ubuntu and local verification uses macOS. It is not a Windows harness. Temporary
fixtures are removed in finally blocks; cleanup never touches project or user files.
