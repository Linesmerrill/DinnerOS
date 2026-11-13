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

// Collections for free months and referrals.
const (
	FreeTimeCollection      = "plus_free_time"
	ReferralCodesCollection = "plus_referral_codes"
	ReferralsCollection     = "plus_referrals"
)

// ReferralIndexes returns the indexes the referral stores rely on.
func ReferralIndexes() []mongodb.IndexSet {
	unique := func(name string, keys ...string) mongo.IndexModel {
		d := bson.D{}
		for _, k := range keys {
			d = append(d, bson.E{Key: k, Value: 1})
		}
		return mongo.IndexModel{Keys: d, Options: options.Index().SetUnique(true).SetName(name)}
	}
	return []mongodb.IndexSet{
		{Collection: FreeTimeCollection, Indexes: []mongo.IndexModel{unique("householdId_unique", "householdId")}},
		{Collection: ReferralCodesCollection, Indexes: []mongo.IndexModel{
			unique("householdId_unique", "householdId"), unique("code_unique", "code"),
		}},
		{Collection: ReferralsCollection, Indexes: []mongo.IndexModel{
			unique("householdId_unique", "householdId"),
			{Keys: bson.D{{Key: "referrerId", Value: 1}}, Options: options.Index().SetName("referrerId")},
		}},
	}
}

// MongoFreeTime is the MongoDB FreeTimeStore.
type MongoFreeTime struct{ coll *mongo.Collection }

var _ FreeTimeStore = (*MongoFreeTime)(nil)

// NewMongoFreeTime returns a store using db.
func NewMongoFreeTime(db *mongo.Database) *MongoFreeTime {
	return &MongoFreeTime{coll: db.Collection(FreeTimeCollection)}
}

// FreeUntil implements FreeTimeStore.
func (s *MongoFreeTime) FreeUntil(ctx context.Context, householdID string) (time.Time, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return time.Time{}, nil
	}
	var d struct {
		Until time.Time `bson:"until"`
	}
	err = s.coll.FindOne(ctx, bson.D{{Key: "householdId", Value: oid}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("get plus free time: %w", err)
	}
	return d.Until.UTC(), nil
}

// Extend implements FreeTimeStore in one update, so two months granted at
// once both count.
func (s *MongoFreeTime) Extend(ctx context.Context, householdID string, from time.Time, d time.Duration) (time.Time, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return time.Time{}, fmt.Errorf("plus: bad household id")
	}
	update := mongo.Pipeline{{{Key: "$set", Value: bson.D{
		{Key: "until", Value: bson.D{{Key: "$add", Value: bson.A{
			bson.D{{Key: "$max", Value: bson.A{"$until", from.UTC()}}}, d.Milliseconds(),
		}}}},
		{Key: "updatedAt", Value: time.Now().UTC()},
	}}}}
	var out struct {
		Until time.Time `bson:"until"`
	}
	err = s.coll.FindOneAndUpdate(ctx, bson.D{{Key: "householdId", Value: oid}}, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&out)
	if err != nil {
		return time.Time{}, fmt.Errorf("extend plus free time: %w", err)
	}
	return out.Until.UTC(), nil
}

// PurgeHousehold implements account.HouseholdPurger.
func (s *MongoFreeTime) PurgeHousehold(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	_, err = s.coll.DeleteOne(ctx, bson.D{{Key: "householdId", Value: oid}})
	return err
}

// MongoReferrals is the MongoDB ReferralStore.
type MongoReferrals struct {
	codes, referrals *mongo.Collection
}

var _ ReferralStore = (*MongoReferrals)(nil)

// NewMongoReferrals returns a store using db.
func NewMongoReferrals(db *mongo.Database) *MongoReferrals {
	return &MongoReferrals{codes: db.Collection(ReferralCodesCollection), referrals: db.Collection(ReferralsCollection)}
}

type codeDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	Code        string        `bson:"code"`
	CreatedAt   time.Time     `bson:"createdAt"`
}

// Code implements ReferralStore. A new code that collides with another
// household's is drawn again.
func (s *MongoReferrals) Code(ctx context.Context, householdID string, newCode func() string) (string, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return "", fmt.Errorf("plus: bad household id")
	}
	filter := bson.D{{Key: "householdId", Value: oid}}
	for range 5 {
		var d codeDoc
		err := s.codes.FindOne(ctx, filter).Decode(&d)
		if err == nil {
			return d.Code, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return "", fmt.Errorf("get referral code: %w", err)
		}
		_, err = s.codes.InsertOne(ctx, codeDoc{HouseholdID: oid, Code: newCode(), CreatedAt: time.Now().UTC()})
		if err == nil || mongo.IsDuplicateKeyError(err) {
			// Stored, or another request stored one first, or the code was
			// taken: read again.
			continue
		}
		return "", fmt.Errorf("make referral code: %w", err)
	}
	return "", errors.New("plus: could not make a referral code")
}

// Owner implements ReferralStore.
func (s *MongoReferrals) Owner(ctx context.Context, code string) (string, error) {
	var d codeDoc
	err := s.codes.FindOne(ctx, bson.D{{Key: "code", Value: code}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", ErrCodeNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find referral code: %w", err)
	}
	return d.HouseholdID.Hex(), nil
}

type referralDoc struct {
	ReferrerID       bson.ObjectID `bson:"referrerId"`
	ReferredID       bson.ObjectID `bson:"householdId"` // the household that used a code
	At               time.Time     `bson:"at"`
	ReferrerRewarded bool          `bson:"referrerRewarded"`
}

