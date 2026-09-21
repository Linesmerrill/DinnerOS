package mealkit

import (
	"regexp"
	"time"
)

// A household's order history is longer than one harvest can read.
//
// The app walks the account's past deliveries in the member's own browser
// session, a page at a time, and a page covers roughly four to five delivered
// weeks — measured against a real account: 160 weeks came back in 40 pages.
// The walk stops at its own page cap, so a household with four years of
// history cannot finish in one sitting: 740 recipes across 160 weeks was
// exactly that, and it stopped on the cap.
//
// So a harvest says where it stopped, and the server remembers it. The next
// harvest resumes from the earliest week already seen instead of walking back
// from today again. What is stored is a pair of ISO weeks and a flag — no
// account, no session, nothing about the member's meal-kit sign-in, which
// stays true of this package (doc.go).

// weekPattern is an ISO week as the import contract writes it, e.g.
// "2026-W38". Zero-padded, so two of them compare correctly as strings.
var weekPattern = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)

// ValidWeek reports whether w is an ISO week this package will store.
func ValidWeek(w string) bool { return weekPattern.MatchString(w) }

// HarvestStop is why a harvest stopped walking the order history. It is the
// difference between "that is all there is" and "there is more, come back".
type HarvestStop string

// The ways a walk ends.
const (
	// HarvestStopCap: the walk hit its own page cap. There is more history.
	HarvestStopCap HarvestStop = "cap"
	// HarvestStopEmpty: a page came back with no delivered weeks at all,
	// which is the start of the account's history.
	HarvestStopEmpty HarvestStop = "empty"
	// HarvestStopEnd: the walk stopped moving backwards — the same page
	// again, or a week it could not read. Treated as the end of history.
	HarvestStopEnd HarvestStop = "end"
	// HarvestStopCaughtUp: the walk reached weeks the server has already
	// seen. It is how a catch-up pass for new deliveries ends, and it says
	// nothing about whether the *oldest* history is finished.
	HarvestStopCaughtUp HarvestStop = "caught_up"
)

// KnownStop reports whether s is a stop reason this build understands.
func KnownStop(s HarvestStop) bool {
	switch s {
	case HarvestStopCap, HarvestStopEmpty, HarvestStopEnd, HarvestStopCaughtUp:
		return true
	default:
		return false
	}
}

// ReachedStartOfHistory reports whether stopping this way means the walk saw
// the beginning of the account's history. Hitting the cap does not, and
// catching up with what we already had says nothing either way.
func (s HarvestStop) ReachedStartOfHistory() bool {
	return s == HarvestStopEmpty || s == HarvestStopEnd
}

// MoreToFetch reports whether the member should be told there is history this
// harvest did not reach.
func (s HarvestStop) MoreToFetch() bool { return s == HarvestStopCap }

// HarvestReport is where one harvest stopped: the oldest and newest delivered
// weeks it reached, how much it walked, and why it stopped.
//
// It arrives from a client with the order history, so every field is
// untrusted input. Validate is the door.
type HarvestReport struct {
	// EarliestWeek is the oldest ISO week this harvest reached, and becomes
	// the floor the next one resumes from.
	EarliestWeek string
	// LatestWeek is the newest ISO week it saw.
	LatestWeek string
	// Pages is how many past-deliveries pages it read; Weeks is how many
	// delivered weeks those pages carried.
	Pages int
	Weeks int
	// Stopped is why it stopped.
	Stopped HarvestStop
}

// MaxHarvestPages bounds the page count a client may claim. It is generous
// next to the app's own cap of 40; it exists so a stored number cannot be
// absurd.
const MaxHarvestPages = 500

// MaxHarvestWeeks bounds the week count a client may claim: about 40 years of
// weekly deliveries.
const MaxHarvestWeeks = 2080

