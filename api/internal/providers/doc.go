// Package providers is the seam between DinnerOS and grocery shopping
// services (docs/shopping-providers.md). A GroceryProvider reads product
// references, builds the handoff that puts a week's products in the
// service's cart, and declares what else it can do through optional
// interfaces (search, lookup, stores, cart write, order import).
//
// Everything here is pure: no network access, no database, no clock. Walmart
// is the first provider, with the add-to-cart link as its only capability
// (Phase 8a). Package counts (CountPackages) are exact rational math, table
// tested separately from any HTTP client. Household settings, saved
// products, and stored handoffs live in internal/shopping.
package providers
