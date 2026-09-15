package events

import (
	"errors"
	"fmt"
	"strings"
)

// Autopilot pairings suggest an add-on recipe or a grocery item with a main
// meal (docs/autopilot.md#add-on-pairings). Their events are server-observed.
// A target is identified by its key: "recipe:<id>" for an add-on recipe, or
// "grocery:<normalized name>" for a grocery item. Grocery item names are short
// product names ("club crackers"), like grocery.item_checked's name.

// Allowed values for pairing payloads.
var (
	PairingKinds       = []string{"recipe", "grocery_item"}
	PairingSources     = []string{"rule", "learned"}
	PairingFrequencies = []string{"always", "suggest"}
	// PairingDismissReasons: dismissed by a member, or left out when
	// accepting a proposal that included it.
	PairingDismissReasons = []string{"dismissed", "excluded"}
)

// maxPairingKey bounds a target key: "grocery:" and a 60-character name.
const maxPairingKey = 80

// PairingSuggested is the payload of pairing.suggested: Autopilot offered a
// pairing with a main meal. Event.RecipeID is the main meal; EntryID is its
// plan entry, or ProposalID and SlotID its proposed slot.
type PairingSuggested struct {
	Key          string  `json:"key" bson:"key"`
	Kind         string  `json:"kind" bson:"kind"`
	Source       string  `json:"source" bson:"source"`
	Frequency    string  `json:"frequency" bson:"frequency"`
	MealCategory string  `json:"mealCategory,omitempty" bson:"mealCategory,omitempty"`
	RuleID       string  `json:"ruleId,omitempty" bson:"ruleId,omitempty"`
	Confidence   float64 `json:"confidence,omitempty" bson:"confidence,omitempty"`
	EntryID      string  `json:"entryId,omitempty" bson:"entryId,omitempty"`
	ProposalID   string  `json:"proposalId,omitempty" bson:"proposalId,omitempty"`
	SlotID       string  `json:"slotId,omitempty" bson:"slotId,omitempty"`
	Day          string  `json:"day,omitempty" bson:"day,omitempty"`
}

// PairingAccepted is the payload of pairing.accepted: a pairing was added to
// the week, as a plan entry (AddedEntryID) or a grocery item (GroceryItemID).
type PairingAccepted struct {
	Key           string  `json:"key" bson:"key"`
	Kind          string  `json:"kind" bson:"kind"`
	Source        string  `json:"source" bson:"source"`
	Frequency     string  `json:"frequency" bson:"frequency"`
	MealCategory  string  `json:"mealCategory,omitempty" bson:"mealCategory,omitempty"`
	RuleID        string  `json:"ruleId,omitempty" bson:"ruleId,omitempty"`
	Confidence    float64 `json:"confidence,omitempty" bson:"confidence,omitempty"`
	EntryID       string  `json:"entryId,omitempty" bson:"entryId,omitempty"`
	ProposalID    string  `json:"proposalId,omitempty" bson:"proposalId,omitempty"`
	SlotID        string  `json:"slotId,omitempty" bson:"slotId,omitempty"`
	Day           string  `json:"day,omitempty" bson:"day,omitempty"`
	AddedEntryID  string  `json:"addedEntryId,omitempty" bson:"addedEntryId,omitempty"`
	GroceryItemID string  `json:"groceryItemId,omitempty" bson:"groceryItemId,omitempty"`
}

// PairingDismissed is the payload of pairing.dismissed: a member dismissed a
// pairing for a meal this week, or left out one a proposal included.
type PairingDismissed struct {
	Key          string  `json:"key" bson:"key"`
	Kind         string  `json:"kind" bson:"kind"`
	Source       string  `json:"source" bson:"source"`
	Frequency    string  `json:"frequency" bson:"frequency"`
	MealCategory string  `json:"mealCategory,omitempty" bson:"mealCategory,omitempty"`
	RuleID       string  `json:"ruleId,omitempty" bson:"ruleId,omitempty"`
	Confidence   float64 `json:"confidence,omitempty" bson:"confidence,omitempty"`
	EntryID      string  `json:"entryId,omitempty" bson:"entryId,omitempty"`
	ProposalID   string  `json:"proposalId,omitempty" bson:"proposalId,omitempty"`
	SlotID       string  `json:"slotId,omitempty" bson:"slotId,omitempty"`
	Day          string  `json:"day,omitempty" bson:"day,omitempty"`
	// Reason is one of PairingDismissReasons.
	Reason string `json:"reason" bson:"reason"`
}

