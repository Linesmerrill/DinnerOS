# Autopilot

Status: vision and interface design. Work starts in Phase 10, after the core
product is stable.

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

- DinnerOS depends only on the `RecommendationProvider` interface:

  ```go
  type RecommendationProvider interface {
      RankMeals(ctx context.Context, req RankRequest) (RankResult, error)
      GenerateWeek(ctx context.Context, req WeekRequest) (WeekResult, error)
  }
  ```

- **Phase 10:** a local, deterministic, explainable implementation.
- **Phase 12:** proprietary logic moves to a **private** repository and service.
  DinnerOS switches to a remote implementation over a versioned API
  (`RECOMMENDATION_PROVIDER=autopilot`).
- This public repository holds the interface, the request and response contracts,
  and a simple baseline. Competitive algorithms are never committed here.
- Autopilot cannot know about DinnerOS or HelloFresh specifically. It sees
  tenants, customers, catalogs, and events.

## Pipeline

```text
Candidate selection   → catalog items available for the dates and region, minus hard-constraint violations
        ↓
Feature calculation   → per (household, meal, slot) signals
        ↓
Meal ranking          → explainable score per candidate
        ↓
Week optimization     → choose the best *set* of meals for the slots
        ↓
Explanation           → the main signals behind each pick (LLM phrasing is optional, never decisive)
```

This is **not** "send every recipe to an LLM and ask what sounds good."

### Candidate features (grown over time)

Historical preference, household rating, would-make-again, weekday affinity,
cuisine and protein affinity, recipe similarity, recency, repetition, novelty,
cook time, and later weather, season, and geography.

## Constraints, preferences, objectives

These are three separate layers with strict precedence:

| Layer | Examples | Effect |
| --- | --- | --- |
| **Hard constraints** | allergy, dietary restriction, unavailable recipe, unsupported delivery region, serving limit, explicit exclusion | Candidates are filtered out *before* scoring. Nothing can override this. |
| **Soft preferences** | likes pasta, tacos on Tuesday, quick weeknight meals, avoid repeats, comfort food in cold weather | Weighted scoring signals |
| **Business objectives** (tenant-supplied) | inventory utilization, margin, promotion, waste reduction, retention | Bounded re-weighting *after* constraints. Can never reintroduce a filtered candidate. |

## Week optimization

The problem is not "which five meals score highest," but "which set of meals
makes the best week." Five top-scoring pasta dishes should not become Monday
through Friday pasta.

The week objective combines:

- the sum of meal scores
- cuisine and protein diversity (penalties for repeats within the week)
- recent repetition across previous weeks
- slot fit (weekday affinity, time available)
- a novelty budget (a controlled share of new meals)

The V1 approach is deterministic greedy or beam search over slots, with
diversity penalties, and a fixed seed for any tie-breaking. The result must be
reproducible for the same inputs and model version.

## Context engine

Context is a generic, optional map of typed signals rather than weather-specific
code:

```text
weekday · holiday · season · temperature · precipitation · region · delivery date · time available · calendar busyness
```

Feature calculators may use context when present and must degrade gracefully when
it is absent. Examples: a cold, rainy Wednesday raises comfort-food affinity; a
busy Wednesday evening favors meals under 20 minutes.

## Explainability

Every recommendation carries its primary signals:

```json
{
  "recipeId": "recipe_123",
  "score": 0.94,
  "signals": {
    "historicalPreference": 0.91,
    "weekdayAffinity": 0.87,
    "ratingAffinity": 0.95,
    "recency": 0.81,
    "variety": 0.92
  },
  "modelVersion": "baseline-2026.1"
}
```

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

LLMs are backend-only and optional (Autopilot must work without them). They can
be used for recipe classification, semantic similarity, ingredient
interpretation, cold-start preferences, and explanation phrasing. They are never
used for authentication, authorization, constraint enforcement, unit conversion,
or grocery arithmetic.

## Success metrics

The primary metric is **week acceptance rate**: the share of generated weeks
accepted with no more than one swap.

Secondary metrics: meal swap rate, recommendation conversion (planned → cooked),
average rating, repeat satisfaction, novelty acceptance, and meal skip rate.

These are computed from `meal_events` (`WEEK_GENERATED`, `WEEK_ACCEPTED`,
`WEEK_MODIFIED`, `MEAL_SWAPPED`, `MEAL_COOKED`, `MEAL_SKIPPED`, `MEAL_RATED`), so
event collection in Phase 9 must record them from day one.
