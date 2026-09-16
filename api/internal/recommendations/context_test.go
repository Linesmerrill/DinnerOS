package recommendations

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestSeasonOf(t *testing.T) {
	for _, tc := range []struct {
		date, zone, want string
	}{
		{"2026-01-15", "America/Denver", "winter"},
		{"2026-03-01", "America/Denver", "spring"},
		{"2026-07-04", "America/New_York", "summer"},
		{"2026-09-17", "America/Denver", "fall"},
		{"2026-12-01", "Europe/London", "winter"},
		{"2026-07-04", "Australia/Sydney", "winter"},
		{"2026-01-15", "America/Argentina/Buenos_Aires", "summer"},
		{"2026-10-10", "Pacific/Auckland", "spring"},
		{"2026-07-04", "", "summer"},
	} {
		if got := seasonOf(date(tc.date), tc.zone); got != tc.want {
			t.Errorf("seasonOf(%s, %q) = %s, want %s", tc.date, tc.zone, got, tc.want)
		}
	}
}

func TestUSHolidays(t *testing.T) {
	want := map[string]Holiday{
		"2026-01-01": {Name: "New Year's Day", Kind: "dayOff"},
		"2026-01-19": {Name: "Martin Luther King Jr. Day", Kind: "dayOff"},
		"2026-02-16": {Name: "Presidents' Day", Kind: "dayOff"},
		"2026-04-05": {Name: "Easter", Kind: "feast"},
		"2026-05-25": {Name: "Memorial Day", Kind: "cookout"},
		"2026-06-19": {Name: "Juneteenth", Kind: "dayOff"},
		"2026-07-04": {Name: "the Fourth of July", Kind: "cookout"},
		"2026-09-07": {Name: "Labor Day", Kind: "cookout"},
		"2026-11-26": {Name: "Thanksgiving", Kind: "feast"},
		"2026-12-24": {Name: "Christmas Eve", Kind: "feast"},
		"2026-12-25": {Name: "Christmas", Kind: "feast"},
		"2026-12-31": {Name: "New Year's Eve", Kind: "feast"},
		"2025-04-20": {Name: "Easter", Kind: "feast"},
		"2024-03-31": {Name: "Easter", Kind: "feast"},
		"2027-03-28": {Name: "Easter", Kind: "feast"},
		"2025-11-27": {Name: "Thanksgiving", Kind: "feast"},
		"2027-05-31": {Name: "Memorial Day", Kind: "cookout"},
	}
	for d, h := range want {
		if got, ok := usHoliday(date(d)); !ok || got != h {
			t.Errorf("usHoliday(%s) = %+v, %v; want %+v", d, got, ok, h)
		}
	}
	// Every other day of 2026 is ordinary, and each holiday appears once.
	count := 0
	for d := date("2026-01-01"); d.Year() == 2026; d = d.AddDate(0, 0, 1) {
		if _, ok := usHoliday(d); ok {
			count++
		}
	}
	if count != 12 {
		t.Errorf("2026 has %d holidays, want 12", count)
	}
	for _, d := range []string{"2026-11-19", "2026-05-18", "2026-09-14", "2026-01-12"} {
		if h, ok := usHoliday(date(d)); ok {
			t.Errorf("usHoliday(%s) = %+v", d, h)
		}
	}
}

func TestWeekContextSignals(t *testing.T) {
	w := mustWeek(t, "2026-W48") // Thanksgiving week: Mon 2026-11-23
	c := weekContext(w, "America/Denver", "sat", DeviceSignals{Days: []DeviceDaySignals{{Day: "tue", Busyness: "busy", EveningFreeMinutes: 25}, {Day: "wed", TemperatureBand: "cold", Precipitation: "snow"}}})
	if c.Season != "fall" || c.OrderDate != "2026-11-28" || len(c.Holidays) != 1 || c.Holidays[0] != (Holiday{Day: "thu", Name: "Thanksgiving", Kind: "feast"}) {
		t.Fatalf("weekContext() = %+v", c)
	}
	wc := WeekContext{Days: []DayOverride{{Day: "tue", MaxMinutes: 30}}}.providerContext(nil)
	c.apply(&wc)
	if wc.Signals[autopilot.ContextSeason].Text != "fall" || wc.Signals[autopilot.ContextOrderDate].Text != "2026-11-28" {
		t.Errorf("week signals = %+v", wc.Signals)
	}
	byDay := map[autopilot.Day]autopilot.DayContext{}
	for _, d := range wc.Days {
		byDay[d.Day] = d
	}
	if tue := byDay["tue"]; tue.MaxMinutes != 30 || tue.Signals[autopilot.ContextBusyness].Text != "busy" || tue.Signals[autopilot.ContextEveningFreeMinutes].Number != 25 {
		t.Errorf("tue = %+v; the day's own override must be kept", tue)
	}
	if wed := byDay["wed"]; wed.Signals[autopilot.ContextTemperatureBand].Text != "cold" || wed.Signals[autopilot.ContextPrecipitation].Text != "snow" {
		t.Errorf("wed = %+v", wed)
	}
	if thu := byDay["thu"]; thu.Signals[autopilot.ContextHoliday].Text != "Thanksgiving" || thu.Signals[autopilot.ContextHolidayKind].Text != "feast" {
		t.Errorf("thu = %+v", thu)
	}

	// Nothing known: no signals at all.
	empty := weekContext(mustWeek(t, "2026-W38"), "", "", DeviceSignals{})
	awc := autopilot.WeekContext{}
	empty.apply(&awc)
	if len(empty.Holidays) != 0 || empty.OrderDate != "" || len(awc.Days) != 0 {
		t.Errorf("empty context = %+v, %+v", empty, awc)
	}
}

