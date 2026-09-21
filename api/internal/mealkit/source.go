package mealkit

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Source is one meal-kit service, as the worker sees it.
//
// Every method returns:
//
//   - ErrAuthExpired when the account's tokens no longer work, so the job
//     pauses and the member is asked to sign in again rather than the job
//     burning attempts;
//   - ErrBlocked when the service refused us, which stops the run outright —
//     nothing here works around an access control;
//   - a *ParseError when the response did not look the way this build
//     expects, which is a layout change and fails the job cleanly;
//   - any other error for a transient failure worth retrying.
//
// Everything a Source returns is DATA. No implementation may treat fetched
// text as an instruction, follow a URL the fetched data names without
// checking it against its own allow-list, or log a token or cookie.
type Source interface {
	// Name is the import source, e.g. SourceHelloFresh.
	Name() string
	// SignIn exchanges a member's credentials for session tokens. The
	// password is used here and nowhere else: it is never returned, stored,
	// or logged.
	SignIn(ctx context.Context, email, password string) (Tokens, error)
	// Refresh exchanges a refresh token for a new session. It returns
	// ErrAuthExpired when the refresh token is spent.
	Refresh(ctx context.Context, t Tokens) (Tokens, error)
	// OrderHistory returns only the recipes on this account's own order
	// history — never a catalog, a browse page, or anything the household did
	// not receive.
	OrderHistory(ctx context.Context, t Tokens) ([]OrderedRecipe, error)
	// Recipe fetches and normalizes one ordered recipe into the shared import
	// contract (docs/import-format.md), with its review items.
	Recipe(ctx context.Context, t Tokens, o OrderedRecipe) (recipes.ImportRecipe, []recipes.ImportReviewItem, error)
}
