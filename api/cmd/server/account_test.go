package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/invitations"
	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
	"github.com/Linesmerrill/DinnerOS/api/internal/skips"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// How account deletion treats each collection. Every collection with indexes
// must be listed, so a new module's collection fails the test until someone
// decides what deleting an account does to it (and wires it into
// newAccountService).
type collectionKind int

const (
	householdData    collectionKind = iota // deleted with an emptied household
	userRatings                            // household data, and the user's own rows are deleted
	userHistory                            // household data; the user's ID is removed, rows stay
	userOnly                               // keyed by userId only; deleted with the user
	householdsModule                       // households and memberships, checked separately
	global                                 // shared catalogs; untouched
)

var accountDeletionKinds = map[string]collectionKind{
	households.HouseholdsCollection:        householdsModule,
	households.MembershipsCollection:       householdsModule,
	users.IdentitiesCollection:             userOnly,
	auth.SessionsCollection:                userOnly,
	push.Collection:                        userOnly,
	invitations.InvitationsCollection:      householdData,
	recipes.RecipesCollection:              householdData,
	recipes.ImportReviewsCollection:        householdData,
	recipes.IngredientsCollection:          global,
	planning.PlansCollection:               householdData,
	pantry.ItemsCollection:                 householdData,
	pantry.PurchasesCollection:             householdData,
	pantry.SettingsCollection:              householdData,
	pantry.CookUsageCollection:             userHistory,
	notifications.Collection:               householdData,
	substitutes.SpecialtiesCollection:      global,
	substitutes.OptionsCollection:          householdData,
	substitutes.ChoicesCollection:          householdData,
	substitutes.SettingsCollection:         householdData,
	shopping.SettingsCollection:            householdData,
	shopping.PreferencesCollection:         householdData,
	shopping.HandoffsCollection:            householdData,
	shopping.StoreRequestsCollection:       householdData,
	shopping.OrderWeeksCollection:          householdData,
	shopping.WeekSpendCollection:           householdData,
	skips.Collection:                       householdData,
	ratings.Collection:                     userRatings,
	events.Collection:                      userHistory,
	recommendations.ProfilesCollection:     householdData,
	recommendations.WeekContextsCollection: householdData,
	recommendations.ProposalsCollection:    householdData,
	recommendations.OverridesCollection:    householdData,
	recommendations.WeekPairingsCollection: householdData,
	// A meal-kit link holds the member's own encrypted tokens, so it goes
	// with them as well as with an emptied household.
	mealkit.LinkCollection: userRatings,
	// Import runs are household history. A run whose link went with a
	// deleted account finds no tokens and stops itself.
	mealkit.JobCollection: householdData,
}

// seedDoc fills every plain indexed field with a unique value, so documents
// satisfy unique indexes whatever their keys are, then sets the ID fields.
func seedDoc(keys []string, n int, ids bson.D) bson.D {
	doc := bson.D{}
	for _, k := range keys {
		if k == "householdId" || k == "userId" || k == "_id" || strings.ContainsAny(k, ".$") {
			continue
		}
		doc = append(doc, bson.E{Key: k, Value: fmt.Sprintf("seed-%d-%s", n, k)})
	}
	return append(doc, ids...)
}

