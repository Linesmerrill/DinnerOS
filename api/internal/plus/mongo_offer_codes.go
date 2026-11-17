package plus

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collections for offer code free months (decision 755).
const (
	// OfferCodesCollection is the pool: one document per code Apple made.
	OfferCodesCollection = "plus_offer_codes"
	// FreeMonthsCollection is one document per month a household earned.
	FreeMonthsCollection = "plus_free_months"
	// OfferAlertsCollection remembers whether the run-dry email went out.
	OfferAlertsCollection = "plus_offer_code_alerts"
)

// Code states in the pool.
const (
	codeAvailable = "available"
	codeGiven     = "given"
	codeRedeemed  = "redeemed"
	// codeExpired is a given code that ran out before it was redeemed.
	codeExpired = "expired"
	// codeDeleted was given to a household that was deleted after seeing it.
	codeDeleted = "deleted"
)

// OfferCodeIndexes returns the indexes the offer code store relies on.
func OfferCodeIndexes() []mongodb.IndexSet {
	exists := func(field string) bson.D {
		return bson.D{{Key: field, Value: bson.D{{Key: "$exists", Value: true}}}}
	}
	return []mongodb.IndexSet{
		{Collection: OfferCodesCollection, Indexes: []mongo.IndexModel{
			{Keys: bson.D{{Key: "key", Value: 1}}, Options: options.Index().SetUnique(true).SetName("key_unique")},
			// One code per month, ever.
			{Keys: bson.D{{Key: "monthId", Value: 1}}, Options: options.Index().SetUnique(true).SetName("monthId_unique").
				SetPartialFilterExpression(exists("monthId"))},
			// Taking the soonest-ending available code.
			{Keys: bson.D{{Key: "state", Value: 1}, {Key: "expiresAt", Value: 1}}, Options: options.Index().SetName("state_expiresAt")},
			{Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetName("householdId").
				SetPartialFilterExpression(exists("householdId"))},
		}},
		{Collection: FreeMonthsCollection, Indexes: []mongo.IndexModel{
			{Keys: bson.D{{Key: "key", Value: 1}}, Options: options.Index().SetUnique(true).SetName("key_unique")},
			{Keys: bson.D{{Key: "householdId", Value: 1}, {Key: "earnedAt", Value: 1}}, Options: options.Index().SetName("householdId_earnedAt")},
			{Keys: bson.D{{Key: "state", Value: 1}, {Key: "earnedAt", Value: 1}}, Options: options.Index().SetName("state_earnedAt")},
		}},
	}
}

// MongoOfferCodes is the MongoDB OfferCodeStore.
type MongoOfferCodes struct {
	codes, months, alerts *mongo.Collection
}

var _ OfferCodeStore = (*MongoOfferCodes)(nil)

// NewMongoOfferCodes returns a store using db.
func NewMongoOfferCodes(db *mongo.Database) *MongoOfferCodes {
	return &MongoOfferCodes{
		codes: db.Collection(OfferCodesCollection), months: db.Collection(FreeMonthsCollection),
		alerts: db.Collection(OfferAlertsCollection),
	}
}

type offerCodeDoc struct {
	ID  bson.ObjectID `bson:"_id,omitempty"`
	Key string        `bson:"key"`
	// Code is unset once it's redeemed or ran out; Key stays.
	Code        string         `bson:"code,omitempty"`
	Offer       string         `bson:"offer"`
	Batch       string         `bson:"batch"`
	ExpiresAt   time.Time      `bson:"expiresAt"`
	ImportedAt  time.Time      `bson:"importedAt"`
	State       string         `bson:"state"`
	HouseholdID *bson.ObjectID `bson:"householdId,omitempty"`
	MonthID     *bson.ObjectID `bson:"monthId,omitempty"`
	GivenAt     *time.Time     `bson:"givenAt,omitempty"`
	RevealedAt  *time.Time     `bson:"revealedAt,omitempty"`
	RedeemedAt  *time.Time     `bson:"redeemedAt,omitempty"`
}

type freeMonthDoc struct {
	ID            bson.ObjectID  `bson:"_id,omitempty"`
	Key           string         `bson:"key"`
	HouseholdID   bson.ObjectID  `bson:"householdId"`
	ReferralID    string         `bson:"referralId"`
	Role          string         `bson:"role"`
	EarnedAt      time.Time      `bson:"earnedAt"`
	State         string         `bson:"state"`
	CodeID        *bson.ObjectID `bson:"codeId,omitempty"`
	GivenAt       *time.Time     `bson:"givenAt,omitempty"`
	CodeExpiresAt *time.Time     `bson:"codeExpiresAt,omitempty"`
	RevealedAt    *time.Time     `bson:"revealedAt,omitempty"`
	RedeemedAt    *time.Time     `bson:"redeemedAt,omitempty"`
}

