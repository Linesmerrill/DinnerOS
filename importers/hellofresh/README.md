# HelloFresh historical importer

A **separate, offline tool** — not part of the API runtime — that imports the
recipe and order history belonging to *our own* authenticated HelloFresh
account into DinnerOS. It implements the `RecipeSourceImporter` seam so other
sources (manual entry, file import, partner catalogs) can follow the same path.

Status: planned for Phase 5. Nothing here runs yet.

## Rules (non-negotiable)

- Only content available to the authenticated account owner. No catalog crawling.
- No bypassing authentication, CAPTCHAs, or other access controls.
- Rate-conscious: sequential requests, conservative delays, backoff on errors.
- Session material (cookies, tokens) is supplied locally at run time and never
  written to the repository.
- Imported data **never** enters Git. Output goes to `importers/hellofresh/data/`
  (git-ignored) or directly to the DinnerOS API.
- The DinnerOS repository is public; paying for a service does not grant the
  right to redistribute its recipe content.

## Pipeline

```text
HelloFresh account (owner session)
        │  fetch order history, then each ordered recipe
        ▼
Raw import model        ← preserved verbatim (source JSON / strings)
        │
        ▼
Normalizer              ← parse quantities/units, map to canonical ingredients
        │
        ▼
Validation              ← low-confidence items go to a review queue, never guessed
        │
        ▼
DinnerOS API → MongoDB  ← upsert keyed by (source="hellofresh", sourceRecipeId)
```

## Printed recipe cards

A public recipe page can redirect a weekly clone to a different variant (pork
delivered, beef page). `go run . variants` lists those deliveries. For each one,
and for recipes with no steps, `go run . cards -history 'data/history-part*.tsv'`
fetches the delivered recipe's printed card PDF — one request at a time, 3 s
apart, stopping on any refusal that isn't a missing file — into
`data/raw/cards/<deliveredId>.pdf` (misses are recorded as `.missing.json` and
not asked for again unless `-retry-missing`), then parses every cached card
into `data/raw/cards/<deliveredId>.json`. `-offline` only re-parses.

`normalize` prefers an account capture, then a card, then the public page. A
card of a different dish becomes its own recipe; a card that can't be parsed is
not used and the delivery's review item stays. Cards, like everything under
`data/`, never enter Git.

## Idempotency

- Recipes are upserted by `(source, sourceRecipeId)`, with `sourceURL` as a
  secondary match. Re-running never duplicates recipes.
- Order dates are merged into `historicalOrderDates[]` as a set.
- A checkpoint file in `data/` lets an interrupted import resume.

## Normalization confidence

Ingredients such as "Tex-Mex Paste", "Cream Sauce Base", or "Chicken Stock
Concentrate" often have no clean grocery equivalent. The importer keeps the raw
string, attaches the best candidate *only* when confidence is high, and otherwise
flags the line for human review.
