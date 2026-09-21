// Package catalog owns the global recipe catalog: one entry per distinct
// recipe, shared by every household, and the discovery and search surfaces
// built on it.
//
// The catalog is deliberately a separate collection from a household's
// recipes. Its documents have no householdId, no order history, no ratings,
// and no notes — there is no field in which one household's private data
// could reach another. A household's library stays exactly what it was: the
// planner, grocery engine, and Autopilot keep reading `recipes` filtered by
// householdId and never learn the catalog exists.
//
// Entries arrive through recipes.CatalogPublisher, which this package's
// Service implements, and leave by being copied into a household's library
// (Service.Add). A copy is a copy: from then on the household owns it and may
// edit, rate, plan, and cook it without the catalog changing underneath.
//
// HTTP routes live under /households/{householdId}/discover and
// /households/{householdId}/catalog. They are household-scoped only so
// results can be marked against that household's library; the catalog data
// they return is the same for everyone.
package catalog