func (d freeMonthDoc) month() FreeMonth {
	return FreeMonth{
		ID: d.ID.Hex(), HouseholdID: d.HouseholdID.Hex(), ReferralID: d.ReferralID, Role: d.Role,
		EarnedAt: d.EarnedAt.UTC(), State: FreeMonthState(d.State),
		GivenAt: timeOrZero(d.GivenAt), CodeExpiresAt: timeOrZero(d.CodeExpiresAt),
		RevealedAt: timeOrZero(d.RevealedAt), RedeemedAt: timeOrZero(d.RedeemedAt),
	}
}

func (s *MongoOfferCodes) findMonths(ctx context.Context, filter bson.D, opts ...options.Lister[options.FindOptions]) ([]FreeMonth, error) {
	cur, err := s.months.Find(ctx, filter, opts...)
	if err != nil {
		return nil, fmt.Errorf("list free months: %w", err)
	}
	var docs []freeMonthDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode free months: %w", err)
	}
	out := make([]FreeMonth, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.month())
	}
	return out, nil
}

// AddMonth implements OfferCodeStore with one upsert on the month's key.
func (s *MongoOfferCodes) AddMonth(ctx context.Context, m FreeMonth) (FreeMonth, error) {
	oid, err := mongodb.ParseID(m.HouseholdID)
	if err != nil {
		return FreeMonth{}, fmt.Errorf("plus: bad household id")
	}
	key := MonthKey(m.ReferralID, m.Role)
	_, err = s.months.UpdateOne(ctx, bson.D{{Key: "key", Value: key}}, bson.D{{Key: "$setOnInsert", Value: bson.D{
		{Key: "householdId", Value: oid}, {Key: "referralId", Value: m.ReferralID}, {Key: "role", Value: m.Role},
		{Key: "earnedAt", Value: m.EarnedAt.UTC()}, {Key: "state", Value: string(MonthOwed)},
	}}}, options.UpdateOne().SetUpsert(true))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return FreeMonth{}, fmt.Errorf("record free month: %w", err)
	}
	var d freeMonthDoc
	if err := s.months.FindOne(ctx, bson.D{{Key: "key", Value: key}}).Decode(&d); err != nil {
		return FreeMonth{}, fmt.Errorf("read free month: %w", err)
	}
	return d.month(), nil
}

// Months implements OfferCodeStore.
func (s *MongoOfferCodes) Months(ctx context.Context, householdID string) ([]FreeMonth, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	return s.findMonths(ctx, bson.D{{Key: "householdId", Value: oid}},
		options.Find().SetSort(bson.D{{Key: "earnedAt", Value: 1}, {Key: "_id", Value: 1}}))
}

// Owed implements OfferCodeStore.
func (s *MongoOfferCodes) Owed(ctx context.Context, limit int) ([]FreeMonth, error) {
	return s.findMonths(ctx, bson.D{{Key: "state", Value: string(MonthOwed)}},
		options.Find().SetSort(bson.D{{Key: "earnedAt", Value: 1}, {Key: "_id", Value: 1}}).SetLimit(int64(limit)))
}

