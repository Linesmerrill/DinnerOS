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

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `recipes` | name, source, sourceRecipeId, sourceURL, servings, times, cuisine, tags, ingredients[] (RecipeIngredient), instructions[], historicalOrderDates[] | **unique partial** `{source, sourceRecipeId}`; `{tags}`; `{cuisine}` |
| `ingredients` | name, normalizedName, aliases[], category, defaultPurchaseUnit, searchTerms[] | **unique** `{normalizedName}`; `{aliases}` |
| `ingredient_review_queue` | rawText, recipeId, candidates[], status | `{status, createdAt}` |

A recipe's ingredient lines and instructions are small and bounded, so they are
embedded. Embedded ingredient lines *reference* canonical `ingredients` by ID.

Ownership: recipes imported from our personal history belong to a household
(`householdId`). A future shared or partner catalog will use an explicit
`catalogId` rather than overloading `householdId`.

### Planning, pantry, grocery

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `pantry_items` | householdId, ingredientId | **unique** `{householdId, ingredientId}` |
| `weekly_plans` | householdId, weekStart, entries[] (MealPlanEntry: date, recipeId, servings, status, source) | **unique** `{householdId, weekStart}` |
| `grocery_lists` | householdId, weeklyPlanId, items[], generatedAt | **unique** `{weeklyPlanId}` |
| `grocery_provider_configurations` | householdId, provider, settings | **unique** `{householdId, provider}` |

A week has at most a few dozen entries, so they are embedded in the plan.

### Behavior

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `meal_ratings` | householdId, userId, recipeId, planEntryRef, rating, wouldMakeAgain, notes | `{householdId, recipeId}`; **unique** `{householdId, userId, planEntryRef}` |
| `meal_events` | householdId, userId, type, recipeId, weeklyPlanId, occurredAt, context{} | `{householdId, occurredAt}`; `{householdId, type, occurredAt}` |

Events are append-only. They are designed to be forwarded to Autopilot later
(see [autopilot.md](autopilot.md)).

## Multi-tenancy

Inside DinnerOS, the **household** is the isolation boundary. Tenant isolation
(`tenantId` on every document and in every query) belongs to the future Autopilot
service, where DinnerOS itself is one tenant. DinnerOS does not add a `tenantId`
to its own collections until it is needed.