func TestIntegrationAccountDeletion(t *testing.T) {
	client := mongotest.Client(t)
	db := client.Database()
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, indexSets()...); err != nil {
		t.Fatal(err)
	}

	userStore := users.NewMongoStore(db)
	now := time.Now().UTC()
	ada, err := userStore.CreateUser(ctx, users.User{DisplayName: "Ada", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	bob, err := userStore.CreateUser(ctx, users.User{DisplayName: "Bob", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	householdService := households.NewService(households.ServiceOptions{Store: households.NewMongoStore(db), Users: userStore})
	solo, _, err := householdService.Create(ctx, ada.ID, households.CreateInput{Name: "Ada's", TimeZone: "America/Denver"})
	if err != nil {
		t.Fatal(err)
	}
	shared, _, err := householdService.Create(ctx, ada.ID, households.CreateInput{Name: "Shared", TimeZone: "America/Denver"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := householdService.AddMember(ctx, shared.ID, bob.ID, households.RoleMember); err != nil {
		t.Fatal(err)
	}

	oid := func(hex string) bson.ObjectID {
		id, err := bson.ObjectIDFromHex(hex)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	soloID, sharedID, adaID, bobID := oid(solo.ID), oid(shared.ID), oid(ada.ID), oid(bob.ID)

	// Seed every indexed collection by kind.
	keysByCollection := map[string][]string{}
	for _, set := range indexSets() {
		for _, idx := range set.Indexes {
			keys, _ := idx.Keys.(bson.D)
			for _, k := range keys {
				keysByCollection[set.Collection] = append(keysByCollection[set.Collection], k.Key)
			}
		}
	}
	n := 0
	insert := func(coll string, ids bson.D) {
		n++
		if _, err := db.Collection(coll).InsertOne(ctx, seedDoc(keysByCollection[coll], n, ids)); err != nil {
			t.Fatalf("seed %s: %v", coll, err)
		}
	}
	for coll := range keysByCollection {
		kind, ok := accountDeletionKinds[coll]
		if !ok {
			t.Errorf("collection %q is not classified for account deletion; add it to accountDeletionKinds and newAccountService", coll)
			continue
		}
		switch kind {
		case householdData:
			insert(coll, bson.D{{Key: "householdId", Value: soloID}})
			insert(coll, bson.D{{Key: "householdId", Value: sharedID}})
		case userRatings, userHistory:
			insert(coll, bson.D{{Key: "householdId", Value: soloID}, {Key: "userId", Value: adaID}})
			insert(coll, bson.D{{Key: "householdId", Value: sharedID}, {Key: "userId", Value: adaID}})
			insert(coll, bson.D{{Key: "householdId", Value: sharedID}, {Key: "userId", Value: bobID}})
		case userOnly:
			insert(coll, bson.D{{Key: "userId", Value: adaID}})
			insert(coll, bson.D{{Key: "userId", Value: bobID}})
		}
	}
	// A notification both members read.
	if _, err := db.Collection(notifications.Collection).InsertOne(ctx, bson.D{
		{Key: "householdId", Value: sharedID}, {Key: "readBy", Value: bson.A{adaID, bobID}},
	}); err != nil {
		t.Fatal(err)
	}
	if t.Failed() {
		return
	}

	res, err := newAccountService(db, householdService, slog.New(slog.DiscardHandler)).Delete(ctx, ada.ID)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if res.PromotedUsers[shared.ID] != bob.ID {
		t.Errorf("result = %+v, want Bob promoted in the shared household", res)
	}

	count := func(coll string, filter bson.D) int64 {
		c, err := db.Collection(coll).CountDocuments(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for coll := range keysByCollection {
		switch accountDeletionKinds[coll] {
		case householdData, userRatings, userHistory:
			if c := count(coll, bson.D{{Key: "householdId", Value: soloID}}); c != 0 {
				t.Errorf("%s: %d documents left for the deleted household", coll, c)
			}
			if c := count(coll, bson.D{{Key: "householdId", Value: sharedID}}); c == 0 {
				t.Errorf("%s: the shared household's documents were deleted", coll)
			}
		}
		switch accountDeletionKinds[coll] {
		case userRatings, userHistory, userOnly:
			if c := count(coll, bson.D{{Key: "userId", Value: adaID}}); c != 0 {
				t.Errorf("%s: %d documents still name the deleted user", coll, c)
			}
			if c := count(coll, bson.D{{Key: "userId", Value: bobID}}); c != 1 {
				t.Errorf("%s: %d of Bob's documents left, want 1", coll, c)
			}
		}
		if accountDeletionKinds[coll] == userHistory {
			if c := count(coll, bson.D{{Key: "householdId", Value: sharedID}}); c != 2 {
				t.Errorf("%s: %d shared history documents, want both kept", coll, c)
			}
		}
	}
	if c := count(notifications.Collection, bson.D{{Key: "readBy", Value: adaID}}); c != 0 {
		t.Errorf("notifications still read by the deleted user: %d", c)
	}
	if c := count(notifications.Collection, bson.D{{Key: "readBy", Value: bobID}}); c != 1 {
		t.Errorf("Bob's read receipt lost: %d", c)
	}

	// Households and users.
	if _, err := householdService.GetHousehold(ctx, solo.ID); !errors.Is(err, households.ErrNotFound) {
		t.Errorf("solo household still exists: %v", err)
	}
	if m, err := householdService.GetMembership(ctx, shared.ID, bob.ID); err != nil || m.Role != households.RoleAdmin {
		t.Errorf("Bob membership = %+v, %v; want admin", m, err)
	}
	if list, _ := households.NewMongoStore(db).ListMembershipsByUser(ctx, ada.ID); len(list) != 0 {
		t.Errorf("Ada memberships left: %+v", list)
	}
	if _, err := userStore.GetUser(ctx, ada.ID); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("Ada still exists: %v", err)
	}
	if _, err := userStore.GetUser(ctx, bob.ID); err != nil {
		t.Errorf("Bob deleted: %v", err)
	}

	// Deleting again is harmless.
	if _, err := newAccountService(db, householdService, nil).Delete(ctx, ada.ID); err != nil {
		t.Errorf("repeat Delete() error = %v", err)
	}
}
