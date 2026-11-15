package catalog

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Shared Recipes shows each real recipe once (decision 689). Catalog entries
// are stored one per CatalogKey, but the same recipe can still arrive under
// several keys: one page imported by six households whose copies parsed a
// little differently, or a link pasted with and without tracking parameters.
// Every entry carries a fingerprint, and the lists group entries by it and
// show one representative per group.

// FingerprintVersion is the version of the fingerprint rules. Entries stored
// under an older version (or none) are refreshed by BackfillFingerprints.
const FingerprintVersion = 1

// Fingerprint kinds, the prefix of every fingerprint.
const (
	FingerprintSource = "src:"
	FingerprintURL    = "url:"
	FingerprintTyped  = "typed:"
)

// Fingerprint returns the identity of "the same recipe" across catalog
// entries, or "" for a recipe with nothing to identify it by.
//
// In order of preference:
//
//   - src: the importer's own recipe ID (HelloFresh's ID, say), for a source
//     that has one. Typed and pasted recipes are given random IDs, so theirs
//     never count (the same rule as recipes.CatalogKey).
//   - url: the source link, normalized (NormalizeSourceURL).
//   - typed: the title, the ingredient lines (name, amount, and unit, in any
//     order), and the steps (in order), each case-folded with whitespace
//     collapsed. Two recipes called "Brownies" with different ingredients or
//     different steps are different recipes.
//
// The value is the kind followed by the hex SHA-256 of the normalized key.
func Fingerprint(c recipes.Recipe) string {
	kind, key := fingerprintKey(c)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return kind + hex.EncodeToString(sum[:])
}

func fingerprintKey(c recipes.Recipe) (kind, key string) {
	source := strings.ToLower(strings.TrimSpace(c.Source))
	id := strings.TrimSpace(c.SourceRecipeID)
	if source != "" && id != "" && source != recipes.SourceManual && source != recipes.SourceUser {
		return FingerprintSource, source + "\n" + id
	}
	if u := NormalizeSourceURL(c.SourceURL); u != "" {
		return FingerprintURL, u
	}
	title := normalizeText(c.Name)
	if title == "" {
		return "", ""
	}
	lines := make([]string, 0, len(c.Ingredients))
	for _, line := range c.Ingredients {
		name := normalizeText(line.Name)
		if name == "" {
			continue
		}
		amounts := make([]string, 0, len(line.Amounts))
		for _, a := range line.Amounts {
			amounts = append(amounts, strconv.Itoa(a.Servings)+" "+normalizeQuantity(a.Quantity)+" "+normalizeText(a.Unit))
		}
		slices.Sort(amounts)
		lines = append(lines, name+"|"+strings.Join(amounts, ";"))
	}
	slices.Sort(lines)
	ordered := slices.Clone(c.Steps)
	slices.SortStableFunc(ordered, func(a, b recipes.Step) int { return cmp.Compare(a.Index, b.Index) })
	steps := make([]string, 0, len(ordered))
	for _, s := range ordered {
		if t := normalizeText(s.Text); t != "" {
			steps = append(steps, t)
		}
	}
	return FingerprintTyped, title + "\x00" + strings.Join(lines, "\x01") + "\x00" + strings.Join(steps, "\x01")
}

// normalizeText case-folds s, collapses its whitespace, and trims it.
func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// normalizeQuantity writes "0.5", "1/2", and "2/4" the same way.
func normalizeQuantity(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if r, ok := new(big.Rat).SetString(q); ok {
		return r.RatString()
	}
	return normalizeText(q)
}

// trackingParams are query parameters that say where a click came from, not
// which page it is. Any parameter starting with utm_ is one too.
var trackingParams = []string{
	"fbclid", "gclid", "gclsrc", "dclid", "msclkid", "yclid", "igshid", "twclid", "ttclid",
	"mc_cid", "mc_eid", "ref", "ref_src", "_hsenc", "_hsmi", "mkt_tok",
}

func isTrackingParam(key string) bool {
	k := strings.ToLower(key)
	return strings.HasPrefix(k, "utm_") || slices.Contains(trackingParams, k)
}

// NormalizeSourceURL returns the comparison form of a recipe's link, or ""
// when it is not a web address: the host lowercased without "www.", the path
// without a trailing slash, and the query without tracking parameters and
// with the rest sorted. The scheme and the fragment are dropped, and so is a
// default port.
func NormalizeSourceURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + strings.TrimPrefix(raw, "//")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if port := u.Port(); port != "" && port != "80" && port != "443" {
		host += ":" + port
	}
	out := host + strings.TrimRight(u.EscapedPath(), "/")
	query := u.Query()
	for k := range query {
		if isTrackingParam(k) {
			delete(query, k)
		}
	}
	for _, values := range query {
		slices.Sort(values)
	}
	// Encode sorts by key.
	if q := query.Encode(); q != "" {
		out += "?" + q
	}
	return out
}

// Completeness scores how much of a recipe a copy carries: a photo counts
// most, then steps, then nutrition. The most complete copy represents its
// group.
func Completeness(c recipes.Recipe) int {
	score := 0
	if strings.TrimSpace(c.ImageURL) != "" {
		score += 4
	}
	if len(c.Steps) > 0 {
		score += 2
	}
	if len(c.Nutrition) > 0 {
		score++
	}
	return score
}

// groupKey is the entry's group: its fingerprint, or its own ID when it has
// none (an entry the backfill has not reached yet stands alone).
func groupKey(e Recipe) string {
	if e.Fingerprint != "" {
		return e.Fingerprint
	}
	return idGroup(e.ID)
}

func idGroup(id string) string { return "id:" + id }

// representsBetter reports whether a should represent a group over b: the
// more complete copy, then the one published first, then the lower ID.
func representsBetter(a, b Recipe) bool {
	if ca, cb := Completeness(a.Content), Completeness(b.Content); ca != cb {
		return ca > cb
	}
	if !a.FirstPublishedAt.Equal(b.FirstPublishedAt) {
		return a.FirstPublishedAt.Before(b.FirstPublishedAt)
	}
	return a.ID < b.ID
}

// Dedupe returns one representative per group of entries, in the order each
// group first appears, with GroupKeys set to every member's CatalogKey (the
// representative's first). MongoStore does the same in its aggregations;
// this is the in-memory form, for tests and tools.
func Dedupe(entries []Recipe) []Recipe {
	index := map[string]int{}
	var out []Recipe
	var members [][]Recipe
	for _, e := range entries {
		g := groupKey(e)
		i, ok := index[g]
		if !ok {
			index[g] = len(out)
			out = append(out, e)
			members = append(members, []Recipe{e})
			continue
		}
		members[i] = append(members[i], e)
		if representsBetter(e, out[i]) {
			out[i] = e
		}
	}
	for i := range out {
		keys := []string{out[i].CatalogKey}
		for _, m := range members[i] {
			if m.ID != out[i].ID && !slices.Contains(keys, m.CatalogKey) {
				keys = append(keys, m.CatalogKey)
			}
		}
		out[i].GroupKeys = keys
	}
	return out
}
