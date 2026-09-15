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
│   │   ├── mongodb/     client lifecycle, IndexSet management, error translation
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
| `AuthProvider` | Apple, Google | 2 |
| `EmailProvider` | Resend, log-only (local/test) | 3 |
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
- **Limits:** request body size (`HTTP_MAX_BODY_BYTES`), server timeouts, and rate
  limiting on auth and invitation endpoints.
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
  `AppConfiguration`: display name, environment, and API base URL.
- **Navigation:** a tab shell (Week, Recipes, Shop, Household) with
  `NavigationStack` per tab, native sheets, and forms.
- **Quality bar:** Dynamic Type, VoiceOver labels, dark mode, and explicit
  loading, empty, and error states for every screen.
- **Visual identity:** Apple-native structure with DinnerOS's own warm accent
  (paprika orange). No HelloFresh visual cloning.

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
| 13 | Heroku container deploy (`heroku.yml` + `api/Dockerfile`) | The same image is built in CI and locally; no third-party monorepo buildpack; non-root distroless runtime |
| 14 | No CORS middleware | The only client is the native iOS app, which doesn't need CORS. Add it when a browser client exists. |
