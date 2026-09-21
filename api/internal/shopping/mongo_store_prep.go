package shopping

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// This file is the shopping_prep_cards half of MongoStore: the one stored
// fact about a prep session, which is what the household answered
// (prep.go). Which lines are bulk packs, what the week needs, and how many
// portions to suggest are all derived on read, so there is nothing here to
// drift out of step with the handoff or the plan.

// PrepCardsCollection holds each prep card's answer.
const PrepCardsCollection = "shopping_prep_cards"

type prepCardDoc struct {
	ID           bson.ObjectID  `bson:"_id"`
	HouseholdID  bson.ObjectID  `bson:"householdId"`
	Week         string         `bson:"week"`
	CardID       string         `bson:"cardId"`
	Status       string         `bson:"status"`
	Portions     int            `bson:"portions,omitempty"`
	FrozenItemID *bson.ObjectID `bson:"frozenItemId,omitempty"`
	AnsweredBy   bson.ObjectID  `bson:"answeredBy"`
	AnsweredAt   time.Time      `bson:"answeredAt"`
}

func (d prepCardDoc) toState() PrepCardState {
	st := PrepCardState{
		HouseholdID: d.HouseholdID.Hex(), Week: d.Week, CardID: d.CardID, Status: PrepStatus(d.Status),
		Portions: d.Portions, AnsweredBy: hexOrEmpty(d.AnsweredBy), AnsweredAt: d.AnsweredAt.UTC(),
	}
	if d.FrozenItemID != nil {
		st.FrozenItemID = d.FrozenItemID.Hex()
	}
	return st
}

// ListPrepCardStates implements Store.
func (s *MongoStore) ListPrepCardStates(ctx context.Context, householdID, week string) ([]PrepCardState, error) {
	hid, err := householdOID(householdID)
	if err != nil {
		return nil, err
	}
	cur, err := s.prepCards.Find(ctx, bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: week}})
	if err != nil {
		return nil, translate(err)
	}
	var docs []prepCardDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]PrepCardState, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.toState())
	}
	return out, nil
}

// PutPrepCardState implements Store. Answering the same card again replaces
// the answer rather than adding a second one, which is what makes the
// checklist resumable and a double tap harmless.
func (s *MongoStore) PutPrepCardState(ctx context.Context, st PrepCardState) error {
	hid, err := householdOID(st.HouseholdID)
	if err != nil {
		return err
	}
	uid, err := userOID(st.AnsweredBy)
	if err != nil {
		return err
	}
	set := bson.D{
		{Key: "status", Value: string(st.Status)},
		{Key: "portions", Value: st.Portions},
		{Key: "answeredBy", Value: uid},
		{Key: "answeredAt", Value: st.AnsweredAt},
	}
	unset := bson.D{}
	if st.FrozenItemID != "" {
		oid, err := bson.ObjectIDFromHex(st.FrozenItemID)
		if err != nil {
			return invalid("frozenItemId must be an object ID")
		}
		set = append(set, bson.E{Key: "frozenItemId", Value: oid})
	} else {
		unset = append(unset, bson.E{Key: "frozenItemId", Value: ""})
	}
	update := bson.D{
		{Key: "$set", Value: set},
		{Key: "$setOnInsert", Value: bson.D{{Key: "_id", Value: bson.NewObjectID()}}},
	}
	if len(unset) > 0 {
		update = append(update, bson.E{Key: "$unset", Value: unset})
	}
	_, err = s.prepCards.UpdateOne(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "week", Value: st.Week}, {Key: "cardId", Value: st.CardID}},
		update, options.UpdateOne().SetUpsert(true))
	return translate(err)
}
