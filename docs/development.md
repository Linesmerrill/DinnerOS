# Local development

## Prerequisites

| Tool | Version | Notes |
| --- | --- | --- |
| Go | 1.25+ | `go version` |
| Xcode | 26+ | includes `swift-format` and iOS 26 simulators |
| Docker Desktop | recent | runs local MongoDB |
| make | system | developer commands |
| Ruby + Bundler | 3.3 (CI) | only needed to run fastlane locally |

Optional: `mongosh` for inspecting the database, the Heroku CLI, and `gh`.

## First-time setup

```bash
cp .env.example .env
make mongo-up
```

`.env` is git-ignored. The defaults work against the docker-compose MongoDB.

> **Port clash:** if a MongoDB is already installed natively (for example, Homebrew
> `mongod` bound to `127.0.0.1:27017`), `localhost:27017` reaches it instead of the
> container. Either use that MongoDB, or move the container to another port:
>
> ```bash
> MONGO_HOST_PORT=27018 make mongo-up
> ```
>
> Then set `MONGODB_URI=mongodb://localhost:27018` in `.env`, and
> `MONGODB_TEST_URI=mongodb://localhost:27018` for integration tests. Integration
> tests only ever create and drop their own `dinneros_test_*` databases.

## Backend

```bash
make api-run
make api-test
make api-test-integration
make api-lint
make api-build
```

- `make api-run` loads `.env`, connects to MongoDB (it exits if MongoDB is
  unreachable), and listens on `PORT` (default 8080).
- `make api-test` runs unit tests. MongoDB integration tests skip unless
  `MONGODB_TEST_URI` is set.
- `make api-test-integration` sets `MONGODB_TEST_URI` to the docker-compose
  MongoDB and runs everything with the race detector. Each test uses and drops
  its own database. CI always runs the integration tests.

```bash
curl -s localhost:8080/health
```

```bash
curl -s localhost:8080/ready
```

To run the API and MongoDB in containers instead:

```bash
docker compose --profile api up --build
```

## iOS

Open the project in Xcode, choose the **DinnerOS** scheme, and run on an iPhone
simulator:

```bash
open ios/DinnerOS.xcodeproj
```

From the command line:

```bash
make ios-build
make ios-test
make ios-lint
make ios-fmt
```

Override the simulator with `IOS_DESTINATION`, for example
`make ios-test IOS_DESTINATION='platform=iOS Simulator,name=iPhone 17,OS=latest'`.

### Per-developer Xcode settings

Create `ios/Config/Local.xcconfig` (git-ignored) for settings that must not be
committed:

```text
DEVELOPMENT_TEAM = ABCDE12345
// Point a physical device at your Mac's API:
API_BASE_URL = http:/$()/192.168.1.20:8080
```

Debug builds default to `http://localhost:8080`, which works from the simulator.

### Developer sign-in from the simulator

Sign in with Apple needs a signed build and a real Apple ID, and Google sign-in
needs `GOOGLE_CLIENT_ID` on the API. For everyday simulator work, use the Debug-only
**Developer sign-in** button instead. It calls `POST /api/v1/auth/dev`.

1. Start the API with development login enabled:

   ```bash
   APP_ENV=development AUTH_DEV_LOGIN_ENABLED=true make api-run
   ```

   Or run it without `make`, on another port, against a throwaway database:

   ```bash
   APP_ENV=development AUTH_DEV_LOGIN_ENABLED=true PORT=18080 \
     MONGODB_URI=mongodb://localhost:27017 MONGODB_DATABASE=dinneros_dev \
     go -C api run ./cmd/server
   ```

2. If the API isn't on port 8080, point Debug builds at it in
   `ios/Config/Local.xcconfig`:

   ```text
   API_BASE_URL = http:/$()/localhost:18080
   ```

   The simulator shares the Mac's network, so `localhost` reaches the API.
   `NSAllowsLocalNetworking` allows plain HTTP to local addresses. A physical device
   needs the Mac's LAN IP instead.

3. Run the **DinnerOS** scheme (Debug), tap **Developer sign-in**, then open the
   **Household** tab. It shows "Simulator Developer" and
   `dev-simulator@example.com` from `GET /api/v1/me`. **Sign Out** asks for
   confirmation and returns to the sign-in screen.

The button only appears in Debug builds whose `AppEnvironment` is `development`.
It isn't compiled into Release. Without `AUTH_DEV_LOGIN_ENABLED=true`, the API
returns `404` and the app shows an inline error with **Try Again**.

The API uses an ephemeral signing key when `AUTH_TOKEN_SIGNING_KEY` is unset, so
restarting it invalidates access tokens. The app recovers through
`/auth/refresh`, because refresh tokens are stored in MongoDB. If you also drop
the database, the next request signs you out.

To clear a stored session from the simulator, sign out or delete the app. Keychain
items are removed with the app on the simulator.

### Adding files

`ios/DinnerOS/` and `ios/DinnerOSTests/` are synchronized folders. Create Swift
files in the right feature folder and Xcode picks them up automatically, with no
project edits.

## Working agreements

- Keep `main` green. CI runs on every push and pull request.
- Write tests for important logic before moving to the next phase. Never disable
  a failing test to get CI green.
- Make one coherent commit per logical change.
- Update the relevant `docs/` page when behavior or architecture changes.
- Never commit secrets, `.env`, or imported personal data (see README).
