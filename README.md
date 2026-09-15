# DinnerOS

> **DinnerOS is a working name.** The product name is configuration, not code —
> see [Naming](#naming).

DinnerOS is a native iOS meal planner for a household. Members plan the week
together, and DinnerOS turns the chosen recipes into a single consolidated
grocery list (scaled for servings, with pantry staples excluded). They cook,
rate, and the app learns from what the household actually does.

The recommendation technology behind it is being designed to become
**Autopilot**: a standalone, multi-tenant API that builds an optimized,
personalized *week* of meals for any food-commerce platform. DinnerOS will be
Autopilot's first customer.

## Status

| Phase | Scope | State |
| ----- | ----- | ----- |
| 0 | Repository, architecture, tooling, CI, TestFlight | ✅ Done |
| 1 | Backend foundation (config, Mongo, middleware, health/ready, Heroku at api.tlps.dev) | ✅ Done |
| 2 | Authentication (Sign in with Apple, Google) — API and iOS | ✅ Done |
| 3 | Households, roles, and invitations (Resend email) — API and iOS | ✅ Done |
| 4 | Recipes, ingredient catalog, units, recipe import — API and iOS library | ✅ Done |
| 5 | HelloFresh historical importer (full order history loaded) | ✅ Done; exact variant capture for some recipes pending |
| 6 | Weekly planner and week grocery list | API ✅ Done; iOS in progress |
| 7 | Pantry and grocery engine | Grocery engine, pantry API, and iOS Pantry tab ✅ (applied to week grocery lists); saved lists pending |
| 8 | Shopping providers | Planned |
| 9 | Ratings and behavioral events | ✅ Done (API and iOS) |
| 10–12 | Autopilot (deterministic V1, context, private service) | Later |

## Repository layout

```text
DinnerOS/
├── api/                    Go modular-monolith API (chi, slog, MongoDB)
│   ├── cmd/server/         HTTP server entry point
│   ├── internal/           Domain packages + HTTP wiring (not importable externally)
│   ├── pkg/                Reusable, domain-free packages (empty until needed)
│   ├── migrations/         Index/data migration conventions
│   └── openapi.yaml        OpenAPI 3.1 specification
├── ios/                    Native SwiftUI app
│   ├── DinnerOS/           App sources (App/, Features/, Resources/)
│   ├── DinnerOSTests/      Swift Testing unit tests
│   ├── Config/             xcconfig files + Info.plist (name, bundle ID, API URL)
│   └── fastlane/           Test and TestFlight lanes
├── importers/hellofresh/   Offline importer for our own order history (Phase 5)
├── docs/                   Architecture and design documentation
├── .github/workflows/      API and iOS CI
├── docker-compose.yml      Local MongoDB (and optional containerized API)
├── Makefile                Common developer commands
└── .env.example            Every backend setting, with placeholders
```

## Quick start

Prerequisites: Go 1.25+, Xcode 26+, Docker Desktop. Details are in
[docs/development.md](docs/development.md).

```bash
cp .env.example .env
make mongo-up
make api-run
```

```bash
curl -s localhost:8080/health
```

```bash
open ios/DinnerOS.xcodeproj
```

Run every test and linter:

```bash
make test lint
```

`make help` lists all commands.

## Secrets and personal data

This repository is public. **Never commit:**

- credentials of any kind (MongoDB, Resend, Apple, Google, Heroku, OpenAI, App
  Store Connect), signing certificates, or `.p8` keys
- `.env` files
- imported HelloFresh recipe or order data, session cookies, or HAR captures

Local secrets live in `.env` (git-ignored). CI secrets live in GitHub Secrets.
Imported data lives outside Git. [docs/deployment.md](docs/deployment.md) lists
every credential and where to get it.

## Documentation

- [Architecture](docs/architecture.md): system shape, module boundaries, key decisions
- [Development](docs/development.md): local setup and workflow
- [Database](docs/database.md): MongoDB collections, indexes, conventions
- [Authentication](docs/authentication.md): identity providers, sessions, authorization
- [API](docs/api.md): REST conventions and the OpenAPI spec
- [Grocery engine](docs/grocery-engine.md): ingredient normalization, units, aggregation
- [Autopilot](docs/autopilot.md): recommendation and week-optimization architecture
- [Deployment](docs/deployment.md): Heroku, TestFlight, and required credentials

## Naming

"DinnerOS" is a placeholder product name:

- **iOS display name:** `APP_DISPLAY_NAME` in `ios/Config/Shared.xcconfig`. Views
  read it from `AppConfiguration`, never from string literals.
- **Backend name:** the `APP_NAME` environment variable.
- **Bundle identifier:** `APP_BUNDLE_ID_BASE`. Changing it creates a new App
  Store Connect app, so change it deliberately.

Code identifiers such as the Xcode target and Go module path still use the
working name. Renaming them is a mechanical refactor that never affects users.

## License

No license has been chosen. Until one is added, all rights are reserved.
