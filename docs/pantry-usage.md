# Smart pantry: usage tracking

Status: API implemented (`api/internal/pantry`, `api/internal/notifications`)
and iOS implemented ([iOS](#ios)). Push delivery (APNs) is implemented
(`api/internal/push`, `api/cmd/sendreminders`; [Push delivery](#push-delivery)).

A pantry item knows how much the household bought, what cooked recipes used,
and how fast the household uses it otherwise. From that it estimates what's
left. When the estimate crosses a threshold, the item is marked `low` and the
household gets a notification.

Example: the grocery list says butter. A member checks it off and confirms
"Add to pantry? 1 cup". Four recipes later the estimate reads "About 19% left:
4 recipes used 0.81 cup", which is past the default 80% threshold. Butter
becomes `low` (set by the estimate), "Butter is running low" appears in the
household's notifications, and the week's grocery list shows it as `toBuy`
again.

## Data model

| Record | Where | What it holds |
| --- | --- | --- |
| Purchase | `pantry_purchases` | item, `source` (`grocery_list`, `manual`, `provider`, `house_made`), quantity and unit, optional unit size, optional ISO week, optional `clientPurchaseId`, who recorded it, `purchasedAt` |
| Usage cycle | `pantry_items.tracking` | the current cycle: its ID (the purchase ID), source, start, tracking unit, starting amount (100%), current segment start and amount, recipe use in the segment and the whole cycle, recipe count, skipped deductions |
| Segment history | `pantry_items.history` | up to 6 closed segments: start, end, unit, starting amount, recipe use, remaining, and whether a person observed the remaining amount |
| Learned rate | `pantry_items.rate` | non-recipe use per day (exact, in a unit), segments it's based on, when computed |
| Unit size | `pantry_items.unitSize` | one discrete unit's size: "1 package = 8 oz" |
| Threshold | `pantry_settings.lowThresholdPercent` (household, default 80) and `pantry_items.lowThresholdPercent` (item override) | percent of the starting amount used that makes an item low |
| Status provenance | `pantry_items.statusSource` (`person`/`estimate`), `statusSetAt`, `lowAlertCycleId` | who set the status, when, and which cycle already alerted |
| Cook deduction | `pantry_cook_usage` | one record per cooked meal (`sourceKey` is unique): recipe, servings, and per matched item the amount deducted or why not |
| Notification | `notifications` | household, type, title, body, subject (`pantry_item` + ID), dedupe key, readers, push state |

Details of every collection and index are in
[database.md](database.md#pantry-usage).

### Cycles and segments

- A **cycle** starts when the household buys an item (a purchase) or first
  records an amount by hand (source `edit`). Its starting amount is 100%.
- A person **correcting the amount** mid-cycle ends the current **segment**
  with the amount they saw and starts a new segment from it. The cycle, and
  its 100%, stay the same. A correction above the starting amount, or in a
  unit that doesn't convert, starts a new cycle instead.
- **Remaining** = segment start − recipe use since the segment started −
  learned rate × days since, never below 0, and 0 when the item is `out`.
- **Purchases add up.** A purchase of an item that is still `in_stock` carries
  the estimated remaining amount into the new cycle: a second bottle of soy
  sauce with 6 fl oz left starts a cycle of 16 fl oz, not 10. The closed
  segment records that estimate as `remaining` with `carried: true`, and the
  learned rate skips carried segments (the remainder is the estimate itself,
  so it teaches nothing). Less than 10% of the old starting amount
  (`MinCarryPercent`), or a remainder in a unit that doesn't convert, counts
  as used up instead.
- A purchase of an item that is `low` (by anyone) or `out` assumes the previous
  cycle was used up (remaining 0, `observed: false`), which is what the rate
  learns from. If a person marked the item `out` first, the segment ends at
  that moment instead (`observed: true`). A person who has a different amount
  corrects it, and their number wins.
- A purchase without an amount, or clearing an item's amount, ends tracking.
  History and the learned rate are kept. Marking an item `out` clears its
  amount but keeps the cycle.

### Purchases

`POST /households/{id}/pantry/purchases` records one. Until shopping
providers exist:

- **Grocery list check-off.** When a member checks off a line, the app asks
  "Add to pantry?" with the line's amount prefilled and editable. On confirm
  it sends `source: grocery_list`, the line's `ingredientId` (or `name` for an
  uncatalogued line), the amount, the list's `week`, and a `clientPurchaseId`.
  Declining sends nothing. The `grocery.item_checked` event is still
  recorded as before.
- **Manual restock** from the Pantry tab sends `source: manual` with the
  item's `itemId`.
- **Shopping providers** write the same record with `source: provider` when a
  member confirms "Did you order these?", but only for the lines the pantry
  tracks ([below](#what-goes-in-the-pantry)). Apps can't send `provider`.
- Every purchase may carry `priceCents`, what it cost in all, set when it's
  recorded or later (`PATCH .../pantry/purchases/{purchaseId}`). Prices
  don't change tracking; shopping's weekly cost reads them
  ([grocery-engine.md](grocery-engine.md#weekly-cost)).
- **House-made batches** of specialty ingredients (a jar of spice blend) are
  purchases with `source: house_made`, recorded by
  `POST .../specialty-ingredients/{specialtyId}/batches` with the batch's
  yield, packet size, and shelf-life expiry. The batch item's key is the
  specialty ingredient's normalized name, so cooked recipes deduct from it
  like anything else; a recipe naming an alias or counting packets matches
  through the specialty module's `KeyResolver`. Apps can't send `house_made`
  to the purchases endpoint. See [specialty-ingredients.md](specialty-ingredients.md).

A grocery line with two amounts ("1 onion" and "8 oz onion") is two
purchases or one the member chooses; the API takes one amount per purchase.

### What goes in the pantry

Confirming a Walmart order (`shopping/leftovers.go`) decides per line:

| Line | Pantry |
| --- | --- |
| Counted by exact measure (`coverage: per_amount`): rice, oil, spices, sauces, sugar, or anything a member switched to it | Tracked: a `provider` purchase |
| Bought for the week (`per_week`: produce, dairy, bakery, deli) with a known package size, all of the week's need convertible to it (exactly or by density), and at least 25% of what was bought left over (`MinLeftoverPercent`) | Tracked: a tub of sour cream for 2 tbsp, a dozen eggs for two, a 3-count bag of onions for 2 |
| Any other `per_week` line: no size, a need that doesn't convert (4 cloves against a bulb), or mostly used | Not tracked |
| Meat and seafood (`per_week`) | Never tracked: what's left is frozen or thrown out, not kept on a shelf |

A line that isn't tracked is still confirmed, with `pantry: not_tracked` and
no purchase. Putting it in the pantry would leave it `in_stock` and keep it off
next week's list ("In your pantry" for last week's ground beef), which is the
gap this rule closes. The same week's Shop tab leaves it out as `ordered`
("Ordered this week") so it isn't sent again. Tracked leftovers count down as
meals are cooked, add up when bought again, and go back on the list when the
estimate marks them `low`.

## Unit conversion

Amounts convert exactly or not at all. There is no density or "1 onion ≈
8 oz" table.

| From → to | Converts? |
| --- | --- |
| volume ↔ volume (tsp, tbsp, fl oz, cup, ml, l) | yes, exact factors |
| weight ↔ weight (oz, lb, g, kg) | yes, exact factors |
| a discrete unit (count, package, can, …) ↔ the same unit | yes |
| a discrete unit with a known size ↔ the size's kind | yes: 2 packages × 8 oz = 16 oz |
| volume ↔ weight | no, except a cooked recipe's deduction (below) |
| a discrete unit ↔ anything else, without a size | no |

- A purchase may send `unitSize` when its unit is discrete ("4 count, each
  1/2 cup"). The item remembers it for later purchases and recipe lines in
  that unit, and the cycle is tracked in the size's unit.
- **A cooked recipe's spoons against a package bought by weight are
  estimated.** 2 tbsp of sour cream from a 16 oz tub converts with the
  ingredient's typical density (`providers.EstimateAmount`, the table package
  counting uses: sour cream ≈ 1.05 g/ml, brown sugar 0.9, spices 0.5), rounded
  to thousandths, and the cook line is marked `estimated: true`. Without it the
  owner's sour cream, brown sugar, and spices never counted down. Only cooked
  recipes estimate: a person's amounts and purchases still convert exactly or
  not at all.
- When a cooked recipe's amount still can't convert (a count against ounces
  with no size), or has no amount ("salt to taste"), **the deduction is
  skipped and flagged**. The cook record keeps
  `skipReason` (`unit_mismatch`, `no_amount`), and the item's
  `estimate.skippedRecipes` counts it, so the app can say "1 recipe couldn't
  be counted". Nothing is guessed.
- Other skip reasons: `not_tracked` (no amount recorded), `item_out`, and
  `before_cycle` (the meal was cooked before the latest purchase or
  correction).

## Cooking deducts recipes

"Mark as Cooked" sends a `recipe.cooked` event. The events service passes
stored events to listeners (`events.Listener`); the pantry's listener
deducts the recipe:

1. The recipe's amount for the cooked `servings`. When the recipe has no
   amounts for that size, the nearest authored size is scaled linearly
   (`scaledFrom` on the record).
2. Each ingredient is matched to a pantry item by catalog ID, the catalog
   key, or its normalized name.
3. A `pantry_cook_usage` record is inserted with a unique
   `{householdId, sourceKey}`. `sourceKey` is `entry:<entryId>` for a planned
   meal, so re-marking the same entry cooked (a new tap, a retry, or a
   duplicate event) never deducts twice. An unplanned cooked event uses
   `event:<userId>:<clientEventId>`. An event with neither, or without
   `servings`, deducts nothing.
4. Each item's cycle adds the converted amount, with version-checked writes
   that retry on conflicts, and then runs the low-stock check.

A planned meal that was customized ([api.md](api.md#customize-a-meal))
deducts what was actually cooked: before step 1, the entry's swapped or
doubled protein replaces the recipe's line, with every authored amount scaled.
The entry is found by its ID in the plans around the cooked date. A failure to
read the customization fails that deduction instead of deducting an ingredient
that may not have been cooked; an entry that isn't customized, or isn't found,
deducts the recipe as written.

Deduction is best effort, like event recording (#71): a failure is logged and
the event is still stored. Any member who can send events can cook, so
deductions don't require `pantry.edit`.

## The learned rate

Butter is used on toast, not only in recipes. For each closed segment:

```text
other use per day = max(0, start − recipe use − remaining) ÷ days
```

- Only segments lasting at least **1 day** count, from the **6 most recent**.
- The rate is the **median** of those, and needs **at least 2**. Until then
  the estimate uses recipes only and `dailyRate` is `null`.
- Segments in a unit that doesn't convert to the current one are ignored.
- It's recomputed whenever a segment closes: a purchase, a correction, or an
  amount after being out.
- **Bias:** a purchase without an observation assumes the item was used up.
  Households that restock early make the rate high and alerts early. A
  person's correction or marking the item out replaces the assumption with
  what they saw. The median limits the effect of one bulk purchase.

The response explains the estimate (see [the estimate](#estimate-fields)):
`"About 30% left: 2 recipes used 6 tbsp, plus about 1 tbsp a day of other
use."`

## Alerts

The estimate marks an item `low` when all of these are true:

- it's tracked and `in_stock`;
- the used share of the starting amount is at least the threshold (the
  item's `lowThresholdPercent`, or the household's, default 80);
- this cycle hasn't alerted yet (`lowAlertCycleId` ≠ the cycle ID);
- no person set the status since the current segment started.

Then, in this order:

1. A `pantry.low` notification is created with dedupe key
   `pantry.low:<itemId>:<cycleId>`. The unique index makes it idempotent.
2. The item is set `low` with `statusSource: estimate`, `statusSetAt`, and
   `lowAlertCycleId`, conditional on its version.

If step 2 loses a race or fails, the next check retries it, and step 1
returns the existing notification. **At most one alert per item per cycle.**

### People win

- A person's status change (PATCH, bulk, or Add) sets `statusSource: person`.
  The estimate won't change it for the rest of that segment. Setting an
  estimated `low` back to `in_stock` sticks, and that cycle doesn't alert
  again.
- A person's amount becomes the new segment start (see above). The estimate
  may then act on the corrected amount, but still alerts at most once per
  cycle.
- A person marking an item `low` or `out` never creates a notification.
- A new purchase starts a new cycle, which can alert again.

## When estimates are computed

Estimates are never stored. They're computed from the cycle, the learned
rate, and the clock:

| Trigger | What happens |
| --- | --- |
| Purchase | the previous segment closes (rate relearned), a new cycle starts |
| Cook (`recipe.cooked` stored) | recipe use is added, then the low-stock check runs for those items |
| Person edits an amount or status | the segment closes or the status source changes, then the check runs for the item |
| `GET .../pantry` | the check runs for every tracked item before responding, so time-based decay is applied |
| `GET .../notifications`, `GET .../notifications/unread-count` | the same household-wide check runs first (`notifications.Refresher`, bounded to 3 s, failures logged) |
| Any item response | the estimate is computed at response time (`estimatedAt`) |
| Push sweep (`cmd/sendreminders`, hourly) | the same household-wide check runs for **every** household, so an item that ran low while nobody had the app open still alerts |

An item that crosses its threshold only because days passed is marked low the
next time any member opens the pantry or notifications, or at the next hourly
push sweep, whichever comes first. `Refresh` is idempotent, so overlapping
sweeps and in-app reads are safe.

## The freezer

A pantry item now knows where in the house it's kept: `storage` is `pantry` or
`freezer`, and an empty value means `pantry`, so every item written before the
freezer existed reads exactly as it did.

**Why there is a freezer at all.** The week's list forces package sizes nobody
asked for. The smallest pork loin at the store is 4 lb and Thursday's meal uses
10 oz, so the week buys 54 oz it has no plan for. The leftovers rule
deliberately refuses to track meat as shelf stock
([What goes in the pantry](#what-goes-in-the-pantry)): calling raw pork
"in stock" would be a lie, and it would keep the line off next week's list. So
the remainder went unrecorded — and next month's list bought another four-pound
loin. The freezer is the third answer: neither a lie about a shelf nor silence.

**Freezing** (`POST .../pantry/freezer`, `pantry.Service.Freeze`) records:

- **the amount actually sealed** — the real remainder ("16 oz"), never the
  package size. It's required;
- **how many portions** it was split into (`portions`, 0 meaning one, up to
  `MaxPortions`);
- **the date** it was sealed (`frozenOn`);
- **the handoff line it came from** (`frozenFrom`, `<handoffId>:<lineId>`).
  That last one makes freezing **idempotent**: sealing the same line twice
  changes nothing and returns the item as it already stands. Without a source
  it's an ordinary write, so a member can freeze something the app never
  bought.

The ingredient's pantry item is added when it's missing, and the sealed amount
restocks it and starts an ordinary usage cycle (`CycleFrozen`). Everything in
this document then applies unchanged: cooked meals count the freezer down like
any other pantry amount, segments close, the rate is learned, and the item goes
`low` when the estimate says so. A frozen item keeps no `expiresOn` — a fridge
date from before it was sealed would only mislead.

**A frozen item stays on the grocery list.** This is the point of the whole
feature, so `GroceryPantry` sorts in-stock items by storage:

| Pantry item | In `PantryStock` | Grocery status of its lines |
| --- | --- | --- |
| `in_stock`, `storage: pantry` | `InStock` | `inPantry` — off the list |
| `in_stock`, `storage: freezer` | `InFreezer` | `fromFreezer` — still on the list, de-emphasized, "Grab from the freezer" |

An `inPantry` line disappears, which is right for a tub of sour cream and wrong
for four pounds of pork: something has to take it out of the freezer in time.
So a `fromFreezer` line is shown and not bought. The freezer beats the staple
hint and beats buying it again, but loses to a fresh item already in the pantry
(that one is nearer to hand) and to a skip (the household asked for it to stay
off the list). Exports leave it out with the reason `in_freezer`
([shopping-providers.md](shopping-providers.md#bulk-packs)), and
[grocery-engine.md](grocery-engine.md#pantry) lists the status beside the
others.

Where the remainder comes from in the first place — which lines are bulk packs,
and the choice between freezing one and planning a second meal with it — is in
[shopping-providers.md](shopping-providers.md#bulk-packs). **How many portions
to split it into** is the prep plan's question, not this one's: the week's own
meals decide the size, and the count decides the thaw estimate below
([the prep plan](shopping-providers.md#the-prep-plan)).

## Thaw reminders

The freezer only works if something takes the food out of it in time. A pork
loin sealed in March isn't dinner on a Thursday unless somebody remembers on
Thursday *morning*, and "I'll remember" is exactly the thing that fails.

### How long it takes

`pantry.ThawFor` estimates fridge thawing for **one portion**:

```text
hours = portion ounces ÷ 16 × hours per pound
```

| Constant | Value | Why |
| --- | --- | --- |
| `FridgeHoursPerPound` | 5 | USDA fridge-thaw guidance is roughly a day per 4–5 lb of meat; 5 hours per pound is that range taken on the safe side |
| `LightHoursPerPound` | 2 | bakery, deli and dairy: bread and deli slices aren't a dense frozen block |
| `MinThawHours` | 2 | the floor — even one small portion needs a couple of hours out |
| `MaxThawHours` | 48 | the ceiling — beyond two days the advice is the same: start now |
| `DefaultThawHours` | 8 | when the amount isn't a weight and can't be converted to one. Long enough to be safe, short enough to be actionable; the estimate is then marked `measured: false` |

The result is clamped to `[MinThawHours, MaxThawHours]` and rounded to whole
hours, because nobody plans a thaw to the minute.

**It weighs one portion, not the bag.** A 64 oz pack split into four sealed
portions is **5 hours, not 20**. That is the difference between "put it in the
fridge after lunch" and "you should have started yesterday" — an estimate for
the whole bag would be wrong for every household that did the sensible thing
and portioned it, and wrong in the direction that makes the advice ignorable.

A volume converts to a weight through the item's typical density (the same
table cooked deductions use, see [Unit conversion](#unit-conversion)); a
discrete unit with a known size converts through that size. Anything else falls
back to `DefaultThawHours`.

### The reminder

Like the grocery order reminder, **nothing is stored**. "Is a frozen item
needed today, and has it long enough to thaw?" is a question about today,
answered from facts that already exist: the week's plan, its recipes'
ingredients, and what's in the freezer. It's derived on read, and — for a phone
nobody opened — by the hourly sweep.

- **It fires on the day a planned meal needs the item.** Today's plan entries
  are read in the household's time zone, their recipes' ingredients are matched
  against in-stock freezer items by the same keys a grocery line carries (the
  catalog ingredient ID, or the normalized name), and each match becomes one
  item with the meals that need it named.
- **`DinnerHour` = 18 local is the only guess in the model,** and the advice is
  anchored to it. The move-by time is dinner less the estimate, so five hours
  reads: *"Chicken Thighs is for Tuscan Chicken tonight. This usually takes
  about 5 hours in the fridge — put it in the fridge by 1 PM, or this morning
  if that's easier."*
- **When the move-by time has already passed** (the usual case for anything
  longer than a workday) the wording changes to *"— move it over this
  morning"* rather than pretending the deadline is still ahead.
- **The household picks the hour** (`thawReminderHour` on the household, 0–23,
  default `households.DefaultThawReminderHour` = 6). Before that hour nothing
  is created: the errand is theirs to do when they're up, and a reminder that
  arrives after the errand is useless while one at 4am is worse than useless.
- **One per item per day.** The notification is
  `notifications.TypePantryThaw` (`pantry.thaw`), its subject is the pantry
  item, and its dedupe key is `pantry.thaw:<date>:<itemId>`, so however often
  the bell is read the household gets one reminder per item per day.
- **Delivery is the existing path**: created by the same `Refresher` that runs
  on a notification read and by `cmd/sendreminders` on the hourly Heroku
  Scheduler run, then pushed by the sweep
  ([Push delivery](#push-delivery)). Before pushing, the sweep asks whether the
  reminder is still true (`push.Relevance`): an item that was taken out, or
  whose meal moved, is `skipped`.
- **Thaw reminders are exempt from quiet hours** (`push.DefaultQuietExempt`).
  Everything else waits out the household's night and arrives after 08:00, but
  a 6am reminder the household itself scheduled must not be held until 8 —
  by then the fridge has lost two hours it can't get back.

`GET /households/{id}/thaw` returns what to move today: the date, the
household's reminder hour, and for each item its meals, the hours, whether they
were measured, the move-by time, and the sentence above ready to show.

## Notifications

`notifications` holds household notifications. Every member sees the same
notifications, and each member reads them separately (`readBy`).

| Field | Meaning |
| --- | --- |
| `type` | `pantry.low` or `shopping.order_due`; stable, so apps can choose an icon and destination |
| `title`, `body` | ready to display and to send as a push alert ("Butter is running low" / the estimate summary) |
| `subject` | `{kind: "pantry_item", id}`: what to open |
| `dedupeKey` | unique per household; a producer's retry returns the existing notification |
| `readBy` | member IDs who've read it |
| `push` | `{status, attempts, lastAttemptAt?, sentAt?}`; created `pending` |

### Push delivery

The collection is the push outbox, and `cmd/sendreminders` is its sender.
Heroku Scheduler runs it hourly ([deployment.md](deployment.md#push-notifications)).
Producers change nothing: they create notifications `pending`, and in-app
reads never touch `push`. Each run:

1. **Refreshes every household.** It pages through all households and runs
   the same refreshers a notification read runs — the pantry's low-stock check
   and shopping's order reminder (`notifications.Service.Refresh`). Both are
   derived on read, so without this a closed app would never produce the
   notification at all.
2. **Takes pending notifications**, oldest first (partial index
   `push_pending_createdAt`, up to 1000 a run).
3. **Waits out the household's night.** Between 21:00 and 08:00 in the
   household's time zone a notification stays `pending` for a later run, so an
   order reminder that becomes due at midnight arrives with breakfast.
4. **Claims it** with one conditional update, `pending` → `sending`, counting
   the attempt. Only the run that wins the claim sends, which is what makes a
   push go out **at most once**, even when runs overlap or a run is repeated.
5. **Checks it's still true** (`push.Relevance`): an order reminder whose
   week has since been marked ordered, or a low-stock alert whose item was
   restocked or deleted, is `skipped`. A check that errors doesn't block the
   push.
6. **Sends it** to every registered device (`device_tokens`) of every current
   member who hasn't read it, each token to its own APNs gateway (sandbox or
   production). Members who already read it on the bell get nothing; neither do
   people who left the household.
7. **Records the outcome**: `sent` (`sentAt`) when any device accepted it,
   `failed` when every send failed for another reason, `skipped` when nobody
   was left to send to or the notification is more than 24 hours old (it's no
   longer news — this also keeps notifications from before push existed from
   arriving all at once).

There is no retry. A `failed` or `sending` push stays that way: sending again
could alert the devices that did get it, and a missed alert still waits on the
bell. APNs `BadDeviceToken` and `410 Unregistered` delete the token.

The alert is the notification's `title` and `body`. Beside `aps` the payload
carries `notificationId`, `householdId`, `type`, and `subject`, so tapping it
opens what the row on the bell opens. `thread-id` is the type and
`apns-collapse-id` the notification ID.

Without `APNS_KEY_ID`, `APNS_TEAM_ID`, and `APNS_AUTH_KEY` (local development)
the sweep still refreshes, logs that push is off, and leaves everything
`pending`. The web server never needs APNs credentials.

**Permission** is asked on iOS only at moments that explain themselves: after
a member saves an order day in Household Settings, or taps **Notify Me on
Order Day** on the Shop tab's order reminder. It is never asked at launch, and
a refusal changes nothing else — the bell is the whole product without push.

## Estimate fields

Every pantry item response carries `statusSource`, `lowThresholdPercent`
(the item's override or `null`), `unitSize`, and `estimate` (`null` when not
tracked). [api.md](api.md#usage-estimates) has the full shape. For example:

```json
"estimate": {
  "cycleId": "66e5a1f2c3b4a5d6e7f80f01",
  "cycleSource": "grocery_list",
  "cycleStartedAt": "2026-09-15T18:30:00Z",
  "adjustedAt": null,
  "unit": "tbsp",
  "startAmount": { "quantity": "16", "quantityValue": 16 },
  "remaining": { "quantity": "5", "quantityValue": 5 },
  "percentRemaining": 31,
  "percentUsed": 69,
  "recipeUse": { "count": 2, "quantity": "6", "quantityValue": 6 },
  "otherUse": { "quantity": "5", "quantityValue": 5 },
  "dailyRate": { "quantity": "1", "quantityValue": 1, "basedOnSegments": 3 },
  "skippedRecipes": 0,
  "lowThresholdPercent": 80,
  "thresholdSource": "household",
  "belowThreshold": false,
  "summary": "About 31% left: 2 recipes used 6 tbsp, plus about 1 tbsp a day of other use.",
  "estimatedAt": "2026-09-20T18:30:00Z"
}
```

`remaining` and `otherUse` are rounded to hundredths, and `dailyRate` to
thousandths. `startAmount` and `recipeUse` are exact. `summary` is English;
apps may build their own text from the numbers.

## iOS

| Flow | Where | What happens |
| --- | --- | --- |
| Grocery check-off | Week → Grocery List (`GroceryListModel`, `GroceryPurchaseBanner`) | Checking a line off shows a card at the bottom: "Add *line* to pantry?" with the line's first amount and unit editable (the ⋯ menu offers the line's other amounts). **Add** closes the card and records `source: grocery_list` in the background; **Skip** records nothing. "Don't Ask During This Trip" stops the card until the list is left. Checking another line replaces an unanswered card. A failure shows **Try Again**, which resends the same `clientPurchaseId`. `grocery.item_checked` is still recorded for every check. |
| House-made batch | Pantry → ⋯ → **Specialty Ingredients…** → **Made a Batch**; grocery list → **Made It** | Records `source: house_made` through the specialty ingredients endpoint ([iOS](specialty-ingredients.md#ios)). The returned item replaces the pantry row, and purchase history reads "House-made batch". |
| Restock | Pantry → item → **Restock / I Bought This** (`PantryRestockSheet`) | Amount, unit, and for a discrete unit an optional package size (`unitSize`). Records `source: manual` with `itemId`. Also on the read-only item detail for members with `pantry.edit`. |
| Estimates | Pantry rows (`PantryItemRow`), item edit sheet or detail (`PantryUsageSections`) | Rows with an `estimate` show a ring and "~31% left". The item screen shows the ring, remaining amount, `summary`, recipe use, daily rate, skipped recipes, the threshold in effect and whether it's the item's or the household's, and the 20 most recent purchases. |
| Status source | `PantryStatusPill` | `statusSource: estimate` with `low` shows an outlined, dashed **Estimated Low** pill; a person's status keeps the filled pill. |
| Thresholds | Pantry → ⋯ → **Low-Stock Alerts…** (`PantryThresholdSheet`); item edit sheet (`PantryThresholdFields`) | The household's percent used (1–100, default 80), read-only without `pantry.edit`. An item toggles **Use Household Setting** off to set its own, which `PATCH`es an integer; turning it back on sends `null`. |
| Notifications | Bell in the Pantry and Week toolbars (`NotificationsView`) | Unread badge from `unread-count`. The sheet lists notifications 50 at a time and loads the next page when the last row appears. Tapping a notification marks it read; `pantry.low` opens the item. **Mark All Read** sends `all: true`. |
| Cooking | Week → swipe or long-press → **Mark as Cooked** | Sends the queued events right away, then refreshes the pantry and the unread count so the deduction shows. |
| Push | Lock screen or Notification Center (`AppDelegate`, `PushNotificationStore`, `MainTabView`) | Tapping a push opens what its bell row opens — `pantry.low` the item (in a sheet), `shopping.order_due` that week in Shop, anything else the bell — switching to its household first if another one is shown, and marks it read. A push that arrives while the app is open still shows as a banner and refreshes the badge. |
| Push registration | `PushNotificationStore`, `DeviceTokensAPI` | Once alerts are allowed, every sign-in and return to the foreground registers for a token and `PUT`s it (`sandbox` in Debug builds, `production` otherwise) once per launch; a rotated token replaces the old one. Sign-out `DELETE`s it before the session's tokens are cleared, waiting at most 3 s. |

The unread count refreshes when a household is activated, when the app becomes
active, after every pantry load or change, after cooking, when a push arrives
while the app is open, and when the notifications sheet closes. Members without `pantry.edit` aren't asked "Add to
pantry?" and don't see Restock or threshold controls; a `403` from a purchase
also turns the prompt off and reloads the household's permissions.

## Limitations and open questions

- **Estimates, not measurements.** Density is used only for cooked recipes'
  volume ↔ weight deductions, and there's no "1 onion ≈ 8 oz". Items bought by count with no size can only be deducted by recipes
  that use the same count unit.
- **Restock assumption.** Purchases of `low` or `out` items assume the old
  stock was used up (see [the learned rate](#the-learned-rate)); purchases of
  `in_stock` items carry the estimate, which is only as good as the estimate.
- **Alerts on time decay** are noticed at the next read or hourly push sweep,
  and pushed after 08:00 in the household's time zone.
- **Deleting an item** leaves its purchases and cook records. They're
  history and are no longer reachable through the API.
- **Un-cooking** isn't supported: there's no "unmark cooked" event, so a
  mistaken cook is corrected by editing the amount.
- **Scaling** for a serving size the recipe doesn't author is linear. The
  grocery list never scales (#50), but a deduction estimate can.
- Open: should the household be able to turn tracking off for an item
  (spices)? Today an item without an amount is simply untracked.
