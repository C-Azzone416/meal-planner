# UX decisions for the launch web experience

Status: decided by the PM orchestrator on 2026-11-07 under the owner's delegation. Covers #36, #37, #38 and #39.
The owner may revise any of this; each section is small enough to change on its own.

## 0. Data boundary for this phase

- There is no recipe, plan, shopping or prep API yet (`contracts/openapi.yaml` only has `/api/health`). The web app therefore runs on a **client-side planner store** seeded with the sample data.
- The store persists to `localStorage` under one key (`mealplanner.preview.v1`), scoped to the browser. The stored value carries a `version` field, currently **2** (the key name stays the same; version 2 changed how shopping/prep progress keys are encoded). A corrupt, ill-formed or old-version value, such as a version 1 blob, is discarded and reseeded with a visible notice, never thrown. There is no migration for preview data.
- Every screen keeps an honest notice: "Preview data · saved in this browser only". House rules gets a **Reset preview data** action (confirm first).
- The store is the single seam the future API will replace: pages read and write through hooks/actions, never through `sampleData.ts` directly.
- Nothing personal is sent anywhere. No new network calls.

## 1. Recipes: view, add, edit (#36)

**Routes (direct URLs, back works everywhere):**
- `/recipes` — list (existing search + filters).
- `/recipes/:recipeId` — full detail **page** (not a modal or drawer: it must be linkable, work on phones, and survive reload). Unknown id → in-page "Recipe not found" state with a link back to Recipes.
- `/recipes/new` — add form.
- `/recipes/:recipeId/edit` — edit form.
- Recipe ids are stable slugs/ids in the store; titles can change without breaking URLs.

**Detail page shows:** title, icon, active minutes, total time, attention level, tags, default servings, ingredients (name, quantity, unit, aisle), and the three sections **Prep ahead / Prep tonight / Cook tonight** with step lists. Prep-ahead items show their storage note. If the recipe is on the current week it says which night, with a link to This week. Actions: Edit, Add to week (picks an open night; disabled with a reason when none is open).

**Editable in the web app (saved recipes):** title, icon, times, attention, tags, servings, ingredients, steps in the three sections, storage notes.
**Draft/review boundary:** agent-created content arrives only as a **draft** on `/drafts`. Drafts are visually distinct (dashed paper border, "Draft · not saved" badge, no magnet) and never appear in Recipes or the planner until **Approve** turns them into a saved recipe. The web "Add recipe" form creates a saved recipe directly because a person typed it; it is not a draft. Prep-ahead safety/storage claims on a draft must each be confirmed before Approve is enabled (existing Drafts behaviour).

**Form behaviour:** Save and Cancel. Cancel with unsaved changes asks "Discard changes?" (Keep editing / Discard). Leaving via back/nav with unsaved changes asks the same. Validation is inline and on submit: title required (1–120 chars), active minutes 0–600, servings 1–12, at least one ingredient and one Cook-tonight step. Errors are announced (`role="alert"`) and focus moves to the first invalid field. Save → detail page with a "Saved" status.

**Editing a recipe that is on this week:** the edit applies to the plan immediately; shopping and prep recalculate under §3 rules, and the detail page shows a one-line notice "This week's shopping and prep were updated." Deleting a recipe is out of scope for now (no delete button).

## 2. Plan my week / Surprise me: fill vs replace (#37)

- A week has 7 nights. Each night is one of: **planned** (recipe + servings), **open**, or **skipped** ("eating out / no cooking"). The plan targets **3 dinners** (house rule, editable later).
- **Pin:** any planned night can be pinned (📌 toggle). Pinned nights are never changed by Plan my week or Surprise me.
- **Plan my week = fill only.** It fills *open* nights (never skipped, never planned) until the week has 3 planned dinners, choosing randomly from saved recipes not already in the week. Non-AI, uniform random; tests inject the random source.
  - Empty week → plans 3 nights (default nights Mon, Wed, Thu if open; otherwise earliest open nights).
  - Partial week (e.g. 1 planned) → adds 2 on open nights; leaves the planned one alone.
  - Full week (3 planned) → no change; status "Your week already has 3 dinners. Use Surprise me on a night to swap one."
  - Not enough saved recipes → fills what it can and says so.