func TestNormalizeDeviceSignals(t *testing.T) {
	got, err := normalizeDeviceSignals(DeviceSignals{Days: []DeviceDaySignals{
		{Day: "thu", Precipitation: "rain"}, {Day: "mon", Busyness: "free"}, {Day: "tue"},
	}})
	if err != nil || len(got.Days) != 2 || got.Days[0].Day != "mon" || got.Days[1].Day != "thu" {
		t.Errorf("normalizeDeviceSignals() = %+v, %v; want ordered, with empty days dropped", got, err)
	}
	for _, bad := range []DeviceDaySignals{
		{Day: "someday", Busyness: "busy"},
		{Day: "mon", Busyness: "slammed"},
		{Day: "mon", EveningFreeMinutes: -5},
		{Day: "mon", EveningFreeMinutes: MaxEveningFreeMinutes + 1},
		{Day: "mon", TemperatureBand: "balmy"},
		{Day: "mon", Precipitation: "hail"},
	} {
		if _, err := normalizeDeviceSignals(DeviceSignals{Days: []DeviceDaySignals{bad}}); !errors.Is(err, ErrInvalid) {
			t.Errorf("normalizeDeviceSignals(%+v) error = %v, want invalid", bad, err)
		}
	}
	if _, err := normalizeDeviceSignals(DeviceSignals{Days: []DeviceDaySignals{{Day: "mon", Busyness: "busy"}, {Day: "mon", Precipitation: "rain"}}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a repeated day error = %v", err)
	}
}

// TestGenerateWithDeviceSignals: signals from the device shape the week, are
// explained, are kept with the proposal, and are reused by swaps.
func TestGenerateWithDeviceSignals(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.svc.UpdateProfile(ctx, hhA, userAda, ProfileUpdate{
		Schedule: &Schedule{PlanDays: []string{"mon", "tue", "wed"}, Weeknights: []string{}, MealsPerWeek: 3},
	}, false); err != nil {
		t.Fatal(err)
	}
	signals := DeviceSignals{Days: []DeviceDaySignals{{Day: "tue", Busyness: "busy"}, {Day: "wed", TemperatureBand: "cold", Precipitation: "rain"}}}
	p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{Signals: signals})
	if err != nil {
		t.Fatal(err)
	}
	if p.Context.Season != "fall" || len(p.Context.Device.Days) != 2 {
		t.Errorf("proposal context = %+v", p.Context)
	}
	if !slices.ContainsFunc(p.Messages, func(m Message) bool { return m.Text == "Planned around your calendar and the forecast." }) {
		t.Errorf("messages = %+v", p.Messages)
	}
	tue := p.Slots[p.slot("tue")]
	if tue.TimeBand == "long" || !slices.ContainsFunc(tue.Reasons, func(r Reason) bool { return strings.Contains(r.Text, "busy Tuesday evening") }) {
		t.Errorf("tue = %s %s %+v", tue.RecipeName, tue.TimeBand, tue.Reasons)
	}
	stored, err := env.store.GetProposal(ctx, hhA, testWeek)
	if err != nil || len(stored.Context.Device.Days) != 2 {
		t.Fatalf("stored = %+v, %v", stored.Context, err)
	}
	swapped, err := env.svc.Swap(ctx, hhA, userAda, testWeek, "tue", p.Version)
	if err != nil {
		t.Fatal(err)
	}
	if next := swapped.Slots[swapped.slot("tue")]; next.Signals["context"] == 0 && next.TimeBand != "medium" {
		t.Errorf("the swap ignored the stored signals: %+v", next)
	}
	if len(swapped.Context.Device.Days) != 2 {
		t.Errorf("swap dropped the context: %+v", swapped.Context)
	}

	if _, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{Signals: DeviceSignals{Days: []DeviceDaySignals{{Day: "tue", Busyness: "hectic"}}}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Generate(invalid signals) error = %v", err)
	}
}

func TestGenerateSignalsOverHTTP(t *testing.T) {
	router, _ := newTestRouter(t)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		return do(t, router, method, path, body, userAda)
	}
	rec := call(http.MethodPost, "/weeks/"+testWeek+"/generate", `{"signals":{"days":[{"day":"wed","busyness":"busy","eveningFreeMinutes":30,"temperatureBand":"hot","precipitation":"none"}]}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("generate = %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`"context":{"season":"fall","orderDate":null,"holidays":[]`, `"signals":{"days":[{"day":"wed","busyness":"busy","eveningFreeMinutes":30,"temperatureBand":"hot","precipitation":"none"}]}`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s: %s", want, body)
		}
	}
	for _, bad := range []string{
		`{"signals":{"days":[{"day":"wed","busyness":"swamped"}]}}`,
		`{"signals":{"days":[{"day":"wed","eveningFreeMinutes":0}]}}`,
		`{"signals":{"days":[{"day":"wed","latitude":40.1}]}}`,
		`{"signals":{"days":[{"day":"mon"},{"day":"tue"},{"day":"wed"},{"day":"thu"},{"day":"fri"},{"day":"sat"},{"day":"sun"},{"day":"mon"}]}}`,
	} {
		if rec := call(http.MethodPost, "/weeks/"+testWeek+"/generate", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("generate %s = %d %s", bad, rec.Code, rec.Body)
		}
	}
}
