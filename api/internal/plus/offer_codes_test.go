package plus

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// syncBuffer is a log sink tests can read while handlers write.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// logTo sends the service's log to a buffer.
func logTo(s *Service) *syncBuffer {
	b := &syncBuffer{}
	s.logger = slog.New(slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return b
}

// qualifyOne has hhB use hh's code and set up, so the referral pays.
func qualifyOne(t *testing.T, s *Service) {
	t.Helper()
	code, _ := s.Referral(t.Context(), hh)
	if _, err := s.Redeem(t.Context(), hhB, code.Code); err != nil {
		t.Fatal(err)
	}
	setUpHousehold(s, hhB)
	if harnessOf(s).state(hhB) != StateQualified {
		t.Fatalf("state = %q, want qualified", harnessOf(s).state(hhB))
	}
}

// With the pool empty the month is owed, one alert goes out, and the next
// import hands both households their codes and tells them.
func TestFreeMonthOwedThenFilledAfterImport(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, free, _ := referralService(t, now)
	h := harnessOf(s)
	h.offers.codes, h.offers.keys = nil, map[string]bool{}
	qualifyOne(t, s)

	for _, id := range []string{hh, hhB} {
		if m := h.months(id); len(m) != 1 || m[0].State != MonthOwed {
			t.Fatalf("%s months = %+v, want one owed", id, m)
		}
	}
	if len(free.m) != 0 {
		t.Errorf("an owed month wrote a database date: %v", free.m)
	}
	if got := h.notes.to(hhB); len(got) != 1 || got[0].Title != FreeMonthOwedTitle || !strings.HasSuffix(got[0].Body, FreeMonthOwedText) {
		t.Errorf("new household told %+v", got)
	}
	if got := h.notes.to(hh); len(got) != 1 || got[0].Body != "Parkers joined from your invite. Your free month is on the way." {
		t.Errorf("sharer told %+v", got)
	}
	if p, _ := s.ReferralProgress(t.Context(), hhB); p == nil || p.Message != FreeMonthOwedTitle {
		t.Errorf("progress = %+v", p)
	}
	// One email for the run dry, however many months wait.
	if len(h.alerts.sent) != 1 || !strings.Contains(h.alerts.sent[0], "out of free month codes. Free months waiting for a code: 1.") {
		t.Fatalf("alerts = %q", h.alerts.sent)
	}
	if n, _ := s.FillOwed(t.Context()); n != 0 || len(h.alerts.sent) != 1 {
		t.Errorf("fill on an empty pool = %d, alerts %d", n, len(h.alerts.sent))
	}
	// A reveal of an owed month says it's on the way.
	if _, err := s.RevealFreeMonth(t.Context(), hhB, h.months(hhB)[0].ID); err != ErrFreeMonthOwed {
		t.Errorf("reveal owed = %v", err)
	}

	res, err := s.ImportOfferCodes(t.Context(), []string{"AAAA1111BBBB", "CCCC2222DDDD", "EEEE3333FFFF"},
		OfferBatch{Offer: "Invite free month", Batch: "2026-11", ExpiresAt: now.Add(90 * 24 * time.Hour)})
	if err != nil || res.Added != 3 || res.Filled != 2 {
		t.Fatalf("import = %+v, %v", res, err)
	}
	h.oneReadyMonth(hh, hhB)
	for _, id := range []string{hh, hhB} {
		got := h.notes.to(id)
		if len(got) != 2 || got[1].Title != FreeMonthReadyTitle || got[1].Body != FreeMonthReadyBody {
			t.Errorf("%s told %+v", id, got)
		}
	}
	// Running dry again after an import alerts again.
	h.offers.mu.Lock()
	for i := range h.offers.codes {
		if h.offers.codes[i].state == codeAvailable {
			h.offers.codes[i].state = codeGiven
		}
	}
	h.offers.mu.Unlock()
	h.offers.months = append(h.offers.months, FreeMonth{ID: "00000000000000000000ff99", HouseholdID: hhC, State: MonthOwed})
	if _, err := s.FillOwed(t.Context()); err != nil || len(h.alerts.sent) != 2 {
		t.Errorf("second run dry: %v, alerts %d", err, len(h.alerts.sent))
	}
}

// The caps are unchanged: a fourth invite in 30 days waits and takes no
// code, and a sharer at the cap takes none (see qualify_test.go for both).
// Here: settling twice never takes a second code.
func TestFreeMonthSettleTakesOneCodePerMonth(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, refs := referralService(t, now)
	h := harnessOf(s)
	qualifyOne(t, s)
	ref, _, _ := refs.UsedBy(t.Context(), hhB)
	for range 3 {
		if err := s.settle(t.Context(), ref); err != nil {
			t.Fatal(err)
		}
	}
	h.oneReadyMonth(hh, hhB)
	if st, _ := h.offers.Stock(t.Context(), now); st.Given != 2 {
		t.Errorf("given = %d, want 2", st.Given)
	}
}

// A code that ran out before anyone revealed it is swapped for a fresh one.
func TestFreeMonthExpiredCodeIsReplaced(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, subs, _, _ := referralService(t, now)
	h := harnessOf(s)
	h.offers.codes, h.offers.keys = nil, map[string]bool{}
	h.offers.stock(2, now.Add(20*24*time.Hour))
	qualifyOne(t, s)
	h.oneReadyMonth(hh, hhB)
	// hhB saw its code and has a subscription: maybe it redeemed, so it
	// keeps its month. hh never saw its code: it gets a new one.
	m := h.months(hhB)[0]
	if _, err := s.RevealFreeMonth(t.Context(), hhB, m.ID); err != nil {
		t.Fatal(err)
	}
	subs.m[hhB] = Entitlement{HouseholdID: hhB, Ended: true, LastEventAt: now}
	h.offers.stock(1, now.Add(200*24*time.Hour))
	h.at(now.Add(21 * 24 * time.Hour))
	if n, err := s.FillOwed(t.Context()); err != nil || n != 1 {
		t.Fatalf("fill = %d, %v", n, err)
	}
	if got := h.months(hh); len(got) != 1 || got[0].State != MonthReady || !got[0].CodeExpiresAt.Equal(now.Add(200*24*time.Hour)) {
		t.Errorf("sharer months = %+v", got)
	}
	if got := h.months(hhB); got[0].CodeExpiresAt.After(now.Add(30*24*time.Hour)) || got[0].State != MonthRedeemed {
		t.Errorf("a revealed month of a subscriber = %+v, want counted as used, not swapped", got)
	}
	// It doesn't come back on the next sweep.
	if exp, _ := h.offers.ExpiredGiven(t.Context(), now.Add(22*24*time.Hour), 10); len(exp) != 0 {
		t.Errorf("still expired: %+v", exp)
	}
}

func TestFreeMonthRedeemedFromRevenueCat(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, _, _, _ := referralService(t, now)
	h := harnessOf(s)
	qualifyOne(t, s)
	m := h.months(hhB)[0]
	// Not revealed: an offer code purchase can't be this month's.
	ev := event("e1", "INITIAL_PURCHASE", now, now.Add(month))
	ev.AppUserID, ev.OfferCode, ev.PeriodType = hhB, "Invite free month", "TRIAL"
	if _, err := s.Apply(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	if got := h.months(hhB)[0]; got.State != MonthReady {
		t.Errorf("unrevealed month = %+v", got)
	}
	if _, err := s.RevealFreeMonth(t.Context(), hhB, m.ID); err != nil {
		t.Fatal(err)
	}
	ev.ID, ev.EventTimestampMs = "e2", now.Add(time.Minute).UnixMilli()
	ev.Type = "RENEWAL"
	if _, err := s.Apply(t.Context(), ev); err != nil {
		t.Fatal(err)
	}
	if got := h.months(hhB)[0]; got.State != MonthRedeemed || got.RedeemedAt.IsZero() {
		t.Errorf("redeemed month = %+v", got)
	}
	if _, err := s.RevealFreeMonth(t.Context(), hhB, m.ID); err != ErrFreeMonthRedeemed {
		t.Errorf("reveal after redeeming = %v", err)
	}
	// Its code's text is forgotten.
	for _, c := range h.offers.codes {
		if c.month == m.ID && c.code != "" {
			t.Error("a redeemed code is still kept")
		}
	}
}

// Without RevenueCat naming the offer code, a month counts as redeemed once
// the household's first subscription started after it saw the code.
func TestFreeMonthRedeemedWhenASubscriptionStartsAfterTheReveal(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	s, subs, _, _ := referralService(t, now)
	h := harnessOf(s)
	qualifyOne(t, s)
	m := h.months(hhB)[0]
	if _, err := s.RevealFreeMonth(t.Context(), hhB, m.ID); err != nil {
		t.Fatal(err)
	}
	subs.m[hhB] = Entitlement{HouseholdID: hhB, Since: now.Add(time.Minute), ExpiresAt: now.Add(month), LastEventAt: now}
	if got := h.months(hhB)[0]; got.State != MonthRedeemed {
		t.Errorf("month = %+v, want redeemed", got)
	}
	// The sharer has been paying for months: its month stays ready.
	subs.m[hh] = Entitlement{HouseholdID: hh, Since: now.Add(-100 * 24 * time.Hour), ExpiresAt: now.Add(month), LastEventAt: now}
	hm := h.months(hh)[0]
	if _, err := s.RevealFreeMonth(t.Context(), hh, hm.ID); err != nil {
		t.Fatal(err)
	}
	if got := h.months(hh)[0]; got.State != MonthReady {
		t.Errorf("sharer month = %+v, want ready", got)
	}
}

// Only the household's own members see its code; nothing else ever holds
// one, logs included.
func TestFreeMonthRevealRoute(t *testing.T) {
	now := time.Now().UTC()
	s, _, _, _ := referralService(t, now)
	h := harnessOf(s)
	logs := logTo(s)
	qualifyOne(t, s)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: s, Authorizer: twoHouseholds{}, Tokens: fakeTokens{}, Operators: []string{"op"},
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	}).Mount)
	do := func(method, path, user, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	mine, theirs := h.months(hhB)[0], h.months(hh)[0]
	rec := do(http.MethodPost, "/households/"+hhB+"/plus/free-months/"+mine.ID+"/reveal", "u2", "")
	var got RevealResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK || got.Code == "" ||
		got.CopiedText != FreeMonthCopiedText || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("reveal = %d %s", rec.Code, rec.Body.String())
	}
	if want := "https://apps.apple.com/redeem?code=" + got.Code + "&ctx=offercodes&id=1234567890"; got.RedeemURL != want {
		t.Errorf("redeem url = %q, want %q", got.RedeemURL, want)
	}
	again := do(http.MethodPost, "/households/"+hhB+"/plus/free-months/"+mine.ID+"/reveal", "u2", "")
	if !strings.Contains(again.Body.String(), got.Code) {
		t.Errorf("a second reveal gave another code: %s", again.Body.String())
	}
	if h.months(hhB)[0].RevealedAt.IsZero() {
		t.Error("reveal wasn't recorded")
	}
	// Another household's month, through this household or that one.
	if rec := do(http.MethodPost, "/households/"+hhB+"/plus/free-months/"+theirs.ID+"/reveal", "u2", ""); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), "free_month_not_found") {
		t.Errorf("someone else's month = %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodPost, "/households/"+hh+"/plus/free-months/"+theirs.ID+"/reveal", "u2", ""); rec.Code == http.StatusOK {
		t.Errorf("a non-member revealed a code: %s", rec.Body.String())
	}
	if rec := do(http.MethodPost, "/households/"+hhB+"/plus/free-months/nope/reveal", "u2", ""); rec.Code != http.StatusNotFound {
		t.Errorf("unknown month = %d", rec.Code)
	}
	// The status, the referral screen and the logs never hold it.
	for _, path := range []string{"/households/" + hhB + "/plus", "/households/" + hhB + "/plus/referral"} {
		if rec := do(http.MethodGet, path, "u2", ""); strings.Contains(rec.Body.String(), got.Code) {
			t.Errorf("%s holds the code", path)
		}
	}
	if strings.Contains(logs.String(), got.Code) {
		t.Errorf("the log holds the code")
	}

	// An owed month is a 409 with the words to show.
	owed, _ := h.offers.AddMonth(t.Context(), FreeMonth{HouseholdID: hhB, ReferralID: "x", Role: RoleReferrer, EarnedAt: now})
	h.offers.mu.Lock()
	for i := range h.offers.codes {
		h.offers.codes[i].state = codeGiven
	}
	h.offers.mu.Unlock()
	rec = do(http.MethodPost, "/households/"+hhB+"/plus/free-months/"+owed.ID+"/reveal", "u2", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "free_month_on_the_way") ||
		!strings.Contains(rec.Body.String(), "We’ll let you know when it’s ready.") {
		t.Errorf("owed = %d %s", rec.Code, rec.Body.String())
	}
}

