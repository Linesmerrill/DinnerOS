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

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collections for free months and referrals.
const (
	FreeTimeCollection      = "plus_free_time"
	ReferralCodesCollection = "plus_referral_codes"
	// ReferralCodeKeysCollection keeps every code any household has had,
	// active or retired, unique by lowercase key (decision 699).
	ReferralCodeKeysCollection = "plus_referral_code_keys"
	ReferralsCollection        = "plus_referrals"
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
			{Keys: bson.D{{Key: "codeKey", Value: 1}}, Options: options.Index().SetUnique(true).SetName("codeKey_unique").
				SetPartialFilterExpression(bson.D{{Key: "codeKey", Value: bson.D{{Key: "$exists", Value: true}}}})},
		}},
		{Collection: ReferralCodeKeysCollection, Indexes: []mongo.IndexModel{
			unique("key_unique", "key"),
			{Keys: bson.D{{Key: "householdId", Value: 1}}, Options: options.Index().SetName("householdId")},
		}},
		{Collection: ReferralsCollection, Indexes: []mongo.IndexModel{
			unique("householdId_unique", "householdId"),
			{Keys: bson.D{{Key: "referrerId", Value: 1}}, Options: options.Index().SetName("referrerId")},
			// The sweep's open referrals.
			{Keys: bson.D{{Key: "state", Value: 1}, {Key: "_id", Value: 1}}, Options: options.Index().SetName("state_id")},
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

// MongoReferrals is the MongoDB ReferralStore. A household's current code
// is its one plus_referral_codes document; plus_referral_code_keys holds
// every code ever used, so a code can't be claimed twice, even after it is
// replaced.
type MongoReferrals struct {
	codes, keys, referrals *mongo.Collection
}

var _ ReferralStore = (*MongoReferrals)(nil)

// NewMongoReferrals returns a store using db.
func NewMongoReferrals(db *mongo.Database) *MongoReferrals {
	return &MongoReferrals{
		codes: db.Collection(ReferralCodesCollection), keys: db.Collection(ReferralCodeKeysCollection),
		referrals: db.Collection(ReferralsCollection),
	}
}

type codeDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	// Code is shown as stored: a generated code in capitals, a picked one
	// lowercase. CodeKey is it lowercase; absent before decision 699 until
	// MigrateCodes sets it.
	Code      string    `bson:"code"`
	CodeKey   string    `bson:"codeKey,omitempty"`
	CreatedAt time.Time `bson:"createdAt"`
	// Changes are when the code was changed, the last few.
	Changes []time.Time `bson:"changes,omitempty"`
}

// Code key states.
const (
	codeActive  = "active"
	codeRetired = "retired"
)

type codeKeyDoc struct {
	Key string `bson:"key"`
	// HouseholdID is unset once the household is deleted; the key stays.
	HouseholdID *bson.ObjectID `bson:"householdId,omitempty"`
	Status      string         `bson:"status"`
	CreatedAt   time.Time      `bson:"createdAt"`
	RetiredAt   *time.Time     `bson:"retiredAt,omitempty"`
}

// claimKey stores key as active for oid, or returns ErrCodeTaken.
func (s *MongoReferrals) claimKey(ctx context.Context, key string, oid bson.ObjectID, at time.Time) error {
	_, err := s.keys.InsertOne(ctx, codeKeyDoc{Key: key, HouseholdID: &oid, Status: codeActive, CreatedAt: at.UTC()})
	if mongo.IsDuplicateKeyError(err) {
		return ErrCodeTaken
	}
	if err != nil {
		return fmt.Errorf("claim referral code: %w", err)
	}
	return nil
}

// unclaimKey removes a key claimed by a write that then lost a race. It was
// never anyone's code, so it is free again.
func (s *MongoReferrals) unclaimKey(ctx context.Context, key string, oid bson.ObjectID) {
	_, _ = s.keys.DeleteOne(ctx, bson.D{{Key: "key", Value: key}, {Key: "householdId", Value: oid}, {Key: "status", Value: codeActive}})
}

// keepKey records key as oid's active code if it isn't recorded yet.
func (s *MongoReferrals) keepKey(ctx context.Context, key string, oid bson.ObjectID, at time.Time) error {
	_, err := s.keys.UpdateOne(ctx, bson.D{{Key: "key", Value: key}}, bson.D{{Key: "$setOnInsert", Value: bson.D{
		{Key: "householdId", Value: oid}, {Key: "status", Value: codeActive}, {Key: "createdAt", Value: at.UTC()},
	}}}, options.UpdateOne().SetUpsert(true))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return fmt.Errorf("keep referral code: %w", err)
	}
	return nil
}

