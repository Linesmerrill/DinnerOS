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

`cookMinutes` in API responses is computed as max(`prepMinutes`,
`totalMinutes`) and isn't stored ([architecture.md](architecture.md#decision-log), #127).
`calories`, `proteinGrams`, and `timeBand` are derived on read too: the first
two from `nutritionPerServing` (names and units matched case-insensitively,
kilojoules converted), the band from the household's Autopilot cook-time
limits (#259).

The [Menu](api.md#menu) reads the whole catalog per request through a
projection that keeps ingredient names but drops amounts, steps, and
descriptions. It stores nothing and needs no collection or index of its own
(#251); the recipe list's projection also reads `nutritionPerServing`.

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
| `weekly_plans` | householdId, week (`2026-W38`), startDate (Monday, `YYYY-MM-DD`), status (`draft`/`finalized`), entries[] (id, recipeId, recipeName, recipeImageUrl, day (`mon`–`sun`; absent when unscheduled), servings, note, addedBy, addedAt, origin (`autopilot`; absent for manual), proposalId (autopilot entries)), createdAt, updatedAt | **unique** `{householdId, week}` |

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
- `origin` is stored only for entries added by accepting an Autopilot
  proposal, with the proposal's `proposalId`; older and manual entries read as
  `manual`. Accepting adds its entries in one `$push` with `$each`, conditioned
  on the plan being a draft with room for all of them.
- Entries snapshot the recipe's name and image for rendering. Recipe details
  and the grocery list read the live recipe, so re-imports show up there.

### Pantry

Implemented in Phase 7 (`internal/pantry`).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `pantry_items` | householdId, ingredientId (catalog; absent for free text), key, displayName, category, quantity, quantityValue, unit, status (`in_stock`/`low`/`out`), isStaple, expiresOn (`YYYY-MM-DD`), note, statusSource, statusSetAt, tracking{}, unitSize{}, history[], rate{}, lowThresholdPercent, lowAlertCycleId (see [Pantry usage](#pantry-usage)), version, createdAt, updatedBy, updatedAt | **unique** `{householdId, key}` |

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

### Pantry usage

Implemented in `internal/pantry` ([pantry-usage.md](pantry-usage.md)).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `pantry_items` (usage fields) | statusSource (`person`/`estimate`; absent on older items, meaning person), statusSetAt, tracking{cycleId, cycleSource, cycleStartedAt, unit, reference, segmentStart, segmentStartedAt, segmentRecipeUsed, recipeUsed, recipeUses, skippedUses}, unitSize{unit, quantity, sizeUnit}, history[{startedAt, endedAt, unit, start, recipeUsed, remaining, observed}], rate{perDay, unit, segments, computedAt}, lowThresholdPercent, lowAlertCycleId | the item's unique key |
| `pantry_purchases` | householdId, itemId, itemKey, source (`grocery_list`/`manual`/`provider`/`house_made`), quantity, quantityValue, unit, unitSize{}, week, clientPurchaseId, provider{key, handoffId, lineId, productId}, recordedBy, purchasedAt | `{householdId, itemId, purchasedAt: -1}`; **unique partial** `{householdId, recordedBy, clientPurchaseId}` where `clientPurchaseId` is a string; **unique partial** `{householdId, provider.handoffId, provider.lineId}` where `provider.handoffId` is a string |
| `pantry_cook_usage` | householdId, sourceKey, recipeId, entryId, userId, servings, scaledFrom, occurredAt, createdAt, lines[{itemId, ingredient, quantity, unit, deducted, trackingUnit, cycleId, skipReason}] | **unique** `{householdId, sourceKey}` |
| `pantry_settings` | householdId, lowThresholdPercent, updatedBy, updatedAt | **unique** `{householdId}` |

- **Usage state lives on the item** and changes with it under the same
  `version` check, so a purchase, a cook deduction, and a person's edit can't
  overwrite each other. Absent fields are `$unset`. Every amount is an exact
  fraction string, like `quantity`. `cycleId` is the purchase's `_id` as hex,
  or a fresh ObjectID hex for a cycle a person started. `history` is capped
  at 6 segments.
- **Estimates aren't stored.** They're computed from these fields and the
  clock on every read.
- **Purchases** are append-only history. `_id` is generated before the item
  write so the cycle can reference it. A retry with the same
  `clientPurchaseId` finds the first purchase through the partial unique
  index.
- **Cook usage** is inserted before the items change. Its unique `sourceKey`
  (`entry:<entryId>` or `event:<userId>:<clientEventId>`) is what makes a
  meal deduct once.
- **Settings** are one document per household, upserted on its unique
  `householdId`. No document means the default threshold (80).
- Deleting an item leaves its purchases and cook records.

### Specialty ingredients

Implemented in `internal/substitutes` ([specialty-ingredients.md](specialty-ingredients.md)).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `specialty_ingredients` (global) | slug, key, name, aliases[], aliasKeys[], category, unitSizes[{per, quantity, quantityValue, unit}], defaultOptionId, options[{id, type, name, notes, per{quantity, quantityValue, unit}, ingredients[{name, quantity, quantityValue, unit, category}], steps[], yield{}, shelfLifeDays}], seedVersion, contentHash, retired, createdAt, updatedAt | **unique** `{slug}`; **unique** `{key}` |
| `specialty_options` | householdId, specialtyId (slug), type, name, notes, per{}, ingredients[], steps[], yield{}, shelfLifeDays, basedOnOptionId, createdBy, updatedBy, createdAt, updatedAt | `{householdId, specialtyId, createdAt}` |
| `specialty_choices` | householdId, specialtyId (slug), optionId (curated slug, option ObjectID hex, or `as_is`), chosenBy, chosenAt | **unique** `{householdId, specialtyId}`; `{householdId, optionId}` |

- **Curated data is seeded.** `specialty_ingredients` is written only by the
  seed sync (startup and `cmd/seedspecialties`): an upsert on `slug` that
  `$setOnInsert`s `_id` and `createdAt`, guarded by `contentHash` so unchanged
  specialties aren't written. Specialties the seed dropped get `retired: true`
  and are never deleted. The collection is small (tens of documents) and read
  whole.
- Curated options are embedded (bounded by the seed); household options are
  their own documents, at most 10 per household and specialty.
- **Exact amounts** as elsewhere: `quantity` strings are authoritative,
  `quantityValue` is a double copy.
- A choice is one upserted document per household and specialty; applying
  defaults uses `$setOnInsert`, so it never changes an existing choice.
  Deleting a household option `deleteMany`s the choices of it.
- **No flag on the catalog.** Whether an ingredient is a specialty ingredient
  is decided at read time by comparing catalog keys and normalized names with
  `key` and `aliasKeys`.
- **Batches live in the pantry**: the batch is the `pantry_items` document
  whose `key` is the specialty's `key`, and each batch made is a
  `pantry_purchases` document with `source: house_made`.

### Notifications

Implemented in `internal/notifications`.

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `notifications` | householdId, type (`pantry.low`), title, body, subject{kind, id}, dedupeKey, readBy[] (user IDs), push{status (`pending`/`sent`/`failed`/`skipped`), attempts, lastAttemptAt, sentAt}, createdAt | **unique** `{householdId, dedupeKey}`; `{householdId, _id: -1}`; **partial** `{push.status, createdAt}` where `push.status` is `pending` |

- **Household-wide, read per member.** Marking read is one `updateMany` with
  `$addToSet: {readBy: userId}`. Unread means `readBy` doesn't contain the
  caller, counted with `countDocuments`.
- Lists page newest first by `_id` with a `before` cursor (the last `_id` of
  the previous page).
- **Idempotent creation.** A producer's retry hits the unique `dedupeKey` and
  gets the existing notification.
- **Push outbox.** Every notification is written with `push.status: pending`.
  The partial index is for a future APNs worker to take pending
  notifications oldest first ([pantry-usage.md](pantry-usage.md#future-apns-hook)).
- Retention: kept indefinitely for now, like events.

### Grocery lists

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `grocery_lists` | householdId, weeklyPlanId, items[], generatedAt | **unique** `{weeklyPlanId}` |

A week's grocery list is still computed on request, with the household pantry
applied, and isn't stored yet. Store settings (planned as
`grocery_provider_configurations`) are `shopping_settings` below.

### Shopping

Implemented in `internal/shopping` (Phase 8a, [shopping-providers.md](shopping-providers.md)).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `shopping_settings` | householdId, provider (`walmart`), storeId, updatedBy, updatedAt | **unique** `{householdId}` |
| `shopping_product_preferences` | householdId, provider, ingredientKey, ingredientName, productId, displayName, packageSize{quantity, quantityValue, unit}, createdBy, createdAt, updatedBy, updatedAt | **unique** `{householdId, provider, ingredientKey}` |
| `shopping_handoffs` | householdId, week, provider, storeId, lines[{id, ingredientKey, name, category, amounts[{quantity, quantityValue, unit}], unquantified, groceryStatus, productId, productName, packageSize{}, computedPackages, packages, reason, status (`pending`/`confirmed`/`skipped`), claimedAt, confirmedPackages, purchaseId, confirmedBy, confirmedAt, skippedBy, skippedAt}], excluded[{ingredientKey, name, category, amounts[], unquantified, groceryStatus, reason}], links[{url, lineIds[], itemCount}], affiliateTracked, createdBy, createdAt, updatedAt | `{householdId, createdAt: -1}`; `{householdId, week, createdAt: -1}` |
| `shopping_store_requests` | householdId, key, name, note, requestedBy, requestedAt, updatedAt | **unique** `{householdId, key}`; `{householdId, requestedAt: -1}`; `{key}` |

- **Settings** are one document per household, replaced with an upsert.
- **Store requests** are the grocers and delivery services a household asked
  DinnerOS to support: Phase 8a's demand signal, not a provider. They are
  upserted on the unique key, so asking twice updates `note` and `updatedAt`
  only — `_id`, `requestedBy`, and `requestedAt` are `$setOnInsert`. `key` is
  a catalog key (`kroger`) or one normalized from a typed name
  (`some-local-market`). The catalog itself lives in code, not in the
  database, so `status` is never stored (it's read from the catalog) and
  `name` matters for the free-text case. At most 25 per household. `{key}`
  backs the global count, a `$group` over the collection.
- **Saved products** are upserted on the unique key: `_id`, `createdBy`, and
  `createdAt` are `$setOnInsert`. `ingredientKey` is the grocery line key (a
  catalog ingredient ID hex, or `name:` + a normalized name). `productId` is
  the Walmart item ID read from a pasted link; the link itself isn't stored.
  At most 1,000 per household and provider. `packageSize` is absent when the
  member didn't give one, and `quantity` is exact like elsewhere.
- **Handoffs** are a snapshot of the match when the member handed off, so a
  later plan or pantry change doesn't alter what's confirmed. Lines are
  bounded by the week's list. Line IDs (`l1`, `l2`, …) are unique within a
  handoff. The status (`open` while a line is `pending`) is derived, and
  `?status=` filters on `lines.status`.
- **Confirming a line** is a positional update on `lines.$`: a conditional
  `claimedAt` (no claim, or one older than a minute, on a line that isn't
  confirmed), then the pantry purchase, then `status: confirmed` with its
  `purchaseId`. The purchase is a `pantry_purchases` document with `source:
  provider` and `provider{key, handoffId, lineId, productId}`, whose unique
  partial index makes a line bought at most once (see
  [Pantry usage](#pantry-usage)).
- Handoffs are kept indefinitely for now, like purchases.

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

### Autopilot

Implemented in Phase 10 (`internal/recommendations`; see [autopilot.md](autopilot.md)).

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `autopilot_profiles` | householdId, taste{likes, dislikes: {cuisines[], tags[], proteins[]}}, restrictions{diets[], allergens[], excludedIngredients[], excludedCuisines[], excludedProteins[], excludedTags[], noSpicy}, schedule{planDays[], weeknights[], mealsPerWeek, defaultServings, weeknightMaxMinutes}, cookTime{quickMaxMinutes, mediumMaxMinutes, maxLongPerWeek, minQuickPerWeek, avoidConsecutiveLong}, novelty, equipment[], weekdayRules[] (day, label, cuisines, tags, proteins, methods, timeBand, frequency), sections{<section>: {updatedBy, updatedAt}}, version, createdBy, createdAt, updatedBy, updatedAt | **unique** `{householdId}` |
| `autopilot_week_contexts` | householdId, week, skip, busy, mealsPerWeek, maxMinutes, servings, days[] (day, skip, maxMinutes, servings), note, version, updatedBy, updatedAt | **unique** `{householdId, week}` |
| `autopilot_proposals` | householdId, week, proposalId, status (`proposed`/`accepted`/`rejected`), version, attempt, modelVersion, inputsHash, requested, planned, candidates, coldStart, slots[] (id = day, day, recipeId, recipeName, recipeImageUrl, cookMinutes, timeBand, servings, score, signals{}, reasons[] (code, text), swapCount, rejectedRecipeIds[]), unfilled[], messages[], objective{}, swapCount, excludedSlots[], generatedBy, generatedAt, updatedAt, decidedBy, decidedAt | **unique** `{householdId, week}` |
| `autopilot_recipe_overrides` | householdId, recipeId, methods{<method>: bool}, mealCategories{<category>: bool}, updatedBy, updatedAt | **unique** `{householdId, recipeId}` |
| `autopilot_week_pairings` | householdId, week, decisions[] (entryId, key, status (`accepted`/`dismissed`), addedEntryId, groceryItemId, decidedBy, decidedAt), groceryItems[] (id, key, name, quantity, unit, forEntryId, forRecipeId, forRecipeName, source, ruleId, addedBy, addedAt), version, updatedAt | **unique** `{householdId, week}` |

- Every read is by its unique key, so no other indexes are needed. User and
  recipe IDs are ObjectIDs; `proposalId` is an ObjectID that changes each time
  the week is generated, while the document (and `_id`) stays.
- **Versioned documents.** Profiles, week contexts, and proposals carry
  `version`. A first write inserts (the unique index rejects a concurrent
  second insert); later writes replace the document on
  `{householdId, [week,] version}` and store `version + 1`. Profile and context
  writes are read-modify-write and retry up to 5 times; proposal writes
  surface the conflict as `409 proposal_changed`.
- Everything is bounded: profile lists have at most 30–50 values and 7 rules,
  a proposal at most 7 slots, and each slot at most 20 swapped-out recipes.
- Only the latest proposal per week is kept. Earlier ones, every swap, and
  every preference change survive as events (`week.*`, `meal.*`,
  `autopilot.*`), so there is no separate history collection.
- An override with no methods and no meal categories is deleted rather than
  stored empty.
- **Pairings** ([autopilot.md](autopilot.md#add-on-pairings)) are stored in
  three places: the profile's `pairings[]` rules (id, label, when{mealCategories,
  cuisines, tags, proteins}, recipeId + recipeName **or** groceryItem{name,
  quantity, unit}, frequency), a snapshot on each proposal slot
  (`slots[].pairings[]`, so a review renders without recomputing), and the
  week's decisions and grocery items in `autopilot_week_pairings`.
- A week's document is versioned like the others, and accepting claims its
  decision before the plan changes, so two members accepting the same pairing
  add it once. Bounds: 20 rules per household, 200 decisions and 50 grocery
  items per week, 3 pairings per meal.
- A paired grocery item names the plan entry it is for. It leaves the grocery
  list when that entry is unplanned, without being deleted, so replanning the
  meal brings it back.

## Multi-tenancy

Inside DinnerOS, the **household** is the isolation boundary. Tenant isolation
(`tenantId` on every document and in every query) belongs to the future Autopilot
service, where DinnerOS itself is one tenant. DinnerOS does not add a `tenantId`
to its own collections until it is needed.
