# Architecture Plan

Status: planning baseline, 2026-11-01

This document describes architectural direction and conceptual boundaries. It deliberately contains no executable API schema, database schema, migration, handler, or generated client.

## Settled architecture

### System shape

Use a modular monorepo rather than microservices:

```text
Responsive React web client
            |
            | authenticated HTTPS
            v
Hosted Go API (validation, authorization, business operations)
            |
            | narrowly scoped database access
            v
Neon Postgres + Neon Auth

Existing agent host -> thin CLI or MCP adapter -> reusable local Go core
                                              -> authenticated draft API
```

The arrows describe responsibility boundaries, not implemented components.

### Technology direction

- Backend and reusable local core: Go.
- Web client: TypeScript and React, responsive for phones and tablets.
- Hosted compute: Vercel is the intended first target.
- Database and application authentication: a separate Neon project using Neon Postgres and Neon Auth.
- Source control and future CI: GitHub, with GitHub Actions deferred until implementation.
- Persistent product data: structured recipes, plans, and feedback in Postgres.

Vercel officially supports Go HTTP functions, but documentation support is not proof that this application's workload will fit its routing, duration, concurrency, observability, or cost constraints. A workload-fit checkpoint is required early in implementation.

### Authority boundaries

- The hosted API owns validation, authorization, and production database writes.
- Browser and local tools do not receive unrestricted Neon credentials.
- Local tools use a narrow, authenticated draft interface.
- The reusable local core contains provider-independent orchestration and domain transformations that are useful to both CLI and MCP adapters.
- CLI and MCP layers remain thin transport adapters; they do not become separate business-logic implementations.
- The web client can generate a plan from saved recipes without requiring a live agent.
- An existing agent host may call tools, but the product does not introduce a separate agent platform.

## Proposed repository boundaries

The following conceptual modules are proposed, not yet created:

| Boundary | Responsibility | Must not own |
| --- | --- | --- |
| Web client | Responsive interaction, session-aware API calls, cooking presentation | Authorization decisions or direct database credentials |
| Hosted API | Request validation, authorization, domain operations, persistence orchestration | UI state or provider-specific agent behavior |
| Domain packages | Product rules and pure transformations | HTTP, CLI, MCP, or database transport |
| Persistence adapter | Postgres queries and transactions | Authentication policy or user-facing behavior |
| Local core | Draft ingestion, transformation, and authenticated API calls | Production database access |
| CLI adapter | Human-invoked commands and readable output | Duplicate domain logic |
| MCP adapter | Narrow tool definitions and request mapping | Broad account access or unrestricted data mutation |

Final directory names are intentionally deferred until the first implementation slice establishes real dependency boundaries.

## Proposed contract shape

The future contract should use an explicit OpenAPI description or equivalent shared schemas, generated clients where they reduce drift, and contract tests. Those artifacts are implementation work and are not included now.

Conceptual resource families:

- **Recipe drafts** — submit an extracted draft, record provenance, review prep-ahead suggestions, approve or reject the structured result.
- **Recipes** — read reviewed recipes and, if launch scope approves it, update reviewed content.
- **Weekly plans** — generate from eligible saved recipes, read a plan, change meal placement, and recalculate affected derived lists.
- **Shopping lists** — read derived ingredients and record progress if progress tracking is approved.
- **Preparation lists** — read tasks grouped by time and meal ownership; record progress only if approved.
- **Feedback** — record the minimal approved outcome and timing observations.
- **Sessions or identity exchange** — obtain or present an application-scoped identity for web and local tools.

### Contract rules to decide before specification

- stable identifier format and idempotency behavior;
- optimistic concurrency and conflict handling;
- whether derived lists are stored, recomputed, or versioned;
- how swaps and recipe edits invalidate derived data;
- error envelope, pagination, and audit metadata;
- versioning and backward-compatibility policy for CLI and MCP clients; and
- the exact authentication flow available to local command-line clients.

## Authentication and authorization

### Settled security posture

