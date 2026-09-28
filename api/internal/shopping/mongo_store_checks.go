package shopping

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// checkDoc is a saved product's latest reported check (ProductCheckState).
type checkDoc struct {
	ProductID  string    `bson:"productId"`
	Status     string    `bson:"status"`
	CheckedAt  time.Time `bson:"checkedAt"`
	Name       string    `bson:"name,omitempty"`
	PriceCents *int64    `bson:"priceCents,omitempty"`
}

func newCheckDoc(c *ProductCheckState) *checkDoc {
	if c == nil {
		return nil
	}
	return &checkDoc{ProductID: c.ProductID, Status: string(c.Status), CheckedAt: c.CheckedAt, Name: c.Name, PriceCents: c.PriceCents}
}

func (d *checkDoc) state() *ProductCheckState {
	if d == nil {
		return nil
	}
	return &ProductCheckState{
		ProductID: d.ProductID, Status: ProductStatus(d.Status), CheckedAt: d.CheckedAt.UTC(), Name: d.Name, PriceCents: d.PriceCents,
	}
}

// lineCheckDoc is the check a handoff line or exclusion went out with.
type lineCheckDoc struct {
	Status    string    `bson:"status"`
	CheckedAt time.Time `bson:"checkedAt"`
	Decision  string    `bson:"decision,omitempty"`
	Saved     bool      `bson:"saved,omitempty"`
}

func newLineCheckDoc(c *LineCheck) *lineCheckDoc {
	if c == nil {
		return nil
	}
	return &lineCheckDoc{Status: string(c.Status), CheckedAt: c.CheckedAt, Decision: string(c.Decision), Saved: c.Saved}
}

func (d *lineCheckDoc) check() *LineCheck {
	if d == nil {
		return nil
	}
	return &LineCheck{Status: ProductStatus(d.Status), CheckedAt: d.CheckedAt.UTC(), Decision: CheckDecision(d.Decision), Saved: d.Saved}
}

// SaveProductCheck implements Store.
func (s *MongoStore) SaveProductCheck(ctx context.Context, p Preference, priceChanged bool) (bool, error) {
	id, err := mongodb.ParseID(p.ID)
	if err != nil {
		return false, ErrNotFound
	}
	set := bson.D{{Key: "check", Value: newCheckDoc(p.Check)}}
	if priceChanged && p.PriceCents != nil {
		set = append(set,
			bson.E{Key: "priceCents", Value: *p.PriceCents}, bson.E{Key: "priceSource", Value: string(p.PriceSource)},
			bson.E{Key: "priceUpdatedAt", Value: p.PriceUpdatedAt})
	}
	// Matching the product ID too means a result for a product the member
	// replaced meanwhile is dropped rather than pinned on the new one.
	res, err := s.preferences.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}, {Key: "productId", Value: p.ProductID}}, bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return false, translate(err)
	}
	return res.MatchedCount > 0, nil
}
