package main

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
)

// plusStats counts what Plus did for a household from its events and its
// meal kit savings, for Your Plus.
type plusStats struct {
	events   *mongo.Collection
	shopping *shopping.Service
}

func (p plusStats) PlusStats(ctx context.Context, householdID string, since time.Time) (plus.Stats, error) {
	var out plus.Stats
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return out, nil
	}
	filter := func(t events.Type) bson.D {
		return bson.D{{Key: "householdId", Value: oid}, {Key: "type", Value: string(t)},
			{Key: "occurredAt", Value: bson.D{{Key: "$gte", Value: since.UTC()}}}}
	}
	weeks, err := p.events.Distinct(ctx, "week", filter(events.TypeWeekGenerated)).Raw()
	if err != nil {
		return out, fmt.Errorf("count planned weeks: %w", err)
	}
	if vals, err := weeks.Values(); err == nil {
		out.WeeksPlanned = len(vals)
	}
	meals, err := p.events.CountDocuments(ctx, filter(events.TypeRecipePlanned))
	if err != nil {
		return out, fmt.Errorf("count planned meals: %w", err)
	}
	carts, err := p.events.CountDocuments(ctx, filter(events.TypeShoppingHandoffCreated))
	if err != nil {
		return out, fmt.Errorf("count carts: %w", err)
	}
	out.MealsPlanned, out.CartsSent = int(meals), int(carts)
	if p.shopping != nil {
		if s, err := p.shopping.Savings(ctx, householdID, shopping.MaxSavingsWeeks); err == nil {
			out.SavedCents = s.TotalSavedCents
		}
	}
	return out, nil
}

func newPlusStats(db *mongo.Database, s *shopping.Service) plusStats {
	return plusStats{events: db.Collection(events.Collection), shopping: s}
}
