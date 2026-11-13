package plus

// The Plus offer (decision 675). These are defaults until Merrill sets the
// real products in App Store Connect: the app shows the App Store's own price
// when StoreKit has it and these words otherwise, so a price change in App
// Store Connect needs no API or app change.
const (
	// ProductMonthly and ProductYearly are the App Store product IDs.
	ProductMonthly = "com.linesmerrill.dinneros.plus.monthly"
	ProductYearly  = "com.linesmerrill.dinneros.plus.yearly"
	// TrialDays is the free trial on either plan, set on the products in App
	// Store Connect; Apple decides who still qualifies for it.
	TrialDays = 14
	// FreeMonthDays is what a referral gives each household.
	FreeMonthDays = 30
	// MaxReferralMonths caps the free months one household can earn by
	// inviting others.
	MaxReferralMonths = 12
)

// Feature names what Plus unlocks, for the API's gate and the app's copy.
type Feature string

// Features behind Plus. Sending a list to a store stays free: it is how
// DinnerOS earns its store commission.
const (
	FeatureAutopilot Feature = "autopilot"
	FeatureCost      Feature = "cost"
)

// Plan is one way to pay for Plus.
type Plan struct {
	ProductID string `json:"productId"`
	// Name and PriceText are shown when StoreKit can't give the price.
	Name      string `json:"name"`
	PriceText string `json:"priceText"`
	// Note is shown under the plan, or empty.
	Note string `json:"note,omitempty"`
}

// Offer is the Plus purchase screen's words. The API owns them so they can
// change without an app update.
type Offer struct {
	Title     string   `json:"title"`
	Subtitle  string   `json:"subtitle"`
	Benefits  []string `json:"benefits"`
	Plans     []Plan   `json:"plans"`
	TrialDays int      `json:"trialDays"`
	// TrialText is the button when a trial is offered; BuyText otherwise.
	TrialText string `json:"trialText"`
	BuyText   string `json:"buyText"`
	// Terms sits under the button. Apple requires the renewal terms on the
	// purchase screen.
	Terms       string `json:"terms"`
	RestoreText string `json:"restoreText"`
	// FreeText says that sending lists to stores stays free.
	FreeText string `json:"freeText"`
}

// DefaultOffer is the offer shown while Plus is on.
func DefaultOffer() Offer {
	return Offer{
		Title:    "DinnerOS Plus",
		Subtitle: "Let DinnerOS plan your week and track what you spend.",
		Benefits: []string{
			"Autopilot plans the week's dinners for you.",
			"See what each week costs and how much you save.",
			"One plan covers everyone in your household.",
		},
		Plans: []Plan{
			{ProductID: ProductYearly, Name: "Yearly", PriceText: "$39.99 a year", Note: "Save 33%"},
			{ProductID: ProductMonthly, Name: "Monthly", PriceText: "$4.99 a month"},
		},
		TrialDays:   TrialDays,
		TrialText:   "Try it free for 14 days",
		BuyText:     "Get DinnerOS Plus",
		Terms:       "Plus renews automatically until you cancel. Cancel anytime in your Apple account settings at least a day before it renews.",
		RestoreText: "Restore purchases",
		FreeText:    "Sending your list to a store is always free.",
	}
}

// RequiredMessage is the error message when a free household asks for a Plus
// feature.
const RequiredMessage = "This is part of DinnerOS Plus."
