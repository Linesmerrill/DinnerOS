# Architecture

This document describes how DinnerOS is structured and why. It is the living
companion to the master project brief. When a decision changes, update the
[decision log](#decision-log).

## System overview

```text
┌───────────────────────┐       HTTPS / JSON        ┌──────────────────────────────┐
│  iOS app (SwiftUI)    │ ───────────────────────▶  │  DinnerOS API (Go, Heroku)   │
│  - Keychain tokens    │      /api/v1/...          │  modular monolith            │
│  - SwiftData cache    │ ◀───────────────────────  │                              │
└───────────────────────┘                           │  auth · users · households    │
         │                                          │  invitations · recipes       │
         │ Sign in with Apple / Google              │  ingredients · planning      │
         ▼                                          │  grocery · providers         │
┌───────────────────────┐   identity token verify   │  events · recommendations    │
│ Apple / Google IdP    │ ◀──────────────────────── │                              │
└───────────────────────┘                           └──────┬───────────┬───────────┘
                                                           │           │
                                              MongoDB Atlas│           │ seams (interfaces)
                                              (authoritative)          ▼
                                                           │   EmailProvider → Resend
                                                           │   GroceryProvider → Manual / Instacart / Walmart
                                                           │   RecommendationProvider → local → Autopilot (private)
                                                           ▼
                                                    ┌──────────────┐
                                                    │   MongoDB    │
                                                    └──────────────┘

┌─────────────────────────────┐
│ importers/hellofresh (CLI)  │ ── owner-only import ──▶ DinnerOS API
└─────────────────────────────┘
```

## Principles

1. **The backend is authoritative.** MongoDB holds the source of truth, and every
   authorization decision is made server-side. The iOS app caches data but never
   decides access.
2. **Modular monolith, not microservices.** One deployable, with clear package
   boundaries so a module can be extracted later. Autopilot is the only planned
   extraction.
3. **Deliberate seams only.** Interfaces exist where multiple implementations are
   *known* to be coming (table below). Everything else is concrete code.
4. **Deterministic core.** Grocery arithmetic, unit conversion, authorization, and
   constraint enforcement are plain, tested code. They never go through an LLM.
5. **Public code, private data.** The repository is public. Personal data, imported
   recipes, and proprietary Autopilot algorithms are not.
6. **The name is configuration.** "DinnerOS" is a working title (see README).

## Backend (`api/`)

```text
api/
├── cmd/server/          main: load config → build logger → router → graceful shutdown
├── internal/
│   ├── config/          environment-driven configuration + validation
│   ├── httpapi/         router assembly, middleware, /health and /ready, route mounting
│   ├── platform/        domain-free infrastructure that domain packages may import
│   │   ├── httpx/       JSON responses, error envelope, strict request decoding
│   │   ├── logging/     slog construction; adds request ID from context
│   │   ├── mongodb/     client lifecycle, IndexSet management, error translation (mongotest/: test DB helper)
│   │   ├── ratelimit/   token-bucket middleware keyed by client IP or user
│   │   └── requestid/   request correlation ID in context
│   ├── auth/            provider token verification, DinnerOS sessions
│   ├── users/           User, AuthIdentity
│   ├── households/      Household, HouseholdMembership, roles/permissions
│   ├── invitations/     HouseholdInvitation, EmailProvider
│   ├── recipes/         Recipe, instructions, source metadata
│   ├── ratings/         per-member recipe ratings, household aggregates
│   ├── ingredients/     Ingredient, units, quantities, conversion
│   ├── planning/        Week plans, entries, week grocery list
│   ├── pantry/          PantryItem, default staples, pantry snapshot for grocery lists
│   ├── grocery/         aggregation engine, GroceryList
│   ├── providers/       GroceryProvider implementations
│   ├── events/          append-only behavior events, Recorder, client ingestion
│   └── recommendations/ RecommendationProvider (local now, Autopilot later)
├── pkg/                 domain-free reusable code (empty until justified)
└── migrations/          index + data migration conventions
```

### Module shape

Each domain package grows into the same small, boring layout as it is built:

```text
internal/<domain>/
├── model.go        domain types and invariants (no HTTP, no Mongo types leaking out)
├── service.go      use cases; depends on interfaces for storage and collaborators
├── store.go        repository interface
├── mongo_store.go  MongoDB implementation + index definitions
├── handler.go      HTTP handlers (decode → service → encode)
└── *_test.go
```

Dependency rules:

- `httpapi` depends on domain packages (to mount handlers). Domain packages never
  depend on `httpapi`. They use `platform/httpx` for responses and decoding.
- `platform/*` packages never import domain packages.
- Domain packages talk to each other through small service interfaces, never by
  reaching into another package's store or collections.
- `cmd/server` is the only place where concrete implementations are chosen and
  wired together.

### Deliberate seams

| Interface | Implementations | Phase |
| --- | --- | --- |
| `AuthProvider` (`auth.IdentityVerifier`) | Apple, Google ✅ | 2 |
| `EmailProvider` | Resend, log-only (local/test) ✅ | 3 |
| `RecipeSourceImporter` | HelloFresh; later manual/import/partner | 5 |
| `GroceryProvider` | Manual; Instacart and Walmart via official APIs only | 8 |
| `RecommendationProvider` | Local deterministic → remote Autopilot | 10–12 |

### Cross-cutting concerns

- **Middleware order:** `RequestID` → `RequestLogger` → `Recoverer` →
  `MaxBodyBytes` → routes. A route that needs a different body limit replaces
  it with `httpx.OverrideBodyLimit`, placed after authorization. Wrapping the
  body in a second `http.MaxBytesReader` would enforce the smaller limit.
- **Logging:** `log/slog`, text locally and JSON in production. Each request
  produces one line with request ID, method, **route pattern**, status, bytes, and
  latency. The raw path and query string are never logged because they can
  contain tokens. `/health` and `/ready` log at debug level. Tokens, cookies, and
  credentials are never logged, and `Config` redacts connection-string passwords.
  Handlers log with `*Context` methods so the request ID is attached automatically.
- **Startup:** the API connects to MongoDB, pings it, and ensures indexes before
  listening. If that fails, it exits (fail fast). On SIGTERM it drains in-flight
  requests for up to 25 seconds.
- **Errors:** every non-2xx response uses `{"error": {"code", "message"}}`.
- **Limits:** request body size (`HTTP_MAX_BODY_BYTES`), server timeouts, and
  per-IP rate limiting on auth endpoints, invitation acceptance, and invitation
  creation, plus per-user rate limiting on event ingestion. Each has its own
  limiter instance. The recipe import route has its
  own body limit (`RECIPE_IMPORT_MAX_BYTES`) and extends the server's read and
  write deadlines for that request with `http.ResponseController`.
- **Household authorization:** routes under `/households/{householdId}` use
  `households.RequirePermission`, which returns 404 to non-members and 403 to
  members lacking the permission, and stores the membership in the request
  context for the handler and service.
- **Behavior events:** modules that observe household behavior record it
  through `events.Recorder` (implemented by `events.Service`), usually with
  `events.RecordOrLog`. Recording is best effort: a failure is logged and never
  fails the user's action. `cmd/server/behavior.go` wires the events and
  ratings modules.
- **Authentication:** domain handlers mount under `/api/v1` through
  `httpapi.Options.APIRoutes` and wrap protected routes in `auth.RequireAuth`,
  which puts the user ID in the request context (`auth.UserIDFromContext`).
- **Versioning:** product routes live under `/api/v1`. Operational routes
  (`/health`, `/ready`) are unversioned.

## iOS (`ios/`)

```text
ios/DinnerOS/
├── App/          entry point, AppConfiguration, dependency container
├── Core/         networking (APIClient), auth session, Keychain, persistence (added as needed)
├── Features/     one folder per feature: Week, Recipes, Shop, Household, Root
└── Resources/    asset catalog, string catalogs
```

- **SwiftUI + Observation** (`@Observable` models), async/await, `URLSession`.
- **Swift 6** language mode with `MainActor` default isolation. Networking and
  parsing run off the main actor explicitly.
- **SwiftData** is a local cache for offline reads and fast launch. It is never
  the source of truth. Server responses replace cached state.
- **Keychain** stores DinnerOS access and refresh tokens. No provider secrets or
  API keys ship in the app.
- **Configuration** flows from `Config/*.xcconfig` → `Info.plist` →
  `AppConfiguration`: display name, environment, API base URL, and Google iOS
  client ID. A missing API URL shows a configuration error screen and doesn't
  crash.
- **Dependencies:** `App/AppDependencies` is the only place that picks concrete
  implementations (`URLSessionTransport`, `KeychainTokenStore`). It injects
  `AuthSession` and `GoogleSignInService` through the SwiftUI environment.
- **Networking (`Core/Networking`):** `APIClient` is a `Sendable` struct over an
  injectable `HTTPTransport`. Its `@concurrent` methods encode, send, and decode
  off the main actor. Every non-2xx response becomes a typed `APIError` built from
  the `{error: {code, message, requestId}}` envelope.
- **Auth (`Core/Auth`):** `AuthSession` is `@Observable` and main-actor-isolated.
  It owns the tokens, restores the session at launch, and runs exactly one
  in-flight refresh that all callers share. Sign in with Apple, Google OAuth with
  PKCE through `ASWebAuthenticationSession`, and a Debug-only developer sign-in
  all end in `AuthSession.signIn`. See
  [authentication.md](authentication.md#ios-client).
- **Root flow:** `RootView` shows `restoring` (splash), then `SignInView`, the
  signed-in root, or the configuration error, based on `AuthSession.state`.
  `SignedInRootView` shows household onboarding until the user belongs to a
  household, then the tab shell.
- **Households (`Core/Households`):** `HouseholdsAPI` wraps the household and
  invitation endpoints. `HouseholdStore` (`@Observable`, main actor) loads the
  user's households after every sign-in, remembers the selected household per
  user in `UserDefaults`, and reloads from the server after each change.
  `HouseholdAccess` turns the response's `permissions` into which actions the UI
  shows. The API still authorizes every request. Invitation links
  (`dinneros://invite?token=...`) arrive through `onOpenURL`. The app asks before
  joining, and holds a link opened while signed out in memory until sign-in.
  Tokens and invite codes are never logged.
- **Planning (`Core/Planning`):** `PlansAPI` wraps the week plan and grocery
  list endpoints. `PlanStore` (`@Observable`, main actor, app lifetime) holds the
  week shown in the Week tab, which starts at this week in the household's time
  zone. Changes apply the plan the API returns; a delete, or a `403`/`404`/`409`,
  reloads the week. `GroceryListModel` backs one grocery list screen, keeps
  check-offs on the device, and exports the list as plain text.
- **Ratings (`Core/Recipes`):** recipe summaries and details carry
  `householdRating` and `myRating`. `RatingDraft` enforces the API's rating
  rules while the user edits. `RecipeLibrary` saves or removes a rating, then
  reloads that recipe so the cache and the list row show the server's average.
- **Events (`Core/Events`):** `EventReporter` (`@Observable`, main actor, app
  lifetime) records `recipe.viewed`, `recipe.cooked`, `recipe.skipped`, and
  `grocery.item_checked` into a JSON queue in Application Support. It sends
  batches when the app goes to the background, every 60 seconds while active,
  and at 20 queued events. The queue belongs to one user and household.
- **Navigation:** a tab shell (Week, Recipes, Shop, Household) with
  `NavigationStack` per tab, native sheets, and forms.
- **Quality bar:** Dynamic Type, VoiceOver labels, dark mode, and explicit
  loading, empty, and error states for every screen.
- **Visual identity:** Apple-native structure with DinnerOS's own herb-green
  accent (`AccentColor` in the asset catalog, with a lighter dark-mode variant).
  No HelloFresh visual cloning.

The Xcode project uses file-system-synchronized folders (Xcode 16+). Adding a
Swift file to `ios/DinnerOS/` or `ios/DinnerOSTests/` needs no project-file edits,
which keeps `project.pbxproj` small and merge-friendly.

## Autopilot boundary

DinnerOS calls recommendations only through `RecommendationProvider`. The first
implementation is local and deterministic. When the engine proves useful, its
proprietary logic moves to a private service. DinnerOS then becomes tenant #1 of
a versioned Autopilot API. See [autopilot.md](autopilot.md).

## Decision log

| # | Decision | Rationale |
| --- | --- | --- |
| 1 | Go modular monolith with `chi` + `log/slog` | Small, idiomatic, no framework lock-in; stdlib-compatible handlers |
| 2 | MongoDB Atlas as authoritative store | Brief requirement. Indexes are designed per collection, not left to collection scans. |
| 3 | Official MongoDB Go driver v2 | Current supported driver |
| 4 | Hand-maintained Xcode project with synchronized folders (no XcodeGen/Tuist) | No extra tool to install. Modern Xcode removes most project-file churn. |
| 5 | iOS 18.0 minimum, Swift 6, Swift Testing | Modern SwiftUI (`Tab`, `@Entry`, Observation, SwiftData) for a private TestFlight audience; easy to raise |
| 6 | iPhone-only initially (`TARGETED_DEVICE_FAMILY = 1`) | Focus. iPad can come later. |
| 7 | swift-format (bundled with Xcode) instead of SwiftLint | Zero install, deterministic formatting |
| 8 | staticcheck + go vet + gofmt instead of golangci-lint | Fewer moving parts. Revisit if more linters are needed. |
| 9 | Fastlane + App Store Connect API key + Xcode cloud-managed signing | No certificates or profiles in Git or a match repo. The key lives in GitHub Secrets. |
| 10 | Opaque DinnerOS tokens separate from provider identities | Multiple login methods per user; provider data never becomes our identity |
| 11 | No license yet | Owner has not chosen one |
| 12 | Shared HTTP/Mongo helpers live in `internal/platform/*` | Domain handlers need response conventions without importing the router package, which would create an import cycle |
| 13 | Heroku container deploy (`heroku.yml` + `api/Dockerfile`) | The same image is built in CI and locally, with no third-party monorepo buildpack. The runtime is non-root Alpine rather than distroless because Heroku launches `CMD` via `/bin/sh -c`; CI checks this. |
| 14 | No CORS middleware | The only client is the native iOS app, which doesn't need CORS. Add it when a browser client exists. |
| 15 | `github.com/golang-jwt/jwt/v5` for JWTs; JWKS fetching written in-house (`internal/auth/jwks.go`) | jwt/v5 is the de facto Go JWT library and supports a strict algorithm allowlist (`RS256` for providers, `HS256` for our tokens), which blocks algorithm-confusion attacks. We only need RSA keys from two fixed URLs, so a ~150-line client is simpler to audit than a JOSE suite (`lestrrat-go/jwx`) or `keyfunc`. It adds a 1h cache, refetches on unknown `kid` at most once a minute, and serves stale keys during a provider outage. |
| 16 | Opaque refresh tokens with rotation and family-wide reuse detection, no grace window | Only the SHA-256 hash is stored. Replaying a rotated or revoked token revokes the whole sign-in. Two concurrent refreshes with the same token count as reuse, so the iOS client must serialize refreshes. We'd rather force a sign-in than ignore a possible token theft. |
| 17 | Logout revokes the whole token family, not just one session | A family has only one live session, so normal logout behaves the same. But a stale client logging out with an older token still ends the sign-in instead of leaving the newer session alive. |
| 18 | In-memory per-IP token-bucket rate limit on `/auth/*` (`internal/platform/ratelimit`, `golang.org/x/time/rate` v0.12.0) | One dyno today, so no shared store is needed. The client IP is the *last* `X-Forwarded-For` entry, which Heroku's router appends and clients can't forge. Each dyno limits separately; move to a shared store if we scale out. x/time is pinned below v0.13 because newer releases require Go 1.26. |
| 19 | `POST /api/v1/auth/dev` gated by `APP_ENV=development` **and** `AUTH_DEV_LOGIN_ENABLED=true` | Simulator and integration testing need sessions without Apple or Google. Config validation refuses to start production with the flag set, and the route isn't mounted otherwise. |
| 20 | `IdentityVerifier` returns `users.VerifiedIdentity`; `auth` depends on `users`, not the reverse | This is the `AuthProvider` seam. `GET /me` lives in the auth handler because it needs the auth context, and putting it in `users` would create an import cycle. |
| 21 | Google sign-in on iOS uses OAuth 2.0 authorization code + PKCE through `ASWebAuthenticationSession`, not the GoogleSignIn SDK | We only need an ID token. The flow is about 200 lines of auditable code: authorize URL, `state` check, form-encoded token exchange. It avoids a third-party dependency, its transitive pods or packages, its privacy manifest, and its release schedule. It also keeps the "Apple frameworks only" rule. The trade-off is that we build the button and handle Google's endpoints ourselves. Both are stable, documented OAuth endpoints. |
| 22 | iOS token refresh is serialized on the main actor with one shared `Task` | The API revokes a sign-in when the same refresh token is used twice (decision 16). Main-actor state makes "check for an in-flight refresh, else start one" atomic without locks. A refresh takes one network round trip, so hopping through the main actor costs nothing noticeable. |
| 23 | Launch refresh failures sign out only when the server rejects the refresh token | If the app signed out on network errors, opening it offline would discard a refresh token that is still valid (60-day sliding). A `400`/`401` from `/auth/refresh` still clears the Keychain. |
| 24 | Developer sign-in compiled only into `DEBUG` builds and shown only when `AppEnvironment == development` | Simulator testing needs a session without Apple or Google. `#if DEBUG` keeps the code path out of Release binaries, on top of the server-side gating in decision 19. |
| 25 | Roles map to permissions in one table (`households.rolePermissions`); code asks `Role.Can`, and granting requires `Role.Covers` | New roles (viewer, shopper, child, guest) are a table row, not a hunt for `isAdmin` checks. `Covers` stops anyone from granting more access than they hold, without special-casing admin. |
| 26 | The last-admin guard uses a conditional `adminCount` decrement on the household, not a transaction | Two admins demoting each other at once can't both succeed. It works on standalone local MongoDB like the Phase 2 code, and crash drift only makes the guard stricter. |
| 27 | The last admin can't leave, even as the only member | This keeps "every household has an admin" unconditional and avoids orphaned households nobody can see. A lone user can rename the household; deleting households is a later feature. |
| 28 | `households.RequirePermission` middleware returns 404 to non-members and 403 to under-privileged members; services re-check permissions on the membership they're given | Hides household existence and loads the membership once per request. The service check keeps authorization correct for non-HTTP callers and is covered by unit tests. |
| 29 | Invitations have a link token and a separate short code, both stored only as SHA-256 hashes | The token (256 bits) stays in the email link. The code (50-bit Crockford base32, with O/I/L read as 0/1/1) can be read aloud or texted, and per-IP rate limiting on accept makes guessing it infeasible. Unsalted SHA-256 is fine at this entropy and allows lookup by hash. |
| 30 | Accepting an invitation doesn't require the signed-in email to match the invited address | Apple private relay addresses and people using a different account would otherwise be locked out. Holding the secret is the proof. |
| 31 | Accept marks the invitation used (conditionally) before creating the membership, and undoes that if joining fails | Two users racing with the same code can't both join. The same user retrying gets their membership back, but a used invitation can't be replayed to rejoin after leaving. |
| 32 | One pending invitation per household and email, enforced by a partial unique index on a `pending` flag | Re-inviting revokes the previous invitation. The index makes that hold under concurrency and also serves the pending list. |
| 33 | Email failures don't fail invitation creation (`emailDelivered: false`) | The admin already has the code, so a Resend outage shouldn't block inviting. |
| 34 | `EMAIL_PROVIDER=log` for development; production requires Resend. `time/tzdata` is embedded | Local development and tests never send email or need a key. The Alpine runtime image has no zoneinfo, so `time.LoadLocation` would reject every time zone without the embedded data. |
| 35 | The recipe import route replaces the body limit (`httpx.OverrideBodyLimit`, `RECIPE_IMPORT_MAX_BYTES`) and extends its own deadlines with `http.ResponseController`, rather than raising the global limit or server timeouts | Only one route legitimately receives 10–15 MB. Every other route keeps the 1 MB cap and 15s/30s timeouts, and the override runs after authorization, so anonymous clients never get the larger allowance. The initial bulk load uses `cmd/importrecipes`, which bypasses HTTP and Heroku's 30-second router timeout. |
| 36 | Recipe identity is `(householdId, source, sourceRecipeId)` plus `sourceAliases`; imports compare the merged recipe with the stored one and bulk-write only changes | Sources republish recipes under new IDs (weekly menu clones), so aliases prevent duplicates. Comparing first makes re-imports no-ops, and bulk writes keep a 1000-recipe import to a handful of round trips. |
| 37 | The ingredient catalog is global and keyed by normalized name; unmatched categories are stored as `other` with `categoryConfident: false` | Ingredients mean the same thing in every household, and grocery aggregation needs one ID per ingredient. Flagging instead of guessing keeps the core deterministic. |
| 38 | `recipes.Service` takes a household ID, not a membership | HTTP routes authorize with `households.RequirePermission` (`household.view`, `recipes.import`). The `importrecipes` command has no signed-in user; it's an operator tool with direct database access, and it checks that the household exists. |
| 39 | The iOS app hides household actions using the `permissions` in responses, plus a copy of the role table (`HouseholdAccess`) to decide which roles a user may grant | Responses list the caller's permissions but not other roles', and `Covers` needs both. The copy only picks what the UI offers: if it drifts from the server, the worst case is a hidden action or a `403`, never extra access. Roles the app doesn't know get no actions. |
| 40 | The selected household ID is stored in `UserDefaults`, keyed by user ID | It isn't a secret, so the Keychain isn't needed. Keying by user means another account on the same device starts fresh. A stale ID falls back to the first household. |
| 41 | Invitation links use a custom URL scheme (`APP_URL_SCHEME`, default `dinneros`) registered through `Info.plist`, not universal links | It matches the API's default `APP_INVITE_URL_BASE` and works without a hosted domain or associated-domains entitlement. The scheme is configuration, so universal links can replace it later. |
| 42 | Opening an invitation link asks before joining. A link opened while signed out waits in memory, not on disk, until sign-in | Joining shares your name with the household and switches your selected household, so a link from a stranger must not do that silently. The token is a secret, so it isn't persisted; if the app quits before sign-in, tap the link again. |
| 43 | `HouseholdStore` reloads from the server after every change, and after a `403`/`404`/`409`, instead of patching local state | The requests are few and small. The screen always matches the server, including after concurrent changes by other members. |
| 44 | The iOS recipe detail shows only the amounts the source authored for each serving size, formatted from the exact `quantity` string (`quantityValue` only as a fallback), and never scales them | Sources don't scale linearly (4 servings may be ¾ oz, not 1 oz), and the grocery engine works from the same authored amounts. A size with no authored amount shows none instead of an invented one. |
| 45 | `RecipeLibrary` is an app-lifetime, in-memory store for the current household's list and loaded recipes; any filter change restarts from the first page, and a generation counter drops responses for old filters or households | Returning from a recipe or another tab shows what was loaded without refetching. Cursors are only valid for the filters that produced them. Nothing personal is cached on disk, and switching households or signing out clears it. |
| 46 | `APIClient` percent-encodes query items with RFC 3986 unreserved characters only | `URLQueryItem`'s default encoding leaves `+`, which Go's query parser reads as a space, so a search for "salt+pepper" would silently change. |
| 47 | A week plan is one `weekly_plans` document per `(householdId, week)`, keyed by the ISO week string (`2026-W38`), with entries embedded and capped at 50 | One read renders a week. Zero-padded week strings sort in week order, so the unique index also serves range queries. ISO weeks involve no time zone. |
| 48 | Concurrent plan edits use atomic single-document updates (`$push` guarded by `status: draft` and `entries.49: {$exists: false}`, positional `$set`/`$unset`, `$pull`), not a version field with `409 conflict` | Members editing the same week never lose writes and never have to reload and retry. A plan created by two first writes at once hits the unique index, and the loser retries once. The cost: when a conditional update matches nothing, a follow-up read decides between `404`, `409 plan_finalized`, and `409 plan_full`. |
| 49 | Entries snapshot the recipe's name and image; recipe details and the grocery list always read the live recipe | Plans render without loading recipes. An entry whose recipe left the household, or no longer offers the entry's serving size, is listed in the grocery list's `skipped` instead of failing the list. |
| 50 | Entry servings must be one of the recipe's authored sizes, and the grocery list uses those authored amounts unscaled (`RecipeServings == TargetServings`) | Same reason as #44: sources don't scale linearly. Scaling from another size would invent amounts. |
| 51 | A finalized plan locks its entries (`409 plan_finalized`) until `PUT .../status` sets it back to `draft` | Finalized means the week's shopping is decided, so the list shouldn't change silently. Reopening is one deliberate call. |
| 52 | `GET /plans/{week}` returns an empty draft for unplanned weeks, and `GET /plans` returns every week in the range (at most 26), not only stored plans | Clients render a calendar without special-casing missing weeks, and reading never writes. |
| 53 | A `recipeId` in a plan request body that isn't in the household is `400 validation_failed`, not `404` | The path's household and week exist; the body is what's wrong. `404` stays reserved for the path (non-members, unknown entries). |
| 54 | The week grocery list is computed on request with `recipes.Service.GetMany` (two round trips for the whole week) and an empty pantry | Nothing is persisted until Phase 7 adds the pantry and saved lists with checked state. Batch loading keeps a full week's cost constant instead of two queries per entry. |
| 55 | A pantry has one item per ingredient: unique `{householdId, key}`, where `key` is the normalized name or the catalog key. `POST .../pantry` merges into an existing item instead of adding a second one | The grocery engine decides per ingredient, so two items ("1 lb flour" in stock, "flour" out) would contradict each other. A merge applies only the fields sent, so a retried add is harmless. Different units of one ingredient share one item and one amount. |
| 56 | Pantry status alone decides grocery status: `in_stock` → `inPantry`; `out` → `toBuy` even for staples; `low` → the engine default (`pantryHint` when every recipe flags a staple, else `toBuy`). `isStaple` and amounts don't affect it | "Out" is something the household knows, so "probably have it" would be wrong. `Aggregate` reads it through the optional `grocery.OutPantry` interface, so its signature doesn't change. "Low" still means some is at home. Comparing amounts needs unit conversion between pantry and recipes, so it's deferred. |
| 57 | `pantry.Service.GroceryPantry` registers each item under its catalog ingredient ID and under `name:<key>`, and resolves unlinked items against the catalog each time a list is built | These are the planner's two line keys (catalog ID, or `name:` for uncatalogued lines). Resolving at read time means an item added as free text still matches after a later import adds the ingredient to the catalog, with no backfill job. |
| 58 | `planning.Service` gets the pantry through an optional `PantrySource` (`WithPantry`), reports `pantryApplied`, and fails the list if the pantry can't be read | Planning doesn't import the pantry package and stays testable without it. A failed request is clearer than a list that silently ignores the pantry. |
| 59 | Pantry writes use a `version` field with conditional updates and up to 5 automatic retries, instead of atomic field updates (#48) or `409` to the client | A pantry item has cross-field rules (an `out` item has no amount; a unit needs a quantity) that separate field updates could break when combined. An attempt fails only when another write committed, so 5 members can change one item at once without an error. |
| 60 | `GET /api/v1/ingredients?q=` is global and needs only a signed-in user, not a household | The catalog is global (#37) and holds only ingredient names and categories, no household data. If user-written ingredients ever enter the catalog, scope search to households first. |
| 61 | Default staples are a fixed list with aliases ("Black Pepper" also matches "Pepper"), added only when the pantry has neither the name nor an alias | Staples must link to the catalog ingredients recipes use, or they won't match grocery lines. Never touching existing items keeps the call idempotent and respects the household's own choices. |
| 62 | Pantry lists aren't paginated, and a household holds at most 1000 items | A pantry is small by nature, like a household's invitations. The cap keeps the unpaginated list bounded. |
| 63 | The iOS `PlanStore` shows the plan a change returns instead of reloading, except for a delete (the `204` has no plan), which removes the entry at once and then reloads. A rejected change (`403`, `404`, `409`) reloads the shown week before the error appears | Adding several recipes costs one request each. A week finalized or changed by another member shows its real state as soon as a change fails, so a `plan_finalized` message comes with the lock already visible. Unlike `HouseholdStore` (#43), successful changes don't reload, because plan responses already include other members' edits (#48). |
| 64 | "This week" on iOS is the ISO week containing now in the household's time zone, falling back to the device's when the stored zone is unknown. Week labels format the week's UTC-midnight dates in UTC | Plan dates are in the household's time zone, so late Sunday in Denver must still be that week even though it's Monday in UTC. `ISOWeek` dates stay time-zone free, so labels never shift a day. |
| 65 | Grocery check-offs are stored on the device in `UserDefaults`, keyed by household and week, with one entry per `ingredientKey` | The API has no checked state until Phase 7's saved lists. Ingredient keys are what the list aggregates by, so a check survives amounts changing when recipes are added. Nothing personal is stored: only IDs. |
| 66 | The Add Recipes sheet searches with its own `RecipeLibrary` instance; its plus button adds at once with `preferredServings(householdDefault:)` and the day chosen at the top, and tapping a recipe opens the full form | Searching in the sheet mustn't change the Recipes tab's list. Planning a week means adding several recipes in a row, so the common case is one tap, and the serving size follows the same rule as the recipe screen. |
| 67 | Entries move between days with a "Move To…" context menu and the edit sheet's day picker, not drag and drop | Dragging rows between `List` sections is unreliable and hard to use with VoiceOver. A menu works the same for every input method. |
| 68 | A rating is one document per (household, recipe, member), upserted, and managing your own needs only `household.view` | Ratings are personal opinions, not household settings, so anyone who can see recipes can rate them. One rating per member keeps averages honest. The history of changes lives in `recipe.rated` events, which carry the previous score, rather than in extra rating documents. |
| 69 | Rating tags come from a fixed allowlist; free text goes only in a comment of at most 500 characters | The recommender needs a vocabulary it can rely on (`make-again`, `never-again`, `kid-favorite`, …). Adding a tag is a one-line change; interpreting arbitrary strings is not. `make-again` together with `never-again` is rejected as contradictory. |
| 70 | Recipe `householdRating` and `myRating` are joined per page from `recipe_ratings`, not stored on recipes | It costs one aggregation and one find per page on the unique index, and there are no counters to keep consistent without transactions. Denormalize if lists need to sort by rating. An unrated recipe has `average: null`, not 0. |
| 71 | Event recording is best effort and synchronous: `events.RecordOrLog` logs failures and never fails the user's action, using a context detached from the request with a 2-second deadline | A rating or import that succeeded must not return 500 because the history write failed. An occasional lost event is acceptable for a recommender; a failed user action is not. The detached context keeps a client disconnect from dropping the event, and writing inline avoids a background queue that could lose events on shutdown. Revisit with an outbox if events ever need to be exact. |
| 72 | Each event type has exactly one typed payload in a closed set (`events.Payload`); unknown payload fields are rejected | Autopilot features need stable shapes, and a closed set keeps free text and personal data out of the history. New types are additive. `recipe.unrated` was added to the starting list so removing a rating is visible in the history. |
| 73 | Clients may send only `recipe.viewed`, `recipe.cooked`, `recipe.skipped`, and `grocery.item_checked`, in batches of at most 100. Each event is validated on its own, and invalid ones are reported in the response | Server-observed facts (rated, planned, imported) must not be forgeable. Rejecting per event keeps one bad event from blocking an offline queue forever. User and household come from the membership, never the body, and recipe IDs are checked against the household in one query per batch. |
| 74 | Client events may carry a `clientEventId`, unique per (household, user) through a partial unique index | A retry after a lost response would otherwise count a meal as cooked twice. Events without an ID are never deduplicated. |
| 75 | Event ingestion is rate limited per user (`ratelimit.Limiter.MiddlewareBy`), after authentication and before the membership lookup | Household members often share one IP, so a per-IP limit would couple them. Unauthenticated requests are rejected before they use a budget. It's the same in-memory, per-dyno limiter as decision 18. |
| 76 | Events are kept indefinitely with no TTL. Client `occurredAt` is accepted from 30 days back to 5 minutes ahead | History is the recommender's raw material, and the volume per household is tiny. Retention and archival are a later decision. The window bounds clock skew and stale offline queues. |
| 77 | Planning records `recipe.planned` and `recipe.unplanned` through an optional recorder (`planning.Service.WithEvents`); `DeleteEntry` takes the acting user and reads the entry first only when a recorder is set | The delete returns the remaining plan, so the removed entry's recipe, day, and date must be read before it. The read and the delete aren't atomic, so a concurrent edit can leave the event's day stale, which is acceptable for behavioral history. Without a recorder nothing extra is read. Entry updates and status changes aren't recorded yet. |
| 78 | After saving or removing a rating, iOS reloads that one recipe and copies its `householdRating` and `myRating` into the list row, instead of computing the new average itself. If the reload fails, only `myRating` is patched in | The server rounds the average and counts other members' concurrent ratings, so a local calculation could disagree with it. One small GET keeps the detail screen and list in step. The rating is already saved when a reload fails, so the user still sees it, and the average catches up on the next load. |
| 79 | `RatingTag` is an open string type with a `known` allowlist. Choosing `make-again` or `never-again` clears the other, rather than showing an error | A tag added on the server later still decodes and survives a re-save. The UI never lets a user build a rating the API rejects, and the rule lives in `RatingDraft`, where it's unit tested. |
| 80 | The iOS event queue is one JSON file in Application Support (`event-queue.json`): written atomically after each change, readable after first unlock, excluded from backup, and capped at 500 events (the oldest are dropped). Not SwiftData or `UserDefaults` | The queue is small, append-mostly, and read once per launch, so a file is simpler than a store and keeps `UserDefaults` free of growing blobs. First-unlock protection lets a background flush read it. Backups must not restore events to another device, where they would be sent twice. The cap bounds a long offline stretch; the API would reject events that old anyway (#76). |
| 81 | A `200` removes the whole batch, including rejected events. `5xx`, `408`, and transport errors retry after 5s, doubling to 15 minutes; `429` starts at 30s. Other `4xx` responses discard the batch; `401` waits for the next trigger. Events older than 29 days are dropped before sending | Rejected events can never succeed (#73), and resending a batch the server refused gets the same answer, so either would block the queue forever. `clientEventId` makes resending a batch after an ambiguous failure safe (#74). The 30s start for `429` matches the refill of one batch every 10 seconds after a burst. 29 days leaves a day of margin under the API's 30-day window. |
| 82 | The queue records its user and household. It is discarded on sign-out, when the selected household changes, and when the user has no household. It isn't touched while the session restores at launch | Events are sent as the signed-in user to one household, so a queue sent after a switch would attribute behavior to the wrong household. Keeping the queue while the session restores keeps offline events from being lost on every launch. A launch that ends signed out keeps the file until the next sign-in, which discards it if the user or household differs. |
| 83 | "Mark as Cooked" and "Skip" on Week rows only record events, so every member can use them, in finalized weeks too. The row shows the outcome only in memory for this launch | Cooking happens after a week is finalized, and reporting it changes no plan, so `plan.edit` isn't needed. The API has no way to read events back, and storing outcomes on the device would create a second, unsynchronized record. A shared "cooked" state belongs in the plan API if households want one. |
| 84 | `recipe.viewed` needs 3 seconds on screen with the app active, then at most one per recipe per 10 minutes (in memory). `grocery.item_checked` is recorded for each individual check and uncheck, but not for "Uncheck All". Lines without a catalog ingredient send only `name` | A quick scroll past a recipe or returning to it isn't a new signal. Unchecking corrects a mistaken check, and the payload has `checked` for that, while "Uncheck All" resets the list and says nothing about shopping. `name:` keys aren't ingredient IDs, so they aren't sent as one. |
