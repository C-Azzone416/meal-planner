# Planner store

The client-side data layer for the preview web app. Pages read and write **only** through `usePlanner()`; never import `data/sampleData.ts`. When a real API arrives, this module is the seam that gets replaced. Product rules live in [`docs/ux-decisions.md`](../../../docs/ux-decisions.md) (§0–§3).

```tsx
const { plan, recipes, shopping, prep, summary, fillWeek, undoInfo, undo } = usePlanner()
```

`PlannerProvider` sits inside `AuthProvider` and around the routes (already done in `App.tsx`). In tests wrap with `<PlannerProvider storage={null} random={mulberry32(1)}>`, or pass an in-memory `StorageLike`.

## Files

| File | What |
| --- | --- |
| `types.ts` | Domain types (`Recipe`, `Draft`, `Night`, `WeekPlan`, progress maps, `HouseRules`…), `DAY_NAMES`, `AISLES`. Days are `0 = Sunday … 6 = Saturday`. |
| `seed.ts` | `createSeedState()`: 8 recipes, 2 drafts, Mon/Wed/Thu plan, some prep done, olive oil running low. |
| `store.tsx` | `PlannerProvider`, `usePlanner()`, and the `PlannerActions` / `PlannerApi` interfaces (the JSDoc there is the action reference). |
| `reducer.ts` | Pure reducer. Random choices arrive as seeds (drawn once per action), so it is safe under StrictMode. |
| `plan.ts` | Pure plan operations (fill, surprise, move, skip, pin, servings, undo) + `mulberry32` seedable RNG. |
| `derive.ts` | Pure derivations: `deriveShopping`, `derivePrep`, `derivePantry`, `deriveNightWarnings`, `planSummary`. |
| `persist.ts` | localStorage key `mealplanner.preview.v1` (`{version, state}`), validation, reseed on corrupt/old data. |
| `keys.ts`, `format.ts` | Stable keys (`shoppingKey`, `prepKey`), ids, quantity/duration formatting. `uniqueId` never returns a reserved recipe id (`RESERVED_RECIPE_IDS` = `new`, `edit`, `by-id`: they are words in the `/recipes/...` URLs), so a title "New" becomes `new-2`. A recipe that already has a reserved id opens at `/recipes/by-id/<id>` (see `pages/recipePath.ts`). |
| `testing.ts` | Test helpers (`emptyWeek`, `cleanSeed`, `run`, `sequence`). |

## What `usePlanner()` returns

State: `recipes`, `drafts`, `plan` (`nights[7]` + `revision`), `houseRules`, `progress`, `proposal`, `undoInfo` (`{message, seq}` | null), `status` (`{seq, kind, text}` | null), `resetNotice`, `getRecipe(id)`, `getDraft(id)`, and raw `state`.

Derived (memoised, always consistent with the plan): `shopping`, `prep`, `pantry`, `summary`, `nightWarnings`.

### Actions

- **Recipes** — `addRecipe(newRecipe) → id`, `updateRecipe(recipe)`. Editing a planned recipe sets `status` to "This week’s shopping and prep were updated." Generate ids for new ingredients/steps with `newItemId()` from `keys.ts`.
- **Drafts** — `approveDraft(id) → recipeId | null` (null until all five guesses are confirmed), `discardDraft`, `setDraftConfirmed(id, key, bool)`, `updateDraft(id, patch)`. Drafts are a separate type and never appear in `recipes`.
- **Plan** — `fillWeek(rng?)` (fill only, up to `houseRules.targetDinners`), `proposeSurprise(day, rng?)` → `proposal`; then `keepProposal()` / `cancelProposal()` ("Try another" = call `proposeSurprise` again; it will not repeat). `moveNight(from, to)` (swaps if the target is planned), `setSkipped`, `setOpen`, `togglePin`, `setServings(day, n)` (1–12), `assignRecipe(day, recipeId, servings?)` ("Add to week"), `setHouseRules(patch)`.
- **Progress** — `toggleShoppingChecked(row.key)`, `togglePrepDone(task.key)`, `confirmPrepTopUp(task.key)`, `togglePantryLow(item.key)`, `dismissNoLongerNeeded('shopping' | 'prep', key)`.
- **Undo / status** — `undo()`, `dismissUndo()`, `clearStatus()`, `dismissResetNotice()`, `resetPreviewData()` (**confirm first**).

