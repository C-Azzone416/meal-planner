# Meal Planner

Meal Planner is a planned web application for reducing the effort required to cook varied dinners at home. The repository now includes a navigable, responsive UI preview and a fail-closed authentication boundary. The UI starts with clearly labeled sample data. Manual recipe storage now has an authenticated Neon-backed API and app integration, pending the reviewed migration and runtime setup. Week planning and preview data remain browser-local, separately for each authenticated account.

> **Preview status.** The November web release includes CI workflows, hosted health and authentication functions, a React UI preview and local command scaffolds. This source tree also contains the local draft restore candidate. The UI uses invitation-only email-and-password authentication when its provider is configured. Existing invited accounts set or recover a password with a short-lived email code. Hosted manual recipes have a reviewed migration path. The app fails closed for hosted saves until the database and runtime configuration are prepared. Week planning uses browser-local storage.

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
- [Local draft contract](docs/local-draft-contract.md) — specification for the shared core and local draft tools.
- [Local draft workflow](docs/local-workflow.md) — candidate builds, synthetic workflow and historical host evidence.
- [Hosted manual recipes](docs/runbooks/recipes.md) — API, migration, least-privilege runtime configuration and Preview verification gates.
- [Authentication runbook](docs/runbooks/authentication.md) — environment-specific allowlisting, provider wiring, and verification gates.

## Local draft restore candidate

The #23 candidate restores the local CLI, file store and stdio MCP tools from the retained #19 source. It rejects ambiguous tool parameters and reused request IDs before SDK dispatch. PR #56's November web release shipped only the version and stdin-draining scaffolds.

Independent review and renewed #25 acceptance in Codex CLI and Claude Code are pending. Caroline's hands-on Preview gate applies before Production shipment. A release requires approval from Caroline or Bagel and starts from reviewed `develop`, followed by a reviewed, CI-passing PR to `main`. The hosted draft bridge remains paused under #55. [Local draft workflow](docs/local-workflow.md)

## Current boundaries

The current UI supports hosted manual recipe add/edit after database setup, plus separately labeled browser-local recipes and meal planning with shopping, prep and cooking views. Browser clearing or device loss can lose that data; it does not sync between browsers. Failed saves show an unsaved warning and a retry action. Legacy browser data without an account owner is preserved separately and is not automatically imported into an authenticated account. In particular, the repository does not yet include:

- hosted planning, feedback or MCP draft operations;
- automatic migration execution or real-user seed data;
- generated application clients;
- importing or modifying source recipes.

Provider project creation is an organizational setup step. Hosted deployment of the skeleton is governed by the [CI/CD plan](docs/ci-cd-plan.md); manual recipe persistence is gated by the separate database runbook.

## Public repository policy

Only generic product and engineering plans belong in this public repository. Do not commit real recipes, household details, account identifiers, provider dashboard links, credentials, tokens, connection strings, or private usage data. Local provider references belong in the ignored `.setup/` directory.

## Status

Planning baseline: **ready for review**. Any item labeled **Proposed** or **Unresolved** requires a decision or validation before it can become an implementation requirement.
