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
│   │   ├── ratelimit/   per-client-IP token-bucket middleware
│   │   └── requestid/   request correlation ID in context
│   ├── auth/            provider token verification, DinnerOS sessions
│   ├── users/           User, AuthIdentity
│   ├── households/      Household, HouseholdMembership, roles/permissions
│   ├── invitations/     HouseholdInvitation, EmailProvider
│   ├── recipes/         Recipe, instructions, source metadata
│   ├── ingredients/     Ingredient, units, quantities, conversion
│   ├── planning/        WeeklyPlan, MealPlanEntry
│   ├── grocery/         PantryItem, aggregation engine, GroceryList
│   ├── providers/       GroceryProvider implementations
│   ├── events/          MealEvent collection
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
  `MaxBodyBytes` → routes.
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
  creation. Each has its own limiter instance.
- **Household authorization:** routes under `/households/{householdId}` use
  `households.RequirePermission`, which returns 404 to non-members and 403 to
  members lacking the permission, and stores the membership in the request
  context for the handler and service.
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
  tab shell, or the configuration error, based on `AuthSession.state`.
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