// Staff import Apple's CSV. Only operators; duplicates and headers are
// skipped; neither the answer, the stock, nor the log holds a code.
func TestOfferCodeImportRoutes(t *testing.T) {
	now := time.Now().UTC()
	s, _, _, _ := referralService(t, now)
	h := harnessOf(s)
	h.offers.codes, h.offers.keys = nil, map[string]bool{}
	logs := logTo(s)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: s, Authorizer: twoHouseholds{}, Tokens: fakeTokens{}, Operators: []string{"op"},
		Logger: slog.New(slog.NewTextHandler(logs, nil)),
	}).Mount)
	do := func(method, path, user, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+user)
		req.Header.Set("Content-Type", "text/csv")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	csv := "Code\r\nQWER1234TYUI\r\nasdf5678ghjk\r\n\r\nQWER1234TYUI\r\nnot a code\r\n"
	q := "/admin/plus/offer-codes?offer=Invite+free+month&batch=2026-11-a&expiresAt=" + now.Add(90*24*time.Hour).Format(time.DateOnly)
	if rec := do(http.MethodPost, q, "u1", csv); rec.Code != http.StatusForbidden {
		t.Errorf("member import = %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/admin/plus/offer-codes/stock", "u1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("member stock = %d", rec.Code)
	}
	for name, path := range map[string]string{
		"no offer":     "/admin/plus/offer-codes?batch=a&expiresAt=2030-01-01",
		"no batch":     "/admin/plus/offer-codes?offer=a&expiresAt=2030-01-01",
		"no expiry":    "/admin/plus/offer-codes?offer=a&batch=a",
		"expired":      "/admin/plus/offer-codes?offer=a&batch=a&expiresAt=2020-01-01",
		"bad date":     "/admin/plus/offer-codes?offer=a&batch=a&expiresAt=soon",
		"empty upload": "",
	} {
		body := csv
		if path == "" {
			path, body = q, "\n\n"
		}
		if rec := do(http.MethodPost, path, "op", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d %s", name, rec.Code, rec.Body.String())
		}
	}
	rec := do(http.MethodPost, q, "op", csv)
	var res ImportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil || rec.Code != http.StatusOK || res.Added != 2 || res.Skipped != 2 ||
		res.Stock.Available != 2 {
		t.Fatalf("import = %d %s", rec.Code, rec.Body.String())
	}
	// The same file again adds nothing.
	rec = do(http.MethodPost, q, "op", csv)
	res = ImportResponse{}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Added != 0 || res.Duplicates != 2 {
		t.Errorf("again = %s", rec.Body.String())
	}
	rec = do(http.MethodGet, "/admin/plus/offer-codes/stock", "op", "")
	var st OfferStock
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || st.Available != 2 {
		t.Errorf("stock = %d %s", rec.Code, rec.Body.String())
	}
	for _, code := range []string{"QWER1234TYUI", "ASDF5678GHJK"} {
		for where, text := range map[string]string{"stock": rec.Body.String(), "log": logs.String()} {
			if strings.Contains(strings.ToUpper(text), code) {
				t.Errorf("the %s holds a code", where)
			}
		}
	}
}

