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
| 409 | `conflict`, `last_admin` |
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
| … | plans, grocery, events | | 5–9 | planned |

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
