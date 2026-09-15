# API conventions

The DinnerOS API is REST + JSON. The contract lives in
[`api/openapi.yaml`](../api/openapi.yaml) and is updated in the same commit as
any endpoint change.

## Routing and versioning

- Product endpoints: `/api/v1/...`. Breaking changes go to `/api/v2`. Additive
  changes (new fields or endpoints) stay in v1.
- Operational endpoints are unversioned:
  - `GET /health`: liveness. The process is serving HTTP; dependencies are not checked.
  - `GET /ready`: readiness. Returns `200 {"status":"ready","checks":{"mongodb":"ok"}}`,
    or `503` with `"unavailable"` when a dependency is down. Failure details go
    to the logs only.

## Requests

- Content type: `application/json`. Bodies are limited by `HTTP_MAX_BODY_BYTES`,
  except recipe imports, which use `RECIPE_IMPORT_MAX_BYTES`.
- Authentication: `Authorization: Bearer <access token>`. See
  [authentication.md](authentication.md) for sign-in, refresh, and error codes.
- Unknown JSON fields are rejected on write endpoints to catch client bugs early.
- Request IDs: the server accepts a well-formed `X-Request-ID` or generates one,
  echoes it in the response, and includes it in every log line for that request.

## Responses

- JSON field names are camelCase. IDs are strings.
- Timestamps are RFC 3339 in UTC (`2026-09-14T18:30:00Z`). Plan dates are
  `YYYY-MM-DD` in the household's time zone.
- Lists use cursor pagination: `?limit=50&cursor=...` returns
  `{ "items": [...], "nextCursor": "..." }`. Lists that are small by nature
  (a user's households, a household's pending invitations) return `items`
  without pagination.

### Errors

Every non-2xx response has the same shape:

```json
{
  "error": {
    "code": "not_found",
    "message": "resource not found",
    "requestId": "5f0c1e9a2b7d4c3e8f6a1b2c"
  }
}
```

Handlers decode request bodies with `httpx.DecodeJSON`. It rejects empty
bodies, malformed JSON, unknown fields, wrong types, and trailing data with
`400 invalid_request`, and bodies over the limit with `413 payload_too_large`.

`code` is stable and machine-readable. `message` is for humans and may change.

| Status | Typical codes |
| --- | --- |
| 400 | `invalid_request`, `validation_failed` |
| 401 | `unauthenticated`, `token_expired` |
| 403 | `forbidden` (the caller can see the resource but not perform the action) |
| 404 | `not_found` (also used when the caller may not know the resource exists), `invitation_invalid` |
| 405 | `method_not_allowed` |
| 409 | `conflict`, `last_admin`, `plan_finalized`, `plan_full` |
| 413 | `payload_too_large` |
| 429 | `rate_limited` |
| 500 | `internal` (details only in server logs) |
| 503 | `provider_unavailable` (sign-in method not configured, or provider keys unreachable) |

## Endpoints

