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

The API deploys as a **container**. `heroku.yml` at the repository root builds
`api/Dockerfile`, the same image CI builds. The image runs as a non-root
distroless binary and listens on Heroku's `$PORT`.

Status:

- **Done:**
  - The app `dinneros-api` exists at
    `https://dinneros-api-bd0859895157.herokuapp.com`, on the container stack
    with an Eco dyno.
  - Build metadata is enabled.
  - `APP_ENV`, `MONGODB_DATABASE`, and `LOG_LEVEL` are set.
  - The Phase 1 image deploys and starts.
- **Remaining:**
  - Set `MONGODB_URI` (step 2).
  - Connect GitHub auto-deploy (step 4).
  - Optionally add the custom domain (step 6).

1. Create the app on the container stack. The name may need to differ if
   `dinneros-api` is taken:

   ```bash
   heroku create dinneros-api --stack container
   ```

2. Set config vars from the credential table below. Replace the placeholder
   values; the `$(...)` generates the signing key locally:

   ```bash
   heroku config:set -a dinneros-api APP_ENV=production MONGODB_URI='mongodb+srv://...' MONGODB_DATABASE=dinneros AUTH_TOKEN_SIGNING_KEY="$(openssl rand -base64 48)"
   ```

3. Expose the deployed commit as `HEROKU_BUILD_COMMIT`, which `/health`
   reports as `version`. Container apps need both labs features, and the
   variables only appear after the next release:

   ```bash
   heroku labs:enable runtime-dyno-build-metadata -a dinneros-api
   ```

   ```bash
   heroku labs:enable runtime-dyno-metadata -a dinneros-api
   ```

4. In the Heroku dashboard, go to **Deploy → GitHub**, connect
   `Linesmerrill/DinnerOS`, enable **automatic deploys from `main`**, and tick
   **"Wait for CI to pass"**. Heroku then never deploys a red build, and no
   Heroku API key is stored in GitHub.
5. Verify the deploy:

   ```bash
   curl -s https://<app>.herokuapp.com/ready
   ```

   Use `/health` for uptime monitoring. `/ready` also checks MongoDB.

6. ✅ Custom domain `api.tlps.dev` is live over HTTPS (Squarespace CNAME →
   Heroku DNS target, certificate managed by Heroku ACM).
   `PRODUCTION_API_BASE_URL` is `https://api.tlps.dev`. To set up a domain like
   this from scratch, Heroku manages the TLS certificate automatically. First add the domain to the app:

   ```bash
   heroku domains:add api.tlps.dev -a dinneros-api
   ```

   At the DNS provider for `tlps.dev`, create a `CNAME` from `api` to the
   **DNS target** Heroku prints. Then turn on automatic certificates and check
   their status:

   ```bash
   heroku certs:auto:enable -a dinneros-api
   ```

   ```bash
   heroku certs:auto -a dinneros-api
   ```

   Once the certificate is issued, set the GitHub variable
   `PRODUCTION_API_BASE_URL=https://api.tlps.dev`.

The API exits at startup if it can't reach MongoDB. A crash-looping dyno after a
deploy almost always means `MONGODB_URI` is wrong or Atlas Network Access
doesn't allow Heroku (Heroku dyno IPs are dynamic, so Atlas must allow
`0.0.0.0/0`, protected by a strong database-user password).

Heroku terminates TLS, so the API is only reachable over HTTPS in production.

### Push notifications

