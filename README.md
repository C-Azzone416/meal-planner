# Meal Planner

Meal Planner is a planned web application for reducing the effort required to cook varied dinners at home. This repository currently contains planning and setup documentation only.

> **Documentation-only status:** There is no application code, database schema, migration, executable API contract, CLI, MCP server, CI workflow, or deployment in this repository.

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

## Current boundaries

This planning milestone intentionally excludes implementation. In particular, it does not include:

- frontend or backend code;
- database tables, SQL, migrations, or seed data;
- OpenAPI files, generated clients, or other executable contracts;
- local-core, CLI, or MCP implementation;
- GitHub Actions workflows;
- a placeholder Vercel deployment; or
- importing or modifying source recipes.

Provider project creation is an organizational setup step only. No live application is deployed by this milestone.

## Public repository policy

Only generic product and engineering plans belong in this public repository. Do not commit real recipes, household details, account identifiers, provider dashboard links, credentials, tokens, connection strings, or private usage data. Local provider references belong in the ignored `.setup/` directory.

## Status

Planning baseline: **ready for review**. Any item labeled **Proposed** or **Unresolved** requires a decision or validation before it can become an implementation requirement.
