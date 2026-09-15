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

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `users` | displayName, primaryEmail, createdAt | — |
| `auth_identities` | userId, provider (`apple`/`google`), subject, email, emailVerified | **unique** `{provider, subject}`; `{userId}` |
| `sessions` | userId, refreshTokenHash, familyId, expiresAt, revokedAt | **unique** `{refreshTokenHash}`; `{userId}`; TTL on `expiresAt` |

### Households

| Collection | Key fields | Indexes |
| --- | --- | --- |
| `households` | name, defaultServings, timeZone | — |
| `household_memberships` | householdId, userId, role | **unique** `{householdId, userId}`; `{userId}` |
| `household_invitations` | householdId, email (normalized), role, tokenHash, codeHash, expiresAt, acceptedAt, revokedAt, createdBy | **unique** `{tokenHash}`; **unique** `{codeHash}`; `{householdId, email}` |

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
