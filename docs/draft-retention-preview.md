# Draft retention readiness (no cleanup authorized)

Status: aggregate inspection and decision packet only. No purge, DELETE, account
cleanup, retention promise, live database access or scheduled job is implemented
or authorized.

`tools/deploy/inspect-draft-retention-readiness.sql` is a timeout-bounded,
read-only query for a future private clean inspector. It returns only aggregate
counts by pairing/grant/intent/review state, owners at current retained-cap
thresholds, and two integrity counts: accepted intents without their hosted
submission and approved reviews without their recipe. It returns no owner,
identifier, hash, credential, draft content or recipe content. Its `as_of` input
must come from the trusted inspector clock. A target-provided result or public
artifact is not evidence.

The inspection deliberately does not decide what to delete:

- Pairing deletion changes how late poll replays distinguish expired/consumed
  work from an unknown pairing. Terminal codes should be scrubbed, but the
  replay/tombstone lifetime needs an explicit policy.
- Grant retirement must not remove a grant needed to authorize a still-pending
  intent. Expiry/revocation ends use, but audit and lost-response behavior still
  need a chosen retention period.
- Intent content duplicates the accepted hosted snapshot, but its operation key
  and terminal result protect retry semantics. Content may be shortened only
  after a tombstone design preserves idempotency and safe polling.
- `draft_ingest_submissions` and `draft_reviews` hold the canonical hosted draft,
  provenance and saved-recipe link. Do not purge them before Caroline chooses
  archive/discard/account-deletion semantics.
- Membership epochs are the manual removal/re-add revocation boundary and must
  not be pruned merely because no active grant is visible.

Recommendation for the first synthetic-only Preview acceptance: do not add an
automatic destructive job. Require all aggregate integrity counts and
owners-at-cap counts to be zero, use only synthetic content, revoke the test
grant, disable the gates after acceptance, and keep longer Preview use blocked
until Caroline chooses the tombstone/content/account-deletion lifetimes. Any
manual deletion still requires an exact migration-owner query, private before/
after aggregates and separate action-time approval.

The disposable PostgreSQL 18 test seeds expired pairing/grant/intent state and
proves the query reports aggregate state without returning its synthetic owner or
hash canaries. PostgreSQL 17 and live Neon remain untested.