- **Surprise me = replace one selected night.** It needs a selected night. On a planned, unpinned night it proposes one random alternative in place ("Try: Red lentil coconut soup — Keep it / Try another / Cancel"); nothing changes until **Keep it**. On an open night it proposes a dinner for that night (same buttons). On a pinned or skipped night the button is disabled with the reason. With no night selected, the button is disabled with "Pick a night first".
- **Replacing is always explicit** (Keep it). After any change to the plan (fill, keep, move, skip, unpin, servings) an **Undo** status appears for 10 seconds / until the next change: "Swapped Thursday to Red lentil coconut soup. Undo". One level of undo is enough for launch.
- **Move/swap nights:** a planned night can be moved to another night; if the target is planned, the two swap. Pinned nights can be moved by the user (pins only block automation).
- **Pins travel with the meal:** moving or swapping a pinned dinner carries its pin to the new night. A planned night cannot be moved onto a **skipped** night; the refusal says why ("Saturday is skipped — make it open first.").
- **Extra nights for Plan my week:** after Mon/Wed/Thu, remaining open nights are used earliest-first, with the prep day (Sunday by default) last.
- **Servings** per planned night, 1–12, default from the recipe.
- Approved destructive copy: Surprise me never overwrites without "Keep it". There is no "replace whole week" action at launch.

## 3. Change propagation for shopping and prep (#38)

**Rule: shopping and prep lists are always derived from the current plan + recipes.** Nothing is stored except per-item progress (checked / done), keyed by a stable item key.

- **Keys:** shopping item key = normalised ingredient name + unit. Prep task key = recipe id + prep-ahead step id. Consolidated shopping rows keep the list of meals that need them ("onions · chili + chicken").
- **Recalculate automatically** on: serving changes (linear scaling, rounded to sensible fractions: ¼ steps for cups/lb, whole numbers for counts), moves, swaps, skips, recipe edits, Plan my week, Surprise me.
- **Completed progress is never silently discarded.**
  - A checked shopping item whose required amount **increases** becomes unchecked and shows "need 1 more lb (you checked 2 lb)". If it decreases or stays, it stays checked.
  - A checked item that is **no longer needed** moves to a "No longer needed" section (still checked, struck through) instead of disappearing; it can be dismissed.
  - A done prep task whose meal was removed moves to "Done, no longer planned" with the same treatment. A done prep task whose quantity increased is marked "needs a top-up" but stays done.
- **Pantry shelf:** pantry-marked ingredients (oil, salt, spices, etc. — an `isPantry` flag on the ingredient) never appear on the shopping list; they show on the Pantry shelf view with "used by: …". Toggling "running low" on a pantry item adds it to the shopping list under Pantry aisle.
- **Prep timing warning:** each prep-ahead step has a `keepsDays` value. If a meal is moved so that (meal day − prep day) > keepsDays, the prep task and the night show a warning: "Marinade keeps 1 day — prep it Wednesday instead". Prep day is Sunday by default.
- **History/undo:** the one-level Undo in §2 covers plan changes and restores progress state too. No longer-term history at launch.
- **Consistency rule for the future API:** the server will own derivation; the client sends plan mutations with the plan's revision number and receives the re-derived lists. Progress records are keyed by item key, so a recalculation never loses them. (Documented here so the API contract can adopt it; not implemented now.)

## 4. States: empty, loading, error, auth (#39)

**Shared state pattern:** one `StateCard` component (paper card, icon magnet, heading, one sentence, at most one primary action) used for every non-happy state, plus a `PageSkeleton` for loading. Same pattern on desktop and mobile.

- **Loading:** skeleton cards (respecting reduced motion: no shimmer when `prefers-reduced-motion`), `aria-busy` on the region.
- **Empty:** Week — "No dinners yet" + Plan my week. Recipes — "No recipes yet" + Add recipe. Shopping/Prep — "Nothing to buy/prep — plan a dinner first" + link to This week. Drafts — "No drafts waiting".
- **No results (Recipes):** "No recipes match 'x' with Vegetarian" + **Clear filters** (resets search and chip, returns focus to search).
- **Offline:** a slim banner when `navigator.onLine` is false: "You're offline. Changes are saved in this browser." (Preview data is local so actions still work.)
- **Recoverable error:** an error boundary around each page renders a StateCard "Something went wrong on this page" + Try again (remounts the page) without losing the shell/nav. Store load failure → reseed + status "We reset the preview data because it could not be read."
- **Not available yet:** unfinished actions are rendered disabled with a visible reason (not hidden, not fake success), e.g. "Feedback after cooking — coming soon".
- **Auth:**
  - Wrong email/password: "That email and password did not match." (never says whether the account exists).
  - Reset code request: always "If that address can sign in, we sent a code." — identical response for unknown/unapproved addresses.
  - Expired/incorrect code: "That code is invalid or expired. Request a new code." Resend is disabled for **60 seconds** with a visible countdown.
  - Unapproved signup / blocked identity: same generic messages as above (no enumeration).
  - Session expiry: redirect to sign-in with "Your session ended. Sign in again." and return to the original page after sign-in.
  - Sign out: confirm-free, returns to sign-in with "You're signed out."
- **Accessibility:** visible focus ring (3px, high contrast) on every interactive element; focus moves to the page `h1` on route change; touch targets ≥ 44×44px on mobile; status messages use `role="status"`, errors `role="alert"`; colour is never the only signal; animations off under `prefers-reduced-motion`.
