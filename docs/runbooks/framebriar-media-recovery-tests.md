# Synthetic screenshot recovery regression coverage

The feature plus current develop is tested without changing application runtime code.
The Go browser fixture uses real application authentication, SQL persistence and the
feedback HTTP adapter, but substitutes the Framebriar provider. It never calls the
live service or uses real invitations/credentials.

Two browser cases send an explicitly selected screenshot, accept metadata, then fail
media with HTTP 502 or return a mismatched final receipt. Both verify no Received,
exactly one unsent item, close the dialog, reload the fixture document in the same
browser storage, and recover after the synthetic failure is removed. The test checks
nonempty metadata/media attempts, unchanged report ID and image digest, one unique
accepted report and an explicit zero-item outbox after the matching receipt.

The app auto-closes the dialog after local save; the test reopens and closes it before
reload. Reload navigates to the synthetic entry because the test router changes the
URL to /week, whose ordinary Vite reload would initialize real auth rather than the
fixture. No API reset or storage reset occurs during recovery.

The local ephemeral Playwright WebKit profile failed a standalone IndexedDB Blob write
without this app or SDK (UnknownError preparing Blob/File data); Uint8Array storage
worked. The same synthetic Blob worked in a disposable persistent WebKit profile.
Screenshot durability tests therefore use a private temporary persistent iPhone WebKit
profile and remove it after browser close. Other tests retain ordinary contexts. This
is not a runtime workaround or proof of a defect in physical Safari. All browser HTTP
fixtures stay on loopback, and external HTTPS requests are blocked.

Go adapter tests cover 502 then matching ready receipt, wrong ID/app, still-pending
receipt, expired retry ticket, and metadata already-ready on retry. Metadata/media
bytes and ID remain stable; retries use the newly returned upload ticket.

Local verification uses the previously installed pinned SDK 0.4.1 via a local dependency
link, not a clean credentialed install. No SDK implementation is copied or vendored.
These tests model expected provider idempotency/ticket renewal; live Framebriar contract,
Preview deployment and actual final image receipt still require separate verification.
