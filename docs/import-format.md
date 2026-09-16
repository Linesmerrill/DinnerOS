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
| `unit` | string | DinnerOS unit code: `count`, `clove`, `can`, `package`, `slice`, `bunch`, `pinch`, `thumb`, `tsp`, `tbsp`, `cup`, `floz`, `oz`, `lb`, `g`, `kg`, `ml`, `l`. It's empty when the source has no unit or an unknown unit. |
| `sourceUnit` | string | Unit exactly as the source wrote it |
| `rawText` | string | Human-readable source line, e.g. `½ ounce Parmesan Cheese` |

## ReviewItem

Anything the importer could not map confidently: an unknown unit, an
ingredient with no amounts, an unparseable duration, or a recipe missing
servings or steps.

A recipe with neither `prepMinutes` nor `totalMinutes` gets a `cookTime` item,
because its effective cook time is unknown and no number is invented for it. A
missing `totalMinutes` on its own is normal (HelloFresh omits it for every
add-on and some mains) and is not flagged: cook time is max of the two, so prep
alone still answers the question.

```json
{ "sourceRecipeId": "…", "recipeName": "…", "field": "ingredients.Mystery Paste.unit", "value": "dollop", "reason": "unknown unit" }
```

Review items are surfaced for a person to resolve. Importers never guess a
value to avoid producing one.

## HelloFresh importer workflow

```text
1. Export order history from the owner's signed-in browser session
      → data/history-part*.tsv (week, menuId, M|A, deliveredId, slug) or JSON
2. go run . fetch -history 'data/history-part*.tsv'
      public recipe pages → data/raw/recipes/<id>.json (idempotent)
3. go run . variants -history ...    delivered IDs whose public page is a
      different variant → data/variants-pending.json
4. (optional) capture those from the signed-in past-deliveries view as
      {deliveredId: recipe} JSON, then: go run . capture -in captures.json
      → data/raw/delivered/<id>.json
5. go run . normalize -history ...   → data/import/recipes.json (this format)
6. Load into DinnerOS (below)
```

Normalization rules worth knowing:

- Weekly menu clones merge into their canonical recipe. Recipes with the same
  name (a dish republished under new IDs) merge under the newest ID; older IDs
  become `sourceAliases`.
- An account capture is the exact variant delivered and replaces the public
  page for that delivered ID. Uncaptured variants produce a `variant` review
  item, because the stored details come from a different variant's page.
- Names that differ only by "and"/"with" are the same variant.

## Loading into DinnerOS

Both ways of loading run the same import (`recipes.Service.Import`). Loading the
same file again changes nothing, and a newer file updates recipes in place (see
[database.md](database.md#recipes-and-ingredients) for matching and merge rules).

The whole file is rejected when `version` isn't `1` or `source` is unknown.
Otherwise each recipe is checked on its own, and invalid ones are reported by
index while the rest still import. A recipe is rejected when:

- its `source` is unknown, or `sourceRecipeId` or `name` is empty
- a serving size is not positive
- a `unit` is not empty and not a DinnerOS unit code
- a `quantity` is negative
- an `orderWeeks` value is not an ISO week
- it shares a source ID with an earlier recipe in the file

Review items are stored per household in `import_reviews`, and read back with
`GET /api/v1/households/{householdId}/recipes/import-reviews` (requires
`recipes.import`). Nothing closes an item automatically: a `variant` item is
resolved by capturing that delivered ID from the signed-in account
(`go run . capture`, after `go run . variants` lists what is pending) and
re-importing, which replaces the stored details with the delivered variant's.

**Command (recommended for a full history).** It writes directly to MongoDB, so
HTTP body limits and timeouts don't apply. The household must already exist.

```bash
MONGODB_URI='<connection string>' MONGODB_DATABASE=dinneros \
  go -C api run ./cmd/importrecipes -file ../importers/hellofresh/data/import/recipes.json -household <householdId>
```

It prints the counts and every rejected recipe, and never prints the URI.

**Endpoint.** `POST /api/v1/households/{householdId}/recipes/import` with the
file as the body, as a member whose role has `recipes.import` (see
[api.md](api.md#recipes)). This route accepts bodies up to
`RECIPE_IMPORT_MAX_BYTES` (default 32 MB) instead of `HTTP_MAX_BODY_BYTES`, and
extends the server's read and write timeouts to 5 minutes for the request.
Heroku's router still times out requests after 30 seconds, so against
production, load a multi-megabyte history with the command.
