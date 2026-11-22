package notifications

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// A group is repeats of one kind of notification the app shows as one: three
// "Your invite counted" read as "3 of your invites counted". Grouping happens on read,
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
		title: func(n int) string { return fmt.Sprintf("%d of your invites counted", n) },
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

// maxGroupPages bounds how many store pages a read scans for a group's
// members: MaxListLimit times this many notifications.
const maxGroupPages = 20

// groupMembers lists every notification matching f that belongs to a group,
// newest first, by group key. A group is counted across the whole household,
// never per page, so its card says the same number wherever it shows.
func (s *Service) groupMembers(ctx context.Context, householdID string, f ListFilter) (map[string][]Notification, error) {
	out := map[string][]Notification{}
	f.Limit, f.Before = MaxListLimit, ""
	for range maxGroupPages {
		items, err := s.store.List(ctx, householdID, f)
		if err != nil {
			return nil, err
		}
		for _, n := range items {
			if k := groupKey(n); k != "" {
				out[k] = append(out[k], n)
			}
		}
		if len(items) < f.Limit {
			break
		}
		f.Before = items[len(items)-1].ID
	}
	return out, nil
}

func hasGroup(items []Notification) bool {
	return slices.ContainsFunc(items, func(n Notification) bool { return groupKey(n) != "" })
}

// collapse shows each group in items (newest first) once, where its newest
// member is, as one notification that carries the group's count in its title
// and body and every member's ID in GroupIDs; members (from groupMembers)
// are the whole group, so an older member on a later page is never shown
// again. The group is unread for userID while any member is, and a banner
// while any unread member is. A group of one is unchanged.
func collapse(items []Notification, userID string, members map[string][]Notification) []Notification {
	out := make([]Notification, 0, len(items))
	for _, n := range items {
		key := groupKey(n)
		all := members[key]
		if key == "" || len(all) < 2 {
			out = append(out, n)
			continue
		}
		if all[0].ID != n.ID {
			continue // shown with the group's newest member
		}
		g := n
		g.ReadBy, g.GroupIDs = slices.Clone(n.ReadBy), make([]string, 0, len(all))
		for _, m := range all {
			g.GroupIDs = append(g.GroupIDs, m.ID)
			if !m.ReadByUser(userID) {
				g.ReadBy = slices.DeleteFunc(g.ReadBy, func(id string) bool { return id == userID })
				g.Banner = g.Banner || m.Banner
			}
		}
		rule := groups[key]
		g.Title, g.Body = rule.title(len(all)), rule.body(len(all))
		out = append(out, g)
	}
	return out
}

// expandGroups adds to ids every other notification in the groups they name,
// so reading or dismissing the grouped card covers all of it.
func (s *Service) expandGroups(ctx context.Context, householdID, userID string, ids []string) ([]string, error) {
	members, err := s.groupMembers(ctx, householdID, ListFilter{Viewer: userID})
	if err != nil {
		return nil, err
	}
	out := slices.Clone(ids)
	for _, list := range members {
		if !slices.ContainsFunc(list, func(n Notification) bool { return slices.Contains(ids, n.ID) }) {
			continue
		}
		for _, n := range list {
			if !slices.Contains(out, n.ID) {
				out = append(out, n.ID)
			}
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
	members, err := s.groupMembers(ctx, householdID, ListFilter{UnreadBy: userID, Viewer: userID})
	if err != nil {
		return 0, err
	}
	for _, list := range members {
		n -= len(list) - 1
	}
	return n, nil
}
