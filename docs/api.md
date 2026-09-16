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
  - `GET /`: a short home page (app name, one-line description, privacy link,
    contact email). Google's OAuth consent screen lists it as the home page.
    chi matches `/` exactly, so it shadows no other route.
  - `GET /privacy`: the privacy policy, linked from the app's sign-in screen and
    Household tab, TestFlight, and Google's consent screen. Both pages are
    static HTML with no script (`default-src 'none'`, style by hash).

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
| DELETE | `/api/v1/me` → `204` ([delete account](#delete-account)) | bearer | 7 | ✅ |
| PUT | `/api/v1/me/device-tokens` `{token, environment, platform?}` → device token | bearer | 7 | ✅ |
| DELETE | `/api/v1/me/device-tokens` `{token}` → `204` | bearer | 7 | ✅ |
| POST | `/api/v1/households` `{name, timeZone, defaultServings?}` → `201 {household, membership}`. When `STARTER_RECIPES_HOUSEHOLD_ID` is set, the new household's recipes are copied from that household; the response waits up to 3 s for the copy (typically under a second), and a slower copy finishes in the background | bearer | 3 | ✅ |
| GET | `/api/v1/households` → `{items: [{household, role, permissions}]}` | bearer | 3 | ✅ |
| GET | `/api/v1/households/{householdId}` → `{household, members, role, permissions}` | `household.view` | 3 | ✅ |
| PATCH | `/api/v1/households/{householdId}` `{name?, timeZone?, defaultServings?, orderDay?}` → household | `household.update` | 3 | ✅ |
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
| GET | `/api/v1/households/{householdId}/recipes/import-reviews` `?status=open\|all&limit` → `{items}`, oldest first | `recipes.import` | 4 | ✅ |
| GET | `/api/v1/households/{householdId}/plans` `?from&to` → `{items: [{week, startDate, status, entryCount, updatedAt}]}` | `household.view` | 6 | ✅ |
| GET | `/api/v1/households/{householdId}/plans/{week}` → plan (an empty draft if unplanned) | `household.view` | 6 | ✅ |
| POST | `/api/v1/households/{householdId}/plans/{week}/entries` `{recipeId, day?, servings, note?}` → `201 {entry, plan}` | `plan.edit` | 6 | ✅ |
| PATCH | `/api/v1/households/{householdId}/plans/{week}/entries/{entryId}` `{day?, servings?, note?}` → plan | `plan.edit` | 6 | ✅ |
| DELETE | `/api/v1/households/{householdId}/plans/{week}/entries/{entryId}` → `204` | `plan.edit` | 6 | ✅ |
| PUT | `/api/v1/households/{householdId}/plans/{week}/status` `{status}` → plan | `plan.edit` | 6 | ✅ |
| GET | `/api/v1/households/{householdId}/plans/{week}/grocery` → `{week, status, pantryApplied, categories, skipped, skippedItems}` | `household.view` | 6 | ✅ |
| GET | `/api/v1/households/{householdId}/grocery-skips` → `{items}` | `household.view` | 6 | ✅ |
| POST | `/api/v1/households/{householdId}/grocery-skips` `{ingredientKey, name?, scope, week?}` → `201` skip, or `200` when it replaced the ingredient's skip | `plan.edit` | 6 | ✅ |
| DELETE | `/api/v1/households/{householdId}/grocery-skips/{skipId}` → `204` | `plan.edit` | 6 | ✅ |
| GET | `/api/v1/households/{householdId}/recipes/{recipeId}/customizations` `?servings&entryId&week` → `{recipeId, servings, groups}` | `household.view` | 6 | ✅ |
| PUT | `/api/v1/households/{householdId}/plans/{week}/entries/{entryId}/customization` `{selections}` → plan | `plan.edit` | 6 | ✅ |
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
| GET | `/api/v1/households/{householdId}/specialty-ingredients/settings` → `{strategy, updatedBy, updatedAt, options}` | `household.view` | 7 | ✅ |
| PUT | `/api/v1/households/{householdId}/specialty-ingredients/settings` `{strategy}` → settings | `pantry.edit` | 7 | ✅ |
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
| GET | `/api/v1/households/{householdId}/autopilot/profile` → profile (defaults when never saved) | `household.view` | 10 | ✅ |
| PUT | `/api/v1/households/{householdId}/autopilot/profile` profile sections → profile (sections left out reset to defaults) | `plan.edit` | 10 | ✅ |
| PATCH | `/api/v1/households/{householdId}/autopilot/profile` `{taste?, restrictions?, schedule?, cookTime?, novelty?, equipment?, weekdayRules?}` → profile | `plan.edit` | 10 | ✅ |
| GET | `/api/v1/households/{householdId}/autopilot/profile/history` `?limit` → `{items}` (newest first) | `household.view` | 10 | ✅ |
| GET | `/api/v1/households/{householdId}/autopilot/vocabulary` → `{cuisines, tags, proteins, diets, allergens, equipment, novelty, timeBands, frequencies, days, catalogRecipeCount, limits}` | `household.view` | 10 | ✅ |
| GET | `/api/v1/households/{householdId}/autopilot/recipe-overrides` → `{items}` | `household.view` | 10 | ✅ |
| GET | `/api/v1/households/{householdId}/autopilot/recipes/{recipeId}/attributes` → attributes | `household.view` | 10 | ✅ |
| PUT | `/api/v1/households/{householdId}/autopilot/recipes/{recipeId}/override` `{methods: {method: true/false/null}}` → attributes | `plan.edit` | 10 | ✅ |
| GET | `/api/v1/households/{householdId}/autopilot/weeks/{week}/context` → week context | `household.view` | 10 | ✅ |
| PUT | `/api/v1/households/{householdId}/autopilot/weeks/{week}/context` `{skip?, busy?, mealsPerWeek?, maxMinutes?, servings?, days?, note?}` → week context | `plan.edit` | 10 | ✅ |
| DELETE | `/api/v1/households/{householdId}/autopilot/weeks/{week}/context` → `204` | `plan.edit` | 10 | ✅ |
| POST | `/api/v1/households/{householdId}/autopilot/weeks/{week}/generate` `{avoidPrevious?}` → `201` proposal | `plan.edit` | 10 | ✅ |
| GET | `/api/v1/households/{householdId}/autopilot/weeks/{week}/proposal` → proposal | `household.view` | 10 | ✅ |
| POST | `/api/v1/households/{householdId}/autopilot/weeks/{week}/proposal/slots/{slotId}/swap` `{version}` → proposal | `plan.edit` | 10 | ✅ |
| POST | `/api/v1/households/{householdId}/autopilot/weeks/{week}/proposal/accept` `{version, excludeSlotIds?}` → `{proposal, plan, added, skipped}` | `plan.edit` | 10 | ✅ |
| POST | `/api/v1/households/{householdId}/autopilot/weeks/{week}/proposal/reject` `{version}` → proposal | `plan.edit` | 10 | ✅ |
| GET | `/api/v1/shopping/providers` → `{items: [{key, name, affiliateTracked, capabilities}]}` | bearer | 8a | ✅ |
| GET | `/api/v1/households/{householdId}/shopping/settings` → `{provider, storeId, updatedBy, updatedAt}` | `household.view` | 8a | ✅ |
| PUT | `/api/v1/households/{householdId}/shopping/settings` `{provider, storeId?}` → settings | `shopping.edit` | 8a | ✅ |
| GET | `/api/v1/households/{householdId}/shopping/{provider}/preferences` → `{items}` | `household.view` | 8a | ✅ |
| GET | `/api/v1/households/{householdId}/shopping/{provider}/preferences/{ingredientKey}` → saved product | `household.view` | 8a | ✅ |
| PUT | `/api/v1/households/{householdId}/shopping/{provider}/preferences/{ingredientKey}` `{productUrl or productId, displayName, packageSize?, ingredientName?}` → `201` saved product, or `200` when replaced | `shopping.edit` | 8a | ✅ |
| DELETE | `/api/v1/households/{householdId}/shopping/{provider}/preferences/{ingredientKey}` → `204` | `shopping.edit` | 8a | ✅ |
| POST | `/api/v1/households/{householdId}/plans/{week}/shopping/{provider}/match` `{lines?, checkedOffKeys?, excludeKeys?}` → proposal (not stored) | `household.view` | 8a | ✅ |
| POST | `/api/v1/households/{householdId}/plans/{week}/shopping/{provider}/handoffs` `{lines?, checkedOffKeys?, excludeKeys?}` → `201` handoff | `shopping.edit` | 8a | ✅ |
| GET | `/api/v1/households/{householdId}/shopping/handoffs` `?week&status&limit` → `{items}` | `household.view` | 8a | ✅ |
| GET | `/api/v1/households/{householdId}/shopping/handoffs/{handoffId}` → handoff | `household.view` | 8a | ✅ |
| POST | `/api/v1/households/{householdId}/shopping/handoffs/{handoffId}/confirm` `{all}` or `{lines: [{lineId, packages?}], skipRest?}` → `{handoff, purchases}` | `pantry.edit` | 8a | ✅ |
| GET | `/api/v1/shopping/catalog` `?q` → `{items: [{key, name, kind, status, aliases, note, requestedByHousehold, requests}]}` | bearer | 8a | ✅ |
| GET | `/api/v1/households/{householdId}/shopping/requests` → `{items}` | `household.view` | 8a | ✅ |
| POST | `/api/v1/households/{householdId}/shopping/requests` `{key or name, note?}` → `201` `{request}`, or `200` when the note is updated | `household.view` | 8a | ✅ |
| DELETE | `/api/v1/households/{householdId}/shopping/requests/{requestId}` → `204` | `household.view` | 8a | ✅ |
| GET | `/api/v1/households/{householdId}/shopping/weeks/{week}/order` → `{week, orderDay, dueOn, due, remind, ordered, orderedBy, orderedAt}` | `household.view` | 8a | ✅ |
| PUT | `/api/v1/households/{householdId}/shopping/weeks/{week}/order` `{ordered}` → order reminder | `shopping.edit` | 8a | ✅ |
| … | saved grocery lists, product search, other providers | | 8 | planned |

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
      "cookMinutes": 30,
      "timesOrdered": 3,
      "lastOrderedWeek": "2026-W30",
      "isAddon": false,
      "tags": ["Quick"],
      "calories": 690,
      "proteinGrams": 36,
      "timeBand": "medium",
      "householdRating": { "average": 4.5, "count": 2 },
      "myRating": null
    }
  ],
  "nextCursor": "eyJzIjoibmFtZSIsIm4iOiJCZWVmIFRhY29zIiwiaSI6IjY2ZTUuLi4ifQ"
}
```

`cookMinutes` is the effective cook time: the larger of `prepMinutes` and
`totalMinutes`, because sources report them inconsistently (a total smaller
than the prep time, or only a prep time). It is omitted when neither is known.
Show it instead of `totalMinutes`. The recipe detail has it too.

`calories` (kcal) and `proteinGrams` are per serving, read from the recipe's
nutrition list; names and units are matched case-insensitively ("Calories",
"Energy (kcal)", "Protein"), and energy reported only in kJ is converted.
`timeBand` is `quick`, `medium`, or `long` under the household's
[Autopilot cook-time limits](#taste-profile), 20 and 35 minutes by default.
All three are `null` when the recipe doesn't say, and all three appear
wherever a recipe summary does, including [menu](#menu) cards.

Every item carries `householdRating` and `myRating` (see [Ratings](#ratings)).
The recipe detail has the same two fields.

### Get

`GET /api/v1/households/{householdId}/recipes/{recipeId}` returns the full recipe.
Each ingredient carries its catalog `category`. `quantity` is exact (`"1/2"`);
use `quantityValue` only for display. Both are `null` when the source gave no
amount. Array fields are always present, possibly empty.

The detail carries the same `cookMinutes`, `timeBand`, `calories`, and
`proteinGrams` as a summary, plus what only the full recipe has:

- `nutritionPerServing` is the whole per-serving list under the source's own
  names ("Energy (kcal)", "Fat", "Saturated Fat", "Carbohydrate", "Sugar",
  "Dietary Fiber", "Protein", "Cholesterol", "Sodium"). `calories` and
  `proteinGrams` are read from it; show the list itself for everything else.
- `allergens` is the recipe's allergen list, and `difficulty` the source's own
  scale when it gave one.
- Each step has an optional `imageUrl`, and each ingredient line carries the
  catalog ingredient's `imageUrl` when it has one.
- Ingredient lines have no allergens of their own: the
  [import format](import-format.md) reports allergens per recipe, so there is
  nothing per ingredient to return.

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
  "difficulty": 2,
  "cookMinutes": 30,
  "timeBand": "medium",
  "calories": 640,
  "proteinGrams": 36,
  "nutritionPerServing": [
    { "name": "Calories", "amount": 640, "unit": "kcal" },
    { "name": "Protein", "amount": 36, "unit": "g" }
  ],
  "ingredients": [
    {
      "ingredientId": "66e5a1f2c3b4a5d6e7f80a10",
      "name": "Parmesan Cheese",
      "category": "dairy-eggs",
      "imageUrl": "https://img.example.com/parmesan.jpg",
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
  "steps": [{ "index": 1, "text": "Preheat the oven.", "imageUrl": "https://img.example.com/step-1.jpg" }],
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

### Starter library

There is no route for it. A household created while `STARTER_RECIPES_HOUSEHOLD_ID`
is set receives a copy of that household's recipes, and `cmd/seedstarter`
copies them into an existing household ([deployment.md](deployment.md#starter-recipe-library)).
Copied recipes are ordinary recipes of the new household with new IDs: the
same content, images and source links, and catalog ingredients, but no order
history (`timesOrdered` 0, no `lastOrderedWeek`), ratings, or plans. A recipe
the household already has from the same `source` (matched by `sourceRecipeId`
or an alias, as import matches) is skipped.

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
      "recipe": { "id": "66e5a1f2c3b4a5d6e7f80915", "name": "Beef Tacos", "isAddon": false, "imageUrl": "https://img.example.com/beef-tacos.jpg" },
      "day": "tue",
      "date": "2026-09-15",
      "servings": 2,
      "note": "extra lime",
      "addedBy": "66e5a1f2c3b4a5d6e7f80912",
      "addedAt": "2026-09-14T18:30:00Z",
      "origin": "manual"
    },
    {
      "id": "66e5a1f2c3b4a5d6e7f80c02",
      "recipe": { "id": "66e5a1f2c3b4a5d6e7f80916", "name": "Onion Soup", "isAddon": false },
      "day": null,
      "date": null,
      "servings": 4,
      "note": "",
      "addedBy": "66e5a1f2c3b4a5d6e7f80917",
      "addedAt": "2026-09-14T19:05:00Z",
      "origin": "autopilot"
    }
  ],
  "createdAt": "2026-09-14T18:30:00Z",
  "updatedAt": "2026-09-14T19:05:00Z"
}
```

