package plus

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The words around referrals (decision 698). They come from the same rules
// the qualify check uses, so changing a threshold changes the copy.

// dinnerPhrase is "plan a dinner" or "plan 2 dinners".
func (r ReferralRules) dinnerPhrase() string {
	if r.MinDinners == 1 {
		return "plan a dinner"
	}
	return fmt.Sprintf("plan %d dinners", r.MinDinners)
}

// recipePhrase is "save a recipe" or "save 3 recipes".
func (r ReferralRules) recipePhrase() string {
	if r.MinRecipes == 1 {
		return "save a recipe"
	}
	return fmt.Sprintf("save %d recipes", r.MinRecipes)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// HowItWorks is the invite screen's explanation, above the code.
type HowItWorks struct {
	Title string
	Steps []string
}

// HowItWorks explains referrals with these rules.
func (r ReferralRules) HowItWorks() HowItWorks {
	return HowItWorks{
		Title: "How it works",
		Steps: []string{
			"Share your code with friends, family, even that one aunt.",
			"They download DinnerOS, start a new household, and enter your code.",
			fmt.Sprintf("Once they %s and %s, the invite counts.", r.dinnerPhrase(), r.recipePhrase()),
			fmt.Sprintf("Within %d hours you both get a free month of Plus. We'll send you a notification when it's on.", int(RewardWithin.Hours())),
		},
	}
}

// SummaryFooter explains qualified and pending on the referral totals.
func (r ReferralRules) SummaryFooter() string {
	return fmt.Sprintf("An invite counts once the new household goes on to %s and %s. Until then it's pending, and it expires after %d days.",
		r.dinnerPhrase(), r.recipePhrase(), r.QualifyDays)
}

// Progress is how far the household's own referral is, for the household
// that entered a code.
type Progress struct {
	// State is pending, qualified, or expired (ready shows as pending).
	State   string
	Message string
	Steps   []ProgressStep
	// ExpiresAt is set while it's pending and setup isn't done.
	ExpiresAt time.Time
	// Summary is the Menu card's line: "Free month: 1 of 3 done".
	Summary string
	// TimeLeft is "14 days left" while ExpiresAt is set; set by the service,
	// which knows the time.
	TimeLeft string
}

// ProgressStep is one thing the household does for its free month.
type ProgressStep struct {
	ID            string
	Title         string
	Done          bool
	Count, Target int
}

// Progress messages.
const (
	ProgressWaiting   = "Thanks for setting up. We'll let you know when your free month is on"
	ProgressQualified = "Your free month of Plus is on"
	ProgressExpired   = "This invite has expired"
	// StepHousehold is the first step, done by the time a code is in.
	StepHousehold = "Create your household"
)

// TimeLeftText is "14 days left", "1 day left", or "Last day".
func TimeLeftText(expires, now time.Time) string {
	if expires.IsZero() {
		return ""
	}
	left := expires.Sub(now)
	if left < 24*time.Hour {
		return "Last day"
	}
	days := int((left + 24*time.Hour - 1) / (24 * time.Hour))
	if days == 1 {
		return "1 day left"
	}
	return fmt.Sprintf("%d days left", days)
}

func publicState(s ReferralState) string {
	if s.Open() {
		return string(StatePending)
	}
	return string(s)
}

// ProgressFor is ref's progress with setup. Nothing in it says which
// anti-farming check a referral is waiting on.
func (r ReferralRules) ProgressFor(ref Referral, setup Setup) Progress {
	met := !ref.SetupMetAt.IsZero()
	// In the order a new household does them. A code needs a household, so
	// that step is always done.
	steps := []ProgressStep{
		{ID: "household", Title: StepHousehold, Done: true, Count: 1, Target: 1},
		{ID: "recipes", Title: capitalize(r.recipePhrase()), Done: met || setup.Recipes >= r.MinRecipes,
			Count: min(setup.Recipes, r.MinRecipes), Target: r.MinRecipes},
		{ID: "dinner", Title: capitalize(r.dinnerPhrase()), Done: met || setup.Dinners >= r.MinDinners,
			Count: min(setup.Dinners, r.MinDinners), Target: r.MinDinners},
	}
	if met || ref.State == StateQualified {
		for i := range steps {
			steps[i].Done, steps[i].Count = true, steps[i].Target
		}
	}
	done := 0
	for _, st := range steps {
		if st.Done {
			done++
		}
	}
	p := Progress{State: publicState(ref.State), Steps: steps,
		Summary: fmt.Sprintf("Free month: %d of %d done", done, len(steps))}
	switch ref.State {
	case StateQualified:
		p.Message = ProgressQualified
	case StateExpired:
		p.Message = ProgressExpired
	default:
		var left []string
		for _, st := range steps {
			if !st.Done {
				left = append(left, strings.ToLower(st.Title[:1])+st.Title[1:])
			}
		}
		if len(left) == 0 {
			p.Message = ProgressWaiting
		} else {
			p.Message = capitalize(strings.Join(left, " and ")) + " to unlock your free month"
			p.ExpiresAt = ref.ExpiresAt
		}
	}
	return p
}

func (s *Service) progress(ctx context.Context, ref Referral) (Progress, error) {
	var setup Setup
	if s.setup != nil && ref.State.Open() {
		var err error
		if setup, err = s.setup.Get(ctx, ref.ReferredID); err != nil {
			return Progress{}, err
		}
	}
	p := s.rules.ProgressFor(ref, setup)
	p.TimeLeft = TimeLeftText(p.ExpiresAt, s.now())
	return p, nil
}

// ReferralProgress is the progress of the referral householdID made by
// entering a code; nil without one.
func (s *Service) ReferralProgress(ctx context.Context, householdID string) (*Progress, error) {
	if s.referrals == nil || !s.EnabledFor(householdID) {
		return nil, nil
	}
	ref, ok, err := s.referrals.UsedBy(ctx, householdID)
	if err != nil || !ok {
		return nil, err
	}
	p, err := s.progress(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// Invite details, as the sharer sees them.
const (
	InviteCountsSoon    = "Counts within 24 hours"
	InviteEarned        = "Free month earned"
	InviteEarnedAtCap   = "Joined. You already have the most free months."
	InviteExpired       = "Expired"
	InviteStatusPending = "Pending"
)

// InviteDetail is the sharer's state and line for one invite. An invite
// waiting on an anti-farming check is plain "pending" with no detail.
func (r ReferralRules) InviteDetail(item InviteItem) (state, detail string) {
	switch item.State {
	case StateQualified:
		if item.ReferrerRewarded {
			return string(StateQualified), InviteEarned
		}
		return string(StateQualified), InviteEarnedAtCap
	case StateExpired:
		return string(StateExpired), InviteExpired
	}
	if item.Checks != ChecksPassed {
		return string(StatePending), ""
	}
	if !item.SetupMetAt.IsZero() {
		return string(StatePending), InviteCountsSoon
	}
	var needs []string
	switch d := r.MinDinners - item.Setup.Dinners; {
	case d == 1:
		needs = append(needs, "a dinner planned")
	case d > 1 && item.Setup.Dinners == 0:
		needs = append(needs, fmt.Sprintf("%d dinners planned", d))
	case d > 1:
		needs = append(needs, fmt.Sprintf("%d more dinners planned", d))
	}
	switch n := r.MinRecipes - item.Setup.Recipes; {
	case n == 1:
		needs = append(needs, "1 more recipe")
	case n > 1:
		needs = append(needs, fmt.Sprintf("%d more recipes", n))
	}
	if len(needs) == 0 {
		return string(StatePending), InviteCountsSoon
	}
	return string(StatePending), "Needs " + strings.Join(needs, " and ")
}