// Give implements OfferCodeStore. The code is taken by one findOneAndUpdate
// on an available code; the unique monthId index means a month racing
// itself keeps the first code, and the loser's update changes nothing.
func (s *MongoOfferCodes) Give(ctx context.Context, monthID string, now, notBefore time.Time) (bool, error) {
	mid, err := mongodb.ParseID(monthID)
	if err != nil {
		return false, ErrFreeMonthNotFound
	}
	for range 3 {
		// A code already given (by a racing call, or one cut off before it
		// marked the month) is the month's.
		var have offerCodeDoc
		err := s.codes.FindOne(ctx, bson.D{{Key: "monthId", Value: mid}}).Decode(&have)
		if err == nil {
			return true, s.markGiven(ctx, mid, have)
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return false, fmt.Errorf("find given code: %w", err)
		}
		var m freeMonthDoc
		err = s.months.FindOne(ctx, bson.D{{Key: "_id", Value: mid}}).Decode(&m)
		if errors.Is(err, mongo.ErrNoDocuments) {
			return false, ErrFreeMonthNotFound
		}
		if err != nil {
			return false, fmt.Errorf("read free month: %w", err)
		}
		if m.State != string(MonthOwed) {
			return true, nil
		}
		hid := m.HouseholdID
		var got offerCodeDoc
		err = s.codes.FindOneAndUpdate(ctx,
			bson.D{{Key: "state", Value: codeAvailable}, {Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: notBefore.UTC()}}}},
			bson.D{{Key: "$set", Value: bson.D{
				{Key: "state", Value: codeGiven}, {Key: "householdId", Value: hid}, {Key: "monthId", Value: mid},
				{Key: "givenAt", Value: now.UTC()},
			}}},
			options.FindOneAndUpdate().SetSort(bson.D{{Key: "expiresAt", Value: 1}, {Key: "_id", Value: 1}}).
				SetReturnDocument(options.After)).Decode(&got)
		switch {
		case errors.Is(err, mongo.ErrNoDocuments):
			return false, nil
		case mongo.IsDuplicateKeyError(err):
			// Another call gave this month a code first: use that one.
			continue
		case err != nil:
			return false, fmt.Errorf("take offer code: %w", err)
		}
		return true, s.markGiven(ctx, mid, got)
	}
	return false, errors.New("plus: could not give a free month code")
}

func (s *MongoOfferCodes) markGiven(ctx context.Context, mid bson.ObjectID, c offerCodeDoc) error {
	given := c.GivenAt
	if given == nil {
		t := time.Now().UTC()
		given = &t
	}
	_, err := s.months.UpdateOne(ctx, bson.D{{Key: "_id", Value: mid}, {Key: "state", Value: string(MonthOwed)}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "state", Value: string(MonthReady)}, {Key: "codeId", Value: c.ID}, {Key: "givenAt", Value: given.UTC()},
			{Key: "codeExpiresAt", Value: c.ExpiresAt.UTC()},
		}}})
	if err != nil {
		return fmt.Errorf("mark free month ready: %w", err)
	}
	return nil
}

// ExpiredGiven implements OfferCodeStore.
func (s *MongoOfferCodes) ExpiredGiven(ctx context.Context, now time.Time, limit int) ([]FreeMonth, error) {
	return s.findMonths(ctx, bson.D{
		{Key: "state", Value: string(MonthReady)}, {Key: "codeExpiresAt", Value: bson.D{{Key: "$lte", Value: now.UTC()}}},
	}, options.Find().SetSort(bson.D{{Key: "earnedAt", Value: 1}}).SetLimit(int64(limit)))
}

// Requeue implements OfferCodeStore. The old code's text is forgotten.
func (s *MongoOfferCodes) Requeue(ctx context.Context, monthID string, now time.Time) error {
	mid, err := mongodb.ParseID(monthID)
	if err != nil {
		return nil
	}
	if _, err := s.codes.UpdateOne(ctx, bson.D{{Key: "monthId", Value: mid}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "state", Value: codeExpired}, {Key: "expiredAt", Value: now.UTC()}}},
		{Key: "$unset", Value: bson.D{{Key: "monthId", Value: ""}, {Key: "code", Value: ""}}},
	}); err != nil {
		return fmt.Errorf("retire expired code: %w", err)
	}
	_, err = s.months.UpdateOne(ctx, bson.D{{Key: "_id", Value: mid}, {Key: "state", Value: string(MonthReady)}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "state", Value: string(MonthOwed)}}},
		{Key: "$unset", Value: bson.D{{Key: "codeId", Value: ""}, {Key: "givenAt", Value: ""}, {Key: "codeExpiresAt", Value: ""}, {Key: "revealedAt", Value: ""}}},
	})
	if err != nil {
		return fmt.Errorf("requeue free month: %w", err)
	}
	return nil
}

