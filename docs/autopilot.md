# Autopilot

Status: **V1 implemented on the API and in the iOS app (Phase 10).** A local, deterministic,
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
  only one full match; a second one is penalized. The limit also covers the
  **kind** of meal the rule placed: once an Italian night is filled with a
  pasta, another pasta is penalized too, even one the catalog labeled with a
  cuisine outside the rule's tree (`north american`). Catalogs label plenty of
  pasta that way, and such a meal would otherwise escape the limit however
  plainly it repeats the same dinner. A rule whose meal has no meal category
  ("Meatloaf à la Mom") caps on its own groups alone, as before.
- `timeBand`: `quick` or `medium` prefer meals at or under that band that day;
  `long` means "long cook OK": no weeknight limit that day, a small bonus for a
  long meal, and it doesn't count against the week's long-meal allowance.
- **Methods dominate.** When a rule sets methods, a meal must suit one of them
  (the recipe's method attributes, including overrides) to earn any rule
  credit, however well its proteins, cuisines, tags, or cook time match. On an
  `every_week` rule a meal that doesn't suit gets −1, so familiarity or recency
  can't carry a pot pie onto smoker night; on an `at_most_once` rule it gets 0.
  It stays soft: when no suitable meal is left, the day is still filled and the
  week says so (`rule_method_unmet`).
- Methods must be in the household's equipment. A request whose rule names a
  method missing from `equipment` is scored as if the rule had no methods.

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

The three cases the household actually has — a lot on, people over, away — are
one tap each in the app (`AutopilotWeekPreset`): **Busy** sets `busy`,
**Guests** sets the week's `servings` to the household's usual plus two, and
**Away** sets `skip`. Away is exclusive, since a week with no meals can't also
be busy or have guests. A skipped week plans nothing and says so rather than
reading as an empty or failed week: the Week tab replaces **Plan with
Autopilot** with "Skipping this week", disables it in the ⋯ menu, and its
empty state is "Skipping This Week — nothing planned, on purpose" with
**Change This Week**. Generating anyway returns an empty proposal
(`week_skipped`), which the app doesn't offer for review.

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
| Smoker | In order: (1) a `smoker` tag or utensil always counts. (2) A dish that isn't a smoker meal never counts, matched in the name's main part before "with", "over", or "in" (so a side doesn't count): pasta shapes and noodles, ramen, soup, stew, pot pie, casserole, bake, stir-fry, fried rice, skillet, tacos, taquitos, burritos, enchiladas, quesadillas, bowls, wraps, pitas, sandwiches, sliders, burgers, pizza, flatbread, meatballs, meatloaf, patties, sausage, dumplings, gyoza, wontons, bibimbap, donburi, katsu, schnitzel. The evidence names it ("Not a smoker dish: pot pie"). (3) A whole or large cut of chicken, pork, beef, or turkey: whole chicken, bone-in chicken, legs, quarters, drumsticks, wings, spatchcock; pork shoulder, butt, tenderloin, filet, loin, chops, steak, belly, and ribs; brisket, beef or short ribs, tri-tip, chuck roast; turkey breast or legs. The cut must not be ground, sliced, diced, cubed, chopped, strips, cutlets, shredded, pulled, minced, cooked, deli, sausage, a mix, patties, crumbles, or meatballs. (4) "smoked" in the name or tags together with chicken, pork, beef, or turkey (so smoked paprika and smoked salmon don't count). Fish isn't included: smoker rules are about large meat cuts. |
| Grill, air fryer, slow cooker, pressure cooker | keywords in the name, tags, or utensils. |
| Meal category | the kind of dinner a pairing matches on: the keyword that comes **last** in the name, then tags that point at exactly one category, then a Mexican cuisine ([Meal categories](#meal-categories)). |

Households override methods and meal categories per recipe ("good for smoker",
"this is a pasta dish": yes/no/auto), and
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
- **Weekday rules match one step further**, in the other direction only: a rule
  for `italian` also matches a recipe the catalog labeled just `southern
  european`, because it could be Italian. It does not match a recipe labeled
  `french`, which shares only the broader `european` region. Catalogs label
  plenty of Italian food with the region alone, and such a recipe would
  otherwise escape both the rule and its `at_most_once` limit. Likes,
  dislikes, and exclusions keep the stricter reading above.
- Variety counts a shared cuisine as a full repeat (−0.20) and a shared region
  as a partial one (−0.10), never both: two Italian dinners repeat fully,
  Italian and French partially, Italian and Thai not at all.
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

### Food types

Catalog tags mix real food characteristics ("One Pot", "Kid Friendly",
"Spicy") with the source's own bookkeeping ("SEO", "dinners"). Only the first
kind is offered as a choice, so `recommendations/tags.go` hides the rest:

1. **A denylist of internal tags**, matched on the canonical value:
   `seo`, `dinner`, `dinners`, `lunch`, `lunches`, `breakfast`, `breakfasts`,
   `grocery`, `sides`, `lto`, `deals-of-theweek`, `static-position`,
   `ineligible-reco`, `free-addon`, and `quick prep internal`. These name a
   meal slot, a catalog shelf, a merchandising slot, or a pricing rule — never
   what the food is like.
2. **Anything on more than 90% of the catalog** (`NearUniversalTagShare`). A
   tag that matches nearly every recipe filters nothing.

Hidden tags are dropped in `buildVocabulary`, before the 60-option cap, so
hiding one frees a slot for a real choice. That is the single place the menu's
Food Types chips, the Autopilot vocabulary, and the weekday-rule and taste
pickers all read, so they agree by construction.

**Hiding changes only what is offered.** Canonicalization, validation, stored
profiles, and tag matching are untouched: a household that already likes or
excludes a hidden tag keeps working, and `?tag=` still filters by it. Labels
keep today's spelling — the most common one the catalog uses.

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
- add-ons (sides, desserts) aren't dinner candidates. They reach a week as
  [pairings](#add-on-pairings), and an add-on entry shares its meal's day
  without taking it or counting as a meal

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
| `rule` | −1…1 | the day's rule: matched groups / set groups; −0.3 for no match on an every-week rule. When the rule sets methods and the meal suits none: −1 on an every-week rule, 0 on an at-most-once rule, whatever else matches | 0.45 |
| `timeFit` | −1…1 | under the day's soft limit: 0.5 + 0.5 × (1 − minutes/limit); over: −2 × overage/limit; long meal on a long-cook day +0.5 | 0.20 |
| `novelty` | −0.6…0.6 | new meal: favorites −0.6, balanced +0.1, adventurous +0.6; familiar: favorites +0.2, adventurous −0.1 | 0.15 |
| `servingsFit` | −1…0 | when no authored size feeds the day's servings: −shortfall/servings | 0.25 |
| `pantry` | 0…1 | ingredients running low it uses, /2 | 0.05 |
| `avoid` | −1…0 | a previous pending proposal's meal (regenerate) | 0.35 |
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
          − 0.20 per pair of meals sharing a cuisine (their own)
          − 0.10 per pair sharing only a cuisine region (Italian and Greek are both southern european); never on top of the cuisine penalty
          − 0.20 per pair sharing a meal category (two pastas, whatever their cuisine labels say)
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
| `rule` | Sunday smoker night · Pork · Long cook OK: the label only when the meal suits the rule's method, or fully matches a rule without methods. A partial match lists only what matches ("Mexican"); a meal that misses the method has no `rule` reason |
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
planned 4 of 5 meals."), `rule_method_unmet` ("No smoker-friendly recipe fits
Sunday; picked the best alternative.", or "Sunday's smoker-friendly recipes
didn't fit this week; picked the best alternative." when suitable ones lost to
the rest of the week), `already_planned`, and `cold_start`. Days that
couldn't be filled are listed as `unfilled` (`no_candidates`,
`no_quick_candidates`).

## Determinism and tuning

- The same inputs, attempt, and model version always give the same week. The
  catalog, ratings, and history are normalized and ordered internally, so their
  order doesn't matter (tested). Ties break with an FNV-1a hash of
  household ID, week, attempt, model version, and item ID.
- `modelVersion` (`baseline-2026.4`) is stored on every proposal and event.
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

## Add-on pairings

Households pair things with dinner: garlic bread with pasta, crackers with
soup. Autopilot learns those pairs and offers them with the meal, from two
sources — the household's **rules**, and what it **usually has** — and adds
them as an add-on plan entry or a grocery line.

```text
main meal ──▶ meal category (pasta) ──┬─▶ rules      → "Your rule: pasta → Garlic Bread"
                                      └─▶ history    → "You usually have Garlic Bread with pasta (95% of pasta weeks)"
                                                ↓
                             accept ──┬─▶ add-on recipe → plan entry, same day, origin autopilot
                                      └─▶ grocery item  → the week's grocery list
```

### Meal categories

Recipes don't carry a "kind of dish", so it is derived at read time from the
name, then tags, then cuisines. The list is closed and small, and lives in the
[vocabulary](#vocabulary) with a recipe count:

| Category | Label | Keywords (plurals match) |
| --- | --- | --- |
| `pasta` | Pasta | pasta, spaghetti, penne, rigatoni, linguine, fettuccine, cavatappi, macaroni, mac & cheese, lasagna, ravioli, tortellini, gnocchi, orzo, ziti, carbonara… |
| `soup` | Soup, stew & chili | soup, stew, chowder, bisque, gumbo, broth, pho, minestrone, pozole, chili con carne, chili verde, and `chili` as the last word |
| `salad` | Salad | salad |
| `tacos` | Tacos & Mexican | taco, burrito, enchilada, quesadilla, fajita, tostada, flauta, taquito, nacho, chimichanga, tamale; or a Mexican/Tex-Mex cuisine |
| `curry` | Curry | curry, curried, masala, korma, vindaloo, dal |
| `bowl` | Rice & grain bowls | bowl, fried rice, bibimbap, donburi, poke |
| `stir-fry` | Stir-fry & noodles | stir fry, lo mein, chow mein, ramen, yakisoba, udon, soba, pad thai, noodle, vermicelli |
| `sandwich` | Burgers & sandwiches | burger, sandwich, sando, wrap, pita, slider, panini, hoagie, gyro, banh mi |
| `pizza` | Pizza & flatbread | pizza, flatbread, calzone |

- **The last keyword wins**, because English dish names end with the dish:
  "Spicy Beef Taco Rigatoni" is pasta, "Chicken Noodle Soup" is soup, "Chicken
  & Greek Salad Pita Pockets" are sandwiches, and "Saucy Beef Burrito Bowls"
  are bowls.
- `chili` counts only as the name's last word, so "Turkey & Bean Chili" is a
  soup and "Sweet Chili Pork Bowls" is not.
- Without a keyword, tags decide when they point at exactly one category
  (`soup-salad` is ambiguous and ignored), then a Mexican or Tex-Mex cuisine.
- The heuristic gives at most one category. A household can override any of
  them per recipe (yes/no/auto), exactly like cooking methods, and overrides
  decide what history is counted under.
- Categories also feed **week variety**: they reach Autopilot as
  `Item.MealCategories`, and a week pays −0.20 per pair sharing one. That is
  what stops two pastas landing in the same week when their cuisine labels
  differ or are missing ([week objective](#week-objective-and-search)).

### Learned pairings

The unit is an **ISO week**: a week's main meals give their categories, and
the add-ons ordered or planned that week are its add-ons. Order weeks
(`recipes.orderWeeks`) and planned weeks (plan entries) are merged, each week
counted once, and the week being planned is left out.

For a category C and an add-on A:

```text
together      = weeks with C and A
confidence    = together / weeks with C
otherRate     = (weeks with A − together) / (weeks without C)
baseRate      = weeks with A / weeks with a main meal
lift          = confidence / baseRate
```

A pairing is suggested when **all** of these hold:

| Threshold | Value | Why |
| --- | --- | --- |
| `together` | ≥ 3 weeks | one or two weeks is a coincidence |
| `confidence` | ≥ 0.5 | it happens with most of those meals |
| weeks without C | ≥ 4 | something to compare against |
| `confidence − otherRate` | ≥ 0.2 | **the pairing is about C**, not a weekly habit |

The last row is the important one. Raw confidence is inflated for an add-on
the household orders nearly every week whatever it eats — imported meal-kit
add-ons collapse many weekly menu aliases into one recipe, so a single add-on
can carry an order week for most weeks in the history. In the owner's own
history, "Garlic Bread" has 203 unique order weeks out of 230 weeks with a
main meal (an 88% base rate), so its confidence is 0.88–1.00 for *every*
category and its lift never exceeds 1.13. Only pasta stands out, and only
because the comparison is against the weeks without pasta:

| Category | together | category weeks | confidence | rate without the category | qualifies |
| --- | --- | --- | --- | --- | --- |
| pasta | 186 | 195 | 0.95 | 0.49 | **yes** |
| tacos | 170 | 186 | 0.91 | 0.75 | no |
| bowl | 87 | 97 | 0.90 | 0.87 | no |
| soup | 70 | 80 | 0.88 | 0.89 | no |
| stir-fry | 77 | 87 | 0.89 | 0.88 | no |

So the household is offered garlic bread with pasta, and not with everything
else it eats. Grocery items are never learned (nothing records that crackers
were bought for a soup); they come from rules.

### Pairing rules

The profile's `pairings` section is a list of at most 20 rules, each with a
stable `id`:

```json
{
  "id": "66e5a1f2c3b4a5d6e7f80f01",
  "label": "Pasta night",
  "when": { "mealCategories": ["pasta"], "cuisines": [], "tags": [], "proteins": [] },
  "add": { "kind": "recipe", "recipeId": "66e5…", "recipeName": "Garlic Bread", "groceryItem": null },
  "frequency": "always"
}
```

- A meal matches when it matches **every** group the rule sets; within a group
  any value matches, and cuisines match a recipe's cuisines or their regions —
  the same rule as weekday rules. A rule needs at least one condition.
- `add` is exactly one of an add-on `recipeId` (an add-on of this household
  that has serving sizes; its name is stored for display) or a `groceryItem`
  (`{name, quantity?, unit?}`, at most 60 characters, an ingredient unit).
- `frequency: always` is included with a proposed meal unless the member takes
  it out; `suggest` is offered.
- Two rules can't repeat the same meals and item. Changes are tracked like
  every other section (who changed it, when, and an
  `autopilot.preferences_updated` diff of added and removed rules).
- `PUT .../profile` (onboarding) **keeps** the pairing rules when it doesn't
  send them, unlike the other sections: rules are made from suggestions, not
  in onboarding.

### Suggestions and what a member can do

Per planned main meal, at most 3 pairings: rules first (`always`, then
`suggest`, in rule order), then learned ones by confidence. A target appears
once, and a rule shows the learned confidence when history backs it.

Already in the week, so not offered:

- an add-on **planned on that meal's day** (the same add-on on another day
  still is: two pasta nights can both have garlic bread);
- a grocery item already on the week's list for a meal that is still planned,
  or one a planned recipe already uses as an ingredient;
- anything dismissed for that meal this week.

| Action | What happens |
| --- | --- |
| Accept | An add-on becomes a plan entry on the meal's day, with the smallest authored size that feeds the meal (`origin: autopilot`); a grocery item joins the week's list. Accepting again adds nothing (`alreadyAdded`). |
| Dismiss | Hidden for that meal this week. |
| Make it a rule | A learned pairing becomes a household rule for its meal category. The same add-on for a second category joins the rule it already has (`merged`). |

### Grocery items on the list

Accepted grocery items are kept per week (`autopilot_week_pairings`) and
join the grocery list through planning's extras hook, keyed like an
uncatalogued ingredient (`name:club crackers`), so the pantry, check-offs, and
the Walmart handoff treat them like any other line. Each carries its
provenance in `extras[]` ("Club crackers for Chicken Noodle Soup"), and an
item leaves the list when its meal is unplanned, without being deleted.

### In a proposal

Generating (and swapping) stores each slot's pairings on the proposal, so the
review screen can ask "Add Garlic Bread?" without another round trip.
`always` rules arrive `included: true`. Accepting adds the meals and their
pairings in one atomic change; `pairingIds` chooses exactly which to add, and
leaving the field out means the included ones. A pairing whose slot is
excluded is dropped, and one whose add-on is already planned that day or can
no longer be planned is reported in `pairingsSkipped`.

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
| `autopilot.recipe_override_updated` | a method or meal-category override changed (`recipeId`) | `{method` **or** `category, value: yes/no/auto, previous}` |
| `pairing.suggested` | a pairing was offered with a meal (`recipeId` is the meal) | `{key, kind, source, frequency, mealCategory?, ruleId?, confidence?, entryId?, proposalId?, slotId?, day?}` |
| `pairing.accepted` | a pairing was added to the week | the same, plus `{addedEntryId?, groceryItemId?}` |
| `pairing.dismissed` | a pairing was dismissed, or left out when accepting | the same, plus `{reason: dismissed/excluded}` |
| `pairing.rule_created` | a learned pairing became a rule | `{ruleId, key, kind, mealCategory, frequency, confidence?, merged?}` |
| `meal.customized` | a member swapped or doubled a planned meal's protein (`recipeId`, `week`) | `{entryId, changes: [{ingredientKey, from, to}]}` |

`meal.customized` is recorded but not yet learned from. It's a direct
preference signal for a later model: a household that always swaps pork for
chicken is saying something its taste profile doesn't, and repeated doubling
says the portions are too small.

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
| Recipe method and meal-category overrides | corrections to the attribute heuristics |
| Pairings suggested, accepted, dismissed, and made into rules | which add-ons a household really wants with which meals, and where the learned thresholds sit |
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

## iOS

| Flow | Where | What happens |
| --- | --- | --- |
| Onboarding | Offered once on the Week tab's first visit while the profile isn't configured; also **Set Up Autopilot** (**Run Setup Again** once set up) in the Week menu (⋯) and Preferences (`AutopilotOnboardingView`, `AutopilotOnboardingSteps`) | **Three** one-screen questions, opening straight on the first, with a progress bar and a "2 of 3" count, and every screen answered by tapping tiles rather than reading: **What do you like?** (a grid of cuisine tiles, each a real catalog photo under a scrim — tap to like, again for "no thanks", again to clear, or long-press to pick), **Anything to avoid?** (allergen and diet tiles, each with its own SF Symbol, plus one "Add an ingredient" field), and **How do your weeks look?** (seven night tiles — the nights picked are the dinners Autopilot plans, so meals per week follows the count). The grid offers the specific cuisines people recognize, using a region only when nothing under it has enough recipes, and every tile gets a distinct photo — a plain tinted tile when none is left (#334, #335). A symbol the app doesn't know falls back to a neutral glyph, and a test checks every symbol is real (#337). At most one subtitle line per screen; longer explanations sit behind an info button. **Skip** is in the nav bar on every step and keeps that section's server defaults; **Set Up Later** is on the first step. **Finish** saves every section with one `PUT` and offers **Plan My Week**, plus a quiet **Fine-tune Autopilot** row. Setup no longer asks for meals per week, servings, the cook-time mix, equipment, weekday rules, novelty, or pairings — they keep the API's defaults and live in Preferences (#330, #337). |
| Plan and review | Week → **Plan with Autopilot** row or ⋯ menu; **Autopilot suggested N meals → Review** (`WeekAutopilotSection`, `ProposalReviewView`) | Generates the shown week and opens the review: `messages` as notes, each slot's date, photo, name, `cookMinutes` with a Quick/Medium/Long badge, servings, and reasons joined with " · ", and `unfilled` days with their text. Each slot has an include checkmark and **Swap**; ⋯ has **Plan Again** and **Dismiss Suggestions**; **Add N Meals to Week** accepts with `excludeSlotIds`. The plan then shows the new entries with a sparkles badge, and skipped meals are explained. |
| This Week's Plans | Week → **This Week's Plans…** row or ⋯ menu (`WeekContextSheet`) | Opens on three tiles — **Busy**, **Guests**, **Away** — that set the whole week in one tap, under one short line saying what the week is now; the exact numbers stay below them (a strict time limit, servings and meals for the week, per-day skip, limit, and servings, and a note). The tiles read the week back, so Guests is on when the week or any day serves extra, and turning it off clears both. **Save** sends the whole context; **Clear This Week's Plans** deletes it. Afterward the Week tab offers **Regenerate** or **Plan with Autopilot**, unless the week is skipped. |
| Preferences | Household → **Autopilot Preferences**; Week → ⋯ → **Fine-tune Autopilot** (`AutopilotPreferencesView`, `AutopilotSectionEditor`) | Every section, in two groups: **From Setup** (taste, restrictions, schedule) and **Fine-tune Autopilot** (cook-time mix, equipment, weekday rules, novelty, pairings — what setup doesn't ask, kept at the API's defaults until changed). Each row carries a one-line description, a summary, and "Changed by *name* · *date*", and opens controls that save that section with `PATCH`. **Change History** lists profile, week context, and recipe override changes with who made them. |
| Recipe methods | A recipe → **Autopilot** (`RecipeAutopilotSection`) | Cook time and band, and **Good for Smoker** (and the household's other equipment): Automatic (Yes/No), Yes, or No, saved with `PUT .../override`. |

Everything that changes Autopilot is hidden without `plan.edit`. Swaps,
accepts, and dismissals send the proposal's `version`; when another member
changed it first, the review reloads and says so. A finalized week offers
**Reopen and Plan**. See the decision log (#170–#181) for the reasoning.

## Limitations of V1

- Weights are hand-set; nothing is learned yet.
- Attribute heuristics are keyword-based and English-only. Method overrides
  exist, but proteins, allergens, and diets can't be overridden per recipe yet.
  Allergen and diet data are only as good as recipe labels and ingredient
  names.
- Cuisine canonicalization is a hand-kept table. Unknown labels are kept as
  normalized, and a recipe's cuisine can't be more specific than its source's
  label.
- One weekday rule per day; one dinner per day. Add-ons reach a week only as
  pairings, and nothing suggests a main meal for an add-on.
- Meal categories are keyword-based and English-only, and the heuristic gives
  a recipe at most one category (overrides add more). A dish the keywords
  don't know ("Meatloaf à la Mom") has none, so only rules can pair with it.
- Learned pairings need add-on history: a household whose add-ons were never
  ordered or planned gets nothing until it makes rules. Grocery-item pairings
  are never learned, because buying crackers for a soup isn't recorded.
- Pairings are learned per meal category, not per recipe, and the week is the
  unit: an add-on ordered in the same week as a pasta dish counts, even if it
  was eaten on another night.
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
