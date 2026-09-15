# Autopilot

Status: **V1 implemented on the API (Phase 10).** A local, deterministic,
explainable baseline plans a week from the household's taste profile, week
context, and history, and the household reviews, swaps, and accepts it. The
context engine (weather, calendar), learned weights, and the private service
come later (Phases 11–12).

> Given a household, its behavioral history, preferences, context, a provider's
> available catalog, constraints, and optional business objectives, construct
> the best personalized **week** of meals.

Autopilot is intended to become a standalone, multi-tenant B2B API. DinnerOS will
be its first tenant.

```text
                ┌────────────────┐
DinnerOS ──────▶│                │◀────── Meal-kit company
                │  Autopilot API │◀────── Grocery retailer
                │                │◀────── Prepared-meal company
                └────────────────┘
```

## Code boundary

| Package | Role |
| --- | --- |
| `internal/autopilot` | The contract: `RecommendationProvider` and generic request/response types (catalog items, ratings, interactions, preferences, week context, fixed meals, objectives, explanations). Imports only the standard library; a test enforces that and that no recipe source is named. |
| `internal/autopilot/baseline` | The V1 implementation: hard filters, weighted signals, beam search, explanations. Hand-set weights. |
| `internal/recommendations` | The DinnerOS module: taste profile, week contexts, recipe attributes and overrides, proposals, HTTP handlers, and the adapter that turns recipes, ratings, plans, events, and the pantry into provider input. |

```go
type RecommendationProvider interface {
    RankMeals(ctx context.Context, req RankRequest) (RankResult, error)
    GenerateWeek(ctx context.Context, req WeekRequest) (WeekResult, error)
}
```

- DinnerOS depends only on this interface. `cmd/server` wires
  `baseline.New(baseline.Options{})`; a remote Autopilot client can replace it
  (Phase 12, `RECOMMENDATION_PROVIDER=autopilot`).
- This public repository holds the interface, the contracts, and a simple
  baseline. Competitive or tuned algorithms are never committed here, and the
  baseline's weights are defaults, not the final algorithm.
- The provider never sees DinnerOS or its recipe sources. It sees an opaque
  household ID, catalog items with generic attributes, member ratings, and
  interactions (`ordered`, `planned`, `cooked`, `skipped`).

## Pipeline

```text
Load (adapter)        → profile, week context, main-meal catalog, overrides, ratings, 26 weeks of plans,
                        52 weeks of cooked/skipped events, low pantry items, this week's plan
        ↓
Attributes (adapter)  → per recipe: cook minutes, time band, proteins, allergens, diets, spicy, methods
        ↓
Hard constraints      → remove candidates (nothing reintroduces them)
        ↓
Slots                 → which open days get a meal, with each day's caps, servings, and rule
        ↓
Meal scoring          → weighted, explainable signals per (meal, day)
        ↓
Week optimization     → beam search + local improvement over the week objective
        ↓
Explanation           → up to 3 reasons per pick, week messages, unfilled days
        ↓
Proposal (adapter)    → stored; the household swaps, accepts, or rejects
```

This is **not** "send every recipe to an LLM and ask what sounds good." No LLM
is used.

## Inputs

### Taste profile

One document per household, edited as seven sections. Each section records who
last changed it and when, and every change is recorded as an
`autopilot.preferences_updated` event with a diff summary.

