package ratings

import (
	"context"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// A note is the member's own: another member of the same household never
// sees it, blank text deletes it, and account deletion removes it.
func TestIntegrationNotesArePrivate(t *testing.T) {
	store, _ := newTestMongoStore(t)
	svc := NewService(ServiceOptions{Store: store, Notes: store, Recipes: testRecipes})
	ctx := context.Background()
	ada := households.Membership{HouseholdID: hhA, UserID: userAda, Role: households.RoleMember}
	alan := households.Membership{HouseholdID: hhA, UserID: userAlan, Role: households.RoleMember}

	if n, err := svc.Note(ctx, ada, tacos); err != nil || n.Text != "" {
		t.Fatalf("no note yet = %+v, %v", n, err)
	}
	if _, err := svc.SaveNote(ctx, ada, tacos, "  Simmered 20 minutes, not 10.  "); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.Note(ctx, ada, tacos); n.Text != "Simmered 20 minutes, not 10." || n.UpdatedAt.IsZero() {
		t.Errorf("ada's note = %+v", n)
	}
	if n, _ := svc.Note(ctx, alan, tacos); n.Text != "" {
		t.Errorf("alan sees %q, want nothing: notes are private", n.Text)
	}
	if _, err := svc.Note(ctx, ada, bobsStew); err == nil {
		t.Error("a recipe from another household should be not found")
	}
	if _, err := svc.SaveNote(ctx, ada, tacos, ""); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.Note(ctx, ada, tacos); n.Text != "" {
		t.Errorf("after clearing = %q", n.Text)
	}

	if _, err := svc.SaveNote(ctx, ada, curry, "More ginger."); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeUser(ctx, userAda); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.Note(ctx, ada, curry); n.Text != "" {
		t.Errorf("after account deletion = %q", n.Text)
	}
}
