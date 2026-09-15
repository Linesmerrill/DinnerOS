// Package recipes owns a household's recipes, the global ingredient catalog
// they reference, and the idempotent import of source-neutral recipe files
// (docs/import-format.md).
//
// Recipes and import review items are household-scoped: every store method
// filters on householdId. The ingredient catalog is shared by all households.
// HTTP routes live under /households/{householdId}/recipes and are authorized
// by households.RequirePermission.
package recipes