| Method | Path | Auth | Phase | Status |
| --- | --- | --- | --- | --- |
| GET | `/health` | — | 0 | ✅ |
| GET | `/ready` | — | 1 | ✅ |
| POST | `/api/v1/auth/apple` `{identityToken, nonce, fullName?}` → session | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/google` `{idToken, nonce?}` → session | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/refresh` `{refreshToken}` → token pair | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/logout` `{refreshToken}` → `204` | rate limited | 2 | ✅ |
| POST | `/api/v1/auth/dev` `{subject, email?, displayName?}` → session (development only) | rate limited | 2 | ✅ |
| GET | `/api/v1/me` → `{user, identities}` | bearer | 2 | ✅ |
| POST | `/api/v1/households` `{name, timeZone, defaultServings?}` → `201 {household, membership}` | bearer | 3 | ✅ |
| GET | `/api/v1/households` → `{items: [{household, role, permissions}]}` | bearer | 3 | ✅ |
| GET | `/api/v1/households/{householdId}` → `{household, members, role, permissions}` | `household.view` | 3 | ✅ |
| PATCH | `/api/v1/households/{householdId}` `{name?, timeZone?, defaultServings?}` → household | `household.update` | 3 | ✅ |
| PATCH | `/api/v1/households/{householdId}/members/{userId}` `{role}` → member | `members.changeRole` | 3 | ✅ |
| DELETE | `/api/v1/households/{householdId}/members/{userId}` → `204` | `members.remove`, or your own ID to leave | 3 | ✅ |
| POST | `/api/v1/households/{householdId}/invitations` `{email, role}` → `201 {invitation, code, emailDelivered}` | `members.invite`, rate limited | 3 | ✅ |
| GET | `/api/v1/households/{householdId}/invitations` → `{items}` (pending only) | `members.invite` | 3 | ✅ |
| DELETE | `/api/v1/households/{householdId}/invitations/{invitationId}` → `204` | `members.invite` | 3 | ✅ |
| POST | `/api/v1/invitations/accept` `{token}` or `{code}` → `{household, role, permissions}` | bearer, rate limited | 3 | ✅ |
| GET | `/api/v1/households/{householdId}/recipes` `?q&addons&tag&cuisine&sort&limit&cursor` → `{items, nextCursor}` | `household.view` | 4 | ✅ |
| GET | `/api/v1/households/{householdId}/recipes/{recipeId}` → recipe | `household.view` | 4 | ✅ |
| POST | `/api/v1/households/{householdId}/recipes/import` import file → `{created, updated, unchanged, ingredientsCreated, reviewItems, errors}` | `recipes.import` | 4 | ✅ |
| GET | `/api/v1/households/{householdId}/plans` `?from&to` → `{items: [{week, startDate, status, entryCount, updatedAt}]}` | `household.view` | 6 | ✅ |
| GET | `/api/v1/households/{householdId}/plans/{week}` → plan (an empty draft if unplanned) | `household.view` | 6 | ✅ |
| POST | `/api/v1/households/{householdId}/plans/{week}/entries` `{recipeId, day?, servings, note?}` → `201 {entry, plan}` | `plan.edit` | 6 | ✅ |
| PATCH | `/api/v1/households/{householdId}/plans/{week}/entries/{entryId}` `{day?, servings?, note?}` → plan | `plan.edit` | 6 | ✅ |
| DELETE | `/api/v1/households/{householdId}/plans/{week}/entries/{entryId}` → `204` | `plan.edit` | 6 | ✅ |
| PUT | `/api/v1/households/{householdId}/plans/{week}/status` `{status}` → plan | `plan.edit` | 6 | ✅ |
| GET | `/api/v1/households/{householdId}/plans/{week}/grocery` → `{week, status, pantryApplied, categories, skipped}` | `household.view` | 6 | ✅ |
| GET | `/api/v1/ingredients` `?q&limit` → `{items: [{id, key, name, category, categoryConfident, imageUrl?}]}` | bearer | 7 | ✅ |
| GET | `/api/v1/households/{householdId}/pantry` `?status&category&staple&q` → `{items}` | `household.view` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/pantry` `{ingredientId? or name, category?, quantity?, unit?, status?, isStaple?, expiresOn?, note?}` → `201` item, or `200` when merged | `pantry.edit` | 7 | ✅ |
| PATCH | `/api/v1/households/{householdId}/pantry/{itemId}` `{displayName?, category?, quantity?, unit?, status?, isStaple?, expiresOn?, note?}` → item | `pantry.edit` | 7 | ✅ |
| DELETE | `/api/v1/households/{householdId}/pantry/{itemId}` → `204` | `pantry.edit` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/pantry/bulk` `{items: [{id, status}]}` → `{items, missing}` | `pantry.edit` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/pantry/staples/defaults` → `{items, skipped}` | `pantry.edit` | 7 | ✅ |
| … | saved grocery lists, providers, events | | 7–9 | planned |