Refused actions (Surprise me on a pinned/skipped night, moving onto a skipped night, filling a full week…) leave the plan untouched and set `status` with the reason; render `status` in a `role="status"` region. They do not consume the undo slot.

### Undo

Every plan-changing action (fill, keep, assign, move, skip, open, pin/unpin, servings) stores one snapshot of **plan + progress** and sets `undoInfo`, e.g. "Swapped Thursday to Red lentil coconut soup". `undo()` restores both and clears it. Repeated servings changes on the same night collapse into one undo step. The 10-second window is UI-side: key a timer on `undoInfo.seq` and call `dismissUndo()` when it fires.

### Rules worth knowing
- **A recipe can be on only one night per week.** `assignRecipe` / `keepProposal` refuse a duplicate with the status "`<title>` is already on Monday"; Plan my week and Surprise me never pick a recipe already in the week; stored data that violates it is discarded.
- **`fillOrder(plan, prepDay)`** (plan.ts) is the order open nights get dinners: Mon/Wed/Thu first, then earliest open nights, prep day last. Use it instead of re-deriving the order.
- **Hooks:** `usePlanner()` (data + actions), `usePlannerData()`, and `usePlannerActions()` (stable object; never re-renders on data changes). Exposed data is typed read-only; change it only through actions. The store copies recipes/drafts it is given, so mutating your object afterwards has no effect.
- **Keys:** `shoppingKey`, `prepKey` and `pantryLowKey` encode each part, so no name or id can collide by containing `:` or `|`. Running-low pantry rows use `pantryLowKey` (never merged with a regular row of the same word).

## Derivation rules (§3)

Shopping and prep are never stored; only progress is (`progress.shoppingChecked`, `progress.prepDone`), keyed by stable keys.

- **`shopping.rows`** — one row per normalised name + unit (`"onions"` ≡ `"Onion"`), quantity scaled by `night.servings / recipe.defaultServings` and **rounded up once on the total**: ¼ steps for measured units (cup, lb, oz, tbsp, tsp…), ½ for bunches, whole numbers otherwise. `meals` / `mealsLabel` ("chicken + chili") list which meals need it. Pantry ingredients (`isPantry`) are excluded unless marked running low, then they appear under **Pantry aisle** with `source: 'pantry-low'`.
- Row `status`: `todo`, `checked`, or `needs-more` (checked earlier, requirement grew; `checked` is false and `note` reads "need 1 more lb (you checked 2 lb)"). A decrease keeps it checked. Ticking a `needs-more` row confirms the new total.
- **`shopping.groups`** (by aisle, `AISLES` order), **`shopping.byMeal`**, **`shopping.noLongerNeeded`** (checked rows nobody needs now; still struck-through until `dismissNoLongerNeeded`). Un-checked vanished rows leave no trace; undo or re-adding the meal re-checks them.
- **`pantry`** — the shelf: every staple in any saved recipe, with `usedBy` (this week) and `usedByRecipes`, `low`.
- **`prep.tasks`** — each planned meal’s prep-ahead steps in day order. `status`: `todo`, `done`, or `topup` (done, but servings went up since — still counted as done; `confirmPrepTopUp` records the new amount; toggling it clears it like any done task). `warning` is set when `(mealDay − prepDay) > keepsDays`: "Marinade keeps 1 day — prep it Wednesday instead". `nightWarnings[day]` repeats those per night.
- **`prep.doneNoLongerPlanned`** — done tasks whose meal was removed (kept, dismissable). `prep.totals` has task/done/minute counts.

## Persistence

`localStorage['mealplanner.preview.v1'] = { version: 1, state: { recipes, drafts, plan, progress, pantryLow, houseRules } }`. Proposal, undo and status are transient. Unparseable, wrong-version or structurally invalid data is discarded: the seed is loaded, `resetNotice` is set ("We reset the preview data because it could not be read."), and the next write overwrites the bad value. There is no cross-tab sync.

## Decisions worth knowing

- Plan my week orders extra nights: Mon/Wed/Thu if open, then the earliest open nights with the prep day (Sunday) last.
- Pins travel with a meal when it is moved or swapped; skipped nights can’t be moved onto.
- `sampleData.ts` is untouched and still feeds the pages that have not migrated yet; it will be deleted once they have.
