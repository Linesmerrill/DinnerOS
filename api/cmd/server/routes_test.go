package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// TestHouseholdRoutesMountTogether mounts the household-scoped handlers on one
// /api/v1 router, as run does. chi panics on conflicting patterns, so a clash
// between modules fails here instead of at startup.
func TestHouseholdRoutesMountTogether(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		recipes.NewHandler(recipes.HandlerOptions{}).Mount(r)
		planning.NewHandler(planning.HandlerOptions{}).Mount(r)
		ratings.NewHandler(ratings.HandlerOptions{}).Mount(r)
		events.NewHandler(events.HandlerOptions{}).Mount(r)
	})

	var routes []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"GET /api/v1/households/{householdId}/recipes/{recipeId}",
		"PUT /api/v1/households/{householdId}/recipes/{recipeId}/rating",
		"DELETE /api/v1/households/{householdId}/recipes/{recipeId}/rating",
		"GET /api/v1/households/{householdId}/recipes/{recipeId}/ratings",
		"POST /api/v1/households/{householdId}/events",
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("route %q not mounted; have %v", want, routes)
		}
	}
}
