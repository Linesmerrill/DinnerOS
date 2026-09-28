package mealkit

import (
	"errors"
	"net/http"
	"strings"
)

// What a member reads about a recipe that did not make it into the library.
//
// These are the only sentences that reach FailedRecipe.Reason. Everything a
// member sees in the "Couldn't Import" list is one of them: plain, short, and
// free of field names, list indexes, recipe ids, HTTP codes, and the import
// pipeline's own validation wording, all of which go to the log instead. A
// recipe that is merely the same dish as another one in the order history is
// not a failure at all and never gets a reason (worker.go, flush).

// reasonGone is for a recipe the service no longer has a page for.
func reasonGone(source string) string {
	return "This recipe is no longer on " + displayName(source) + "."
}

// reasonUnreadable is for a recipe page we could not read.
func reasonUnreadable(source string) string {
	return "We couldn't read this recipe on " + displayName(source) + "."
}

// reasonIncomplete is for a recipe the library refused: it was missing
// something every recipe needs, such as a name or its ingredients.
func reasonIncomplete() string {
	return "This recipe was missing details we need."
}

// fetchFailureReason is the member's sentence for a recipe whose page failed.
func fetchFailureReason(source string, err error) string {
	var parse *ParseError
	if errors.As(err, &parse) && (parse.Status == http.StatusNotFound || parse.Status == http.StatusGone) {
		return reasonGone(source)
	}
	return reasonUnreadable(source)
}

// memberReason returns reason if it is one of the sentences above, and
// otherwise the sentence that replaces it.
//
// Jobs imported before these sentences existed stored the pipeline's raw
// wording ("name is required", "matches the same stored recipe as
// recipes[1]") and parse details meant for a log. The status route runs every
// stored reason through here, so none of that reaches a member even from an
// old job. ok is false for the old duplicate messages: those recipes were not
// failures, and the caller drops them.
func memberReason(source, reason string) (string, bool) {
	switch reason {
	case reasonGone(source), reasonUnreadable(source), reasonIncomplete():
		return reason, true
	}
	if isDuplicateProblem(reason) {
		return "", false
	}
	if strings.Contains(reason, "required") || strings.Contains(reason, "must ") {
		return reasonIncomplete(), true
	}
	if strings.Contains(reason, "HTTP 404") || strings.Contains(reason, "HTTP 410") {
		return reasonGone(source), true
	}
	return reasonUnreadable(source), true
}

// isDuplicateProblem recognizes the pipeline's two duplicate messages, which
// older jobs stored as failures.
func isDuplicateProblem(reason string) bool {
	return strings.HasPrefix(reason, "matches the same stored recipe as recipes[") ||
		strings.HasPrefix(reason, "shares a source ID with recipes[")
}
