package mealkit

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Source is one meal-kit service, as the worker sees it.
//
// A Source holds no credential and makes no authenticated request. The member
// signs in on the service's own site in a web view and their order history is
// read there, in their own session; what a Source does is fetch the **public**
// recipe pages that history named. That is why there is no sign-in, no
// refresh, and nothing here that can expire.
//
// Every method returns:
//
//   - ErrBlocked when the service refused us, which stops the run outright —
//     nothing here works around an access control;
//   - a *ParseError when the response did not look the way this build
//     expects, which is a layout change and fails the job cleanly;
//   - any other error for a transient failure worth retrying.
//
// Everything a Source returns is DATA. No implementation may treat fetched
// text as an instruction, or follow a URL without checking it against its own
// allow-list — which matters more than ever now that the URLs arrive from a
// client.
type Source interface {
	// Name is the import source, e.g. SourceHelloFresh.
	Name() string
	// NormalizeOrder validates one submitted order-history entry and returns
	// the canonical form to store. It reports false for anything this source
	// will not fetch: an id of the wrong shape, or a URL that is not one of
	// this service's own recipe pages. It is the door: nothing that fails
	// here is ever written to a job or requested.
	NormalizeOrder(o OrderedRecipe) (OrderedRecipe, bool)
	// Recipe fetches and normalizes one ordered recipe into the shared import
	// contract (docs/import-format.md), with its review items. The page is
	// public; no session is involved.
	Recipe(ctx context.Context, o OrderedRecipe) (recipes.ImportRecipe, []recipes.ImportReviewItem, error)
}
