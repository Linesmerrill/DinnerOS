package plus

import (
	"context"
	"fmt"
	"slices"
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

// recipePhrase is "add a recipe" or "add 3 recipes".
func (r ReferralRules) recipePhrase() string {
	if r.MinRecipes == 1 {
		return "add a recipe"
	}
	return fmt.Sprintf("add %d recipes", r.MinRecipes)
}

// recipeStep is the checklist's recipe step, which says the ways to add one
// (decision 706).
func (r ReferralRules) recipeStep() string {
	if r.MinRecipes == 1 {
		return "Add a recipe: write your own, import one from a website, or pick one from Shared Recipes."
	}
	return fmt.Sprintf("Add %d recipes: write your own, import them from a website, or pick them from Shared Recipes.", r.MinRecipes)
}

// PlusExplainerTitle and PlusExplainerLines say what Plus is, at the top of
// the free month checklist (decision 708). They name what Plus really does:
// Autopilot plans the week, and each week's grocery cost and savings.
const PlusExplainerTitle = "What is DinnerOS Plus?"

// PlusExplainerLines are the lines under PlusExplainerTitle.
var PlusExplainerLines = []string{
	"Plus plans your week for you.",
	"Autopilot picks dinners your household will like and fits them to your busy nights.",
	"You also see what each week costs and what you saved.",
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

// HowItWorksRedeem is the last step: the free month is a subscription
// offer, so it says so before anyone earns one (decision 755).
const HowItWorksRedeem = "Redeem it through Apple: Plus starts with the first month free, then renews at the normal price unless you cancel."

// HowItWorks explains referrals with these rules.
func (r ReferralRules) HowItWorks() HowItWorks {
	return HowItWorks{
		Title: "How it works",
		Steps: []string{
			"Share your code with friends, family, even that one aunt.",
			"They download DinnerOS, start a new household, and enter your code.",
			fmt.Sprintf("Once they %s and %s, the invite counts.", r.dinnerPhrase(), r.recipePhrase()),
			fmt.Sprintf("Within %d hours you both get a free month of Plus. We’ll send you a notification when it’s ready.", int(RewardWithin.Hours())),
			HowItWorksRedeem,
		},
	}
}

// SummaryFooter explains qualified and pending on the referral totals.
func (r ReferralRules) SummaryFooter() string {
	return fmt.Sprintf("An invite counts once the new household goes on to %s and %s. Until then it’s pending, and it expires after %d days.",
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
	// PlusTitle and PlusLines say what Plus is, above the steps.
	PlusTitle string
	PlusLines []string
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
	ProgressWaiting   = "Thanks for setting up. We’ll let you know when your free month is on"
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
		{ID: "recipes", Title: r.recipeStep(), Done: met || setup.Recipes >= r.MinRecipes,
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
		Summary:   fmt.Sprintf("Free month: %d of %d done", done, len(steps)),
		PlusTitle: PlusExplainerTitle, PlusLines: slices.Clone(PlusExplainerLines)}
	// The message names what's left in a few words; the recipe step's
	// title is longer, so it isn't used here.
	phrases := map[string]string{"recipes": r.recipePhrase(), "dinner": r.dinnerPhrase()}
	switch ref.State {
	case StateQualified:
		p.Message = ProgressQualified
	case StateExpired:
		p.Message = ProgressExpired
	default:
		var left []string
		for _, st := range steps {
			if st.Done {
				continue
			}
			if phrase, ok := phrases[st.ID]; ok {
				left = append(left, phrase)
			} else {
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
	if ref.State == StateQualified && s.offerCodes != nil {
		// Since decision 755 the month is a code: ready, or on the way. A
		// referral paid in a date before that keeps its words.
		months, err := s.offerCodes.Months(ctx, ref.ReferredID)
		if err != nil {
			return Progress{}, err
		}
		for _, m := range months {
			if m.ReferralID == ref.ID && m.Role == RoleReferred {
				p.Message = FreeMonthReadyTitle
				if m.State == MonthOwed {
					p.Message = FreeMonthOwedTitle
				}
			}
		}
	}
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
	InviteEarnedAtCap   = "Joined. " + FreeMonthLimitText
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
	case n == 1 && item.Setup.Recipes == 0:
		needs = append(needs, "a recipe")
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
