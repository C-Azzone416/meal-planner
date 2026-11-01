# Product Plan

Status: planning baseline, 2026-11-01

This document distinguishes settled product direction from proposed launch scope and unresolved product rules. It is not an implementation specification.

## Goal

Reduce the planning, preparation, cleanup, and attention required to cook varied dinners at home while preserving flexibility during the week.

## Settled decisions

### Weekly planning

- Start with three dinners per weekly plan.
- Use saved recipes as the plan's source.
- Generate a plan only when the user requests one; there is no autonomous weekly generation.
- Provide a non-AI random selection option from saved recipes.
- Default to four to six servings, with an adjustable serving count.
- Plan one shared dinner rather than separate meals for different people.
- Allow meal nights to move or swap during the week.
- Do not make repeated batch leftovers the primary planning strategy.

### Effort model

- Assume one person handles preparation and one person handles cooking.
- Do not assume both people can work in parallel.
- Target roughly 30–45 minutes of weekend preparation.
- Aim for roughly 10–15 minutes of weeknight active work where practical.
- Treat labor, cleanup, attention, and timing variability as important constraints, not just total elapsed time.
- Track active time and total ready time separately.
- Include both low-attention, "dump-and-go" meals and meals with more active or variable timing.
- Weeknight active time includes any preparation performed that evening.

### Recipe structure

Every reviewed recipe breakdown has three sections:

1. **Prep ahead** — tasks that can safely be completed before the meal day.
2. **Prep tonight** — active preparation required on the meal day.
3. **Cook tonight** — cooking and final assembly steps.

An agent may flag prep-ahead candidates when a recipe draft is created, before user review. The system saves the reviewed breakdown, not an unreviewed claim about safety or timing.

### Data and source material

- Structured recipes, weekly plans, and feedback will be stored in Postgres.
- Existing source notes remain unchanged and undeleted.
- Source notes are a temporary source until selected recipes are copied into the application.
- There is no two-way synchronization with source notes.
- No recipe sampling or import is part of this planning milestone.
- Public source code must never imply that user recipes or usage data are public.

## Proposed launch scope

The following capabilities fit the product direction but remain proposed until launch scope is approved:

- review and approve structured recipe drafts;
- generate a three-dinner plan from saved recipes;
- manually swap meal nights;
- view a consolidated shopping list;
- view a consolidated preparation list while preserving which meal owns each task;
- use a responsive phone and tablet cooking view; and
- record lightweight post-meal feedback.

Detailed editing behavior, progress tracking, and the exact feedback experience are unresolved and must not be assumed by implementation.

## Unresolved product rules

These questions must be answered explicitly before the relevant implementation begins.

### Recipe eligibility and variety

- What food exclusions, nutritional constraints, or household preferences determine eligibility?
- What counts as variety across cuisine, protein, cooking method, texture, or recent history?
- What happens when three eligible meals exceed the weekend preparation budget?
- When should the planner favor effort predictability over variety?

### Serving and ingredient math

- Which ingredients scale linearly and which require recipe-specific rules?
- How are units normalized and displayed?
- How are equivalent ingredients consolidated without losing preparation notes?
- How are shared preparation tasks grouped while preserving meal ownership?

### Changes after planning

- How do meal swaps affect shopping, preparation tasks, and completion state?
- What happens when a recipe or serving count changes after shopping begins?
- Which changes require recalculation, warning, or user confirmation?

### Review and editing

- Which recipe-draft fields require explicit approval?
- Can users edit structured recipes in the launch web experience?
- What feedback fields are useful enough for launch?
- What defines success: cooked as planned, liked, timing accuracy, reduced effort, or a combination?

### Food safety and timing

- Which sources and review process support storage-duration and prep-ahead advice?
- How will the interface distinguish sourced guidance from unverified estimates?
- How will the planner avoid assuming that weekend preparation remains safe for every later meal day?

### Experience design

- What are the launch screens and navigation model?
- What information density works for phone and tablet cooking views?
- How should attention, cleanup, and timing variability appear without making planning cumbersome?

### Import strategy

- Will source-note import be gradual or bulk?
- What source-note structure and access are actually available?
- How will imported content be restricted to explicitly selected notes when delegated access is broader than one page?

## Deferred ideas

These ideas are outside the committed first version:

- Local store sale-item recommendations based on dated, location-specific, package-level offer data. This should not claim to find the cheapest possible meal.
- Dining-out and grocery-cost logging.
- Weekly cost comparisons and budgets.
- Photo or file storage. Neon Object Storage may be evaluated later; availability, access controls, and product fit must be verified before adoption.
- A desktop or other local graphical interface.
- An independent agent platform. Existing agent hosts and command-line tools should invoke narrow application interfaces instead.

## Product acceptance gate

Before implementation starts, the team should approve:

- the launch capability list;
- the unresolved rules needed by the first vertical slice;
- the recipe review and safety-source policy;
- a minimal success measure; and
- the initial responsive experience map.
