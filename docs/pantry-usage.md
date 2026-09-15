# Smart pantry: usage tracking

Status: API implemented (`api/internal/pantry`, `api/internal/notifications`)
and iOS implemented ([iOS](#ios)). Push delivery (APNs) is later.

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
- A purchase assumes the previous cycle was used up (remaining 0,
  `observed: false`). If a person marked the item `out` first, the segment
  ends at that moment instead (`observed: true`). Leftovers aren't carried
  into the new cycle; a person who has more corrects the amount, and their
  number wins.
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
- **Phase 8 providers** will write the same record with `source: provider`
  (plus an order reference field added then). Apps can't send `provider`.
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

## Unit conversion

Amounts convert exactly or not at all. There is no density or "1 onion ≈
8 oz" table.

| From → to | Converts? |
| --- | --- |
| volume ↔ volume (tsp, tbsp, fl oz, cup, ml, l) | yes, exact factors |
| weight ↔ weight (oz, lb, g, kg) | yes, exact factors |
| a discrete unit (count, package, can, …) ↔ the same unit | yes |
| a discrete unit with a known size ↔ the size's kind | yes: 2 packages × 8 oz = 16 oz |
| volume ↔ weight | no |
| a discrete unit ↔ anything else, without a size | no |

- A purchase may send `unitSize` when its unit is discrete ("4 count, each
  1/2 cup"). The item remembers it for later purchases and recipe lines in
  that unit, and the cycle is tracked in the size's unit.
- When a cooked recipe's amount can't convert, or has no amount ("salt to
  taste"), **the deduction is skipped and flagged**. The cook record keeps
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

There's no scheduler. An item that crosses its threshold only because days
passed is marked low the next time any member opens the pantry or
notifications. That's enough for an in-app list.

Push notifications will need a periodic check so an alert reaches a phone
that isn't open. The plan is a small command (for example
`cmd/pantrysweep`) that calls `pantry.Service.Refresh` for households with
tracked items, run hourly by **Heroku Scheduler** (`heroku addons:create
scheduler:standard`, then a job running `bin/pantrysweep`) on a one-off dyno
with the same config vars. `Refresh` is idempotent, so overlapping runs and
in-app reads are safe.

## Notifications

`notifications` holds household notifications. Every member sees the same
notifications, and each member reads them separately (`readBy`).

| Field | Meaning |
| --- | --- |
| `type` | `pantry.low` today; stable, so apps can choose an icon and destination |
| `title`, `body` | ready to display and to send as a push alert ("Butter is running low" / the estimate summary) |
| `subject` | `{kind: "pantry_item", id}`: what to open |
| `dedupeKey` | unique per household; a producer's retry returns the existing notification |
| `readBy` | member IDs who've read it |
| `push` | `{status, attempts, lastAttemptAt?, sentAt?}`; created `pending` |

### Future APNs hook

The collection is the push outbox. The partial index `push_pending_createdAt`
finds pending notifications oldest first. A future worker will:

1. claim a pending notification (`findOneAndUpdate` to set `attempts`,
   `lastAttemptAt`, guarded by status and attempts);
2. send it to every member's registered devices (device tokens are a later
   collection) with `title`, `body`, and `subject` as the payload;
3. set `push.status` to `sent` (`sentAt`), `skipped` (no devices), or back to
   `pending` with backoff, giving up as `failed` after a few attempts.

Nothing else changes for producers. In-app reads never touch `push`.

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
| Restock | Pantry → item → **Restock / I Bought This** (`PantryRestockSheet`) | Amount, unit, and for a discrete unit an optional package size (`unitSize`). Records `source: manual` with `itemId`. Also on the read-only item detail for members with `pantry.edit`. |
| Estimates | Pantry rows (`PantryItemRow`), item edit sheet or detail (`PantryUsageSections`) | Rows with an `estimate` show a ring and "~31% left". The item screen shows the ring, remaining amount, `summary`, recipe use, daily rate, skipped recipes, the threshold in effect and whether it's the item's or the household's, and the 20 most recent purchases. |
| Status source | `PantryStatusPill` | `statusSource: estimate` with `low` shows an outlined, dashed **Estimated Low** pill; a person's status keeps the filled pill. |
| Thresholds | Pantry → ⋯ → **Low-Stock Alerts…** (`PantryThresholdSheet`); item edit sheet (`PantryThresholdFields`) | The household's percent used (1–100, default 80), read-only without `pantry.edit`. An item toggles **Use Household Setting** off to set its own, which `PATCH`es an integer; turning it back on sends `null`. |
| Notifications | Bell in the Pantry and Week toolbars (`NotificationsView`) | Unread badge from `unread-count`. The sheet lists notifications 50 at a time and loads the next page when the last row appears. Tapping a notification marks it read; `pantry.low` opens the item. **Mark All Read** sends `all: true`. |
| Cooking | Week → swipe or long-press → **Mark as Cooked** | Sends the queued events right away, then refreshes the pantry and the unread count so the deduction shows. |

The unread count refreshes when a household is activated, when the app becomes
active, after every pantry load or change, after cooking, and when the
notifications sheet closes. Members without `pantry.edit` aren't asked "Add to
pantry?" and don't see Restock or threshold controls; a `403` from a purchase
also turns the prompt off and reloads the household's permissions.

## Limitations and open questions

- **Estimates, not measurements.** No density conversions, no "1 onion ≈
  8 oz". Items bought by count with no size can only be deducted by recipes
  that use the same count unit.
- **Restock assumption.** Purchases assume the old stock was used up (see
  [the learned rate](#the-learned-rate)).
- **Alerts on time decay need a read** until the push sweep exists.
- **Deleting an item** leaves its purchases and cook records. They're
  history and are no longer reachable through the API.
- **Un-cooking** isn't supported: there's no "unmark cooked" event, so a
  mistaken cook is corrected by editing the amount.
- **Scaling** for a serving size the recipe doesn't author is linear. The
  grocery list never scales (#50), but a deduction estimate can.
- Open: should the household be able to turn tracking off for an item
  (spices)? Today an item without an amount is simply untracked.