| Section | Fields | Layer |
| --- | --- | --- |
| `taste` | liked and disliked cuisines, tags (food types), proteins | soft |
| `restrictions` | diets, allergens, excluded ingredients, cuisines, proteins, tags, no spicy | **hard** |
| `schedule` | plan days, weeknights, meals per week, default servings (null: household's), weeknight time limit | slots; soft limit |
| `cookTime` | quick and medium band limits, max long meals per week, min quick meals, avoid back-to-back long meals | soft (week objective) |
| `novelty` | `favorites`, `balanced`, `adventurous` | soft |
| `equipment` | `smoker`, `grill`, `air-fryer`, `slow-cooker`, `pressure-cooker` | enables method rules |
| `weekdayRules` | one per day: label, cuisines, tags, proteins, methods, time band, frequency | soft |

Defaults: plan Monday–Friday, weeknights Monday–Thursday, 4 meals, household
servings, no weeknight limit, bands 20/35 minutes, at most 2 long meals, avoid
back-to-back long meals, balanced novelty.

**Weekday rules** model day-specific habits such as "Taco Tuesday" or "Sunday
smoker night: chicken or pork, long cook OK":

- A meal fully matches when it matches every group the rule sets (proteins,
  methods, cuisines, tags); within a group any value matches. Partial matches
  earn partial credit.
- `every_week`: the rule's day strongly prefers matching meals, and meals that
  don't match at all get a small penalty that day.
- `at_most_once`: the rule's day prefers matching meals, but the week allows
  only one full match; a second one is penalized.
- `timeBand`: `quick` or `medium` prefer meals at or under that band that day;
  `long` means "long cook OK": no weeknight limit that day, a small bonus for a
  long meal, and it doesn't count against the week's long-meal allowance.
- Methods must be in the household's equipment.

### Week context

Per ISO week, all optional: `skip` the week, `busy`, `maxMinutes` (hard cap),
`servings`, `mealsPerWeek`, per-day overrides (`skip`, `maxMinutes`,
`servings`), and a free-text `note` that V1 stores but doesn't interpret.
Guests are a servings override for the week or a day, or more meals.

A busy week is about weeknights. `busy` is soft and applies only to the
profile's `weeknights`: the quick band becomes their soft limit, they allow no
long meals, and at least half of their meals should be quick. Other days keep
their usual cook-time handling, and a day whose rule has `timeBand: long`
keeps its long cook, even on a weeknight. Only `maxMinutes` or a day's
`maxMinutes` caps those days.

### Recipe attributes

Recipes don't carry these fields, so the adapter derives them from names, tags,
utensils, allergens, and ingredient lines with small keyword rules
(`recommendations/attributes.go`). `GET .../autopilot/recipes/{id}/attributes`
shows the result and its evidence.

| Attribute | Rule |
| --- | --- |
| Cook minutes | `recipes.CookMinutes` = max(prepMinutes, totalMinutes), ignoring missing values; 0 is unknown. Sources report the two fields inconsistently (totals below prep, prep only), so neither is used alone. |
| Time band | quick ≤ `quickMaxMinutes` (20), medium ≤ `mediumMaxMinutes` (35), otherwise long. Unknown time is medium, and never passes a hard cap. |
| Proteins | keyword phrases per protein (`chicken`, `pork`, `sausage`…), ignoring non-protein uses (`chicken stock`, `fish sauce`, `egg noodles`, `green beans`). |
| Allergens | recipe allergen labels mapped to codes, plus ingredient keywords (`parmesan` → milk, `soy sauce` → soy and wheat, `oyster sauce` → shellfish). Plant milks and nut butters are handled. |
| Diets | evidence of a violation (meat or stock, seafood, dairy, eggs, honey, wheat, barley) always rules a diet out. Without evidence, a recipe needs ingredient data, or a tag that asserts the diet. |
| Spicy | a `spicy` tag or name, or a heat ingredient (sriracha, jalapeño, chili flakes…); chili powder and paprika don't count. |
| Smoker | a whole or large cut of chicken, pork, beef, or turkey (whole chicken, bone-in thighs, legs, wings, pork shoulder/butt/tenderloin/loin/chops/belly/ribs, brisket, short ribs, tri-tip, turkey breast) that isn't ground, sliced, diced, cubed, shredded, or cooked; or a `smoker` tag; or "smoked" in the name or tags together with one of those proteins (so smoked paprika and smoked salmon don't count). |
| Grill, air fryer, slow cooker, pressure cooker | keywords in the name, tags, or utensils. |

Households override methods per recipe ("good for smoker": yes/no/auto), and
overrides always win. Hard-constraint heuristics are conservative: when unsure,
a recipe contains the allergen and doesn't satisfy the diet.

### Cuisines

