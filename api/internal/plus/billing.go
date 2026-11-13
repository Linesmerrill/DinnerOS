package plus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Your Plus (decision 680): a household's plan, status, billing history from
// the RevenueCat webhook, and what Plus did for it.

// BillingEvent is one RevenueCat event kept for a household's billing
// history. Apple sends the receipts; this is only a timeline.
type BillingEvent struct {
	HouseholdID string
	EventID     string
	// Type is RevenueCat's event type, like RENEWAL.
	Type      string
	At        time.Time
	ProductID string
	Trial     bool
	// Refund is a CANCELLATION Apple's support made: a refund.
	Refund bool
	// Amount is what was charged, in Currency; zero for a free trial.
	Amount   float64
	Currency string
}

// Title is the history line's words.
func (e BillingEvent) Title() string {
	switch e.Type {
	case "INITIAL_PURCHASE":
		if e.Trial {
			return "Trial started"
		}
		return "Subscribed"
	case "RENEWAL":
		return "Renewed"
	case "PRODUCT_CHANGE":
		return "Plan changed"
	case "CANCELLATION":
		if e.Refund {
			return "Refunded"
		}
		return "Canceled"
	case "UNCANCELLATION":
		return "Turned renewal back on"
	case "EXPIRATION":
		return "Ended"
	case "BILLING_ISSUE":
		return "Apple couldn't charge the renewal"
	case "REFUND_REVERSED":
		return "Refund reversed"
	case "NON_RENEWING_PURCHASE":
		return "Bought"
	}
	return ""
}

// BillingStore keeps billing events.
type BillingStore interface {
	// Add stores e; a repeated event ID changes nothing.
	Add(ctx context.Context, e BillingEvent) error
	// List returns up to limit events, newest first.
	List(ctx context.Context, householdID string, limit int) ([]BillingEvent, error)
	PurgeHousehold(ctx context.Context, householdID string) error
}

// WithBilling keeps billing history from webhook events. It returns s.
func (s *Service) WithBilling(b BillingStore) *Service {
	s.billing = b
	return s
}

// Stats is what Plus did for a household since it started.
type Stats struct {
	WeeksPlanned, MealsPlanned, CartsSent int
	// SavedCents is the estimated saving against the household's meal kit
	// price; nil when it has none set.
	SavedCents *int64
}

// StatsSource counts a household's activity since a time.
type StatsSource interface {
	PlusStats(ctx context.Context, householdID string, since time.Time) (Stats, error)
}

// WithStats adds the stats strip. It returns s.
func (s *Service) WithStats(src StatsSource) *Service {
	s.stats = src
	return s
}

// FoundingMemberBefore is when the Founding member badge stops being given:
// households that started Plus before it keep it.
var FoundingMemberBefore = time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC)

// BillingHistoryLimit is how many billing events Your Plus lists.
const BillingHistoryLimit = 50

// Details is Your Plus for one member.
type Details struct {
	Status Status
	// Entitlement is the household's subscription; nil if it never had one.
	Entitlement *Entitlement
	// Since is when the household first had Plus; zero if never.
	Since time.Time
	// CanManage is true for the member who started Plus, or any admin when
	// that isn't known. Everyone else sees status only.
	CanManage bool
	Billing   []BillingEvent
	// Invites earned: free months this household got from codes.
	UsedCode    *Referral
	InviteJoins []Referral
	Stats       *Stats
}

// Details returns Your Plus for userID in householdID.
func (s *Service) Details(ctx context.Context, householdID string, m households.Membership) (Details, error) {
	if !s.EnabledFor(householdID) {
		return Details{}, ErrReferralsOff
	}
	st, err := s.Status(ctx, householdID)
	if err != nil {
		return Details{}, err
	}
	d := Details{Status: st}
	e, err := s.store.Get(ctx, householdID)
	switch {
	case err == nil:
		d.Entitlement = &e
		d.Since = e.Since
		d.CanManage = e.StartedBy == m.UserID || (e.StartedBy == "" && m.Role == households.RoleAdmin)
	case errors.Is(err, ErrNotFound):
		d.CanManage = m.Role == households.RoleAdmin
	default:
		return Details{}, err
	}
	if s.billing != nil {
		if d.Billing, err = s.billing.List(ctx, householdID, BillingHistoryLimit); err != nil {
			return Details{}, err
		}
	}
	if s.referrals != nil {
		if used, ok, err := s.referrals.UsedBy(ctx, householdID); err != nil {
			return Details{}, err
		} else if ok {
			d.UsedCode = &used
		}
		if _, d.InviteJoins, err = s.referrals.Joins(ctx, householdID, RecentJoins); err != nil {
			return Details{}, err
		}
	}
	// Plus started at the first of: the subscription, the oldest billing
	// event, or the first free month from a code.
	consider := func(t time.Time) {
		if !t.IsZero() && (d.Since.IsZero() || t.Before(d.Since)) {
			d.Since = t
		}
	}
	if d.Entitlement != nil && d.Since.IsZero() {
		consider(d.Entitlement.LastEventAt)
	}
	for _, b := range d.Billing {
		consider(b.At)
	}
	if d.UsedCode != nil {
		consider(d.UsedCode.At)
	}
	for _, j := range d.InviteJoins {
		if j.ReferrerRewarded {
			consider(j.At)
		}
	}
	if !d.CanManage {
		d.Billing, d.InviteJoins, d.UsedCode = nil, nil, nil
	}
	if s.stats != nil && !d.Since.IsZero() {
		stats, err := s.stats.PlusStats(ctx, householdID, d.Since)
		if err != nil {
			return Details{}, err
		}
		d.Stats = &stats
	}
	return d, nil
}