// PairingRuleCreated is the payload of pairing.rule_created: a member turned
// a learned pairing into a household rule. Merged is true when the category
// was added to an existing rule for the same target.
type PairingRuleCreated struct {
	RuleID       string  `json:"ruleId" bson:"ruleId"`
	Key          string  `json:"key" bson:"key"`
	Kind         string  `json:"kind" bson:"kind"`
	MealCategory string  `json:"mealCategory" bson:"mealCategory"`
	Frequency    string  `json:"frequency" bson:"frequency"`
	Confidence   float64 `json:"confidence,omitempty" bson:"confidence,omitempty"`
	Merged       bool    `json:"merged,omitempty" bson:"merged,omitempty"`
}

// EventType implements Payload.
func (PairingSuggested) EventType() Type { return TypePairingSuggested }

// EventType implements Payload.
func (PairingAccepted) EventType() Type { return TypePairingAccepted }

// EventType implements Payload.
func (PairingDismissed) EventType() Type { return TypePairingDismissed }

// EventType implements Payload.
func (PairingRuleCreated) EventType() Type { return TypePairingRuleCreated }

// validPairing checks the fields every pairing payload shares.
func validPairing(key, kind, source, frequency, category, ruleID string, confidence float64) error {
	var errs []error
	switch {
	case key == "" || len(key) > maxPairingKey:
		errs = append(errs, invalid(fmt.Sprintf("key must be 1 to %d characters", maxPairingKey)))
	case !strings.HasPrefix(key, "recipe:") && !strings.HasPrefix(key, "grocery:"):
		errs = append(errs, invalid("key must start with recipe: or grocery:"))
	}
	if kind == "" {
		errs = append(errs, invalid("kind is required"))
	}
	if frequency == "" {
		errs = append(errs, invalid("frequency is required"))
	}
	if len(category) > 32 {
		errs = append(errs, invalid("mealCategory must be at most 32 characters"))
	}
	if confidence < 0 || confidence > 1 {
		errs = append(errs, invalid("confidence must be between 0 and 1"))
	}
	return errors.Join(append(errs, optionalEnum("kind", kind, PairingKinds), optionalEnum("source", source, PairingSources),
		optionalEnum("frequency", frequency, PairingFrequencies), validID("ruleId", ruleID, false))...)
}

// validPairingSubject checks the main meal a pairing is for.
func validPairingSubject(source, entryID, proposalID, slotID, day string) error {
	var errs []error
	if source == "" {
		errs = append(errs, invalid("source is required"))
	}
	return errors.Join(append(errs, validEntryID(entryID), validID("proposalId", proposalID, false), validID("slotId", slotID, false),
		optionalEnum("day", day, PlanDays))...)
}

func (p PairingSuggested) validate() error {
	return errors.Join(validPairing(p.Key, p.Kind, p.Source, p.Frequency, p.MealCategory, p.RuleID, p.Confidence),
		validPairingSubject(p.Source, p.EntryID, p.ProposalID, p.SlotID, p.Day))
}

func (p PairingAccepted) validate() error {
	return errors.Join(validPairing(p.Key, p.Kind, p.Source, p.Frequency, p.MealCategory, p.RuleID, p.Confidence),
		validPairingSubject(p.Source, p.EntryID, p.ProposalID, p.SlotID, p.Day),
		validEntryID(p.AddedEntryID), validID("groceryItemId", p.GroceryItemID, false))
}

func (p PairingDismissed) validate() error {
	var reason error
	if p.Reason == "" {
		reason = invalid("reason is required")
	}
	return errors.Join(validPairing(p.Key, p.Kind, p.Source, p.Frequency, p.MealCategory, p.RuleID, p.Confidence),
		validPairingSubject(p.Source, p.EntryID, p.ProposalID, p.SlotID, p.Day), reason,
		optionalEnum("reason", p.Reason, PairingDismissReasons))
}

func (p PairingRuleCreated) validate() error {
	var category error
	if p.MealCategory == "" {
		category = invalid("mealCategory is required")
	}
	return errors.Join(validPairing(p.Key, p.Kind, "learned", p.Frequency, p.MealCategory, p.RuleID, p.Confidence),
		validID("ruleId", p.RuleID, true), category)
}
