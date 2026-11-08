# Meal Planner

Meal Planner is a planned web application for reducing the effort required to cook varied dinners at home. The repository now includes a navigable, responsive UI preview and a fail-closed authentication boundary. The UI starts with clearly labeled sample data. Recipe and planning changes are saved only in this browser, separately for each authenticated account; there is no hosted sync or backup.

> **Preview status:** The repository has CI workflows, hosted health and authentication functions, a React UI preview, a `mealplanner` CLI (`version`, plus read-only `draft validate/list/show`), and a stdio `mealplanner-mcp` server for local recipe drafts. The UI covers the planned views and uses invitation-only email-and-password authentication when its provider is configured. Existing invited accounts set or recover a password with a short-lived email code. There is no hosted recipe planning, hosted draft save, database schema, or migration yet. Browser-local planning is not a database-backed service.

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

- [Product plan](docs/product-plan.md) — settled product decisions, proposed launch scope, unresolved rules, and deferred ideas.
- [Architecture plan](docs/architecture.md) — system boundaries, conceptual API shape, security posture, and verification checkpoints.
- [Setup and delivery plan](docs/setup-and-delivery-plan.md) — provider setup, implementation phases, and release gates.
- [CI/CD configuration plan](docs/ci-cd-plan.md) — branch protections, affected-component CI, deployment and release sequencing, configuration inventory, and verification steps.
- [Repository layout](docs/repo-layout.md) — component map and check commands for the current skeleton.
- [Local draft contract](docs/local-draft-contract.md) — the versioned contract for the local recipe-draft slice described below.
- [Local draft workflow](docs/local-workflow.md) — host setup, privacy boundary, and the verification record for Codex CLI and Claude Code.
- [Authentication runbook](docs/runbooks/authentication.md) — environment-specific allowlisting, provider wiring, and verification gates.

## Local draft slice

The first feature work is a local staging workflow: an agent host (Codex CLI or Claude Code) creates, revises, and validates structured recipe drafts through a small stdio MCP server, and a minimal CLI inspects them. Drafts are stored as private JSON files on the local machine. This is a staging step before the hosted API and database are connected; the long-term hosted Postgres direction is unchanged. Every saved object stays a draft, and validation is structural only.

The slice does not include weekly planning, serving scaling, shopping or prep lists, a cooking UI, delete or publish operations, hosted save, source-note import or automatic fetching of source links, or any LLM call by the application. Progress is tracked in the GitHub issues linked from epic #19; the [local draft contract](docs/local-draft-contract.md) is the source of truth.

Setup for Codex CLI and Claude Code, the privacy boundary, and the verification record are in [Local draft workflow](docs/local-workflow.md). Verification status: Claude Code passed; Codex CLI is only partially verified (the destructive `draft_update` call needs approval that the headless run did not grant), so dual-host verification is still open. The server runs locally, but anything used in an agent conversation may be sent by the host to its model provider; the workflow is not offline.

## Current boundaries

The current UI supports browser-local recipe editing and meal planning, with shopping, prep and cooking views. Browser clearing or device loss can lose that data; it does not sync between browsers. Failed saves show an unsaved warning and a retry action. Legacy browser data without an account owner is preserved separately and is not automatically imported into an authenticated account. In particular, the repository does not yet include:

- persisted recipe, planning, or feedback operations in the hosted API;
- database tables, SQL, migrations, or seed data;
- an application OpenAPI contract or generated clients (only the health-check contract exists);
- draft create, update, or delete commands in the CLI, or any hosted draft save; or
- importing or modifying source recipes.

Provider project creation is an organizational setup step. Hosted deployment of the skeleton is governed by the [CI/CD plan](docs/ci-cd-plan.md); no product data is stored there.

## Public repository policy

Only generic product and engineering plans belong in this public repository. Do not commit real recipes, household details, account identifiers, provider dashboard links, credentials, tokens, connection strings, or private usage data. Local provider references belong in the ignored `.setup/` directory.

## Status

Planning baseline: **ready for review**. Any item labeled **Proposed** or **Unresolved** requires a decision or validation before it can become an implementation requirement.
