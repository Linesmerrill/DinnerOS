package notifications

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// A group is repeats of one kind of notification the app shows as one: three
// "Your invite counted" read as "3 invites counted". Grouping happens on read,
// so every notification is still stored (and pushed) on its own, and marking
// the group read or dismissing it covers every notification in it.
type group struct {
	title func(count int) string
	body  func(count int) string
}

// groupInvitesCounted is the sharer's "an invite counted" notification
// (plus.ReferrerTitle), whose dedupe key ends in ":referrer".
const groupInvitesCounted = "plus.referral:referrer"

var groups = map[string]group{
	groupInvitesCounted: {
		title: func(n int) string { return fmt.Sprintf("%d invites counted", n) },
		body:  func(n int) string { return fmt.Sprintf("%d households joined from your invites.", n) },
	},
}

// groupKey names the group n belongs to, or "" when it stands alone.
func groupKey(n Notification) string {
	if n.Type == TypePlusReferral && strings.HasSuffix(n.DedupeKey, ":referrer") {
		return groupInvitesCounted
	}
	return ""
}

// collapse folds each group in items (newest first) into its newest
// notification, which takes the group's count in its title and body and the
// IDs of all of them in GroupIDs. The group is unread for userID while any of
// them is, and a banner while any unread one is. A group of one is unchanged.
func collapse(items []Notification, userID string) []Notification {
	out := make([]Notification, 0, len(items))
	at := map[string]int{}
	for _, n := range items {
		key := groupKey(n)
		i, seen := at[key]
		if key == "" || !seen {
			if key != "" {
				at[key] = len(out)
				n.GroupIDs = []string{n.ID}
			}
			out = append(out, n)
			continue
		}
		g := &out[i]
		g.GroupIDs = append(g.GroupIDs, n.ID)
		if !n.ReadByUser(userID) {
			g.ReadBy = slices.DeleteFunc(slices.Clone(g.ReadBy), func(id string) bool { return id == userID })
			g.Banner = g.Banner || n.Banner
		}
	}
	for i := range out {
		g := &out[i]
		if len(g.GroupIDs) < 2 {
			g.GroupIDs = nil
			continue
		}
		rule := groups[groupKey(*g)]
		g.Title, g.Body = rule.title(len(g.GroupIDs)), rule.body(len(g.GroupIDs))
	}
	return out
}

// expandGroups adds to ids every other notification in the groups they name,
// so reading or dismissing the grouped card covers all of it.
func (s *Service) expandGroups(ctx context.Context, householdID, userID string, ids []string) ([]string, error) {
	items, err := s.store.List(ctx, householdID, ListFilter{Viewer: userID, Limit: MaxListLimit})
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, n := range items {
		if k := groupKey(n); k != "" && slices.Contains(ids, n.ID) {
			keys[k] = true
		}
	}
	if len(keys) == 0 {
		return ids, nil
	}
	out := slices.Clone(ids)
	for _, n := range items {
		if keys[groupKey(n)] && !slices.Contains(out, n.ID) {
			out = append(out, n.ID)
		}
	}
	return out, nil
}

// unreadCount counts userID's unread notifications, a group as one.
func (s *Service) unreadCount(ctx context.Context, householdID, userID string) (int, error) {
	n, err := s.store.CountUnread(ctx, householdID, userID)
	if err != nil || n < 2 {
		return n, err
	}
	unread, err := s.store.List(ctx, householdID, ListFilter{UnreadBy: userID, Viewer: userID, Limit: MaxListLimit})
	if err != nil {
		return 0, err
	}
	return n - (len(unread) - len(collapse(unread, userID))), nil
}
