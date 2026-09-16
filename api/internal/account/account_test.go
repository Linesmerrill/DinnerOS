package account

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

const userAda = "aaaaaaaaaaaaaaaaaaaaaaaa"

// recorder logs every call in order, so tests can check what ran and when.
type recorder struct{ calls []string }

func (r *recorder) add(s string) { r.calls = append(r.calls, s) }

type fakeHouseholds struct {
	rec        *recorder
	departures []households.Departure
	deleteErr  error
	promote    map[string]string
}

func (f *fakeHouseholds) AccountDepartures(context.Context, string) ([]households.Departure, error) {
	f.rec.add("departures")
	return f.departures, nil
}

func (f *fakeHouseholds) DeleteEmptiedHousehold(_ context.Context, householdID, _ string) error {
	f.rec.add("delete household " + householdID)
	return f.deleteErr
}

func (f *fakeHouseholds) LeaveForAccountDeletion(_ context.Context, householdID, _ string) (string, error) {
	f.rec.add("leave " + householdID)
	return f.promote[householdID], nil
}

type fakePurger struct {
	rec  *recorder
	name string
	err  error
}

func (p fakePurger) PurgeHousehold(_ context.Context, householdID string) error {
	p.rec.add(p.name + " purge household " + householdID)
	return p.err
}

func (p fakePurger) PurgeUser(_ context.Context, userID string) error {
	p.rec.add(p.name + " purge user")
	return p.err
}

func TestDeleteOrdersHouseholdsBeforeUserData(t *testing.T) {
	rec := &recorder{}
	hh := &fakeHouseholds{
		rec: rec,
		departures: []households.Departure{
			{HouseholdID: "solo", LastMember: true},
			{HouseholdID: "shared"},
		},
		promote: map[string]string{"shared": "bob"},
	}
	svc := NewService(Options{
		Households:    hh,
		HouseholdData: []HouseholdPurger{fakePurger{rec: rec, name: "plans"}, fakePurger{rec: rec, name: "pantry"}},
		UserData:      []UserPurger{fakePurger{rec: rec, name: "sessions"}, fakePurger{rec: rec, name: "users"}},
	})

	res, err := svc.Delete(context.Background(), userAda)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"departures",
		"plans purge household solo",
		"pantry purge household solo",
		"delete household solo",
		"leave shared",
		"sessions purge user",
		"users purge user",
	}
	if !slices.Equal(rec.calls, want) {
		t.Errorf("calls = %q\nwant %q", rec.calls, want)
	}
	if !slices.Equal(res.DeletedHouseholds, []string{"solo"}) || !slices.Equal(res.LeftHouseholds, []string{"shared"}) ||
		res.PromotedUsers["shared"] != "bob" {
		t.Errorf("result = %+v", res)
	}
}

func TestDeleteStopsBeforeUserDataOnFailure(t *testing.T) {
	rec := &recorder{}
	boom := errors.New("boom")
	svc := NewService(Options{
		Households:    &fakeHouseholds{rec: rec, departures: []households.Departure{{HouseholdID: "solo", LastMember: true}}},
		HouseholdData: []HouseholdPurger{fakePurger{rec: rec, name: "plans", err: boom}},
		UserData:      []UserPurger{fakePurger{rec: rec, name: "users"}},
	})
	if _, err := svc.Delete(context.Background(), userAda); !errors.Is(err, boom) {
		t.Fatalf("Delete() error = %v, want boom", err)
	}
	if slices.Contains(rec.calls, "users purge user") {
		t.Errorf("user deleted after a failed household purge: %q", rec.calls)
	}
}

func TestDeleteLeavesAHouseholdSomeoneJoinedMidway(t *testing.T) {
	rec := &recorder{}
	svc := NewService(Options{
		Households: &fakeHouseholds{
			rec:        rec,
			departures: []households.Departure{{HouseholdID: "solo", LastMember: true}},
			deleteErr:  households.ErrConflict,
		},
	})
	if _, err := svc.Delete(context.Background(), userAda); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(rec.calls, "leave solo") {
		t.Errorf("calls = %q, want a leave after the conflict", rec.calls)
	}
}

type fakeTokens map[string]string

func (f fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := f[token]; ok {
		return id, nil
	}
	return "", errors.New("invalid")
}

type fakeDeleter struct {
	userID string
	err    error
}

func (f *fakeDeleter) Delete(_ context.Context, userID string) (Result, error) {
	f.userID = userID
	return Result{}, f.err
}

func TestHandlerDeleteMe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  string
		err    error
		status int
	}{
		{name: "deleted", token: "good", status: http.StatusNoContent},
		{name: "unauthenticated", token: "bad", status: http.StatusUnauthorized},
		{name: "failure", token: "good", err: errors.New("db down"), status: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deleter := &fakeDeleter{err: tc.err}
			r := chi.NewRouter()
			NewHandler(HandlerOptions{Service: deleter, Tokens: fakeTokens{"good": userAda}}).Mount(r)

			req := httptest.NewRequest(http.MethodDelete, "/me", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.token == "good" && deleter.userID != userAda {
				t.Errorf("deleted user = %q", deleter.userID)
			}
			if tc.token == "bad" && deleter.userID != "" {
				t.Errorf("unauthenticated request deleted %q", deleter.userID)
			}
		})
	}
}
