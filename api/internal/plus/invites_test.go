package plus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

// A household sees who used its code, newest first, without names, and
// whether it joined with someone else's code.
func TestReferralInvites(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, _ := referralService(t, now)

	info, err := s.Referral(ctx, hh)
	if err != nil || info.Joined != 0 || len(info.Recent) != 0 || info.UsedCode != nil || !info.FreeUntil.IsZero() {
		t.Fatalf("empty = %+v, %v", info, err)
	}
	if resp := invitesResponse(info); resp.EmptyText != InvitesEmptyText || len(resp.Joins) != 0 || resp.Joins == nil ||
		resp.CapText != "0 of 12" || resp.FreeUntil != nil {
		t.Errorf("empty response = %+v", resp)
	}

	if _, err := s.Redeem(ctx, hhB, info.Code); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return now.Add(24 * time.Hour) }
	if _, err := s.Redeem(ctx, hhC, info.Code); err != nil {
		t.Fatal(err)
	}

	info, err = s.Referral(ctx, hh)
	if err != nil || info.Joined != 2 || len(info.Recent) != 2 || info.Rewarded != 2 {
		t.Fatalf("after two = %+v, %v", info, err)
	}
	if !info.Recent[0].At.Equal(now.Add(24*time.Hour)) || info.Recent[0].ReferredID != hhC {
		t.Errorf("newest first: %+v", info.Recent)
	}
	if want := now.Add(2 * month); !info.FreeUntil.Equal(want) {
		t.Errorf("free until %v, want %v", info.FreeUntil, want)
	}
	resp := invitesResponse(info)
	if resp.Joined != 2 || resp.MonthsEarned != 2 || resp.MonthsCap != MaxReferralMonths || resp.CapText != "2 of 12" ||
		resp.FreeUntil == nil || resp.UsedCode != nil {
		t.Errorf("response = %+v", resp)
	}
	for _, j := range resp.Joins {
		if !j.Counted || j.Status != "Counted" {
			t.Errorf("join = %+v", j)
		}
	}
	// Other households are never named or identified.
	raw, _ := json.Marshal(resp)
	for _, id := range []string{hhB, hhC} {
		if strings.Contains(string(raw), id) {
			t.Errorf("response names %s: %s", id, raw)
		}
	}

	// The household that joined sees its own use, and when its month ends.
	joined, err := s.Referral(ctx, hhB)
	if err != nil || joined.UsedCode == nil || joined.UsedCode.ReferrerID != hh {
		t.Fatalf("joiner = %+v, %v", joined, err)
	}
	if u := invitesResponse(joined).UsedCode; u == nil || !u.At.Equal(now) || !u.FreeUntil.Equal(now.Add(month)) {
		t.Errorf("used code = %+v", u)
	}

	// Free time that ended isn't shown.
	s.now = func() time.Time { return now.Add(3 * month) }
	if info, _ := s.Referral(ctx, hh); !info.FreeUntil.IsZero() {
		t.Errorf("ended free time shown: %v", info.FreeUntil)
	}
}

// Joins past the cap still list, marked as not earning a month.
func TestReferralInvitesPastTheCap(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, refs := referralService(t, now)
	_, _ = s.Referral(ctx, hh)
	for i := range MaxReferralMonths + 1 {
		refs.refs = append(refs.refs, Referral{
			ReferrerID: hh, ReferredID: "x" + string(rune('a'+i)),
			At: now.Add(time.Duration(i) * time.Hour), ReferrerRewarded: i < MaxReferralMonths,
		})
	}
	info, _ := s.Referral(ctx, hh)
	resp := invitesResponse(info)
	if resp.Joined != MaxReferralMonths+1 || resp.CapText != "12 of 12" {
		t.Errorf("response = %+v", resp)
	}
	if j := resp.Joins[0]; j.Counted || j.Status != "Limit reached" {
		t.Errorf("newest join = %+v", j)
	}
	if j := resp.Joins[1]; !j.Counted || j.Status != "Counted" {
		t.Errorf("second join = %+v", j)
	}
}