// Reveal implements OfferCodeStore: only the household the month is for.
func (s *MongoOfferCodes) Reveal(ctx context.Context, householdID, monthID string, now time.Time) (string, FreeMonth, error) {
	hid, err1 := mongodb.ParseID(householdID)
	mid, err2 := mongodb.ParseID(monthID)
	if err1 != nil || err2 != nil {
		return "", FreeMonth{}, ErrFreeMonthNotFound
	}
	var m freeMonthDoc
	err := s.months.FindOne(ctx, bson.D{{Key: "_id", Value: mid}, {Key: "householdId", Value: hid}}).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", FreeMonth{}, ErrFreeMonthNotFound
	}
	if err != nil {
		return "", FreeMonth{}, fmt.Errorf("read free month: %w", err)
	}
	switch FreeMonthState(m.State) {
	case MonthOwed:
		return "", FreeMonth{}, ErrFreeMonthOwed
	case MonthRedeemed:
		return "", FreeMonth{}, ErrFreeMonthRedeemed
	}
	var c offerCodeDoc
	err = s.codes.FindOne(ctx, bson.D{{Key: "monthId", Value: mid}, {Key: "householdId", Value: hid}}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) || (err == nil && c.Code == "") {
		return "", FreeMonth{}, ErrFreeMonthNotFound
	}
	if err != nil {
		return "", FreeMonth{}, fmt.Errorf("read offer code: %w", err)
	}
	notYet := bson.D{{Key: "revealedAt", Value: bson.D{{Key: "$exists", Value: false}}}}
	set := bson.D{{Key: "$set", Value: bson.D{{Key: "revealedAt", Value: now.UTC()}}}}
	if _, err := s.codes.UpdateOne(ctx, append(bson.D{{Key: "_id", Value: c.ID}}, notYet...), set); err != nil {
		return "", FreeMonth{}, fmt.Errorf("mark code revealed: %w", err)
	}
	if _, err := s.months.UpdateOne(ctx, append(bson.D{{Key: "_id", Value: mid}}, notYet...), set); err != nil {
		return "", FreeMonth{}, fmt.Errorf("mark free month revealed: %w", err)
	}
	out := m.month()
	if out.RevealedAt.IsZero() {
		out.RevealedAt = now.UTC()
	}
	return c.Code, out, nil
}

// MarkRedeemed implements OfferCodeStore.
func (s *MongoOfferCodes) MarkRedeemed(ctx context.Context, householdID string, at time.Time) (bool, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return false, nil
	}
	var m freeMonthDoc
	err = s.months.FindOneAndUpdate(ctx,
		bson.D{{Key: "householdId", Value: hid}, {Key: "state", Value: string(MonthReady)}, {Key: "revealedAt", Value: bson.D{{Key: "$exists", Value: true}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "state", Value: string(MonthRedeemed)}, {Key: "redeemedAt", Value: at.UTC()}}}},
		options.FindOneAndUpdate().SetSort(bson.D{{Key: "revealedAt", Value: 1}, {Key: "_id", Value: 1}})).Decode(&m)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("mark free month redeemed: %w", err)
	}
	_, err = s.codes.UpdateOne(ctx, bson.D{{Key: "monthId", Value: m.ID}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "state", Value: codeRedeemed}, {Key: "redeemedAt", Value: at.UTC()}}},
		{Key: "$unset", Value: bson.D{{Key: "code", Value: ""}}},
	})
	if err != nil {
		return true, fmt.Errorf("mark code redeemed: %w", err)
	}
	return true, nil
}

// Import implements OfferCodeStore with one unordered insert: a code seen
// before, by its key, is a duplicate. It also clears the run-dry alert.
func (s *MongoOfferCodes) Import(ctx context.Context, codes []string, b OfferBatch, now time.Time) (ImportResult, error) {
	var res ImportResult
	if len(codes) == 0 {
		return res, nil
	}
	docs := make([]any, 0, len(codes))
	for _, c := range codes {
		docs = append(docs, offerCodeDoc{
			ID: bson.NewObjectID(), Key: OfferCodeKey(c), Code: c, Offer: b.Offer, Batch: b.Batch,
			ExpiresAt: b.ExpiresAt.UTC(), ImportedAt: now.UTC(), State: codeAvailable,
		})
	}
	_, err := s.codes.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	res.Added = len(codes)
	if err != nil {
		var bulk mongo.BulkWriteException
		if !errors.As(err, &bulk) || bulk.WriteConcernError != nil {
			// The error never carries the codes' text out of here.
			return ImportResult{}, errors.New("plus: import offer codes failed")
		}
		for _, we := range bulk.WriteErrors {
			if we.Code != 11000 {
				return ImportResult{}, errors.New("plus: import offer codes failed")
			}
			res.Duplicates++
		}
		res.Added -= res.Duplicates
	}
	if res.Added > 0 {
		if _, err := s.alerts.UpdateOne(ctx, bson.D{{Key: "_id", Value: "shortage"}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "alerted", Value: false}, {Key: "importedAt", Value: now.UTC()}}}},
			options.UpdateOne().SetUpsert(true)); err != nil {
			return res, fmt.Errorf("reset code alert: %w", err)
		}
	}
	return res, nil
}

