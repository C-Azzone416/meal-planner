# Meal Planner

Meal Planner is a planned web application for reducing the effort required to cook varied dinners at home. The repository now includes a navigable, responsive UI preview and a fail-closed authentication boundary. The UI starts with clearly labeled sample data. Manual recipe storage now has an authenticated Neon-backed API and app integration, pending the reviewed migration and runtime setup. Week planning and preview data remain browser-local, separately for each authenticated account.

> **Preview status:** The repository has CI workflows, hosted health and authentication functions, a React UI preview, the existing version-only `mealplanner` CLI and protocol-clean `mealplanner-mcp` scaffold. Local recipe-draft commands, file storage and MCP tools are deferred from this web release. The UI covers the planned views and uses invitation-only email-and-password authentication when its provider is configured. Existing invited accounts set or recover a password with a short-lived email code. Hosted manual recipes have a reviewed-migration path; hosted planning and hosted MCP draft submission are not implemented. The app fails closed for hosted saves until the database/runtime configuration is prepared. Browser-local planning is not a database-backed service.

## Product direction

The initial experience is centered on:

- generating a week with three varied dinners from saved recipes;
- adjusting a default serving range of four to six;
- planning for one person to prepare and one person to cook, without assuming simultaneous help;
- keeping weekend preparation around 30–45 minutes and weeknight active work around 10–15 minutes where practical;
- tracking active effort, total ready time, attention, cleanup, and timing variability instead of optimizing only for elapsed time;
- presenting each recipe as **prep ahead**, **prep tonight**, and **cook tonight**; and
- allowing meal nights to move without treating repeated batch leftovers as the main strategy.

The application will keep user data private even though the source repository is public.

## Planning documents

- [November UI release scope](docs/releases/ui-auth-disabled-2026-11.md) — complete included surfaces, dependencies, judge regressions and code-only deployment gates.

- [Product plan](docs/product-plan.md) — settled product decisions, proposed launch scope, unresolved rules, and deferred ideas.
- [Architecture plan](docs/architecture.md) — system boundaries, conceptual API shape, security posture, and verification checkpoints.
- [Setup and delivery plan](docs/setup-and-delivery-plan.md) — provider setup, implementation phases, and release gates.
- [CI/CD configuration plan](docs/ci-cd-plan.md) — branch protections, affected-component CI, deployment and release sequencing, configuration inventory, and verification steps.
- [Repository layout](docs/repo-layout.md) — component map and check commands for the current skeleton.
- [Local draft contract](docs/local-draft-contract.md) — shared-core specification and deferred local-tool design; not executable setup guidance for this release.
- [Hosted manual recipes](docs/runbooks/recipes.md) — API, migration, least-privilege runtime configuration and Preview verification gates.
- [Authentication runbook](docs/runbooks/authentication.md) — environment-specific allowlisting, provider wiring, and verification gates.

## Deferred local draft tools

The executable and file-store surface of local recipe-draft epic #19 is excluded from this web release. Its implementation and host-verification work remain in develop and pre-split release history. This release preserves the existing version-only CLI and MCP scaffold, build archives, smoke tests and separately gated CLI publication workflow. It does not install a local store, expose MCP draft tools or change an agent host's configuration.

The transport-independent `internal/core/draft` package, its tests and synthetic fixtures remain. Hosted manual recipes reuse its strict JSON-string validation to reject malformed Unicode before decoding. The [local draft contract](docs/local-draft-contract.md) remains the shared-core specification and a design for a future separately reviewed local-tool release; its CLI and MCP examples are not available in this release.

## Current boundaries

The current UI supports hosted manual recipe add/edit after database setup, plus separately labeled browser-local recipes and meal planning with shopping, prep and cooking views. Browser clearing or device loss can lose that data; it does not sync between browsers. Failed saves show an unsaved warning and a retry action. Legacy browser data without an account owner is preserved separately and is not automatically imported into an authenticated account. In particular, the repository does not yet include:

- hosted planning, feedback or MCP draft operations;
- automatic migration execution or real-user seed data;
- generated application clients;
- local draft CLI commands, local draft file storage, MCP draft tools, or hosted draft save; or
- importing or modifying source recipes.

Provider project creation is an organizational setup step. Hosted deployment of the skeleton is governed by the [CI/CD plan](docs/ci-cd-plan.md); manual recipe persistence is gated by the separate database runbook.

## Public repository policy

Only generic product and engineering plans belong in this public repository. Do not commit real recipes, household details, account identifiers, provider dashboard links, credentials, tokens, connection strings, or private usage data. Local provider references belong in the ignored `.setup/` directory.

## Status

Planning baseline: **ready for review**. Any item labeled **Proposed** or **Unresolved** requires a decision or validation before it can become an implementation requirement.