// Code implements ReferralStore. A new code that collides with any code
// ever used is drawn again.
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
		code, now := newCode(), time.Now().UTC()
		key := CodeKey(code)
		if err := s.claimKey(ctx, key, oid, now); errors.Is(err, ErrCodeTaken) {
			continue
		} else if err != nil {
			return "", err
		}
		_, err = s.codes.InsertOne(ctx, codeDoc{HouseholdID: oid, Code: code, CodeKey: key, CreatedAt: now})
		if err == nil {
			return code, nil
		}
		s.unclaimKey(ctx, key, oid)
		if mongo.IsDuplicateKeyError(err) {
			// Another request stored one first: read again.
			continue
		}
		return "", fmt.Errorf("make referral code: %w", err)
	}
	return "", errors.New("plus: could not make a referral code")
}

// Owner implements ReferralStore. A code from before decision 699 that
// MigrateCodes hasn't reached yet is found by its capitals. A deleted
// household's code is not found, not replaced.
func (s *MongoReferrals) Owner(ctx context.Context, code string) (string, error) {
	key := CodeKey(code)
	var d codeDoc
	err := s.codes.FindOne(ctx, bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "codeKey", Value: key}},
		bson.D{{Key: "code", Value: strings.ToUpper(key)}, {Key: "codeKey", Value: bson.D{{Key: "$exists", Value: false}}}},
	}}}).Decode(&d)
	if err == nil {
		return d.HouseholdID.Hex(), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return "", fmt.Errorf("find referral code: %w", err)
	}
	// A deleted household's codes are retired with no household: those are
	// just not found.
	n, err := s.keys.CountDocuments(ctx, bson.D{
		{Key: "key", Value: key}, {Key: "status", Value: codeRetired}, {Key: "householdId", Value: bson.D{{Key: "$exists", Value: true}}},
	}, options.Count().SetLimit(1))
	if err != nil {
		return "", fmt.Errorf("find retired referral code: %w", err)
	}
	if n > 0 {
		return "", ErrCodeRetired
	}
	return "", ErrCodeNotFound
}

// Taken implements ReferralStore.
func (s *MongoReferrals) Taken(ctx context.Context, code string) (bool, error) {
	key := CodeKey(code)
	n, err := s.keys.CountDocuments(ctx, bson.D{{Key: "key", Value: key}}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("check referral code: %w", err)
	}
	if n > 0 {
		return true, nil
	}
	n, err = s.codes.CountDocuments(ctx, bson.D{{Key: "code", Value: strings.ToUpper(key)}}, options.Count().SetLimit(1))
	if err != nil {
		return false, fmt.Errorf("check referral code: %w", err)
	}
	return n > 0, nil
}

// CodeChanges implements ReferralStore.
func (s *MongoReferrals) CodeChanges(ctx context.Context, householdID string) ([]time.Time, error) {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil, nil
	}
	var d codeDoc
	err = s.codes.FindOne(ctx, bson.D{{Key: "householdId", Value: oid}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get referral code changes: %w", err)
	}
	return d.Changes, nil
}

// keptChanges is how many change times a code document keeps.
const keptChanges = 2 * CodeChangeLimit

// Replace implements ReferralStore. The new key is claimed first, so two
// households can't both get it; the swap is conditional on old, so two
// changes at once can't both pass one limit check.
func (s *MongoReferrals) Replace(ctx context.Context, householdID, old, code string, at time.Time) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return fmt.Errorf("plus: bad household id")
	}
	key := CodeKey(code)
	if err := s.claimKey(ctx, key, oid, at); err != nil {
		return err
	}
	res, err := s.codes.UpdateOne(ctx, bson.D{{Key: "householdId", Value: oid}, {Key: "code", Value: old}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "code", Value: code}, {Key: "codeKey", Value: key}}},
		{Key: "$push", Value: bson.D{{Key: "changes", Value: bson.D{
			{Key: "$each", Value: bson.A{at.UTC()}}, {Key: "$slice", Value: -keptChanges},
		}}}},
	})
	if err != nil {
		s.unclaimKey(ctx, key, oid)
		return fmt.Errorf("replace referral code: %w", err)
	}
	if res.MatchedCount == 0 {
		s.unclaimKey(ctx, key, oid)
		return ErrCodeChangedNow
	}
	retired := at.UTC()
	_, err = s.keys.UpdateOne(ctx, bson.D{{Key: "key", Value: CodeKey(old)}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "status", Value: codeRetired}, {Key: "retiredAt", Value: retired}}},
		{Key: "$setOnInsert", Value: bson.D{{Key: "householdId", Value: oid}, {Key: "createdAt", Value: retired}}},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		// The old code already stopped working; only its record is behind.
		return fmt.Errorf("retire referral code: %w", err)
	}
	return nil
}

