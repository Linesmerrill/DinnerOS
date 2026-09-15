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
- Browser-facing routes are unversioned too. They're mounted through
  `httpapi.Options.WebRoutes` from `api/internal/applinks`:
  - `GET /invite`: the invitation landing page. The token is in the URL
    fragment, so the server never sees it. See
    [authentication.md](authentication.md#invitation-links).
  - `GET /.well-known/apple-app-site-association`: the iOS app's universal-link
    association, served as `application/json` once `APPLE_TEAM_ID` is set.

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
| POST | `/api/v1/invitations/preview` `{token}` or `{code}` → `{householdName, inviterName, role, expiresAt}` | none, rate limited (shares accept's bucket) | 3 | ✅ |
| GET | `/invite` → HTML landing page for invitation links (token in the `#token=` fragment) | — | 3 | ✅ |
| GET | `/.well-known/apple-app-site-association` → universal-link association JSON (404 until `APPLE_TEAM_ID` is set) | — | 3 | ✅ |
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
| PATCH | `/api/v1/households/{householdId}/pantry/{itemId}` `{displayName?, category?, quantity?, unit?, status?, isStaple?, expiresOn?, note?, lowThresholdPercent?}` → item | `pantry.edit` | 7 | ✅ |
| DELETE | `/api/v1/households/{householdId}/pantry/{itemId}` → `204` | `pantry.edit` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/pantry/bulk` `{items: [{id, status}]}` → `{items, missing}` | `pantry.edit` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/pantry/staples/defaults` → `{items, skipped}` | `pantry.edit` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/pantry/purchases` `{itemId? or ingredientId?/name?, source, quantity?, unit?, unitSize?, week?, clientPurchaseId?}` → `201 {purchase, item}`, or `200` for a repeated `clientPurchaseId` | `pantry.edit` | 7 | ✅ |
| GET | `/api/v1/households/{householdId}/pantry/{itemId}/purchases` → `{items}` (newest 20) | `household.view` | 7 | ✅ |
| GET | `/api/v1/households/{householdId}/pantry/settings` → `{lowThresholdPercent, defaultLowThresholdPercent, updatedBy, updatedAt}` | `household.view` | 7 | ✅ |
| PUT | `/api/v1/households/{householdId}/pantry/settings` `{lowThresholdPercent}` → settings | `pantry.edit` | 7 | ✅ |
| GET | `/api/v1/households/{householdId}/specialty-ingredients` `?all` → `{items}` | `household.view` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/specialty-ingredients/choices/defaults` → `{items, skipped}` | `pantry.edit` | 7 | ✅ |
| GET | `/api/v1/households/{householdId}/specialty-ingredients/{specialtyId}` → specialty ingredient | `household.view` | 7 | ✅ |
| PUT | `/api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/choice` `{optionId}` → specialty ingredient | `pantry.edit` | 7 | ✅ |
| DELETE | `/api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/choice` → `204` | `pantry.edit` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/options` option → `201` option | `pantry.edit` | 7 | ✅ |
| PUT | `/api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/options/{optionId}` option → option | `pantry.edit` | 7 | ✅ |
| DELETE | `/api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/options/{optionId}` → `204` | `pantry.edit` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/batches` `{optionId?, batches?, clientPurchaseId?}` → `201 {purchase, item, option}`, or `200` for a repeated `clientPurchaseId` | `pantry.edit` | 7 | ✅ |
| GET | `/api/v1/households/{householdId}/notifications` `?unread&limit&before` → `{items, nextCursor}` | `household.view` | 7 | ✅ |
| GET | `/api/v1/households/{householdId}/notifications/unread-count` → `{unreadCount}` | `household.view` | 7 | ✅ |
| POST | `/api/v1/households/{householdId}/notifications/read` `{ids}` or `{all: true}` → `{unreadCount}` | `household.view` | 7 | ✅ |
| PUT | `/api/v1/households/{householdId}/recipes/{recipeId}/rating` `{score, comment?, tags?}` → rating | `household.view` | 9 | ✅ |
| DELETE | `/api/v1/households/{householdId}/recipes/{recipeId}/rating` → `204` | `household.view` | 9 | ✅ |
| GET | `/api/v1/households/{householdId}/recipes/{recipeId}/ratings` → `{householdRating, items}` | `household.view` | 9 | ✅ |
| POST | `/api/v1/households/{householdId}/events` `{events}` → `{accepted, duplicates, rejected}` | `household.view`, rate limited per user | 9 | ✅ |
| … | saved grocery lists, providers | | 8 | planned |

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
      "tags": ["Quick"],
      "householdRating": { "average": 4.5, "count": 2 },
      "myRating": null
    }
  ],
  "nextCursor": "eyJzIjoibmFtZSIsIm4iOiJCZWVmIFRhY29zIiwiaSI6IjY2ZTUuLi4ifQ"
}
```

Every item carries `householdRating` and `myRating` (see [Ratings](#ratings)).
The recipe detail has the same two fields.

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
  "updatedAt": "2026-09-14T18:30:00Z",
  "householdRating": { "average": 4.5, "count": 2 },
  "myRating": {
    "recipeId": "66e5a1f2c3b4a5d6e7f80915",
    "userId": "66e5a1f2c3b4a5d6e7f80912",
    "score": 5,
    "comment": "",
    "tags": ["kid-favorite"],
    "createdAt": "2026-09-14T19:00:00Z",
    "updatedAt": "2026-09-14T19:00:00Z"
  }
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

#### Specialty ingredients on the list

Specialty ingredients (meal-kit blends, sauces, concentrates) are handled as
the household chose ([Specialty ingredients](#specialty-ingredients),
[specialty-ingredients.md](specialty-ingredients.md)) before aggregating.
`specialtiesApplied` is `true` when that happened. Every item has three more
fields, and the list has `batches`:

```json
{
  "specialtiesApplied": true,
  "categories": [
    {
      "category": "condiments",
      "items": [
        {
          "ingredientKey": "66e5a1f2c3b4a5d6e7f80a20",
          "name": "Tomato Paste",
          "amounts": [{ "quantity": "7/3", "quantityValue": 2.3333333333333335, "unit": "tbsp", "text": "2 ⅓ tbsp" }],
          "quantityText": "2 ⅓ tbsp",
          "unquantified": false,
          "status": "toBuy",
          "recipes": [{ "id": "…15", "name": "Chili Bowls" }, { "id": "…16", "name": "Smoky Pork Tacos" }],
          "specialty": false,
          "specialtyDetail": null,
          "via": [
            {
              "kind": "store_alternative",
              "specialtyId": "tex-mex-paste", "specialtyKey": "tex mex paste", "specialtyName": "Tex-Mex Paste",
              "optionId": "tex-mex-paste.store", "optionName": "Tomato paste and chili spices",
              "yield": null, "batches": null,
              "recipes": [{ "id": "…16", "name": "Smoky Pork Tacos" }],
              "text": "for Tex-Mex Paste in Smoky Pork Tacos"
            }
          ]
        },
        {
          "ingredientKey": "name:sweet soy glaze",
          "name": "Sweet Soy Glaze",
          "amounts": [{ "quantity": "2", "quantityValue": 2, "unit": "tbsp", "text": "2 tbsp" }],
          "quantityText": "2 tbsp", "unquantified": false, "status": "toBuy",
          "recipes": [{ "id": "…17", "name": "Teriyaki Bowls" }],
          "specialty": true,
          "specialtyDetail": {
            "id": "sweet-soy-glaze", "key": "sweet soy glaze", "name": "Sweet Soy Glaze",
            "choiceType": null, "optionId": null, "houseMade": false,
            "suggestedOptions": [
              { "id": "sweet-soy-glaze.store", "type": "store_alternative", "name": "Soy and honey", "isDefault": true },
              { "id": "sweet-soy-glaze.batch", "type": "house_made_batch", "name": "Sweet soy glaze (house batch)", "isDefault": false }
            ],
            "text": "Specialty ingredient: choose a store alternative or a house-made batch"
          },
          "via": []
        }
      ]
    }
  ],
  "batches": [
    {
      "specialtyId": "southwest-spice-blend", "specialtyKey": "southwest spice blend", "specialtyName": "Southwest Spice Blend",
      "optionId": "southwest-spice-blend.batch", "optionName": "Southwest spice blend (house blend)",
      "yield": { "quantity": "12", "quantityValue": 12, "unit": "tbsp", "text": "12 tbsp" },
      "status": "make", "reason": "missing", "batches": 1,
      "pantryItemId": null, "remaining": null,
      "needed": { "quantity": "2", "quantityValue": 2, "unit": "tbsp", "text": "2 tbsp" },
      "recipes": [{ "id": "…15", "name": "Chili Bowls" }, { "id": "…16", "name": "Smoky Pork Tacos" }],
      "text": "Make a batch (makes about 12 tbsp)"
    }
  ]
}
```

- `specialty` is `true` when the item is a specialty ingredient by its own
  name: the household hasn't chosen (`specialtyDetail.choiceType: null`, with
  `suggestedOptions`, the default first), chose `as_is`, or a house-made batch
  in the pantry covers the week (`choiceType: house_made_batch`,
  `houseMade: true`, `status: inPantry`, `ingredientKey: "name:<key>"`).
- `via` lists what the item stands in for. `store_alternative`: the option's
  ingredients, scaled exactly from the recipe's amount (packet counts convert
  through the specialty's packet size; an amount that can't convert lists the
  ingredient without one). `house_made_batch`: the ingredients to make
  `batches` batches, each `yield`. `text` is ready to show.
- `batches` has one entry per house-made specialty the week uses. `status` is
  `inPantry` or `make`; `reason` is `enough` or `inStock` (in pantry), or
  `notEnough`, `low`, `out`, `missing` (make). `needed` is the week's total in
  the yield's unit (`null` when a recipe gives no amount), `remaining` the
  pantry estimate. Several batches are asked for when the shortfall exceeds
  one yield.
- Without the specialty module, `specialtiesApplied` is `false`, `batches` and
  `via` are empty, and `specialty` is `false`.

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

### Usage estimates

Pantry items estimate what's left from purchases, cooked recipes, and learned
use, and are marked `low` automatically at a threshold
([pantry-usage.md](pantry-usage.md)). Every item response (list, add, update,
bulk, staples, purchases) has these fields:

| Field | Meaning |
| --- | --- |
| `statusSource` | `person`, or `estimate` when the usage estimate marked it `low` |
| `lowThresholdPercent` | The item's own threshold (1–100), or `null` to use the household's |
| `unitSize` | `{per, quantity, quantityValue, unit}`: one `per` ("count") holds `quantity` `unit` ("1/2" "cup"), or `null` |
| `estimate` | `null` when no amount is recorded; otherwise the object below |

| `estimate` field | Meaning |
| --- | --- |
| `cycleId`, `cycleSource`, `cycleStartedAt` | The current cycle: the purchase ID and source (`grocery_list`, `manual`, `provider`, `house_made` for a batch made), or `edit` when a person set the amount without a purchase |
| `adjustedAt` | When a person last corrected the amount in this cycle, or `null` |
| `unit` | Every amount below is in this unit code |
| `startAmount` | 100%: the amount bought (exact) |
| `remaining` | Estimated amount left, rounded to hundredths |
| `percentRemaining`, `percentUsed` | Whole percents that add up to 100 |
| `recipeUse` | `{count, quantity, quantityValue}`: cooked recipes deducted this cycle |
| `otherUse` | Learned non-recipe use applied since the last purchase or correction |
| `dailyRate` | `{quantity, quantityValue, basedOnSegments}`, or `null` until at least 2 usage periods are known |
| `skippedRecipes` | Cooked recipes that used the item but couldn't be deducted (no amount, or units that don't convert) |
| `lowThresholdPercent`, `thresholdSource` | The threshold in effect and whether it's the `item`'s or the `household`'s |
| `belowThreshold` | At least `lowThresholdPercent` of `startAmount` is used |
| `summary` | English explanation: "About 31% left: 2 recipes used 6 tbsp, plus about 1 tbsp a day of other use." |
| `estimatedAt` | The moment the estimate describes (response time) |

- Reading the pantry first applies time-based decay: an item that crossed its
  threshold since the last read comes back `low` with `statusSource:
  estimate`, and a notification is created.
- `PATCH` with `lowThresholdPercent` sets the item's threshold; `null`
  returns it to the household's. `0` is invalid.
- A person's `quantity` becomes the new starting point for the estimate, and
  a person's `status` always replaces an estimated one.
- Cooking deducts automatically: a `recipe.cooked` event with `entryId` (or a
  `clientEventId`) and `servings` deducts once per entry. There's nothing
  else to call.

### Purchases

`POST .../pantry/purchases` (`pantry.edit`) records that the household bought
an item. The item is added if needed, set `in_stock` with the bought amount,
and a new usage cycle starts.

```json
{
  "ingredientId": "66e5a1f2c3b4a5d6e7f80a14",
  "source": "grocery_list",
  "quantity": "4",
  "unit": "count",
  "unitSize": { "quantity": "1/2", "unit": "cup" },
  "week": "2026-W38",
  "clientPurchaseId": "5b0c7c52-3f6e-4d8e-9f0a-8d7f1c2b3a41"
}
```

```json
{
  "purchase": {
    "id": "66e5a1f2c3b4a5d6e7f80f01",
    "householdId": "66e5a1f2c3b4a5d6e7f80913",
    "itemId": "66e5a1f2c3b4a5d6e7f80d01",
    "source": "grocery_list",
    "quantity": "4",
    "quantityValue": 4,
    "unit": "count",
    "unitSize": { "per": "count", "quantity": "1/2", "quantityValue": 0.5, "unit": "cup" },
    "week": "2026-W38",
    "clientPurchaseId": "5b0c7c52-3f6e-4d8e-9f0a-8d7f1c2b3a41",
    "recordedBy": "66e5a1f2c3b4a5d6e7f80912",
    "purchasedAt": "2026-09-15T18:30:00Z"
  },
  "item": { "id": "66e5a1f2c3b4a5d6e7f80d01", "status": "in_stock", "estimate": { "unit": "cup", "startAmount": { "quantity": "2", "quantityValue": 2 }, "…": "…" }, "…": "…" }
}
```

- Identify the item with `itemId` (restocking from the Pantry tab), or with
  `ingredientId` or `name` (checking off a grocery line), not both.
- `source` is `grocery_list` or `manual`. `provider` is reserved for shopping
  providers and rejected. `house_made` appears on batches recorded through
  [`POST .../specialty-ingredients/{specialtyId}/batches`](#specialty-ingredients)
  and is rejected here.
- `quantity` and `unit` follow the pantry rules. Without `quantity` the item
  is in stock but untracked.
- `unitSize` applies to a discrete `unit` (`count`, `package`, `can`, …) and
  must be a volume or weight. The item remembers it, and its estimate is kept
  in that unit.
- `week` is optional and only for `grocery_list`.
- `clientPurchaseId` (optional, at most 64 characters, per member) makes
  retries safe: a repeat returns `200` with the first purchase and the item as
  it is now, and changes nothing.
- **Grocery check-off flow:** after a member checks a line, ask "Add to
  pantry?" with the line's first amount prefilled and editable. On confirm,
  send the line's `ingredientId` (or its `name` for an uncatalogued line), the
  amount, `source: grocery_list`, the week, and a new `clientPurchaseId`.
  Declining sends nothing.

`GET .../pantry/{itemId}/purchases` (`household.view`) returns the item's
newest 20 purchases: `{"items": [purchase, ...]}`.

### Settings

`GET .../pantry/settings` (`household.view`) and `PUT .../pantry/settings`
(`pantry.edit`) read and set the household's threshold:

```json
{ "lowThresholdPercent": 80, "defaultLowThresholdPercent": 80, "updatedBy": null, "updatedAt": null }
```

`PUT` takes `{"lowThresholdPercent": 70}`, from 1 to 100. `updatedBy` and
`updatedAt` are `null` until someone changes it.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Unknown or `provider` source; both or neither of `itemId` and `ingredientId`/`name`; bad amount, unit size, week, or `clientPurchaseId`; threshold missing or outside 1–100 |
| 403 | `forbidden` | Recording a purchase or changing settings without `pantry.edit` |
| 404 | `not_found` | Not a member of the household; `itemId` isn't in its pantry |
| 409 | `conflict` | The item kept changing concurrently; retry |

## Specialty ingredients

Meal-kit blends, sauces, pastes, and concentrates that recipes name but stores
don't sell under that name, with curated store alternatives and house-made
batches ([specialty-ingredients.md](specialty-ingredients.md)). Reads need
`household.view`; every change needs `pantry.edit`. `specialtyId` is a stable
slug (`southwest-spice-blend`).

### List and get

`GET .../specialty-ingredients` returns the specialty ingredients the
household's recipes use, most used first, then by name; `?all=true` returns
every curated one. `GET .../specialty-ingredients/{specialtyId}` returns one.

```json
{
  "id": "southwest-spice-blend",
  "key": "southwest spice blend",
  "name": "Southwest Spice Blend",
  "aliases": ["Southwestern Spice Blend"],
  "category": "spices",
  "ingredientIds": ["66e5a1f2c3b4a5d6e7f80a30"],
  "recipeCount": 52,
  "unitSizes": [{ "per": "count", "quantity": "1", "quantityValue": 1, "unit": "tbsp", "text": "1 tbsp" }],
  "defaultOptionId": "southwest-spice-blend.batch",
  "retired": false,
  "choice": { "optionId": "southwest-spice-blend.batch", "type": "house_made_batch", "optionName": "Southwest spice blend (house blend)", "chosenBy": "66e5…12", "chosenAt": "2026-09-15T18:30:00Z" },
  "options": [
    {
      "id": "southwest-spice-blend.store", "specialtyId": "southwest-spice-blend", "source": "curated",
      "type": "store_alternative", "name": "Southwest blend from the spice rack", "notes": "", "isDefault": false,
      "per": { "quantity": "1", "quantityValue": 1, "unit": "tbsp", "text": "1 tbsp" },
      "ingredients": [{ "name": "Chili Powder", "quantity": "3/2", "quantityValue": 1.5, "unit": "tsp", "text": "1 ½ tsp Chili Powder", "category": null }],
      "steps": [], "yield": null, "shelfLifeDays": null, "basedOnOptionId": null,
      "summary": "1 tbsp = 1 ½ tsp Chili Powder + ¾ tsp Ground Cumin + ½ tsp Smoked Paprika + ¼ tsp Garlic Powder",
      "createdBy": null, "updatedBy": null, "createdAt": null, "updatedAt": null
    },
    {
      "id": "southwest-spice-blend.batch", "specialtyId": "southwest-spice-blend", "source": "curated",
      "type": "house_made_batch", "name": "Southwest spice blend (house blend)", "notes": "", "isDefault": true,
      "per": null,
      "ingredients": [{ "name": "Chili Powder", "quantity": "3", "quantityValue": 3, "unit": "tbsp", "text": "3 tbsp Chili Powder", "category": null }],
      "steps": ["Stir everything together in a bowl until evenly colored.", "Store in an airtight jar away from heat and light."],
      "yield": { "quantity": "12", "quantityValue": 12, "unit": "tbsp", "text": "12 tbsp" },
      "shelfLifeDays": 180, "basedOnOptionId": null,
      "summary": "Makes about 12 tbsp and keeps 180 days.",
      "createdBy": null, "updatedBy": null, "createdAt": null, "updatedAt": null
    }
  ],
  "batch": { "pantryItemId": "66e5…d01", "status": "in_stock", "remaining": { "quantity": "9", "quantityValue": 9, "unit": "tbsp", "text": "9 tbsp" }, "percentRemaining": 75, "expiresOn": "2027-03-14" }
}
```

- `recipeCount` counts the household's recipes whose ingredient is this one
  (by catalog ingredient, including aliases). `ingredientIds` are those
  catalog ingredients.
- `options` are the curated options, then the household's (`source:
  household`, oldest first). `per` and the ingredient amounts are exact;
  ingredient `quantity` is `null` for "to taste".
- `choice.type` is `as_is` or the chosen option's type; `null` when there's
  no choice.
- `batch` is the pantry item holding the batch (key = `key`), or `null`.
  `remaining` is `null` when it has no recorded amount.
- A `retired` specialty ingredient (removed from the curated set) is still
  returned by `GET .../{specialtyId}` but isn't listed and can't be chosen.

### Choose

- `PUT .../{specialtyId}/choice` `{"optionId": "southwest-spice-blend.batch"}`
  chooses a curated option, one of the household's options, or `"as_is"`
  (keep it on lists by its own name, and stop suggesting). Returns the
  specialty ingredient.
- `DELETE .../{specialtyId}/choice` clears it (`204`, also when there was
  none).
- `POST .../specialty-ingredients/choices/defaults` chooses `defaultOptionId`
  for every used specialty ingredient without a choice and returns
  `{"items": [...chosen], "skipped": 1}`. Existing choices never change.

### Household options

`POST .../{specialtyId}/options` adds one (`201`); `PUT .../options/{optionId}`
replaces it; `DELETE .../options/{optionId}` deletes it and clears any choice
of it. Curated options can't be changed: copy one with `basedOnOptionId`.

```json
{
  "type": "house_made_batch",
  "name": "Mild southwest blend",
  "notes": "No cayenne.",
  "ingredients": [
    { "name": "Chili Powder", "quantity": "1 1/2", "unit": "tbsp" },
    { "name": "Ground Cumin", "quantity": "1", "unit": "tbsp", "category": "spices" },
    { "name": "Salt" }
  ],
  "steps": ["Mix."],
  "yield": { "quantity": "3", "unit": "tbsp" },
  "shelfLifeDays": 90,
  "basedOnOptionId": "southwest-spice-blend.batch"
}
```

- `type` is `store_alternative` (needs `per`; every ingredient needs a
  quantity; no `yield`, `shelfLifeDays`, or `steps`) or `house_made_batch`
  (needs `yield` and `shelfLifeDays` 1–730; no `per`; ingredients may omit the
  amount).
- `name` 1–100 characters, `notes` ≤ 500, 1–20 ingredients, ≤ 20 steps of ≤ 500
  characters. Quantities and units follow the pantry rules (exact positive
  amounts, DinnerOS unit codes, a quantity without a unit is a `count`).
  `category` is a grocery category, used when the catalog doesn't know the
  ingredient.
- At most 10 household options per specialty ingredient.

### Made a batch

`POST .../{specialtyId}/batches` `{"optionId"?, "batches"?, "clientPurchaseId"?}`
records a batch made and returns `201 {purchase, item, option}`:

- `optionId` defaults to the household's choice and must be a
  `house_made_batch`. `batches` is 1–10 (default 1).
- `purchase` has `source: house_made`, the yield × `batches`, and starts a
  usage cycle on `item`, the pantry item keyed by the specialty ingredient
  (added when missing, named "… (house-made)"), which is `in_stock` with an
  `estimate`, the specialty's packet `unitSize`, and `expiresOn` from the
  shelf life (UTC date).
- Cooking recipes that use the specialty ingredient (by name, alias, or
  packet count) deducts from it like any pantry item, and the low-stock
  alert applies.
- A repeated `clientPurchaseId` returns `200` and changes nothing.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Unknown or wrong-specialty `optionId`; invalid option content; option limit reached; `all` not a boolean; `batches` out of range; recording a batch without a batch option |
| 400 | `invalid_request` | Body is malformed or has unknown fields |
| 403 | `forbidden` | Changing anything without `pantry.edit` |
| 404 | `not_found` | Not a member; unknown or retired specialty ingredient (for changes); unknown household option |
| 409 | `conflict` | The batch's pantry item kept changing concurrently; retry |

## Notifications

Household notifications, such as "Butter is running low". Every member sees
the same list and reads it separately. All routes need `household.view`.
Reading the list or the unread count first applies the pantry's time-based
low-stock check.

`GET /api/v1/households/{householdId}/notifications?unread=true&limit=50&before=<cursor>`

```json
{
  "items": [
    {
      "id": "66e5a1f2c3b4a5d6e7f81001",
      "householdId": "66e5a1f2c3b4a5d6e7f80913",
      "type": "pantry.low",
      "title": "Butter is running low",
      "body": "About 19% left: 4 recipes used 0.81 cup.",
      "subject": { "kind": "pantry_item", "id": "66e5a1f2c3b4a5d6e7f80d01" },
      "read": false,
      "createdAt": "2026-09-18T18:30:00Z"
    }
  ],
  "nextCursor": null
}
```

- Newest first. `limit` is 1–100 (default 50). Pass `nextCursor` as `before`
  for the next page; it's `null` on the last page.
- `unread=true` keeps notifications the caller hasn't read.
- `read` is for the caller. `subject` says what to open: `pantry_item` is a
  pantry item ID.
- `type` is stable. Only `pantry.low` exists today; show unknown types with
  their title and body.

`GET .../notifications/unread-count` returns `{"unreadCount": 3}`.

`POST .../notifications/read` with `{"ids": ["..."]}` (at most 100) or
`{"all": true}` marks them read for the caller and returns
`{"unreadCount": 0}`. Unknown IDs are ignored.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | `limit` out of range; malformed `before`; `unread` not a boolean; neither or both of `ids` and `all`; empty or too many IDs |
| 400 | `invalid_request` | Malformed body or unknown field |
| 404 | `not_found` | Not a member of the household |

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

## Ratings

Each household member can rate a recipe once: a `score` from 1 to 5, an
optional `comment` of up to 500 characters, and `tags` from a fixed list.
Rating again replaces your rating. Ratings are personal, so `household.view`
is enough to rate, change, or remove your own, and every member can see
everyone's ratings.

| Tag | Meaning |
| --- | --- |
| `make-again` | Would happily have it again |
| `never-again` | Don't plan it again (can't be combined with `make-again`) |
| `kid-favorite` | The kids loved it |
| `kids-disliked` | The kids didn't eat it |
| `too-spicy`, `too-bland` | Seasoning was off |
| `too-much-work` | Not worth the effort |
| `great-leftovers` | Good the next day |

Anything else goes in `comment`. Duplicate tags are ignored, and tags are
returned in the order above.

### Rate

`PUT /api/v1/households/{householdId}/recipes/{recipeId}/rating`

```json
{ "score": 4, "comment": "Less chili next time", "tags": ["make-again", "too-spicy"] }
```

It returns your rating (`200`). `comment` is `""` and `tags` is `[]` when
there are none.

```json
{
  "recipeId": "66e5a1f2c3b4a5d6e7f80915",
  "userId": "66e5a1f2c3b4a5d6e7f80912",
  "score": 4,
  "comment": "Less chili next time",
  "tags": ["make-again", "too-spicy"],
  "createdAt": "2026-09-14T19:00:00Z",
  "updatedAt": "2026-09-15T18:00:00Z"
}
```

`DELETE` on the same path removes your rating and returns `204`, also when
you had none.

### List

`GET /api/v1/households/{householdId}/recipes/{recipeId}/ratings` returns
every member's rating, most recently updated first, and the household
aggregate. It isn't paginated: there is at most one rating per member.

```json
{
  "householdRating": { "average": 4.5, "count": 2 },
  "items": [
    { "recipeId": "66e5…", "userId": "66e5…", "displayName": "Ada", "score": 4, "comment": "", "tags": [], "createdAt": "…", "updatedAt": "…" }
  ]
}
```

`householdRating.average` is rounded to two decimals and is `null` when
nobody has rated the recipe. Recipe lists and details carry the same
`householdRating`, plus `myRating` (your rating, or `null`).

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Score outside 1–5, comment over 500 characters, unknown tag, or `make-again` with `never-again` |
| 400 | `invalid_request` | Malformed body, unknown field, or a non-integer score |
| 404 | `not_found` | Not a member of the household, or the recipe isn't the household's |

Each change records a `recipe.rated` or `recipe.unrated` event (below). A
failure to record the event is logged and never fails the rating.

## Events

DinnerOS keeps an append-only history of household behavior for the
recommendation engine ([autopilot.md](autopilot.md#signals-available-today)).
The server records what it observes itself. The app sends only what it alone
can observe.

| Type | Recorded by | `recipeId` | Payload |
| --- | --- | --- | --- |
| `recipe.viewed` | app | required | `{surface?}`: `detail`, `plan`, `search`, `recommendation` |
| `recipe.cooked` | app | required | `{entryId?, date?, servings?}` |
| `recipe.skipped` | app | required | `{entryId?, date?, reason?}`: `no-time`, `ate-out`, `missing-ingredients`, `not-in-the-mood`, `other` |
| `grocery.item_checked` | app | — | `{ingredientId?, name?, checked}` (`ingredientId` or `name` required) |
| `recipe.rated` | server (ratings) | required | `{score, previousScore?, tags?}` (comments are never copied) |
| `recipe.unrated` | server (ratings) | required | `{previousScore}` |
| `recipe.planned` | server (planning) | required | `{entryId?, day?, date?, servings?, origin?}` |
| `recipe.unplanned` | server (planning) | required | `{entryId?, day?, date?}` |
| `import.completed` | server (recipe import) | — | `{source, created, updated, unchanged, rejected}` |

`date` is `YYYY-MM-DD`, `day` is `mon`–`sun`, and `servings` is 1–12.

### Send events

`POST /api/v1/households/{householdId}/events`

```json
{
  "events": [
    {
      "clientEventId": "0d8f6c1e-2c1a-4c55-9d7e-3f0f1b6a2e11",
      "type": "recipe.cooked",
      "recipeId": "66e5a1f2c3b4a5d6e7f80915",
      "week": "2026-W38",
      "occurredAt": "2026-09-15T01:30:00Z",
      "payload": { "entryId": "66e5a1f2c3b4a5d6e7f80c01", "date": "2026-09-14", "servings": 2 }
    }
  ]
}
```

```json
{ "accepted": 1, "duplicates": 0, "rejected": [] }
```

- A batch holds 1–100 events. Only the four app types above are accepted.
- The user and household come from the request. The body can't set them, or
  the `source`.
- Each event is validated on its own. Invalid events are listed in `rejected`
  as `{index, message}`, and the rest are stored. After a `200`, drop the
  whole batch from the device queue: retrying a rejected event can't succeed.
- `clientEventId` (optional, at most 64 characters, unique per user) makes
  retries safe. An event whose ID was already stored counts in `duplicates`.
- `occurredAt` is required and must be within the last 30 days and no more
  than 5 minutes in the future. `week` is optional (`2026-W38`). Payloads are
  at most 1024 bytes, and unknown payload fields are rejected.
- `recipeId` must be one of the household's recipes.
- Requests are rate limited per user: a burst of 30 batches, then one batch
  every 10 seconds (`429 rate_limited`). Send events in batches, for example
  when the app goes to the background, rather than one request per event.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | No events, or more than 100 |
| 400 | `invalid_request` | Malformed body or an unknown field on the batch or an event |
| 404 | `not_found` | Not a member of the household |
| 413 | `payload_too_large` | Body over `HTTP_MAX_BODY_BYTES` |
| 429 | `rate_limited` | Too many batches from this user |