- `day` is `mon`–`sun`, or `null` for "this week, not scheduled". `date` is
  that day's `YYYY-MM-DD`, or `null`.
- `recipe` is a snapshot of the name, image, and `isAddon` taken when the
  entry was added. Use `GET .../recipes/{id}` for details. `isAddon` is true
  for a pairing's add-on (garlic bread alongside the pasta), which is planned
  with a meal and never counted as one, so a client never has to work that out
  from the catalog. Entries added before add-ons were marked read as `false`.
- Entries are in the order they were added.
- `origin` is `manual`, or `autopilot` for entries added by accepting an
  [Autopilot](#autopilot) proposal. Autopilot entries are ordinary entries:
  edit or delete them like any other.
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

### Customize a meal

A recipe's protein can be swapped for another or doubled ("Customize your
meal"). The chosen customization changes the week's grocery list and what
cooking the meal deducts from the pantry. There are no prices.

`GET /api/v1/households/{householdId}/recipes/{recipeId}/customizations`
(`household.view`)

```json
{
  "recipeId": "66e5a1f2c3b4a5d6e7f80915",
  "servings": 2,
  "groups": [
    {
      "ingredientKey": "66e5a1f2c3b4a5d6e7f80a20",
      "ingredientName": "Ground Pork",
      "amountText": "10 ounce",
      "quantity": "10",
      "unit": "oz",
      "imageUrl": "https://img.example.com/ground-pork.png",
      "selectedChoiceId": "original",
      "choices": [
        { "id": "original", "label": "Ground Pork", "ingredientName": "Ground Pork", "amountText": "10 ounce",
          "quantity": "10", "unit": "oz", "imageUrl": "https://img.example.com/ground-pork.png", "kind": "original", "badge": null },
        { "id": "double", "label": "2x Ground Pork", "ingredientName": "Ground Pork", "amountText": "20 ounce",
          "quantity": "20", "unit": "oz", "imageUrl": "https://img.example.com/ground-pork.png", "kind": "double", "badge": "Double portion" },
        { "id": "swap:ground-beef", "label": "Ground Beef", "ingredientName": "Ground Beef", "amountText": "10 ounce",
          "quantity": "10", "unit": "oz", "imageUrl": "https://img.example.com/ground-beef.png", "kind": "swap", "badge": null },
        { "id": "swap:ground-beef:double", "label": "2x Ground Beef", "ingredientName": "Ground Beef", "amountText": "20 ounce",
          "quantity": "20", "unit": "oz", "imageUrl": "https://img.example.com/ground-beef.png", "kind": "swap_double", "badge": "Double portion" }
      ]
    }
  ]
}
```

- **Groups.** Only protein lines get a group, decided by a curated table in
  the API (ground meats, chicken cuts, pork cuts, steaks, sausage, seafood,
  tofu). A line qualifies only when it has an exact weight for the serving
  size, so counts ("2 Pork Chops") and amount-less lines have no group. A
  recipe with no protein lines returns `{"groups": []}`.
- **Choices.** `original` first, then `double`, then each swap followed by its
  double. `kind` is `original`, `double`, `swap`, or `swap_double`, and
  `badge` is `"Double portion"` on the doubled ones (otherwise `null`).
  `id` is what you send back. Swap families: ground meats swap with each other
  plus diced chicken; chicken cuts with each other plus pork chops and tofu;
  pork cuts and steaks with each other plus chicken; sausage with sausage and
  ground meat; seafood with seafood and chicken; tofu with chicken and shrimp.
- **Amounts.** `amountText` is for display ("10 ounce"); `quantity` is exact
  and `unit` is the unit code. Swaps are the same weight unless the table sets
  a ratio for the pair; a double is twice the amount.
- **Serving size.** The recipe's smallest by default, `?servings=N` for one of
  its sizes (400 otherwise), or the entry's when `?entryId=&week=` are given
  (both or neither, 400 otherwise; 404 when that entry isn't this recipe's).
  `servings` wins over the entry's size.
- **`selectedChoiceId`** is the entry's current choice (`"original"` when it
  isn't customized) with `entryId`, and `null` without one.
- **Restrictions.** Choices the household's [Autopilot](#autopilot)
  restrictions rule out — an excluded protein or ingredient, an allergen, or a
  diet such as vegetarian — are dropped. The `original` is always offered, as
  is a choice the entry already has.
- **`imageUrl`** is the catalog ingredient's image when there is one, else
  `null`.

`PUT /api/v1/households/{householdId}/plans/{week}/entries/{entryId}/customization`
(`plan.edit`)

```json
{ "selections": [{ "ingredientKey": "66e5a1f2c3b4a5d6e7f80a20", "choiceId": "swap:ground-beef" }] }
```

Returns the plan, like the other entry mutations. `selections` is required and
replaces the entry's customizations: `[]` resets the meal, and a line you
leave out goes back to the original. Sending `"original"` stores nothing.
Unknown `ingredientKey`s or `choiceId`s (including a choice the restrictions
rule out) are `400 validation_failed`, and a finalized plan is
`409 plan_finalized`, as everywhere else in the planner.

The entry then carries `customizations` (omitted when empty):

```json
"customizations": [{ "ingredientKey": "66e5a1f2c3b4a5d6e7f80a20", "choiceId": "swap:ground-beef", "label": "Ground Beef" }]
```

The week's [grocery list](#grocery-list) buys the chosen ingredient and
amount, with `via` provenance ("Ground Beef instead of Ground Pork in One-Pan
Pork Tacos"), and marking the meal cooked deducts the chosen ingredient from
the pantry. The change is recorded as a `meal.customized`
[event](#events) for Autopilot.

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
- `extras` on an item lists what put it on the list besides a recipe: an
  accepted Autopilot [pairing](#add-on-pairings) shows
  `{"id": "…", "origin": "pairing", "text": "Club crackers for Chicken Noodle Soup"}`.
  It is empty for ordinary items, and the item is aggregated, checked off, and
  handed to a store like any other line.
- `skipped` lists entries that couldn't contribute: `recipeUnavailable` (the
  recipe is no longer in the household) or `servingsUnavailable` (the recipe
  no longer offers that serving size).
- `skippedItems` lists the ingredients the household chose not to buy
  ([Skipped ingredients](#skipped-ingredients)). They are **not** in
  `categories`, so nothing asks anyone to buy them, but each one is a full
  grocery item — amounts, `recipes`, `via` — plus `skipScope` (`week` or
  `always`) and `skipText`. Items in `categories` never carry those two
  fields. This is a different thing from `skipped`, which is about plan
  *entries*, not ingredients.

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
  `batches` batches, each `yield`. `customized`: the line a member
  [customized](#customize-a-meal) — `specialtyName` is the recipe's original
  ingredient and `optionName` the chosen label. `text` is ready to show
  ("Ground Beef instead of Ground Pork in One-Pan Pork Tacos", or "2x Ground
  Pork in …" for a doubled line).
- `batches` has one entry per house-made specialty the week uses. `status` is
  `inPantry` or `make`; `reason` is `enough` or `inStock` (in pantry), or
  `notEnough`, `low`, `out`, `missing` (make). `needed` is the week's total in
  the yield's unit (`null` when a recipe gives no amount), `remaining` the
  pantry estimate. Several batches are asked for when the shortfall exceeds
  one yield.
- `via[].strategy` is set when the household's
  [strategy](#settings-the-household-strategy) picked the option rather than a
  member, and `via[].text` then says so: "Store alternative for Tex-Mex Paste
  in Smoky Pork Tacos (your default)", or "House-made batch to make Southwest
  Spice Blend (makes about 12 tbsp) (your default)". It is empty for an
  explicit choice, whose text is unchanged.
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

## Skipped ingredients

Some ingredients a household will buy, throw away, and resent buying again. A
**skip** leaves one off the grocery list on purpose, with two lifetimes: `week`
("skip once", back next week with no action) and `always` ("skip forever",
until someone resumes it).

A skip is about an **ingredient for a household** — not a product, not one
recipe, and not one line. It is a third state alongside the two that already
exist, and the app should keep them apart:

| State | Means | Where |
| --- | --- | --- |
| `inPantry` | the household **has** it | [pantry](#pantry) |
| checked off | someone **bought** it | the app, per week |
| skipped | the household never **wants** it | here |

Skipping never rewrites a recipe: the recipe still lists the ingredient, and
the week's list reports it in `skippedItems` rather than dropping it, so the
two never disagree about what a meal needs.

`GET /api/v1/households/{householdId}/grocery-skips` (`household.view`)

```json
{
  "items": [
    {
      "id": "66e5a1f2c3b4a5d6e7f80c31",
      "ingredientKey": "name:cilantro",
      "key": "cilantro",
      "name": "Cilantro",
      "scope": "always",
      "week": null,
      "text": "Never buying this",
      "createdBy": "66e5a1f2c3b4a5d6e7f80912",
      "createdAt": "2026-09-15T18:30:00Z",
      "updatedBy": "66e5a1f2c3b4a5d6e7f80912",
      "updatedAt": "2026-09-15T18:30:00Z"
    }
  ]
}
```

`POST /api/v1/households/{householdId}/grocery-skips` (`plan.edit`)
`{ingredientKey, name?, scope, week?}`

- `ingredientKey` is the grocery line's key, as the list gave it: a catalog
  ingredient ID, or `name:<normalized name>`.
- `name` is what to show on the review screen. It is required unless
  `ingredientKey` is a `name:` key, which already carries one.
- `scope` is `week` or `always`. `week` requires `week` (`"2026-W38"`);
  `always` requires it to be absent.
- One ingredient has at most one skip per household, so posting again
  **replaces** it and answers `200` instead of `201`. That is how "skip once"
  becomes "skip forever" — no delete first, and no two skips to reconcile.
- A household may skip at most 500 ingredients. Changing a skip it already has
  still works at the cap.

`DELETE /api/v1/households/{householdId}/grocery-skips/{skipId}` → `204`
(`plan.edit`) resumes the ingredient: it is back on the next list built.

**Matching.** A skip is registered under the key it was made from, under
`name:<normalized name>`, and under the catalog ingredient ID for that name
when the catalog has one — the same three spellings the [pantry](#pantry)
matches. So skipping cilantro from a free-text line also skips a recipe that
reaches cilantro through the catalog.

**Specialty ingredients.** Skips resolve *after* the household's
[specialty choices](#specialty-ingredients) are applied, against the keys the
list actually ends up with. Skipping an ingredient that only appears because a
store alternative calls for it drops that one component and leaves the rest of
the alternative on the list, with `via` still saying where it came from.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Missing `ingredientKey`, unknown `scope`, `week` missing or given for the wrong scope, no usable `name`, or past the 500 cap |
| 400 | `invalid_request` | Body is empty, malformed, has unknown fields, or wrong types |
| 403 | `forbidden` | Skipping or resuming without `plan.edit` |
| 404 | `not_found` | Not a member of the household; no such skip |

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
- `source` is `grocery_list` or `manual`. `provider` is rejected: it's
  recorded from a shopping handoff by
  [`POST .../shopping/handoffs/{handoffId}/confirm`](#confirm-an-order), and
  such purchases carry `provider: {key, handoffId, lineId, productId}` (`null`
  on every other purchase). `house_made` appears on batches recorded through
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

### Settings: the household strategy

Deciding one ingredient at a time doesn't scale, so a household picks one
standing answer. `GET .../specialty-ingredients/settings`:

```json
{
  "strategy": "similar",
  "updatedBy": "66e5…12",
  "updatedAt": "2026-09-15T18:30:00Z",
  "options": [
    { "value": "similar", "label": "Something similar", "description": "Buy something close from the store — quicker, tastes a little different" },
    { "value": "closest", "label": "As close as possible", "description": "Make a jar you reuse across several meals — more work, closest to the original" },
    { "value": "ask", "label": "Ask me each time", "description": "Leave each specialty ingredient on the list until someone picks an option" }
  ]
}
```

- `strategy` is `similar` (the default), `closest`, or `ask`.
  - **`similar`** prefers a store alternative wherever the curated set has
    one, and falls back to a house-made batch when it doesn't.
  - **`closest`** prefers a house-made batch, and falls back to a store
    alternative.
  - **`ask`** applies nothing: every unchosen specialty ingredient stays on
    the list by its own name with `suggestedOptions`, as before this setting
    existed.
- `updatedBy` and `updatedAt` are `null` for a household that never set one.
- `options` is the same three entries every time, in the order to offer them;
  it's there so the app can show the trade-off without hardcoding the copy.
- `PUT .../settings` `{"strategy": "closest"}` sets it (`pantry.edit`) and
  returns the same shape.

The strategy is resolved **when a grocery list is built**, never written to the
household's choices. Changing it changes future lists, and nothing has to be
migrated or re-chosen. A per-ingredient choice always wins over it, and `as_is`
still means "leave the line alone".

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
  "note": "",
  "retired": false,
  "choiceSource": "household",
  "choice": { "source": "household", "optionId": "southwest-spice-blend.batch", "type": "house_made_batch", "optionName": "Southwest spice blend (house blend)", "strategy": null, "chosenBy": "66e5…12", "chosenAt": "2026-09-15T18:30:00Z" },
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
- `choiceSource` says where the current plan comes from, and `choice` matches
  it:
  - `household` — a member chose it. `choice.chosenBy` and `choice.chosenAt`
    are set, and `choice.strategy` is `null`.
  - `strategy` — nobody chose, so the household's strategy picked the option.
    `choice.strategy` is `similar` or `closest`, and `chosenBy`/`chosenAt` are
    `null` because nobody decided it. A member can still override it.
  - `none` — nothing applies (`ask`, or no curated option), and `choice` is
    `null`.
- `choice.type` is `as_is` or the option's type.
- `note` is a short shopping note for the specialty ingredient ("Bottled
  ponzu is in the Asian aisle…"), empty for most. It's there for the ones
  whose store route needs a caveat.
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
| 400 | `validation_failed` | Unknown or wrong-specialty `optionId`; invalid option content; option limit reached; `all` not a boolean; `batches` out of range; recording a batch without a batch option; `strategy` not one of `similar`, `closest`, `ask` |
| 400 | `invalid_request` | Body is malformed or has unknown fields |
| 403 | `forbidden` | Changing anything without `pantry.edit` |
| 404 | `not_found` | Not a member; unknown or retired specialty ingredient (for changes); unknown household option |
| 409 | `conflict` | The batch's pantry item kept changing concurrently; retry |

## Shopping

Phase 8a hands the week's grocery list to Walmart as add-to-cart links, with
no Walmart account connection, API keys, or approval. The API never fetches
Walmart pages ([shopping-providers.md](shopping-providers.md)). The flow:
store setup, save a product per ingredient, match, create a handoff and open
its links, then confirm what was ordered.

`{provider}` is `walmart`. A planned provider that isn't enabled
(`instacart`, `kroger`) returns `503 provider_unavailable`; any other value
returns `404 not_found`.

### Providers and store

`GET /api/v1/shopping/providers` (signed in):

```json
{
  "items": [
    {
      "key": "walmart",
      "name": "Walmart",
      "affiliateTracked": false,
      "capabilities": {
        "handoff": "cart_link", "pasteProductLink": true, "storeId": true,
        "productSearch": false, "productLookup": false, "storeFinder": false, "cartWrite": false, "orderImport": false
      }
    }
  ]
}
```

`affiliateTracked` is true only when the server has the Impact IDs
(`WALMART_IMPACT_*`). Show "DinnerOS may earn a commission" next to the
handoff button only then.

`GET .../shopping/settings` (`household.view`) and `PUT .../shopping/settings`
(`shopping.edit`):

```json
{ "provider": "walmart", "storeId": "5435", "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-15T18:30:00Z" }
```

All fields are `null` until someone sets them. `PUT` takes `{"provider":
"walmart", "storeId": "5435"}`. `storeId` is the store number the member
types (1–6 digits; leading zeros are dropped). `null` or omitted means no
store, and an empty string is `400`. Links carry the store only while the
settings' provider matches.

### Saved products

`GET .../shopping/{provider}/preferences` (`household.view`) returns
`{"items": [...]}` ordered by ingredient name. `GET`, `PUT` (`shopping.edit`),
and `DELETE` (`shopping.edit`) `.../preferences/{ingredientKey}` read, save,
and remove one. `{ingredientKey}` is the grocery line's `ingredientKey`,
percent-encoded (`name:red%20onion`).

```json
{
  "productUrl": "https://www.walmart.com/ip/Great-Value-80-20-Ground-Beef-1-lb/123456789?classType=REGULAR",
  "displayName": "Great Value 80/20 ground beef",
  "packageSize": { "quantity": "16", "unit": "oz" }
}
```

```json
{
  "id": "66e5a1f2c3b4a5d6e7f80e11",
  "provider": "walmart",
  "ingredientKey": "66e5a1f2c3b4a5d6e7f80a14",
  "ingredientId": "66e5a1f2c3b4a5d6e7f80a14",
  "ingredientName": "Ground Beef",
  "productId": "123456789",
  "productUrl": "https://www.walmart.com/ip/123456789",
  "displayName": "Great Value 80/20 ground beef",
  "packageSize": { "quantity": "16", "quantityValue": 16, "unit": "oz", "text": "16 oz" },
  "createdBy": "66e5a1f2c3b4a5d6e7f80912",
  "createdAt": "2026-09-15T18:30:00Z",
  "updatedBy": "66e5a1f2c3b4a5d6e7f80912",
  "updatedAt": "2026-09-15T18:30:00Z"
}
```

- Send `productUrl` (the pasted link) or `productId` (the numeric item ID),
  not both. The item ID is read from the text: `http(s)://walmart.com` or
  `www.walmart.com`, path `/ip/<id>` or `/ip/<name>/<id>`, any query or
  fragment. Other hosts, short links (`walmrt.us`), search or cart pages, and
  text around the link are `400`. On iOS, extract the URL from shared text
  before sending it.
- `productUrl` in responses is rebuilt from the ID.
- `displayName` (at most 100 characters) is read from the link's own slug
  when it's omitted or empty: `/ip/Garlic-Bulb-Fresh-Whole-Each/123` saves
  `Garlic Bulb Fresh Whole Each`, trimmed to 100 characters. The page is
  never fetched, so the name is only as good as the slug, and a link with no
  slug (or a bare `productId`) still requires `displayName`. Hyphens in the
  slug become spaces, so the original punctuation doesn't survive.
- `packageSize` is optional: a positive exact quantity and a unit code
  (`oz`, `floz`, `lb`, `g`, `ml`, `count`, `can`, …). When it's `null` or
  omitted, an unambiguous size is read from the slug — a trailing `-15-oz`,
  `-2-lb`, `-64-fl-oz`, `-12-Count`, or `-Each` (1 ct). A decimal written as
  two numbers (`-2-5-lb`) is ambiguous and yields no size rather than a wrong
  one. With no size at all, every line with that product is flagged "check
  amount". Both derived values are defaults to confirm, not facts from
  Walmart: anything sent explicitly wins.
- `coverage` is optional: `per_week`, `per_amount`, or omitted to follow the
  ingredient's grocery category (see [Match and hand off](#match-and-hand-off)).
- `ingredientName` is optional: it defaults to the catalog name, or the
  normalized name after `name:`.
- `201` creates, `200` replaces (keeping `id`, `createdBy`, `createdAt`). At
  most 1,000 saved products per store.

### Match and hand off

`POST .../plans/{week}/shopping/{provider}/match` (`household.view`) matches
the week's list and stores nothing. `POST .../plans/{week}/shopping/{provider}/handoffs`
(`shopping.edit`) does the same and stores the result (`201`). Both take:

```json
{
  "lines": [{ "ingredientKey": "66e5a1f2c3b4a5d6e7f80a14", "packages": 2 }],
  "checkedOffKeys": ["name:cilantro"],
  "excludeKeys": []
}
```

Every field is optional; send `{}` for the defaults. Without `lines`, the
candidates are the list's `toBuy` lines. With `lines`, exactly those lines
are candidates, whatever their status, and `packages` (1–99) overrides the
computed count. `checkedOffKeys` and `excludeKeys` are left out.

```json
{
  "id": "66e5a1f2c3b4a5d6e7f80b01",
  "provider": "walmart",
  "week": "2026-W38",
  "storeId": "5435",
  "status": "open",
  "lines": [
    {
      "id": "l1",
      "ingredientKey": "66e5a1f2c3b4a5d6e7f80a14",
      "ingredientId": "66e5a1f2c3b4a5d6e7f80a14",
      "name": "Ground Beef",
      "category": "meat-seafood",
      "amounts": [{ "quantity": "9/4", "quantityValue": 2.25, "unit": "lb", "text": "2 ¼ lb" }],
      "quantityText": "2 ¼ lb",
      "unquantified": false,
      "groceryStatus": "toBuy",
      "product": {
        "productId": "123456789",
        "displayName": "Great Value 80/20 ground beef",
        "productUrl": "https://www.walmart.com/ip/123456789",
        "packageSize": { "quantity": "16", "quantityValue": 16, "unit": "oz", "text": "16 oz" }
      },
      "computedPackages": 3,
      "packages": 3,
      "packagesOverridden": false,
      "checkAmount": false,
      "reason": null,
      "reasonText": null,
      "coverageText": "3 × 16 oz covers 36 oz",
      "confirmation": { "status": "pending", "packages": null, "purchaseId": null, "confirmedBy": null, "confirmedAt": null, "skippedBy": null, "skippedAt": null }
    }
  ],
  "excluded": [
    {
      "ingredientKey": "name:flour tortillas", "ingredientId": null, "name": "Flour Tortillas", "category": "bakery",
      "amounts": [{ "quantity": "6", "quantityValue": 6, "unit": "count", "text": "6" }], "quantityText": "6",
      "unquantified": false, "groceryStatus": "toBuy", "reason": "no_product", "text": "Choose a Walmart product"
    }
  ],
  "cartLinks": [
    { "url": "https://www.walmart.com/sc/cart/addToCart?items=123456789_3&storeId=5435", "lineIds": ["l1"], "itemCount": 1 }
  ],
  "affiliateTracked": false,
  "createdBy": "66e5a1f2c3b4a5d6e7f80912",
  "createdAt": "2026-09-15T18:30:00Z",
  "updatedAt": "2026-09-15T18:30:00Z"
}
```

A match has the same shape without `id`, `status`, `createdBy`, `createdAt`,
and `updatedAt`, and every `confirmation` is `null`.

**Package counts** (`computedPackages`) are exact:

| Line amount vs package size | Packages | `reason` |
| --- | --- | --- |
| Converts (same unit, or both weights or both volumes): 20 oz for 16 oz | Total ÷ size, rounded up: 2 | `null` |
| Several amounts: 1 cup + 3 tbsp for 8 fl oz | Converted and summed, rounded up: 2 | `null` |
| Discrete units only convert to themselves: 2 cans for 1 can, 13 count for 12 count | 2, 2 | `null` |
| Doesn't convert, `coverage: per_amount`: 4 cloves for 3 count, 1 cup for 5 lb | 1 | `unit_not_convertible` |
| Doesn't convert, `coverage: per_week`: 4 cloves for a 1 ct bulb | 1, `coversWeek: true` | `null` |
| Partly converts: 1 count + 40 oz for 32 oz | What converts, at least 1: 2 | `unit_not_convertible` |
| No saved package size | 1 | `no_package_size` |
| Unquantified ("to taste") | 1 | `null` |
| More than 99 | 99 | `package_count_capped` |

`checkAmount` is true whenever `reason` is set; show `reasonText`
("Check amount: 4 cloves doesn't convert to a 3 ct package") and let the
member change the count. Flags never block the handoff.

**Coverage** (`coverage`) says how one package maps to a week's need, and is
the only thing that changes the table above. It affects exactly one row: a
need in a unit that can't be measured against the package at all.

| Rule | What it does |
| --- | --- |
| `per_week` | One package is assumed to cover the week, with no flag: a recipe asking for a clove and a bulb sold by the each buys one bulb, not one per clove. `coverageText` reads "1 × 1 ct covers this week (4 cloves)". |
| `per_amount` | Always count by exact measure, flagging what can't be measured. |

A saved product's `coverage` sets it; when that is empty the rule comes from
the line's grocery category — `per_week` for `produce`, `meat-seafood`,
`dairy-eggs`, `bakery` and `deli`, which are bought fresh and assumed not to
carry over, and `per_amount` for everything else, because rice and spices do
carry over. A measurable need is never collapsed: 2 ¼ lb of beef against
16 oz packages is 3 packages under either rule.

Nothing carries produce forward between weeks, and nothing needs to: each
week's list is rebuilt from that week's recipes and the pantry, so the
following week asks for the bulb again. The resolved rule is stored on the
handoff line, so reading an old handoff back recomputes the count it was
created with even if the household changes the rule afterwards.

**Exclusions** (`excluded[].reason`), each with display `text`:

| Reason | When |
| --- | --- |
| `in_pantry` | The pantry has it (`inPantry`) and `lines` didn't select it |
| `pantry_hint` | Probably have it (`pantryHint`) and `lines` didn't select it |
| `house_made` | A house-made specialty batch in the pantry (never bought) |
| `checked_off` | Listed in `checkedOffKeys` |
| `excluded` | Listed in `excludeKeys` |
| `not_selected` | `lines` was sent without it |
| `no_product` | No saved product for the ingredient: "Choose a Walmart product" |
| `not_on_list` | A `lines` key that isn't on the week's list (only `ingredientKey` is set) |

**Cart links**: `https://www.walmart.com/sc/cart/addToCart?items=ID_QTY,ID&storeId=N`.
A quantity of 1 has no suffix, lines that share a product are merged into one
item, and every link carries the store. Items stay in aisle order. A new link
starts past 2,000 characters or 40 products, so a big week can have several:
open them one after another, each fills the same Walmart cart. With affiliate
tracking each URL is a `goto.walmart.com` link wrapping that one. Creating a
handoff with no line to add is `400`.

`GET .../shopping/handoffs` (`household.view`) lists handoffs newest first:
`?week=2026-W38`, `?status=open` (some line pending) or `done`, `?limit=`
(1–50, default 20). `GET .../shopping/handoffs/{handoffId}` returns one.

### Confirm an order

`POST .../shopping/handoffs/{handoffId}/confirm` (`pantry.edit`) records what
was ordered. Walmart doesn't report orders, so the member confirms:

```json
{ "lines": [{ "lineId": "l1", "packages": 2 }, { "lineId": "l3" }], "skipRest": true }
```

or `{"all": true}` for every line that isn't skipped, at its `packages`.
`packages` (1–99) defaults to the line's. `skipRest` marks the other pending
lines `skipped` (not ordered). `{"skipRest": true}` alone says nothing was
ordered.

```json
{
  "handoff": { "id": "66e5a1f2c3b4a5d6e7f80b01", "status": "done", "…": "…" },
  "purchases": [
    {
      "lineId": "l1",
      "ingredientKey": "66e5a1f2c3b4a5d6e7f80a14",
      "created": true,
      "purchase": {
        "id": "66e5a1f2c3b4a5d6e7f80f09", "source": "provider", "quantity": "2", "unit": "package",
        "unitSize": { "per": "package", "quantity": "16", "quantityValue": 16, "unit": "oz" }, "week": "2026-W38",
        "provider": { "key": "walmart", "handoffId": "66e5a1f2c3b4a5d6e7f80b01", "lineId": "l1", "productId": "123456789" },
        "…": "…"
      },
      "item": { "id": "66e5a1f2c3b4a5d6e7f80d01", "status": "in_stock", "…": "…" }
    }
  ]
}
```

Each confirmed line is one pantry purchase with `source: provider`, written
from the stored line (the app can't send amounts):

| Saved package size | Purchase |
| --- | --- |
| A weight or volume (16 oz) | `quantity: packages`, `unit: package`, `unitSize` = the size, so recipe use deducts exactly |
| A discrete unit (12 count, 1 can) | The exact amount: 2 × 12 count = `24 count` |
| None | `quantity: packages`, `unit: package` |

- The item is added if needed (by catalog ID, or the `name:` name), set
  `in_stock`, and starts a usage cycle, as a grocery check-off does.
- **Idempotent per line:** a confirmed line keeps its first purchase. Sending
  it again (by anyone, with any `packages`) returns that purchase with
  `created: false` and changes nothing. A skipped line can still be confirmed.
- After confirming, treat those grocery lines as checked off: don't also ask
  "Add to pantry?", which would record a second purchase.

### Order reminders

A household picks the weekday it means to order (`orderDay` on the household),
and gets one reminder a week from that day until someone says they ordered
([shopping-providers.md](shopping-providers.md#order-reminders)).

`GET .../shopping/weeks/{week}/order` (`household.view`):

```json
{
  "week": "2026-W38", "orderDay": "thu", "dueOn": "2026-09-17",
  "due": true, "remind": true,
  "ordered": false, "orderedBy": null, "orderedAt": null
}
```

- **Everything but `ordered` is derived on read.** No reminder is stored and
  nothing is scheduled, so there is nothing to drift or fire twice. `due` means
  `dueOn` has arrived and the week hasn't ended, both in the household's time
  zone; `remind` is `due` and not `ordered`, and is the only thing an app
  should show a reminder for.
- **A reminder belongs to its own week.** It stops when the week ends, so an
  unmarked past week goes quiet instead of piling up.
- Without an `orderDay`, `orderDay` and `dueOn` are `null` and `due` is false.

`PUT .../shopping/weeks/{week}/order` with `{"ordered": true}`
(`shopping.edit`) marks the week and returns the new state; `false` takes it
back and the reminder returns.

- **Only a person marks a week.** Nothing infers it — a Walmart hand-off is not
  proof an order was placed — so marking is never automatic, and it is undoable
  because a mis-tap must not leave a household un-remindable for the week.
- Marking is idempotent and keeps the first member and time. It also marks the
  caller's copy of the `shopping.order_due` notification read.

### Request a store

The Shop tab supports Walmart only, so members can say what they'd use
instead, and the counts decide what gets built next
([shopping-providers.md](shopping-providers.md#demand-signal)).

`GET /api/v1/shopping/catalog` (signed in, **not** household scoped):

```json
{
  "items": [
    {
      "key": "kroger", "name": "Kroger", "kind": "grocer", "status": "researched",
      "aliases": ["kroger co", "krogers"],
      "note": "Public API with cart write; needs customer sign-in",
      "requestedByHousehold": false, "requests": 3
    }
  ]
}
```

- `status` is what
  [shopping-providers.md](shopping-providers.md#demand-signal) concluded, not
  live data: `available` (Walmart today), `researched` (that document
  assessed it), or `unsupported` (no integration and no research yet). Only
  `available` can take a list.
- `kind` is `grocer`, `delivery`, `warehouse`, or `other`.
- `?q=` is a case-insensitive prefix of the name, the key, or an alias
  (`krog`, `fry's`, `quality food`), at most 100 characters. Without `q` the
  whole list comes back, ordered by status then name.
- `requests` counts the households that asked, across all of DinnerOS;
  `requestedByHousehold` is whether the caller's own household did. The route
  isn't household scoped, so `requestedByHousehold` uses the caller's first
  household and is `false` for a user who belongs to none.

`GET` and `POST .../shopping/requests` and `DELETE
.../shopping/requests/{requestId}` (all `household.view`) read, record, and
withdraw one household's requests:

```json
{ "request": { "id": "66e5a1f2c3b4a5d6e7f80d01", "key": "kroger", "name": "Kroger",
               "status": "researched", "note": "closest to us",
               "requestedBy": "66e5a1f2c3b4a5d6e7f80912", "requestedAt": "2026-09-15T18:30:00Z" } }
```

- Send `{"key": "kroger"}` for a store in the catalog, or `{"name": "Some
  Local Market"}` for one that isn't — not both. `note` is optional, at most
  280 characters.
- A typed `name` is trimmed, its spaces collapsed, and matched against
  catalog names, keys, and aliases first, so `"frys"` records as Fry's. Only
  a name matching nothing becomes its own entry, keyed `some-local-market`
  and title cased for display. It does **not** join the catalog.
- **Idempotent per store:** `201` the first time, `200` when a later request
  updates the note. The first requester and time are kept, whoever asks again.
- `GET` returns `{"items": [...]}` newest first. `DELETE` is `204`.
- A household can ask for at most 25 stores; a new one past that is `409`.
  Updating a note still works at the cap.

### Contract notes for iOS

- **Search server-side.** Send `?q=` as the member types (debounced) instead
  of fetching the catalog once and filtering on the device: aliases mean
  "frys" and "quality food" match entries whose names don't contain the typed
  text.
- **Never offer a store as usable.** `status` is research, not capability.
  Only `available` belongs in the store picker; the rest belong in "request a
  store", with `note` as the explanation if you show one.
- **Drive the button from `requestedByHousehold`** ("Request" vs
  "Requested"). A second `POST` is safe — it just updates the note — so
  retries and double taps are fine.
- **Prefer `key`.** Send `name` only when the search returned nothing, so a
  member tapping a catalog row never creates a duplicate free-text entry.
- **`requests` is our demand signal**, not a promise about what ships next.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Bad link, product ID, name, package size, key, store, week, `status`, or `limit`; both or neither of `productUrl`/`productId`; no line to hand off; confirm with neither `all`, `lines`, nor `skipRest`, both `all` and `lines`, an unknown or repeated `lineId`, or `packages` outside 1–99; a store request with both or neither of `key`/`name`, a `key` that isn't in the catalog, a `name` with no letter or digit, or a `q`, `name`, or `note` that is too long |
| 403 | `forbidden` | Settings, saved products, handoffs, or marking a week ordered without `shopping.edit`; confirm without `pantry.edit` |
| 404 | `not_found` | Not a member; unknown provider; no saved product, handoff, or store request |
| 409 | `conflict` | Another request is confirming the same line, or a pantry item kept changing; retry. Also a household past its 25-store request cap |
| 503 | `provider_unavailable` | A planned provider that isn't enabled |

## Notifications

Household notifications, such as "Butter is running low". Every member sees
the same list and reads it separately. All routes need `household.view`.
Reading the list or the unread count first applies the pantry's time-based
low-stock check and the grocery order reminder. The hourly push sweep applies
the same checks for every household and pushes new notifications to members'
registered devices ([Push device tokens](#push-device-tokens)); reading or
marking notifications never depends on push.

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
- `type` is stable. `pantry.low` and `shopping.order_due` exist today; show
  unknown types with their title and body. A `shopping.order_due` subject is
  `{kind: "shopping_week", id: "2026-W38"}`: the week to open in Shop.

`GET .../notifications/unread-count` returns `{"unreadCount": 3}`.

`POST .../notifications/read` with `{"ids": ["..."]}` (at most 100) or
`{"all": true}` marks them read for the caller and returns
`{"unreadCount": 0}`. Unknown IDs are ignored.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | `limit` out of range; malformed `before`; `unread` not a boolean; neither or both of `ids` and `all`; empty or too many IDs |
| 400 | `invalid_request` | Malformed body or unknown field |
| 404 | `not_found` | Not a member of the household |

### Push device tokens

A signed-in app registers its APNs device token so the push sweep can deliver
household notifications to it ([pantry-usage.md](pantry-usage.md#push-delivery)).
These routes need only a bearer token. The device token travels in the body,
not the path, so it never appears in request logs.

`PUT /api/v1/me/device-tokens`

```json
{ "token": "a1b2c3…64 hex digits…", "environment": "production", "platform": "ios" }
```

returns

```json
{
  "token": "a1b2c3…",
  "environment": "production",
  "platform": "ios",
  "createdAt": "2026-09-16T12:00:00Z",
  "updatedAt": "2026-09-18T07:10:00Z"
}
```

- `token` is the device token as hex; it is stored lowercase. `environment` is
  `sandbox` (Xcode debug builds) or `production` (TestFlight and App Store); the
  sweep sends each token to that APNs gateway. `platform` is optional and only
  `ios`.
- Idempotent, keyed by token: the app registers again on every launch, which
  only updates `environment` and `updatedAt`. A token registered by another
  user moves to the caller (a shared device that someone else signed in on).

`DELETE /api/v1/me/device-tokens` with `{"token": "..."}` returns `204`. The
app sends it at sign-out. It removes the token only if it belongs to the
caller, and a token that isn't registered is not an error.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | `token` not hex (16–400 digits, even length); `environment` not `sandbox` or `production`; `platform` not `ios` |
| 400 | `invalid_request` | Malformed body or unknown field |
| 401 | `unauthenticated` | Missing or invalid access token |

### Delete account

`DELETE /api/v1/me` (no body) permanently deletes the caller's account and
returns `204`. The app offers it as **Delete Account** on the Household tab and
on the no-household screen (App Review guideline 5.1.1(v)).

- **A household the caller is the last member of** is deleted with everything
  stored for it: recipes, plans, pantry, notifications, specialty choices,
  shopping settings and handoffs, skips, ratings, events, Autopilot data, and
  pending invitations.
- **A household others still share** is left, and its shared data stays. If the
  caller is its only admin, the longest-standing other member is made admin
  first, so deletion never fails on the last-admin rule that blocks leaving.
- **The caller's own records**: their ratings are deleted; their user ID is
  removed from the events and cook usage they recorded in shared households and
  from notification read receipts; their sessions, device tokens, sign-in
  identities, and user record are deleted, in that order and last.

Every step is idempotent, and the user record goes last, so a failure part-way
(`500`) leaves an account that can sign in and delete again. Access tokens
already issued stay valid until they expire (15 minutes), but no session can
refresh and `GET /me` answers `401`. Apple sign-in tokens aren't revoked with
Apple: the API never exchanges an authorization code, so it holds none.

| Status | Code | When |
| --- | --- | --- |
| 401 | `unauthenticated` | Missing or invalid access token |
| 500 | `internal` | A store failed part-way; retrying is safe |

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
    { "id": "66e5a1f2c3b4a5d6e7f80a13", "key": "oil", "name": "Oil", "category": "pantry", "categoryConfident": true, "imageUrl": "https://img.example.com/oil.jpg" },
    { "id": "66e5a1f2c3b4a5d6e7f80a14", "key": "olive oil", "name": "Olive Oil", "category": "pantry", "categoryConfident": true }
  ]
}
```

`categoryConfident: false` means no category rule matched, so the category is
a placeholder (`other`) awaiting review.

`imageUrl` is the ingredient's image, when the import that created it supplied
one; it is omitted otherwise. The catalog is where ingredient images live, so
a [recipe detail](#get)'s ingredient lines resolve the same image by
`ingredientId` rather than storing a copy on the recipe.

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

## Autopilot

Autopilot proposes a week of dinners from the household's taste profile, the
week's context, and its history, and explains each pick
([autopilot.md](autopilot.md)). Routes are under
`/api/v1/households/{householdId}/autopilot`. Reading needs `household.view`;
every change needs `plan.edit`.

### Taste profile

`GET .../autopilot/profile`

```json
{
  "householdId": "66e5a1f2c3b4a5d6e7f80913",
  "configured": true,
  "taste": {
    "likes": { "cuisines": ["mexican", "thai"], "tags": ["comfort food"], "proteins": ["chicken", "pork"] },
    "dislikes": { "cuisines": [], "tags": [], "proteins": ["lamb"] }
  },
  "restrictions": {
    "diets": [], "allergens": ["peanuts"], "excludedIngredients": ["cilantro"],
    "excludedCuisines": [], "excludedProteins": [], "excludedTags": [], "noSpicy": false
  },
  "schedule": {
    "planDays": ["mon", "tue", "wed", "thu", "fri", "sun"], "weeknights": ["mon", "tue", "wed", "thu"],
    "mealsPerWeek": 5, "defaultServings": null, "weeknightMaxMinutes": 35
  },
  "cookTime": { "quickMaxMinutes": 20, "mediumMaxMinutes": 35, "maxLongPerWeek": 1, "minQuickPerWeek": 2, "avoidConsecutiveLong": true },
  "novelty": "balanced",
  "equipment": ["smoker"],
  "weekdayRules": [
    { "day": "tue", "label": "Taco Tuesday", "cuisines": ["mexican"], "tags": [], "proteins": [], "methods": [], "timeBand": null, "frequency": "every_week" },
    { "day": "sun", "label": "Sunday smoker night", "cuisines": [], "tags": [], "proteins": ["chicken", "pork"], "methods": ["smoker"], "timeBand": "long", "frequency": "at_most_once" }
  ],
  "sections": {
    "taste": { "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-14T18:30:00Z" },
    "restrictions": { "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-14T18:30:00Z" },
    "schedule": { "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-14T18:30:00Z" },
    "cookTime": { "updatedBy": "66e5a1f2c3b4a5d6e7f80917", "updatedAt": "2026-09-15T08:10:00Z" },
    "novelty": { "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-14T18:30:00Z" },
    "equipment": { "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-14T18:30:00Z" },
    "weekdayRules": { "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-14T18:30:00Z" }
  },
  "effective": { "defaultServings": 2 },
  "createdBy": "66e5a1f2c3b4a5d6e7f80912",
  "createdAt": "2026-09-14T18:30:00Z",
  "updatedBy": "66e5a1f2c3b4a5d6e7f80917",
  "updatedAt": "2026-09-15T08:10:00Z"
}
```

- A household that never saved a profile gets the defaults with
  `configured: false`, every `sections` value `null`, and `null` timestamps.
  Defaults: plan Monday–Friday, 4 meals, weeknights Monday–Thursday, bands
  20/35 minutes, at most 2 long meals, avoid back-to-back long meals,
  `balanced`.
- `PUT` replaces the whole profile: sections you leave out reset to their
  defaults. Use it for onboarding. `PATCH` replaces only the sections you send.
  Each section is replaced whole, so send all of its fields.
- `sections.<name>` is who last changed that section and when. A section only
  changes when its values do; saving identical values changes nothing.
- `effective.defaultServings` is `schedule.defaultServings`, or the
  household's default servings when that is `null`.
- Validation (`400 validation_failed`):
  - Cuisines, tags, and excluded ingredients are free text, trimmed,
    lowercased, and deduplicated: at most 30 values of 40 characters (50
    excluded ingredients of 60 characters). Cuisines and tags are stored in
    canonical form (`North America` → `north american`; see
    [autopilot.md](autopilot.md#cuisines)), and stored profiles read that way
    too.
  - Proteins, diets, allergens, equipment, days, `novelty`, `timeBand`, and
    `frequency` must come from the [vocabulary](#vocabulary).
  - A value can't be both liked and disliked, or liked and excluded.
  - `planDays` needs at least one day. `mealsPerWeek` is between 1 and the
    number of plan days.
  - `defaultServings` is `null` or 1–12. `weeknightMaxMinutes` is `null` or
    5–480.
  - `quickMaxMinutes` is 5–480, and `mediumMaxMinutes` is greater than it and
    at most 480.
  - `maxLongPerWeek` and `minQuickPerWeek` are 0–7; `maxLongPerWeek: 7` means
    no limit.
  - `weekdayRules` holds at most one rule per day. Each rule needs at least one
    cuisine, tag, protein, method, or time band, with at most 10 values per
    list and a label of at most 40 characters (default: the day's name). Its
    methods must be in `equipment`.
- Restrictions are hard: Autopilot never suggests a recipe that violates one.
  Everything else is a preference.

### Vocabulary

`GET .../autopilot/vocabulary` returns the choices for the onboarding and
preference screens:

```json
{
  "cuisines": [{ "value": "mexican", "label": "Mexican", "recipeCount": 42 }, { "value": "thai", "label": "Thai", "recipeCount": 0 }],
  "tags": [{ "value": "comfort food", "label": "Comfort Food", "recipeCount": 12 }],
  "proteins": [{ "value": "chicken", "label": "Chicken", "recipeCount": 120 }, { "value": "tofu", "label": "Tofu & tempeh", "recipeCount": 8 }],
  "diets": [{ "value": "vegetarian", "label": "Vegetarian", "description": "No meat or fish" }],
  "allergens": [{ "value": "peanuts", "label": "Peanuts" }],
  "equipment": [{ "value": "smoker", "label": "Smoker", "description": "Whole or large cuts of chicken, pork, beef, or turkey" }],
  "novelty": [{ "value": "balanced", "label": "A mix", "description": "Favorites with something new now and then" }],
  "timeBands": [{ "value": "long", "label": "Long cook OK", "description": "Longer than the medium limit" }],
  "frequencies": [{ "value": "every_week", "label": "Every week" }, { "value": "at_most_once", "label": "At most once a week" }],
  "days": [{ "value": "mon", "label": "Monday" }],
  "catalogRecipeCount": 429,
  "limits": { "maxListValues": 30, "maxExcludedIngredients": 50, "maxValueLength": 40, "maxIngredientLength": 60, "maxRuleValues": 10, "maxLabelLength": 40, "maxNoteLength": 500, "minCookMinutes": 5, "maxCookMinutes": 480, "maxServings": 12 }
}
```

- Cuisines and tags are counted in canonical form across the household's main
  meals, most used first, followed by a starter list (`recipeCount: 0`), at
  most 60 each, so onboarding works before any recipes are imported. Proteins
  list every protein with its recipe count.
- A cuisine counts for its regions too: an Italian recipe counts for
  `italian`, `southern european`, and `european`, so `recipeCount` is how many
  recipes a like of the value matches. Known cuisines have title-case labels;
  other values show the catalog's most common spelling.
- Diets, allergens, proteins, equipment, novelty, time bands, frequencies, and
  days are fixed lists without `recipeCount`.

### Preference history

`GET .../autopilot/profile/history?limit=50` (1–100, default 50) lists
preference changes, newest first:

```json
{
  "items": [
    { "type": "autopilot.preferences_updated", "userId": "66e5a1f2c3b4a5d6e7f80917", "occurredAt": "2026-09-15T08:10:00Z",
      "sections": ["cookTime"], "changes": [{ "field": "cookTime.maxLongPerWeek", "from": "2", "to": "1" }] },
    { "type": "autopilot.week_context_updated", "userId": "66e5a1f2c3b4a5d6e7f80912", "occurredAt": "2026-09-14T19:00:00Z",
      "week": "2026-W38", "changes": [{ "field": "maxMinutes", "to": "20" }] },
    { "type": "autopilot.recipe_override_updated", "userId": "66e5a1f2c3b4a5d6e7f80912", "occurredAt": "2026-09-14T18:45:00Z",
      "recipeId": "66e5a1f2c3b4a5d6e7f80915", "method": "smoker", "value": "yes", "previous": "auto" }
  ]
}
```

List fields report `added` and `removed` values; other fields report `from` and
`to` as text (omitted when empty). A cleared week context has `cleared: true`.
Match `userId` against the household's members for names.

### Recipe attributes and overrides

`GET .../autopilot/recipes/{recipeId}/attributes` shows what Autopilot
derives from a recipe:

```json
{
  "recipeId": "66e5a1f2c3b4a5d6e7f80915",
  "cookMinutes": 90,
  "timeBand": "long",
  "cuisines": ["southern"],
  "cuisineRegions": ["north american"],
  "tags": [],
  "proteins": ["pork"],
  "allergens": [],
  "diets": ["gluten-free", "dairy-free"],
  "spicy": false,
  "methods": [
    { "method": "smoker", "label": "Smoker", "suits": false, "source": "override", "heuristicSuits": true, "evidence": "Pork Tenderloin" },
    { "method": "grill", "label": "Grill", "suits": false, "source": "heuristic", "heuristicSuits": false }
  ],
  "override": { "recipeId": "66e5a1f2c3b4a5d6e7f80915", "methods": { "smoker": false }, "updatedBy": "66e5a1f2c3b4a5d6e7f80912", "updatedAt": "2026-09-14T18:45:00Z" }
}
```

- `cuisines` and `tags` are canonical. `cuisineRegions` are the broader
  regions of `cuisines`, which likes, dislikes, exclusions, and weekday rules
  also match.
- `methods` lists every equipment option. `heuristicSuits` and `evidence` are
  the heuristic's answer and why; `suits` is what Autopilot uses, and `source`
  says whether the household overrode it. See
  [autopilot.md](autopilot.md#recipe-attributes) for the rules (for example,
  "smoker" means a whole or large cut of chicken, pork, beef, or turkey).
- `PUT .../autopilot/recipes/{recipeId}/override` `{"methods": {"smoker": true, "grill": null}}`
  sets "good for smoker" to yes (`true`) or no (`false`), or back to automatic
  (`null`). Methods you leave out don't change. Returns the attributes.
- `GET .../autopilot/recipe-overrides` lists every override:
  `{"items": [{recipeId, methods, updatedBy, updatedAt}]}`.
- `cookMinutes` is `null` when unknown; an unknown cook time is treated as
  medium, and never passes a week's `maxMinutes`.

### Week context

`GET .../autopilot/weeks/{week}/context`

```json
{
  "householdId": "66e5a1f2c3b4a5d6e7f80913",
  "week": "2026-W38",
  "startDate": "2026-09-14",
  "endDate": "2026-09-20",
  "configured": true,
  "skip": false,
  "busy": true,
  "mealsPerWeek": null,
  "maxMinutes": 20,
  "servings": null,
  "days": [
    { "day": "fri", "skip": false, "maxMinutes": null, "servings": 6 },
    { "day": "sat", "skip": true, "maxMinutes": null, "servings": null }
  ],
  "note": "Grandparents visiting Friday",
  "updatedBy": "66e5a1f2c3b4a5d6e7f80912",
  "updatedAt": "2026-09-14T19:00:00Z"
}
```

- `PUT` replaces the week's context with `{skip, busy, mealsPerWeek, maxMinutes, servings, days, note}`
  (all optional). `DELETE` clears it (`204`, also when there was none). A week
  without a context returns `configured: false` and `null` timestamps.
- `skip`: plan nothing this week. `days[].skip`: don't plan that day.
- `maxMinutes` (5–480) is a hard cap for every day; `days[].maxMinutes` is a
  hard cap for one day (the tighter applies). Recipes with an unknown cook time
  don't pass a cap.
- `busy` is softer and about weeknights. On the profile's `weeknights` it
  prefers quick meals, allows no long ones, and wants at least half of them
  quick, without a cap. Other days keep their usual cook-time handling, and a
  day whose rule has `timeBand: long` keeps its long cook (a Sunday smoker
  night stays long). Only `maxMinutes` or `days[].maxMinutes` caps those days.
  Reasons say "Quick for your busy week" on weeknights, and "Ready in 20 min
  for Sunday" under a cap on other days.
- `servings` (1–12) overrides the household's servings for the week, and
  `days[].servings` for one day (guests). Autopilot picks the smallest serving
  size a recipe offers that feeds that many.
- `mealsPerWeek` (1–7) overrides the profile's count for this week, for example
  for extra meals.
- `note` (at most 500 characters) is kept for the household; Autopilot doesn't
  read it yet.
- Numbers are `null` or positive: `0` is `400 validation_failed`. Day overrides
  with nothing set are dropped.

### Proposals

`POST .../autopilot/weeks/{week}/generate` (body optional:
`{"avoidPrevious": false}`) generates a proposal and returns `201`. `GET
.../autopilot/weeks/{week}/proposal` returns the latest one (`404` when there
is none).

```json
{
  "id": "66e5a1f2c3b4a5d6e7f80e01",
  "householdId": "66e5a1f2c3b4a5d6e7f80913",
  "week": "2026-W38",
  "startDate": "2026-09-14",
  "endDate": "2026-09-20",
  "status": "proposed",
  "version": 2,
  "attempt": 1,
  "modelVersion": "baseline-2026.2",
  "inputsHash": "9f2c4b1d0a7e6c35",
  "requestedMeals": 5,
  "plannedMeals": 3,
  "candidateCount": 3,
  "coldStart": false,
  "slots": [
    {
      "id": "tue",
      "day": "tue",
      "date": "2026-09-15",
      "recipe": { "id": "66e5a1f2c3b4a5d6e7f80915", "name": "Beef Tacos", "imageUrl": "https://img.example.com/beef-tacos.jpg" },
      "servings": 2,
      "cookMinutes": 18,
      "timeBand": "quick",
      "score": 1.042,
      "signals": { "rating": 1, "feedback": 0.6, "familiarity": 0.5, "conversion": 0.6, "recency": 0, "weekdayAffinity": 0.5,
                    "taste": 0.4, "rule": 1, "timeFit": 0.55, "novelty": 0, "servingsFit": 0, "pantry": 0, "avoid": 0, "variety": -0.15 },
      "reasons": [
        { "code": "rule", "text": "Taco Tuesday · Mexican" },
        { "code": "busyWeek", "text": "Quick for your busy week (18 min)" },
        { "code": "rating", "text": "You rated this 5★" }
      ],
      "swapCount": 1
    }
  ],
  "unfilled": [
    { "day": "thu", "date": "2026-09-17", "code": "no_quick_candidates", "text": "No remaining recipe is ready within 20 minutes on Thursday." }
  ],
  "messages": [
    { "code": "not_enough_candidates", "text": "Only 3 quick recipes (≤20 min) match; planned 3 of 5 nights." }
  ],
  "objective": { "meals": 2.91, "variety": -0.15, "cookTime": 0, "rules": 0, "novelty": 0, "total": 2.76 },
  "swapCount": 1,
  "excludedSlotIds": [],
  "generatedBy": "66e5a1f2c3b4a5d6e7f80912",
  "generatedAt": "2026-09-14T19:02:00Z",
  "updatedAt": "2026-09-14T19:03:10Z",
  "decidedBy": null,
  "decidedAt": null
}
```

- **One proposal per week.** Generating again replaces it with a new `id` and
  `attempt`. A pending proposal is recorded as rejected, and its meals are
  avoided where alternatives exist (`avoidPrevious: false` turns that off).
  The same inputs always produce the same week.
- **The plan comes first.** Days that already have an entry aren't planned,
  and those meals count toward `requestedMeals`. A finalized week can't be
  generated (`409 plan_finalized`).
- **Slots** are ordered by day, and a slot's `id` is its day. `reasons` (at
  most 3, most important first) are for display: join the texts with " · ".
  `signals` and `objective` are the numbers behind them, for debugging and
  "why?" screens.
- **Shortfalls are explained.** `messages` covers `week_skipped`,
  `empty_catalog`, `week_full`, `not_enough_candidates`, `not_enough_days`,
  `rule_method_unmet` (a rule day with methods got a meal that doesn't suit
  them, because nothing suitable was left), `already_planned`, and `cold_start`. Days that couldn't be filled are in
  `unfilled`. A skipped week returns `201` with no slots.
- **Swap.** `POST .../proposal/slots/{slotId}/swap` `{"version": 2}` replaces
  that day's meal with the next best one that fits the same constraints and the
  rest of the week. A meal swapped out isn't offered for that day again. It
  returns the proposal with `version` increased; `409 no_alternative` when
  nothing else fits.
- **Accept.** `POST .../proposal/accept` `{"version": 3, "excludeSlotIds": ["thu"]}`
  adds the other meals to the draft plan in one change and returns
  `{proposal, plan, added, skipped}`. `added` are the new plan entries
  (`origin: autopilot`). `skipped` lists meals that weren't added because the
  day now has an entry (`dayTaken`) or the recipe is already in the week
  (`alreadyPlanned`). Existing entries are never replaced. `409
  nothing_to_accept` when every meal is excluded or skipped.
- **Reject.** `POST .../proposal/reject` `{"version": 3}` dismisses the
  proposal (`status: rejected`).
- **Versions.** Swap, accept, and reject need the proposal's current
  `version`. When another member changed the proposal first, the response is
  `409 proposal_changed`: reload the proposal and try again.

| Status | Code | When |
| --- | --- | --- |
| 400 | `validation_failed` | Invalid week, profile, context, override, limit, or `excludeSlotIds`; missing `version`; a `slotId` that isn't a day code; a PATCH whose only sections are `null` (a section's value must be an object) |
| 400 | `invalid_request` | Malformed body, unknown fields, or wrong types |
| 403 | `forbidden` | Changing anything without `plan.edit` |
| 404 | `not_found` | Not a member; no proposal for the week; no slot on that day; recipe not in the household |
| 409 | `plan_finalized` | The week's plan is finalized; set it back to `draft` first |
| 409 | `plan_full` | Accepting would exceed 50 entries |
| 409 | `proposal_changed` | The `version` is stale, or another member generated the week at the same time |
| 409 | `proposal_not_pending` | The proposal was already accepted or rejected |
| 409 | `no_alternative` | No other recipe fits the slot |
| 409 | `nothing_to_accept` | Every meal is excluded, or its day or recipe is already planned |
| 409 | `proposal_stale` | A proposed recipe was removed or lost its serving size; generate again |
| 409 | `conflict` | The profile or context kept changing concurrently; retry |

### Add-on pairings

Pairings suggest an add-on recipe ("Garlic Bread") or a grocery item ("Club
crackers") with a main meal, from the household's rules and from what it
usually has ([autopilot.md](autopilot.md#add-on-pairings)).

`GET .../autopilot/weeks/{week}/pairings[?entryId=…]` (`household.view`)

```json
{
  "week": "2026-W38",
  "startDate": "2026-09-14",
  "endDate": "2026-09-20",
  "meals": [
    {
      "entryId": "66e5a1f2c3b4a5d6e7f80c01",
      "day": "mon",
      "date": "2026-09-14",
      "recipe": { "id": "66e5a1f2c3b4a5d6e7f80915", "name": "Creamy Garlic Spaghetti" },
      "servings": 2,
      "mealCategories": ["pasta"],
      "pairings": [
        {
          "key": "recipe:66e5a1f2c3b4a5d6e7f81008",
          "target": {
            "kind": "recipe",
            "recipe": { "id": "66e5a1f2c3b4a5d6e7f81008", "name": "Garlic Bread", "imageUrl": "https://img.example.com/garlic-bread.jpg",
                        "cookMinutes": 15, "timesOrdered": 203, "lastOrderedWeek": "2026-W37", "isAddon": true, "tags": [] }
          },
          "servings": 2,
          "source": "learned",
          "frequency": "suggest",
          "mealCategory": "pasta",
          "confidence": 0.95,
          "learned": { "weeksTogether": 186, "mealCategoryWeeks": 195, "otherWeeks": 35, "otherWeeksRate": 0.49 },
          "reason": "You usually have Garlic Bread with pasta (95% of pasta weeks)",
          "ruleId": null,
          "inPlan": false,
          "canMakeRule": true
        }
      ]
    }
  ],
  "groceryItems": [
    {
      "id": "66e5a1f2c3b4a5d6e7f80f02",
      "key": "grocery:club crackers",
      "groceryItem": { "name": "Club crackers", "quantity": 1, "unit": "package" },
      "entryId": "66e5a1f2c3b4a5d6e7f80c02",
      "recipe": { "id": "66e5a1f2c3b4a5d6e7f80916", "name": "Chicken Noodle Soup" },
      "source": "rule",
      "ruleId": "66e5a1f2c3b4a5d6e7f80f01",
      "text": "Club crackers for Chicken Noodle Soup",
      "addedBy": "66e5a1f2c3b4a5d6e7f80912",
      "addedAt": "2026-09-14T19:10:00Z"
    }
  ]
}
```

- One entry per planned **main meal**, in plan order; add-on entries aren't
  meals. `entryId` narrows it to one meal, for the "you added a pasta dish"
  prompt.
- At most 3 `pairings` per meal: rules first (`always`, then `suggest`), then
  learned ones by confidence. `source` is `rule` or `learned`; `target.kind`
  is `recipe` or `grocery_item`; `confidence` and `learned` are present when
  history backs the pairing (also for a rule).
- Pairings already in the week or dismissed are left out here. An add-on
  counts as "in the week" only on that meal's **day**; a grocery item counts
  for the whole week.
- `groceryItems` are the accepted grocery items on this week's list. They
  disappear from it when their meal is unplanned.
- Reading records `pairing.suggested` once per meal and target.

| Endpoint | Body | What it does |
| --- | --- | --- |
| `POST .../pairings/accept` | `{"entryId": "…", "key": "…"}` | Adds it: an add-on becomes a plan entry on the meal's day (`origin: autopilot`), a grocery item joins the week's list. Returns `{status, pairing, entry, groceryItem, plan}` with `status` `added` or `alreadyAdded`. |
| `POST .../pairings/dismiss` | `{"entryId": "…", "key": "…"}` | Hides it for that meal this week. Returns the week's pairings. |
| `POST .../pairings/rules` | `{"entryId"` **or** `"slotId", "key", "frequency"?}` | Turns a learned pairing into a household rule for its meal category. Returns `{status, rule, profile}`, `status` `created`, `merged` (added to the rule that item already had), or `unchanged`. |
| `DELETE .../pairings/grocery-items/{itemId}` | — | Takes a paired grocery item off the week (`204`). |

All four need `plan.edit`. `404 not_found`: no such plan entry, pairing, or
item. `409 plan_finalized`: accepting or removing in a finalized week.

`GET /api/v1/households/{householdId}/recipes/{recipeId}/pairings?week=2026-W38`
(`household.view`) is the recipe detail's "Tastes even better with" carousel:

```json
{
  "recipeId": "66e5a1f2c3b4a5d6e7f80915",
  "week": "2026-W38",
  "entryId": "66e5a1f2c3b4a5d6e7f80c01",
  "mealCategories": ["pasta"],
  "items": [ { "key": "recipe:66e5…", "target": { "kind": "recipe", "recipe": { "…": "…" } }, "source": "learned",
               "confidence": 0.95, "reason": "You usually have Garlic Bread with pasta (95% of pasta weeks)",
               "inPlan": false, "ruleId": null, "canMakeRule": true } ]
}
```

- `week` is optional. With one, `inPlan` says whether the target is already
  planned (on the recipe's day when it is planned, otherwise anywhere that
  week) or on the week's list, and `entryId` is the recipe's plan entry when
  it has one. Without a week, nothing is in the plan and `entryId` is `null`.
- Unlike the week endpoint, items already in the week are **included** with
  `inPlan: true`, so the carousel is stable; show them as checked.
- At most 6 items. Add-on recipes have none.

### Pairing rules in the profile

Rules are the profile's `pairings` section, edited with `PATCH .../profile`
like any other section:

```json
{
  "pairings": [
    { "id": "66e5a1f2c3b4a5d6e7f80f01", "label": "Pasta night",
      "when": { "mealCategories": ["pasta"], "cuisines": [], "tags": [], "proteins": [] },
      "add": { "kind": "recipe", "recipeId": "66e5a1f2c3b4a5d6e7f81008", "recipeName": "Garlic Bread", "groceryItem": null },
      "frequency": "always" },
    { "id": "66e5a1f2c3b4a5d6e7f80f03", "label": "",
      "when": { "mealCategories": ["soup"], "cuisines": [], "tags": [], "proteins": [] },
      "add": { "kind": "grocery_item", "recipeId": null, "recipeName": null,
               "groceryItem": { "name": "Club crackers", "quantity": 1, "unit": "package" } },
      "frequency": "suggest" }
  ]
}
```

- `id` is `null` (or absent) for a new rule and assigned by the server; send a
  rule's `id` back to keep it. `recipeName` and `add.kind` are read-only.
- A meal matches when it matches every group `when` sets; within a group any
  value matches, and cuisines match a recipe's cuisines or their regions. A
  rule needs at least one condition.
- `add` is exactly one of `recipeId` (an add-on of this household with serving
  sizes) or `groceryItem` (`name` 1–60 characters, optional `quantity`
  0 < q ≤ 99 and `unit` from the ingredient units; a unit needs a quantity).
- `frequency`: `always` (included with a proposed meal unless taken out) or
  `suggest` (offered). Default `suggest`. At most 20 rules, and two rules
  can't repeat the same meals and item.
- **`PUT .../profile` keeps `pairings` when it doesn't send them**, unlike
  every other section, so onboarding never deletes rules.
- `mealCategories` values, `pairingFrequencies`, and the limits
  (`maxPairingRules`, `maxGroceryItemNameLength`, `maxGroceryItemQuantity`)
  come from the [vocabulary](#vocabulary). A recipe's categories and their
  overrides are on its [attributes](#recipe-attributes-and-overrides):
  `mealCategories: [{category, label, suits, source, heuristicSuits, evidence}]`,
  written with `PUT .../recipes/{id}/override` `{"mealCategories": {"pasta": true}}`
  (`null` returns one to the heuristic; the same request may carry `methods`).

### Contract notes for the app

- **Onboarding modal.**
  1. Load `GET .../vocabulary` and `GET .../profile` in parallel, and show
     onboarding while `configured` is `false`.
  2. Steps map to sections:
     - cuisines, food types, and proteins liked or disliked → `taste`;
     - allergens, diets, excluded ingredients, cuisines, and proteins, plus
       "no spicy" → `restrictions`;
     - plan days, meals per week, servings (`null` = household default),
       weeknights, and weeknight time limit → `schedule`;
     - time bands, max long meals, and back-to-back long meals → `cookTime`;
     - favorites vs. new → `novelty`;
     - equipment, then weekday rules such as "Taco Tuesday" or "Sunday smoker
       night" → `equipment` and `weekdayRules`.
  3. Offer free-text cuisines and tags as chips from the vocabulary, allowing
     custom values. Enforce `limits` locally.
  4. Finish with one `PUT .../profile` containing every section, then offer to
     plan this week (`POST .../generate`). A `400 validation_failed` message is
     safe to show next to the form.
- **Preference screens.** Use one screen per section, saved with `PATCH
  .../profile` containing only that section, and always send the whole section
  object. Show "Changed by {member} {relative time}" from
  `sections.<name>`, resolving `userId` through the household's members. A
  history screen reads `GET .../profile/history`. A per-recipe "Good for
  smoker" control reads `GET .../recipes/{id}/attributes`: show
  `heuristicSuits` and `evidence` as the automatic answer, and write it with
  `PUT .../recipes/{id}/override` (`null` returns it to automatic).
- **Week-context sheet.**
  1. From the week screen, `GET .../weeks/{week}/context` and edit it as a
     whole: skip week; busy toggle; strict time cap; servings for the week;
     per-day skip, cap, and servings; a note.
  2. Save with `PUT` and clear with `DELETE`.
  3. After saving, offer "Regenerate" when a proposal exists.
- **Generate, swap, and accept.**
  1. "Plan my week" calls `POST .../generate` (`201`). Show the proposal
     separately from the plan, not as plan entries.
  2. Show each slot's day, date, recipe, `cookMinutes`, and reasons joined with
     " · ".
  3. Show `messages` as a banner, and `unfilled` days as empty rows with their
     `text`.
  4. Keep the proposal's `version`. Every swap, accept, or reject sends it, and
     every successful response returns the new proposal (and `version`) to
     display. On `409 proposal_changed`, reload the proposal. On
     `409 no_alternative`, show the message and keep the meal.
  5. "Accept" sends the slots the member switched off as `excludeSlotIds`.
     Then show the returned `plan` (entries with `origin: autopilot` can carry
     an Autopilot badge) and mention `skipped` meals.
  6. "Regenerate" calls generate again; "Dismiss" calls reject.
  7. On `409 plan_finalized`, explain that the week is finalized and offer to
     reopen it (`PUT .../plans/{week}/status` `{"status": "draft"}`).
- **Pairings in the review screen.** Each slot carries `pairings[]` with an
  `id` and `included`. Show them under the meal as "Add Garlic Bread?" with a
  checkmark, pre-checked when `included` (the household's `always` rules) and
  visibly secondary to the meal itself. Accepting sends `pairingIds` with
  exactly the ones checked, for slots that are still included; leave the field
  out to accept the pre-checked ones. The response's `pairingsAdded` names
  each added add-on entry or grocery item, and `pairingsSkipped` the ones that
  no longer fit (`alreadyPlanned`, `unavailable`). Excluding a slot drops its
  pairings, so uncheck the meal rather than its pairings when both go.
- **The manual-add prompt.** After a member adds a meal to the week, call
  `GET .../weeks/{week}/pairings?entryId={the new entry}`. When it returns
  pairings, offer the first one ("Add Garlic Bread?" / "Add Club crackers to
  the list?"); accepting posts to `pairings/accept`, and "Not this time" to
  `pairings/dismiss`, which hides it for that meal this week. Don't prompt
  again for the same entry and key; the API leaves accepted and dismissed
  pairings out of later reads.
- **The recipe carousel.** `GET .../recipes/{id}/pairings?week=` backs "Tastes
  even better with": a row of cards with the add-on's image, name,
  `cookMinutes`, and a checkmark, using `reason` as the caption. Items with
  `inPlan: true` are already in the week; show them checked, not hidden.
  Tapping one **when the recipe is planned that week** (`entryId` is set)
  accepts through `POST .../weeks/{week}/pairings/accept` with that `entryId`.
  When the recipe isn't planned (`entryId` is `null`), plan the main recipe
  first (`POST .../plans/{week}/entries`), then accept the pairing with the
  new entry's id: a pairing always belongs to a planned meal.
- **Pairing rules in preferences.** Add a "Pairings" row to Autopilot
  Preferences, backed by the profile's `pairings` section: one line per rule
  ("Pasta → Garlic Bread · Always"), edited as when (meal categories from the
  vocabulary, plus cuisines, tags, proteins), what to add (an add-on recipe
  picker, or a grocery item with an optional quantity and unit), and
  `frequency`. Save the whole section with `PATCH .../profile`, keeping each
  rule's `id`. "Changed by" comes from `sections.pairings` like every other
  section, and the change history lists added and removed rules. A learned
  suggestion can be kept from the week screen with `pairings/rules`, which
  returns the updated profile.
- **Add-on entries in the week.** An accepted add-on is an ordinary plan entry
  with `origin: autopilot` on the meal's day. Show it attached to its meal
  rather than as a separate dinner; removing it is a normal entry delete.
- **Events.** The server records generation, swaps, acceptance, rejection,
  pairing suggestions and decisions, and preference changes itself. Keep sending `recipe.cooked` and
  `recipe.skipped` with the plan entry's `entryId`: that is how Autopilot learns
  which accepted meals were actually cooked.

## Menu

The Menu screen is one screen for the week: a week strip, the week's plan,
curated carousels, and an "All Meals" list. Every route is under
`/api/v1/households/{householdId}` and needs `household.view`. Weeks are ISO
weeks (`2026-W38`); omitting `week` means the household's current week in its
time zone.

Nothing here is stored. Each request reads the catalog once and builds the
response in memory, so sections always reflect the household's current
ratings, orders, plans, and Autopilot preferences.

### The menu

`GET /api/v1/households/{householdId}/menu?week=2026-W38`

```json
{
  "week": "2026-W38",
  "weekStart": "2026-09-14",
  "weekEnd": "2026-09-20",
  "currentWeek": "2026-W38",
  "timing": "current",
  "plan": { "week": "2026-W38", "status": "draft", "entries": [] },
  "proposal": { "id": "66e5...", "status": "proposed", "version": 3, "plannedMeals": 5 },
  "sections": [
    {
      "id": "favorites",
      "kind": "carousel",
      "title": "Your Favorites",
      "subtitle": "Based on what you rate and reorder",
      "moreQuery": { "sort": "popular" },
      "items": [
        {
          "recipe": { "id": "66e5...", "name": "Beef Tacos", "cookMinutes": 30, "calories": 690, "proteinGrams": 36, "timeBand": "medium", "...": "a full RecipeSummary" },
          "badges": [{ "code": "make_again", "text": "Make Again" }],
          "reason": "Ordered 21 times",
          "inPlan": true,
          "planEntryIds": ["66e5a1f2c3b4a5d6e7f80c01"]
        }
      ]
    }
  ]
}
```

- `timing` is `past`, `current`, or `upcoming`, comparing `week` with
  `currentWeek`.
- `plan` is the same object as [`GET .../plans/{week}`](#get-a-week), or
  `null` when nothing is stored for the week.
- `proposal` summarizes the week's [Autopilot proposal](#proposals), or is
  `null`. Load the full proposal from its own endpoint to review it.
- `recipe` is a `RecipeSummary`, so cards show ratings, `cookMinutes`,
  `calories`, `proteinGrams`, and `timeBand` without another request.
- `inPlan` and `planEntryIds` refer to the week being shown.
- `reason` is a short explanation or `null`; `moreQuery` is a set of
  `GET .../menu/recipes` query parameters (all string values) that lists more
  of the same, or `null`.

**Sections.** Sections are returned in display order, an empty one is left
out, each has at most 12 items, and the order within a section is
deterministic. Add-ons appear only in `sides` and in the history sections.

| Section | Shown | What it lists |
| --- | --- | --- |
| `favorites` | every week | Main meals with a household average of 4+, a make-again or kid-favorite tag, 3+ orders within two years, or 2+ cooks, by favorite score. On a past week it is titled "Cook It Again". |
| `quick` | current, upcoming | Meals with a known cook time within the household's quick limit (20 minutes by default), by favorite score then shortest |
| `new_to_you` | current, upcoming | Meals never ordered and never cooked, by taste fit |
| `rule_<day>` | current, upcoming | One per [weekday rule](autopilot.md#taste-profile), Monday first, titled from the rule's label ("Smoker Night Ideas", subtitle "For Sunday"). A second rule on one day is `rule_<day>_2`. |
| `long_cooks` | current, upcoming | "Worth the Wait": meals longer than the medium limit, by favorite score |
| `sides` | current, upcoming | Add-ons, most ordered first |
| `history_planned` | past | "You Planned": that week's plan entries by day, one card per recipe carrying every entry ID |
| `history_ordered` | past | "You Ordered": recipes whose `orderWeeks` contain the week, add-ons included |

A recipe that breaks a hard [restriction](autopilot.md#taste-profile) (diet,
allergen, excluded cuisine, protein, tag, ingredient, or "no spicy") is left
out of the suggestion sections — `quick`, `new_to_you`, `rule_<day>`, and
`long_cooks` — and so is anything a member tagged never-again. History and
`favorites` are a record of what the household did, so they are not filtered.

Weekday rule sections match the way Autopilot scores rules: a rule's methods
dominate, so a meal must suit one of them (when the household has that
equipment) to appear, and it must match at least half the rule's groups.

**Badges** are at most two per card, most important first, from a closed set:
`make_again`, `top_rated` (average 4.5+), `kid_favorite`, `autopilot_pick` (in
the week's pending proposal or added to the plan from one), `smoker_friendly`
(the household has a smoker and the recipe suits it), `often_ordered` (5+),
`quick`, and `new`. A section drops the badge that would repeat its own title,
and a smoker rule's section leads with `smoker_friendly`.

### All Meals

`GET /api/v1/households/{householdId}/menu/recipes`

| Parameter | Meaning |
| --- | --- |
| `q` | Text the name or headline contains, case-insensitive; at most 100 characters |
| `protein` | An Autopilot protein value (`chicken`, `beef`, …), from `GET .../menu/filters` |
| `cuisine` | A canonical cuisine; a region also matches its cuisines (`asian` matches Thai) |
| `tag` | A canonical tag; spellings that differ only by a space are one value |
| `maxMinutes` | Keeps meals with a known cook time at most this long |
| `addons` | `true` lists add-ons instead of main meals (default `false`) |
| `sort` | `recommended` (default), `popular`, `recent`, `quick`, or `name` |
| `week` | The week `inPlan` and `planEntryIds` refer to (default: the current week) |
| `limit`, `cursor` | Page size (default 20, at most 50) and the previous page's `nextCursor` |

```json
{ "items": [ { "recipe": {}, "badges": [], "reason": "Ready in 15 min", "inPlan": false, "planEntryIds": [] } ], "nextCursor": "eyJzIjoibmFtZSIsIms..." }
```

`recommended` is a deterministic score: the favorite score (ratings,
make-again and kid-favorite tags, how often and how recently the household
ordered or cooked it) plus taste fit (the profile's likes and dislikes, plus
the share of past orders with the recipe's proteins and cuisines), lowered for
recipes that break a hard restriction — those are listed, not hidden. It does
not call the Autopilot planner: ranking a whole catalog per request is a
week-planning cost, and browsing needs stable, explainable order.

A cursor is the last item's position in the sort, not an offset, so pages stay
in order for the same inputs even when recipes change between requests. Send
it back with the same `sort`; a cursor from another sort is
`400 validation_failed`. `nextCursor` is `null` on the last page.

### Filter chips

`GET /api/v1/households/{householdId}/menu/filters`

```json
{
  "proteins": [{ "value": "chicken", "label": "Chicken", "count": 128 }],
  "cuisines": [{ "value": "north american", "label": "North American", "count": 96 }],
  "tags": [{ "value": "one pot", "label": "One Pot", "count": 41 }],
  "maxMinutes": [15, 20, 30, 45],
  "sorts": [{ "value": "recommended", "label": "Recommended" }]
}
```

Values and counts come from the household's main meals through the same
[Autopilot vocabulary](#vocabulary) the preference screens use, so a chip's
`count` is how many recipes the matching filter returns. Options the catalog
doesn't use are left out.

### Week strip

`GET /api/v1/households/{householdId}/weeks?around=2026-W38&before=8&after=4`

```json
{
  "items": [
    { "week": "2026-W30", "weekStart": "2026-07-20", "weekEnd": "2026-07-26", "timing": "past", "plannedCount": 5, "addOnCount": 1, "cookedCount": 3, "orderedCount": 4, "status": "finalized" }
  ],
  "earliestWeek": "2023-W05"
}
```

- `around` defaults to the current week; `before` and `after` default to 8 and
  4 and are clamped to 52 each. Weeks are oldest first.
- `plannedCount` is planned **main meals**, `addOnCount` the week's planned
  add-ons (a pairing's garlic bread), `cookedCount` distinct `recipe.cooked`
  events in that week, and `orderedCount` main meals whose `orderWeeks`
  contain it. Add-ons are excluded from every count that says "meals", so a
  week with five dinners and a garlic bread reads `plannedCount: 5,
  addOnCount: 1` — show it as "5 meals · 1 add-on", never as six meals.
- The same rule holds elsewhere: a proposal's `plannedMeals` counts only main
  meals, because add-ons never take a day slot and are never Autopilot
  candidates. `GET .../menu`'s `plan.entries`, by contrast, is the raw entry
  list and **does** include add-on entries, so count meals there by excluding
  entries whose recipe is an add-on rather than taking `entries.length`.
- `status` is `draft`, `finalized`, or `none` when no plan is stored.
- `earliestWeek` is the earliest week with a planned entry or an ordered main
  meal, so the app knows how far back "Past" goes. It is `null` for a
  household with no history.

### Contract notes for the app

- **One screen.** Load `GET .../weeks` once for the strip and `GET .../menu`
  per week shown. Both are cheap enough to re-request on pull to refresh;
  neither is cached server-side.
- **Cards.** Render `recipe.imageUrl` large, and the facts line from
  `cookMinutes`, `calories`, and `proteinGrams` ("30 min · 690 cal · 36g
  protein"), leaving out what is `null`. Show `badges` as-is — the codes are
  closed, so each can have its own icon — and `reason` as one short line.
- **Sections.** Treat `sections` as opaque and ordered: render whatever comes
  back, in order, and key carousels by `id`. New section IDs can appear, so
  don't switch on them exhaustively; `kind` says whether it is a carousel or a
  history list. "See all" opens All Meals with `moreQuery` applied.
- **All Meals.** Build chips from `GET .../menu/filters` and send the values
  back unchanged. Keep `nextCursor` for paging and drop it whenever a filter,
  sort, or search changes.
- **The plan.** `plan` is the same shape the Week tab already decodes, and
  `planEntryIds` lets a card open or remove the entry it belongs to.
- **Past weeks** have no `quick`, `new_to_you`, rule, `long_cooks`, or `sides`
  sections; upcoming and current weeks have no history sections.

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
| `recipe.planned` | server (planning) | required | `{entryId?, day?, date?, servings?, origin?, proposalId?}` |
| `recipe.unplanned` | server (planning) | required | `{entryId?, day?, date?, origin?}` |
| `meal.customized` | server (customize) | required | `{entryId, changes: [{ingredientKey, from, to}]}` (`from`/`to` are choice IDs, `original` when none) |
| `week.generated` | server (Autopilot) | — | `{proposalId, modelVersion, attempt, requested, planned, unfilled, candidates, coldStart?, replacedProposalId?}` |
| `meal.swapped` | server (Autopilot) | required (swapped in) | `{proposalId, slotId, day, date?, previousRecipeId, modelVersion, swapNumber}` |
| `week.accepted` | server (Autopilot) | — | `{proposalId, modelVersion, planned, added, excluded, skipped, swaps}` |
| `meal.rejected` | server (Autopilot) | required | `{proposalId, slotId, day, date?, modelVersion}` (left out when accepting) |
| `week.rejected` | server (Autopilot) | — | `{proposalId, modelVersion, planned, swaps, reason}`: `dismissed`, `regenerated` |
| `autopilot.preferences_updated` | server (Autopilot) | — | `{sections, changes?: [{field, added?, removed?, from?, to?}]}` |
| `autopilot.week_context_updated` | server (Autopilot) | — | `{changes?, cleared?}` |
| `autopilot.recipe_override_updated` | server (Autopilot) | required | `{method, value, previous?}`: `yes`, `no`, `auto` |
| `import.completed` | server (recipe import) | — | `{source, created, updated, unchanged, rejected}` |
| `shopping.handoff_created` | server (shopping) | — | `{handoffId, provider, lines, packages, checkAmount, excluded, links}` (counts only, never product IDs) |
| `shopping.order_confirmed` | server (shopping) | — | `{handoffId, provider, confirmed, packages, skipped}` |
| `shopping.store_requested` | server (shopping) | — | `{key, catalog}` (`catalog` is false when a member typed a store the catalog doesn't list; the note is never recorded) |

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
