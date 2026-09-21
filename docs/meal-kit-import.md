# Meal-kit recipe import

A new household should not start empty. This is the onboarding path: the member
links their meal-kit account (HelloFresh first), closes the app, and a backend
worker imports **the recipes that account actually ordered** into the
household's library. A push notification says when it is done, and a different
one says when it needs them. Skipping is a first-class choice — recipes can be
added by hand later.

Implemented in `api/internal/mealkit` (queue, service, worker, HTTP),
`api/internal/mealkit/hellofresh` (the source client), and
`api/cmd/importmealkit` (the scheduled worker). The iOS side is
`ios/DinnerOS/Core/MealKit` and `ios/DinnerOS/Features/Household`.

## What it is not

- It does not crawl HelloFresh. The only things it requests are the account's
  own plans, its past-deliveries endpoint, and the recipe pages those
  deliveries name.
- It does not write to the recipe collections. Everything goes through
  `recipes.Service.Import`, the same pipeline a file import uses
  ([import-format.md](import-format.md)), so imported recipes get the same
  validation, alias splitting, ingredient creation, and review items.
- It does not ask for a password. The member signs in on HelloFresh's own
  page, in a web view, and neither the app nor the API ever sees what they
  typed. See [The credential lifecycle](#the-credential-lifecycle).

## The shape

```text
iPhone                              API (web dyno)        MongoDB        Worker (Scheduler)
  │  WKWebView → hellofresh.com/login                 │                       │
  │  (their password, their site, their password manager)                     │
  │  ◀── apiV2Auth cookie                             │                       │
  │  PUT .../meal-kit/hellofresh/link                 │                       │
  │  {accessToken, refreshToken, expiresAt} ──▶ seal ▶│ meal_kit_links        │
  │                              enqueue ────────────▶│ meal_kit_jobs         │
  │  ◀──── 200 {link, job}       (app can close)      │                       │
  │                                                   │◀── claim (findAndModify)
  │                                                   │    plans → subscription id
  │                                                   │    past-deliveries, week by week
  │                                                   │    recipe pages (rate limited)
  │                                                   │    recipes.Service.Import
  │                                                   │◀── checkpoint after each batch
  │  ◀──── push: "Your recipes are ready" ─── notifications outbox ◀──────────┘
  │  GET .../meal-kit/hellofresh  →  what was imported, what failed and why
```

## Job lifecycle

A job is one import run for one household. States:

| State | What it means | What the member sees |
| --- | --- | --- |
| `queued` | Waiting for a worker. `availableAt` is the backoff. | "Importing your recipes… starting shortly" |
| `running` | A worker holds the lease. | Progress: *n* of *m* recipes |
| `paused_auth` | The stored session expired and could not be refreshed. | "Sign in to finish importing" — with a button that re-links |
| `succeeded` | The whole order history was handled. Individual recipes may still have failed. | "*n* recipes added", plus the failures and why |
| `dead` | Gave up: the attempt limit, or something retrying cannot fix (a layout change, a refusal). | The reason, and an option to try again later |
| `canceled` | The member unlinked, or their account was deleted. | Nothing in flight |

Phases inside `running` are the resume point, stored on the checkpoint:
`orders` → `recipes` → `import` → `done`.

**Claiming.** One `findOneAndUpdate` matches either a `queued` job whose
`availableAt` has passed or a `running` job whose lease expired, sets
`running`, the owner, and a new lease, and `$inc`s `attempts`. MongoDB applies
it to a single document, so however many workers race, a job goes to exactly
one of them. `TestIntegrationClaimJobIsExactlyOnceUnderConcurrency` is that
property.

**Leases.** `DefaultLease` is 8 minutes, longer than a capped run takes and
longer than the worker's own 6-minute run timeout, so a dyno killed mid-run
keeps its lease until after it is gone and the *next* run takes the job over
from its checkpoint. Every write a worker makes — checkpoint, lease extension,
finish, requeue — is conditional on `{status: running, leaseOwner: me}`. A
worker that lost the lease, or whose job was canceled, gets `ErrJobGone` and
stops.

**Checkpoints.** The order history is stored on the job once. After every batch
of 10 recipes reaches the import pipeline, the fetched IDs are appended to
`checkpoint.done`. A restart re-fetches at most those 10 — never the whole
history. Nothing is marked done before the import returns, so a crash costs a
re-fetch, never a lost recipe.

**Per-run cap.** A run fetches at most `MEAL_KIT_RECIPES_PER_RUN` recipe pages
(default 40), then checkpoints and requeues itself one minute out. A
400-recipe history therefore spreads over ten scheduled runs instead of one
long session against HelloFresh. Stopping on the cap deliberately does **not**
spend an attempt: a long history must not dead-letter itself for being long.

**Retries.** A transient failure requeues with exponential backoff (2 minutes
doubling, capped at 2 hours) plus up to a quarter again of jitter, so jobs that
failed in the same run do not retry in lockstep. After `MaxAttempts` (5) the
job is dead-lettered. Auth expiry, a refusal, and a parse failure are *not*
retryable and skip straight to `paused_auth` or `dead`.

## The credential lifecycle

> **A note on two earlier wordings.** This was first described as storing the
> credential "hashed". A hash is one-way: we could never replay it to fetch
> anything, so it cannot be what a background job needs. It was then built as a
> password sign-in: the app collected an email and password and the server
> exchanged them for tokens. That is gone too — HelloFresh has no password
> grant we can call, and asking for someone's password in our own UI was the
> wrong shape even when we thought it would work. **The member signs in on
> HelloFresh's page; we keep only the session it produces, encrypted.**

1. **Sign-in, on HelloFresh's site.** `MealKitWebLoginView` loads
   `https://www.hellofresh.com/login` in a `WKWebView` with a **non-persistent**
   data store. The member sees the real domain (it is printed under the title),
   their password manager fills it, and nothing in DinnerOS reads the page,
   evaluates script in it, screenshots it, or logs anything about it beyond
   which stage of the flow we are in.
   - `ASWebAuthenticationSession` cannot be used here: its ephemeral session
     gives no cookie access, and HelloFresh offers no third-party OAuth
     callback to redirect to.
2. **The session, out of the cookie.** On success HelloFresh writes the
   `apiV2Auth` cookie. The app polls its own web view's cookie store, parses
   the tokens out (`MealKitWebSession`), and `PUT`s **only** the access token,
   the refresh token, and the computed expiry to
   `.../meal-kit/hellofresh/link`. The cookie also carries `user_data` — email,
   id, roles. **None of it is decoded or sent.** The link's display label stays
   the generic `"HelloFresh account"`, as it already was.
3. **The web view is wiped.** When the screen goes away, whichever way it went,
   every website data type is removed from the store. No HelloFresh session
   lingers inside DinnerOS.
4. **At rest: envelope encryption.** Unchanged. Each link gets a random 32-byte
   data key (DEK). The tokens are sealed with the DEK (AES-256-GCM); the DEK is
   sealed with a key-encryption key (KEK) derived from
   `RECIPE_IMPORT_ENCRYPTION_KEY`. What MongoDB holds is
   `{keyId, key, ciphertext}` — useless without the config var.
5. **Startup.** With `MEAL_KIT_IMPORT_ENABLED=true` and no
   `RECIPE_IMPORT_ENCRYPTION_KEY`, the API and the worker refuse to start and
   say which variable is missing. There is no path that stores a token in the
   clear.
6. **Use.** Only the worker decrypts, and only into memory. `mealkit.Tokens`
   implements `slog.LogValuer` and prints as `redacted`.
7. **Refresh — not implemented, deliberately.** HelloFresh's refresh request has
   not been observed, and `hellofresh.Client.Refresh` therefore returns
   `ErrAuthExpired` without making a request. Guessing at an auth contract would
   mean firing an unknown request at someone else's auth service and retrying it
   when it failed. The cost is that an expired session pauses the job and the
   member signs in again through the web login, which is a few taps.
8. **Expiry.** As before: the job pauses at `paused_auth`, the link goes
   `needs_reauth`, the needs-attention notification goes out, and signing in
   again replaces the tokens and requeues the paused job where it stopped.
9. **Unlink.** `DELETE .../link` deletes the stored tokens first, then cancels
   every non-terminal job for that service.
10. **Account deletion.** A member's links go with their account (`PurgeUser`);
    a household's links and jobs go with the household (`PurgeHousehold`).

Nothing about the credential comes back over the API: the link response carries
a generic `accountLabel`, never an address and never anything derived from a
token. **What the API stores about the account is the access token, the refresh
token, and an expiry — nothing else.**

### What the sign-in screen handles

| What happens | What the member sees |
| --- | --- |
| They tap Cancel | The sheet closes; nothing is linked |
| The login page will not load | "We couldn't load the … sign-in page", with **Try Again** |
| They finish, but no cookie appears within 30 s | "We couldn't pick up your … sign-in", with **Try Again** |
| The login lands somewhere unexpected on HelloFresh | The 30 s clock starts there too, so it ends in the message above rather than spinning |
| The link call fails after a good sign-in | An alert with **Try Again**, which retries the link with the session already in hand — not another sign-in |

## What is verified, and what is not

These shapes were **captured live from a signed-in HelloFresh session** in a
browser (values redacted). That capture, not a public write-up, is the source.

**Verified.**

- The session cookie `apiV2Auth`: URL-encoded JSON carrying `access_token`,
  `refresh_token`, `expires_in`, `issued_at`, `refresh_expires_in`,
  `token_type`, and `user_data`.
- `GET /gw/my-deliveries/past-deliveries?country=US&from=<ISO week>&locale=en-US&rating-scale=5&subscription=<id>`
  → `200`, shaped `{"weeks":[{"week","menuId","meals":[…],"addons":[…]}]}`.
  Each meal carries `id`, `name`, `headline`, `prepTime`, `totalTime`, `image`,
  `websiteURL`, `tags`, `nutrition`, `cuisines`, `category`.
- `websiteURL` is the recipe page, so nothing has to build a URL from an id any
  more. It is still only followed when it already starts with
  `https://www.hellofresh.com/recipes/`; anything else is rebuilt from the id
  and name we validated ourselves.
- The old `GET /gw/api/customers/me/deliveries` **does not exist**. Neither
  does a password grant at `/gw/auth/token` that we can call.

**Not verified — one live run settles each.**

| Unknown | What this build does | How it fails if wrong |
| --- | --- | --- |
| Whether these endpoints accept `Authorization: Bearer <access_token>` rather than the cookie | Sends the bearer header | `401` → `ErrAuthExpired` → the job pauses and asks for a new sign-in |
| The `/gw/api/plans?includeCanceled=false` response shape | Accepts `{items:[…]}`, `{plans:[…]}`, or a bare array, and takes the first `id`/`subscriptionId` | `ParseError` "HelloFresh did not name a subscription this build can read" |
| How the history paging terminates | Starts at the current ISO week and steps `from` to the week before the earliest week each page returned; stops on an empty page, on no progress, or after 40 pages | Too few weeks imported, or the 40-page cap; never a loop |
| The refresh request | Does not make one (see above) | n/a — the member re-links |
| `country`/`locale` outside the US | Hard-coded `US`/`en-US` (`hellofresh.Options`) | Empty or wrong-region history for a non-US household |

**Diagnostics for the next live attempt.** `hellofresh.Client` takes a
`*slog.Logger` (wired from `cmd/server` and `cmd/importmealkit`). When a
response cannot be read it logs, at **debug** level only:

```text
level=DEBUG msg="meal-kit response was unreadable" source=hellofresh
  endpoint=plans|past-deliveries contentType=… finalPath=… bytes=… excerpt="…"
```

`finalPath` is the path after redirects with the query dropped (it carries the
subscription id). `excerpt` is at most 300 characters with any JSON field whose
name looks like a token, secret, password, cookie, authorization, or session
replaced, and any bare JWT removed. It is a *response* body, so it cannot
contain a password — the member never typed one into this process — and it is
redacted anyway. Turn it on with `LOG_LEVEL=debug` for one run; leave it off
otherwise.

### Add-ons are imported

`past-deliveries` separates `meals` from `addons` (garlic bread, a side salad).
Both are imported, with add-ons flagged `isAddon`, because the household paid
for them and ate them and will want to cook them again; the flag already exists
in the import contract, so the library can tell a side from a main without us
dropping half of what was delivered.

`meals[].tags` carries a `{"name":"Spicy","type":"spicy"}` tag. The importer
does not read tags from this endpoint — the recipe page is the source of truth
for the recipe itself — but it is there if the spicy-highlighting work ever
wants a cheaper signal than a page fetch.

## Being a good citizen

The policy lives in `mealkit.Fetcher` and is the same for every source:

| Rule | Value | Why |
| --- | --- | --- |
| Concurrency | 1 request at a time, per source, per run | No parallel hammering. A `Fetcher` is deliberately not safe for concurrent use. |
| Interval | ≥ 2.5 s between requests, plus up to 40% jitter | Slower than a person browsing; jitter keeps households out of lockstep. |
| Retries | 4 attempts, backoff doubling on top of the interval | Transient failures without a tight loop. |
| 429 | `Retry-After` honoured, capped at 60 s, then backoff | Do what we are told. A longer wait is left to the job's backoff rather than holding a dyno. |
| 5xx | Retried within the attempt budget | Their problem, not a reason to give up on the member. |
| 403 | **Stop the whole run immediately, dead-letter it** | We do not retry around an access control or work out how to look like someone else. |
| 401 | Refresh once, else pause for a new sign-in | An expired session is not a reason to hammer. |
| Per-run cap | 40 recipe pages | A real cap, not a comment. The rest waits for the next scheduled run. |
| User-Agent | `DinnerOS/1.0 (+https://api.tlps.dev; personal meal-kit order history import)` | Honest: who we are, why, and where to complain. No browser impersonation. |
| Scope | The account's plans, its past-deliveries endpoint, and the recipe pages it names | Only what the household ordered. |

**Fetched pages are data.** Nothing read from HelloFresh is ever treated as an
instruction, and no URL that comes back in account data is followed unless it
already starts with the recipe-page prefix — a delivery record naming another
host gets a URL rebuilt from the recipe ID we validated ourselves. Step text is
stored as recipe content and nothing more.

**Parse defensively.** Every response is checked for the shape this build
expects, and anything else is a `ParseError` carrying a message a member can
read ("HelloFresh has changed its page layout"), never a quote of the page. A
parse failure before any recipe has succeeded fails the job as a layout change;
one after that is recorded against that single recipe and the run continues.
Either way the library only ever receives complete, validated recipes.

## Failure modes, and what the member sees

| What happened | Job | Notification | In the app |
| --- | --- | --- | --- |
| Finished, everything imported | `succeeded` | "Your recipes are ready" | *n* recipes added |
| Finished, some recipes unreadable | `succeeded` | "…*k* could not be imported" | The list of failures with reasons |
| Session expired (there is no refresh) | `paused_auth` | "Sign in to finish importing" | A **Sign In Again** button that reopens the web login; progress is kept |
| HelloFresh changed its layout | `dead` (`parse`) | "Recipe import needs you" | "We could not read …. Nothing was changed in your recipes." |
| HelloFresh refused us (403) | `dead` (`blocked`) | "Recipe import needs you" | "…refused our requests… Try again later." |
| Network trouble | `queued`, retried | none until it gives up | "Importing your recipes…" |
| Five failed attempts | `dead` (`network`) | "Recipe import needs you" | The reason, and **Import Again** |
| A recipe the import pipeline rejected | `succeeded`, failure recorded | part of the finished one | The pipeline's own reason, e.g. "name is required" |
| Member unlinked | `canceled` | none | Nothing in flight; the account is gone |
| Worker dyno killed mid-run | stays `running`, lease expires | none | Progress unchanged; the next run resumes |

Review items (an unknown unit, a recipe with no steps) are not failures: they
are recorded by the shared pipeline and read through
`GET /api/v1/households/{householdId}/recipes/import-reviews`, exactly as for a
file import.

## Operating the worker

**Why Heroku Scheduler and not a worker dyno.** The app runs one Eco web dyno
and already pays for and operates Heroku Scheduler for `/sendreminders`. A
long-running worker process would mean a second always-on dyno for work that is
bursty (a household imports once, at onboarding) and deliberately slow (one
request every 2.5 seconds). Eco dynos also sleep, which a long-running consumer
cannot tolerate, and adding a Redis-backed queue would add an add-on to operate
for the same reason. A scheduler-driven command needs no new infrastructure,
and the lease plus the atomic claim already make overlapping or duplicated runs
safe. The cost is latency: a queued import waits up to one scheduler interval
before it starts, which is invisible against a job that takes several runs
anyway.

Set the job up once:

```bash
heroku addons:open scheduler -a dinneros-api
```

**Add Job** → every **10 minutes** → command `/importmealkit` → dyno size
**Eco**. A run claims at most 5 jobs, fetches at most 40 recipe pages per job,
and stops itself after 6 minutes.

Check a run by hand:

```bash
heroku run /importmealkit -a dinneros-api
```

It ends with `meal-kit import run finished` and counts: `claimed`, `succeeded`,
`paused`, `requeued`, `dead`, `gone`, `recipes`, `recipeFailures`. With the
feature off it logs that and exits 0, so the scheduled job is harmless on a
deployment that has not enabled it.

### When a run goes wrong

Everything below is read from the job documents; no log has a token in it.

**A household says nothing happened.** Read the status route as they would, or
in Mongo:

```js
db.meal_kit_jobs.find({householdId: ObjectId("…")}).sort({_id: -1}).limit(3)
```

`status`, `attempts`, `availableAt`, and `lastError` say which of the failure
modes above it is. `checkpoint.done.length` against `checkpoint.orders.length`
is the progress.

**A job is stuck `running`.** Its worker died. Nothing to do: the lease expires
within 8 minutes and the next scheduled run takes it over. To hurry it, clear
the lease:

```js
db.meal_kit_jobs.updateOne({_id: ObjectId("…")},
  {$set: {status: "queued", availableAt: new Date()}, $unset: {leaseOwner: "", leaseExpiresAt: ""}})
```

**A dead-lettered job should be retried.** Fix the cause first (a `parse`
failure means this build cannot read HelloFresh any more — that is a code
change, and retrying will only fail again). Then re-queue it, resetting
attempts so it gets a full budget:

```js
db.meal_kit_jobs.updateOne({_id: ObjectId("…")},
  {$set: {status: "queued", attempts: 0, availableAt: new Date()}})
```

Or simply have the member tap **Import Again**, which queues a fresh job; the
import is idempotent, so recipes already in the library are reported unchanged.

**Everything is failing with `blocked`.** HelloFresh is refusing us. Do not
raise the rate or change the User-Agent. Turn the feature off
(`heroku config:set -a dinneros-api MEAL_KIT_IMPORT_ENABLED=false`), which
makes the routes answer 503 and the scheduled run a no-op, and work out what
changed.

**Rotating `RECIPE_IMPORT_ENCRYPTION_KEY`.** Every stored link becomes
undecryptable, and jobs will pause for a new sign-in. That is the intended
failure: it is safe, not silent. Tell members to link again, or delete the
links first (`db.meal_kit_links.deleteMany({})`) so the app shows the
not-linked state cleanly rather than a pause.

**A member wants their meal-kit credentials gone.** Unlink in the app, or:

```js
db.meal_kit_links.deleteOne({householdId: ObjectId("…"), source: "hellofresh"})
```

then cancel their jobs. Deleting their DinnerOS account does both.

## Configuration

| Variable | Default | What it does |
| --- | --- | --- |
| `MEAL_KIT_IMPORT_ENABLED` | `false` | Turns the feature on. |
| `RECIPE_IMPORT_ENCRYPTION_KEY` | — | The KEK for stored tokens. **Required** when enabled; missing stops startup. `openssl rand -base64 48`. |
| `MEAL_KIT_RECIPES_PER_RUN` | `40` | Recipe pages one run fetches per job. Lower is more polite. |
| `MEAL_KIT_HELLOFRESH_BASE_URL` | HelloFresh | Overrides the account API origin; for testing against a stub. Must be https. |
| `LOG_LEVEL=debug` | `info` | Turns on the redacted response diagnostics above for one run. |

See [deployment.md](deployment.md#meal-kit-recipe-import) for the Heroku steps.

## The seam into the library

`mealkit.RecipePublisher` is the only way recipes leave this package:

```go
type RecipePublisher interface {
    Import(ctx context.Context, householdID string, file recipes.ImportFile) (recipes.ImportResult, error)
}
```

`*recipes.Service` satisfies it as it stands, through `mealkit.ServicePublisher`.
The worker, the queue, and the sources know nothing beyond this interface, so
when the global recipe catalog grows its own "publish an imported recipe"
interface, `publisher.go` is the one file that changes.

## Still to check on a device

The endpoints above were captured from a signed-in browser, and the client was
written against that capture, but **this flow has not yet run end to end from
the app**. One live attempt settles all of it. Watch, in order:

1. **The web login.** The sheet shows `www.hellofresh.com` under the title; the
   password manager offers to fill; after signing in the veil says "Finishing
   up…" and the sheet closes on its own. If it instead shows "We couldn't pick
   up your HelloFresh sign-in", the cookie name or domain has changed.
2. **The link call.** `PUT .../meal-kit/hellofresh/link` → `200`, and the
   status screen shows "HelloFresh account connected …". A `400` here means the
   cookie parsed to an empty access token.
3. **The worker run** (`heroku run /importmealkit`, or the local command), with
   `LOG_LEVEL=debug`:
   - `meal-kit import queued` → the job exists.
   - If the plans call is wrong: the debug line with `endpoint=plans` says
     whether it was HTML (a redirect to a login page — the bearer header is not
     accepted) or JSON in a shape we did not expect (read `excerpt` and fix
     `plansResponse`).
   - If past-deliveries is wrong: the same line with
     `endpoint=past-deliveries`.
   - On success: `recipesFound` on the job should be roughly the number of
     recipes the household has ever received, and the `from` walk should have
     stopped on an empty page rather than at the 40-page cap.
4. **The recipe pages.** These already work offline
   (`importers/hellofresh`), so a failure here is a rate-limit or a layout
   change, and the failure list on the job says which.

Only `hellofresh.go` changes if any of it is different: the queue, the worker,
the encryption, and the app do not care.
