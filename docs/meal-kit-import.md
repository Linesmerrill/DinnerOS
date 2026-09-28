# Meal-kit recipe import

A new household should not start empty. This is the onboarding path: the member
signs in on the meal kit's **own website** in a web view (HelloFresh first),
their order history is read **there, in their own session**, the app hands us
that list, and a backend worker imports **the recipes that account actually
ordered** into the household's library. A push notification says when it is
done, and a [Live Activity](#live-activity) shows progress on the Lock Screen in
between. Skipping is a first-class choice — recipes can be added by hand later.

> **Nothing about the meal-kit account is stored.** Not a password, not a
> session token, not a cookie, not an email address. There is no field for one
> in the database, no field for one on the API, and no key to encrypt one with.
> What the server keeps is a list of recipe ids and public page URLs, and what
> it fetches are pages anyone can open.

Implemented in `api/internal/mealkit` (queue, service, worker, HTTP),
`api/internal/mealkit/hellofresh` (the public recipe-page reader), and
`api/cmd/importmealkit` (the scheduled worker). The iOS side is
`ios/DinnerOS/Core/MealKit` and `ios/DinnerOS/Features/Household`.

## What it is not

- It does not crawl HelloFresh. The app reads the account's own plans and
  past-deliveries endpoints, in the member's own session; the server requests
  nothing but the public recipe pages that history named.
- It does not write to the recipe collections. Everything goes through
  `recipes.Service.Import`, the same pipeline a file import uses
  ([import-format.md](import-format.md)), so imported recipes get the same
  validation, alias splitting, ingredient creation, and review items.
- It does not ask for a password, and it does not keep a session. The member
  signs in on HelloFresh's own page, in a web view; their order history is read
  there and thrown away with the web view. See
  [Where the credential is (and is not)](#where-the-credential-is-and-is-not).

## The shape

```text
iPhone                                    API (web dyno)     MongoDB     Worker (web runner, or Scheduler)
  │  WKWebView → hellofresh.com/login                   │                      │
  │  (their password, their site, their password manager)                      │
  │  ◀── apiV2Auth cookie (stays in the web view)       │                      │
  │  GET .../meal-kit/hellofresh → history cursor ──────│ meal_kit_cursors     │
  │  in-page, in their session:                         │                      │
  │    /gw/api/plans → subscription id                  │                      │
  │    /gw/my-deliveries/past-deliveries, week by week, │                      │
  │      resuming at the cursor's earliest week         │                      │
  │  ◀── recipe ids + public URLs + weeks + where it stopped                   │
  │  [web view wiped]                                   │                      │
  │  POST .../meal-kit/hellofresh/imports               │                      │
  │  {recipes:[…], harvest:{…}} ─────────── validate ──▶│ meal_kit_jobs        │
  │                                          cursor ───▶│ meal_kit_cursors     │
  │  ◀──── 202 {job}          (app can close)           │                      │
  │                                   kick ─ runner in the same process, at once ─▶│
  │                                                     │◀── claim (findAndModify)
  │                                                     │    public recipe pages (rate limited)
  │                                                     │    recipes.Service.Import
  │                                                     │◀── checkpoint after each batch
  │  ◀──── push: "Your recipes are ready" ─── notifications outbox ◀───────────┘
  │  GET .../meal-kit/hellofresh  →  what was imported, what failed and why
```

Note what is **not** in that diagram: any arrow carrying a credential to the
API, and any box holding one.

The worker on the right is one piece of code run from two places: the **web
runner** inside the API process, which starts a job the moment it is queued and
keeps going batch by batch, and **`/importmealkit` on Heroku Scheduler**, the
backstop. Both claim from the same queue with the same find-and-modify
([Who does the work](#who-does-the-work)).


## Job lifecycle

A job is one import run for one household. States:

| State | What it means | What the member sees |
| --- | --- | --- |
| `queued` | Waiting for a worker: for the few seconds between queueing and the web runner claiming it, then resting between batches. `availableAt` is when it may be claimed again, and the status route returns it as `nextRunAt`. | "Starting your import. 740 recipes to import. Starting now." at first; between batches the bar stays put and it says "50 of 740 recipes imported. Next batch in about a minute." (or "Trying again in about 4 minutes." after a hiccup) |
| `running` | A worker holds the lease. `recipesDone` climbs at every checkpoint of 10. | A real bar: "12 of 740 recipes imported. You can close the app." |
| `succeeded` | The whole order history was handled. Individual recipes may still have failed. | "*n* recipes added", plus the failures and why |
| `dead` | Gave up: the attempt limit, or something retrying cannot fix (a layout change, a refusal). | The reason, and an option to try again later |
| `canceled` | The member stopped the import, or their household was deleted. | Nothing in flight |

There is no `paused_auth` any more, because there is no stored session to
expire. A job starts at the `recipes` phase — the order history arrived with
the request that queued it — so the phases are `recipes` → `import` → `done`.

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
stops. The web runner is gentler still when it is *told* it is stopping (a
restart, an Eco dyno going to sleep): it hands the job back to the queue, due
at once, without spending an attempt (`WorkerOptions.ReleaseOnCancel`), so the
next worker resumes in seconds rather than after the lease. Only a process that
dies without warning leaves a lease to expire.

**Checkpoints.** The order history is stored on the job when it is queued. After every batch
of 10 recipes reaches the import pipeline, the fetched IDs are appended to
`checkpoint.done`. A restart re-fetches at most those 10 — never the whole
history. Nothing is marked done before the import returns, so a crash costs a
re-fetch, never a lost recipe.

**Per-run cap.** A scheduled run fetches at most `MEAL_KIT_RECIPES_PER_RUN`
recipe pages (default 40) per job, then checkpoints and requeues it one minute
out. A web-runner batch is the same thing with a cap of 50
(`mealkit.DefaultBatchSize`). Stopping on the cap deliberately does **not**
spend an attempt: a long history must not dead-letter itself for being long.

### Who does the work

In production on 2026-09-27 a member imported a 740-recipe history and the
screen sat on "Import queued" with an empty bar. Nothing was wrong with the
worker: the queue was only ever drained by a Heroku Scheduler job for
`/importmealkit`, that job had not been created, and so nothing ran. One pass
run by hand imported 40 recipes in about 127 seconds with no failures.

So the API process now does the work itself, with the Scheduler job as the
backstop. `mealkit.Runner` (`runner.go`) is one goroutine in the web process
that drives an ordinary `mealkit.Worker` — the same claim, lease, checkpoints,
`Fetcher` politeness and 401/403 stop as the scheduled command, not a second
code path:

1. **Start immediately.** `POST .../imports` queues the job and `Service`
   kicks the runner (`Service.SetKicker`). The request returns `202 queued`
   straight away; the runner claims *that* job by ID
   (`Store.ClaimJobByID`, the same find-and-modify with `_id` added) within
   milliseconds, and the status the app polls says `running`. The first
   checkpoint — `recipesDone` 10 — lands about 30 seconds later.
2. **One batch, then rest.** A batch stops on 50 pages, checkpoints, and
   requeues the job one minute out (`DefaultBatchPause`, the same rest a capped
   scheduled run gives). Fifty is about 150 seconds at the polite ~3 s a page
   (about 160 s with the import, from the 127 s / 40 measured): a bar that
   visibly crosses a real distance, well inside the 8-minute lease (extended
   at every checkpoint anyway), and short enough that another household's
   import never waits long behind it.
3. **Keep going.** Between kicks the runner polls every 20 seconds for any due
   job — its own rested ones, one whose worker died (an expired lease), or one
   the scheduled command put down — and runs the next batch. An import
   therefore finishes without the Scheduler job, as long as the web dyno is
   awake.
4. **Stop cleanly.** Canceling the runner (SIGTERM on a restart or when an Eco
   dyno sleeps) hands the job in hand back to the queue, due at once. The web
   server waits for that one write before it closes the database.

**Why keep going in the web process rather than stop after the first batch.**
A first-batch-only runner would leave the rest of a 740-recipe history to a
Scheduler job that has already once been missing, and the member's bar would
stop at 50 for as long as that took. Continuing costs almost nothing: the work
is one goroutine that mostly sleeps (one request every 2.5–3.5 s, then a
minute's rest), holds one page in memory at a time, and never touches a request
path — queueing returns before any fetching starts, so request latency is
unchanged. Its limits are real and handled rather than hidden:

- **The Eco dyno sleeps** after 30 minutes without HTTP traffic, and a
  goroutine is not traffic. While the member watches, the app's status poll
  keeps the dyno awake. If they close the app the dyno sleeps partway through,
  the runner hands the job back, and either the next scheduled run continues it
  or, without one, it waits until the next request wakes the dyno — the app
  opening is such a request, and the runner's first act on start-up is to
  drain the queue. This is why the Scheduler job is still recommended
  ([deployment.md](deployment.md#meal-kit-recipe-import)).
- **One web process** means one polite conversation: the runner is a single
  goroutine, so this process never fetches for two batches at once and never
  runs two batches for one household. Across processes the atomic claim and
  the lease hold the line: a scheduled run that fires mid-batch finds the job
  leased and gets nothing (`TestAScheduledRunCannotClaimAJobTheRunnerIsWorking`,
  and the other way round), and a claim by ID racing a scheduled claim in
  MongoDB is won exactly once
  (`TestIntegrationClaimByIDRacesTheScheduledClaimExactlyOnce`). At most two
  conversations with HelloFresh exist at once — the web runner and one
  scheduled run, each on a different household's job.
- **A web dyno killed without warning** leaves its lease; nobody touches the
  job until it expires, then any worker takes it over from the checkpoint
  (`TestTheRunnerTakesOverALeaseHeldByADeadProcess`).

The scheduled command is unchanged: 40 pages per job per run, a one-minute
rest after the cap, and a cut-off run leaves its lease to expire.

**Retries.** A transient failure requeues with exponential backoff (2 minutes
doubling, capped at 2 hours) plus up to a quarter again of jitter, so jobs that
failed in the same run do not retry in lockstep. After `MaxAttempts` (5) the
job is dead-lettered. A refusal and a parse failure are *not* retryable and
skip straight to `dead`.

## Where the credential is (and is not)

> **A note on three earlier designs.** This was first described as storing the
> credential "hashed" — a hash is one-way, so a background job could never
> replay it. It was then a password sign-in in our own UI, exchanged
> server-side for tokens; HelloFresh has no password grant to exchange it with,
> and collecting someone else's password was the wrong shape anyway. It was
> then a web-view sign-in whose **session tokens** we stored, encrypted. That
> is gone too, and this is why:
>
> - A HelloFresh access token lives **1800 seconds**. An import that spans
>   several scheduled runs outlives it within the hour.
> - **There is no refresh we can use.** `POST /gw/auth/token` answers `400
>   unsupported_grant_type` for every grant we tried (`refresh_token`,
>   `refresh`, `refresh-token`, `refreshToken`, `password`,
>   `client_credentials`, `authorization_code`); form-encoded gives a `500`;
>   `/gw/auth/refresh` is a `404`.
> - So a stored token was a credential we held, could not renew, and which was
>   dead by the time the worker used it. That is the worst of both worlds:
>   the risk of holding it without the benefit.
>
> The fix is not a better vault. It is **not holding it at all**.

1. **Sign-in, on the meal kit's site.** `MealKitWebLoginView` loads
   `https://www.hellofresh.com/login` in a `WKWebView` with a **non-persistent**
   data store. The member sees the real domain (it is printed under the title),
   their password manager fills it, and nothing in DinnerOS reads the page,
   screenshots it, or logs anything about it beyond which stage of the flow we
   are in.
   - `ASWebAuthenticationSession` cannot be used here: its ephemeral session
     gives no cookie access, and HelloFresh offers no third-party OAuth
     callback to redirect to.
2. **The order history is read there.** The app reads the `apiV2Auth` cookie
   from its own web view (`MealKitWebSession` — access token and token type,
   nothing else) and passes it into `MealKitHarvestScript`, which runs in an
   **isolated content world** inside that page. The script walks the account's
   own endpoints, and the token never leaves the web view.
3. **What comes back** is a list: recipe id, public page URL, delivery weeks,
   add-on or not. That, and only that, is `POST`ed to
   `.../meal-kit/hellofresh/imports`. There is no field on that request for a
   token, and the body is `additionalProperties: false`, so an app that tried
   to send one would get a `400`.
4. **The web view is wiped** when the screen goes away, whichever way it went:
   every website data type is removed from the store. No meal-kit session
   lingers inside DinnerOS, and none was ever written outside the web view.
5. **The worker holds nothing.** It fetches `https://www.hellofresh.com/recipes/…`
   pages, which need no session at all, so it can take as long as it likes and
   there is nothing on it that can expire.
6. **Re-importing is re-reading.** There is no "re-link": **Import Again**
   opens the same web view, reads the history afresh, and queues a new run.
   That is also how an incremental re-sync works — see
   [Importing twice](#importing-twice).
7. **Stopping.** `DELETE .../imports` cancels every run in flight. There is
   nothing to delete besides, which is the point.
8. **Account deletion.** A household's import runs go with the household
   (`PurgeHousehold`). There is no `PurgeUser` any more: this package stored a
   member's meal-kit tokens, and now stores nothing that belongs to one person.

### What the member's untrusted-input surface is

The harvested list arrives from an app, having been read out of someone else's
web page. It is treated as hostile input at three points:

| Where | What it enforces |
| --- | --- |
| The app, `MealKitService.canImport` | 24 lowercase hex id; URL starts with `https://www.hellofresh.com/recipes/` |
| The service, `Source.NormalizeOrder` | The same, server-side: a bad id is dropped, a URL off the recipe prefix is **rebuilt** from the id and name, weeks must be ISO weeks, names are trimmed to 200 bytes, at most 1000 recipes and 200 weeks each |
| The source, before each fetch | The URL is checked against the prefix again |

A list with nothing usable in it is a `400`; a list with *some* junk in it
imports the rest, because one odd row should not cost a member their other two
hundred recipes.

### What the sign-in screen handles

| What happens | What the member sees |
| --- | --- |
| They tap Cancel | The sheet closes; nothing is imported |
| The login page will not load | "We couldn't load the … sign-in page", with **Try Again** |
| They finish, but no cookie appears within 30 s | "We couldn't pick up your … sign-in", with **Try Again** |
| The login lands somewhere unexpected on the site | The 30 s clock starts there too, so it ends in the message above rather than spinning |
| A `403` part-way through reading the history | "Your … session ended before we finished reading your orders", with **Try Again** |
| The account has no past deliveries | "We couldn't find any past deliveries on that … account" |
| Their API answered in a shape we cannot read | "…can't read. Nothing was changed in your recipes" — and **no** Try Again, because retrying cannot help |
| They close the sheet mid-read | The task is cancelled with the view and the web view is wiped; nothing is queued |
| The queueing call itself fails | An alert with **Try Again**, which retries the `POST` with the list already in hand — not another sign-in |

### Importing twice

Running an import again is safe and is the only "re-sync" there is. The
recipes pipeline matches on `source` plus `sourceRecipeId`
([import-format.md](import-format.md)), so a recipe already in the library
comes back as `unchanged` (or `updated` when its page changed), never as a
second copy. `TestStartImportNeverDuplicatesARunInFlight` covers the other
half: a second import while one is in flight returns the run already going
rather than queueing a duplicate, and
`TestASecondImportOfTheSameRecipesChangesNothing` covers the first: the second
pass reports `unchanged`, imports nothing, and fails nothing.

## Reading a long history, over several sittings

**Measured, on 2026-09-21, against a real account:** one harvest returned
**740 recipes across 160 delivered weeks in 40 pages**, and it stopped because
it hit the app's 40-page cap — this household has about four years of history.
A `past-deliveries` page covers roughly **four to five weeks**, so the walk
steps back about a month per request and 40 pages reaches about three years.
One sitting cannot finish four years, and the old build stopped at the cap
*silently*.

So the harvest says where it stopped, and the server remembers it.

**The harvest reports.** `MealKitHarvest` now carries `earliestWeek`,
`latestWeek` and `stopped`, and they are posted with the recipes
(`MealKitHarvestReport` in [openapi.yaml](../api/openapi.yaml)). `stopped` is
one of:

| `stopped` | What it means | Effect on the cursor |
| --- | --- | --- |
| `cap` | The walk hit its own page cap. There is more history behind it. | Floor moves down; **not** complete |
| `empty` | A page came back with no delivered weeks: the start of the account's history. | Complete |
| `end` | The walk stopped moving backwards. Treated as the start of the history. | Complete |
| `caught_up` | It reached weeks already imported. What a catch-up pass does every time. | Ceiling moves up; completeness unchanged |

**The server remembers.** `meal_kit_cursors` holds one document per household
and source (unique index on `{householdId, source}`): `earliestWeek`,
`latestWeek`, `complete`, and `blockedAt`. That is the whole of it — two ISO
weeks and two flags. There is still nothing about the meal-kit account
anywhere, which is the promise at the top of this page and is why the cursor is
weeks rather than, say, a page token of theirs.

`Cursor.Merge` only ever gains ground: the floor moves down, the ceiling moves
up, and `complete` is sticky, so a replayed or out-of-order report cannot lose
history and **a completed history stays completed**.

**The next harvest resumes.** `GET .../meal-kit/{source}` returns the cursor as
`history`, and the app plans the walk from it
(`MealKitHarvestPlan.segments(for:)`) as up to two segments:

1. **Catch-up** — from today back to `latestWeek`, then stop. This is how a
   *new* delivery is picked up. On a household whose history is finished it is
   the whole plan, and it normally costs one page.
2. **Resume** — starting the week *before* `earliestWeek`, walking back until
   the history runs out or the page cap bites again. Only for a history that
   is not complete.

A household that has never imported gets one segment from today with no floor,
which is exactly what the old build did.

Two details that matter:

- A segment **with a floor can never report the start of the history**. An
  empty page near today means "nothing new", not "you never ordered", so it
  reports `caught_up` and the cursor's `complete` is left alone.
- If the *catch-up* segment runs out of pages, it never reached today's end of
  the history, so the harvest reports **no** `latestWeek` and the server leaves
  its ceiling where it was. The next pass walks that gap rather than skipping
  it.

**A partial harvest posts what it has.** The 1000-recipe cap on a submitted
history is comfortably above the 740 measured, and the recipes a capped walk
did reach belong in the library now — not after however many more sign-ins the
rest of the history takes. A capped harvest is queued exactly like a whole one.

**Only the member can fetch the rest.** The server cannot walk their order
history; that happens in their own browser session, in the web view. So the
app says there is more and that importing again continues from where it
stopped — and nothing anywhere promises the server will finish it alone.

### How long a 740-recipe history takes

Server-side fetching is the slow half, deliberately. The per-page rate is the
same everywhere; what differs is how long a job rests between stretches of
work:

| | Web runner (normal) | Scheduler only (backstop) |
| --- | --- | --- |
| Interval per recipe page | 2.5 s + up to 40% jitter ≈ **3 s** | the same |
| First visible progress | ~30 s after queueing (checkpoint of 10) | up to 10 minutes, then ~30 s |
| Per stretch of work | 50 pages ≈ **2.5 minutes** | 40 pages ≈ **2 minutes** |
| Rest between stretches | 1 minute + up to one 20 s poll | until the next run, 10 minutes |
| 740 recipes | 740 ÷ 50 = **15 batches** | 740 ÷ 40 = **19 runs** |
| Average over the import | one page every ~4.4 s | one page every ~15 s |
| **End to end** | **about 55 minutes** | **about 3 hours** |

`TestTheWebBatchesFinishALongHistoryInAboutAnHourAtThePoliteRate` and
`TestThePolitenessBudgetSpreadsALongHistoryOverHours` are that arithmetic, so
raising a cap or shortening a rest fails a test rather than quietly turning the
importer into a crawler. If the member closes the app and the Eco dyno sleeps
partway through, the rest continues at the Scheduler's pace.

Two harvests are needed for the measured history (40 pages reaches about three
years; the rest is one more sitting), and the recipes from the first are
importing while the member decides whether to do the second.

**One household, one job.** `StartImport` returns the run already in flight for
the same source and *refuses* a run on another source while one is going
(`AnyActiveJob`). A job is claimed by exactly one worker
(`TestIntegrationClaimJobIsExactlyOnceUnderConcurrency`) and every write is
conditional on its lease, so "never two jobs for one household at once" holds
across overlapping scheduled runs, not just within one.

## What is verified, and what is not

These were **measured against a signed-in HelloFresh session**, not guessed.

**Verified.**

- The session cookie `apiV2Auth`: URL-encoded JSON carrying `access_token`,
  `refresh_token`, `expires_in`, `issued_at`, `refresh_expires_in`,
  `token_type` and `user_data`. Only `access_token` and `token_type` are read,
  and only inside the web view.
- `GET /gw/my-deliveries/past-deliveries?country=US&from=<ISO week>&locale=en-US&rating-scale=5&subscription=<id>`
  → `200` with `Authorization: <token_type> <access_token>` and **no cookies**.
  Shaped `{"weeks":[{"week","menuId","meals":[…],"addons":[…]}]}`; each meal
  carries `id`, `name`, `headline`, `prepTime`, `totalTime`, `image`,
  `websiteURL`, `tags`, `nutrition`, `cuisines`, `category`.
- With no auth, or a stale token, the same request is **`403`, not `401`**.
  That 403 is what made the old worker give up: the token it had stored had
  already expired.
- **Access tokens live 1800 seconds**, and the page silently mints a fresh one
  on load. This is why the reads happen in the page and not on a worker that
  runs minutes later.
- **There is no refresh we can use.** `POST /gw/auth/token` with JSON answers
  `400 unsupported_grant_type` for `refresh_token`, `refresh`, `refresh-token`,
  `refreshToken`, `password`, `client_credentials` and `authorization_code`;
  form-encoded gives a `500`; `/gw/auth/refresh` is a `404`.
- **Recipe pages need no auth at all**:
  `https://www.hellofresh.com/recipes/<slug>-<id>` is `200` with no session.
  That is the whole reason the server can do its half without a credential.
- The old `GET /gw/api/customers/me/deliveries` does not exist.

**Not verified — one run on a device settles each.**

| Unknown | What this build does | How it fails if wrong |
| --- | --- | --- |
| The `/gw/api/plans?includeCanceled=false` response shape | **Measured 2026-09-21:** a bare array of plans, whose `legacySubscriptionId` (a number, e.g. `16908749`) is the id the order history wants. A plan's own `id`, and the `hf_plan_id` cookie, are the customer plan's UUID — `past-deliveries` answers **403** to those, which is what made the first live attempt fail. The script accepts `{items:[…]}`, `{plans:[…]}` or a bare array, and takes the first numeric `legacySubscriptionId`/`subscriptionId` | The sheet says "…can't read", with no **Try Again** |
| How the history paging terminates | Starts at the current ISO week and steps `from` to the week before the earliest week each page returned; stops on an empty page, on no progress, or after 40 pages | Too few weeks imported, or the 40-page cap; never a loop |
| Whether `apiV2Auth` is `HttpOnly` | Reads it from `WKHTTPCookieStore`, which sees `HttpOnly` cookies, rather than `document.cookie`, which does not | n/a — this is the working-either-way choice |
| `country`/`locale` outside the US | Hard-coded `US`/`en-US` (`MealKitService`) | An empty or wrong-region history for a non-US household |

**Diagnostics.** `hellofresh.Client` takes a `*slog.Logger` (wired from
`cmd/server` and `cmd/importmealkit`). When a recipe page cannot be read it
logs, at **debug** level only:

```text
level=DEBUG msg="meal-kit recipe page was unreadable" source=hellofresh
  sourceRecipeId=… contentType=… finalPath=… bytes=… excerpt="…"
```

`excerpt` is at most 300 characters with any JSON field whose name looks like a
token, secret, password, cookie, authorization or session replaced, and any
bare JWT removed. Nothing this client fetches should contain a credential —
which is exactly why a page that does must not reach a log. Turn it on with
`LOG_LEVEL=debug` for one run; leave it off otherwise.

The app's own logging is counts only: how many recipes and weeks were read, and
a one-word reason when they were not. Never the page, never the cookie, never
the email.

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
| Concurrency | 1 request at a time, per source, per process | No parallel hammering. A `Fetcher` is deliberately not safe for concurrent use, and the web runner is a single goroutine. At most the web runner and one scheduled run talk to HelloFresh at once, on different households' jobs. |
| Interval | ≥ 2.5 s between requests, plus up to 40% jitter | Slower than a person browsing; jitter keeps households out of lockstep. |
| Retries | 4 attempts, backoff doubling on top of the interval | Transient failures without a tight loop. |
| 429 | `Retry-After` honoured, capped at 60 s, then backoff | Do what we are told. A longer wait is left to the job's backoff rather than holding a dyno. |
| 5xx | Retried within the attempt budget | Their problem, not a reason to give up on the member. |
| 401 / 403 | **Stop the whole run immediately, dead-letter it** | Nothing the worker fetches needs a session, so being turned away is a refusal, not an expired credential. We do not retry around an access control or work out how to look like someone else. |
| Per-run cap | 40 recipe pages per scheduled run; 50 per web-runner batch, then a minute's rest | A real cap, not a comment. The rest waits for the next batch or run. |
| User-Agent | `DinnerOS/1.0 (+https://api.tlps.dev; personal meal-kit order history import)` | Honest: who we are, why, and where to complain. No browser impersonation. |
| Scope | Public recipe pages, and only the ones the household's own order history named | Only what the household ordered. |

| Refusal cooldown | 6 hours, per household and source | A 403 is recorded on the cursor and `StartImport` refuses a new run until it passes, with a sentence saying why. Backing off hard beats letting a member re-queue 740 pages at a service that just turned us away. |

The same politeness applies **in the app**, where the account reads happen:
`MealKitHarvestScript` makes one request at a time, 500 ms apart, at most 40
pages **across all its segments**, and stops on an empty page or a walk that
stops moving backwards. A member watching a spinner sets the interval; the cap
is what stops a paging change becoming a crawl. Resuming from the cursor is
also politeness: a second harvest does not re-read three years of pages to
reach the one week it needs.

**Fetched pages are data.** Nothing read from HelloFresh is ever treated as an
instruction, and no URL is followed unless it already starts with the
recipe-page prefix — an entry naming another host gets a URL rebuilt from the
recipe ID we validated ourselves. Step text is stored as recipe content and
nothing more. The harvest script reads nothing from the DOM, runs in an
isolated content world so the page cannot see or change what it does, and
returns only ids, URLs and weeks.

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
| Finished, some recipes unreadable | `succeeded` | "…*k* couldn't be imported. Open Recipe Import to see which." | The list under **Couldn't Import**, each with one plain sentence (below) |
| The same dish under two recipe ids | `succeeded`, merged | none | Nothing: it is counted as already in the library, and its delivery weeks land on the one recipe |
| The session ended while reading the history | no job is queued | none | The sheet says so, with **Try Again** — nothing was half-imported |
| HelloFresh changed its layout | `dead` (`parse`) | "Recipe import needs you" | "We couldn't read HelloFresh's recipe pages. Nothing was changed in your recipes." |
| HelloFresh refused us (403) | `dead` (`blocked`) | "Recipe import needs you" | "HelloFresh turned us away, so we stopped. Try again later." Importing is then refused for 6 hours with a sentence saying so, rather than queued. |
| The harvest stopped at its page cap | `succeeded` for what it got | "…You have older orders too. Open Recipe Import to get them." | "Got your HelloFresh orders back to April 2023. Tap Import Again for older ones." and **Import Again** |
| A catch-up found nothing new | no job is queued | none | "Already up to date" — not an error, and not an empty-account message |
| Network trouble | `queued`, retried | none until it gives up | The bar where it was, and "50 of 740 recipes imported. Trying again in about 4 minutes." |
| Five failed attempts | `dead` (`network`) | "Recipe import needs you" | The reason, and **Import Again** |
| A recipe the import pipeline rejected | `succeeded`, failure recorded | part of the finished one | "This recipe was missing details we need." The pipeline's own wording ("name is required") goes to the log |
| Member stopped the import | `canceled` | none | Nothing in flight; imported recipes stay |
| Web dyno restarted, or asleep, mid-batch | handed back to `queued`, due at once | none | "Next batch starting now"; the next worker resumes from the checkpoint |
| Worker dyno killed without warning | stays `running`, lease expires | none | Progress unchanged for up to 8 minutes; the next worker resumes |
| Scheduler job missing | the web runner carries on alone | as usual | As usual while the dyno is awake; a sleeping dyno resumes on the next request |

**What a member reads about one recipe.** Every entry under **Couldn't
Import** carries one of three fixed sentences (`reasons.go`), never the
pipeline's wording, a field name, a list index, a recipe id or an HTTP code:

| What happened | What the member reads |
| --- | --- |
| The recipe page is gone (404, 410) | "This recipe is no longer on HelloFresh." |
| The page could not be read | "We couldn't read this recipe on HelloFresh." |
| The library refused it (a missing name, no ingredients) | "This recipe was missing details we need." |

The detail behind each goes to the log with the recipe id. Jobs stored before
this carried raw wording (production showed "Garlic Bread — matches the same
stored recipe as recipes[1]"); the status route passes every stored reason
through the same sentences and drops the old duplicate entries, so an old job
reads correctly too. `TestNoFailureReasonReadsLikeAnInternalMessage` and
`TestStoredRawReasonsNeverReachTheMember` hold that line.

**One dish, two recipe ids, is a merge.** HelloFresh re-releases a dish under a
new id, and an add-on ordered in different weeks can carry different ids. The
library's identity rule keeps one recipe for it and refuses the second copy in
a batch as a duplicate (`recipes.RecipeError.Duplicate`). The worker treats
that as a merge, not a failure: the second copy's delivery weeks are carried
onto the recipe it matched with one more small import of that recipe, and it
is counted as already in the library
(`TestIntegrationTheSameDishUnderTwoIdsLandsOnOneRecipe`, against the real
pipeline).

Review items (an unknown unit, a recipe with no steps) are not failures: they
are recorded by the shared pipeline and read through
`GET /api/v1/households/{householdId}/recipes/import-reviews`, exactly as for a
file import, and resolved from the app (see
[import-format.md](import-format.md#loading-into-dinneros)).

**One review number.** The Import Review row on Recipe Import and on the
Household tab both show the household's open backlog, the same rows the screen
lists. A job's `reviewItems` is what that run flagged, and it can include items
an earlier import (or the offline file import) already recorded, so it is not
shown as a badge; showing it produced "Import Review 3" over a screen of 90.

**What the finished run says.** Recipes that were new, then recipes whose
order history changed, then any that could not be imported, one per line:
"5 new recipes." / "671 updated with your order history." A run that changed
nothing reads "Your recipes were already up to date."

## Operating the worker

**Why the web process and Heroku Scheduler, and not a worker dyno.** The app
runs one Eco web dyno and already pays for and operates Heroku Scheduler for
`/sendreminders`. A long-running worker process would mean a second always-on
dyno for work that is bursty (a household imports once, at onboarding) and
deliberately slow (one request every 2.5 seconds), and a Redis-backed queue
would be an add-on to operate for the same job. The web runner does the work
while someone is around to watch it ([Who does the work](#who-does-the-work));
the Scheduler job finishes what a sleeping Eco dyno put down. Neither needs new
infrastructure, and the lease plus the atomic claim make them safe together.
Relying on the Scheduler alone was the original design, and its cost turned out
not to be invisible: a member saw "queued" and nothing moving, and when the job
was missing, nothing ran at all.

The Scheduler job is still **recommended**, as the backstop. Set it up once:

```bash
heroku addons:open scheduler -a dinneros-api
```

**Add Job** → every **10 minutes** → command `/importmealkit` → dyno size
**Eco**. A run claims at most 5 jobs, fetches at most 40 recipe pages per job,
and stops itself after 6 minutes. A job the web runner holds is not claimable,
so the two never double up.

The web runner logs `meal-kit import runner started` at boot and one
`meal-kit import batch finished` line (with `worker=web` and the same counts
as the scheduled run) per batch:

```bash
heroku logs -a dinneros-api --tail | grep 'meal-kit import'
```

Check a run by hand:

```bash
heroku run /importmealkit -a dinneros-api
```

It ends with `meal-kit import run finished` and counts: `claimed`, `succeeded`,
`requeued`, `dead`, `gone`, `recipes`, `recipeFailures`. With the
feature off it logs that and exits 0, so the scheduled job is harmless on a
deployment that has not enabled it.

### When a run goes wrong

Everything below is read from the job documents. There is no token in a log
because there is no token anywhere — the only collection this feature has is
`meal_kit_jobs`.

**A household says nothing happened.** Read the status route as they would, or
in Mongo:

```js
db.meal_kit_jobs.find({householdId: ObjectId("…")}).sort({_id: -1}).limit(3)
```

`status`, `attempts`, `availableAt`, and `lastError` say which of the failure
modes above it is. `checkpoint.done.length` against `checkpoint.orders.length`
is the progress.

**A job is stuck `running`.** Its worker died without warning (a graceful
stop hands the job back at once). Nothing to do: the lease expires within 8
minutes and the web runner (or the next scheduled run) takes it over. To hurry
it, clear the lease:

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

**A member wants their meal-kit credentials gone.** There is nothing to
delete: we never had them. If a deployment still has the old `meal_kit_links`
collection from before this redesign, drop it — it holds encrypted tokens that
nothing reads any more:

```js
db.meal_kit_links.drop()
```

`RECIPE_IMPORT_ENCRYPTION_KEY` can be removed from the config at the same time;
a leftover value is ignored rather than a startup failure, so the order does
not matter.

**Stopping a household's import.** They can tap **Stop Importing**, or:

```js
db.meal_kit_jobs.updateMany({householdId: ObjectId("…"), status: {$in: ["queued", "running"]}},
  {$set: {status: "canceled", finishedAt: new Date(), updatedAt: new Date()}})
```

## Configuration

| Variable | Default | What it does |
| --- | --- | --- |
| `MEAL_KIT_IMPORT_ENABLED` | `false` | Turns the feature on. It is the only setting the feature needs: there is no secret, because nothing about a meal-kit account is stored. |
| `MEAL_KIT_RECIPES_PER_RUN` | `40` | Recipe pages one scheduled run fetches per job. Lower is more polite; set below 50, it lowers the web runner's batch too. |
| `MEAL_KIT_HELLOFRESH_BASE_URL` | HelloFresh | Overrides the origin recipe pages are read from; for testing against a stub. Must be https. |
| `LOG_LEVEL=debug` | `info` | Turns on the redacted response diagnostics above for one run. |

See [deployment.md](deployment.md#meal-kit-recipe-import) for the Heroku steps.

## Live Activity

A 740-recipe import takes about three hours of polite batches, so the progress
lives on the Lock Screen and in the Dynamic Island rather than only on the
Recipe Import screen.

| State | Lock Screen / expanded island | Compact island | Minimal |
| --- | --- | --- | --- |
| `importing` | "HelloFresh recipes", **40** of 740 recipes (SF Rounded), progress bar, "Importing" | fork-and-knife, `40/740` | circular progress |
| `waiting` | same, "Starting soon" (nothing done yet) or "Next batch soon" | same | same |
| `done` | **Green** banner (`DoneBackground`, white text): "740 recipes imported", full bar, "All done" or "3 couldn't be read" | check, `740` | green check |
| `failed` | "Import stopped", "Open DinnerOS to see why", orange icon, no bar | orange `!` | orange `!` |
| `canceled` | "Import stopped", "Recipes already imported stay" | stop icon | stop icon |
| stale (no update for 30 min) | the count, and "Waiting for an update" instead of the status | — | — |

**What it may show is counts only**: the service's name and recipe numbers. No
recipe names, nothing about the meal-kit account, nothing about the household —
the Lock Screen is readable by anyone holding the phone.
`MealKitImportActivityAttributes` (`ios/Shared/`, compiled into the app and the
`DinnerOSLiveActivities` widget extension) is the whole of it: attributes
`serviceName`, `service`, `householdID` (ours, never shown; used to route a
rotated token after a relaunch), `jobID`; content state `phase`, `done`,
`total`, `failed`. An encoded state is under 128 bytes; the push payload limit
is 4 KB.

**Starting.** When a member queues an import (Recipe Import screen, Menu
get-started card — both go through `MealKitImportStore.startImport`), the app
starts the activity **after** the run is queued, with `pushType: .token`,
unless the member has Live Activities off (`ActivityAuthorizationInfo`). With
them off, or when ActivityKit refuses, the import proceeds exactly as before
(`MealKitLiveActivities.StartDecision`). A second tap on a run already followed
updates it instead of starting another; an activity left from an older run of
the same household is ended. A run another member started never grows an
activity on this phone.

**Tokens.** Every token ActivityKit hands out — including each rotation, and
activities that survive a relaunch — goes to
`PUT .../meal-kit/{source}/imports/{jobId}/live-activity` (body only, never the
path). The server stores it on the job (`liveActivity` subdocument of
`meal_kit_jobs`, written only by `internal/liveactivity`), never logs or
returns it, and removes it when the job ends. It is refused (`404`) for a run
that already finished. Swiping the activity away sends a `DELETE`.

**Updating while the app is open.** The status poll the screens already run
(every 15 s while a run is in flight) updates the activity locally and ends it
when it sees the run finish. Local updates cost no push budget.

**Updating while the app is closed.** `liveactivity.Pusher` implements
`mealkit.ProgressObserver`, a hook the worker calls after each checkpoint and
when a run puts the job down, and the worker and `StopImports` call when a job
ends. It sends through the same APNs token-auth client as alerts:

| | `update` | `end` |
| --- | --- | --- |
| Headers | `apns-push-type: liveactivity`, `apns-topic: <bundle id>.push-type.liveactivity` | same |
| `apns-priority` | **5** | 10 |
| Payload `aps` | `timestamp`, `event`, `content-state`, `stale-date` (+30 min) | `timestamp`, `event`, `content-state`, `dismissal-date` |
| `apns-expiration` | the stale date | the dismissal date |
| Rate limit | yes (below) | never |

An ended activity stays on the Lock Screen for **4 hours when done** (Apple's
maximum — a three-hour import usually finishes while nobody is looking),
**1 hour when failed**, and not at all when canceled — the member stopped it on
purpose. The app uses the same values when it ends the activity itself.

**The push budget.** Apple budgets Live Activity pushes per activity and
throttles apps that send high-priority updates too often, without publishing
the numbers. So progress goes at priority 5 (not counted against the
high-priority budget; delivered when the system finds it convenient) and
`liveactivity.Limiter` spaces it out per job — and a household has at most one
job in flight, so per household:

- nothing new (same phase, same counts): no push;
- same phase, new count: at most one per **30 s**;
- a change of phase (importing ↔ waiting): at most one per **15 s**;
- `end`: always, once.

That is at most **4 updates in any minute**
(`TestTheLimiterNeverAllowsMoreThanAHandfulAMinute`) and about 2 in practice,
since a batch of ten recipes takes about thirty seconds. A 740-recipe import is
roughly 74 checkpoints plus a phase change per run — under a hundred
low-priority pushes over three hours, and one high-priority `end`. We do not
set `NSSupportsLiveActivitiesFrequentUpdates`. The last sent state and time are
stored on the job, so the limit holds across worker processes.

A token APNs rejects as gone (`410`, `BadDeviceToken`) is removed at once.
Without `APNS_*` configured the observer is nil: no pushes, and the activity is
only as live as the app's poll — ActivityKit ends an activity nobody updates
after eight hours anyway.

**What can be checked where.**

| | Simulator | Device |
| --- | --- | --- |
| The views, all states, light and dark | Yes (widget previews, or start one by queuing an import) | Yes |
| Start on queue; update/end from the poll with the app open | Yes | Yes |
| "Live Activities off" skips it | Yes (Settings → DinnerOS) | Yes |
| A push token arrives and reaches the API | Unreliable; do not trust a missing one | Yes |
| Server `update`/`end` pushes while closed | No reliable delivery; a local `.apns` drag-in does not exercise our server | Yes, on a TestFlight or Xcode build against an API with `APNS_*` set |
| Throttling, priority-5 delivery timing, the 4-hour linger | No | Yes, over a real import |

## Where the import is offered

Three places, one flow. `MealKitSignInFlow` is the only sign-in; nothing keeps a
second copy of it.

| Where | What it is | When it shows |
| --- | --- | --- |
| `MealKitImportOfferView` | The offer right after a household is created | Once, in `CreateHouseholdForm` |
| `GetStartedSection` (Menu tab) | The first-run surface at the top of the Menu, above Your Meals | Whenever the household's library is empty (#530) |
| `MealKitImportStatusView` (Household tab) | The full status: failures, Import Review, import again, unlink | Always, for a member with `recipes.import` |

The Menu surface is the one a new household actually meets, because onboarding
is a moment and the Menu is where they land afterwards. It offers importing
first, the catalog (`DiscoverRoute`) second, and typing a recipe
(`AddRecipeView`) third, and it starts the import itself rather than pointing at
the Household tab.

What it says is decided by `FirstRunRecipes.state(for:)`, which is pure and
tested in words rather than checked by eye:

| State | What the household sees |
| --- | --- |
| `hidden` | The library has recipes, hasn't loaded, is filtered, or this member dismissed it |
| `offer` | No account linked: **Import from HelloFresh**, with the credential explanation under it |
| `linked` | An account is connected but nothing has run: **Import Now** |
| `importing` | The run's own progress, and "you can close the app" — the job is on the server |
| `needsSignIn` | The stored session expired: **Sign In Again** |
| `stopped` | The run gave up, with its message: **Try Again** |
| `imported(count)` | A run finished and added recipes this list hasn't caught up with: **Show Them** |
| `foundNothing` | A run finished having found nothing: **Try Again**, plus the catalog |
| `unavailable` | No `recipes.import`, or no import on this server: the catalog and typing a recipe |

Three details are deliberate. A member **without `recipes.import` never reads the
status** (`FirstRunRecipes.readsImportStatus`), because that request is a
guaranteed `403`; they are offered the catalog instead, and told plainly that
importing is up to whoever manages the household. A run started by **another
member** shows here too, because the job belongs to the household, so a second
phone sees progress rather than an offer to start a second import. And a
**finished** run triggers one library read, so the surface gives way to the
recipes it just claimed.

Dismissal is stored by `FirstRunDismissalStorage` in `UserDefaults`, keyed by
user **and** household — one member closing it is not the household saying it,
and one phone may hold two households at different stages. It is a preference
about a screen, not household data, so it never reaches the server. Recipes beat
dismissal in both directions: a household with recipes never sees the surface,
and a dismissal is never undone by a later import.

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

The endpoints above were measured from a signed-in browser, and the app was
written against those measurements, but **this flow has not yet run end to end
from a device**. One attempt settles it. Watch, in order:

1. **The web login.** The sheet shows `www.hellofresh.com` under the title and
   the password manager offers to fill. After signing in the veil should say
   "Finishing up…" and then "Reading your HelloFresh order history…". If it
   stops at "We couldn't pick up your HelloFresh sign-in", the cookie name or
   domain has changed.
2. **The read itself**, with the Console filtered to `DinnerOS`:
   - `Meal-kit order history read: N recipes, M weeks` — N should be roughly
     what the household has ever received, and M should be more than the
     number of pages, since a page carries several weeks.
   - `Meal-kit order history could not be read: forbidden` means the token did
     not carry; `unreadable` means `/gw/api/plans` or `past-deliveries`
     answered in a shape this build does not know — that is the one thing in
     the flow still unverified.
3. **`POST .../imports` → `202`**, and within seconds the status screen shows
   "Importing your recipes" with `recipesFound` matching what the log said,
   and the bar moving every half-minute or so. A `400` means everything
   harvested was dropped by the server's allow-list, which would mean the id or
   URL shape changed. Still "Starting your import" after a minute means the web
   runner is not running: look for `meal-kit import runner started` in the API
   log.
4. **The batches** (`heroku logs`, `meal-kit import batch finished`), and
   after the first 50 the screen saying "next batch in about a minute". Run
   `heroku run /importmealkit` with `LOG_LEVEL=debug` to see the scheduled
   path too. Recipe pages already work offline (`importers/hellofresh`), so a
   failure here is a rate limit, a block, or a layout change, and the debug
   line and the job's failure list say which.
5. **Import again** on the same household and confirm the second run reports
   the recipes as `unchanged` rather than adding duplicates.

Also unconfirmed until someone outside the US tries it: `country`/`locale` are
hard-coded to `US`/`en-US`.
