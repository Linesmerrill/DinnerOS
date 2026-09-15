package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
)

// TestHouseholdRoutesMountTogether mounts the household-scoped handlers on one
// /api/v1 router, as run does. chi panics on conflicting patterns, so a clash
// between modules fails here instead of at startup.
func TestHouseholdRoutesMountTogether(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		recipes.NewHandler(recipes.HandlerOptions{}).Mount(r)
		planning.NewHandler(planning.HandlerOptions{}).Mount(r)
		pantry.NewHandler(pantry.HandlerOptions{}).Mount(r)
		substitutes.NewHandler(substitutes.HandlerOptions{}).Mount(r)
		notifications.NewHandler(notifications.HandlerOptions{}).Mount(r)
		recommendations.NewHandler(recommendations.HandlerOptions{}).Mount(r)
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
		"PATCH /api/v1/households/{householdId}/pantry/{itemId}",
		"POST /api/v1/households/{householdId}/pantry/purchases",
		"GET /api/v1/households/{householdId}/pantry/settings",
		"PUT /api/v1/households/{householdId}/pantry/settings",
		"GET /api/v1/households/{householdId}/pantry/{itemId}/purchases",
		"GET /api/v1/households/{householdId}/specialty-ingredients",
		"POST /api/v1/households/{householdId}/specialty-ingredients/choices/defaults",
		"GET /api/v1/households/{householdId}/specialty-ingredients/{specialtyId}",
		"PUT /api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/choice",
		"DELETE /api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/choice",
		"POST /api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/options",
		"PUT /api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/options/{optionId}",
		"DELETE /api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/options/{optionId}",
		"POST /api/v1/households/{householdId}/specialty-ingredients/{specialtyId}/batches",
		"GET /api/v1/households/{householdId}/notifications",
		"GET /api/v1/households/{householdId}/notifications/unread-count",
		"POST /api/v1/households/{householdId}/notifications/read",
		"GET /api/v1/households/{householdId}/autopilot/profile",
		"PATCH /api/v1/households/{householdId}/autopilot/profile",
		"GET /api/v1/households/{householdId}/autopilot/vocabulary",
		"PUT /api/v1/households/{householdId}/autopilot/recipes/{recipeId}/override",
		"PUT /api/v1/households/{householdId}/autopilot/weeks/{week}/context",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/generate",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/proposal/slots/{slotId}/swap",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/proposal/accept",
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("route %q not mounted; have %v", want, routes)
		}
	}
}