// Record implements ReferralStore.
func (s *MongoReferrals) Record(ctx context.Context, r Referral) error {
	from, err1 := mongodb.ParseID(r.ReferrerID)
	to, err2 := mongodb.ParseID(r.ReferredID)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("plus: bad household id")
	}
	_, err := s.referrals.InsertOne(ctx, referralDoc{ReferrerID: from, ReferredID: to, At: r.At, ReferrerRewarded: r.ReferrerRewarded})
	if mongo.IsDuplicateKeyError(err) {
		return ErrAlreadyRedeemed
	}
	if err != nil {
		return fmt.Errorf("record referral: %w", err)
	}
	return nil
}

// Used implements ReferralStore.
func (s *MongoReferrals) Used(ctx context.Context, householdID string) (bool, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return false, nil
	}
	n, err := s.referrals.CountDocuments(ctx, bson.D{{Key: "householdId", Value: oid}}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("check referral: %w", err)
	}
	return n > 0, nil
}

// Rewarded implements ReferralStore.
func (s *MongoReferrals) Rewarded(ctx context.Context, referrerID string) (int, error) {
	oid, err := mongodb.ParseID(referrerID)
	if err != nil {
		return 0, nil
	}
	n, err := s.referrals.CountDocuments(ctx, bson.D{{Key: "referrerId", Value: oid}, {Key: "referrerRewarded", Value: true}})
	if err != nil {
		return 0, fmt.Errorf("count referrals: %w", err)
	}
	return int(n), nil
}

func (d referralDoc) referral() Referral {
	return Referral{ReferrerID: d.ReferrerID.Hex(), ReferredID: d.ReferredID.Hex(), At: d.At.UTC(), ReferrerRewarded: d.ReferrerRewarded}
}

// Joins implements ReferralStore.
func (s *MongoReferrals) Joins(ctx context.Context, referrerID string, limit int) (int, []Referral, error) {
	oid, err := mongodb.ParseID(referrerID)
	if err != nil {
		return 0, nil, nil
	}
	filter := bson.D{{Key: "referrerId", Value: oid}}
	n, err := s.referrals.CountDocuments(ctx, filter)
	if err != nil {
		return 0, nil, fmt.Errorf("count joins: %w", err)
	}
	if n == 0 {
		return 0, nil, nil
	}
	cur, err := s.referrals.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return 0, nil, fmt.Errorf("list joins: %w", err)
	}
	var docs []referralDoc
	if err := cur.All(ctx, &docs); err != nil {
		return 0, nil, fmt.Errorf("decode joins: %w", err)
	}
	out := make([]Referral, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.referral())
	}
	return int(n), out, nil
}

// UsedBy implements ReferralStore.
func (s *MongoReferrals) UsedBy(ctx context.Context, householdID string) (Referral, bool, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Referral{}, false, nil
	}
	var d referralDoc
	err = s.referrals.FindOne(ctx, bson.D{{Key: "householdId", Value: oid}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Referral{}, false, nil
	}
	if err != nil {
		return Referral{}, false, fmt.Errorf("find used code: %w", err)
	}
	return d.referral(), true, nil
}

// Report implements ReferralStore. Top names come from the households
// collection; a deleted household's referrals are purged with it.
func (s *MongoReferrals) Report(ctx context.Context, since time.Time, top int) (ReferralReport, error) {
	var out ReferralReport
	all, err := s.referrals.CountDocuments(ctx, bson.D{})
	if err != nil {
		return out, fmt.Errorf("count referrals: %w", err)
	}
	recent, err := s.referrals.CountDocuments(ctx, bson.D{{Key: "at", Value: bson.D{{Key: "$gte", Value: since.UTC()}}}})
	if err != nil {
		return out, fmt.Errorf("count recent referrals: %w", err)
	}
	out.Joined, out.JoinedSince = int(all), int(recent)
	cur, err := s.referrals.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$referrerId"}, {Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}}}}},
		{{Key: "$sort", Value: bson.D{{Key: "count", Value: -1}, {Key: "_id", Value: 1}}}},
		{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: "households"}, {Key: "localField", Value: "_id"},
			{Key: "foreignField", Value: "_id"}, {Key: "as", Value: "household"},
		}}},
	})
	if err != nil {
		return out, fmt.Errorf("group referrals: %w", err)
	}
	var rows []struct {
		ID        bson.ObjectID `bson:"_id"`
		Count     int           `bson:"count"`
		Household []struct {
			Name string `bson:"name"`
		} `bson:"household"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return out, fmt.Errorf("decode referral groups: %w", err)
	}
	out.Codes = len(rows)
	for _, r := range rows[:min(top, len(rows))] {
		c := ReferrerCount{HouseholdID: r.ID.Hex(), Count: r.Count}
		if len(r.Household) > 0 {
			c.Name = r.Household[0].Name
		}
		out.Top = append(out.Top, c)
	}
	return out, nil
}

// PurgeHousehold implements account.HouseholdPurger: the household's code
// stops working and its referrals, either way, are forgotten.
func (s *MongoReferrals) PurgeHousehold(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	if _, err := s.codes.DeleteMany(ctx, bson.D{{Key: "householdId", Value: oid}}); err != nil {
		return err
	}
	_, err = s.referrals.DeleteMany(ctx, bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "referrerId", Value: oid}}, bson.D{{Key: "householdId", Value: oid}},
	}}})
	return err
}