func TestParseOfferCodes(t *testing.T) {
	codes, skipped := ParseOfferCodes("\ufeffOffer Code\nAB12CD34EF\n ab12cd34ef \n\"GH56IJ78KL\",Invite\nABCDEFGHJK\nshort\nbad-code!\n\n")
	if strings.Join(codes, ",") != "AB12CD34EF,GH56IJ78KL,ABCDEFGHJK" || skipped != 3 {
		t.Errorf("codes = %v, skipped %d", codes, skipped)
	}
	if OfferCodeKey("ab12cd34ef") != OfferCodeKey(" AB12CD34EF") || strings.Contains(OfferCodeKey("AB12CD34EF"), "AB12") {
		t.Error("keys must match without case and hide the code")
	}
	if RedeemURL("", "X") != "" {
		t.Error("no app ID, no link")
	}
}

// The dev route fills the local pool for UI tests.
func TestDevOfferCodes(t *testing.T) {
	now := time.Now().UTC()
	s, _, _, _ := referralService(t, now)
	h := harnessOf(s)
	h.offers.codes, h.offers.keys = nil, map[string]bool{}
	qualifyOne(t, s)
	r := chi.NewRouter()
	r.Route("/api/v1", NewDevOfferCodesHandler(s, fakeTokens{}, nil).Mount)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/plus/dev/offer-codes", strings.NewReader(`{"count":4}`))
	req.Header.Set("Authorization", "Bearer token-u1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"added":4`) || !strings.Contains(rec.Body.String(), `"filled":2`) {
		t.Fatalf("seed = %d %s", rec.Code, rec.Body.String())
	}
	h.oneReadyMonth(hh, hhB)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/plus/dev/offer-codes", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d", rec.Code)
	}
}
