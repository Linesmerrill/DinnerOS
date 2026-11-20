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

### Referrals and DeviceCheck

Decision 698. A referral pays only once the new household is set up and
passes the anti-farming checks, one of which is Apple DeviceCheck. The web
dyno calls DeviceCheck when a household is created or joined and when a code
is entered, and sweeps referrals at start and every hour (nothing to
schedule).

- **Key.** `DEVICECHECK_KEY_ID`, `DEVICECHECK_TEAM_ID`, and
  `DEVICECHECK_PRIVATE_KEY` default to `APNS_KEY_ID`, `APNS_TEAM_ID`, and
  `APNS_AUTH_KEY`, so the APNs key is used unless they're set. The key must
  have **DeviceCheck** enabled (developer.apple.com → Keys → the key →
  DeviceCheck). Without a usable key nothing can be checked and, in
  production, no referral qualifies: they stay pending and expire.
- **Check the key** against the deployed config. It prints only Apple's
  status and short error text:

  ```bash
  heroku run -a dinneros-api -- /devicecheckprobe
  ```

  Locally, from `api/`: `go run ./cmd/devicecheckprobe` (add `-development`
  for Apple's development server). `HTTP 400: Missing or badly formatted
  device token payload` means the key is accepted; `401` or `403` means it
  isn't.
- **Thresholds.** `REFERRAL_QUALIFY_DAYS` (14), `REFERRAL_MIN_DINNERS` (1),
  `REFERRAL_MIN_RECIPES` (1), and `REFERRAL_MAX_PER_30_DAYS` (3). The app's
  words follow them.

### Sign in with Apple revocation

Decisions 750 and 751. When someone deletes their account, the API tells
Apple to end DinnerOS's Sign in with Apple authorization for them, as App
Review requires (guideline 5.1.1(v)). It is off until the key below is set;
off, sign-in and deletion work exactly as before and the startup log says
`signInWithAppleRevocation="not configured"`.

Status: **not set yet.** Hark creates the key once the developer portal is
back, then sets the three Heroku config vars.

1. **Apple:** developer.apple.com → Certificates, Identifiers & Profiles →
   **Keys** → **+**. Name it `DinnerOS Sign in with Apple`, tick **Sign in with
   Apple**, click **Configure**, pick the primary App ID
   **com.linesmerrill.dinneros** (team `6VTPDG2HNK`), save, **Continue**,
   **Register**. Note the 10-character **Key ID** and **Download** the
   `AuthKey_<KEYID>.p8` file. Apple allows the download once; keep it only in
   the Heroku config var.
2. **Heroku:** set the vars without printing them:

   ```bash
   heroku config:set -a dinneros-api SIWA_KEY_ID=<key id> SIWA_TEAM_ID=6VTPDG2HNK \
     SIWA_PRIVATE_KEY="$(cat AuthKey_<KEYID>.p8)"
   ```

   Then delete the downloaded file. The dyno restarts; its startup log line
   shows `signInWithAppleRevocation=configured` (never the key or Key ID). A
   bad or partial set never stops the API: revocation stays off and the log
   line reads `not configured: <what's wrong>`, naming the variable. Set all
   three in one command as above, then check with
   `heroku logs -a dinneros-api --tail`.

How it works: the app sends Apple's one-time authorization code with each
Apple sign-in, and the API trades it at `https://appleid.apple.com/auth/token`
for a refresh token, stored encrypted per user. At deletion an Apple user
confirms with Apple once more, the app sends that fresh code with `DELETE
/me`, and the API revokes both tokens at `https://appleid.apple.com/auth/revoke`
before deleting the account and the stored token. People who signed in before
build 116 have no stored token, so their fresh code is what gets revoked.
Apple failures are logged (status and Apple's error code only) and never stop a
sign-in or a deletion. After a test deletion, the Apple ID's **Sign in with
Apple** list (Settings → your name → Sign in with Apple) no longer shows
DinnerOS.

The API must be deployed before an app build that sends the code: the old API
rejects the unknown `authorizationCode` field.

### Free month codes (Apple offer codes)

Decision 755. Each free month an invite earns is a one-time-use Apple offer
code for DinnerOS Plus, from a pool in `plus_offer_codes`. The API gives one
per earned month the moment the invite counts. With the pool empty the month
waits as owed, one email goes to `REPORT_ALERT_EMAIL` (Resend must be on), and
the next import hands codes to every waiting month, oldest first, with a
"Your free month is ready" notification. Codes that end within 14 days are
never given. Codes are secrets: never paste one in chat, a ticket or a commit,
and delete the CSV once it's imported.

**One-time App Store Connect setup** (Hark, once Apple is back):

1. App Store Connect → DinnerOS → Subscriptions → the Plus group →
   **DinnerOS Plus Monthly** (`com.linesmerrill.dinneros.plus.monthly`) →
   Offer Codes → **Create Offer Code** (in the newer screen: Subscription
   Prices → Offer Codes → +).
2. Settings:
   - **Reference name:** `Invite free month` (only staff see it; it becomes
     the import's `offer`).
   - **Customer eligibility:** tick all three, **New**, **Existing** and
     **Expired** subscribers. The household that joined is new; the household
     that shared may already pay or may have lapsed. If App Store Connect
     won't take all three on one free offer, stop and say so: the pool hands
     any unused code to any household, so two offers with different
     eligibility would need the API to keep them apart first.
   - **Offer type / payment mode:** **Free**.
   - **Duration:** **1 month**.
   - **Countries or regions:** all where DinnerOS is sold.
   - If asked how it combines with the introductory offer, pick the option
     that lets new subscribers use the code (Apple's wording changes; the
     free month replaces the 14-day trial for them).
3. Create a batch of **one-time-use codes**: **Quantity** 500 to start (Apple
   limits how many codes an app can make each quarter, so don't make more
   than a quarter's need), **Expiration** the furthest date Apple allows (at
   most 6 months out). Name the batch by month, like `2026-11-a`.
4. When Apple says the codes are ready, **Download** the CSV.

Only the monthly subscription is needed: after the free month it renews at
$4.99 a month, the smaller charge, and the household can change plans in
Apple's settings. One-time-use codes from App Store Connect work only for an
app that is live on the App Store.

**Import** (pick one):

- Through the running API, which gives waiting months their codes at once:

  ```bash
  OPERATOR_TOKEN=<an operator's access token> scripts/offer-codes.sh \
    import ~/Downloads/codes.csv "Invite free month" 2026-11-a 2027-05-15
  OPERATOR_TOKEN=<token> scripts/offer-codes.sh stock
  ```

  The token is an access token of a user in `OPERATOR_USER_IDS`. Behind it is
  `POST /api/v1/admin/plus/offer-codes?offer=&batch=&expiresAt=` with the CSV
  as the body, and `GET /api/v1/admin/plus/offer-codes/stock`.
- Straight into MongoDB, with no token (waiting months get codes at the API's
  next hourly sweep):

  ```bash
  heroku run --no-tty -a dinneros-api -- /offercodes -offer "Invite free month" \
    -batch 2026-11-a -expires 2027-05-15 < ~/Downloads/codes.csv
  heroku run -a dinneros-api -- /offercodes -stock
  ```

Both read one code per line (the first column of a CSV row), skip a header
and blank lines, and skip any code the pool has ever held, so importing the
same file twice adds nothing. They print counts only.

**Stock.** `available` is what can still be given, `owed` is months waiting,
`expired` is unused codes past or within 14 days of their end. Import a new
batch when `available` gets near a month's invites, and before a batch
expires. A code given to a household that never opened it goes back to the
pool when the household is deleted.

**Making codes with the App Store Connect API** (researched, not set up).
The API has `POST /v1/subscriptionOfferCodes` (an offer on a subscription:
name, customer eligibilities `NEW`/`EXISTING`/`EXPIRED`, offer mode free,
duration one month, with per-territory prices) and
`POST /v1/subscriptionOfferCodeOneTimeUseCodes` (a batch: number of codes and
expiration date, related to the offer). The batch's codes come back as CSV
from `GET /v1/subscriptionOfferCodeOneTimeUseCodes/{id}/values`. Managing
in-app purchases needs a key with the **Admin** or **App Manager** role; a
Developer key can't. The fastlane key in CI (`APP_STORE_CONNECT_KEY_ID`,
`APP_STORE_CONNECT_ISSUER_ID`, `APP_STORE_CONNECT_PRIVATE_KEY`) uploads builds,
which App Manager covers, but its role isn't recorded here: check it in App
Store Connect → Users and Access → Integrations before relying on it. A job
could then:

1. `workflow_dispatch` with inputs `quantity` and `expires`.
2. Sign a JWT with the existing key, look up the `Invite free month` offer
   (`GET /v1/subscriptions/{id}/offerCodes`), and `POST` a one-time-use batch.
3. Poll the batch until Apple marks it active, then `GET .../values`.
4. Pipe the CSV straight into `POST /api/v1/admin/plus/offer-codes` with an
   operator token from a new GitHub secret. Nothing in the job may print the
   CSV or write it to a file or an artifact: the repo and its Actions logs are
   public.

It would need one new secret (the operator token, which expires, so a
long-lived operator credential would have to be designed first). Until then,
the download and `scripts/offer-codes.sh` are the way.

### Removed: meal-kit recipe import

The in-app HelloFresh import is gone (decision in
[architecture.md](architecture.md)). A deployment that ran it has leftovers to
clean up by hand:

- Delete the **Heroku Scheduler** job that runs `/importmealkit`. The binary is
  no longer in the image, so the job fails every run until it is removed.
- Unset `MEAL_KIT_IMPORT_ENABLED`, `MEAL_KIT_RECIPES_PER_RUN`,
  `MEAL_KIT_HELLOFRESH_BASE_URL`, and `RECIPE_IMPORT_ENCRYPTION_KEY`. Nothing
  reads them.
- The `meal_kit_jobs` and `meal_kit_cursors` collections are no longer read or
  written and can be dropped. If `meal_kit_links` still exists from the earlier
  design that kept encrypted session tokens (decision 521), drop it too.

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

### Screenshot review (Hark, from build 120)

Nothing goes to TestFlight until Hark has reviewed that build's screenshots.

1. Run the full QA matrix (`scripts/qa-matrix.sh`, docs/development.md) and rerun only what
   failed. Screenshots come from the seeded local test accounts only (developer sign-in on the
   local API), never a real account.
2. Publish them: `scripts/qa-screenshots.py <build> <matrix OUT> [<rerun OUT> ...]` (later folders
   win for the same config and test). It writes one PNG per screen per config: every tab and
   sheet from the sweeps and screen tests, the sign-in screen with and without Face ID, and Start
   Cooking on a scrolled recipe, in light and dark, on the small and large iPhone, the iPhone Duo
   cover screen, and the iPad upright and on its side. `manifest.json` lists each `file`,
   `screen`, `config`, `device`, `state` (appearance and orientation) and `size`.
   Since build 121 (Hark's first review): the matrix has 10 configs (the Duo in dark too), and
   every config runs every screenshot test. `snap()` waits for the screen to settle (no keyboard
   tip, system banner or spinner) and keeps upright PNGs (an iPad on its side had come out
   sideways with an EXIF rotation). The script strips EXIF, refuses a landscape PNG that's taller
   than wide, and refuses to publish when a config or a required screen (sign-in with and
   without Face ID, each tab, the Plus paywall, Start Cooking) is missing. The sign-in shots
   hide the Debug build's developer sign-in (`QA_HIDE_DEVELOPER_SIGN_IN`), so they show the
   Release screen.
3. The script attaches the zip to a temporary draft release and runs the `QA screenshots`
   workflow (`.github/workflows/qa-screenshots.yml`), which publishes the GitHub Actions artifact
   **`qa-screenshots-<build>`** on Linesmerrill/DinnerOS (kept 30 days) and deletes the draft. The
   PNGs are never committed.
4. Post **`SCREENSHOTS READY build <n>, run <run-id>`** (the script prints it).
5. Wait for Hark's **`Screenshots approved for build <n>`**. Until then, don't dispatch the
   TestFlight upload. Fix every numbered issue Hark sends, rerun the affected QA, publish again
   with the same build number, and post SCREENSHOTS READY again.
6. Once approved: fast-forward `main` with a `[skip ci]` head commit and dispatch `iOS CI` on
   `main` with `internal_only=true`.

### Signing (fastlane match)

One Apple Distribution certificate and one App Store profile are shared by every
machine and CI run. They live encrypted in the private repository
`Linesmerrill/dinneros-certificates`, and CI reads them through an SSH deploy
key. Cloud-managed signing used to request a new certificate per run, which hit
Apple's per-account certificate limit and failed every archive.

- **Three identifiers are signed**: the app, `com.linesmerrill.dinneros`; its
  widget extension, `com.linesmerrill.dinneros.LiveActivities`
  (`LIVE_ACTIVITIES_BUNDLE_ID`); and its Safari share sheet extension,
  `com.linesmerrill.dinneros.ShareExtension` (`SHARE_BUNDLE_ID`, both in
  `ios/Config/Shared.xcconfig`). Each has its own App Store profile in match;
  `beta` passes them to their targets as `APP_PROFILE_SPECIFIER`,
  `LIVE_ACTIVITIES_PROFILE_SPECIFIER`, and `SHARE_PROFILE_SPECIFIER`, never as
  one global `PROVISIONING_PROFILE_SPECIFIER`, which would put the app's profile
  on an extension. The `signing` lane registers both extensions' App IDs (no
  capabilities; they share the app's keychain group) before making profiles.
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

#### Adding the Live Activities extension (once)

Until this is done every `beta` run fails at `match` with "match did not install
a profile for com.linesmerrill.dinneros.LiveActivities". Lint and tests are
unaffected: the extension builds for the simulator without signing.

1. Run the writing lane once: Actions → iOS CI → Run workflow on `main` with
   **Create or renew the shared signing certificate** ticked (or
   `bundle exec fastlane signing` in `ios/` with the same secrets). It
   registers the App ID `com.linesmerrill.dinneros.LiveActivities` ("DinnerOS
   Live Activities", no capabilities) through the App Store Connect API key if
   it is missing, then creates its App Store profile and stores both profiles
   in match. The existing certificate is reused.
2. If step 1 fails while registering the App ID (for example, the key's role
   cannot create identifiers), register it by hand and re-run step 1:
   **Certificates, Identifiers & Profiles → Identifiers → + → App IDs → App** →
   Description `DinnerOS Live Activities`, Bundle ID **Explicit**
   `com.linesmerrill.dinneros.LiveActivities`, no capabilities → Register.
3. Re-run Actions → iOS CI on `main`; `beta` now signs both targets.

The extension needs no entitlement. It keeps the Live Activities name and
bundle ID, but since the meal-kit import was removed it draws only the Dinner
widget; nothing starts a Live Activity any more.
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
| `KROGER_CLIENT_ID`, `KROGER_CLIENT_SECRET` | optional (Fry's) | developer.kroger.com → create a production application with the **Cart** and **Products** APIs. The secret is a **secret**. Set both to offer Fry's (or the `KROGER_BANNER` store) in the Shop tab; unset, only Walmart shows. See [shopping-providers.md](shopping-providers.md#implemented-for-kroger). |
| `INSTACART_ENABLED`, `INSTACART_API_KEY` | optional (Instacart) | Instacart developer dashboard (dashboard.instacart.com) → API keys. The key is a **secret**. Instacart shows as a store only when `INSTACART_ENABLED=true` and the key is set; the flag is off by default. A development key also needs `INSTACART_API_BASE=https://connect.dev.instacart.tools`. Set `INSTACART_AFFILIATE=true` once Instacart has tied the key to the Impact account. See [shopping-providers.md](shopping-providers.md#instacart-developer-platform). |
| `UNSPLASH_ACCESS_KEY` | optional (recipe photo search) | The **Access Key** of an app at unsplash.com/oauth/applications. It is a **secret**: never print or commit it. Unset leaves "Search the Web" for recipe photos hidden in the app; the search routes answer 503 `photo_search_off`. A new Unsplash app is in demo mode (50 requests an hour); apply for production (5,000 an hour) once attribution is in place. Search pages are cached for an hour. Decision 705. |
| `PLUS_ENABLED`, `REVENUECAT_WEBHOOK_SECRET` | optional (DinnerOS Plus) | `PLUS_ENABLED=true` turns on the paywall; off, every feature is open. The webhook secret is a **secret** (`openssl rand -hex 24`, at least 24 characters): RevenueCat → Project → Integrations → Webhooks → URL `https://api.tlps.dev/api/v1/webhooks/revenuecat`, Authorization header set to the same value. Unset, the webhook route answers 404. |
| `KROGER_REDIRECT_URL` | with Kroger | The callback registered with the application: `https://api.tlps.dev/api/v1/shopping/kroger/callback`. |
| `KROGER_API_BASE`, `KROGER_BANNER` | optional | `https://api.kroger.com` (or `https://api-ce.kroger.com` for a certification app) and `frys` by default. `KROGER_BANNER` is one of `frys`, `kroger`, `ralphs`, `kingsoopers`, `smiths`, `fredmeyer`, `qfc`, `dillons`, `citymarket`, `marianos`, `picknsave`, `metromarket`, `bakers`, `food4less`, `foodsco`. |
| `KROGER_BANNERS` | optional | Other banners to offer beside `KROGER_BANNER`: a comma list of the keys above, or `all`. Empty (the default) offers `KROGER_BANNER` alone. A household's banner follows the store it picks (decision 652). Turn a banner on only after a real cart test there (see [shopping-providers.md](shopping-providers.md#implemented-for-kroger)). |
| `OPERATOR_USER_IDS` | optional | User IDs of the people who run DinnerOS, comma separated. They can read data across households: today, the store request report (`GET /shopping/requests/summary`, shown in the app under Household → Running DinnerOS). Empty (the default) means nobody. |
| `APP_INVITE_URL_BASE` | Phase 3 | Set to `https://api.tlps.dev/invite` (also the default). The token is appended as `#token=…`, so it never reaches the server. A value ending in `=`, such as the old `dinneros://invite?token=`, gets the token appended directly, but such links do nothing without the app. See [Invitation links](#invitation-links). |
| `APP_URL_SCHEME` | Phase 3 | Optional, default `dinneros`. Must match `APP_URL_SCHEME` in `ios/Config/Shared.xcconfig`; the `/invite` page's **Open** button uses it. |
| `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_AUTH_KEY` | push | ✅ Set. developer.apple.com → Certificates, Identifiers & Profiles → **Keys** → a key with **Apple Push Notifications service (APNs)** enabled (one key serves sandbox and production). `APNS_KEY_ID` is its 10-character key ID, `APNS_TEAM_ID` is `6VTPDG2HNK`, and `APNS_AUTH_KEY` is the **entire** downloaded `AuthKey_XXXX.p8`, BEGIN and END lines included (literal `\n` escapes are accepted too). Apple allows downloading it once; store it only here. Set all three or none: none makes push a logged no-op, a partial or unparseable set stops the API and the sweep at startup. Read only by `/sendreminders`. See [Push notifications](#push-notifications). |
| `APNS_TOPIC` | push | Optional; defaults to `APPLE_BUNDLE_ID` (`com.linesmerrill.dinneros`). |
| `DEVICECHECK_KEY_ID`, `DEVICECHECK_TEAM_ID`, `DEVICECHECK_PRIVATE_KEY` | referrals | Optional; each defaults to its `APNS_*` value, so the APNs key is used. The key must have DeviceCheck enabled. The private key is a **secret** (the whole `.p8`). Set all three (after defaults) or none: a partial or unparseable set stops the API at startup. Read by the web dyno. Check with `/devicecheckprobe`. See [Referrals and DeviceCheck](#referrals-and-devicecheck). |
| `SIWA_KEY_ID`, `SIWA_TEAM_ID`, `SIWA_PRIVATE_KEY` | account deletion | Not set yet. developer.apple.com → Certificates, Identifiers & Profiles → **Keys** → a key with **Sign in with Apple** enabled and configured for the primary App ID `com.linesmerrill.dinneros`. `SIWA_KEY_ID` is its 10-character key ID, `SIWA_TEAM_ID` is `6VTPDG2HNK`, and `SIWA_PRIVATE_KEY` is the **entire** `AuthKey_XXXX.p8`, BEGIN and END lines included (literal `\n` escapes are accepted too). The private key is a **secret**: never print or commit it. Set all three to revoke Apple sign-in when an account is deleted, or none (revocation off). A partial or unparseable set stops the API at startup. Read by the web dyno. See [Sign in with Apple revocation](#sign-in-with-apple-revocation). |
| `SIWA_CLIENT_ID` | optional | Defaults to `APPLE_BUNDLE_ID` (`com.linesmerrill.dinneros`), the audience Apple identity tokens are checked against. Change it only if the app's bundle ID changes. Not a secret. |
| `AUTH_RATE_BURST` | optional | Sign-in requests one address may make at once, default 10 (refills one every 6 seconds). Raise only for local QA. Not a secret. |
| `REFERRAL_QUALIFY_DAYS`, `REFERRAL_MIN_DINNERS`, `REFERRAL_MIN_RECIPES`, `REFERRAL_MAX_PER_30_DAYS` | optional | Referral thresholds, defaults 14, 1, 1, and 3. Not secrets. |
| `APP_STORE_APP_ID` | optional (free month codes) | The app's numeric **Apple ID**: App Store Connect → the app → App Information → General Information → Apple ID. With it, a revealed free month code also carries Apple's redeem link that fills the code in. Not a secret. Digits only; a bundle ID is refused at boot. |
| `STARTER_RECIPES_HOUSEHOLD_ID` | optional | The 24-character hex ID of the household whose recipes every new household receives as a starter library. Empty (the default) disables it; a malformed value stops the API at startup. Not a secret. See [Starter recipe library](#starter-recipe-library). |
| `REVIEW_ACCOUNT_EMAIL`, `REVIEW_ACCOUNT_PASSWORD` | optional | The App Review demo account (decision 752): the one email and password that Sign in with email accepts. **Both are secrets** (the repo is public; never commit them). Off unless both are set; a half-set pair, an address that isn't an email, or a password under 16 characters leaves it off with a startup warning and never stops the API. The startup log says `reviewAccount=true` or `false`, never the values. Its household gets eight recipes of our own with Unsplash photos and this week planned (decision 759), no starter library, and no Plus. See docs/app-store.md, App Review notes. |
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
| `TESTFLIGHT_EXTERNAL_GROUPS` | variable | Optional. Comma-separated external TestFlight group names (e.g. `Friends & Family`). When set, every main build is also distributed to those groups and submitted for Beta App Review, with the commit subject as "What to Test". The job then waits for build processing, so it takes longer. Empty keeps builds internal-only. To ship one build to internal testers only, run iOS CI from main by hand with `internal_only` checked (and push that commit with `[skip ci]` so the push run doesn't also send it out). |
| `PRODUCTION_API_BASE_URL` | variable | The production API URL, e.g. `https://<app>.herokuapp.com` |

Cloudflare Pages also skips a push whose newest commit says `[skip ci]`, so the website does not deploy with an internal-only release. Deploy it by pushing a website-only commit to main without `[skip ci]` afterwards: it touches no `ios/` or `api/` path, so no CI runs and nothing goes to TestFlight.

## Weekly numbers

`heroku run /metrics -weeks 8 -a dinneros-api` prints each week's active
households, carts sent, and average cart value per store (decision 674). It
only reads the events collection. Cart value is the household's saved package
prices for what was sent; stores with no saved prices show "no prices".
