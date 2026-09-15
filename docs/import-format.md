# Recipe import format (v1)

Source importers convert source data into this source-neutral JSON, and the
DinnerOS API ingests it. The HelloFresh importer is the first producer. Manual,
file, and partner imports will use the same contract.

Imported files contain personal order history. They live under
`importers/**/data/` (git-ignored) and are never committed.

## File

```json
{
  "version": 1,
  "source": "hellofresh",
  "generatedAt": "2026-09-15T05:00:00Z",
  "recipes": [ /* ImportRecipe */ ],
  "review":  [ /* ReviewItem */ ]
}
```

## ImportRecipe

| Field | Type | Notes |
| --- | --- | --- |
| `source` | string | `hellofresh`, `manual`, `import`, `user`, `provider`, `partner` |
| `sourceRecipeId` | string | Canonical ID in the source. With `source`, this is the idempotency key. |
| `sourceAliases` | string[] | Other source IDs for the same recipe, e.g. weekly menu clones that were delivered |
| `sourceUrl` | string | Canonical source page |
| `name`, `headline`, `description` | string | Preserved as given (trimmed) |
| `imageUrl` | string | Hero image |
| `isAddon` | bool | Sides, desserts, and extras rather than a main meal |
| `servings` | int[] | Serving sizes with authored amounts, e.g. `[2, 4]` |
| `prepMinutes`, `totalMinutes` | int | Parsed from source durations. Omitted when absent. The source's values are kept even if they look inconsistent. |
| `difficulty` | int | Source scale |
| `cuisines`, `tags`, `utensils`, `allergens` | string[] | De-duplicated display names |
| `nutritionPerServing` | `{name, amount, unit}[]` | |
| `ingredients` | ImportIngredient[] | In source order |
| `steps` | `{index, text, imageUrl}[]` | Ordered and indexed from 1. Text is plain, with one bullet per line. |
| `orderWeeks` | string[] | ISO weeks (`2026-W30`) the household received this recipe, across all aliases |

## ImportIngredient

| Field | Type | Notes |
| --- | --- | --- |
| `sourceIngredientId` | string | Source ingredient ID, used to map to canonical DinnerOS ingredients |
| `name`, `slug`, `imageUrl` | string | |
| `pantryStaple` | bool | Source hint that the ingredient isn't shipped (salt, pepper, oil) and is expected at home. The household pantry decides the final grocery list. |
| `amounts` | ImportAmount[] | One entry per serving size, ascending |

## ImportAmount

| Field | Type | Notes |
| --- | --- | --- |
| `servings` | int | |
| `quantity` | number or null | `null` means no amount was given ("to taste"). Nothing is invented. |
| `unit` | string | DinnerOS unit code: `count`, `clove`, `can`, `package`, `slice`, `bunch`, `pinch`, `tsp`, `tbsp`, `cup`, `floz`, `oz`, `lb`, `g`, `kg`, `ml`, `l`. It's empty when the source has no unit or an unknown unit. |
| `sourceUnit` | string | Unit exactly as the source wrote it |
| `rawText` | string | Human-readable source line, e.g. `½ ounce Parmesan Cheese` |

## ReviewItem

Anything the importer could not map confidently: an unknown unit, an
ingredient with no amounts, an unparseable duration, or a recipe missing
servings or steps.

```json
{ "sourceRecipeId": "…", "recipeName": "…", "field": "ingredients.Mystery Paste.unit", "value": "dollop", "reason": "unknown unit" }
```

Review items are surfaced for a person to resolve. Importers never guess a
value to avoid producing one.

## HelloFresh importer workflow

```text
1. Export order history from the owner's signed-in browser session
      → importers/hellofresh/data/order-history.json
2. go run . fetch        public recipe pages → data/raw/recipes/<id>.json (idempotent)
3. go run . normalize    → data/import/recipes.json (this format)
4. Load into DinnerOS via the recipe import endpoint (Phase 4)
```
