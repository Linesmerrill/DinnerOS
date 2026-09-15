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

## Backend

```bash
make api-run
make api-test
make api-lint
make api-build
```

`make api-run` loads `.env`. The API listens on `PORT` (default 8080):

```bash
curl -s localhost:8080/health
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
