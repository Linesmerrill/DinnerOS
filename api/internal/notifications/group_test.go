package notifications

import (
	"context"
	"slices"
	"testing"
)

func inviteCounted(referredID string) New {
	return New{
		HouseholdID: hhA, Type: TypePlusReferral, Title: "Your invite counted",
		Body:      "Garcias joined from your invite. Your free month is ready.",
		Subject:   Subject{Kind: SubjectPlus},
		DedupeKey: "plus.referral:" + referredID + ":referrer", Banner: true,
	}
}

func TestRepeatedInvitesCountedReadAsOne(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	actor := memberOf(hhA, userA)
	var ids []string
	for _, ref := range []string{"r1", "r2", "r3"} {
		n, _, err := svc.Create(ctx, inviteCounted(ref))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
	}
	// The new household's own notification never joins the sharer's group.
	welcome := inviteCounted("r9")
	welcome.Title, welcome.DedupeKey = "Your free month is ready", "plus.referral:r9:referred"
	if _, _, err := svc.Create(ctx, welcome); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Create(ctx, lowNotification("pantry.low:item1:cycle1")); err != nil {
		t.Fatal(err)
	}

	page, err := svc.List(ctx, actor, ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range page.Items {
		titles = append(titles, n.Title)
	}
	want := []string{"Butter is running low", "Your free month is ready", "3 invites counted"}
	if !slices.Equal(titles, want) {
		t.Fatalf("titles = %q, want %q", titles, want)
	}
	g := page.Items[2]
	if g.Body != "3 households joined from your invites." || g.ID != ids[2] || !slices.Equal(g.GroupIDs, []string{ids[2], ids[1], ids[0]}) {
		t.Errorf("group = %+v", g)
	}
	if r := newResponse(g, userA); len(r.GroupedIDs) != 3 || r.Title != "3 invites counted" {
		t.Errorf("response = %+v", r)
	}
	if page.Items[1].GroupIDs != nil {
		t.Errorf("a notification on its own has GroupIDs %v", page.Items[1].GroupIDs)
	}
	if n, err := svc.UnreadCount(ctx, actor); err != nil || n != 3 {
		t.Errorf("UnreadCount = %d, %v; want 3 (the group counts once)", n, err)
	}
	banners, err := svc.Banners(ctx, actor)
	if err != nil || len(banners) != 2 || banners[1].Title != "3 invites counted" {
		t.Errorf("banners = %+v, %v", banners, err)
	}

	// Reading the group's card reads all three, for this member only.
	if n, err := svc.MarkRead(ctx, actor, []string{g.ID}, false); err != nil || n != 2 {
		t.Errorf("MarkRead(group) unread = %d, %v; want 2", n, err)
	}
	if n, err := svc.UnreadCount(ctx, memberOf(hhA, userB)); err != nil || n != 3 {
		t.Errorf("other member UnreadCount = %d, %v; want 3", n, err)
	}
	// Dismissing it hides all three.
	if _, err := svc.Dismiss(ctx, memberOf(hhA, userB), []string{g.ID}); err != nil {
		t.Fatal(err)
	}
	page, err = svc.List(ctx, memberOf(hhA, userB), ListQuery{})
	if err != nil || len(page.Items) != 2 {
		t.Errorf("after dismissing the group: %+v, %v", page.Items, err)
	}
}

func TestOneInviteCountedStaysAsWritten(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	if _, _, err := svc.Create(ctx, inviteCounted("r1")); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(ctx, memberOf(hhA, userA), ListQuery{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List = %+v, %v", page.Items, err)
	}
	if n := page.Items[0]; n.Title != "Your invite counted" || n.GroupIDs != nil {
		t.Errorf("single = %+v", n)
	}
}

func TestAGroupIsUnreadWhileAnyOfItIs(t *testing.T) {
	svc, store := newTestService()
	ctx := context.Background()
	actor := memberOf(hhA, userA)
	old, _, _ := svc.Create(ctx, inviteCounted("r1"))
	newest, _, _ := svc.Create(ctx, inviteCounted("r2"))
	if err := store.MarkRead(ctx, hhA, userA, []string{newest.ID}); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(ctx, actor, ListQuery{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List = %+v, %v", page.Items, err)
	}
	if page.Items[0].ReadByUser(userA) {
		t.Errorf("group reads as read while %s is unread", old.ID)
	}
}

func TestCreateMakesApostrophesCurly(t *testing.T) {
	svc, _ := newTestService()
	n, _, err := svc.Create(context.Background(), New{
		HouseholdID: hhA, Type: TypePantryLow, Title: "Time to order this week's groceries", Body: "It's order day.",
		DedupeKey: "k1",
	})
	if err != nil || n.Title != "Time to order this week’s groceries" || n.Body != "It’s order day." {
		t.Errorf("Create = %q / %q, %v", n.Title, n.Body, err)
	}
}
