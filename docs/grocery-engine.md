# Ingredient normalization and grocery engine

Status:

- **Implemented, pure logic with tests:**
  - Exact quantities and units: `api/internal/ingredients` (`Quantity`, `Unit`, `Convert`, `Amount`).
  - The aggregation engine: `api/internal/grocery` (`Aggregate`).
  - Ingredient categories: `api/internal/ingredients` (`Categorize`), stored in
    the global `ingredients` catalog by `api/internal/recipes` (Phase 4; see
    [database.md](database.md#recipes-and-ingredients)).
  - A week's grocery list: `api/internal/planning` builds one
    `RecipeSelection` per plan entry from the live recipe's authored amounts
    for the entry's serving size (never scaled), and
    `GET /api/v1/households/{householdId}/plans/{week}/grocery` returns the
    result grouped by category (Phase 6; see [api.md](api.md#grocery-list)).
    It is computed on every request.
  - The household pantry: `api/internal/pantry` (Phase 7; see [Pantry](#pantry)
    and [api.md](api.md#pantry)). `planning.Service.GroceryList` passes it to
    `Aggregate`, so items get `inPantry`, and the list reports
    `pantryApplied: true`.
  - Specialty ingredients (meal-kit blends, sauces, concentrates):
    `grocery.ApplySpecialties` replaces or keeps those lines as the household
    chose before `Aggregate` (see [Specialty ingredients](#specialty-ingredients)).
- **Pending:**
  - Saved grocery lists with checked state, and the shopping UI (Phase 7).
  - Comparing pantry amounts with what the recipes need.

The implemented engine differs from the pipeline below in two ways:

- The display unit for a combined amount is picked only from the units that
  contributed: the largest unit in which the total is at least 1, otherwise the
  smallest contributing unit. The choice never depends on input order.
- Each item has a status: `inPantry` (in stock in the household pantry),
  `fromFreezer` (in stock in the household freezer), `pantryHint` (every source
  flagged it as a staple, and the pantry doesn't record it as out), or `toBuy`.
  See [Pantry](#pantry).

This is the heart of DinnerOS's usefulness. Ingredient **strings are not the
grocery system**. Every recipe line keeps its raw text *and* a normalized,
structured form.

## Model

```text
RecipeIngredient
  rawText        "½ oz Parmesan Cheese"   ← always preserved, never rewritten
  ingredientId   → Ingredient "parmesan cheese" (nil if unresolved)
  quantity       1/2                       ← exact rational
  unit           ounce
  preparation    "grated"                  (optional)
  optional       false
  pantryStaple   false                     (source hint; household pantry decides)
  confidence     high | low                (low → review queue)

Ingredient
  name, normalizedName, aliases[], category, defaultPurchaseUnit, searchTerms[]
  conversions[]  optional ingredient-specific facts (e.g. 1 medium onion ≈ 8 oz)
```

## Quantities

- Quantities are **exact rationals** (numerator/denominator), not floats. Recipe
  sources use ½, ⅓, and ¼, and float arithmetic would make aggregation
  non-deterministic (`1/3 + 1/3 + 1/3` must equal `1`).
- Rounding happens only at display time, and never before aggregation.
- Ranges ("1–2 cloves") keep both bounds. The list uses the upper bound and shows
  the range.
- Unquantified lines ("salt to taste") have no quantity and are never invented one.

## Units

Units are a domain type with a **dimension**:

| Dimension | Units | Base |
| --- | --- | --- |
| count | count (each, clove, can, package, …) | count |
| volume | tsp, tbsp, cup, fl oz, ml, l | ml |
| mass | oz, lb, g, kg | g |

- Conversion is allowed **within a dimension** using exact factors (1 tbsp = 3 tsp;
  1 lb = 16 oz; 1 oz = 28.349523125 g).
- Conversion **across dimensions** (count ↔ mass, volume ↔ mass) is allowed only
  when the ingredient has an explicit conversion fact. Otherwise the quantities
  stay as separate lines under the same ingredient: `1 onion` + `8 oz onion` is
  **not** `2 onions`.
- Packaging counts ("1 can", "1 packet") are distinct count units and only combine
  with the same unit.

## Aggregation pipeline

```text
selected MealPlanEntries
  │
  ├─ 1. Load recipes and their normalized ingredient lines
  ├─ 2. Scale each line: quantity × (entry.servings ÷ recipe.servings)
  ├─ 3. Group by ingredientId (unresolved lines group by normalized raw text)
  ├─ 4. Within a group, convert to a common unit where valid, then sum
  ├─ 5. Mark pantry staples present in the household pantry (excluded or shown as "have it")
  ├─ 6. Choose a display unit (ingredient defaultPurchaseUnit when convertible)
  ├─ 7. Categorize (produce, dairy, meat, pantry, …)
  └─ 8. GroceryList with items that trace back to their source recipes
```

Example: Recipe A needs `½ onion` and Recipe B needs `½ onion`, so the list shows
`1 onion (A, B)`.

Regenerating the list after changing a recipe or its servings produces the new
list. Manual checked and unchecked state is kept for items that still exist.

## Pantry

A household keeps one pantry item per ingredient (`pantry_items`, unique by
household and normalized key; see [database.md](database.md#pantry)). An item
has a status (`in_stock`, `low`, or `out`), an optional exact amount, and an
`isStaple` flag for always-have items such as salt and oil.

`pantry.Service.GroceryPantry` turns the pantry into the `grocery.PantryStock`
that `Aggregate` takes:

| Pantry item | In `PantryStock` | Grocery status of its lines |
| --- | --- | --- |
| `in_stock`, kept in the pantry | `InStock` | `inPantry` |
| `in_stock`, kept in the freezer (`storage: freezer`) | `InFreezer` | `fromFreezer` |
| `low` | `OutOfStock` | `toBuy`, even if every recipe flags it as a staple: running low means buy more |
| `out` | `OutOfStock` | `toBuy`, even if every recipe flags it as a staple |
| not in the pantry | neither | `pantryHint` if every recipe flags it as a staple, otherwise `toBuy` |

- `Aggregate` reads out-of-stock keys through the optional `grocery.OutPantry`
  interface, and frozen keys through `grocery.FrozenPantry`, so a plain
  `PantrySet` still works.
- **`fromFreezer` stays on the list.** An `inPantry` line disappears, which is
  right for a tub of sour cream and wrong for four pounds of pork that somebody
  has to take out in time. A `fromFreezer` line is shown, de-emphasized ("Grab
  from the freezer"), and never bought again by an export. It beats the staple
  hint and `toBuy`, but loses to an `inPantry` item (nearer to hand) and to a
  skip. See [pantry-usage.md](pantry-usage.md#the-freezer).
- `isStaple` doesn't change the status. It marks always-have items for the UI
  and for the default staples (`POST .../pantry/staples/defaults`).
- **Keys.** An item is registered under its catalog ingredient ID and under
  `name:<normalized name>`, the two ways the planner keys recipe lines (with
  and without a catalog ID). A free-text item is linked to the catalog when
  it's added, if its name is a catalog ingredient, and resolved against the
  catalog again each time a list is built, so an ingredient the catalog learns
  later still matches. A default staple also answers for its aliases, each
  resolved the same way: the "Cooking Oil" staple matches a recipe's
  "Vegetable Oil", "Black Pepper" matches "Pepper" (`pantry/staples.go`). Other
  items match only their own name; guessing equivalences beyond that list
  would hide things a household needs to buy.
- **A member's status beats the recipe's staple hint.** Only `in_stock` keeps
  a line off the list; `low` and `out` always put it on, and `pantryHint`
  ("Usually on hand" on the Shop tab) is only for staples the pantry doesn't
  track at all.
- **Amounts are informational.** The engine doesn't yet compare the pantry's
  amount with what recipes need: 1 tbsp of olive oil in stock makes a recipe's
  ½ cup `inPantry`. Mark the item `low` or `out` to put it on the list. Usage
  tracking ([pantry-usage.md](pantry-usage.md)) marks items `low` on its own
  when the estimate crosses the household's threshold, which puts them back
  on the list.

## Skipped ingredients

A household can leave an ingredient off its list on purpose — the one it buys,
throws away, and resents buying again. `skips.Service.GrocerySkips` turns the
household's skips into the `grocery.SkipSet` that `AggregateWith` takes
([api.md](api.md#skipped-ingredients),
[database.md](database.md#skipped-ingredients)).

Two lifetimes, and nothing else:

| Scope | Applies to | Ends |
| --- | --- | --- |
| `week` | the one ISO week it names | on its own, next week |
| `always` | every week | when someone resumes it |

- **Three different states.** `inPantry` says the household *has* it, check-off
  says someone *bought* it, and a skip says the household never *wants* it.
  They answer different questions and are never conflated: a skip outranks the
  pantry, because whatever is at home, the household asked for this ingredient
  to stay off the list.
- **Skipped items are held back, not dropped.** `AggregateWith` aggregates them
  exactly like every other line — amounts, sources, `Via` — then puts them in
  `List.SkippedItems` with `StatusSkipped` instead of `List.Items`. The recipe
  still needs the ingredient, and the list says so rather than quietly
  disagreeing with the recipe screen. Anything built from the list's categories
  (the shopping handoff included) therefore leaves them out with no change of
  its own.
- **Order matters.** Skips resolve *after* `ApplySpecialties`, against the keys
  the list actually ends up with. Skipping a component a store alternative
  introduced drops that component and leaves the rest of the alternative alone,
  still marked with the `via` that explains it.
- **Keys** work as the pantry's do: the key the skip was made from,
  `name:<normalized name>`, and the catalog ingredient ID resolved from that
  name, so skipping cilantro once covers every way a recipe reaches cilantro.
- **Not an Autopilot signal.** A forever-skip is weak evidence against recipes
  built around that ingredient, but nothing reads it as one. Autopilot already
  has explicit restrictions and ratings, and making a skip quietly rank meals
  down would remove recipes the household never rejected, with no screen saying
  why. If it is ever wanted, it should be surfaced, not hidden.

## Meal customizations

`customize.ApplyGrocery(selection, picks)` runs before `ApplySpecialties` when
the planner has a customization source ([api.md](api.md#customize-a-meal)).
It's pure: the entry's stored choices come in already resolved against the
curated protein table.

- **`double`:** the line keeps its ingredient with twice the amount.
- **`swap` and `swap_double`:** the line becomes the chosen protein's catalog
  ingredient (its ID, name, and category, or `name:<key>` when the catalog
  doesn't have it), with the amount times the choice's factor — the same
  weight by default, or the table's ratio for that pair. A swapped line is no
  longer a pantry staple.
- Changed lines carry a `Line.Via` of kind `customized` naming the original
  line and the choice, so the item reads "Ground Beef instead of Ground Pork
  in One-Pan Pork Tacos", or "2x Ground Pork in …" for a doubled line.
- A line without an amount stays without one, and lines nothing customizes are
  untouched — including specialty ingredients, which `ApplySpecialties`
  handles next. `Aggregate` then merges the swapped-in ingredient with the
  same ingredient from other recipes and scales by servings as usual, so the
  two transforms compose.

## Specialty ingredients

`ApplySpecialties(selections, specialties)` runs before `Aggregate` when the
planner has a specialty source ([specialty-ingredients.md](specialty-ingredients.md)).
It's pure: the household's choices, option ingredients (already linked to
catalog keys), and batch stock come in as `grocery.Specialties`, keyed by line
key.

- **No choice or `as_is`:** the line is kept and marked (`Line.Specialty`).
- **Store alternative:** the line's amount is converted exactly to the
  option's `per` unit (`ConvertMeasure`, through the specialty's packet sizes
  for discrete units), and each option ingredient becomes a line scaled by
  that ratio, with `Line.Via`. `Aggregate` then scales by servings as usual,
  so a store alternative scales exactly like the recipe line it replaces.
- **House-made batch:** the week's lines are totaled in the yield's unit. If
  the batch item is in stock and covers the total, the lines are kept under
  the batch's pantry key (`name:<key>`) and marked house-made, so the pantry
  makes them `inPantry`. Otherwise they're removed and the option's
  ingredients for `ceil(shortfall ÷ yield)` batches (at least 1) are added in
  a synthetic selection whose lines carry `Line.Sources` (the recipes that
  need the batch) instead of a recipe of their own. A `BatchPlan` reports
  either outcome.

`Aggregate` merges `Via` per item by kind, specialty, and option, with the
recipes each is for, and keeps the `Specialty` marking. Lines without these
fields aggregate exactly as before, and the output still doesn't depend on
input order.

## Weekly cost

`shopping.Service.WeekCost` (`internal/shopping/cost.go`) answers "what did
this week of groceries really cost?" A $130 Walmart cart and a $130 meal kit
aren't the same spend when the cart's sour cream, soy sauce, and spices last
for weeks. Everything is computed on read, in integer US cents, from what
members entered; nothing derived is stored.

| Figure | Rule |
| --- | --- |
| **Spent** | The week's order total (`shopping_week_spend`, fees, tax, and tip included) when entered, else the sum of known line prices |
| **Used this week** | Σ per priced item (below) + **earlier stock**: this week's cooked planned meals × the pantry deductions from purchases made for other weeks, valued at each purchase's price per unit + **fees and unpriced items**: order total − priced items, when a total was entered |
| **Stocked for later** | Σ what's left of this week's priced items |
| **Cost per meal** | Used ÷ the week's planned meals (add-ons don't count) |
| **vs meal kit** | Meal kit price per meal (`mealKit.weeklyCents ÷ meals`, rounded) × meals − used |
| **Savings** | The sum of vs meal kit over recent weeks with a confirmed order or an order total |

Per confirmed handoff line with a price (the line's, or its pantry purchase's):

1. **Not tracked** (fresh for the week, [pantry-usage.md](pantry-usage.md#what-goes-in-the-pantry)):
   the whole price is used (`usage: whole_package`).
2. **Tracked and measurable**: used = price × the week's need ÷ what was
   bought, the need measured against the package exactly as package counting
   does (densities included), capped at 1 (`measured`). Then, while the
   pantry's current cycle is still this purchase, stocked is capped at price ×
   the estimate's remaining ÷ bought, and the difference moves to used: when
   more was cooked or used than planned, the pantry knows.
3. **Tracked, not measurable** (no package size, a need that doesn't convert):
   the whole price counts as used (`unknown`), so savings are never
   overstated.

Worked example: sour cream, $2.48 for 16 oz, 2 tbsp for the week: 2 tbsp ≈
1.1 oz at 1.05 g/ml, 6.8% of the tub, so **17¢ used and $2.31 stocked**. Two
16 oz packs of ground beef at $11.98 for 20 oz are not tracked, so $11.98 used.

**Honesty.** Unpriced items are left out of used and stocked, never guessed;
`itemsPriced` of `itemsBought` and `partial` say how much the figures rest on,
and `summary` says it in a sentence ("Based on 18 of 24 items with prices.").
An order total covers the unpriced items, which then count as used. Rounding
is to the nearest cent per item.

## Rules

- Pure, deterministic Go. No LLM, no network, no clock inside the engine.
- The output does not depend on input order (items are sorted deterministically).
- Anything uncertain is flagged for review, never guessed.

## Testing

- Table tests for parsing: `"1 ½ cups"`, `"½ oz"`, `"2 (15 oz) cans"`, `"1-2 cloves"`,
  unicode fractions, and unquantified lines.
- Table tests for every unit conversion factor and every forbidden conversion.
- Golden-file fixtures: a set of synthetic recipes (not imported personal data)
  → the expected grocery list.
- Property tests: input order doesn't matter; scaling by *k* and then aggregating
  equals aggregating and then scaling by *k*; converting to a base unit and back
  is the identity.
