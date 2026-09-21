// Package sources builds the meal-kit clients a deployment offers.
//
// It is its own package so that mealkit itself never imports a concrete
// source: the worker and the service know only the mealkit.Source interface,
// and adding a second meal-kit service touches one file.
package sources

import (
	"log/slog"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit/hellofresh"
)

// Options configures the clients.
type Options struct {
	// HelloFreshBaseURL overrides the HelloFresh account API origin
	// (MEAL_KIT_HELLOFRESH_BASE_URL). Empty uses the package default.
	HelloFreshBaseURL string
	// Fetcher, when set, replaces the polite HTTP client. Tests use it; in
	// production each source gets its own with the default politeness.
	Fetcher *mealkit.Fetcher
	// Logger, when set, receives debug-level diagnostics about a response a
	// source could not read: status shape, content type, and a short redacted
	// excerpt. Never a token or a cookie.
	Logger *slog.Logger
}

// All returns the meal-kit clients keyed by name. Both cmd/server and
// cmd/importmealkit call it, so the two always agree on which services exist.
func All(opts Options) map[string]mealkit.Source {
	return map[string]mealkit.Source{
		mealkit.SourceHelloFresh: hellofresh.New(hellofresh.Options{
			Fetcher: opts.Fetcher,
			BaseURL: opts.HelloFreshBaseURL,
			Logger:  opts.Logger,
		}),
	}
}