Household-scoped routes return `404 not_found` to anyone who isn't a member,
so a household's existence is never revealed, and `403 forbidden` to members
whose role lacks the permission in the Auth column. Roles, permissions,
invitations, and the last-admin rule are described in
[authentication.md](authentication.md#authorization).

## Recipes

### List

`GET /api/v1/households/{householdId}/recipes`

| Parameter | Meaning |
| --- | --- |
| `q` | Text the name contains, case-insensitive. Literal text, not a pattern; at most 100 characters. |
| `addons` | `true` for add-ons only (sides, desserts), `false` for main meals only |
| `tag`, `cuisine` | Case-insensitive match |
| `sort` | `name` (default, A–Z), `recent` (latest `lastOrderedWeek` first, never-ordered last), or `popular` (most `timesOrdered` first). Ties break by name. |
| `limit`, `cursor` | Page size (default 50, at most 200). Send `nextCursor` back as `cursor` with the same `sort` and filters. |

```json
{
  "items": [
    {
      "id": "66e5a1f2c3b4a5d6e7f80915",
      "name": "Beef Tacos",
      "headline": "with Lime Crema",
      "imageUrl": "https://img.example.com/beef-tacos.jpg",
      "totalMinutes": 30,
      "timesOrdered": 3,
      "lastOrderedWeek": "2026-W30",
      "isAddon": false,
      "tags": ["Quick"]
    }
  ],
  "nextCursor": "eyJzIjoibmFtZSIsIm4iOiJCZWVmIFRhY29zIiwiaSI6IjY2ZTUuLi4ifQ"
}
```

### Get

`GET /api/v1/households/{householdId}/recipes/{recipeId}` returns the full recipe.
Each ingredient carries its catalog `category`. `quantity` is exact (`"1/2"`);
use `quantityValue` only for display. Both are `null` when the source gave no
amount. Array fields are always present, possibly empty.

```json
{
  "id": "66e5a1f2c3b4a5d6e7f80915",
  "householdId": "66e5a1f2c3b4a5d6e7f80913",
  "source": "hellofresh",
  "sourceRecipeId": "5f4d…",
  "sourceAliases": ["6123…"],
  "name": "Beef Tacos",
  "isAddon": false,
  "servings": [2, 4],
  "cuisines": ["Mexican"], "tags": ["Quick"], "utensils": [], "allergens": [],
  "nutritionPerServing": [{ "name": "Calories", "amount": 640, "unit": "kcal" }],
  "ingredients": [
    {
      "ingredientId": "66e5a1f2c3b4a5d6e7f80a10",
      "name": "Parmesan Cheese",
      "category": "dairy-eggs",
      "pantryStaple": false,
      "amounts": [
        { "servings": 2, "quantity": "1/2", "quantityValue": 0.5, "unit": "oz", "sourceUnit": "ounce", "rawText": "½ ounce Parmesan Cheese" }
      ]
    },
    {
      "ingredientId": "66e5a1f2c3b4a5d6e7f80a11",
      "name": "Salt",
      "category": "spices",
      "pantryStaple": true,
      "amounts": [
        { "servings": 2, "quantity": null, "quantityValue": null, "unit": "", "sourceUnit": "", "rawText": "Salt" }
      ]
    }
  ],
  "steps": [{ "index": 1, "text": "Preheat the oven." }],
  "orderWeeks": ["2026-W12", "2026-W30"],
  "timesOrdered": 2,
  "lastOrderedWeek": "2026-W30",
  "createdAt": "2026-09-14T18:30:00Z",
  "updatedAt": "2026-09-14T18:30:00Z"
}
```

### Import

`POST /api/v1/households/{householdId}/recipes/import` takes an import file
([import-format.md](import-format.md)) as the body. The body may be up to
`RECIPE_IMPORT_MAX_BYTES` (default 32 MB), and the server's read and write
timeouts are extended for this request. The import is idempotent: posting the
same file again reports every recipe as `unchanged`. Invalid recipes are listed
in `errors` by their index in `recipes`, and the rest still import.

```json
{
  "created": 2,
  "updated": 0,
  "unchanged": 0,
  "ingredientsCreated": 14,
  "reviewItems": 1,
  "errors": [
    {
      "index": 2,
      "sourceRecipeId": "r-3",
      "name": "Mystery Stew",
      "problems": ["ingredients[1] (Mystery Paste): unit \"dollop\" is not a DinnerOS unit code"]
    }
  ]
}
```

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Invalid list parameter or cursor; import file with a `version` other than 1 or an unknown `source` |
| 400 | `invalid_request` | Import body is empty, malformed, or has unknown fields |
| 403 | `forbidden` | Importing without `recipes.import` |
| 404 | `not_found` | Not a member of the household, or no such recipe in it |
| 409 | `conflict` | A concurrent import of the same recipes won; retry |
| 413 | `payload_too_large` | Import body over `RECIPE_IMPORT_MAX_BYTES` |

For a full order history against production, prefer the `importrecipes`
command (see [import-format.md](import-format.md#loading-into-dinneros)).

## Plans

A plan is a household's recipes for one ISO week (`2026-W38`, Monday to
Sunday). The week number must exist in its year: 2026 has 53 weeks, 2025 has
52. An invalid week is `400 validation_failed`.

### Get a week

`GET /api/v1/households/{householdId}/plans/{week}`

```json
{
  "householdId": "66e5a1f2c3b4a5d6e7f80913",
  "week": "2026-W38",
  "startDate": "2026-09-14",
  "endDate": "2026-09-20",
  "status": "draft",
  "entries": [
    {
      "id": "66e5a1f2c3b4a5d6e7f80c01",
      "recipe": { "id": "66e5a1f2c3b4a5d6e7f80915", "name": "Beef Tacos", "imageUrl": "https://img.example.com/beef-tacos.jpg" },
      "day": "tue",
      "date": "2026-09-15",
      "servings": 2,
      "note": "extra lime",
      "addedBy": "66e5a1f2c3b4a5d6e7f80912",
      "addedAt": "2026-09-14T18:30:00Z"
    },
    {
      "id": "66e5a1f2c3b4a5d6e7f80c02",
      "recipe": { "id": "66e5a1f2c3b4a5d6e7f80916", "name": "Onion Soup" },
      "day": null,
      "date": null,
      "servings": 4,
      "note": "",
      "addedBy": "66e5a1f2c3b4a5d6e7f80917",
      "addedAt": "2026-09-14T19:05:00Z"
    }
  ],
  "createdAt": "2026-09-14T18:30:00Z",
  "updatedAt": "2026-09-14T19:05:00Z"
}
```

- `day` is `mon`–`sun`, or `null` for "this week, not scheduled". `date` is
  that day's `YYYY-MM-DD`, or `null`.
- `recipe` is a snapshot of the name and image taken when the entry was
  added. Use `GET .../recipes/{id}` for details.
- Entries are in the order they were added.
- A week nobody has planned returns `status: "draft"`, `entries: []`, and
  `null` timestamps. Nothing is stored until someone adds an entry or sets the
  status.

### List weeks

`GET /api/v1/households/{householdId}/plans?from=2026-W30&to=2026-W40`
returns every week from `from` to `to`, in order, including unplanned weeks
(`entryCount: 0`, `updatedAt: null`). Both parameters are required, `to` must
not be before `from`, and the range covers at most 26 weeks.

```json
{ "items": [{ "week": "2026-W30", "startDate": "2026-07-20", "status": "draft", "entryCount": 3, "updatedAt": "2026-07-18T09:00:00Z" }] }
```

### Change a week

Requires `plan.edit`.

- `POST .../plans/{week}/entries` `{recipeId, day?, servings, note?}`.
  `recipeId` must be a recipe in the household, `servings` one of the
  recipe's `servings`, and `note` at most 500 characters. Returns
  `201 {entry, plan}`. A week holds at most 50 entries.
- `PATCH .../plans/{week}/entries/{entryId}` with at least one of `day`,
  `servings`, or `note`. `"day": null` unschedules; fields you leave out
  don't change. Returns the plan.
- `DELETE .../plans/{week}/entries/{entryId}` returns `204`.
- `PUT .../plans/{week}/status` `{"status": "finalized"}` or `"draft"`. A
  finalized plan's entries can't change (`409 plan_finalized`) until its
  status is set back to `draft`.

Members can edit the same week at the same time. Each change is applied to
the stored plan atomically, so nobody's change is lost and there is no version
to send or `409 conflict` to retry. Responses show the plan after your change,
including other members' changes.

### Grocery list

`GET /api/v1/households/{householdId}/plans/{week}/grocery` (`household.view`)

```json
{
  "week": "2026-W38",
  "status": "draft",
  "pantryApplied": true,
  "categories": [
    {
      "category": "produce",
      "items": [
        {
          "ingredientKey": "66e5a1f2c3b4a5d6e7f80a12",
          "name": "Yellow Onion",
          "amounts": [
            { "quantity": "3/2", "quantityValue": 1.5, "unit": "count", "text": "1 ½" },
            { "quantity": "8", "quantityValue": 8, "unit": "oz", "text": "8 oz" }
          ],
          "quantityText": "1 ½ + 8 oz",
          "unquantified": false,
          "status": "toBuy",
          "recipes": [
            { "id": "66e5a1f2c3b4a5d6e7f80915", "name": "Beef Tacos" },
            { "id": "66e5a1f2c3b4a5d6e7f80916", "name": "Onion Soup" }
          ]
        }
      ]
    }
  ],
  "skipped": []
}
```

- The list is computed on every request by the grocery engine
  ([grocery-engine.md](grocery-engine.md)) from each entry's live recipe, using
  the amounts the source authored for the entry's serving size. Amounts are
  never scaled from another size.
- Categories are in aisle order (`produce`, `meat-seafood`, `dairy-eggs`,
  `bakery`, `deli`, `pantry`, `spices`, `condiments`, `frozen`, `beverages`,
  `other`). Empty categories are left out.
- Amounts whose units can't be combined stay separate, as with the onion
  above. `quantity` is exact, `text` is for display, and `quantityText` joins
  the texts with ` + `. `unquantified: true` means at least one recipe gave no
  amount ("salt to taste").
- `status` comes from the household [pantry](#pantry): `inPantry` when the
  item is `in_stock`, `toBuy` when it's `out`, and otherwise `pantryHint` when
  every contributing recipe marks it as a pantry staple, or `toBuy`. Pantry
  amounts aren't compared with what the recipes need yet.
- `pantryApplied` is `true` when the pantry decided the statuses. A server
  running without a pantry returns `false`, and statuses are then only
  `toBuy` or `pantryHint`.
- `skipped` lists entries that couldn't contribute: `recipeUnavailable` (the
  recipe is no longer in the household) or `servingsUnavailable` (the recipe
  no longer offers that serving size).

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Invalid week, range, day, servings, status, or note; no fields in a PATCH; `recipeId` not in the household |
| 400 | `invalid_request` | Body is empty, malformed, has unknown fields, or wrong types |
| 403 | `forbidden` | Changing a plan without `plan.edit` |
| 404 | `not_found` | Not a member of the household; no such entry in that week |
| 409 | `plan_finalized` | The plan is finalized; set its status to `draft` first |
| 409 | `plan_full` | The week already has 50 entries |

## Pantry

A household's pantry records what it has at home, with one item per
ingredient. `key` is the normalized name (`"Olive Oil"` → `olive oil`), or the
catalog ingredient's key when the item references the catalog. A pantry holds
at most 1000 items, and lists aren't paginated.

### List

`GET /api/v1/households/{householdId}/pantry` (`household.view`)

| Parameter | Meaning |
| --- | --- |
| `status` | `in_stock`, `low`, or `out` |
| `category` | A grocery category (`produce`, …, `other`) |
| `staple` | `true` for staples only, `false` for the rest |
| `q` | Text the display name contains (case-insensitive), or that the normalized name contains, so `jalapeno` finds "Jalapeño". Literal text; at most 100 characters. |

Items are in aisle order, then by name.

```json
{
  "items": [
    {
      "id": "66e5a1f2c3b4a5d6e7f80d01",
      "householdId": "66e5a1f2c3b4a5d6e7f80913",
      "ingredientId": "66e5a1f2c3b4a5d6e7f80a14",
      "key": "olive oil",
      "displayName": "Olive Oil",
      "category": "pantry",
      "quantity": "3/2",
      "quantityValue": 1.5,
      "unit": "cup",
      "status": "in_stock",
      "isStaple": true,
      "expiresOn": "2027-03-01",
      "note": "big tin",
      "updatedBy": "66e5a1f2c3b4a5d6e7f80912",
      "createdAt": "2026-09-15T18:30:00Z",
      "updatedAt": "2026-09-15T18:30:00Z"
    },
    {
      "id": "66e5a1f2c3b4a5d6e7f80d02",
      "householdId": "66e5a1f2c3b4a5d6e7f80913",
      "ingredientId": null,
      "key": "za'atar",
      "displayName": "Za'atar",
      "category": "spices",
      "quantity": null,
      "quantityValue": null,
      "unit": null,
      "status": "low",
      "isStaple": false,
      "expiresOn": null,
      "note": "",
      "updatedBy": "66e5a1f2c3b4a5d6e7f80912",
      "createdAt": "2026-09-15T18:31:00Z",
      "updatedAt": "2026-09-15T18:31:00Z"
    }
  ]
}
```

- `quantity` is exact; use `quantityValue` only for display. `quantity`,
  `quantityValue`, and `unit` are `null` together when no amount was recorded
  ("have some").
- `ingredientId` is `null` for free text that matched no catalog ingredient.

### Change the pantry

Requires `pantry.edit`.

- `POST .../pantry` adds an ingredient by `ingredientId` (from the
  [ingredient catalog](#ingredient-catalog)) or by `name`. A name that is a
  catalog ingredient is linked to it. `category` defaults to the catalog's, or
  to the category rules, and `status` defaults to `in_stock`. Returns `201`
  with the new item, or `200` when the pantry already had that ingredient. In
  that case `status` and every field you sent are applied, fields you left out
  keep their values, and the display name is kept. Retrying is safe.
- `PATCH .../pantry/{itemId}` changes the fields you send: `displayName`,
  `category`, `quantity`, `unit`, `status`, `isStaple`, `expiresOn`, or
  `note`. Send `null` (or `""`) for `quantity`, `expiresOn`, or `note` to
  clear it; clearing `quantity` also clears `unit`.
- `DELETE .../pantry/{itemId}` returns `204`.
- `POST .../pantry/bulk` `{"items": [{"id": "...", "status": "in_stock"}]}`
  sets up to 200 statuses at once, for example after shopping. It returns
  `{items, missing}`: the updated items in request order, and the requested
  IDs that aren't in the pantry (say, another member deleted them).
- `POST .../pantry/staples/defaults`, with no body, adds salt, black pepper,
  cooking oil, olive oil, butter, sugar, flour, garlic powder, and onion
  powder, in stock and marked `isStaple`. Each links to the catalog ingredient
  with its name or an alias ("Pepper" for black pepper). Staples the pantry
  already has, under either name, are left as they are and counted:
  `{"items": [...added items], "skipped": 2}`. Calling it again adds nothing.

Rules:

- `quantity` is a positive amount such as `2`, `0.5`, `1/2`, `1 1/2`, or `½`,
  stored reduced (`"3/2"`). It's a string: a JSON number is
  `400 invalid_request`.
- `unit` is a DinnerOS unit code (`count`, `clove`, `can`, `tsp`, `tbsp`,
  `cup`, `ml`, `oz`, `lb`, `g`, …; see
  [grocery-engine.md](grocery-engine.md#units)). A quantity without a unit is
  a `count`; a unit without a quantity is invalid.
- Setting `status` to `out` clears the amount, and an `out` item can't be
  given one.
- `expiresOn` is `YYYY-MM-DD`. `note` is at most 500 characters.
- Grocery lists use the pantry as described under [Grocery list](#grocery-list).

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Missing name; unknown unit code; a quantity that isn't a positive amount; unknown status or category; bad date; no fields in a PATCH; `null` for `displayName`, `category`, `status`, or `isStaple`; unknown `ingredientId`; empty, blank, or repeated bulk items; a full pantry; invalid list filters |
| 400 | `invalid_request` | Body is empty, malformed, has unknown fields, or wrong types |
| 403 | `forbidden` | Changing the pantry without `pantry.edit` |
| 404 | `not_found` | Not a member of the household; no such item in its pantry |
| 409 | `conflict` | The item kept changing concurrently; retry |

## Ingredient catalog

`GET /api/v1/ingredients?q=oil&limit=20` searches the global ingredient
catalog, for example to autocomplete a pantry item. Any signed-in user may
search; no household is involved.

- `q` is required. It's normalized like catalog keys (case, accents, and
  punctuation are ignored) and matches the start of any word in the name:
  `oil` finds "Oil", "Olive Oil", and "Sesame Oil", but not "Boiled Egg".
- The exact name comes first, then names that start with `q`, then names with
  a later word that starts with it. Each group is alphabetical.
- `limit` defaults to 20; values above 50 are treated as 50.

```json
{
  "items": [
    { "id": "66e5a1f2c3b4a5d6e7f80a13", "key": "oil", "name": "Oil", "category": "pantry", "categoryConfident": true },
    { "id": "66e5a1f2c3b4a5d6e7f80a14", "key": "olive oil", "name": "Olive Oil", "category": "pantry", "categoryConfident": true }
  ]
}
```

`categoryConfident: false` means no category rule matched, so the category is
a placeholder (`other`) awaiting review.
