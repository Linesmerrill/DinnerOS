package plus

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestValidateCustomCode(t *testing.T) {
	for in, want := range map[string]error{
		"lines-fam":              nil,
		"Lines-Fam":              nil, // lowercased
		"  lines-fam ":           nil, // trimmed
		"abc":                    nil, // shortest
		"a1b2c3d4e5f6g7h8i9j0":   nil, // longest, 20
		"a-b-c":                  nil,
		"ab":                     ErrCodeInvalid,
		"":                       ErrCodeInvalid,
		"a1b2c3d4e5f6g7h8i9j0k":  ErrCodeInvalid, // 21
		"lines fam":              ErrCodeInvalid,
		"lines_fam":              ErrCodeInvalid,
		"lines.fam":              ErrCodeInvalid,
		"café-fam":               ErrCodeInvalid,
		"-lines":                 ErrCodeHyphenEnds,
		"lines-":                 ErrCodeHyphenEnds,
		"---":                    ErrCodeHyphenEnds,
		"admin":                  ErrCodeNotAllowed,
		"DinnerOS":               ErrCodeNotAllowed,
		"support":                ErrCodeNotAllowed,
		"help":                   ErrCodeNotAllowed,
		"invite":                 ErrCodeNotAllowed,
		"plus":                   ErrCodeNotAllowed,
		"shit-fam":               ErrCodeNotAllowed,
		"bigfuckfam":             ErrCodeNotAllowed, // inside a word
		"f-u-c-k":                ErrCodeNotAllowed, // hyphens taken out
		"SHIT":                   ErrCodeNotAllowed, // any case
		"ad-min":                 ErrCodeNotAllowed, // the same code as admin
		"admin-fam":              nil,               // reserved words only match whole
		"plus-family":            nil,
		"classic-glass":          nil, // short words inside ordinary ones are fine
		"torpedo-fam":            nil,
		"scunthorpe-fam-is-long": ErrCodeInvalid,
	} {
		got, err := ValidateCustomCode(in)
		if !errors.Is(err, want) {
			t.Errorf("ValidateCustomCode(%q) = %q, %v; want %v", in, got, err, want)
			continue
		}
		if want == nil && got != strings.ToLower(strings.TrimSpace(in)) {
			t.Errorf("ValidateCustomCode(%q) = %q, want lowercase", in, got)
		}
	}
}