// Validate returns the report as it will be stored, or a ValidationError. An
// absent report is valid and means "this client did not say": nothing is then
// marked complete, which is the safe direction.
func (r HarvestReport) Validate() (HarvestReport, error) {
	out := HarvestReport{Stopped: r.Stopped, Pages: r.Pages, Weeks: r.Weeks}
	if out.Stopped != "" && !KnownStop(out.Stopped) {
		return HarvestReport{}, invalid("that is not a way a harvest stops")
	}
	for _, pair := range []struct {
		in  string
		out *string
	}{{r.EarliestWeek, &out.EarliestWeek}, {r.LatestWeek, &out.LatestWeek}} {
		if pair.in == "" {
			continue
		}
		if !ValidWeek(pair.in) {
			return HarvestReport{}, invalid("%q is not an ISO week", pair.in)
		}
		*pair.out = pair.in
	}
	if out.EarliestWeek != "" && out.LatestWeek != "" && out.LatestWeek < out.EarliestWeek {
		out.EarliestWeek, out.LatestWeek = out.LatestWeek, out.EarliestWeek
	}
	if out.Pages < 0 || out.Pages > MaxHarvestPages {
		return HarvestReport{}, invalid("that is not a plausible number of history pages")
	}
	if out.Weeks < 0 || out.Weeks > MaxHarvestWeeks {
		return HarvestReport{}, invalid("that is not a plausible number of delivered weeks")
	}
	return out, nil
}

// Cursor is what the server remembers about one household's order history on
// one source, so the next harvest resumes instead of starting over.
//
// It is two ISO weeks and two flags. There is nothing here about the
// meal-kit account, and there is nowhere in it to put one.
type Cursor struct {
	HouseholdID string
	Source      string
	// EarliestWeek is the oldest delivered week any harvest has reached. The
	// next harvest starts the week before it.
	EarliestWeek string
	// LatestWeek is the newest delivered week seen. A catch-up pass walks
	// back from today only until it reaches this, so a new delivery is picked
	// up without re-reading four years.
	LatestWeek string
	// Complete is set once a harvest reached the start of the account's
	// history. It never goes back to false: a finished history stays
	// finished, and later harvests only catch up on new deliveries.
	Complete bool
	// BlockedAt is when the source last refused us. A new import inside
	// BlockedCooldown is refused rather than queued, so a refusal is backed
	// off from hard instead of retried by hand.
	BlockedAt time.Time
	UpdatedAt time.Time
}

// BlockedCooldown is how long an import stays refused after the source turned
// us away. It is deliberately long: being blocked is the one failure this
// system must not argue with.
const BlockedCooldown = 6 * time.Hour

// Blocked reports whether the source refused us recently enough that a new
// run must not be queued.
func (c Cursor) Blocked(now time.Time) bool {
	return !c.BlockedAt.IsZero() && now.Sub(c.BlockedAt) < BlockedCooldown
}

// ResumeFrom is the week a later harvest should walk back from, or "" for
// "start at today". A completed history has nothing to resume.
func (c Cursor) ResumeFrom() string {
	if c.Complete {
		return ""
	}
	return c.EarliestWeek
}

// MoreToFetch reports whether history is known to be left unread.
func (c Cursor) MoreToFetch() bool { return !c.Complete && c.EarliestWeek != "" }

// Merge folds a harvest report into the cursor and returns the result.
//
// The floor only ever moves down and the ceiling only ever moves up, so an
// out-of-order or repeated harvest can never lose ground, and Complete is
// sticky: reaching the start of the history once is enough, and a later
// catch-up pass that stops on the cap does not unset it.
func (c Cursor) Merge(r HarvestReport, at time.Time) Cursor {
	if r.EarliestWeek != "" && (c.EarliestWeek == "" || r.EarliestWeek < c.EarliestWeek) {
		c.EarliestWeek = r.EarliestWeek
	}
	if r.LatestWeek != "" && r.LatestWeek > c.LatestWeek {
		c.LatestWeek = r.LatestWeek
	}
	if r.Stopped.ReachedStartOfHistory() {
		c.Complete = true
	}
	c.UpdatedAt = at
	return c
}
