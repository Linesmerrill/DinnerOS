// Package shopping hands a household's week grocery list to a shopping
// provider (docs/shopping-providers.md). It owns the household's store
// settings, the product saved for each ingredient per provider, and stored
// handoffs: the cart links built from a week's list and what members confirm
// they ordered, which are recorded as pantry purchases with source provider.
//
// Provider specifics (link formats, product IDs, package math) live in
// internal/providers. Nothing here fetches provider pages or calls provider
// APIs.
package shopping

import (
	"errors"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Errors returned by stores and the service.
var (
	ErrNotFound  = errors.New("shopping: not found")
	ErrDuplicate = errors.New("shopping: duplicate")
	// ErrForbidden means the actor's role lacks the permission.
	ErrForbidden = errors.New("shopping: forbidden")
	// ErrConflict means another request is confirming the same handoff line,
	// or changed the handoff while it was being sent again.
	ErrConflict = errors.New("shopping: concurrent change")
	// ErrUnknownProvider is a provider key DinnerOS doesn't know.
	ErrUnknownProvider = errors.New("shopping: unknown provider")
	// ErrProviderUnavailable is a known provider that isn't enabled.
	ErrProviderUnavailable = errors.New("shopping: provider unavailable")

	errHouseholdRequired = errors.New("shopping: household id is required")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Limits.
const (
	// MaxPreferencesPerProvider bounds a household's saved products for one
	// provider, so lists stay unpaginated.
	MaxPreferencesPerProvider = 1000
	MaxDisplayNameLength      = 100
	maxIngredientKeyLength    = 128
	maxQuantityLength         = 32
	// MaxSelectionKeys bounds each key list in a match or handoff request.
	MaxSelectionKeys = 300
	// MaxHandoffList is the most handoffs one list request returns.
	MaxHandoffList     = 50
	defaultHandoffList = 20
	// claimTimeout is how long a line being confirmed is reserved for the
	// request confirming it. A later request may take over a stale claim; the
	// pantry's unique handoff line keeps the purchase single.
	claimTimeout = time.Minute
)

// Settings are a household's shopping settings.
type Settings struct {
	HouseholdID string
	// Provider is the household's store, or "" when unset.
	Provider providers.Key
	// StoreID is the provider's store number, or "".
	StoreID string
	// UpdatedBy and UpdatedAt are empty until someone sets them.
	UpdatedBy string
	UpdatedAt time.Time
}

// PackageSize is how much one package of a product holds, exactly.
type PackageSize struct {
	// Quantity is exact: "n" or "n/d".
	Quantity string
	// Unit is a DinnerOS unit code: a measure (oz, fl oz) or a discrete unit
	// (count, can).
	Unit string
}

// Preference is the product a household buys for an ingredient from one
// provider.
type Preference struct {
	ID          string
	HouseholdID string
	Provider    providers.Key
	// IngredientKey is a grocery line key: a catalog ingredient ID or
	// "name:" + a normalized name. Unique per household and provider.
	IngredientKey string
	// IngredientName is for display.
	IngredientName string
	// ProductID is the provider's product (a Walmart item ID).
	ProductID string
	// DisplayName is what a member called the product.
	DisplayName string
	// PackageSize is nil when the member didn't give one.
	PackageSize *PackageSize
	// Coverage overrides how a package maps to a week's need. Empty follows
	// the ingredient's grocery category (coverage.go).
	Coverage  providers.Coverage
	CreatedBy string
	CreatedAt time.Time
	UpdatedBy string
	UpdatedAt time.Time
}

// PreferenceInput saves a product for an ingredient. Exactly one of
// ProductURL and ProductID is required.
type PreferenceInput struct {
	ProductURL string
	ProductID  string
	// DisplayName is optional when the link's slug names the product; it is
	// required when it doesn't (a bare item ID, or a link with no slug).
	DisplayName string
	// PackageSize is optional: the link's slug supplies one when it carries
	// an unambiguous size.
	PackageSize    *PackageSize
	IngredientName string
	// Coverage is the member's override, or empty to follow the category.
	Coverage providers.Coverage
}

// ExclusionReason says why a grocery line isn't in a handoff.
type ExclusionReason string

// Exclusion reasons.
const (
	// ExcludedInPantry: the pantry has it (status inPantry), and the member
	// didn't select it.
	ExcludedInPantry ExclusionReason = "in_pantry"
	// ExcludedPantryHint: recipes flag it as a staple the household probably
	// has, and the member didn't select it.
	ExcludedPantryHint ExclusionReason = "pantry_hint"
	// ExcludedHouseMade: a house-made specialty batch in the pantry; it
	// isn't bought.
	ExcludedHouseMade ExclusionReason = "house_made"
	// ExcludedCheckedOff: the app says the member already checked it off.
	ExcludedCheckedOff ExclusionReason = "checked_off"
	// ExcludedByMember: the member left it out.
	ExcludedByMember ExclusionReason = "excluded"
	// ExcludedNotSelected: the request selected lines and not this one.
	ExcludedNotSelected ExclusionReason = "not_selected"
	// ExcludedNoProduct: no product is saved for the ingredient.
	ExcludedNoProduct ExclusionReason = "no_product"
	// ExcludedNotOnList: a selected key isn't on the week's list (anymore).
	ExcludedNotOnList ExclusionReason = "not_on_list"
)

// LineStatus is a handoff line's confirmation state.
type LineStatus string

// Line statuses.
const (
	LinePending   LineStatus = "pending"
	LineConfirmed LineStatus = "confirmed"
	// LineSkipped: a member said it wasn't ordered. It can still be
	// confirmed later.
	LineSkipped LineStatus = "skipped"
)

// HandoffStatus summarizes a handoff's lines.
type HandoffStatus string

// Handoff statuses.
const (
	// HandoffOpen: some line is still pending.
	HandoffOpen HandoffStatus = "open"
	// HandoffDone: every line is confirmed or skipped.
	HandoffDone HandoffStatus = "done"
)

// Amount is an exact grocery amount.
type Amount struct {
	Quantity string
	Unit     string
}

// LineSource is the grocery line a handoff line or exclusion came from.
type LineSource struct {
	IngredientKey string
	Name          string
	Category      string
	Amounts       []Amount
	Unquantified  bool
	// GroceryStatus is empty for ExcludedNotOnList.
	GroceryStatus grocery.Status
}

// IngredientID returns the catalog ingredient ID the key names, or "".
func (l LineSource) IngredientID() string { return catalogID(l.IngredientKey) }

// HandoffLine is a grocery line with its product and package count.
type HandoffLine struct {
	ID string
	LineSource
	ProductID   string
	ProductName string
	PackageSize *PackageSize
	// Coverage is the rule the count was computed under, already resolved
	// from the saved product and the line's category, so a stored handoff
	// recomputes the same count later.
	Coverage providers.Coverage
	// ComputedPackages is providers.CountPackagesFor; Packages is what the
	// link asks for (the member's override when given).
	ComputedPackages int
	Packages         int
	// Reason is set when the computed count needs checking.
	Reason providers.Reason
	// CoversWeek is true when the count rests on the weekly coverage rule
	// rather than on measured package math.
	CoversWeek bool

	Status LineStatus
	// ConfirmedPackages, PurchaseID, ConfirmedBy, and ConfirmedAt are set
	// once confirmed.
	ConfirmedPackages int
	PurchaseID        string
	ConfirmedBy       string
	ConfirmedAt       time.Time
	// SkippedBy and SkippedAt are set when a member said it wasn't ordered.
	SkippedBy string
	SkippedAt time.Time

	// Cart is set on a match line when the week has a current handoff: what
	// that handoff already put in the provider's cart for this product. It is
	// derived on read and never stored.
	Cart *LineCart
}

// LineCart compares a match line with what the week's current handoff
// already sent to the provider's cart for the same ingredient and product.
type LineCart struct {
	// SentPackages is what the handoff's links already added.
	SentPackages int
	// AddPackages is what the next send adds: Packages less SentPackages, or
	// 0 when the cart already holds enough.
	AddPackages int
	// RemovePackages is SentPackages less Packages when the count went down.
	// A cart link can only add, so the member removes these in the provider's
	// app.
	RemovePackages int
}

// SentReason says why a product in the cart isn't one of a match's lines.
type SentReason string

// Sent reasons.
const (
	// SentNotOnList: the ingredient is no longer on the week's list.
	SentNotOnList SentReason = "not_on_list"
	// SentProductChanged: the household saved another product for the
	// ingredient since this one was sent.
	SentProductChanged SentReason = "product_changed"
	// SentNotIncluded: the line is on the list but left out of this match
	// (checked off, in the pantry, not selected, and so on).
	SentNotIncluded SentReason = "not_included"
)

// SentLine is a product the week's current handoff put in the cart that
// isn't one of the match's lines.
type SentLine struct {
	LineSource
	ProductID    string
	ProductName  string
	PackageSize  *PackageSize
	SentPackages int
	// RemovePackages is SentPackages when the product is no longer wanted
	// (not on the list, or replaced by another product), else 0.
	RemovePackages int
	Reason         SentReason
}

// CartState is what the week's current handoff already put in the cart, set
// on a match when there is one.
type CartState struct {
	HandoffID string
	// SentAt is when the handoff last changed.
	SentAt time.Time
	// Other lists sent products that aren't among the match's lines.
	Other []SentLine
}

// Excluded is a grocery line left out of a handoff.
type Excluded struct {
	LineSource
	Reason ExclusionReason
}

// CartLink is one handoff URL.
type CartLink struct {
	URL string
	// LineIDs are the handoff lines whose products the link adds.
	LineIDs []string
	// ItemCount is the number of distinct products in the link.
	ItemCount int
}

// Proposal is a week's grocery list matched to a provider's products.
type Proposal struct {
	HouseholdID      string
	Week             string
	Provider         providers.Key
	StoreID          string
	Lines            []HandoffLine
	Excluded         []Excluded
	Links            []CartLink
	AffiliateTracked bool
	// Cart is set on a match when the week has a current handoff (see
	// Service.Match). It is never stored.
	Cart *CartState
}

// CloseReason says why a handoff stopped collecting sends.
type CloseReason string

// Close reasons.
const (
	// ClosedOrdered: a member marked the week ordered.
	ClosedOrdered CloseReason = "ordered"
	// ClosedStartedOver: a member chose to send everything again.
	ClosedStartedOver CloseReason = "started_over"
	// ClosedConfirmed: every line was confirmed or skipped, so the order was
	// placed.
	ClosedConfirmed CloseReason = "confirmed"
)

// Handoff is what a week's list sent to a provider's cart, with what was
// confirmed.
//
// A household has at most one current handoff per week and provider
// (Active). Opening the provider again adds to it: new lines, and for a line
// whose package count went up, only the extra packages. Each line's Packages
// is what its links have put in the cart so far, and Links are the latest
// send's. Marking the week ordered, starting over, or answering every line
// closes it, and the next send starts a new handoff.
type Handoff struct {
	ID string
	Proposal
	// Active is true while the handoff is the week's current one.
	Active bool
	// ClosedAt and ClosedReason are set once it isn't.
	ClosedAt     time.Time
	ClosedReason CloseReason
	// Revision increases with every change to the lines, so two concurrent
	// sends can't both add the same packages.
	Revision  int64
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Status reports whether any line is still pending.
func (h Handoff) Status() HandoffStatus {
	for _, l := range h.Lines {
		if l.Status == LinePending {
			return HandoffOpen
		}
	}
	return HandoffDone
}

// Line returns the line with id.
func (h Handoff) Line(id string) (HandoffLine, bool) {
	for _, l := range h.Lines {
		if l.ID == id {
			return l, true
		}
	}
	return HandoffLine{}, false
}

// LineSelection selects a grocery line for a handoff, optionally with a
// package count.
type LineSelection struct {
	IngredientKey string
	// Packages overrides the computed count when positive.
	Packages int
}

// MatchInput shapes a proposal. With Lines nil, every toBuy line is a
// candidate; with Lines set, exactly those lines are, whatever their
// status.
type MatchInput struct {
	Lines          []LineSelection
	CheckedOffKeys []string
	ExcludeKeys    []string
}

// HandoffFilter filters a handoff list. Empty fields don't filter.
type HandoffFilter struct {
	Week   string
	Status HandoffStatus
	Limit  int
}

// ConfirmLine is one handoff line a member says was ordered.
type ConfirmLine struct {
	LineID string
	// Packages is what was ordered; 0 means the line's Packages.
	Packages int
}

// ConfirmInput says what was ordered from a handoff: All lines that aren't
// skipped, at their package counts, or the listed Lines (skipped lines
// included). Lines already confirmed return their first purchase. SkipRest
// marks the other pending lines as not ordered.
type ConfirmInput struct {
	All      bool
	Lines    []ConfirmLine
	SkipRest bool
}