func TestCodeKey(t *testing.T) {
	for in, want := range map[string]string{
		"lines-fam":   "linesfam",
		"LINES-FAM":   "linesfam",
		"LINESFAM":    "linesfam", // an app build that drops hyphens
		"lines–fam":   "linesfam", // a typographic dash
		"ABCD 2345":   "abcd2345",
		"ab-cd-23-45": "abcd2345",
	} {
		if got := CodeKey(in); got != want {
			t.Errorf("CodeKey(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"https://dinneros.app/invite/lines-fam":       "linesfam",
		"https://dinneros.app/invite/LINES-FAM?utm=x": "linesfam",
		"https://dinneros.app/invite/ABCD2345":        "abcd2345",
		"dinneros://plus?code=abcd2345":               "abcd2345",
		" abcd-2345 ":                                 "abcd2345",
		"":                                            "",
	} {
		if got := redeemKey(in); got != want {
			t.Errorf("redeemKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewCodesAreClean(t *testing.T) {
	for range 200 {
		c := newCleanCode()
		if len(c) != CodeLength || rude(CodeKey(c)) {
			t.Fatalf("newCleanCode = %q", c)
		}
	}
}

// A household can pick a code or get a new random one; the old code stops
// working at once and the share text uses the new one.
func TestChangeCode(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, _ := referralService(t, now)
	first, _ := s.Referral(ctx, hh)
	if first.CodeChanges.Left != CodeChangeLimit {
		t.Fatalf("changes left = %+v", first.CodeChanges)
	}

	info, err := s.ChangeCode(ctx, hh, "Lines-Fam", false)
	if err != nil || info.Code != "lines-fam" || info.CodeChanges.Left != CodeChangeLimit-1 {
		t.Fatalf("custom = %+v, %v", info, err)
	}
	if share, _ := ReferralText(info.Code, 0, 1); !strings.Contains(share, "lines-fam") || !strings.HasSuffix(share, "/invite/lines-fam") {
		t.Errorf("share = %q", share)
	}

	// The old code is replaced; redeeming it says so.
	if _, err := s.Redeem(ctx, hhB, first.Code); !errors.Is(err, ErrCodeRetired) {
		t.Errorf("old code = %v, want replaced", err)
	}
	if RedeemMessage(ErrCodeRetired) != "That code was replaced. Ask for the new one." {
		t.Errorf("message = %q", RedeemMessage(ErrCodeRetired))
	}

	// A random one replaces the custom one.
	again, err := s.ChangeCode(ctx, hh, "", true)
	if err != nil || len(again.Code) != CodeLength || again.Code == first.Code || again.CodeChanges.Left != CodeChangeLimit-2 {
		t.Fatalf("regenerate = %+v, %v", again, err)
	}
	if _, err := s.Redeem(ctx, hhB, "lines-fam"); !errors.Is(err, ErrCodeRetired) {
		t.Errorf("old custom code = %v", err)
	}

	// The new one works, typed in any case.
	if _, err := s.ChangeCode(ctx, hh, "lines-fam-2", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redeem(ctx, hhB, "LINES-FAM-2"); err != nil {
		t.Errorf("redeem new code: %v", err)
	}
	// App builds and the invite page from before drop the hyphens.
	if _, err := s.Redeem(ctx, hhC, "https://dinneros.app/invite/LINESFAM2"); err != nil {
		t.Errorf("redeem without hyphens: %v", err)
	}

	for name, tc := range map[string]struct {
		custom string
		regen  bool
		want   error
	}{
		"nothing":      {"", false, ErrCodeNoChoice},
		"too short":    {"ab", false, ErrCodeInvalid},
		"hyphen ends":  {"-ab-", false, ErrCodeHyphenEnds},
		"reserved":     {"Support", false, ErrCodeNotAllowed},
		"rude":         {"shitfam", false, ErrCodeNotAllowed},
		"already ours": {"LINES-FAM-2", false, ErrCodeSame},
	} {
		if _, err := s.ChangeCode(ctx, hh, tc.custom, tc.regen); !errors.Is(err, tc.want) {
			t.Errorf("%s = %v, want %v", name, err, tc.want)
		}
	}
	// None of those counted.
	if info, _ := s.Referral(ctx, hh); info.CodeChanges.Left != CodeChangeLimit-3 {
		t.Errorf("changes left = %d", info.CodeChanges.Left)
	}
}

// Codes are unique without case across households, and a replaced code is
// gone for good, for its own household too.
func TestCodesAreUniqueAndRetiredForever(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, _ := referralService(t, now)
	generated, _ := s.Referral(ctx, hhB)
	if _, err := s.ChangeCode(ctx, hh, "lines-fam", false); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"lines-fam", "LINES-FAM", "Lines-Fam", "linesfam", "lin-es-fam"} {
		if _, err := s.ChangeCode(ctx, hhB, code, false); !errors.Is(err, ErrCodeTaken) {
			t.Errorf("hhB takes %q = %v", code, err)
		}
	}
	// Another household's generated code, typed lowercase.
	if _, err := s.ChangeCode(ctx, hh, strings.ToLower(generated.Code), false); !errors.Is(err, ErrCodeTaken) {
		t.Errorf("generated code taken = %v", err)
	}

	if _, err := s.ChangeCode(ctx, hh, "lines-home", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeCode(ctx, hh, "lines-fam", false); !errors.Is(err, ErrCodeTaken) {
		t.Errorf("own retired code = %v", err)
	}
	if _, err := s.ChangeCode(ctx, hhB, "Lines-Fam", false); !errors.Is(err, ErrCodeTaken) {
		t.Errorf("other's retired code = %v", err)
	}
	if ok, msg, _ := s.CodeAvailability(ctx, hhB, "lines-fam"); ok || msg != "That code is taken." {
		t.Errorf("availability of retired = %v %q", ok, msg)
	}
}

// An invite made with the old code still counts after the code changes: the
// referral points at the household.
func TestPendingInviteSurvivesCodeChange(t *testing.T) {
	ctx := t.Context()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, free, _ := referralService(t, now)
	old, _ := s.Referral(ctx, hh)
	if _, err := s.Redeem(ctx, hhB, old.Code); err != nil {
		t.Fatal(err)
	}
	if harnessOf(s).state(hhB) != StatePending {
		t.Fatalf("state = %q", harnessOf(s).state(hhB))
	}
	if _, err := s.ChangeCode(ctx, hh, "lines-fam", false); err != nil {
		t.Fatal(err)
	}
	if info, _ := s.Referral(ctx, hh); info.Counts.Pending != 1 || len(info.Recent) != 1 {
		t.Errorf("after change = %+v", info.Counts)
	}
	setUpHousehold(s, hhB)
	if harnessOf(s).state(hhB) != StateQualified {
		t.Errorf("state = %q, want qualified", harnessOf(s).state(hhB))
	}
	if !free.m[hh].Equal(now.Add(month)) || !free.m[hhB].Equal(now.Add(month)) {
		t.Errorf("free = %v / %v", free.m[hh], free.m[hhB])
	}
	if info, _ := s.Referral(ctx, hh); info.Rewarded != 1 || info.Counts.Qualified != 1 {
		t.Errorf("sharer = %+v", info)
	}
}

// Five changes in any 30 days; the sixth says when the next one is.
func TestCodeChangeLimit(t *testing.T) {
	ctx := t.Context()
	start := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, _ := referralService(t, start)
	h := harnessOf(s)
	home := h.homes.m[hh]
	home.TimeZone = "America/Denver"
	h.homes.m[hh] = home

	for i := range CodeChangeLimit {
		h.at(start.Add(time.Duration(i) * 24 * time.Hour))
		if _, err := s.ChangeCode(ctx, hh, "", true); err != nil {
			t.Fatalf("change %d: %v", i+1, err)
		}
	}
	h.at(start.Add(6 * 24 * time.Hour))
	info, _ := s.Referral(ctx, hh)
	next := start.Add(CodeChangeWindow) // the first change drops out then
	if info.CodeChanges.Left != 0 || !info.CodeChanges.Next.Equal(next) {
		t.Fatalf("changes = %+v", info.CodeChanges)
	}
	_, err := s.ChangeCode(ctx, hh, "lines-fam", false)
	var limit *CodeLimitError
	if !errors.As(err, &limit) || !errors.Is(err, ErrCodeLimit) || !limit.Next.Equal(next) {
		t.Fatalf("sixth = %v", err)
	}
	// 12:00 UTC on December 13 is still December 13 in Denver.
	if got := CodeMessage(err); got != "You can change your code again on December 13." {
		t.Errorf("message = %q", got)
	}
	if got := CodeChangesText(info.CodeChanges, info.Location); got != "You can change your code again on December 13." {
		t.Errorf("text = %q", got)
	}
	// The custom code was never claimed.
	if ok, _, _ := s.CodeAvailability(ctx, hhB, "lines-fam"); !ok {
		t.Error("a refused change took the code")
	}

	// Once the first change is 30 days old, one more is allowed.
	h.at(next.Add(time.Minute))
	if info, _ := s.Referral(ctx, hh); info.CodeChanges.Left != 1 ||
		CodeChangesText(info.CodeChanges, nil) != "You can change your code 1 more time in the next 30 days." {
		t.Errorf("after 30 days = %+v", info.CodeChanges)
	}
	if _, err := s.ChangeCode(ctx, hh, "lines-fam", false); err != nil {
		t.Errorf("change after 30 days: %v", err)
	}
	if got := CodeChangesText(CodeChanges{Left: 5}, nil); got != "You can change your code 5 more times in the next 30 days." {
		t.Errorf("text = %q", got)
	}
}

func TestCodeRoutes(t *testing.T) {
	now := time.Now()
	s, _, _, _ := referralService(t, now)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: s, Authorizer: twoHouseholds{}, Tokens: fakeTokens{}}).Mount)
	do := func(method, path, user, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	avail := func(user, household, code string) CodeAvailabilityResponse {
		t.Helper()
		rec := do(http.MethodGet, "/households/"+household+"/plus/referral/code-availability?code="+url.QueryEscape(code), user, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("availability %q = %d %s", code, rec.Code, rec.Body.String())
		}
		var out CodeAvailabilityResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	rec := do(http.MethodGet, "/households/"+hh+"/plus/referral", "u1", "")
	var ref ReferralResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &ref)
	if ref.CodeChangesLeft != CodeChangeLimit || ref.CodeChangesLimit != CodeChangeLimit || ref.CodeChangesNext != nil ||
		ref.CodeChangesText == "" || ref.CodeHelp != CodeHelp {
		t.Fatalf("referral = %s", rec.Body.String())
	}
	oldCode := ref.Code

	for code, want := range map[string]CodeAvailabilityResponse{
		"lines-fam": {true, "You can use that code."},
		"ab":        {false, "Use 3 to 20 letters, numbers or hyphens."},
		"lines fam": {false, "Use 3 to 20 letters, numbers or hyphens."},
		"-lines":    {false, "Start and end with a letter or number."},
		"admin":     {false, "That code isn't allowed."},
		"shit-fam":  {false, "That code isn't allowed."},
		oldCode:     {false, "That's already your code."},
	} {
		if got := avail("u1", hh, code); got != want {
			t.Errorf("availability %q = %+v, want %+v", code, got, want)
		}
	}

	rec = do(http.MethodPost, "/households/"+hh+"/plus/referral/code", "u1", `{"custom":"Lines-Fam"}`)
	ref = ReferralResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ref)
	if rec.Code != http.StatusOK || ref.Code != "lines-fam" || ref.ShareURL != "https://dinneros.app/invite/lines-fam" ||
		!strings.Contains(ref.ShareText, "lines-fam") || !strings.Contains(ref.ShareMessage, "lines-fam") ||
		ref.CodeChangesLeft != CodeChangeLimit-1 {
		t.Fatalf("change = %d %s", rec.Code, rec.Body.String())
	}
	if got := avail("u2", hhB, "LINES-FAM"); got != (CodeAvailabilityResponse{false, "That code is taken."}) {
		t.Errorf("taken = %+v", got)
	}
	if got := avail("u2", hhB, strings.ToLower(oldCode)); got != (CodeAvailabilityResponse{false, "That code is taken."}) {
		t.Errorf("retired = %+v", got)
	}

	for body, want := range map[string]struct {
		status int
		code   string
	}{
		`{}`:                     {http.StatusUnprocessableEntity, "code_invalid"},
		`not json`:               {http.StatusBadRequest, "validation_failed"},
		`{"custom":"ab"}`:        {http.StatusUnprocessableEntity, "code_invalid"},
		`{"custom":"help"}`:      {http.StatusUnprocessableEntity, "code_not_allowed"},
		`{"custom":"lines-fam"}`: {http.StatusConflict, "code_taken"},
	} {
		rec := do(http.MethodPost, "/households/"+hh+"/plus/referral/code", "u1", body)
		if rec.Code != want.status || !strings.Contains(rec.Body.String(), `"code":"`+want.code+`"`) {
			t.Errorf("%s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	rec = do(http.MethodPost, "/households/"+hhB+"/plus/referral/code", "u2", `{"custom":"lines-fam"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "That code is taken.") {
		t.Errorf("other household = %d %s", rec.Code, rec.Body.String())
	}

	// The old code no longer redeems.
	rec = do(http.MethodPost, "/households/"+hhB+"/plus/referral/redeem", "u2", `{"code":"`+oldCode+`"}`)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "code_replaced") ||
		!strings.Contains(rec.Body.String(), "That code was replaced. Ask for the new one.") {
		t.Errorf("old code = %d %s", rec.Code, rec.Body.String())
	}

	// Four more random codes use up the limit; the next is a plain 429.
	for range CodeChangeLimit - 1 {
		if rec := do(http.MethodPost, "/households/"+hh+"/plus/referral/code", "u1", `{"regenerate":true}`); rec.Code != http.StatusOK {
			t.Fatalf("regenerate = %d %s", rec.Code, rec.Body.String())
		}
	}
	rec = do(http.MethodPost, "/households/"+hh+"/plus/referral/code", "u1", `{"regenerate":true}`)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "code_change_limit") ||
		!strings.Contains(rec.Body.String(), "You can change your code again on ") {
		t.Errorf("sixth = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/households/"+hh+"/plus/referral", "u1", "")
	ref = ReferralResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &ref)
	if ref.CodeChangesLeft != 0 || ref.CodeChangesNext == nil || !strings.HasPrefix(ref.CodeChangesText, "You can change your code again on ") {
		t.Errorf("at limit = %s", rec.Body.String())
	}

	// Another household's routes stay closed.
	if rec := do(http.MethodPost, "/households/"+hh+"/plus/referral/code", "u2", `{"regenerate":true}`); rec.Code == http.StatusOK {
		t.Errorf("other member changed the code: %d", rec.Code)
	}
}

// Every code word is plain: no em dashes, nothing internal.
func TestCodeCopy(t *testing.T) {
	errs := []error{
		ErrCodeInvalid, ErrCodeHyphenEnds, ErrCodeNotAllowed, ErrCodeTaken, ErrCodeSame, ErrCodeChangedNow,
		ErrCodeNoChoice, &CodeLimitError{Next: time.Now()}, errors.New("x"),
	}
	for _, err := range errs {
		if m := CodeMessage(err); m == "" || strings.ContainsAny(m, "—–") || strings.Contains(m, "plus:") {
			t.Errorf("message for %v = %q", err, m)
		}
	}
}

func TestIntegrationReferralCodesMongo(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	if err := client.EnsureIndexes(ctx, ReferralIndexes()...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	refs := NewMongoReferrals(db)
	at := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

	// Codes from before decision 699 have no key yet; they still redeem and
	// count as taken, and the migration records them, once.
	hhOID, _ := bson.ObjectIDFromHex(hh)
	hhBOID, _ := bson.ObjectIDFromHex(hhB)
	if _, err := db.Collection(ReferralCodesCollection).InsertMany(ctx, []any{
		bson.D{{Key: "householdId", Value: hhOID}, {Key: "code", Value: "V6FZZFTW"}, {Key: "createdAt", Value: at}},
		bson.D{{Key: "householdId", Value: hhBOID}, {Key: "code", Value: "ABCD2345"}, {Key: "createdAt", Value: at}},
	}); err != nil {
		t.Fatal(err)
	}
	if owner, err := refs.Owner(ctx, "v6fzzftw"); err != nil || owner != hh {
		t.Errorf("unmigrated owner = %q, %v", owner, err)
	}
	if taken, _ := refs.Taken(ctx, "abcd2345"); !taken {
		t.Error("unmigrated code not taken")
	}
	if n, err := refs.MigrateCodes(ctx); err != nil || n != 2 {
		t.Fatalf("migrate = %d, %v", n, err)
	}
	if n, err := refs.MigrateCodes(ctx); err != nil || n != 0 {
		t.Errorf("second migrate = %d, %v", n, err)
	}
	if n, _ := db.Collection(ReferralCodeKeysCollection).CountDocuments(ctx, bson.D{{Key: "status", Value: "active"}}); n != 2 {
		t.Errorf("active keys = %d", n)
	}
	if code, _ := refs.Code(ctx, hh, NewCode); code != "V6FZZFTW" {
		t.Errorf("code after migration = %q", code)
	}
	for _, typed := range []string{"V6FZZFTW", "v6fzzftw"} {
		if owner, err := refs.Owner(ctx, typed); err != nil || owner != hh {
			t.Errorf("owner %q = %q, %v", typed, owner, err)
		}
	}

	// A change swaps the code and retires the old one for good.
	if err := refs.Replace(ctx, hh, "V6FZZFTW", "lines-fam", at); err != nil {
		t.Fatal(err)
	}
	if code, _ := refs.Code(ctx, hh, NewCode); code != "lines-fam" {
		t.Errorf("code = %q", code)
	}
	if owner, err := refs.Owner(ctx, "LINES-FAM"); err != nil || owner != hh {
		t.Errorf("owner = %q, %v", owner, err)
	}
	if _, err := refs.Owner(ctx, "V6FZZFTW"); !errors.Is(err, ErrCodeRetired) {
		t.Errorf("old code = %v", err)
	}
	if changes, _ := refs.CodeChanges(ctx, hh); len(changes) != 1 || !changes[0].Equal(at) {
		t.Errorf("changes = %v", changes)
	}
	// Taken, retired or active, by either household, in any case.
	for _, tc := range []struct{ household, old, code string }{
		{hhB, "ABCD2345", "Lines-Fam"},
		{hhB, "ABCD2345", "v6fzzftw"},
		{hh, "lines-fam", "V6FZZFTW"},
		{hh, "lines-fam", "abcd2345"},
	} {
		if err := refs.Replace(ctx, tc.household, tc.old, tc.code, at); !errors.Is(err, ErrCodeTaken) {
			t.Errorf("replace %s with %q = %v", tc.household, tc.code, err)
		}
		if taken, _ := refs.Taken(ctx, tc.code); !taken {
			t.Errorf("%q not taken", tc.code)
		}
	}
	// A change from a code that is no longer current loses, and frees the
	// key it tried.
	if err := refs.Replace(ctx, hh, "V6FZZFTW", "lines-home", at); !errors.Is(err, ErrCodeChangedNow) {
		t.Errorf("stale change = %v", err)
	}
	if taken, _ := refs.Taken(ctx, "lines-home"); taken {
		t.Error("a lost change kept its code")
	}

	// The change limit keeps the last few times.
	for i := range keptChanges + 3 {
		next := "lines-" + string(rune('a'+i))
		cur, _ := refs.Code(ctx, hh, NewCode)
		if err := refs.Replace(ctx, hh, cur, next, at.Add(time.Duration(i+1)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if changes, _ := refs.CodeChanges(ctx, hh); len(changes) != keptChanges {
		t.Errorf("kept %d changes", len(changes))
	}

	// A new household's first code is recorded too.
	if code, err := refs.Code(ctx, hhC, func() string { return "ZZZZ2345" }); err != nil || code != "ZZZZ2345" {
		t.Fatalf("new code = %q, %v", code, err)
	}
	if taken, _ := refs.Taken(ctx, "zzzz2345"); !taken {
		t.Error("new code not recorded")
	}
	// A drawn code that collides with a retired one is drawn again.
	draws := []string{"V6FZZFTW", "YYYY2345"}
	hhD := "66e5a1f2c3b4a5d6e7f80a04"
	if code, err := refs.Code(ctx, hhD, func() string { c := draws[0]; draws = draws[1:]; return c }); err != nil || code != "YYYY2345" {
		t.Errorf("redraw = %q, %v", code, err)
	}

	// A deleted household's codes stay taken, but just aren't found.
	if err := refs.PurgeHousehold(ctx, hhC); err != nil {
		t.Fatal(err)
	}
	if _, err := refs.Owner(ctx, "ZZZZ2345"); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("purged = %v", err)
	}
	if taken, _ := refs.Taken(ctx, "ZZZZ2345"); !taken {
		t.Error("purged code free again")
	}
}