// billingTypes are the RevenueCat events kept in billing history.
var billingTypes = map[string]bool{
	"INITIAL_PURCHASE": true, "RENEWAL": true, "PRODUCT_CHANGE": true, "CANCELLATION": true,
	"UNCANCELLATION": true, "EXPIRATION": true, "BILLING_ISSUE": true, "REFUND_REVERSED": true,
	"NON_RENEWING_PURCHASE": true,
}

func (s *Service) recordBilling(ctx context.Context, householdID string, ev Event) error {
	if s.billing == nil || !billingTypes[ev.Type] || ev.ID == "" {
		return nil
	}
	b := BillingEvent{
		HouseholdID: householdID, EventID: ev.ID, Type: ev.Type, At: time.UnixMilli(ev.EventTimestampMs).UTC(),
		ProductID: ev.ProductID, Trial: ev.PeriodType == "TRIAL",
		Refund:   ev.Type == "CANCELLATION" && ev.CancelReason == "CUSTOMER_SUPPORT",
		Currency: strings.ToUpper(ev.Currency),
	}
	if ev.Price != nil && *ev.Price > 0 && !b.Trial {
		b.Amount = *ev.Price
	}
	if ev.PurchasedAtMs != nil && ev.Type != "CANCELLATION" && ev.Type != "EXPIRATION" && ev.Type != "BILLING_ISSUE" {
		b.At = time.UnixMilli(*ev.PurchasedAtMs).UTC()
	}
	return s.billing.Add(ctx, b)
}

// BillingCollection holds billing events.
const BillingCollection = "plus_billing_events"

// BillingIndexes returns the indexes MongoBilling relies on.
func BillingIndexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{Collection: BillingCollection, Indexes: []mongo.IndexModel{
		{Keys: bson.D{{Key: "eventId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("eventId_unique")},
		{Keys: bson.D{{Key: "householdId", Value: 1}, {Key: "at", Value: -1}}, Options: options.Index().SetName("householdId_at")},
	}}}
}

// MongoBilling is the MongoDB BillingStore.
type MongoBilling struct{ coll *mongo.Collection }

var _ BillingStore = (*MongoBilling)(nil)

// NewMongoBilling returns a store using db.
func NewMongoBilling(db *mongo.Database) *MongoBilling {
	return &MongoBilling{coll: db.Collection(BillingCollection)}
}

type billingDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	EventID     string        `bson:"eventId"`
	Type        string        `bson:"type"`
	At          time.Time     `bson:"at"`
	ProductID   string        `bson:"productId,omitempty"`
	Trial       bool          `bson:"trial"`
	Refund      bool          `bson:"refund"`
	Amount      float64       `bson:"amount"`
	Currency    string        `bson:"currency,omitempty"`
}

// Add implements BillingStore.
func (s *MongoBilling) Add(ctx context.Context, e BillingEvent) error {
	oid, err := mongodb.ParseID(e.HouseholdID)
	if err != nil {
		return fmt.Errorf("plus: bad household id")
	}
	_, err = s.coll.InsertOne(ctx, billingDoc{
		HouseholdID: oid, EventID: e.EventID, Type: e.Type, At: e.At, ProductID: e.ProductID,
		Trial: e.Trial, Refund: e.Refund, Amount: e.Amount, Currency: e.Currency,
	})
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("add billing event: %w", err)
	}
	return nil
}

// List implements BillingStore.
func (s *MongoBilling) List(ctx context.Context, householdID string, limit int) ([]BillingEvent, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	cur, err := s.coll.Find(ctx, bson.D{{Key: "householdId", Value: oid}},
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list billing events: %w", err)
	}
	var docs []billingDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode billing events: %w", err)
	}
	out := make([]BillingEvent, 0, len(docs))
	for _, d := range docs {
		out = append(out, BillingEvent{
			HouseholdID: d.HouseholdID.Hex(), EventID: d.EventID, Type: d.Type, At: d.At.UTC(), ProductID: d.ProductID,
			Trial: d.Trial, Refund: d.Refund, Amount: d.Amount, Currency: d.Currency,
		})
	}
	return out, nil
}

// PurgeHousehold implements account.HouseholdPurger.
func (s *MongoBilling) PurgeHousehold(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteMany(ctx, bson.D{{Key: "householdId", Value: oid}})
	return err
}