// Stock implements OfferCodeStore. Nothing it reads or returns is a code.
func (s *MongoOfferCodes) Stock(ctx context.Context, now time.Time) (OfferStock, error) {
	out := OfferStock{Batches: []BatchStock{}}
	cur, err := s.codes.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{{Key: "offer", Value: "$offer"}, {Key: "batch", Value: "$batch"}, {Key: "expiresAt", Value: "$expiresAt"}, {Key: "state", Value: "$state"}}},
			{Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}},
			{Key: "revealed", Value: bson.D{{Key: "$sum", Value: bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$gt", Value: bson.A{"$revealedAt", nil}}}, 1, 0,
			}}}}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id.expiresAt", Value: 1}, {Key: "_id.batch", Value: 1}}}},
	})
	if err != nil {
		return out, fmt.Errorf("count offer codes: %w", err)
	}
	var groups []struct {
		ID struct {
			Offer     string    `bson:"offer"`
			Batch     string    `bson:"batch"`
			ExpiresAt time.Time `bson:"expiresAt"`
			State     string    `bson:"state"`
		} `bson:"_id"`
		N        int `bson:"n"`
		Revealed int `bson:"revealed"`
	}
	if err := cur.All(ctx, &groups); err != nil {
		return out, fmt.Errorf("decode offer code counts: %w", err)
	}
	index := map[string]int{}
	usable := now.Add(CodeMinLife)
	for _, g := range groups {
		k := g.ID.Offer + "\x00" + g.ID.Batch + "\x00" + g.ID.ExpiresAt.String()
		i, ok := index[k]
		if !ok {
			i = len(out.Batches)
			index[k] = i
			out.Batches = append(out.Batches, BatchStock{Offer: g.ID.Offer, Batch: g.ID.Batch, ExpiresAt: g.ID.ExpiresAt.UTC()})
		}
		b := &out.Batches[i]
		switch g.ID.State {
		case codeAvailable:
			if g.ID.ExpiresAt.After(usable) {
				b.Available += g.N
				out.Available += g.N
			} else {
				out.Expired += g.N
			}
		case codeGiven:
			b.Given += g.N
			out.Given += g.N
			out.Revealed += g.Revealed
		case codeRedeemed:
			b.Redeemed += g.N
			out.Redeemed += g.N
		case codeExpired:
			out.Expired += g.N
		}
	}
	owed, err := s.months.CountDocuments(ctx, bson.D{{Key: "state", Value: string(MonthOwed)}})
	if err != nil {
		return out, fmt.Errorf("count owed months: %w", err)
	}
	out.Owed = int(owed)
	return out, nil
}

// ClaimShortageAlert implements OfferCodeStore: a held claim makes the
// upsert collide on _id.
func (s *MongoOfferCodes) ClaimShortageAlert(ctx context.Context, now time.Time) (bool, error) {
	res, err := s.alerts.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: "shortage"}, {Key: "alerted", Value: bson.D{{Key: "$ne", Value: true}}}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "alerted", Value: true}, {Key: "alertedAt", Value: now.UTC()}}}},
		options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim code alert: %w", err)
	}
	return res.ModifiedCount+res.UpsertedCount > 0, nil
}

// PurgeHousehold implements account.HouseholdPurger. A code the household
// never asked for goes back to the pool for someone else. One it saw is
// deleted: its text and household go, and only its hash stays, so the
// same download can never import it again. Its months are deleted.
func (s *MongoOfferCodes) PurgeHousehold(ctx context.Context, householdID string) error {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	if _, err := s.codes.UpdateMany(ctx, bson.D{
		{Key: "householdId", Value: hid}, {Key: "state", Value: codeGiven}, {Key: "revealedAt", Value: bson.D{{Key: "$exists", Value: false}}},
	}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "state", Value: codeAvailable}}},
		{Key: "$unset", Value: bson.D{{Key: "householdId", Value: ""}, {Key: "monthId", Value: ""}, {Key: "givenAt", Value: ""}}},
	}); err != nil {
		return fmt.Errorf("return offer codes: %w", err)
	}
	if _, err := s.codes.UpdateMany(ctx, bson.D{{Key: "householdId", Value: hid}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "state", Value: codeDeleted}}},
		{Key: "$unset", Value: bson.D{{Key: "householdId", Value: ""}, {Key: "monthId", Value: ""}, {Key: "code", Value: ""}}},
	}); err != nil {
		return fmt.Errorf("delete offer codes: %w", err)
	}
	_, err = s.months.DeleteMany(ctx, bson.D{{Key: "householdId", Value: hid}})
	return err
}
