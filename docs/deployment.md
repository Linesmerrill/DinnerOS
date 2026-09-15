# Deployment and credentials

## Environments

| Environment | API | Database | iOS build |
| --- | --- | --- | --- |
| development | `make api-run` on localhost:8080 | docker-compose MongoDB | Debug → `http://localhost:8080` |
| production | Heroku | MongoDB Atlas | Release via TestFlight → `PRODUCTION_API_BASE_URL` (https only) |

Configuration comes entirely from environment variables (`.env.example` lists
all of them). `APP_ENV=production` switches to JSON logs and requires an explicit
`MONGODB_URI`.

## Backend on Heroku

Status: the container image builds in CI. The Heroku app itself is configured in
Phase 1.

Planned approach:

1. Create the app. The name may need to differ if `dinneros-api` is taken:

   ```bash
   heroku create dinneros-api
   ```

2. Deploy the API from the `api/` subdirectory. The chosen option is verified and
   documented here in Phase 1:
   - **Container** (`heroku.yml` + `api/Dockerfile`, stack `container`), or
   - **Go buildpack** with a monorepo `APP_BASE=api` buildpack and a `Procfile`.
3. Enable **GitHub integration → automatic deploys from `main` → "Wait for CI to
   pass"**. Heroku then never deploys a red build, and no Heroku API key is stored
   in GitHub.
4. Set config vars (`heroku config:set KEY=value`) from the credential table below.
5. Point Heroku's health check and uptime monitoring at `GET /health`. `GET /ready`
   also checks MongoDB.

Heroku terminates TLS, so the API is only reachable over HTTPS in production.

## iOS on TestFlight

The `iOS CI` workflow always runs lint and tests. Its `testflight` job runs on
pushes to `main` **only** after the repository variable `TESTFLIGHT_ENABLED` is
set to `true`, so `main` stays green before Apple setup is done.

The job runs `bundle exec fastlane beta`, which:

1. authenticates with an App Store Connect API key (no Apple ID password or 2FA)
2. sets the build number to the latest TestFlight build + 1
3. archives a Release build with Xcode **cloud-managed signing**
   (`-allowProvisioningUpdates`), so no certificates or provisioning profiles are
   stored anywhere
4. uploads to TestFlight, where the internal group receives it automatically

### One-time Apple setup (manual)

1. **Apple Developer → Identifiers:** register the App ID
   `com.linesmerrill.dinneros` and enable **Sign in with Apple** (needed in Phase 2).
2. **App Store Connect → Apps → +:** create the app with that bundle ID. The App
   Store name must be globally unique and can differ from the display name.
3. **App Store Connect → Users and Access → Integrations → App Store Connect API:**
   create a Team key with the **Admin** role (cloud-managed distribution signing
   requires it). Download the `.p8` file. Apple lets you download it only once.
4. **TestFlight → Internal Testing:** create a group (for example "Household")
   with **automatic distribution** enabled, and add Merrill and Rachel. Internal
   testers must be users on the App Store Connect team.
5. **GitHub → Settings → Secrets and variables → Actions:** add the values below
   and create an environment named `testflight`.

## Credential reference

Never commit any of these. Local values go in `.env` (backend) or
`ios/Config/Local.xcconfig` (Team ID only). Deployed values go in Heroku config
vars or GitHub Secrets.

### Backend (Heroku config vars / `.env`)

| Variable | Needed from | Where to get it |
| --- | --- | --- |
| `MONGODB_URI` | Phase 1 | MongoDB Atlas → your cluster → **Connect** → Drivers. Create a database user with a strong password and allow Heroku's egress in Network Access. |
| `MONGODB_DATABASE` | Phase 1 | Your choice (default `dinneros`) |
| `AUTH_TOKEN_SIGNING_KEY` | Phase 2 | Generate locally: `openssl rand -base64 48` |
| `APPLE_TEAM_ID` | Phase 2 | developer.apple.com → Account → **Membership details** → Team ID |
| `APPLE_BUNDLE_ID` | Phase 2 | `com.linesmerrill.dinneros` (must match the app) |
| `APPLE_SERVICE_ID` | only for web sign-in | Identifiers → **Services IDs**. Not needed for native iOS. |
| `GOOGLE_CLIENT_ID` | Phase 2 | Google Cloud Console → APIs & Services → Credentials → **Create OAuth client ID → iOS** (bundle ID above) |
| `RESEND_API_KEY` | Phase 3 | resend.com → **API Keys**. Also verify a sending domain under **Domains**. |
| `EMAIL_FROM` | Phase 3 | An address on the verified Resend domain |
| `OPENAI_API_KEY` | future | platform.openai.com → API keys (backend only) |

### GitHub Actions

| Name | Type | Where to get it |
| --- | --- | --- |
| `APP_STORE_CONNECT_KEY_ID` | secret | App Store Connect → Integrations → the key's **Key ID** |
| `APP_STORE_CONNECT_ISSUER_ID` | secret | Same page, **Issuer ID** at the top |
| `APP_STORE_CONNECT_PRIVATE_KEY` | secret | Contents of `AuthKey_XXXX.p8`, base64-encoded: `base64 -i AuthKey_XXXX.p8 \| pbcopy` |
| `DEVELOPMENT_TEAM` | secret | Apple Team ID (as above) |
| `TESTFLIGHT_ENABLED` | variable | Set to `true` once the setup above is complete |
| `PRODUCTION_API_BASE_URL` | variable | The production API URL, e.g. `https://<app>.herokuapp.com` |
