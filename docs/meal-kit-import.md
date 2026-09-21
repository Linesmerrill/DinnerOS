# Meal-kit recipe import

A new household should not start empty. This is the onboarding path: the member
signs in on the meal kit's **own website** in a web view (HelloFresh first),
their order history is read **there, in their own session**, the app hands us
that list, and a backend worker imports **the recipes that account actually
ordered** into the household's library. A push notification says when it is
done. Skipping is a first-class choice — recipes can be added by hand later.

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
iPhone                                    API (web dyno)     MongoDB     Worker (Scheduler)
  │  WKWebView → hellofresh.com/login                   │                      │
  │  (their password, their site, their password manager)                      │
  │  ◀── apiV2Auth cookie (stays in the web view)       │                      │
  │  in-page, in their session:                         │                      │
  │    /gw/api/plans → subscription id                  │                      │
  │    /gw/my-deliveries/past-deliveries, week by week  │                      │
  │  ◀── recipe ids + public URLs + weeks               │                      │
  │  [web view wiped]                                   │                      │
  │  POST .../meal-kit/hellofresh/imports               │                      │
  │  {recipes:[…]} ──────────────────────── validate ──▶│ meal_kit_jobs        │
  │  ◀──── 202 {job}          (app can close)           │                      │
  │                                                     │◀── claim (findAndModify)
  │                                                     │    public recipe pages (rate limited)
  │                                                     │    recipes.Service.Import
  │                                                     │◀── checkpoint after each batch
  │  ◀──── push: "Your recipes are ready" ─── notifications outbox ◀───────────┘
  │  GET .../meal-kit/hellofresh  →  what was imported, what failed and why
```

Note what is **not** in that diagram: any arrow carrying a credential to the
API, and any box holding one.

## Job lifecycle

A job is one import run for one household. States:

| State | What it means | What the member sees |
| --- | --- | --- |
| `queued` | Waiting for a worker. `availableAt` is the backoff. | "Importing your recipes… starting shortly" |
| `running` | A worker holds the lease. | Progress: *n* of *m* recipes |
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
stops.

**Checkpoints.** The order history is stored on the job when it is queued. After every batch
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
rather than queueing a duplicate.

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
| Concurrency | 1 request at a time, per source, per run | No parallel hammering. A `Fetcher` is deliberately not safe for concurrent use. |
| Interval | ≥ 2.5 s between requests, plus up to 40% jitter | Slower than a person browsing; jitter keeps households out of lockstep. |
| Retries | 4 attempts, backoff doubling on top of the interval | Transient failures without a tight loop. |
| 429 | `Retry-After` honoured, capped at 60 s, then backoff | Do what we are told. A longer wait is left to the job's backoff rather than holding a dyno. |
| 5xx | Retried within the attempt budget | Their problem, not a reason to give up on the member. |
| 401 / 403 | **Stop the whole run immediately, dead-letter it** | Nothing the worker fetches needs a session, so being turned away is a refusal, not an expired credential. We do not retry around an access control or work out how to look like someone else. |
| Per-run cap | 40 recipe pages | A real cap, not a comment. The rest waits for the next scheduled run. |
| User-Agent | `DinnerOS/1.0 (+https://api.tlps.dev; personal meal-kit order history import)` | Honest: who we are, why, and where to complain. No browser impersonation. |
| Scope | Public recipe pages, and only the ones the household's own order history named | Only what the household ordered. |

The same politeness applies **in the app**, where the account reads happen:
`MealKitHarvestScript` makes one request at a time, 500 ms apart, at most 40
pages, and stops on an empty page or a walk that stops moving backwards. A
member watching a spinner sets the interval; the cap is what stops a paging
change becoming a crawl.

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
| Finished, some recipes unreadable | `succeeded` | "…*k* could not be imported" | The list of failures with reasons |
| The session ended while reading the history | no job is queued | none | The sheet says so, with **Try Again** — nothing was half-imported |
| HelloFresh changed its layout | `dead` (`parse`) | "Recipe import needs you" | "We could not read …. Nothing was changed in your recipes." |
| HelloFresh refused us (403) | `dead` (`blocked`) | "Recipe import needs you" | "…refused our requests… Try again later." |
| Network trouble | `queued`, retried | none until it gives up | "Importing your recipes…" |
| Five failed attempts | `dead` (`network`) | "Recipe import needs you" | The reason, and **Import Again** |
| A recipe the import pipeline rejected | `succeeded`, failure recorded | part of the finished one | The pipeline's own reason, e.g. "name is required" |
| Member stopped the import | `canceled` | none | Nothing in flight; imported recipes stay |
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
| `MEAL_KIT_RECIPES_PER_RUN` | `40` | Recipe pages one run fetches per job. Lower is more polite. |
| `MEAL_KIT_HELLOFRESH_BASE_URL` | HelloFresh | Overrides the origin recipe pages are read from; for testing against a stub. Must be https. |
| `LOG_LEVEL=debug` | `info` | Turns on the redacted response diagnostics above for one run. |

See [deployment.md](deployment.md#meal-kit-recipe-import) for the Heroku steps.

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
3. **`POST .../imports` → `202`**, and the status screen shows a queued run
   whose `recipesFound` matches what the log said. A `400` means everything
   harvested was dropped by the server's allow-list, which would mean the id or
   URL shape changed.
4. **The worker run** (`heroku run /importmealkit`, or locally), with
   `LOG_LEVEL=debug`. Recipe pages already work offline
   (`importers/hellofresh`), so a failure here is a rate limit, a block, or a
   layout change, and the debug line and the job's failure list say which.
5. **Import again** on the same household and confirm the second run reports
   the recipes as `unchanged` rather than adding duplicates.

Also unconfirmed until someone outside the US tries it: `country`/`locale` are
hard-coded to `US`/`en-US`.