Sources label cuisines inconsistently ("North America" next to "North
American", "East Asia" next to "East Asian"), so Autopilot reads and stores
every cuisine in one canonical form (`recommendations/canonical.go`):
vocabulary counts, recipe attributes, likes and dislikes, excluded cuisines,
and weekday rules.

1. Trim, lowercase, and single-space the label.
2. A known value or alias maps to its canonical value: `latin` → `latin
   american`, `middle east` → `middle eastern`, `american` → `north
   american`, `southwest` → `southwestern`, `tex mex` → `tex-mex`.
3. Otherwise a trailing region noun becomes its adjective (`southern europe` →
   `southern european`, `central africa` → `central african`), and step 2 is
   tried again.
4. Anything else is kept as normalized.

Canonical cuisines form a small hierarchy, so choices keep their specificity:

| Value | Parent | Children in the table |
| --- | --- | --- |
| `asian` | | `east asian`, `southeast asian`, `south asian` |
| `east asian` | `asian` | `chinese`, `japanese`, `korean` |
| `southeast asian` | `asian` | `thai`, `vietnamese`, `filipino`, `indonesian` |
| `south asian` | `asian` | `indian` |
| `european` | | `southern european`, `western european`, `eastern european`, `northern european` |
| `southern european` | `european` | `italian`, `greek`, `spanish`, `portuguese` |
| `western european` | `european` | `french`, `german`, `british`, `irish` |
| `eastern european` | `european` | `hungarian`, `polish`, `russian` |
| `north american` | | `southern`, `southwestern`, `tex-mex`, `cajun` |
| `latin american` | | `mexican`, `caribbean` (`cuban`, `jamaican`), `central american`, `south american` (`brazilian`, `peruvian`) |
| `middle eastern` | | `lebanese`, `turkish`, `persian` |
| `african` | | `north african` (`moroccan`), `west african`, `east african` (`ethiopian`) |
| `pacific islander` | | `hawaiian` |
| `mediterranean`, `fusion` | | |

- A recipe carries its canonical cuisines and their regions (`cuisineRegions`
  in the attributes). Likes, dislikes, exclusions, and weekday rules match
  either, so liking `european` or `southern european` covers Italian recipes,
  and excluding `asian` excludes Japanese ones. Liking `italian` doesn't match
  a recipe labeled only `southern european`: a label can't be more specific
  than its source.
- Variety compares only a recipe's own cuisines: two Italian dinners repeat,
  but Italian and French don't.
- `asian` is the parent of `east asian`, not a synonym: catalogs use it for
  East and Southeast Asian dishes alike. `american` means `north american`.
  `mediterranean` spans regions, so it has no parent.
- Vocabulary counts roll up: a region's `recipeCount` is how many recipes a
  like of it matches. Known values have title-case labels.
- Profiles saved before canonicalization are read canonically, and writes store
  canonical values. If merging spellings makes a stored value both liked and
  disliked or excluded, the restrictive choice wins when read.
- Tags only merge case, spacing, and a few spellings missing a space
  (`familyfriendly` → `family friendly`). The rest are distinct labels, not
  synonyms.

### History

| Signal | DinnerOS source | Generic input |
| --- | --- | --- |
| Ratings and feedback tags | `recipe_ratings` (every member) | `Rating{itemId, memberId, score, tags}` |
| Order history | `recipes.orderWeeks` | `ordered` interactions |
| Planned meals, weekday affinity | `weekly_plans` entries, previous 25 weeks | `planned` interactions with a day |
| Cooked and skipped | `recipe.cooked` / `recipe.skipped` events, 52 weeks | `cooked` / `skipped` interactions |
| This week's plan | `weekly_plans` for the week | fixed meals (occupy days, count toward meals and variety) |
| Pantry running low | pantry items with status `low` | `context.pantryLow` |

## Hard constraints

Applied before scoring. Business objectives and weights can never reintroduce a
removed meal (tested by giving a forbidden meal every advantage).

- no authored serving sizes (it can't be planned)
- any member tagged it `never-again`
- contains a household allergen
- doesn't satisfy every household diet
- excluded cuisine (or a region its cuisine belongs to), protein, or tag; an
  excluded ingredient word (`mushroom`
  excludes "cremini mushrooms"); spicy when "no spicy" is set
- over the week's or day's `maxMinutes`, or unknown cook time under a cap
- already in this week's plan (a recipe appears at most once a week)
- add-ons (sides, desserts) aren't dinner candidates

## Meal score

`score = Σ weight × signal` for a meal on a day. Every pick returns its
signals.

| Signal | Range | Definition | Weight |
| --- | --- | --- | --- |
| `rating` | −1…1 | (mean household score − 3) / 2 | 0.30 |
| `feedback` | −1…1 | make-again +0.6, kid favorite +0.4, great leftovers +0.2, kids disliked −0.4, too spicy −0.3, too bland −0.2, too much work −0.3 (−0.6 on weeknights) | 0.20 |
| `familiarity` | 0…1 | log₂(1 + ordered + planned + cooked) / log₂ 9 | 0.10 |
| `conversion` | −1…1 | 2 × (cooked + 1) / (cooked + skipped + 2) − 1 | 0.10 |
| `recency` | −1…0.3 | nearest week had: ≤1 → −1, 2 → −0.6, 3 → −0.4, 4 → −0.25, ≤8 → −0.1; ≥10 weeks for a familiar, well-rated meal → +0.3 | 0.25 |
| `weekdayAffinity` | 0…1 | share of the meal's planned days that were this weekday (needs 2) | 0.10 |
| `taste` | −1…1 | liked cuisine +0.4, tag +0.4, protein +0.3; each disliked −0.5 | 0.25 × (1 + (1 − confidence)) |
| `rule` | −0.3…1 | the day's rule: matched groups / set groups; −0.3 for no match on an every-week rule | 0.45 |
| `timeFit` | −1…1 | under the day's soft limit: 0.5 + 0.5 × (1 − minutes/limit); over: −2 × overage/limit; long meal on a long-cook day +0.5 | 0.20 |
| `novelty` | −0.6…0.6 | new meal: favorites −0.6, balanced +0.1, adventurous +0.6; familiar: favorites +0.2, adventurous −0.1 | 0.15 |
| `servingsFit` | −1…0 | when no authored size feeds the day's servings: −shortfall/servings | 0.25 |
| `pantry` | 0…1 | ingredients running low it uses, /2 | 0.05 |
| `avoid` | −1…0 | a previous pending proposal's meal (regenerate) | 0.30 |
| `objective` | ±0.2 | tenant business boost (DinnerOS sends none) | 1 |

The day's soft limit is, in order: none on a long-cook rule day, the quick band
on a busy week's weeknights, the day's rule band, or the weeknight limit on
weeknights. Servings are the smallest authored
size that feeds the day's servings (day override, week override, profile, then
household default).

**Cold start.** `confidence` = min(1, (ratings + interactions) / 40). Below 0.3
the proposal is marked `coldStart` with a message. History signals are zero, so
the taste weight, doubled at zero confidence, and catalog attributes (cook
time, rules, variety) decide, and ties break with the seed rather than always
the same meals.

## Week objective and search

```text
objective = Σ meal scores
          − 0.20 per pair of meals sharing a cuisine (their own, not regions)
          − 0.15 per pair sharing a protein
          − 0.35 per long meal beyond maxLongPerWeek, and per long meal on a busy week's weeknight (long-cook rule days exempt)
          − 0.25 per pair of long meals on adjacent days (when avoidConsecutiveLong)
          − 0.35 per quick meal the week can no longer fit to reach minQuickPerWeek
          − 0.35 per quick meal a busy week can no longer fit to make half its weeknight meals quick
          − 0.40 per extra full match of an at-most-once rule
          − 0.20 per new meal beyond the novelty budget (favorites 20%, balanced 50%, adventurous 100%)
          − 1.00 per open day left without a meal
```

1. **Days.** Requested meals = context `mealsPerWeek` or the profile's.
   Meals already in the plan count. Open days are plan days minus skipped days
   and days that already have an entry. When there are more open days than
   meals, days with rules come first (every-week before at-most-once), then
   the weekdays the household plans most, then week order.
2. **Beam search** over days, most constrained day first (fewest candidates).
   Each partial week tries its 40 best candidates for the day; the beam keeps
   16 weeks, first the best week for each distinct cook-time and rule pattern,
   then the best of the rest, so interchangeable meals can't crowd out
   different arrangements.
3. **Local improvement.** Replace one day's meal with an unused candidate, or
   swap two days' meals, whenever that raises the objective, until nothing
   does. This recovers arrangements such as alternating long cooks.
4. The proposal returns the objective's breakdown (`meals`, `variety`,
   `cookTime`, `rules`, `novelty`, `total`), and each pick's week penalties as
   signals (`variety`, `cookTimeMix`, `ruleFrequency`, `noveltyBudget`) when
   non-zero.

