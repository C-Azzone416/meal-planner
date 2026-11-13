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
