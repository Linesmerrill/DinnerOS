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
  own deliveries endpoint and the recipe pages those deliveries name.
- It does not write to the recipe collections. Everything goes through
  `recipes.Service.Import`, the same pipeline a file import uses
  ([import-format.md](import-format.md)), so imported recipes get the same
  validation, alias splitting, ingredient creation, and review items.
- It does not store a password. Ever. See
  [The credential lifecycle](#the-credential-lifecycle).

## The shape

```text
iPhone                    API (web dyno)              MongoDB              Worker (Scheduler)
  │  PUT .../meal-kit/hellofresh/link                    │                       │
  │  {email, password} ────────▶ sign in once ───────────┼──▶ HelloFresh         │
  │                             seal tokens ────────────▶│ meal_kit_links        │
  │                             enqueue ────────────────▶│ meal_kit_jobs         │
  │  ◀──── 200 {link, job}       (app can close)         │                       │
  │                                                      │◀── claim (findAndModify)
  │                                                      │    order history      │
  │                                                      │    recipe pages (rate limited)
  │                                                      │    recipes.Service.Import
  │                                                      │◀── checkpoint after each batch
  │  ◀──── push: "Your recipes are ready" ───── notifications outbox ◀───────────┘
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

> **A note on the original wording.** This was first described as storing the
> credential "hashed". A hash is one-way: we could never replay it to fetch
> anything, so it cannot be what a background job needs. What is stored is the
> **session and refresh tokens**, *encrypted* (reversible, by us, with a key
> that is not in the database). The password is not stored in any form.

1. **Sign-in, once.** `PUT .../meal-kit/{source}/link` carries the email and
   password over TLS. The handler passes them straight to `Source.SignIn`,
   which exchanges them for tokens. The password lives in that one call stack
   and is never written to Mongo, a log, or a response.
2. **At rest: envelope encryption.** Each link gets a random 32-byte data key
   (DEK). The tokens are sealed with the DEK (AES-256-GCM); the DEK is sealed
   with a key-encryption key (KEK) derived from `RECIPE_IMPORT_ENCRYPTION_KEY`.
   What MongoDB holds is `{keyId, key, ciphertext}` — useless without the
   config var. Rotating the KEK means re-wrapping small DEKs, not
   re-encrypting every secret.
3. **Startup.** With `MEAL_KIT_IMPORT_ENABLED=true` and no
   `RECIPE_IMPORT_ENCRYPTION_KEY`, the API and the worker refuse to start and
   say which variable is missing. There is no path that stores a token in the
   clear.
4. **Use.** Only the worker decrypts, and only into memory. `mealkit.Tokens`
   implements `slog.LogValuer` and prints as `redacted`, so a token handed to a
   logger by accident still does not appear.
5. **Refresh.** A session that is spent (or rejected mid-run) is refreshed once
   and the new one re-sealed and stored.
6. **Expiry.** A refresh that fails pauses the job at `paused_auth`, marks the
   link `needs_reauth`, and sends the needs-attention notification. Signing in
   again replaces the tokens and requeues the paused job where it stopped — it
   does not start over.
7. **Unlink.** `DELETE .../link` deletes the stored tokens first, then cancels
   every non-terminal job for that service. A worker mid-run finds its next
   conditional write rejected and stops without touching the library again.
8. **Account deletion.** A member's links go with their account
   (`PurgeUser`); a household's links and jobs go with the household
   (`PurgeHousehold`). Both are wired into `newAccountService`, and
   `TestIntegrationAccountDeletion` fails if a future collection is not.

Nothing about the credential comes back over the API: the link response
carries a generic `accountLabel` ("HelloFresh account"), never the email
address and never anything derived from a token.

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
| Scope | The account's deliveries endpoint and the recipe pages it names | Only what the household ordered. |

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
| Session expired, refresh failed | `paused_auth` | "Sign in to finish importing" | A **Sign In Again** button; progress is kept |
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

## Open question for the owner

The HelloFresh account endpoints in `internal/mealkit/hellofresh` — the token
exchange and the deliveries endpoint — are written against the shapes this
build expects, with `MEAL_KIT_HELLOFRESH_BASE_URL` to point them elsewhere.
They have **not** been verified against a real signed-in HelloFresh account,
because doing so needs the owner's own credentials. Confirm them (and the
recipe page's embedded data, which the offline importer in `importers/hellofresh`
already reads successfully) before turning `MEAL_KIT_IMPORT_ENABLED` on in
production. If they differ, only `hellofresh.go` changes: the queue, the
worker, the encryption, and the app do not care.
