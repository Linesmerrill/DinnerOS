# Database

MongoDB Atlas is the **authoritative** data store (free tier to start). The iOS
app's SwiftData store is only a cache.

Status: the collections below are the planned design. Each is finalized, with
indexes, in the phase that implements it. Update this page as that happens.

## Conventions

- **IDs:** `_id` is an ObjectID. The API exposes it as a hex string (`"id"`).
  External and partner identifiers are stored in separate fields and never reuse
  `_id`.
- **Field names:** camelCase in both Mongo and JSON.
- **Timestamps:** `createdAt` and `updatedAt` on every document, stored as UTC
  `Date`. Calendar concepts such as plan dates are stored as `YYYY-MM-DD` strings
  in the household's time zone, so they never shift across time zones.
- **Isolation:** every household-owned document carries `householdId`, and every
  household-scoped query filters on it. Indexes lead with `householdId`.
- **Document size:** keep documents bounded. Unbounded lists (events, ratings,
  order history across years) get their own collections instead of growing an
  array.
- **Indexes:** each domain package declares its indexes in code and applies them
  idempotently at startup. See `api/migrations/README.md`.
- **Secrets:** tokens (invitations, refresh tokens) are stored only as SHA-256
  hashes.

## Collections

### Identity

Implemented in Phase 2 (`internal/users`, `internal/auth`).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `users` | displayName, primaryEmail (verified only), createdAt, updatedAt | — (looked up by `_id` only) |
| `auth_identities` | userId, provider (`apple`/`google`/`dev`), subject, email, emailVerified, createdAt, lastUsedAt | **unique** `{provider, subject}`; `{userId}` |
| `sessions` | userId, familyId, tokenHash, expiresAt, createdAt, lastUsedAt, rotatedAt, revokedAt | **unique** `{tokenHash}`; `{userId}`; `{familyId}`; TTL on `expiresAt` (`expireAfterSeconds: 0`) |

- `auth_identities.email` is stored only when the provider verified it.
- `sessions` holds one document per refresh token. `tokenHash` is the hex
  SHA-256 of the token; the token itself is never stored. Rotated sessions stay
  until their original `expiresAt` so reuse can be detected, then MongoDB's TTL
  monitor deletes them (within about a minute). `{familyId}` supports revoking
  a whole sign-in at once.
- User and identity creation doesn't use a transaction, so it also works on a
  standalone local MongoDB. The unique `{provider, subject}` index is the guard:
  the loser of a concurrent first sign-in deletes its orphaned user and signs
  in to the winner's account.

### Households

Implemented in Phase 3 (`internal/households`, `internal/invitations`).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `households` | name, defaultServings (1–12, default 2), timeZone (IANA name), createdBy, adminCount, createdAt, updatedAt | — (looked up by `_id` only) |
| `household_memberships` | householdId, userId, role (`admin`/`member`), createdAt, updatedAt | **unique** `{householdId, userId}`; `{userId}` |
| `household_invitations` | householdId, email (trimmed, lowercase), role, tokenHash, codeHash, expiresAt, pending, acceptedAt, acceptedBy, revokedAt, createdBy, createdAt | **unique** `{tokenHash}`; **unique** `{codeHash}`; **unique partial** `{householdId, email}` where `pending: true` |

- **At least one admin, without transactions.** `adminCount` backs this rule.
  Before demoting or removing an admin, the service runs
  `updateOne({_id, adminCount: {$gt: 1}}, {$inc: {adminCount: -1}})`. If
  nothing matches, the request fails with `409 last_admin`. The membership
  change is then conditional on the role read earlier, and if it fails the
  count is added back. Promotions and admin joins increment the count after
  the membership changes. If the process dies between the two writes, the
  count ends up *lower* than the real number of admins, which only makes the
  guard stricter. This works on a standalone local MongoDB.
- A household can't become empty yet: its last admin can't leave, even as the
  only member. Deleting households comes later.
- Invitations store only the hex SHA-256 of the token and of the normalized
  code. `pending: true` is set on insert and removed (`$unset`) when the
  invitation is accepted or revoked; expiry doesn't clear it. The partial
  unique index therefore allows one pending invitation per household and
  address, and re-inviting revokes the old one first. The pending list
  (`{householdId, pending: true, expiresAt: {$gt: now}}`) uses the same index.
- Acceptance is a conditional update on `{_id, pending: true, expiresAt: {$gt: now}}`,
  so only one acceptance can succeed. Accepted and revoked invitations are kept
  as history; there is no TTL index yet.
- `time/tzdata` is compiled into the API, so time-zone validation doesn't
  depend on the container having zoneinfo.

### Recipes and ingredients