// MigrateCodes records every code from before decision 699 as an active
// key and sets its codeKey. It is idempotent and returns how many it moved.
func (s *MongoReferrals) MigrateCodes(ctx context.Context) (int, error) {
	cur, err := s.codes.Find(ctx, bson.D{{Key: "codeKey", Value: bson.D{{Key: "$exists", Value: false}}}})
	if err != nil {
		return 0, fmt.Errorf("list referral codes: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	n := 0
	for cur.Next(ctx) {
		var d codeDoc
		if err := cur.Decode(&d); err != nil {
			return n, fmt.Errorf("decode referral code: %w", err)
		}
		key := CodeKey(d.Code)
		created := d.CreatedAt
		if created.IsZero() {
			created = time.Now()
		}
		if err := s.keepKey(ctx, key, d.HouseholdID, created); err != nil {
			return n, err
		}
		res, err := s.codes.UpdateOne(ctx,
			bson.D{{Key: "householdId", Value: d.HouseholdID}, {Key: "codeKey", Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{Key: "$set", Value: bson.D{{Key: "codeKey", Value: key}}}})
		if err != nil {
			return n, fmt.Errorf("migrate referral code: %w", err)
		}
		n += int(res.ModifiedCount)
	}
	return n, cur.Err()
}

type referralDoc struct {
	ID               bson.ObjectID `bson:"_id,omitempty"`
	ReferrerID       bson.ObjectID `bson:"referrerId"`
	ReferredID       bson.ObjectID `bson:"householdId"` // the household that used a code
	At               time.Time     `bson:"at"`
	ReferrerRewarded bool          `bson:"referrerRewarded"`
	// State is absent on referrals from before decision 698, which were
	// paid when the code was entered: they read as qualified.
	State       string     `bson:"state,omitempty"`
	ExpiresAt   *time.Time `bson:"expiresAt,omitempty"`
	SetupMetAt  *time.Time `bson:"setupMetAt,omitempty"`
	QualifiedAt *time.Time `bson:"qualifiedAt,omitempty"`
	Checks      string     `bson:"checks,omitempty"`
	Settled     bool       `bson:"settled,omitempty"`
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func (d referralDoc) referral() Referral {
	r := Referral{
		ID: d.ID.Hex(), ReferrerID: d.ReferrerID.Hex(), ReferredID: d.ReferredID.Hex(), At: d.At.UTC(),
		ReferrerRewarded: d.ReferrerRewarded, State: ReferralState(d.State),
		ExpiresAt: timeOrZero(d.ExpiresAt), SetupMetAt: timeOrZero(d.SetupMetAt), QualifiedAt: timeOrZero(d.QualifiedAt),
		Checks: Checks(d.Checks), Settled: d.Settled,
	}
	if r.State == "" {
		r.State, r.Settled = StateQualified, true
	}
	return r
}

// Record implements ReferralStore.
func (s *MongoReferrals) Record(ctx context.Context, r *Referral) error {
	from, err1 := mongodb.ParseID(r.ReferrerID)
	to, err2 := mongodb.ParseID(r.ReferredID)
	if err1 != nil || err2 != nil {
		return fmt.Errorf("plus: bad household id")
	}
	state := r.State
	if state == "" {
		state = StatePending
	}
	d := referralDoc{ID: bson.NewObjectID(), ReferrerID: from, ReferredID: to, At: r.At.UTC(), ReferrerRewarded: r.ReferrerRewarded, State: string(state)}
	if !r.ExpiresAt.IsZero() {
		t := r.ExpiresAt.UTC()
		d.ExpiresAt = &t
	}
	_, err := s.referrals.InsertOne(ctx, d)
	if mongo.IsDuplicateKeyError(err) {
		return ErrAlreadyRedeemed
	}
	if err != nil {
		return fmt.Errorf("record referral: %w", err)
	}
	r.ID, r.State = d.ID.Hex(), state
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

// qualifiedFilter matches qualified referrals, including those from before
// decision 698 that the migration hasn't marked yet.
var qualifiedFilter = bson.D{{Key: "$or", Value: bson.A{
	bson.D{{Key: "state", Value: string(StateQualified)}},
	bson.D{{Key: "state", Value: bson.D{{Key: "$exists", Value: false}}}},
}}}

// Rewarded implements ReferralStore.
func (s *MongoReferrals) Rewarded(ctx context.Context, referrerID string) (int, error) {
	oid, err := mongodb.ParseID(referrerID)
	if err != nil {
		return 0, nil
	}
	n, err := s.referrals.CountDocuments(ctx, bson.D{
		{Key: "referrerId", Value: oid}, {Key: "referrerRewarded", Value: true}, {Key: "$and", Value: bson.A{qualifiedFilter}},
	})
	if err != nil {
		return 0, fmt.Errorf("count referrals: %w", err)
	}
	return int(n), nil
}

// QualifiedSince implements ReferralStore. A referral from before decision
// 698 qualified when its code was entered.
func (s *MongoReferrals) QualifiedSince(ctx context.Context, referrerID string, since time.Time) (int, error) {
	oid, err := mongodb.ParseID(referrerID)
	if err != nil {
		return 0, nil
	}
	n, err := s.referrals.CountDocuments(ctx, bson.D{
		{Key: "referrerId", Value: oid},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "state", Value: string(StateQualified)}, {Key: "qualifiedAt", Value: bson.D{{Key: "$gte", Value: since.UTC()}}}},
			bson.D{{Key: "state", Value: bson.D{{Key: "$exists", Value: false}}}, {Key: "at", Value: bson.D{{Key: "$gte", Value: since.UTC()}}}},
		}},
	})
	if err != nil {
		return 0, fmt.Errorf("count recent referrals: %w", err)
	}
	return int(n), nil
}

// countByState groups the referrals filter matches by state.
func (s *MongoReferrals) countByState(ctx context.Context, filter bson.D) (ReferralCounts, error) {
	var counts ReferralCounts
	cur, err := s.referrals.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		{{Key: "$group", Value: bson.D{{Key: "_id", Value: "$state"}, {Key: "n", Value: bson.D{{Key: "$sum", Value: 1}}}}}},
	})
	if err != nil {
		return counts, fmt.Errorf("count referrals by state: %w", err)
	}
	var groups []struct {
		State *string `bson:"_id"`
		N     int     `bson:"n"`
	}
	if err := cur.All(ctx, &groups); err != nil {
		return counts, fmt.Errorf("decode referral states: %w", err)
	}
	for _, g := range groups {
		state := StateQualified
		if g.State != nil && *g.State != "" {
			state = ReferralState(*g.State)
		}
		counts.add(state, g.N)
	}
	return counts, nil
}

