# Architecture Plan

Status: planning baseline, 2026-11-01

This document describes architectural direction and conceptual boundaries. It deliberately contains no executable API schema, database schema, migration, handler, or generated client. The repository now has a verification skeleton (see [repository layout](repo-layout.md)); the components below remain proposals unless stated otherwise.

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
- Source control and future CI: GitHub, with GitHub Actions CI scaffolding starts ahead of Phase 3 on a hello-world verification slice (see the [CI/CD plan](ci-cd-plan.md)).
- Persistent product data: structured recipes, plans, and feedback in Postgres.

Vercel officially supports Go HTTP functions, but documentation support is not proof that this application's workload will fit its routing, duration, concurrency, observability, or cost constraints. A workload-fit checkpoint is required early in implementation.

### Release units

The monorepo has separate builds for the web client, hosted Go API, and local Go tools. The frontend and hosted API are initially one coordinated release unit, subject to the Vercel workload-fit checkpoint. The local CLI and MCP adapter are versioned and released separately. A local-tool release must prove compatibility with every supported hosted API version before publication.

### Local-tool update behavior

The approved user-facing update policy is:

- The CLI discovers stable releases through GitHub Releases using CLI-specific release metadata rather than treating the web application's version as the CLI version.
- A bounded update lookup may run when the CLI is used, but a cached result limits it to at most once per day.
- A lookup failure never blocks a normal command.
- Installation is user-controlled; the tool does not silently update itself.
- Update notices do not pollute JSON output, scripted standard output, or the MCP protocol stream.
- The MCP adapter exposes a status tool, and its skill convention can use that tool to report an available update. This does not guarantee that every MCP host will proactively notify the user.
- An optional newer release and an unsupported operation are distinct states with different messages.
- After updating the MCP executable, the host must restart the MCP process before the new version is active.

The installer mechanism, artifact format, signature system, checksums, platform packaging, and exact GitHub Release metadata remain implementation decisions.

### Authority boundaries

- The hosted API owns validation, authorization, and production database writes.
- Browser and local tools do not receive unrestricted Neon credentials.
- Local tools use a narrow, authenticated draft interface.
- The reusable local core contains provider-independent orchestration and domain transformations that are useful to both CLI and MCP adapters.
- CLI and MCP layers remain thin transport adapters; they do not become separate business-logic implementations.
- The logged-in web experience lets the user generate a plan from stored saved recipes without requiring a live agent.
- An existing agent host may call tools, but the product does not introduce a separate agent platform.

The user-facing behavior above is settled; the computation placement is not. Plan generation may ultimately run in the web client or behind the hosted API. The implementation must select one authoritative placement rather than duplicate the generation rules in both. In either case, the hosted API remains responsible for authorization, validation at its trust boundary, and persisted plan writes.

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

The current hosted API and web client directories are recorded in the [repository layout](repo-layout.md). The local draft restore candidate uses the package boundaries below.

## Shared draft core and local restore candidate

The web release retains the transport-independent `internal/core/draft` model, structural validation, service and persistence interface, with the in-memory store and conformance tests. Hosted manual recipes use its `ValidJSONString` helper to reject malformed Unicode before decoding. The core imports only the standard library and does not import a file store, CLI, MCP SDK or hosted service.

The #23 candidate restores the file store (`internal/localstore`), MCP adapter (`internal/mcpadapter`) and local draft commands from epic #19. The candidate is pending independent review and renewed #25 host acceptance. The November web release retains the shared core and command scaffolds.

The [local draft contract](local-draft-contract.md) defines the shared core and local tools. The workflow stages drafts locally without replacing hosted Postgres; it does not provide a reviewed or approved state, hosted save, import, sync, source fetching or an application LLM call. It does not satisfy Checkpoint E. Local tool and host-compatibility acceptance must be completed in a separate release before those tools are offered for use.

## Proposed contract shape

The future contract should use an explicit OpenAPI description or equivalent shared schemas, generated clients where they reduce drift, and contract tests. Those artifacts are implementation work and are not included now.

Conceptual resource families:

- **Recipe drafts** — submit an extracted draft, record provenance, review prep-ahead suggestions, approve or reject the structured result.
- **Recipes** — read reviewed recipes and, if launch scope approves it, update reviewed content.
- **Weekly plans** — create and read a plan, change meal placement, and recalculate affected derived lists. The eventual contract will reflect the single selected generation placement: either a server-side generation operation or validation and persistence of a client-generated candidate, not both implementations.
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
- **Proposed safeguard, pending the open versioning and concurrency decision:** plan changes need an explicit consistency mechanism so shopping and preparation progress cannot silently drift. Versioned domain operations are one candidate, not a settled requirement.
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

Also prove that local release checks reject an incompatible hosted API or unsupported operation without confusing that state with an optional update notice.

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
