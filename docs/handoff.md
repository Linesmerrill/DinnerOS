# DinnerOS — handoff context

Written 2026-09-17. Everything here is context that is **not** already in the repo.
The repo's own docs are the source of truth for how the system works:
`README.md` (status table, layout), `docs/architecture.md` (decision log, currently
through row 510), `docs/api.md`, `docs/database.md`, `docs/autopilot.md`,
`docs/shopping-providers.md`, `docs/pantry-usage.md`, `docs/deployment.md`,
`docs/development.md`, `docs/testflight.md`.

## What this is

Native SwiftUI iOS app (`ios/`) plus a Go modular-monolith API (`api/`, chi + slog +
MongoDB). A household plans a week of dinners together, the app builds one consolidated
grocery list, hands it to Walmart, tracks the pantry and what the week cost against a
meal kit. Autopilot (the recommender) is meant to become a standalone multi-tenant
service later; DinnerOS is its first customer.

Phases 0–11 are done. Phase 12 (Autopilot as a private service) is the next big one.

## Live environment

- **API:** Heroku app `dinneros-api`, container stack, at `https://api.tlps.dev`.
  Auto-deploys from GitHub `main` **with "Wait for CI to pass" enabled** — so a red
  check on a commit means no deploy. This bit us on 2026-09-16: three commits were
  never deployed because the iOS check failed on Apple's upload limit. Since
  commit 2b25f77 a failed TestFlight upload is a warning, not a check failure.
  Manual deploy if ever needed: `git push heroku main`.
- **Database:** MongoDB Atlas, database `dinneros`. Local tests use `dinneros_sim`.
- **iOS:** TestFlight via GitHub Actions + fastlane (`ios/fastlane/Fastfile`).
  Every push touching `ios/**` uploads a build. Apple caps uploads per app per day
  (error 90382, hit after ~8 in a day) — batch app changes into bigger pushes.
  External group "Friends & Family", public link `https://testflight.apple.com/join/cyVY8K6r`.
  Build 27 is in Beta App Review and contains a since-fixed notification crash;
  whether to replace it is still undecided.
- **Config:** Heroku config vars (names only) — `APNS_AUTH_KEY`, `APNS_KEY_ID`,
  `APNS_TEAM_ID`, `APNS_TOPIC`, `APP_ENV`, `APP_INVITE_URL_BASE`, `APPLE_BUNDLE_ID`,
  `APPLE_TEAM_ID`, `AUTH_TOKEN_SIGNING_KEY`, `EMAIL_FROM`, `GOOGLE_CLIENT_ID`,
  `LOG_FORMAT`, `LOG_LEVEL`, `MONGODB_DATABASE`, `MONGODB_URI`, `RESEND_API_KEY`,
  `STARTER_RECIPES_HOUSEHOLD_ID`. GitHub repo variables: `PRODUCTION_API_BASE_URL`,
  `TESTFLIGHT_ENABLED`, `TESTFLIGHT_EXTERNAL_GROUPS`.
  Heroku Scheduler runs `/sendreminders` hourly.

## Standing rules (these matter)

- **The GitHub repo is public.** Never commit credentials, `.env`, imported HelloFresh
  data, receipts, screenshots or real email addresses. Test fixtures use invented brands.
- **Never read or source the repo's `.env`** — it points at production. Read production
  config with `heroku config:get NAME -a dinneros-api` into an env var, without printing it.
- Never put secrets in chat.
- Local integration tests must run with `MONGODB_DATABASE=dinneros_sim`.
- Pushing straight to `main` is pre-authorized; no PR needed.

## How the work runs

1. Background agents do the work in git worktrees, one branch per task.
2. Merge the branch, then run `make docs-check` (catches reused decision-log row
   numbers, which collide constantly when two agents both append rows).
3. Run `make api-test-integration` (with `dinneros_sim`) and `make ios-test`, and
   **read the logs** — exit codes alone have lied before.
4. After committing, check `git show --stat` and
   `git status --porcelain --ignored | grep -E '^!!.*\.(go|swift)$'`. A broad
   `.gitignore` rule (`coverage.*`) once silently swallowed a Go source file, and the
   agent's green test run was true of a tree that wasn't the commit.
5. Push to `main`. If Docker isn't running, a throwaway local mongod works:
   `mongod --dbpath <tmp> --port 27017 --fork --logpath <tmp>/mongod.log`.
   Go builds need `-C api` (e.g. `go build -C api ./cmd/server`).

## Working style the user expects

- Short answers: what happened, then what they need to do. No narration or recaps.
- Do the browser and console work yourself — Apple Developer, App Store Connect,
  Google Cloud, Heroku, Outlook. Verify by looking rather than asking.
- Keep `README.md` current when scope changes.
- Batch iOS pushes because of Apple's daily upload limit. They explicitly rejected a
  nightly auto-upload schedule.

## Recent work (this session, all on `main`)

- Prices in integer cents, weekly cost vs a meal-kit baseline ($130 / 5 meals),
  leftover tracking into the pantry.
- Walmart cart screenshot price import, and three rounds of fixes to it:
  prices binding to the wrong item (Walmart draws the price *above* the name);
  Vision returning "$244" for $2.44 because the cart uses big dollars with small
  raised cents (now: two or more separator-less prices on a screen ⇒ all are read as
  ending in cents, cross-checked against the order total); product-photo words
  polluting titles. Fixtures use the user's real cart of 17 items.
- Household setting for the first day of the week, defaulting to Sunday. Week keys
  stay ISO (`2026-W38`); the start day decides which dates a key covers, and changing
  it moves affected meals rather than dates. Existing households read as Monday.
- Equal photo sizes for every menu card.
- Shop cost-card sheets now presented from the screen root, fixing a sheet that closed
  itself on first tap while the week's cost was still loading.

## Open threads

- **On-device checks pending** (next build): Import Prices opening first tap while Shop
  is still loading; the corrected prices and titles; week-start switch; Autopilot
  permission explainer, calendar/weather effects and Siri phrases.
- **User's own household is still on Monday weeks.** Switch in Household Settings
  between shops (a moved Sunday meal lands on the next week's list), or run
  `heroku run -a dinneros-api -- /weekstart -all` (dry run; `-apply` to write).
- Autopilot's weekday rules and plan-day lists still start on Monday.
- Undecided: replace build 27 in Beta App Review; a garlic-herb-butter batch recipe;
  weighing a chicken demi-glace packet; a sanity check of the Autopilot weights.
- Walmart order confirmation emails contain no item prices, which is
  why prices come from screenshots.
