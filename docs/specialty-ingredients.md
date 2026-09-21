# Specialty ingredients

Status: API implemented (`api/internal/substitutes`, with changes in
`internal/grocery`, `internal/planning`, `internal/pantry`, and
`internal/recipes`) and iOS implemented ([iOS](#ios)). The
[household strategy](#the-household-strategy) is API-only so far; the iOS
contract notes below say what the app has to change.

Recipes imported from a meal-kit history name items a grocery store doesn't
sell under that name: "Tex-Mex Paste", "Southwest Spice Blend", "Chicken Stock
Concentrate". Cooking those recipes from a normal store needs, for each one:

- a **store alternative**: regular ingredients per amount ("1 tbsp Tex-Mex
  Paste = 2 tsp tomato paste + ½ tsp chili powder + ¼ tsp cumin + ½ tsp
  oil"), or
- a **house-made batch**: a small recipe that makes a jar ("Southwest spice
  blend, makes about 12 tbsp, keeps 180 days"), kept in the pantry. Cooking
  deducts from it, and the grocery list asks for its raw ingredients only when
  the batch is low, out, missing, or short for the week.

Example: the household chooses the batch for Southwest Spice Blend. This
week's list says "Ground Cumin 2 tbsp, to make Southwest Spice Blend (makes
about 12 tbsp)". A member makes the jar and taps "Made a batch", which starts
a 12 tbsp pantry cycle. Next week the blend shows as "In pantry (house-made)".
Tacos cooked with 1 packet deduct 1 tbsp. When the estimate crosses the low
threshold, the pantry marks it low, notifies the household, and the list asks
for the ingredients again.

## The household strategy

Choosing an option for 29 blends one at a time is a wall, so a household picks
one standing answer instead. `GET`/`PUT
.../specialty-ingredients/settings` holds it:

| Strategy | What it does |
| --- | --- |
| `similar` (default) | Prefer a **store alternative** wherever the curated set has one; fall back to a house-made batch only when there is no store option. "Buy something close from the store — quicker, tastes a little different." |
| `closest` | Prefer a **house-made batch** wherever there is one; fall back to a store alternative. "Make a jar you reuse across several meals — more work, closest to the original." |
| `ask` | Apply nothing. Every unchosen specialty ingredient stays on the list by its own name with suggestions — the behavior before this setting existed. |

The settings response carries `options[]` (`value`, `label`, `description`)
with exactly that wording, so the app shows the trade-off without hardcoding
copy.

**How it resolves.** For each specialty ingredient on a week's list:

1. An explicit household choice wins, including `as_is` ("leave the line
   alone"). A choice of an option that has since been deleted counts as no
   choice, so the strategy takes over rather than the line silently staying.
2. Otherwise the strategy picks a **curated** option: its preferred kind if
   the specialty has one, else the other kind. It never picks a household's
   own option — a household that wrote one chooses it deliberately.
3. Otherwise nothing applies and the line stays with its suggestions.

**Nothing is stored.** The strategy is resolved when the list is built, not
written to `specialty_choices`. Changing the setting changes future lists with
no migration, and there is no stored choice to un-pick (decision 440).

The list response reports which happened per ingredient: `choiceSource` is
`household`, `strategy`, or `none`, and `choice.source` matches it, with
`choice.strategy` naming the strategy and `chosenBy`/`chosenAt` null when
nobody chose (decision 442).

A strategy change records `specialty.strategy_updated` (`{strategy,
previous}`) server-side.

## Data model

| Record | Where | What it holds |
| --- | --- | --- |
| Specialty ingredient | `specialty_ingredients` (global) | `slug` (the API `id`), `key` (normalized name), name, aliases and their keys, category, `unitSizes` (estimated packet size per discrete unit), `defaultOptionId`, curated `options[]`, `seedVersion`, `contentHash`, `retired` |
| Option | curated: embedded in the specialty; household: `specialty_options` | type (`store_alternative`/`house_made_batch`), name, notes, `per` (store), `ingredients[]` (name, exact quantity, unit, optional category), `steps[]`, `yield`, `shelfLifeDays` (batch), `basedOnOptionId` |
| Choice | `specialty_choices` | one per household and specialty: `optionId` (curated, household, or `as_is`), who, when |
| Batch | `pantry_items` + `pantry_purchases` | the pantry item keyed by the specialty's `key`; each batch made is a purchase with `source: house_made` that starts a usage cycle |

- **The specialty flag.** An ingredient is a specialty ingredient when its
  catalog key or normalized name equals a curated name or alias. It's
  resolved at read time, like pantry items (decision 57), so an ingredient the
  catalog learns later matches without a backfill. Unknown specialty-looking
  names are simply not flagged.
- **Curated defaults and household overrides.** Curated options are global
  and read-only. `defaultOptionId` is suggested first; nothing changes a list
  until a member chooses, or applies every default with
  `POST .../specialty-ingredients/choices/defaults`. A household overrides a
  curated option by adding its own (usually a copy, `basedOnOptionId`) and
  choosing it.
- **`as_is`** keeps the item on grocery lists by its own name and stops the
  prompt (the household buys it somewhere, or doesn't want to be asked).
- Amounts are exact fractions in DinnerOS unit codes, like everywhere else.
  Conversions are exact or not at all (decision 98); a packet counted as
  `count` or `package` converts through the specialty's `unitSizes`. A unit
  may have one size per kind (Garlic Herb Butter: 1 count = 1 oz and 1 count
  = 2 tbsp), so a line in either weight or volume reaches a `count` option;
  weight and volume still never convert to each other directly (decision
  480).

## Curated seed

`api/internal/substitutes/seed/specialty-ingredients.json`, versioned in the
repo and embedded in the binary. Every entry is an original, general-knowledge
approximation written for DinnerOS: no meal-kit company's text, no branding,
generic option names. Packet sizes are estimates.

| Specialty ingredient | Options (default first) |
| --- | --- |
| Chicken, Beef, Veggie Stock Concentrate | store (concentrated bouillon base; packet→tsp conversion unverified) |
| Mushroom, Pork Ramen Stock Concentrate | store (bouillon base, seasoned where needed; same unverified conversion) |
| Sweet Soy Glaze | store (bottled sweet soy glaze), batch |
| Southwest Spice Blend | batch (4 tbsp, 180 days), store |
| Sweet Thai Chili Sauce | store (bottled sweet chili sauce) |
| Tex-Mex Paste | store (smoky chipotle base with tomato paste), batch |
| Ponzu Sauce | store (soy with citrus), store (bottled ponzu), batch |
| Cream Sauce Base | store (butter, flour, and milk, made in the pan — no batch by design) |
| Smoky Red Pepper Crema | store (sour cream with roasted peppers), batch |
| Fry Seasoning | batch (1:1 paprika and garlic powder), store |
| Szechuan Paste (alias Sichuan Paste) | store (chili garlic sauce with soy), batch |
| Tuscan Heat Spice | batch (Italian seasoning, garlic powder, cayenne), store |
| Mexican, Fajita Spice Blend | batch, store |
| Shawarma Spice Blend | store (bought shawarma blend) |
| Bulgogi Sauce, Umami Ginger Sauce | store, batch |
| Blackening Spice | store (bought blackening or Cajun blend) |
| Brown Sugar Bourbon Seasoning | batch, store |
| Sesame Dressing | store (bottled toasted sesame dressing), batch |
| Miso Sauce Concentrate, Cheese Roux Concentrate | store |
| Garlic-Ginger Scallion Paste | store (garlic and ginger paste with scallions), batch |
| Sweet and Smoky BBQ Seasoning, Tunisian, Cuban Spice Blend | batch, store |
| Garlic Herb Butter | store (garlic and herb butter, 2 tbsp per 1 oz packet); batch recipe pending |
| Beef Demi-Glace (alias Demi-Glace Concentrate), Chicken Demi-Glace | store (one 37.5 g concentrate sachet per packet; chicken size unverified) |

32 specialty ingredients, 20 with a batch. Plain store products (hoisin, BBQ,
gochujang, soy sauce, tomato paste, hot sauce, Italian seasoning) and produce
mixes (coleslaw mix) aren't in it; `TestEmbeddedSeed` enforces that, and that
no text names a meal-kit brand.

**The shopping audit.** A household on `similar` must have something to buy
for every entry, so `TestSeedStoreAlternatives` requires each one to carry a
store alternative **or** an explicit `note` saying why there isn't an honest
one. All 29 already had a store alternative when the rule was added, so seed
v2 changed two things rather than adding options:

- **A `note` field** (≤ 300 characters, shown by the app) on the six entries
  whose store route needs a caveat — Sweet Thai Chili Sauce, Ponzu Sauce,
  Szechuan Paste, Sesame Dressing, Miso Sauce Concentrate, and
  Garlic-Ginger Scallion Paste. These depend on an international-aisle or
  refrigerated product, which is a shopping caveat, not a missing
  alternative. Tex-Mex Paste carries one for the canned chipotle.
- **A better Tex-Mex paste**: its store alternative gained chipotle peppers
  in adobo, since the old version had chili powder and cumin but no smoky
  element.

The rule is a test rather than a one-off review, so a new curated entry can't
ship batch-only without saying so.

**Loading.** The API syncs the embedded seed on startup, after indexes
(`substitutes.EnsureSeed`). Each specialty carries a SHA-256 of its content, so
a sync writes only what changed: new ones are inserted, changed ones replaced
(keeping `createdAt`), and ones the seed dropped are marked `retired`, never
deleted, so a household's choice still resolves. Syncing twice writes nothing.
`cmd/seedspecialties` runs the same sync as a dry run (default) or `-apply`,
and prints which catalog ingredients each specialty flags, so a seed change
can be previewed against a copy of production data:

```sh
MONGODB_URI=... MONGODB_DATABASE=dinneros go run ./cmd/seedspecialties
```

Why startup and a command, not only a command: curated option IDs are part of
the API contract (choices reference them), so the database must match the
running binary. Heroku has no release phase configured (`heroku.yml`), and a
hash-guarded sync costs one small read per boot.

## Grocery lists

`planning.Service.GroceryList` builds the week's selections, asks
`substitutes.Service.GrocerySpecialties` which lines are specialty
ingredients (with the household's choices, option ingredients linked to the
catalog, and batch stock from the pantry), and runs the pure
`grocery.ApplySpecialties` before `Aggregate`. Ordinary lines aren't touched.

What counts as "the choice" below is the resolved one: the household's
explicit choice, or the option its
[strategy](#the-household-strategy) picked when there is none. A line the
strategy produced carries `via[].strategy` and reads "(your default)".

| Choice | What the list does |
| --- | --- |
| none (`ask`, or no curated option) | The item stays, `specialty: true`, with `specialtyDetail.suggestedOptions` (default first) |
| `as_is` | The item stays, `specialty: true`, `choiceType: as_is`, no suggestions |
| store alternative | The line becomes the option's ingredients, scaled by the line's amount converted to `per` (through `unitSizes` for packets), aggregated with everything else. Each carries `via` ("for Tex-Mex Paste in Smoky Pork Tacos"). A line without an amount, or in units that don't convert, lists the ingredients without amounts |
| house-made batch | The week's amounts are totaled in the yield's unit. If the batch item is `in_stock` and its estimate covers the total (or can't be compared), the lines stay as one `inPantry` item keyed `name:<key>`, `houseMade: true` ("In pantry (house-made)"). If it's low, out, missing, or short, the lines are replaced by the ingredients for enough batches to cover the shortfall (at least one), each with `via` ("to make Southwest Spice Blend (makes about 12 tbsp)") |

Every house-made specialty the week uses is also in the response's
`batches`: `status` `inPantry` or `make`, `reason` (`enough`, `inStock`,
`notEnough`, `low`, `out`, `missing`), `batches`, `needed`, `remaining`, and
`pantryItemId`. See [api.md](api.md#grocery-list) for the JSON.

A failure to load choices fails the list, like the pantry (decision 58).

## Cooking instructions

A grocery list that says "tomato paste" and a recipe step that still says
"Tex-Mex Paste" contradict each other in the one place it matters: the screen
someone reads with a pan in hand. `GET .../recipes/{recipeId}/instructions`
([api.md](api.md#cooking-instructions)) renders the steps with the household's
choice applied.

**Rendered at read time, never stored.** The instructions are built from the
recipe as imported plus the choices resolved at that moment, through the same
`GrocerySpecialties` the week's list uses, so the list and the steps can't
disagree. Changing a choice — or the strategy — changes every recipe's steps
on the next read, with no rewrite of stored recipes and nothing to migrate
(decision 512). The recipe's own wording is always still there as
`originalText`.

| Choice | What the step says |
| --- | --- |
| none | Unchanged. The step reads as the card wrote it, and the specialty ingredient is listed in `unchosenSpecialties` so the app can offer the choice |
| `as_is` | Unchanged, and not reported: the household buys the thing itself |
| store alternative, one ingredient | The name and the amount are swapped: "Stir in **2 tsp tomato paste**". The amount is the line's amount converted to the option's `per` (through `unitSizes` for packets) times the option's own amount |
| store alternative, several ingredients | The card's name stays, and the step gets a note: "Instead of 1 tbsp Tex-Mex Paste, use 2 tsp Tomato Paste, ½ tsp Chili Powder, and ¼ tsp Ground Cumin." Swapping one word can't say that the method changed |
| store alternative, amount that doesn't convert | Same as above: the note lists the ingredients without amounts rather than inventing one |
| house-made batch | The name stays, the amount becomes the jar's: "1 packet" becomes "1 tbsp", with a note saying it comes from the house-made batch. Without an exact conversion the recipe's own amount stays |

A mention is marked `spicy` when the ingredient — or the substitute that
replaced it — brings heat. Heat isn't a grocery category (chili flakes and
cinnamon are both `spices`), so it is a second classification of catalog names
next to `Categorize`: `ingredients.Spicy`, written as ordered phrase rules
with the exceptions first, so "Sweet Thai Chili Sauce" and "Bell Pepper" stay
mild.

## Batches and pantry cycles

`POST .../specialty-ingredients/{specialtyId}/batches` calls
`pantry.Service.RecordHouseMade`:

1. The option is `optionId` or the household's choice; it must be a batch.
2. The purchase is `source: house_made`, quantity = yield × `batches`, in the
   yield's unit. Apps can't send `house_made` to `POST .../pantry/purchases`.
3. The item is the pantry item with the specialty's `key` (added when missing,
   named "Southwest Spice Blend (house-made)", linked to the catalog). The
   purchase restocks it exactly like any purchase: the previous segment
   closes, a new cycle starts at the yield, and it's `in_stock`.
4. The item gets the specialty's first packet size as its `unitSize` (so
   "1 count" lines deduct) and `expiresOn` = the UTC date made plus the shelf
   life.

Cooking needs nothing new: the pantry's `recipe.cooked` listener matches a
recipe line to the batch item by catalog ID, key, or name, as for any item.
The specialty module is the pantry's optional `KeyResolver`, so a line naming
an alias ("Sichuan Paste") matches the "szechuan paste" batch, and a line
counted in another packet unit converts through the specialty's other unit
sizes. The learned rate, low threshold, and notifications then work unchanged.

## iOS contract notes

- **Settings (new).** `GET .../specialty-ingredients/settings` returns
  `strategy` and `options[]` (`value`, `label`, `description`). Show the three
  as a picker with the descriptions as subtitles — they are the trade-off in
  the owner's words — and save with
  `PUT .../settings {"strategy": "closest"}` (`pantry.edit`). Put it at the
  top of the setup screen: it's the decision that saves a member from the
  per-ingredient ones.
- **`choice` now covers the default, so read `choiceSource`.** A specialty
  ingredient nobody chose for still returns a `choice` when the strategy
  applies one. Branch on `choiceSource`: `household` is a member's decision
  (badge as today), `strategy` is the default (badge it as such, e.g. "Store
  Alternative · default", and keep the option pickable so a member can
  override), `none` is Not Set. **`choice.chosenBy` and `choice.chosenAt` are
  now null** on a strategy-sourced choice, so they must decode as optional —
  this is the one breaking change (decision 442).
- **`note`.** Each specialty ingredient may carry a short `note` (usually
  empty). Show it on the detail screen near the store alternative; it says
  things like which aisle a bottled product is in.
- **Setup screen.** `GET .../specialty-ingredients` lists what the household's
  recipes use, most used first. Show `name`, `recipeCount`, and the choice
  (`choice.type`, `choice.optionName`). Offer `options` (curated first; the
  one with `isDefault`), each with `summary` and, for batches, `ingredients`
  (`text`), `steps`, `yield.text`, and `shelfLifeDays`. Choose with
  `PUT .../{id}/choice {"optionId"}` (or `"as_is"`), clear with `DELETE`.
  "Use suggested for all" is `POST .../choices/defaults`. "Customize" posts a
  copy to `.../options` with `basedOnOptionId`, then chooses its `id`. Use
  `?all=true` for a browse-everything view. `pantry.edit` is needed to change
  anything; `household.view` to read.
- **Grocery list.** Keep rendering `categories` as before. When `via` isn't
  empty, show each `via[].text` under the item. When `specialty` is true and
  `specialtyDetail.choiceType` is null, show `specialtyDetail.text` with a
  "Choose" action that opens the setup screen for `specialtyDetail.id`
  (`suggestedOptions` is enough for a quick picker). `houseMade: true` items
  read "In pantry (house-made)". Show `batches` with `status: make` as a
  "Make" section using `text`, `recipes`, and the ingredients whose `via`
  matches `specialtyId`.
- **Made a batch.** From a `make` batch row or the setup screen, send
  `POST .../{specialtyId}/batches` with a new `clientPurchaseId` (and
  `batches` when more than one). Replace the pantry item with `item` and
  reload the grocery list. A `400` means the choice isn't a batch.
- Checking off a raw ingredient line still offers "Add to pantry?" as
  before; checking off a house-made item needs no purchase.

## iOS

| Flow | Where | What happens |
| --- | --- | --- |
| Setup | Pantry → ⋯ → **Specialty Ingredients…** (`SpecialtyIngredientsView` in a sheet, `SpecialtyStore`) | Most used first: name, "Used in N recipes", a choice badge (Not Set, Keep as Is, Store Alternative, House-Made Batch), the chosen option's name, and the batch in the pantry. **Use Suggested for All** asks to confirm, sends `choices/defaults`, and says how many got their suggestion and how many already had a choice. |
| Options | Setup → an ingredient (`SpecialtyDetailView`) | Every option with `summary`, notes, and ingredients, headed "Replaces 1 tbsp Tex-Mex Paste" for a store alternative. Batches add numbered steps, **Makes** (`yield.text`), and **Keeps** (`shelfLifeDays`). **Use This Option**, **Keep as Is**, and **Clear Choice**. |
| Customize | An option → **Customize…** (`SpecialtyOptionEditor`, `SpecialtyOptionDraft`) | Edits a copy: name, notes, "replaces" amount or yield and shelf life, ingredients, and steps. **Save & Use** posts it with `basedOnOptionId` and chooses it. Household options also show **Edit…** and **Delete…**. Amounts are parsed like pantry amounts ("1 1/2" → `"3/2"`). |
| Made a batch | Detail → **Made a Batch** (`SpecialtyBatchSheet`); grocery list → **Made It** | 1–10 batches with one `clientPurchaseId` per sheet (or per list batch until it's recorded). The returned `item` replaces the pantry item, and open grocery lists reload. |
| Cooking steps | A recipe → **Cooking Steps** (`CookingStepsSection`, `InstructionStepRow`, `RecipeInstructionsViews`) | The steps as rendered for the serving size on screen: every listed ingredient bold with its amount, spicy ones bold and red with a flame (never colour alone), substitution notes under the step, and **Your Substitutes** at the end with a **Choose** button for anything unchosen. A server without the endpoint, or a failed load, leaves the recipe's own steps on screen. |
| Grocery list | Week → Grocery List (`GroceryListView`, `GroceryListLayout`, `GrocerySpecialtyViews`) | Each `via[].text` under its item. An unchosen line shows `specialtyDetail.text` and **Choose**, a sheet with `suggestedOptions`, **Keep as Is**, and **See All Options**; a notice at the top opens setup. **Make This Week** lists `make` batches with their recipes, why they're needed, and **Made It**, with the ingredients bought only for that batch under it. **Already Made** lists `inPantry` batches. House-made lines read "In pantry (house-made)" and don't ask "Add to pantry?". Shared text includes the batches and `via` lines. |

Choosing from a grocery list closes the picker at once and saves in the
background, with progress on the line, then reloads the list; a failure shows
an alert with **Try Again**. Members without `pantry.edit` see every screen
read-only, and the list hides **Choose**, **Made It**, and the notice. A `403`
turns those actions off and reloads the household's role. The store resets on
sign-out and when the household changes. Decisions 139–144 in
[architecture.md](architecture.md) record the placement, grouping, and flows.

## Limitations

- **Approximations.** Blends and sauces are generic; flavor won't match a
  meal kit exactly. Packet sizes are estimates, so conversions from packet
  counts are estimates too.
- **Only curated specialties are flagged.** A household can't flag a new
  specialty ingredient yet, and unknown names aren't guessed.
- **The strategy is all-or-nothing per household.** There is one setting, not
  one per category, so a household that wants to buy its spice blends but make
  its sauces still chooses those ingredients individually. The per-ingredient
  choice is the escape hatch.
- **The strategy never picks a household's own option.** Only curated options
  are considered, so a household that wrote its own has to choose it.
- **Changing the strategy changes past weeks' lists too.** Lists are built on
  request, so reopening last week's list after a change shows the new
  resolution. Only an explicit choice pins an ingredient.
- **Instructions match names, they don't understand sentences.** Only the
  recipe's own ingredient names (with plural, singular, and alias forms) are
  found in step text. A step that calls something by a name the recipe doesn't
  list is left alone, and an amount inserted in front of a name that the
  sentence qualifies ("the cooked rice") can read a little oddly.
- **Store alternatives don't deduct** their ingredients from the pantry when
  cooked; only a batch (or an item with the specialty's own name) does.
- **One pantry unit size** per batch item. Other packet units convert only
  through the resolver when cooking, and a household-made option in an
  unusual unit may not compare with recipe amounts (the list then trusts the
  item's status).
- **Batch decisions use the estimate at request time** without running the
  low-stock check, so a batch that just crossed its threshold is judged by its
  remaining amount until the pantry or notifications are read.
- **`expiresOn` uses UTC dates**, not the household's time zone.
- **Yields** are the nominal jar size, not a measured volume.