func TestReferralInviteRoutes(t *testing.T) {
	now := time.Now().UTC()
	s, _, _, _ := referralService(t, now)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: s, Authorizer: twoHouseholds{}, Tokens: fakeTokens{}, Operators: []string{"op"},
	}).Mount)
	do := func(method, path, user, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodGet, "/households/"+hh+"/plus/referral", "u1", "")
	var ref ReferralResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ref); err != nil || ref.Invites.EmptyText == "" || ref.Invites.Joins == nil {
		t.Fatalf("referral = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"joins":[]`) {
		t.Errorf("joins should be an empty list: %s", rec.Body.String())
	}
	if rec := do(http.MethodPost, "/households/"+hhB+"/plus/referral/redeem", "u2", `{"code":"`+ref.Code+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("redeem = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/households/"+hh+"/plus/referral", "u1", "")
	ref = ReferralResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ref)
	if ref.Invites.Joined != 1 || len(ref.Invites.Joins) != 1 || ref.Invites.Joins[0].Status != "Counted" || ref.Invites.FreeUntil == nil {
		t.Errorf("after a join = %s", rec.Body.String())
	}
	rec = do(http.MethodGet, "/households/"+hhB+"/plus/referral", "u2", "")
	ref = ReferralResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ref)
	if ref.Invites.UsedCode == nil {
		t.Errorf("joiner = %s", rec.Body.String())
	}

	// The summary is for the people who run DinnerOS only.
	if rec := do(http.MethodGet, "/plus/referrals/summary", "u1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("member summary = %d", rec.Code)
	}
	rec = do(http.MethodGet, "/plus/referrals/summary", "op", "")
	var sum ReferralSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("summary = %d %s", rec.Code, rec.Body.String())
	}
	if sum.CodesUsed != 1 || sum.JoinedAllTime != 1 || sum.JoinedThisWeek != 1 || len(sum.TopReferrers) != 1 ||
		sum.TopReferrers[0].Count != 1 || sum.TopReferrers[0].Name == "" {
		t.Errorf("summary = %+v", sum)
	}
	if rec := httptest.NewRecorder(); true {
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plus/referrals/summary", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("signed out summary = %d", rec.Code)
		}
	}
}

// Invite words are plain: no em dashes.
func TestInviteCopy(t *testing.T) {
	for _, s := range []string{InvitesEmptyText, "Counted", "Limit reached"} {
		if strings.Contains(s, "—") {
			t.Errorf("%q has an em dash", s)
		}
	}
}

func TestIntegrationReferralJoinsAndReport(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, ReferralIndexes()...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	refs := NewMongoReferrals(db)
	hhOID, _ := bson.ObjectIDFromHex(hh)
	if _, err := db.Collection("households").InsertOne(ctx, map[string]any{"_id": hhOID, "name": "Lines"}); err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	if n, list, err := refs.Joins(ctx, hh, 20); err != nil || n != 0 || len(list) != 0 {
		t.Errorf("no joins = %d %v %v", n, list, err)
	}
	if _, ok, err := refs.UsedBy(ctx, hhB); err != nil || ok {
		t.Errorf("unused = %v %v", ok, err)
	}
	for _, r := range []Referral{
		{ReferrerID: hh, ReferredID: hhB, At: at, ReferrerRewarded: true},
		{ReferrerID: hh, ReferredID: hhC, At: at.Add(48 * time.Hour), ReferrerRewarded: false},
		{ReferrerID: hhB, ReferredID: "66e5a1f2c3b4a5d6e7f80a04", At: at.Add(-30 * 24 * time.Hour), ReferrerRewarded: true},
	} {
		if err := refs.Record(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	n, list, err := refs.Joins(ctx, hh, 1)
	if err != nil || n != 2 || len(list) != 1 || list[0].ReferredID != hhC || list[0].ReferrerRewarded {
		t.Errorf("joins = %d %+v %v", n, list, err)
	}
	if r, ok, err := refs.UsedBy(ctx, hhB); err != nil || !ok || r.ReferrerID != hh || !r.At.Equal(at) {
		t.Errorf("used by = %+v %v %v", r, ok, err)
	}

	rep, err := refs.Report(ctx, at.Add(-7*24*time.Hour), 5)
	if err != nil || rep.Codes != 2 || rep.Joined != 3 || rep.JoinedSince != 2 || len(rep.Top) != 2 {
		t.Fatalf("report = %+v %v", rep, err)
	}
	if rep.Top[0].HouseholdID != hh || rep.Top[0].Count != 2 || rep.Top[0].Name != "Lines" {
		t.Errorf("top = %+v", rep.Top[0])
	}
	if rep.Top[1].Name != "" || rep.Top[1].Count != 1 {
		t.Errorf("second = %+v", rep.Top[1])
	}
}