Implemented in Phase 4 (`internal/recipes`; categories and units from `internal/ingredients`).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `recipes` | householdId, source, sourceRecipeId, sourceAliases[], sourceUrl, name, headline, description, imageUrl, isAddon, servings[], prepMinutes, totalMinutes, difficulty, cuisines[], tags[], utensils[], allergens[], nutritionPerServing[], ingredients[] (ingredientId, name, pantryStaple, amounts[]: servings, quantity, quantityValue, unit, sourceUnit, rawText), steps[], orderWeeks[], timesOrdered, lastOrderedWeek, createdAt, updatedAt | **unique** `{householdId, source, sourceRecipeId}`; `{householdId, source, sourceAliases}`; with collation `en`/strength 2: `{householdId, name, _id}`, `{householdId, lastOrderedWeek: -1, name, _id}`, `{householdId, timesOrdered: -1, name, _id}`, `{householdId, tags}`, `{householdId, cuisines}` |
| `ingredients` | key, name, category, categoryConfident, sourceRefs[] (source, sourceIngredientId), imageUrl, createdAt, updatedAt | **unique** `{key}`; `{sourceRefs.source, sourceRefs.sourceIngredientId}`; `{categoryConfident, key}` |
| `import_reviews` | householdId, key, source, sourceRecipeId, recipeName, field, value, reason, status (`open`), createdAt, updatedAt | **unique** `{householdId, key}`; `{householdId, status, createdAt}` |

A recipe's ingredient lines and steps are small and bounded, so they are
embedded. Embedded ingredient lines *reference* catalog `ingredients` by ID; the
category is read from the catalog, not copied onto the recipe.

- **Exact quantities.** `quantity` is a reduced fraction string (`"3/4"`, `"2"`)
  and is authoritative. `quantityValue` is the same amount as a double, for
  display and ad-hoc queries. Both are absent when the source gave no amount.
- **The ingredient catalog is global**, not household-scoped. `key` is
  `ingredients.NormalizeName(name)`. An import line resolves by
  `(source, sourceIngredientId)` first and falls back to `key`; a name match
  gains the new source reference. New ingredients get a category from
  `ingredients.Categorize`. When no rule matches, the category is `other` with
  `categoryConfident: false`, so `{categoryConfident: false}` is the review
  queue. Imports never overwrite a stored name or category.
- **Idempotent imports.** A recipe matches a stored one when its
  `sourceRecipeId` or any alias equals the stored `sourceRecipeId` or any
  stored alias, so a later file with a different canonical ID updates the
  recipe in place. `orderWeeks` and `sourceAliases` merge as sorted set
  unions; every other field takes the file's value. Recipes that didn't change
  aren't written. An import is five bulk round trips at most, whatever its
  size: find ingredients, upsert ingredients (and re-read them), find recipes,
  write recipes, and upsert review items.
- `orderWeeks` holds one ISO week per delivery, so it stays small (a few dozen
  entries at most). `timesOrdered` and `lastOrderedWeek` are derived from it
  for sorting. `lastOrderedWeek` is stored as `""` for never-ordered recipes so
  they sort last.
- List sorting and the tag and cuisine filters are case-insensitive through the
  collation, and list queries use the same collation so these indexes apply.
  The unique import index has no collation, so source IDs match exactly. Name
  search is a case-insensitive regex of the escaped search text within one
  household.
- `import_reviews` keeps importer review items per household. `key` is the hex
  SHA-256 of source, sourceRecipeId, field, value, and reason. Re-imports use
  `$setOnInsert`, so an item's status is never reset. No endpoint reads them yet.

Ownership: recipes imported from our personal history belong to a household
(`householdId`). A future shared or partner catalog will use an explicit
`catalogId` rather than overloading `householdId`.

### Week plans

Implemented in Phase 6 (`internal/planning`).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `weekly_plans` | householdId, week (`2026-W38`), startDate (Monday, `YYYY-MM-DD`), status (`draft`/`finalized`), entries[] (id, recipeId, recipeName, recipeImageUrl, day (`mon`–`sun`; absent when unscheduled), servings, note, addedBy, addedAt), createdAt, updatedAt | **unique** `{householdId, week}` |

- One document per household and ISO week, created by the first added entry
  or status change. Reading an unplanned week writes nothing. ISO weeks have
  no time zone; `startDate` is stored for future date queries.
- `week` is zero-padded, so string order is week order, and the unique index
  also serves range queries (`week: {$gte, $lte}`). Summaries project
  `entryCount` with `$size`.
- A week has at most 50 entries, so they are embedded. `id`, `recipeId`, and
  `addedBy` are ObjectIDs.
- **No lost writes, without a version field.** Every entry change is one
  atomic update of the plan document:
  - add: an upsert with `$setOnInsert` makes sure the plan exists, then `$push`
    on `{householdId, week, status: "draft", "entries.49": {$exists: false}}`;
  - edit: positional `$set`/`$unset` (`entries.$.day`) on
    `{householdId, week, status: "draft", "entries.id": id}`;
  - delete: `$pull` with the same filter.

  Concurrent edits by different members all land. When nothing matches, the
  store reads the plan once to report `404` (no such entry), `409
  plan_finalized`, or `409 plan_full`. If two first writes race to create a
  plan, the unique index rejects one and it retries once, matching the
  winner's document.