// Joins implements ReferralStore.
func (s *MongoReferrals) Joins(ctx context.Context, referrerID string, limit int) (ReferralCounts, []Referral, error) {
	oid, err := mongodb.ParseID(referrerID)
	if err != nil {
		return ReferralCounts{}, nil, nil
	}
	filter := bson.D{{Key: "referrerId", Value: oid}}
	counts, err := s.countByState(ctx, filter)
	if err != nil || counts.Total() == 0 {
		return counts, nil, err
	}
	found, err := s.referrals.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return counts, nil, fmt.Errorf("list joins: %w", err)
	}
	var docs []referralDoc
	if err := found.All(ctx, &docs); err != nil {
		return counts, nil, fmt.Errorf("decode joins: %w", err)
	}
	out := make([]Referral, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.referral())
	}
	return counts, out, nil
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

// Open implements ReferralStore.
func (s *MongoReferrals) Open(ctx context.Context, after string, limit int) ([]Referral, error) {
	filter := bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "state", Value: bson.D{{Key: "$in", Value: bson.A{string(StatePending), string(StateReady)}}}}},
		bson.D{{Key: "state", Value: string(StateQualified)}, {Key: FlagSettled, Value: bson.D{{Key: "$ne", Value: true}}}},
	}}}
	if after != "" {
		oid, err := mongodb.ParseID(after)
		if err != nil {
			return nil, fmt.Errorf("plus: bad referral id")
		}
		filter = append(filter, bson.E{Key: "_id", Value: bson.D{{Key: "$gt", Value: oid}}})
	}
	cur, err := s.referrals.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, fmt.Errorf("list open referrals: %w", err)
	}
	var docs []referralDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode open referrals: %w", err)
	}
	out := make([]Referral, 0, len(docs))
	for _, d := range docs {
		out = append(out, d.referral())
	}
	return out, nil
}

// update sets set on referredID's referral if it also matches filter, and
// reports whether it changed.
func (s *MongoReferrals) update(ctx context.Context, referredID string, filter, set bson.D) (bool, error) {
	oid, err := mongodb.ParseID(referredID)
	if err != nil {
		return false, nil
	}
	res, err := s.referrals.UpdateOne(ctx, append(bson.D{{Key: "householdId", Value: oid}}, filter...), bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return false, fmt.Errorf("update referral: %w", err)
	}
	return res.ModifiedCount > 0, nil
}

