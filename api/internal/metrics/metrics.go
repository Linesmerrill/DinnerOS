// Package metrics reports the numbers that say whether DinnerOS is earning
// its keep (decision 674): weekly active households, carts sent each week,
// and the average cart value per store. It reads the events collection and
// writes nothing.
package metrics

import (
	"context"
	"fmt"
	"slices"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
)

// Row is one event as the report reads it.
type Row struct {
	HouseholdID    string
	Type           string
	OccurredAt     time.Time
	Provider       string
	EstimatedCents int64
	PricedLines    int
}

// Store is one store's carts in a week.
type Store struct {
	Carts int
	// PricedCarts are carts with an estimate; ValueCents sums their
	// estimates.
	PricedCarts int
	ValueCents  int64
}

// AverageCents is the average estimated cart value, or 0 with no priced
// carts.
func (s Store) AverageCents() int64 {
	if s.PricedCarts == 0 {
		return 0
	}
	return s.ValueCents / int64(s.PricedCarts)
}

// Week is one ISO week's numbers.
type Week struct {
	// Week is the ISO week, "2026-W46".
	Week string
	// ActiveHouseholds counts households with any recorded activity.
	ActiveHouseholds int
	// CartsSent counts sends to any store (a send that adds to the week's
	// cart counts again).
	CartsSent int
	Stores    map[string]Store
}

func isoWeek(t time.Time) string {
	y, w := t.UTC().ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

// Summarize groups rows into weeks, oldest first.
func Summarize(rows []Row) []Week {
	byWeek := map[string]*Week{}
	active := map[string]map[string]bool{}
	for _, r := range rows {
		key := isoWeek(r.OccurredAt)
		w, ok := byWeek[key]
		if !ok {
			w = &Week{Week: key, Stores: map[string]Store{}}
			byWeek[key] = w
			active[key] = map[string]bool{}
		}
		if r.HouseholdID != "" {
			active[key][r.HouseholdID] = true
		}
		if r.Type != string(events.TypeShoppingHandoffCreated) {
			continue
		}
		w.CartsSent++
		s := w.Stores[r.Provider]
		s.Carts++
		if r.EstimatedCents > 0 {
			s.PricedCarts++
			s.ValueCents += r.EstimatedCents
		}
		w.Stores[r.Provider] = s
	}
	out := make([]Week, 0, len(byWeek))
	for key, w := range byWeek {
		w.ActiveHouseholds = len(active[key])
		out = append(out, *w)
	}
	slices.SortFunc(out, func(a, b Week) int {
		switch {
		case a.Week < b.Week:
			return -1
		case a.Week > b.Week:
			return 1
		}
		return 0
	})
	return out
}

// Load reads the rows for events that occurred in [from, to).
func Load(ctx context.Context, db *mongo.Database, from, to time.Time) ([]Row, error) {
	filter := bson.D{{Key: "occurredAt", Value: bson.D{{Key: "$gte", Value: from}, {Key: "$lt", Value: to}}}}
	opts := options.Find().SetProjection(bson.D{
		{Key: "householdId", Value: 1}, {Key: "type", Value: 1}, {Key: "occurredAt", Value: 1},
		{Key: "payload.provider", Value: 1}, {Key: "payload.estimatedCents", Value: 1}, {Key: "payload.pricedLines", Value: 1},
	})
	cur, err := db.Collection(events.Collection).Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("read events: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var rows []Row
	for cur.Next(ctx) {
		var d struct {
			HouseholdID bson.ObjectID `bson:"householdId"`
			Type        string        `bson:"type"`
			OccurredAt  time.Time     `bson:"occurredAt"`
			Payload     struct {
				Provider       string `bson:"provider"`
				EstimatedCents int64  `bson:"estimatedCents"`
				PricedLines    int    `bson:"pricedLines"`
			} `bson:"payload"`
		}
		if err := cur.Decode(&d); err != nil {
			return nil, fmt.Errorf("decode event: %w", err)
		}
		r := Row{Type: d.Type, OccurredAt: d.OccurredAt, Provider: d.Payload.Provider,
			EstimatedCents: d.Payload.EstimatedCents, PricedLines: d.Payload.PricedLines}
		if !d.HouseholdID.IsZero() {
			r.HouseholdID = d.HouseholdID.Hex()
		}
		rows = append(rows, r)
	}
	return rows, cur.Err()
}

// Weeks reports the n ISO weeks ending with the one now falls in.
func Weeks(ctx context.Context, db *mongo.Database, now time.Time, n int) ([]Week, error) {
	end := mondayOf(now).AddDate(0, 0, 7)
	start := end.AddDate(0, 0, -7*n)
	rows, err := Load(ctx, db, start, end)
	if err != nil {
		return nil, err
	}
	return Summarize(rows), nil
}

func mondayOf(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(day.Weekday()) + 6) % 7
	return day.AddDate(0, 0, -offset)
}

// Plus is DinnerOS Plus right now, plus referrals each week.
type Plus struct {
	// Paying, Trial and FreeMonths count households with Plus now: paying,
	// in the App Store trial, or on free months from referrals only.
	Paying, Trial, FreeMonths int
	// Referrals counts codes used, per ISO week.
	Referrals map[string]int
}

// PlusNow reads Plus subscriptions, free months, and referrals from from on.
func PlusNow(ctx context.Context, db *mongo.Database, now, from time.Time) (Plus, error) {
	out := Plus{Referrals: map[string]int{}}
	subscribed := map[bson.ObjectID]bool{}
	cur, err := db.Collection(plus.Collection).Find(ctx, bson.D{})
	if err != nil {
		return out, fmt.Errorf("read plus: %w", err)
	}
	for cur.Next(ctx) {
		var d struct {
			HouseholdID bson.ObjectID `bson:"householdId"`
			Trial       bool          `bson:"trial"`
			ExpiresAt   *time.Time    `bson:"expiresAt"`
			Ended       bool          `bson:"ended"`
		}
		if err := cur.Decode(&d); err != nil {
			_ = cur.Close(ctx)
			return out, fmt.Errorf("decode plus: %w", err)
		}
		if d.Ended || (d.ExpiresAt != nil && !now.Before(*d.ExpiresAt)) {
			continue
		}
		subscribed[d.HouseholdID] = true
		if d.Trial {
			out.Trial++
		} else {
			out.Paying++
		}
	}
	_ = cur.Close(ctx)
	if err := cur.Err(); err != nil {
		return out, err
	}

	cur, err = db.Collection(plus.FreeTimeCollection).Find(ctx, bson.D{{Key: "until", Value: bson.D{{Key: "$gt", Value: now}}}})
	if err != nil {
		return out, fmt.Errorf("read free months: %w", err)
	}
	for cur.Next(ctx) {
		var d struct {
			HouseholdID bson.ObjectID `bson:"householdId"`
		}
		if err := cur.Decode(&d); err == nil && !subscribed[d.HouseholdID] {
			out.FreeMonths++
		}
	}
	_ = cur.Close(ctx)

	cur, err = db.Collection(plus.ReferralsCollection).Find(ctx, bson.D{{Key: "at", Value: bson.D{{Key: "$gte", Value: from}}}})
	if err != nil {
		return out, fmt.Errorf("read referrals: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	for cur.Next(ctx) {
		var d struct {
			At time.Time `bson:"at"`
		}
		if err := cur.Decode(&d); err == nil {
			out.Referrals[isoWeek(d.At)]++
		}
	}
	return out, cur.Err()
}