**Swap** uses `RankMeals` for the slot's day with the plan's entries and the
other proposed meals as fixed, so the next best alternative honors the same
constraints, variety, cook-time mix, and rule frequency. It excludes the current
meal and every meal already swapped out of that slot (up to 20).

## Explanations

Each pick has up to three reasons (`{code, text}`): the day's rule and
time-cap reasons first, then the largest positive contributions. The app joins
them with " · ".

| Code | Example |
| --- | --- |
| `rule` | Sunday smoker night · Pork · Long cook OK |
| `busyWeek` | Quick for your busy week (15 min), on weeknights only |
| `dayLimit` | Ready in 12 min for Wednesday: a day's cap, or the week's cap on other days |
| `rating` | You rated this 5★ / Rated 4.5★ by your household |
| `feedback` | Marked make-again / A kid favorite / Great leftovers |
| `familiar` | A household regular (6 times) |
| `notRecently` | Haven't had it in 13 weeks |
| `conversion` | You usually cook it when it's planned |
| `weekday` | Often on Tuesdays |
| `taste` | You like Mexican |
| `weeknight` / `quick` | 25 min, easy for a weeknight / Ready in 18 min |
| `new` | Something new to try |
| `guests` | Serves 6 for your guests |
| `pantry` | Uses up the cilantro running low |
| `fit` | fallback: Ready in 30 min |

