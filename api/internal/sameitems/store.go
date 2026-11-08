package sameitems

import "context"

// Store persists a household's links. Every method is scoped by householdID
// and never trusts a HouseholdID on values passed in except in PutLink, where
// it is the scope. Missing records are ErrNotFound.
type Store interface {
	// ListLinks returns the household's links, oldest first.
	ListLinks(ctx context.Context, householdID string) ([]Link, error)
	// PutLink creates the link for l.VariantKey or replaces the stored one's
	// names and kept item, keeping its ID, CreatedBy, and CreatedAt. created
	// is false when it replaced one.
	PutLink(ctx context.Context, l Link) (stored Link, created bool, err error)
	// RepointKept moves every link whose kept item is fromKey to keptKey and
	// keptName.
	RepointKept(ctx context.Context, householdID, fromKey, keptKey, keptName string) error
	// DeleteLink removes one link. A malformed ID is ErrNotFound.
	DeleteLink(ctx context.Context, householdID, id string) error
}
