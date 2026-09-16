package shopping

import (
	"strconv"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file holds the pure half of sending a week's list to a cart more than
// once: comparing a match with what the week's current handoff already sent,
// and adding only the difference to that handoff. It has no I/O.
//
// A provider's add-to-cart link adds quantities to whatever the cart holds,
// and DinnerOS can neither read nor clear that cart. So what's in it is
// tracked here: per (ingredient, product), the packages the handoff's lines
// have put there. Skipped lines don't count (a member said they weren't
// ordered, which only happens as a handoff is answered and closed).

type sentKey struct{ ingredientKey, productID string }

// sentPackages sums a handoff's packages by ingredient and product, in line
// order, with the first line of each for display.
func sentPackages(h Handoff) (map[sentKey]int, []sentKey, map[sentKey]HandoffLine) {
	sent := map[sentKey]int{}
	var order []sentKey
	first := map[sentKey]HandoffLine{}
	for _, l := range h.Lines {
		if l.Status == LineSkipped {
			continue
		}
		k := sentKey{l.IngredientKey, l.ProductID}
		if _, seen := sent[k]; !seen {
			order = append(order, k)
			first[k] = l
		}
		sent[k] += l.Packages
	}
	return sent, order, first
}

// applyCart annotates a match with the week's current handoff h: each line's
// LineCart, the sent products that aren't match lines, and cart links for the
// difference only.
func applyCart(p providers.GroceryProvider, proposal *Proposal, h Handoff) error {
	counts, order, first := sentPackages(h)

	matched := map[sentKey]bool{}
	productFor := map[string]string{}
	items := make([]providers.CartItem, 0, len(proposal.Lines))
	for i := range proposal.Lines {
		l := &proposal.Lines[i]
		k := sentKey{l.IngredientKey, l.ProductID}
		n := counts[k]
		matched[k] = true
		productFor[l.IngredientKey] = l.ProductID
		l.Cart = &LineCart{SentPackages: n, AddPackages: max(l.Packages-n, 0), RemovePackages: max(n-l.Packages, 0)}
		if l.Cart.AddPackages > 0 {
			items = append(items, providers.CartItem{ProductID: l.ProductID, Quantity: l.Cart.AddPackages, LineIDs: []string{l.ID}})
		}
	}
	onList := map[string]bool{}
	for _, e := range proposal.Excluded {
		if e.Reason != ExcludedNotOnList {
			onList[e.IngredientKey] = true
		}
	}

	cart := &CartState{HandoffID: h.ID, SentAt: h.UpdatedAt, Other: []SentLine{}}
	for _, k := range order {
		if matched[k] {
			continue
		}
		l := first[k]
		sl := SentLine{
			LineSource: l.LineSource, ProductID: l.ProductID, ProductName: l.ProductName, PackageSize: l.PackageSize,
			SentPackages: counts[k], Reason: SentNotIncluded,
		}
		if _, isLine := productFor[k.ingredientKey]; isLine {
			sl.Reason, sl.RemovePackages = SentProductChanged, counts[k]
		} else if !onList[k.ingredientKey] {
			sl.Reason, sl.RemovePackages = SentNotOnList, counts[k]
		}
		cart.Other = append(cart.Other, sl)
	}
	proposal.Cart = cart

	var err error
	proposal.Links, err = cartLinks(p, items, proposal.StoreID)
	return err
}

// mergeSend adds what a match annotated by applyCart still needs to the
// handoff h, and returns the updated handoff with links for the additions,
// the IDs of the lines they touch, and the packages they add. An addition
// goes onto the pending line for the same ingredient and product, or a new
// line when there is none (a confirmed line's purchase is already recorded,
// so it is never grown). Nothing to add returns no line IDs and h unchanged.
func mergeSend(p providers.GroceryProvider, h Handoff, proposal Proposal, now time.Time) (Handoff, []string, int, error) {
	next := h
	next.Lines = append([]HandoffLine(nil), h.Lines...)
	lastID := 0
	for _, l := range next.Lines {
		if n, err := strconv.Atoi(strings.TrimPrefix(l.ID, "l")); err == nil && n > lastID {
			lastID = n
		}
	}
	var items []providers.CartItem
	var lineIDs []string
	packages := 0
	for _, l := range proposal.Lines {
		if l.Cart == nil || l.Cart.AddPackages == 0 {
			continue
		}
		add := l.Cart.AddPackages
		fresh := l
		fresh.Cart = nil
		fresh.Status = LinePending
		index := -1
		for i, existing := range next.Lines {
			if existing.IngredientKey == l.IngredientKey && existing.ProductID == l.ProductID && existing.Status == LinePending {
				index = i
				break
			}
		}
		if index >= 0 {
			fresh.ID = next.Lines[index].ID
			fresh.Packages = min(next.Lines[index].Packages+add, providers.MaxPackages)
			next.Lines[index] = fresh
		} else {
			lastID++
			fresh.ID = "l" + strconv.Itoa(lastID)
			fresh.Packages = add
			next.Lines = append(next.Lines, fresh)
		}
		items = append(items, providers.CartItem{ProductID: l.ProductID, Quantity: add, LineIDs: []string{fresh.ID}})
		lineIDs = append(lineIDs, fresh.ID)
		packages += add
	}
	if len(items) == 0 {
		return h, nil, 0, nil
	}
	links, err := cartLinks(p, items, proposal.StoreID)
	if err != nil {
		return Handoff{}, nil, 0, err
	}
	next.Links, next.Excluded, next.StoreID, next.AffiliateTracked = links, proposal.Excluded, proposal.StoreID, proposal.AffiliateTracked
	next.UpdatedAt = now
	return next, lineIDs, packages, nil
}