Week messages explain shortfalls gracefully: `week_skipped`, `empty_catalog`,
`week_full`, `not_enough_candidates` ("Only 3 quick recipes (≤20 min) match;
planned 3 of 5 nights."), `not_enough_days` ("Only 4 days are open this week;
planned 4 of 5 meals."), `already_planned`, and `cold_start`. Days that
couldn't be filled are listed as `unfilled` (`no_candidates`,
`no_quick_candidates`).

## Determinism and tuning

- The same inputs, attempt, and model version always give the same week. The
  catalog, ratings, and history are normalized and ordered internally, so their
  order doesn't matter (tested). Ties break with an FNV-1a hash of
  household ID, week, attempt, model version, and item ID.
- `modelVersion` (`baseline-2026.1`) is stored on every proposal and event.
  `inputsHash` fingerprints the provider request, so identical inputs can be
  recognized.
- **Tuning:** change `baseline.DefaultWeights` (or pass `Options.Weights`) and
  bump `ModelVersion`. The baseline tests pin behavior (hard constraints,
  rules, cook-time mix, busy weeks, determinism) rather than exact scores, so
  small weight changes only need the few signal-value tests updated.
  `BenchmarkGenerateWeek` checks speed.
- Learned or proprietary weights belong behind the same interface in a private
  implementation, calibrated from the events below.

## Proposals, swap, accept

Proposals are separate from plans: suggestions don't appear in the grocery
list or in other members' plans until they're accepted, and `recipe.planned`
stays a fact about real plans.

```text
                generate (again: previous pending → week.rejected "regenerated")
                   │
                   ▼
   ┌──────────── proposed ──── swap (version+1, meal.swapped)
   │               │
   │ reject        │ accept {version, excludeSlotIds}
   ▼               ▼
rejected        accepted ──▶ draft plan gains entries (origin autopilot, proposalId)
```