- Public source does not mean public user data.
- Every user-data request is authenticated and authorized server-side.
- The API derives ownership from a verified identity; it does not trust user IDs supplied in request bodies.
- Database connection strings, signing material, OAuth secrets, and provider tokens remain outside Git.
- Production credentials are limited to the service that needs them and separated from local Microsoft credentials, agent login, and browser sessions.
- Logs and telemetry must exclude recipe text, tokens, connection strings, and other sensitive payloads by default.
- Draft ingestion is least-privilege and cannot call arbitrary database operations.

### Neon Auth proposal

Neon Auth is the selected application-auth direction. Its documented session and JWT support is promising for a browser session plus API verification model. Exact login methods, CLI device or browser flow, issuer and audience validation, token lifetime, revocation, and trusted-origin configuration remain implementation checkpoints.

The first authenticated request must prove all of the following before broader feature work:

- browser sign-in creates a valid session;
- the Go API validates the token using the supported Neon Auth metadata;
- the API derives a stable subject and rejects missing, expired, malformed, wrong-issuer, and wrong-audience tokens;
- one user cannot read or mutate another user's records; and
- local CLI authentication does not require a long-lived unrestricted database credential.

### Future source-note import

If source-note import is approved later, delegated Microsoft Graph access may require a scope broader than one page. The importer must therefore add application-level restrictions so it can read only explicitly selected notebooks, sections, or pages. Microsoft credentials remain local and must never be stored in this repository, browser code, or application database. Original notes are never modified or deleted.

## Data and derived state principles

- Store reviewed, structured recipes separately from their source and from unreviewed generated suggestions.
- Preserve provenance and review state so untested generated guidance cannot be mistaken for verified content.
- Model active time, total ready time, attention, cleanup, and timing variability independently.
- Preserve the meal ownership of ingredients and preparation tasks even when producing consolidated views.
- Treat serving conversion and ingredient consolidation as domain rules with explicit tests, not display-only math.
- Treat plan changes as versioned domain operations so shopping and preparation progress cannot silently drift.
- Do not encode storage or food-safety claims without a source and review policy.

## Verification checkpoints

### Checkpoint A: platform fit

Before feature development, prove a minimal Go HTTP path can run in the intended Vercel project with acceptable routing, cold-start behavior, timeouts, logs, and connection handling. This is a disposable verification slice, not a product feature.

### Checkpoint B: authenticated ownership

Prove Neon Auth to Go API token validation and cross-user isolation before adding recipe operations.

### Checkpoint C: contract workflow

Approve the conceptual resources, write the executable contract, generate or validate clients, and add contract tests before implementing multiple adapters.

### Checkpoint D: persistence behavior

Prove transaction boundaries, pooled serverless connections, migrations, backup or restore expectations, and preview isolation before storing meaningful data.

### Checkpoint E: local-tool boundary

Prove one reviewed draft can travel through the reusable core and narrow API using an application-scoped identity. Verify that CLI and MCP adapters share the same core and permissions.

### Checkpoint F: first product slice

Only after the earlier checkpoints, implement one end-to-end reviewed-recipe-to-plan path and measure correctness, effort representation, and change propagation.

## Risks and mitigations

| Risk | Planning mitigation |
| --- | --- |
| Go runtime does not fit the workload | Run Checkpoint A early and keep hosting behind standard HTTP boundaries. |
| Browser and CLI auth capabilities diverge | Treat both as Checkpoint B outcomes before feature growth. |
| Generated prep guidance is unsafe or misleading | Separate generated from reviewed data and require a sourcing policy. |
| Swaps cause stale shopping or prep progress | Define versioning and invalidation rules before those features. |
| Public repository leaks private data | Keep data and identifiers out of Git, add secret scanning, and review every publication. |
| Adapters duplicate business logic | Keep shared behavior in the local core and enforce dependency boundaries. |

## Official references

These references establish provider capabilities only; they do not resolve application-specific fit.

- Vercel Go runtime: https://vercel.com/docs/functions/runtimes/go
- Vercel Functions limitations: https://vercel.com/docs/functions/limitations
- Vercel Hobby plan: https://vercel.com/docs/plans/hobby
- Neon project management: https://neon.com/docs/manage/projects
- Neon Auth overview: https://neon.com/blog/neon-auth-branchable-identity-in-your-database
