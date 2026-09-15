# Specialty ingredients

Status: API implemented (`api/internal/substitutes`, with changes in
`internal/grocery`, `internal/planning`, `internal/pantry`, and
`internal/recipes`). The iOS screens are a follow-up.

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
  `count` or `package` converts through the specialty's `unitSizes`.

## Curated seed

`api/internal/substitutes/seed/specialty-ingredients.json`, versioned in the
repo and embedded in the binary. Every entry is an original, general-knowledge
approximation written for DinnerOS: no meal-kit company's text, no branding,
generic option names. Packet sizes are estimates.

| Specialty ingredient | Options (default first) |
| --- | --- |
| Chicken, Beef, Veggie, Mushroom, Pork Ramen Stock Concentrate | store (bouillon base, seasoned where needed) |
| Sweet Soy Glaze | store (soy and honey), batch |
| Southwest Spice Blend | batch (12 tbsp, 180 days), store |
| Sweet Thai Chili Sauce | store (bottled sweet chili sauce) |
| Tex-Mex Paste | store (tomato paste and chili spices), batch |
| Ponzu Sauce | store (soy with citrus), store (bottled ponzu), batch |
| Cream Sauce Base | store (heavy cream with cream cheese) |
| Smoky Red Pepper Crema | store (sour cream with roasted peppers), batch |
| Fry Seasoning | batch, store |
| Szechuan Paste (alias Sichuan Paste) | store (chili garlic sauce with soy), batch |
| Tuscan Heat Spice | store (Italian seasoning with chili flakes), batch |
| Mexican, Fajita, Shawarma Spice Blend | batch, store |
| Bulgogi Sauce, Umami Ginger Sauce | store, batch |
| Blackening Spice, Brown Sugar Bourbon Seasoning | batch, store |
| Sesame Dressing | store (bottled toasted sesame dressing), batch |
| Miso Sauce Concentrate, Cheese Roux Concentrate | store |
| Garlic-Ginger Scallion Paste | store (garlic and ginger paste with scallions), batch |
| Sweet and Smoky BBQ Seasoning, Tunisian, Cuban Spice Blend | batch, store |

29 specialty ingredients, 20 with a batch. Plain store products (hoisin, BBQ,
gochujang, soy sauce, tomato paste, hot sauce, Italian seasoning) and produce
mixes (coleslaw mix) aren't in it; `TestEmbeddedSeed` enforces that, and that
no text names a meal-kit brand.

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

| Choice | What the list does |
| --- | --- |
| none | The item stays, `specialty: true`, with `specialtyDetail.suggestedOptions` (default first) |
| `as_is` | The item stays, `specialty: true`, `choiceType: as_is`, no suggestions |
| store alternative | The line becomes the option's ingredients, scaled by the line's amount converted to `per` (through `unitSizes` for packets), aggregated with everything else. Each carries `via` ("for Tex-Mex Paste in Smoky Pork Tacos"). A line without an amount, or in units that don't convert, lists the ingredients without amounts |
| house-made batch | The week's amounts are totaled in the yield's unit. If the batch item is `in_stock` and its estimate covers the total (or can't be compared), the lines stay as one `inPantry` item keyed `name:<key>`, `houseMade: true` ("In pantry (house-made)"). If it's low, out, missing, or short, the lines are replaced by the ingredients for enough batches to cover the shortfall (at least one), each with `via` ("to make Southwest Spice Blend (makes about 12 tbsp)") |

Every house-made specialty the week uses is also in the response's
`batches`: `status` `inPantry` or `make`, `reason` (`enough`, `inStock`,
`notEnough`, `low`, `out`, `missing`), `batches`, `needed`, `remaining`, and
`pantryItemId`. See [api.md](api.md#grocery-list) for the JSON.

A failure to load choices fails the list, like the pantry (decision 58).

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

## Limitations

- **Approximations.** Blends and sauces are generic; flavor won't match a
  meal kit exactly. Packet sizes are estimates, so conversions from packet
  counts are estimates too.
- **Only curated specialties are flagged.** A household can't flag a new
  specialty ingredient yet, and unknown names aren't guessed.
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
