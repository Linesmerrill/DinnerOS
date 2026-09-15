# Ingredient normalization and grocery engine

Status:

- **Implemented, pure logic with tests:**
  - Exact quantities and units: `api/internal/ingredients` (`Quantity`, `Unit`, `Convert`, `Amount`).
  - The aggregation engine: `api/internal/grocery` (`Aggregate`).
  - Ingredient categories: `api/internal/ingredients` (`Categorize`), stored in
    the global `ingredients` catalog by `api/internal/recipes` (Phase 4; see
    [database.md](database.md#recipes-and-ingredients)).
- **Pending:**
  - Grocery list persistence, API, and UI (Phase 7).

The implemented engine differs from the pipeline below in two ways:

- The display unit for a combined amount is picked only from the units that
  contributed: the largest unit in which the total is at least 1, otherwise the
  smallest contributing unit. The choice never depends on input order.
- Each item has a status: `inPantry` (in the household pantry), `pantryHint`
  (every source flagged it as a staple, but it isn't in the household pantry),
  or `toBuy`.

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
