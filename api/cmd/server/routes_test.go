package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/customize"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/menu"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
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
		customize.NewHandler(customize.HandlerOptions{}).Mount(r)
		pantry.NewHandler(pantry.HandlerOptions{}).Mount(r)
		substitutes.NewHandler(substitutes.HandlerOptions{}).Mount(r)
		shopping.NewHandler(shopping.HandlerOptions{}).Mount(r)
		notifications.NewHandler(notifications.HandlerOptions{}).Mount(r)
		recommendations.NewHandler(recommendations.HandlerOptions{}).Mount(r)
		ratings.NewHandler(ratings.HandlerOptions{}).Mount(r)
		events.NewHandler(events.HandlerOptions{}).Mount(r)
		menu.NewHandler(menu.HandlerOptions{}).Mount(r)
		auth.NewHandler(auth.HandlerOptions{}).Mount(r)
		push.NewHandler(push.HandlerOptions{}).Mount(r)
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
		"GET /api/v1/me",
		"PUT /api/v1/me/device-tokens",
		"DELETE /api/v1/me/device-tokens",
		"GET /api/v1/households/{householdId}/notifications",
		"GET /api/v1/households/{householdId}/notifications/unread-count",
		"POST /api/v1/households/{householdId}/notifications/read",
		"GET /api/v1/households/{householdId}/autopilot/profile",
		"PATCH /api/v1/households/{householdId}/autopilot/profile",
		"GET /api/v1/households/{householdId}/autopilot/vocabulary",
		"GET /api/v1/households/{householdId}/autopilot/weeks/{week}/pairings",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/pairings/accept",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/pairings/dismiss",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/pairings/rules",
		"DELETE /api/v1/households/{householdId}/autopilot/weeks/{week}/pairings/grocery-items/{itemId}",
		"GET /api/v1/households/{householdId}/recipes/{recipeId}/pairings",
		"PUT /api/v1/households/{householdId}/autopilot/recipes/{recipeId}/override",
		"PUT /api/v1/households/{householdId}/autopilot/weeks/{week}/context",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/generate",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/proposal/slots/{slotId}/swap",
		"POST /api/v1/households/{householdId}/autopilot/weeks/{week}/proposal/accept",
		"GET /api/v1/households/{householdId}/plans/{week}/grocery",
		"GET /api/v1/households/{householdId}/recipes/{recipeId}/customizations",
		"PUT /api/v1/households/{householdId}/plans/{week}/entries/{entryId}/customization",
		"PATCH /api/v1/households/{householdId}/plans/{week}/entries/{entryId}",
		"GET /api/v1/households/{householdId}/menu",
		"GET /api/v1/households/{householdId}/menu/recipes",
		"GET /api/v1/households/{householdId}/menu/filters",
		"GET /api/v1/households/{householdId}/weeks",
		"GET /api/v1/shopping/providers",
		"GET /api/v1/households/{householdId}/shopping/settings",
		"PUT /api/v1/households/{householdId}/shopping/settings",
		"GET /api/v1/households/{householdId}/shopping/{provider}/preferences",
		"GET /api/v1/households/{householdId}/shopping/{provider}/preferences/{ingredientKey}",
		"PUT /api/v1/households/{householdId}/shopping/{provider}/preferences/{ingredientKey}",
		"DELETE /api/v1/households/{householdId}/shopping/{provider}/preferences/{ingredientKey}",
		"POST /api/v1/households/{householdId}/plans/{week}/shopping/{provider}/match",
		"POST /api/v1/households/{householdId}/plans/{week}/shopping/{provider}/handoffs",
		"GET /api/v1/households/{householdId}/shopping/handoffs",
		"GET /api/v1/households/{householdId}/shopping/handoffs/{handoffId}",
		"POST /api/v1/households/{householdId}/shopping/handoffs/{handoffId}/confirm",
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("route %q not mounted; have %v", want, routes)
		}
	}
}
