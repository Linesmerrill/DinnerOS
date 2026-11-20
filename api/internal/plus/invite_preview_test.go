package plus

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// An invite link shows who invited you before you sign in: no token needed.
// Unknown and replaced codes get the same words the app shows when redeeming.
func TestInvitePreviewRoute(t *testing.T) {
	now := time.Now()
	s, _, _, _ := referralService(t, now)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: s, Authorizer: twoHouseholds{}, Tokens: fakeTokens{}}).Mount)
	get := func(code string) (*httptest.ResponseRecorder, InvitePreviewResponse) {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/referral-codes/"+code, nil))
		var out InvitePreviewResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}

	info, err := s.Referral(t.Context(), hh)
	if err != nil {
		t.Fatal(err)
	}
	rec, p := get(info.Code)
	if rec.Code != http.StatusOK || p.Title != "Lines invited you to DinnerOS" || p.HouseholdName != "Lines" ||
		p.Message != "You both get a free month of Plus once you’re set up." || p.Code != info.Code {
		t.Fatalf("preview = %d %s", rec.Code, rec.Body.String())
	}
	if strings.Join(p.Steps, "|") != "Create your household|Add a recipe|Plan a dinner" {
		t.Errorf("steps = %q", p.Steps)
	}
	if p.ForExisting.Title != "This code is for new households" || p.ForOwn.Title != "That’s your own code" ||
		p.ForExisting.Message == "" || p.ForOwn.Message == "" {
		t.Errorf("notices = %+v %+v", p.ForExisting, p.ForOwn)
	}
	// Typed the way the website shows it: lowercase, with a space.
	if rec, _ := get(strings.ToLower(info.Code[:4]) + "%20" + info.Code[4:]); rec.Code != http.StatusOK {
		t.Errorf("spaced lowercase = %d %s", rec.Code, rec.Body.String())
	}

	if rec, _ := get("ZZZZZZZZ"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code_not_found"`) ||
		!strings.Contains(rec.Body.String(), "couldn’t find that code") {
		t.Errorf("unknown = %d %s", rec.Code, rec.Body.String())
	}

	if _, err := s.ChangeCode(t.Context(), hh, "lines-fam", false); err != nil {
		t.Fatal(err)
	}
	if rec, _ := get(info.Code); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"code_replaced"`) ||
		!strings.Contains(rec.Body.String(), "That code was replaced. Ask for the new one.") {
		t.Errorf("replaced = %d %s", rec.Code, rec.Body.String())
	}
	if rec, p := get("lines-fam"); rec.Code != http.StatusOK || p.Title != "Lines invited you to DinnerOS" {
		t.Errorf("custom = %d %s", rec.Code, rec.Body.String())
	}
}

func TestInviteTitleWithoutAName(t *testing.T) {
	if got := InviteTitle("  "); got != "You’re invited to DinnerOS" {
		t.Errorf("no name = %q", got)
	}
}

func TestTimeLeftText(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		left time.Duration
		want string
	}{
		{14 * 24 * time.Hour, "14 days left"},
		{13*24*time.Hour + time.Minute, "14 days left"},
		{2 * 24 * time.Hour, "2 days left"},
		{24 * time.Hour, "1 day left"},
		{23 * time.Hour, "Last day"},
	} {
		if got := TimeLeftText(now.Add(tc.left), now); got != tc.want {
			t.Errorf("%v = %q, want %q", tc.left, got, tc.want)
		}
	}
	if got := TimeLeftText(time.Time{}, now); got != "" {
		t.Errorf("no deadline = %q", got)
	}
}

// The new words are plain too.
func TestInviteCopyIsPlain(t *testing.T) {
	for _, s := range append([]string{
		InviteMessage, InviteExistingTitle, InviteExistingMessage, InviteOwnTitle, InviteOwnMessage, InvitesOffText,
		InviteTitle("Lines"), StepHousehold,
	}, DefaultReferralRules().SetupSteps()...) {
		for _, bad := range []string{"—", "referral", "household_id", "qualif", "redeem"} {
			if strings.Contains(strings.ToLower(s), bad) {
				t.Errorf("%q contains %q", s, bad)
			}
		}
	}
}