Pushes are sent by `/sendreminders`, a second binary in the same container
image, run by **Heroku Scheduler** on a one-off dyno with the app's config
vars. The web dyno never sends a push and starts without APNs credentials.
What a run does is in [pantry-usage.md](pantry-usage.md#push-delivery).

Status:

- **Done:** the APNs auth key exists (one key, valid for sandbox and
  production), and `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_TOPIC`
  (`com.linesmerrill.dinneros`), and `APNS_AUTH_KEY` (the full `.p8` PEM) are
  set on `dinneros-api`.
- **Remaining:** the Scheduler job (step 1) and the App ID capability plus a
  regenerated profile (step 3).

1. Add the scheduler and one job:

   ```bash
   heroku addons:create scheduler:standard -a dinneros-api
   ```

   ```bash
   heroku addons:open scheduler -a dinneros-api
   ```

   **Add Job** → schedule **Every hour at :00** → command `/sendreminders` →
   dyno size **Eco** (or the app's smallest). Hourly is enough: the order
   reminder is day-granular, the low-stock estimate changes slowly, and pushes
   wait until 08:00 in each household's time zone anyway. A run takes seconds;
   runs that overlap can't double-send, because each notification is claimed
   before it's sent.

2. Check a run by hand, then its log line:

   ```bash
   heroku run /sendreminders -a dinneros-api
   ```

   It ends with `sweep finished` and counts (`households`, `sent`, `skipped`,
   `failed`, `deferred`, `deliveries`, `tokensRemoved`, `pushEnabled`).
   `pushEnabled=false` means the `APNS_*` variables aren't set; the run then
   only refreshes reminders and leaves notifications pending.

3. **Apple:** in **Certificates, Identifiers & Profiles → Identifiers →
   com.linesmerrill.dinneros**, tick **Push Notifications** and save. The app
   now has the `aps-environment` entitlement (`development` in Debug,
   `production` in Release, from `APS_ENVIRONMENT` in `ios/Config/*.xcconfig`),
   so the App Store profile in fastlane match must be regenerated to include
   it, or the next `beta` archive fails with a provisioning error naming
   `aps-environment`. Changing the App ID's capabilities invalidates the old
   profile, and match renews an invalid profile, so re-run the one writing
   lane: Actions → iOS CI → Run workflow with **Create or renew the shared
   signing certificate** ticked, or locally `bundle exec fastlane signing` in
   `ios/` ([Signing](#signing-fastlane-match)). If `beta` still reports the
   missing entitlement, the stored profile predates the change; run
   `bundle exec fastlane match appstore --force` in `ios/` (it replaces only
   the profile, not the certificate).

   Xcode's automatic signing picks the capability up for local device builds.

The key is valid for both gateways, and each device token carries its
environment (`sandbox` from Xcode debug builds, `production` from TestFlight),
so one deployment serves both kinds of build. Local development needs no APNs
setup: without the variables the sweep is a logged no-op, and the simulator
can't receive remote pushes anyway.

### Meal-kit recipe import

New households can import the recipes they actually ordered from their meal-kit
service instead of starting empty ([meal-kit-import.md](meal-kit-import.md)).
The member links the account in the app; a worker binary in the same container
image, `/importmealkit`, does the fetching on **Heroku Scheduler**. The web dyno
only signs in once and queues the job.

The feature is **off** unless `MEAL_KIT_IMPORT_ENABLED=true`, and turning it on
without `RECIPE_IMPORT_ENCRYPTION_KEY` stops the API and the worker at startup
with a message naming the variable. That is deliberate: the feature stores a
member's meal-kit session tokens, and those are only ever stored encrypted.

1. Generate the key locally and set it. Store it in a password manager too:
   rotating it makes every stored link undecryptable, so every member has to
   sign in to their meal kit again.

   ```bash
   heroku config:set -a dinneros-api MEAL_KIT_IMPORT_ENABLED=true RECIPE_IMPORT_ENCRYPTION_KEY="$(openssl rand -base64 48)"
   ```

2. Add the worker to the scheduler:

   ```bash
   heroku addons:open scheduler -a dinneros-api
   ```

   **Add Job** → **Every 10 minutes** → command `/importmealkit` → dyno size
   **Eco**. Ten minutes is enough: an import is a background chore, a run is
   capped at 40 recipe pages per household, and a long order history is meant
   to spread across runs rather than hammer HelloFresh. Overlapping runs are
   safe — a job is claimed with one atomic find-and-modify under a lease.

3. Check a run by hand, then its log line:

   ```bash
   heroku run /importmealkit -a dinneros-api
   ```

   It ends with `meal-kit import run finished` and counts (`claimed`,
   `succeeded`, `paused`, `requeued`, `dead`, `gone`, `recipes`,
   `recipeFailures`). With the feature off it logs that and exits 0.

Optional: `MEAL_KIT_RECIPES_PER_RUN` lowers the per-run cap, and
`MEAL_KIT_HELLOFRESH_BASE_URL` points the client at a stub for testing.
[meal-kit-import.md](meal-kit-import.md#when-a-run-goes-wrong) has the runbook
for a run that goes wrong.

### Starter recipe library

New households get a copy of one household's recipes so friends and family
who sign up don't start with an empty menu. Set `STARTER_RECIPES_HOUSEHOLD_ID`
to that household's ID; unset, new households start empty. The copy runs in
the background, and household creation waits up to 3 s for it (about half a
second for ~460 recipes locally) and copies recipe content only, never order history,
ratings, plans, pantry, or events. A failed copy is logged
(`copy starter recipes failed`) and never fails the creation.

To backfill an existing household, or finish a failed copy, run the command
on a one-off dyno. It is a dry run without `-apply`, and re-running it never
duplicates recipes:

```sh
heroku run -a dinneros-api -- /seedstarter -household <householdId>
heroku run -a dinneros-api -- /seedstarter -household <householdId> -apply
```

It reads `MONGODB_URI`, `MONGODB_DATABASE`, and `STARTER_RECIPES_HOUSEHOLD_ID`
(`-source <householdId>` overrides it). Locally:
`go -C api run ./cmd/seedstarter -household <householdId>`.

### First day of the week backfill

Households created before `weekStartsOn` existed have Monday-first weeks (the
field is absent and reads as `mon`); new households start on Sunday. `/weekstart`
moves the households that never chose to Sunday-first weeks through the same
code path as Household Settings, moving each scheduled meal whose date now falls
in the neighboring week into that week (decision 503). It never touches a
household that chose, is a dry run without `-apply`, and is safe to re-run:

```sh
heroku run -a dinneros-api -- /weekstart -all
heroku run -a dinneros-api -- /weekstart -all -apply
```

`-household <householdId>` limits it to one household and `-to mon` picks another
day. It reads `MONGODB_URI` and `MONGODB_DATABASE`. The moved meals of a week
already ordered then sit on the next week's list, so run it between shops.

### Invitation links

Invitation emails link to `https://api.tlps.dev/invite#token=…`. The API serves
both halves of that link:

- `GET /invite`, the landing page for people who don't have the app yet
- `GET /.well-known/apple-app-site-association`, which lets iOS open the same
  link in the app. Apple requires it at `https://api.tlps.dev` directly, with no
  redirect, as `application/json`. It's served only when `APPLE_TEAM_ID` is set.

The config vars involved are public identifiers, not secrets. Set them (they're
already the defaults or already set, but explicit values document intent):

```bash
heroku config:set -a dinneros-api APPLE_TEAM_ID=6VTPDG2HNK APPLE_BUNDLE_ID=com.linesmerrill.dinneros APP_INVITE_URL_BASE=https://api.tlps.dev/invite
```

After the deploy, check the association file:

```bash
curl -si https://api.tlps.dev/.well-known/apple-app-site-association
```

Apple's CDN caches the file, so changes can take a while to reach devices.
Invitation emails sent earlier used `dinneros://invite?token=…`; the app still
accepts those links. If `APP_INVITE_URL_BASE` is still set to that old value, the
API keeps appending the token directly, but those links do nothing on a phone
without the app, so switch it to the value above.

## iOS on TestFlight

The `iOS CI` workflow always runs lint and tests. Its `testflight` job runs on
pushes to `main`, or on a manual dispatch from `main`, **only** when the
repository variable `TESTFLIGHT_ENABLED` is `true`.

Apple limits uploads per app per day ("Upload limit reached … wait 1 day",
error 90382; hit on 2026-09-16 after about eight uploads). Every push to `main`
that touches `ios/` uploads, so batch app changes into a few larger pushes a day
rather than pushing each small fix. A failed upload, including a capped one, is
reported as a warning and leaves the check green, because Heroku deploys the API
only when every check on the commit passes (a red upload held back three API
deploys on 2026-09-16). Lint and test failures still fail the check.

Status: ✅ enabled and working. The first build was uploaded on 2026-09-15 by run
34927108505, and internal testers in "Household" receive new builds
automatically once Apple finishes processing. Retry a failed upload from
Actions → iOS CI → Run workflow on `main`.

The job runs `bundle exec fastlane beta`, which:

1. authenticates with an App Store Connect API key (no Apple ID password or 2FA)
2. installs the shared certificate and profile with **fastlane match**, read-only
3. sets the build number to the latest TestFlight build + 1
4. archives a Release build with manual signing, using the match profile
5. uploads to TestFlight, where the internal group receives it automatically

### Signing (fastlane match)

One Apple Distribution certificate and one App Store profile are shared by every
machine and CI run. They live encrypted in the private repository
`Linesmerrill/dinneros-certificates`, and CI reads them through an SSH deploy
key. Cloud-managed signing used to request a new certificate per run, which hit
Apple's per-account certificate limit and failed every archive.

- **`ios/fastlane/Matchfile`** points at the storage repository and defaults to
  read-only.
- **`bundle exec fastlane beta`** never creates or revokes anything.
- **`bundle exec fastlane signing`** (or Actions → iOS CI → Run workflow with
  **Create or renew the shared signing certificate** ticked) is the only thing
  that writes: it creates the certificate and profile if they are missing and
  stores them encrypted.
- **Apple's certificate limit:** an account may hold only so many distribution
  certificates. If `signing` reports the limit is reached, revoke an unused
  **Apple Distribution** certificate in Certificates, Identifiers & Profiles and
  run it again. `bundle exec fastlane run match_nuke type:appstore` revokes every
  distribution certificate and profile for the team, so use it only deliberately.
- **Rotating the passphrase or the deploy key** means regenerating the secrets
  below and re-running `signing`.

### One-time Apple setup (manual)

1. ✅ **Apple Developer → Identifiers:** App ID `com.linesmerrill.dinneros`
   ("DinnerOS", team `6VTPDG2HNK`) is registered with **Sign in with Apple**
   enabled as a primary App ID. **Associated Domains** (universal invitation
   links, `applinks:api.tlps.dev`) is expected to be enabled the same way Sign
   in with Apple was: the entitlement is in `ios/Config/DinnerOS.entitlements`,
   and cloud-managed signing with `-allowProvisioningUpdates` adds the capability
   to the App ID during the next `beta` run. If that run fails to archive or
   upload with a provisioning or entitlement error that mentions
   `com.apple.developer.associated-domains`, open the App ID in **Certificates,
   Identifiers & Profiles**, tick **Associated Domains**, save, and re-run
   Actions → iOS CI → Run workflow on `main`. Push Notifications needs the same
   treatment plus a regenerated profile; see
   [Push notifications](#push-notifications).
2. ✅ **App Store Connect → Apps → +:** the "DinnerOS" app record exists (Apple
   ID `6812159676`, SKU `dinneros-ios`, bundle `com.linesmerrill.dinneros`, English
   (U.S.)). The App Store name can still be changed before any public release.
3. ✅ **App Store Connect → Users and Access → Integrations → App Store Connect API:**
   Team key "DinnerOS CI" (Key ID `5D9BNM3J38`, **Admin**, issuer
   `8fd65765-270f-41f2-9f5a-7f5cbc65a8e7`) exists. The owner downloads the `.p8`
   file, which Apple allows only once, and stores it only as the
   `APP_STORE_CONNECT_PRIVATE_KEY` GitHub secret. Rachel
   (`rachel.lines1@gmail.com`) is invited with the Marketing role and access to
   DinnerOS only.
4. ✅ **TestFlight → Internal Testing:** the group "Household" exists with
   **automatic distribution** enabled. Apple doesn't allow changing that setting
   later. Merrill (`merrilliscool@icloud.com`) is a tester. Rachel is added once
   she accepts her App Store Connect invitation, because internal testers must be
   users on the team.
5. **GitHub → Settings → Secrets and variables → Actions:** add the values below
   and create an environment named `testflight`.
6. ✅ **Signing storage:** the private repository `Linesmerrill/dinneros-certificates`
   holds the encrypted certificate and profile, with a write deploy key named
   "DinnerOS CI (fastlane match)".

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
| `APPLE_TEAM_ID` | Phase 2 | developer.apple.com → Account → **Membership details** → Team ID (`6VTPDG2HNK`). Required for universal invitation links: without it, `/.well-known/apple-app-site-association` isn't served. |
| `APPLE_BUNDLE_ID` | Phase 2 | `com.linesmerrill.dinneros` (must match the app) |
| `APPLE_SERVICE_ID` | only for web sign-in | Identifiers → **Services IDs**. Not needed for native iOS. |
| `GOOGLE_CLIENT_ID` | Phase 2 | ✅ Created. Google Cloud project `dinneros-508702` → Google Auth Platform → Clients → "DinnerOS iOS" (bundle ID above, team `6VTPDG2HNK`). The value is `600707694145-ifi6jfhmial52rtjgrrs18eh5muiqsnt.apps.googleusercontent.com`. It's a public identifier, not a secret. The iOS app also needs the reversed client ID as a URL scheme: `com.googleusercontent.apps.600707694145-ifi6jfhmial52rtjgrrs18eh5muiqsnt`. |
| `RESEND_API_KEY` | Phase 3 | ✅ Set (a send-only key). resend.com → **API Keys**. The verified sending domain is `api.tlps.dev`: its DKIM record (`resend._domainkey.api.tlps.dev`) and the SPF/MX records on `send.api.tlps.dev` are in Squarespace DNS. A DMARC record (`_dmarc.api.tlps.dev`) is optional and not yet added. |
| `EMAIL_FROM` | Phase 3 | ✅ `DinnerOS <invites@api.tlps.dev>`. It must be an address on the verified Resend domain. |
| `EMAIL_PROVIDER` | Phase 3 | Optional. Defaults to `resend` when `RESEND_API_KEY` is set. Production refuses `log`. |
| `WALMART_IMPACT_PUBLISHER_ID`, `WALMART_IMPACT_AD_ID`, `WALMART_IMPACT_CAMPAIGN_ID` | optional (Phase 8) | Walmart affiliate program on Impact (affiliates.walmart.com) → Impact dashboard. Numeric identifiers, not secrets. Set all three to wrap Walmart cart links in `goto.walmart.com` tracking links, or none (the default: untracked links). Walmart handoffs need no other Walmart setting. See [shopping-providers.md](shopping-providers.md#credentials). |
| `APP_INVITE_URL_BASE` | Phase 3 | Set to `https://api.tlps.dev/invite` (also the default). The token is appended as `#token=…`, so it never reaches the server. A value ending in `=`, such as the old `dinneros://invite?token=`, gets the token appended directly, but such links do nothing without the app. See [Invitation links](#invitation-links). |
| `APP_URL_SCHEME` | Phase 3 | Optional, default `dinneros`. Must match `APP_URL_SCHEME` in `ios/Config/Shared.xcconfig`; the `/invite` page's **Open** button uses it. |
| `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_AUTH_KEY` | push | ✅ Set. developer.apple.com → Certificates, Identifiers & Profiles → **Keys** → a key with **Apple Push Notifications service (APNs)** enabled (one key serves sandbox and production). `APNS_KEY_ID` is its 10-character key ID, `APNS_TEAM_ID` is `6VTPDG2HNK`, and `APNS_AUTH_KEY` is the **entire** downloaded `AuthKey_XXXX.p8`, BEGIN and END lines included (literal `\n` escapes are accepted too). Apple allows downloading it once; store it only here. Set all three or none: none makes push a logged no-op, a partial or unparseable set stops the API and the sweep at startup. Read only by `/sendreminders`. See [Push notifications](#push-notifications). |
| `APNS_TOPIC` | push | Optional; defaults to `APPLE_BUNDLE_ID` (`com.linesmerrill.dinneros`). |
| `MEAL_KIT_IMPORT_ENABLED` | meal-kit import | Your choice, default `false`. `true` turns on asynchronous recipe import from a member's meal-kit account. Not a secret. See [Meal-kit recipe import](#meal-kit-recipe-import). |
| `RECIPE_IMPORT_ENCRYPTION_KEY` | meal-kit import | **Required when `MEAL_KIT_IMPORT_ENABLED=true`; a missing or too-short value stops the API and the worker at startup.** Generate locally: `openssl rand -base64 48`. It wraps the per-link data keys that encrypt members' meal-kit session and refresh tokens, so it is as sensitive as `AUTH_TOKEN_SIGNING_KEY`: store it only in Heroku config and a password manager, never in the repository. Rotating it makes every stored link undecryptable and every member has to sign in to their meal kit again. |
| `MEAL_KIT_RECIPES_PER_RUN` | optional | Recipe pages one worker run fetches per household (default 40). Lower is more polite to the meal-kit service; the rest waits for the next scheduled run. Not a secret. |
| `MEAL_KIT_HELLOFRESH_BASE_URL` | optional | Overrides the HelloFresh account API origin (https only), for testing against a stub. Empty uses HelloFresh. Not a secret. |
| `STARTER_RECIPES_HOUSEHOLD_ID` | optional | The 24-character hex ID of the household whose recipes every new household receives as a starter library. Empty (the default) disables it; a malformed value stops the API at startup. Not a secret. See [Starter recipe library](#starter-recipe-library). |
| `OPENAI_API_KEY` | future | platform.openai.com → API keys (backend only) |

### GitHub Actions

| Name | Type | Where to get it |
| --- | --- | --- |
| `APP_STORE_CONNECT_KEY_ID` | secret | App Store Connect → Integrations → the key's **Key ID** |
| `APP_STORE_CONNECT_ISSUER_ID` | secret | Same page, **Issuer ID** at the top |
| `APP_STORE_CONNECT_PRIVATE_KEY` | secret | Contents of `AuthKey_XXXX.p8`, base64-encoded: `base64 -i AuthKey_XXXX.p8 \| pbcopy` |
| `MATCH_PASSWORD` | Signing | The passphrase that encrypts the match repository. Generate once (`LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom \| head -c 40`) and store it in a password manager; losing it means re-running `fastlane signing` after a `match_nuke`. |
| `MATCH_DEPLOY_KEY` | Signing | The private half of the SSH deploy key with write access to `Linesmerrill/dinneros-certificates`. Generate with `ssh-keygen -t ed25519`, add the public half as a deploy key with write access. |
| `DEVELOPMENT_TEAM` | secret | Apple Team ID (as above) |
| `TESTFLIGHT_ENABLED` | variable | Set to `true` once the setup above is complete |
| `TESTFLIGHT_EXTERNAL_GROUPS` | variable | Optional. Comma-separated external TestFlight group names (e.g. `Friends & Family`). When set, every main build is also distributed to those groups and submitted for Beta App Review, with the commit subject as "What to Test". The job then waits for build processing, so it takes longer. Empty keeps builds internal-only. |
| `PRODUCTION_API_BASE_URL` | variable | The production API URL, e.g. `https://<app>.herokuapp.com` |