- One proposal per household and week (`autopilot_proposals`); generating
  replaces it with a new ID. A finalized plan can't be generated, swapped, or
  accepted (`409 plan_finalized`); reopen the week first.
- Generating keeps what's already planned: days with entries aren't planned
  and those meals count toward the week.
- Swap, accept, and reject send the proposal's `version`; a stale version is
  `409 proposal_changed`.
- Accept adds every non-excluded meal to the draft plan in one atomic change.
  A meal whose day now has an entry (`dayTaken`) or whose recipe is already in
  the week (`alreadyPlanned`) is skipped and reported, never replaced. The
  proposal is claimed before the plan changes and reopened if the change fails,
  so two members can't both add it.
- Accepted entries are ordinary plan entries: members edit or remove them as
  usual, and generating again treats them like manual entries.

## Events and learning signals

Everything is recorded in `events` ([database.md](database.md#behavior)).
Autopilot types are server-observed; clients can't send them.

| Type | When | Payload |
| --- | --- | --- |
| `week.generated` | a proposal is generated | `{proposalId, modelVersion, attempt, requested, planned, unfilled, candidates, coldStart?, replacedProposalId?}` |
| `meal.swapped` | a slot's meal is swapped (`recipeId` = new meal) | `{proposalId, slotId, day, date, previousRecipeId, modelVersion, swapNumber}` |
| `week.accepted` | a proposal is accepted | `{proposalId, modelVersion, planned, added, excluded, skipped, swaps}` |
| `meal.rejected` | a meal was left out when accepting (`recipeId`) | `{proposalId, slotId, day, date, modelVersion}` |
| `week.rejected` | dismissed, or replaced by generating again | `{proposalId, modelVersion, planned, swaps, reason}` |
| `autopilot.preferences_updated` | a profile section changed | `{sections, changes: [{field, added?, removed?, from?, to?}]}` |
| `autopilot.week_context_updated` | a week context changed or was cleared (`week`) | `{changes?, cleared?}` |
| `autopilot.recipe_override_updated` | a method override changed (`recipeId`) | `{method, value: yes/no/auto, previous}` |

Plan entries carry `origin` and `proposalId`. `recipe.planned` includes
`origin` and `proposalId`, and `recipe.unplanned` includes `origin`, so
`recipe.cooked` and `recipe.skipped` (which carry `entryId`) join back to the
Autopilot meal and proposal that produced them.

Signals available for future learning (V1 stays deterministic and doesn't
learn from them):

| Signal | Use |
| --- | --- |
| Swaps: meal out, meal in, day, swap number | per-feature preference pairs (what the household prefers over the pick, on which day) |
| Accepted vs. excluded meals, rejected weeks | pick-level and week-level acceptance labels |
| Cooked / skipped for autopilot entries | conversion of accepted suggestions |
| Removing an accepted entry (`recipe.unplanned` with origin autopilot) | late rejection |
| Preference diffs with timestamps | which stated preferences change and when |
| Recipe method overrides | corrections to the attribute heuristics |
| Signals and model version on every stored proposal | the features behind each decision, for offline calibration |

## Success metrics

Computed from `events`:

- **Week acceptance rate** (primary): `week.accepted` with `swaps ≤ 1` over
  `week.generated`, per `proposalId`.
- **Meal swap rate:** `meal.swapped` per planned meal in `week.generated`.
- **Recommendation conversion:** `recipe.cooked` whose `entryId` belongs to an
  autopilot entry, over autopilot entries added.
- Average rating of accepted meals, repeat satisfaction, novelty acceptance
  (new meals accepted), and skip rate of autopilot entries.

## Performance

Generation reads the whole main-meal catalog (at most 2000 recipes, without
steps or nutrition), the household's ratings, 26 weeks of plans, 52 weeks of
cooked and skipped events, and low pantry items, with nothing cached. Measured
locally: about 65 ms end to end for 500 recipes through MongoDB and HTTP. The
baseline alone plans a 500-recipe, 5000-interaction week in a few milliseconds
(`BenchmarkGenerateWeek`).

## Limitations of V1

- Weights are hand-set; nothing is learned yet.
- Attribute heuristics are keyword-based and English-only. Method overrides
  exist, but proteins, allergens, and diets can't be overridden per recipe yet.
  Allergen and diet data are only as good as recipe labels and ingredient
  names.
- Cuisine canonicalization is a hand-kept table. Unknown labels are kept as
  normalized, and a recipe's cuisine can't be more specific than its source's
  label.
- One weekday rule per day; one dinner per day; add-ons are never planned.
- The week note is stored, not interpreted. There's no weather, season, or
  calendar context yet.
- Only the latest proposal per week is stored; earlier ones survive only as
  events.
- Equipment doesn't filter recipes that need equipment the household lacks.

## Context engine (later)

Context is a generic, optional map of typed signals rather than weather-specific
code:

```text
weekday · holiday · season · temperature · precipitation · region · delivery date · time available · calendar busyness
```

Feature calculators may use context when present and must degrade gracefully when
it is absent. Examples: a cold, rainy Wednesday raises comfort-food affinity; a
busy Wednesday evening favors meals under 20 minutes (V1 covers busy weeks and
per-day time caps through the week context).

## Multi-tenant model (service)

`Tenant → Customer (opaque external ID) → Household`, together with `Catalog`,
`Recipe`, `RecommendationRequest`, `RecommendationResult`, `ModelVersion`, and
`Experiment`.

- Partners send opaque IDs (`"customerId": "customer_abc123"`), not PII.
- Every document and query is scoped by `tenantId`, and cross-tenant access is
  tested explicitly.

Conceptual endpoints:

```text
POST /v1/catalog
POST /v1/events
POST /v1/rank
POST /v1/autopilot/week
POST /v1/feedback
```

## LLM usage

LLMs are backend-only and optional (Autopilot must work without them; V1 uses
none). They can be used for recipe classification, semantic similarity,
ingredient interpretation, cold-start preferences, and explanation phrasing.
They are never used for authentication, authorization, constraint enforcement,
unit conversion, or grocery arithmetic.

## Signals available today

Everything is household-scoped and lives in two collections
([database.md](database.md#behavior)):

- `recipe_ratings`: each member's current opinion of a recipe.
- `events`: an append-only history. Every event has `householdId`, `userId`,
  `type`, `recipeId` (recipe events), `week` (ISO week, when it applies), a
  small typed `payload`, `occurredAt`, `recordedAt`, and `source`
  (`api` for server-observed, `client` for app-observed).

| Candidate feature | Signal | Recorded by | Status |
| --- | --- | --- | --- |
| Household rating | `recipe_ratings.score`; `recipe.rated` / `recipe.unrated` events with the previous score | API (ratings) | ✅ used |
| Would make again, kid appeal, dislikes | Rating tags: `make-again`, `never-again`, `kid-favorite`, `kids-disliked`, `too-spicy`, `too-bland`, `too-much-work`, `great-leftovers` | API (ratings) | ✅ used |
| Historical preference, recency, repetition | `recipes.orderWeeks` / `timesOrdered` / `lastOrderedWeek`; `import.completed` marks each refresh | API (import) | ✅ used |
| Planned meals, weekday affinity | plan entries; `recipe.planned` / `recipe.unplanned` with `entryId`, `day`, `date`, `servings`, `origin`, `proposalId` | API (planning) | ✅ used (from plans) |
| Conversion (planned → cooked), skip rate | `recipe.cooked` / `recipe.skipped` (`entryId` links to the plan entry; `reason` for skips) | App, via `POST /events` | ✅ used |
| Interest | `recipe.viewed` with `surface` | App | recorded, not used |
| Shopping behavior | `grocery.item_checked` | App | recorded, not used |
| Week generation, swaps, acceptance | `week.*`, `meal.*` | API (Autopilot) | ✅ recorded for metrics and learning |
| Preference changes | `autopilot.*` | API (Autopilot) | ✅ recorded |

- Server-observed types can't be sent by clients, so they can be weighted as
  facts. App events are as trustworthy as the signed-in member.
- Recording is best effort. A lost event never failed a user action, so
  features tolerate small gaps.
