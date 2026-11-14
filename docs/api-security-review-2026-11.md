# Recipe API security review: November 2026

Scope: hosted manual recipes and their bearer identity boundary. This is not a
certification, live-provider acceptance, MCP bridge implementation or Production
activation approval. All examples and local database tests use synthetic data.

## Boundary and findings

- The API derives owner issuer/subject only from the verified bearer identity.
  Every recipe and retry-receipt query binds both owner values. Foreign and absent
  recipe IDs use the same not-found result; clients cannot set owner/status/origin.
- SQL values are parameters, not interpolated recipe content or identity strings.
  The additional adversarial integration matrix tests equal subjects across
  issuers, different subjects under one issuer, SQL metacharacters in identities
  and content, foreign reads/updates, and equal idempotency keys across owners.
- A malformed Ed25519 signature previously caused a provider JWKS request before
  rejection, including an unavailable response during provider outage. Rejecting
  invalid encoding/length before network I/O avoids that unnecessary amplification.
  It does not bypass signature verification or fix general request flooding.
- Request/query/JSON bounds, strict object-key handling, generic storage errors,
  no-store responses, verified TLS configuration and transaction-local timeouts
  remain required. Tests must preserve these boundaries.

## Residual risks and deployment gates

- No database row-level security exists. The dedicated runtime credential can
  access all recipe rows; server authorization is the trusted tenant boundary.
  A leaked runtime credential is therefore a recipe-data incident, even though
  it must not grant auth-schema access, role administration or object ownership.
  Do not describe application-level isolation as database-enforced isolation.
- Well-formed invalid tokens still require key retrieval. Deployment/provider
  abuse controls and aggregate serverless concurrency/cost limits need separate
  verification; this patch is not a rate limiter or a JWKS cache.
- JWTs remain bearer credentials until expiration. Membership is re-evaluated
  per request, but local logout alone is not server-side revocation of a stolen
  JWT. Keep token lifetimes short and verify provider/session behavior separately.
- Local disposable Postgres tests cannot prove the live Neon role's effective
  grants, TLS, Vercel runtime secret injection, or save/reload across real sessions.
  The Preview acceptance checklist in `runbooks/recipes.md` remains mandatory.
- Toolchain/dependency vulnerability scanning and the actual deployed Go runtime
  must both be checked. Passing tests on an older local Go toolchain does not
  establish that a deployed artifact includes security patches.
- No MCP listener, hosted MCP publishing route, credentials, allowlist, Production
  settings or database grants are added by this change.

## Reproducible checks

`go test -race ./...`, `go vet ./...`, and
`TEST_DATABASE_URL=<disposable-loopback-database> go test -race -tags=integration ./hosted/...`.
The integration harness refuses remote database targets and creates isolated
throwaway databases. Run it only on a disposable local/CI cluster. Keep test
passwords/connection strings out of reports and public artifacts.

## Follow-up: bounded abuse and stolen credentials

The bearer verifier also rejects malformed claims, wrong issuer/audience, expired
or not-yet-active tokens, and malformed NumericDates before fetching signing keys.
These are untrusted rejection filters only. Signature verification is still
mandatory before identity is returned or private membership is consulted. Time
claims are rechecked after key retrieval so a token expiring during provider I/O
cannot gain an extra validity window. No signing-key cache or authorization cache
is introduced; no stale-key/revoked-member acceptance window is added.

This reduces wasted provider work but does not establish a per-IP or global rate
limit. A syntactically plausible forged token still forces a verification attempt.
An in-process limiter alone is insufficient across serverless instances and can
let an attacker exhaust the local budget ahead of a legitimate caller. Before
public activation, review platform-level request/concurrency/spending limits and
provider abuse controls, including reset-email requests, with explicit approval
for any security-settings changes. Do not trust an arbitrary forwarded-IP header
or store raw tokens/emails as telemetry/rate-limit keys.

For stolen JWTs, do not impose an arbitrary maximum token age without confirming
the provider's actual issuance and refresh contract. Requiring a new claim or
shorter lifetime blindly can lock out real users without revoking provider
sessions. The existing next-request membership-removal boundary is an emergency
application-access lever, not provider-wide revocation; changing it requires the
appropriate approval. Session introspection or a persisted per-subject revocation
cutoff would need an availability/fail-closed design, verified issuance times,
reset/logout integration, retention rules and failure/replay tests before rollout.

### Database isolation feasibility

RLS can provide defense against an accidentally omitted owner predicate, but a
policy reading a caller-set session variable is not by itself protection against
arbitrary SQL using that same runtime role: the role can set the variable to a
victim's identity. Never claim that such a policy makes a leaked runtime secret
safe. Any future design should use a non-owner, non-BYPASSRLS runtime role, forced
policies where appropriate, transaction-local verified identity, and tested pool
reuse/rollback/cancellation behavior. All three recipe/receipt tables and every
operation must be covered, not just reads. A stronger database-enforced boundary
requires a separately verified identity/capability that a compromised runtime
cannot freely forge. That is a reviewed architectural change, not a quick grant
or migration applied overnight. No RLS policy, grants or credential changes are
included in this follow-up.

Reference semantics: [PostgreSQL row security](https://www.postgresql.org/docs/current/ddl-rowsecurity.html),
[transaction-local settings](https://www.postgresql.org/docs/current/sql-set.html),
and [Neon session/JWT guidance](https://neon.com/docs/auth/guides/plugins/jwt).
The warning about caller-set identity variables is a design inference from those
boundaries, not a claim that a new RLS implementation was deployed or tested.