// SetSetupMet implements ReferralStore.
func (s *MongoReferrals) SetSetupMet(ctx context.Context, referredID string, at time.Time) error {
	_, err := s.update(ctx, referredID, bson.D{{Key: "setupMetAt", Value: bson.D{{Key: "$exists", Value: false}}}},
		bson.D{{Key: "setupMetAt", Value: at.UTC()}})
	return err
}

// SetChecks implements ReferralStore.
func (s *MongoReferrals) SetChecks(ctx context.Context, referredID string, c Checks) error {
	_, err := s.update(ctx, referredID, nil, bson.D{{Key: "checks", Value: string(c)}})
	return err
}

func stateList(states []ReferralState) bson.A {
	out := bson.A{}
	for _, st := range states {
		out = append(out, string(st))
	}
	return out
}

// Move implements ReferralStore.
func (s *MongoReferrals) Move(ctx context.Context, referredID string, from []ReferralState, to ReferralState) (bool, error) {
	return s.update(ctx, referredID, bson.D{{Key: "state", Value: bson.D{{Key: "$in", Value: stateList(from)}}}},
		bson.D{{Key: "state", Value: string(to)}})
}

// Qualify implements ReferralStore.
func (s *MongoReferrals) Qualify(ctx context.Context, referredID string, at time.Time, rewarded bool) (bool, error) {
	return s.update(ctx, referredID,
		bson.D{{Key: "state", Value: bson.D{{Key: "$in", Value: bson.A{string(StatePending), string(StateReady)}}}}},
		bson.D{{Key: "state", Value: string(StateQualified)}, {Key: "qualifiedAt", Value: at.UTC()}, {Key: "referrerRewarded", Value: rewarded}})
}

// Claim implements ReferralStore.
func (s *MongoReferrals) Claim(ctx context.Context, referredID, flag string) (bool, error) {
	switch flag {
	case FlagReferredGranted, FlagReferrerGranted, FlagSettled:
	default:
		return false, fmt.Errorf("plus: unknown referral flag %q", flag)
	}
	return s.update(ctx, referredID, bson.D{{Key: flag, Value: bson.D{{Key: "$ne", Value: true}}}}, bson.D{{Key: flag, Value: true}})
}

// MigrateReferrals marks referrals from before decision 698 qualified: they
// were paid when the code was entered. It is idempotent and returns how many
// it marked.
func (s *MongoReferrals) MigrateReferrals(ctx context.Context) (int, error) {
	res, err := s.referrals.UpdateMany(ctx, bson.D{{Key: "state", Value: bson.D{{Key: "$exists", Value: false}}}}, mongo.Pipeline{
		{{Key: "$set", Value: bson.D{
			{Key: "state", Value: string(StateQualified)},
			{Key: "qualifiedAt", Value: "$at"},
			{Key: "checks", Value: string(ChecksPassed)},
			{Key: FlagReferredGranted, Value: true},
			{Key: FlagReferrerGranted, Value: "$referrerRewarded"},
			{Key: FlagSettled, Value: true},
		}}},
	})
	if err != nil {
		return 0, fmt.Errorf("migrate referrals: %w", err)
	}
	return int(res.ModifiedCount), nil
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
	if out.Counts, err = s.countByState(ctx, bson.D{}); err != nil {
		return out, err
	}
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
// stops working for good and its referrals, either way, are forgotten.
func (s *MongoReferrals) PurgeHousehold(ctx context.Context, householdID string) error {
	oid, err := mongodb.ParseID(householdID)
	if err != nil {
		return nil
	}
	if _, err := s.codes.DeleteMany(ctx, bson.D{{Key: "householdId", Value: oid}}); err != nil {
		return err
	}
	// Its codes stay retired, with no household, so no one can claim them.
	if _, err := s.keys.UpdateMany(ctx, bson.D{{Key: "householdId", Value: oid}}, bson.D{
		{Key: "$set", Value: bson.D{{Key: "status", Value: codeRetired}, {Key: "retiredAt", Value: time.Now().UTC()}}},
		{Key: "$unset", Value: bson.D{{Key: "householdId", Value: ""}}},
	}); err != nil {
		return err
	}
	_, err = s.referrals.DeleteMany(ctx, bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: "referrerId", Value: oid}}, bson.D{{Key: "householdId", Value: oid}},
	}}})
	return err
}