- Entries snapshot the recipe's name and image for rendering. Recipe details
  and the grocery list read the live recipe, so re-imports show up there.

### Pantry

Implemented in Phase 7 (`internal/pantry`).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `pantry_items` | householdId, ingredientId (catalog; absent for free text), key, displayName, category, quantity, quantityValue, unit, status (`in_stock`/`low`/`out`), isStaple, expiresOn (`YYYY-MM-DD`), note, version, createdAt, updatedBy, updatedAt | **unique** `{householdId, key}` |

- **One item per ingredient.** `key` is `ingredients.NormalizeName` of the
  name, or the catalog ingredient's `key` when the item references the
  catalog, so "Olive Oil" and "olive oil" are the same item. Adding an
  ingredient that's already there updates that item. The unique index settles
  concurrent adds: the loser merges into the winner's item. Different units of
  one ingredient share the item and its single amount.
- The unique index serves every query: lists filter on `householdId` (plus
  status, category, isStaple, or a name regex) and lookups use
  `{householdId, key: {$in: [...]}}`. A pantry holds at most 1000 items, so
  lists aren't paginated.
- **Exact amounts**, as on recipes: `quantity` is a reduced fraction string
  and authoritative; `quantityValue` is a double copy. Both, and `unit`, are
  absent when no amount was recorded. An `out` item never has an amount:
  marking items out `$unset`s it.
- **Optimistic concurrency.** Every write increments `version`. An update sets
  the mutable fields (and `$unset`s cleared ones) on `{_id, householdId,
  version}`. When nothing matches, a count tells a deleted item (`404`) from a
  concurrent change, which the service retries, up to 5 attempts. Bulk status
  changes are one `updateMany` that also bumps `version`. `key` and
  `createdAt` never change. `ingredientId` and `updatedBy` are ObjectIDs.
- `ingredientId` references the global catalog but isn't required. A free-text
  name that matches no catalog ingredient is stored unlinked and is resolved
  against the catalog again whenever a grocery list is built.

### Grocery lists

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `grocery_lists` | householdId, weeklyPlanId, items[], generatedAt | **unique** `{weeklyPlanId}` |
| `grocery_provider_configurations` | householdId, provider, settings | **unique** `{householdId, provider}` |

A week's grocery list is still computed on request, with the household pantry
applied, and isn't stored yet.

### Behavior

Implemented in Phase 9 (`internal/ratings`, `internal/events`).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `recipe_ratings` | householdId, recipeId, userId, score (1–5), comment (`""` when none), tags[], createdAt, updatedAt | **unique** `{householdId, recipeId, userId}` |
| `events` | householdId, userId (absent for operator imports), type, recipeId (recipe events only), week, payload{}, clientEventId, occurredAt, recordedAt, source (`api`/`client`) | `{householdId, occurredAt}`; `{householdId, recipeId, type, occurredAt}`; **unique partial** `{householdId, userId, clientEventId}` where `clientEventId` is a string |

- **One rating per member.** Rating is a single `findOneAndUpdate` upsert on
  the unique key that returns the replaced document, so `recipe.rated` can
  carry the previous score. `_id` and `createdAt` are set only on insert.
  When two first ratings by the same member race, the loser hits the unique
  index and retries once as an update.
- **Aggregates are computed, not stored.** Recipe list and detail responses
  join ratings for the page: one `$match`/`$group` over the page's recipe IDs
  for count and sum, and one find for the caller's own ratings. Both use the
  unique index, and there is no counter on the recipe to drift. If lists ever
  need to sort by rating, denormalize then.
- Ratings stay when a member leaves the household. Display names are read from
  `users` when ratings are listed.
- **Events are append-only.** Nothing updates or deletes them. `payload` is a
  small sub-document whose shape is fixed per `type` (`events.Payload`). Free
  text, such as rating comments, is never copied into events. Fields that
  don't apply are omitted rather than stored as `null`.
- `recordedAt` is server time. `occurredAt` is when it happened: server time
  for server events, the device clock for client events (accepted within the
  last 30 days and up to 5 minutes ahead).
- The partial unique index makes client retries idempotent per user. It only
  covers events that carry a `clientEventId`. An unordered `insertMany`
  stores the rest of a batch when some are duplicates.
- **Retention:** events are kept indefinitely; there is no TTL index.
  Retention, archival, and rolling events up into per-household features are
  a later decision, made once Autopilot shows which history it needs.

Events are designed to be forwarded to Autopilot later
(see [autopilot.md](autopilot.md#signals-available-today)).

## Multi-tenancy

Inside DinnerOS, the **household** is the isolation boundary. Tenant isolation
(`tenantId` on every document and in every query) belongs to the future Autopilot
service, where DinnerOS itself is one tenant. DinnerOS does not add a `tenantId`
to its own collections until it is needed.
