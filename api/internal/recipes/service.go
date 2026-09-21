package recipes

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Service implements recipe import and queries.
type Service struct {
	store Store
	now   func() time.Time
	// catalog publishes public-source recipes into the global recipe catalog
	// (sharing.go). It is nil until WithCatalog wires one.
	catalog CatalogPublisher
}

// NewService returns a Service backed by store.
func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

// ImportResult summarizes an import. Every valid recipe is counted exactly
// once in Created, Updated, or Unchanged.
type ImportResult struct {
	Created   int
	Updated   int
	Unchanged int
	// IngredientsCreated counts new catalog ingredients.
	IngredientsCreated int
	// CatalogPublished counts recipes written to the global recipe catalog.
	// It is 0 when no publisher is wired or nothing was publishable.
	CatalogPublished int
	// ReviewItems counts distinct review items in the file. Items recorded by
	// an earlier import are not duplicated.
	ReviewItems int
	// Released counts stored recipes outside the file that gave up aliases
	// the file assigns to other recipes. They are not deleted.
	Released int
	// Errors lists rejected recipes in file order. The rest still import.
	Errors []RecipeError
}

// RecipeError explains why a recipe in the file was not imported.
type RecipeError struct {
	// Index is the recipe's position in ImportFile.Recipes.
	Index          int
	SourceRecipeID string
	Name           string
	Problems       []string
}

// importCandidate is a valid recipe from the file and the stored recipe it
// updates, if any.
type importCandidate struct {
	index    int
	in       ImportRecipe
	ids      []string
	existing *Recipe
	match    storedMatch
	// released are other file recipes that take IDs existing holds as
	// aliases (see Import).
	released []ImportRecipe
}

// Import idempotently upserts a file's recipes into a household. Callers
// authorize first: the HTTP route requires households.PermRecipesImport, and
// the importrecipes command is an operator tool with direct database access.
//
// A recipe matches a stored one when its sourceRecipeId or any alias equals the
// stored recipe's sourceRecipeId or any of its aliases, so re-imports that
// pick a different canonical ID do not duplicate recipes. Order weeks and
// aliases merge as set unions; every other field takes the file's value. A
// file recipe that an earlier import stored only as another recipe's alias is
// split out as its own recipe when another file recipe matches that stored
// recipe more closely. Re-importing the same file writes no recipes.
//
// It returns ErrInvalidImport for an unusable file. The work is a handful of
// bulk round trips regardless of file size.
func (s *Service) Import(ctx context.Context, householdID string, file ImportFile) (ImportResult, error) {
	if householdID == "" {
		return ImportResult{}, errHouseholdRequired
	}
	if err := validateFile(file); err != nil {
		return ImportResult{}, err
	}
	now := s.now().UTC()
	var res ImportResult
	reject := func(index int, r ImportRecipe, problems ...string) {
		res.Errors = append(res.Errors, RecipeError{Index: index, SourceRecipeID: r.SourceRecipeID, Name: strings.TrimSpace(r.Name), Problems: problems})
	}

	// 1. Validate, and reject recipes that share a source ID with an earlier
	// recipe in the same file.
	var candidates []importCandidate
	claimed := map[sourceRecipeKey]int{}
	for i, raw := range file.Recipes {
		r := cleanImportRecipe(raw)
		if problems := validateRecipe(r); len(problems) > 0 {
			reject(i, r, problems...)
			continue
		}
		ids := append([]string{r.SourceRecipeID}, r.SourceAliases...)
		if other, dup := firstClaim(claimed, r.Source, ids); dup {
			reject(i, r, fmt.Sprintf("shares a source ID with recipes[%d]", other))
			continue
		}
		for _, id := range ids {
			claimed[sourceRecipeKey{source: r.Source, id: id}] = i
		}
		candidates = append(candidates, importCandidate{index: i, in: r, ids: ids})
	}

	// 2. Match stored recipes by canonical ID or alias.
	idsBySource := map[string][]string{}
	for _, c := range candidates {
		idsBySource[c.in.Source] = append(idsBySource[c.in.Source], c.ids...)
	}
	stored := map[string][]Recipe{}
	for _, source := range sortedKeys(idsBySource) {
		found, err := s.store.FindRecipesBySourceIDs(ctx, householdID, source, idsBySource[source])
		if err != nil {
			return ImportResult{}, fmt.Errorf("find recipes: %w", err)
		}
		stored[source] = found
	}
	// A stored recipe goes to the candidate that matches it most closely. The
	// file may split a stored recipe: a delivered variant that an earlier
	// import merged under a canonical recipe as an alias now arrives as its
	// own recipe. When another candidate claims the stored recipe more closely
	// (by its canonical ID, or by sharing more IDs), the alias-only candidate
	// is created as a new recipe. Only an exact tie is rejected.
	for i := range candidates {
		candidates[i].existing, candidates[i].match = matchStored(stored[candidates[i].in.Source], candidates[i].in.SourceRecipeID, candidates[i].ids)
	}
	winners := map[string]int{} // stored recipe ID -> position in candidates
	for i, c := range candidates {
		if c.existing == nil {
			continue
		}
		if w, ok := winners[c.existing.ID]; !ok || c.match.beats(candidates[w].match) {
			winners[c.existing.ID] = i
		}
	}
	matched := make([]importCandidate, 0, len(candidates))
	for i, c := range candidates {
		if c.existing != nil {
			if w := winners[c.existing.ID]; w != i {
				winner := candidates[w]
				if !winner.match.beats(c.match) || c.match.strength != matchAlias {
					reject(c.index, c.in, fmt.Sprintf("matches the same stored recipe as recipes[%d]", winner.index))
					continue
				}
				c.existing = nil
			}
		}
		matched = append(matched, c)
	}
	candidates = matched

	// The file is the authority on which recipe an ID belongs to. A stored
	// recipe gives up the aliases, and the order weeks that came with them, that
	// the file assigns to another recipe, whether that recipe is split out, was
	// stored separately already, or leaves the stored recipe out of the file.
	// Stored recipes keep their identity either way, so nothing that refers to
	// them breaks.
	owner := map[string]int{} // stored recipe ID -> position in candidates
	claimedBy := map[sourceRecipeKey][]int{}
	for i, c := range candidates {
		if c.existing != nil {
			owner[c.existing.ID] = i
		}
		for _, id := range c.ids {
			key := sourceRecipeKey{source: c.in.Source, id: id}
			claimedBy[key] = append(claimedBy[key], i)
		}
	}
	var detached []Recipe
	for _, source := range sortedKeys(stored) {
		seen := map[string]bool{}
		for _, st := range stored[source] {
			if seen[st.ID] {
				continue
			}
			seen[st.ID] = true
			self, owned := owner[st.ID]
			var released []ImportRecipe
			taken := map[int]bool{}
			for _, id := range st.SourceAliases {
				for _, i := range claimedBy[sourceRecipeKey{source: source, id: id}] {
					if (!owned || i != self) && !taken[i] {
						taken[i] = true
						released = append(released, candidates[i].in)
					}
				}
			}
			switch {
			case len(released) == 0:
			case owned:
				candidates[self].released = released
			default:
				detached = append(detached, releaseFromStored(st, released))
			}
		}
	}

	// 3. Resolve ingredient lines to catalog IDs.
	ingredientIDs, created, err := s.resolveIngredients(ctx, candidates, now)
	if err != nil {
		return ImportResult{}, err
	}
	res.IngredientsCreated = created

	// 4. Build, compare with the stored recipe, and write only what changed.
	var writes []Recipe
	for _, c := range candidates {
		source := c.in.Source
		r := buildRecipe(c.in, func(ing ImportIngredient) string { return ingredientIDs[lineKeyOf(source, ing)] })
		r.HouseholdID = householdID
		if c.existing == nil {
			r.CreatedAt, r.UpdatedAt = now, now
			writes = append(writes, r)
			res.Created++
			continue
		}
		r = mergeStored(r, *c.existing, c.released)
		if reflect.DeepEqual(r, *c.existing) {
			res.Unchanged++
			continue
		}
		r.UpdatedAt = now
		writes = append(writes, r)
		res.Updated++
	}
	for _, r := range detached {
		r.UpdatedAt = now
		writes = append(writes, r)
		res.Released++
	}
	if len(writes) > 0 {
		if err := s.store.SaveRecipes(ctx, householdID, writes); err != nil {
			return ImportResult{}, fmt.Errorf("save recipes: %w", err)
		}
		// Public-source recipes reach the global catalog as they are imported,
		// so every household's import improves the catalog for the next one
		// (identity.go, docs/architecture.md#decision-log #520).
		if res.CatalogPublished, err = s.PublishToCatalog(ctx, writes); err != nil {
			return ImportResult{}, err
		}
	}

	// 5. Keep review items so they can be surfaced later.
	items := reviewItems(file)
	if len(items) > 0 {
		if err := s.store.SaveReviewItems(ctx, householdID, items, now); err != nil {
			return ImportResult{}, fmt.Errorf("save review items: %w", err)
		}
	}
	res.ReviewItems = len(items)

	slices.SortFunc(res.Errors, func(a, b RecipeError) int { return cmp.Compare(a.Index, b.Index) })
	return res, nil
}

type sourceRecipeKey struct{ source, id string }

func firstClaim(claimed map[sourceRecipeKey]int, source string, ids []string) (int, bool) {
	for _, id := range ids {
		if other, ok := claimed[sourceRecipeKey{source: source, id: id}]; ok {
			return other, true
		}
	}
	return 0, false
}

// How closely a file recipe matches a stored one, weakest first.
const (
	matchAlias     = iota + 1 // they share an ID only through aliases
	matchClaims               // the file recipe lists the stored canonical ID as an alias
	matchCanonical            // same canonical ID
)

type storedMatch struct {
	strength int
	shared   int // IDs the two have in common
}

func (m storedMatch) beats(o storedMatch) bool {
	if m.strength != o.strength {
		return m.strength > o.strength
	}
	return m.shared > o.shared
}

// matchStored returns the stored recipe a file recipe matches most closely:
// the same canonical ID, then a stored canonical ID the file lists as an
// alias, then any shared ID, preferring more shared IDs.
func matchStored(stored []Recipe, canonical string, ids []string) (*Recipe, storedMatch) {
	var best *Recipe
	var bestMatch storedMatch
	for i := range stored {
		s := &stored[i]
		m := storedMatch{}
		for _, id := range ids {
			if s.SourceRecipeID == id || slices.Contains(s.SourceAliases, id) {
				m.shared++
			}
		}
		switch {
		case m.shared == 0:
			continue
		case s.SourceRecipeID == canonical:
			m.strength = matchCanonical
		case slices.Contains(ids, s.SourceRecipeID):
			m.strength = matchClaims
		default:
			m.strength = matchAlias
		}
		if best == nil || m.beats(bestMatch) {
			best, bestMatch = s, m
		}
	}
	return best, bestMatch
}

// mergeStored applies stored identity and the set-union fields to r, less
// the aliases and order weeks released to other file recipes (see Import).
func mergeStored(r, stored Recipe, released []ImportRecipe) Recipe {
	r.ID, r.HouseholdID = stored.ID, stored.HouseholdID
	r.CreatedAt, r.UpdatedAt = stored.CreatedAt, stored.UpdatedAt
	// A file never carries either: sharing is the household's own decision,
	// and the key is derived by the store on write. Carrying them over keeps
	// a re-import of the same file "unchanged" rather than a rewrite.
	r.SharedToCatalog, r.CatalogKey = stored.SharedToCatalog, stored.CatalogKey
	aliases, weeks := releaseIDs(
		slices.Concat(stored.SourceAliases, r.SourceAliases, []string{stored.SourceRecipeID}),
		slices.Concat(stored.OrderWeeks, r.OrderWeeks), r.OrderWeeks, released)
	r.SourceAliases = sortedSet(aliases, r.SourceRecipeID)
	r.OrderWeeks = sortedSet(weeks, "")
	r.TimesOrdered, r.LastOrderedWeek = orderStats(r.OrderWeeks)
	return r
}

// releaseFromStored returns a stored recipe that isn't in the file without
// the aliases and order weeks the file gives to other recipes.
func releaseFromStored(stored Recipe, released []ImportRecipe) Recipe {
	aliases, weeks := releaseIDs(slices.Clone(stored.SourceAliases), slices.Clone(stored.OrderWeeks), nil, released)
	stored.SourceAliases = sortedSet(aliases, stored.SourceRecipeID)
	stored.OrderWeeks = sortedSet(weeks, "")
	stored.TimesOrdered, stored.LastOrderedWeek = orderStats(stored.OrderWeeks)
	return stored
}

// releaseIDs removes the released recipes' IDs from aliases and their order
// weeks from weeks, keeping any week in keep.
func releaseIDs(aliases, weeks, keep []string, released []ImportRecipe) ([]string, []string) {
	for _, other := range released {
		ids := append([]string{other.SourceRecipeID}, other.SourceAliases...)
		aliases = slices.DeleteFunc(aliases, func(id string) bool { return slices.Contains(ids, strings.TrimSpace(id)) })
		weeks = slices.DeleteFunc(weeks, func(w string) bool {
			return slices.Contains(other.OrderWeeks, strings.TrimSpace(w)) && !slices.Contains(keep, w)
		})
	}
	return aliases, weeks
}

// lineKey identifies an ingredient line for catalog resolution.
type lineKey struct {
	ref SourceRef // SourceIngredientID may be empty
	key string
}

func lineKeyOf(source string, ing ImportIngredient) lineKey {
	return lineKey{
		ref: SourceRef{Source: source, SourceIngredientID: strings.TrimSpace(ing.SourceIngredientID)},
		key: ingredients.NormalizeName(ing.Name),
	}
}

// resolveIngredients maps every ingredient line to a catalog ID. A line
// matches by (source, sourceIngredientId) first and falls back to the
// normalized name. Unmatched names are added to the catalog; a name match
// gains the line's source reference.
func (s *Service) resolveIngredients(ctx context.Context, candidates []importCandidate, now time.Time) (map[lineKey]string, int, error) {
	var lines []lineKey
	names := map[lineKey]ImportIngredient{}
	for _, c := range candidates {
		for _, ing := range c.in.Ingredients {
			l := lineKeyOf(c.in.Source, ing)
			if _, ok := names[l]; !ok {
				names[l] = ing
				lines = append(lines, l)
			}
		}
	}
	if len(lines) == 0 {
		return nil, 0, nil
	}
	var refs []SourceRef
	var keys []string
	for _, l := range lines {
		if l.ref.SourceIngredientID != "" && !slices.Contains(refs, l.ref) {
			refs = append(refs, l.ref)
		}
		if !slices.Contains(keys, l.key) {
			keys = append(keys, l.key)
		}
	}

	byRef, byKey, err := s.findIngredients(ctx, refs, keys)
	if err != nil {
		return nil, 0, err
	}
	pending := map[string]*Ingredient{}
	for _, l := range lines {
		hasRef := l.ref.SourceIngredientID != ""
		if _, ok := byRef[l.ref]; ok && hasRef {
			continue
		}
		if _, ok := byKey[l.key]; ok && !hasRef {
			continue
		}
		p := pending[l.key]
		if p == nil {
			ing := names[l]
			name := strings.TrimSpace(ing.Name)
			category, confident := ingredients.Categorize(name)
			p = &Ingredient{
				Key: l.key, Name: name, Category: category, CategoryConfident: confident,
				ImageURL: strings.TrimSpace(ing.ImageURL), CreatedAt: now, UpdatedAt: now,
			}
			pending[l.key] = p
		}
		if hasRef && !slices.Contains(p.SourceRefs, l.ref) {
			p.SourceRefs = append(p.SourceRefs, l.ref)
		}
	}

	inserted := 0
	if len(pending) > 0 {
		upserts := make([]Ingredient, 0, len(pending))
		for _, key := range sortedKeys(pending) {
			upserts = append(upserts, *pending[key])
		}
		if inserted, err = s.store.UpsertIngredients(ctx, upserts); err != nil {
			return nil, 0, fmt.Errorf("upsert ingredients: %w", err)
		}
		if byRef, byKey, err = s.findIngredients(ctx, refs, keys); err != nil {
			return nil, 0, err
		}
	}

	ids := make(map[lineKey]string, len(lines))
	for _, l := range lines {
		if id, ok := byRef[l.ref]; ok && l.ref.SourceIngredientID != "" {
			ids[l] = id
		} else if id, ok := byKey[l.key]; ok {
			ids[l] = id
		} else {
			return nil, 0, fmt.Errorf("ingredient %q missing from catalog after upsert", l.key)
		}
	}
	return ids, inserted, nil
}

func (s *Service) findIngredients(ctx context.Context, refs []SourceRef, keys []string) (byRef map[SourceRef]string, byKey map[string]string, err error) {
	found, err := s.store.FindIngredients(ctx, refs, keys)
	if err != nil {
		return nil, nil, fmt.Errorf("find ingredients: %w", err)
	}
	byRef, byKey = map[SourceRef]string{}, map[string]string{}
	for _, ing := range found {
		byKey[ing.Key] = ing.ID
		for _, ref := range ing.SourceRefs {
			byRef[ref] = ing.ID
		}
	}
	return byRef, byKey, nil
}

func reviewItems(file ImportFile) []ReviewItem {
	var out []ReviewItem
	seen := map[string]bool{}
	for _, it := range file.Review {
		item := ReviewItem{
			Source:         file.Source,
			SourceRecipeID: strings.TrimSpace(it.SourceRecipeID),
			RecipeName:     strings.TrimSpace(it.RecipeName),
			Field:          it.Field,
			Value:          it.Value,
			Reason:         it.Reason,
		}
		if key := reviewKey(item); !seen[key] {
			seen[key] = true
			out = append(out, item)
		}
	}
	return out
}

// Review listing page sizes.
const (
	// DefaultReviewLimit is the page size when the caller names none.
	DefaultReviewLimit = 200
	// MaxReviewLimit caps the page size.
	MaxReviewLimit = 500
)

// ImportReviews returns the household's import review items with status
// (empty for every status), oldest first, each carrying the ID of the recipe
// it is about when that recipe is still stored.
func (s *Service) ImportReviews(ctx context.Context, householdID, status string, limit int) ([]ReviewRecord, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if limit <= 0 {
		limit = DefaultReviewLimit
	}
	limit = min(limit, MaxReviewLimit)

	items, err := s.store.ListReviewItems(ctx, householdID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list review items: %w", err)
	}

	// An item names a recipe by source ID; the stored recipe may carry it as
	// an alias, because clones of one dish merge under the newest ID.
	idsBySource := map[string][]string{}
	for _, it := range items {
		if it.SourceRecipeID != "" {
			idsBySource[it.Source] = append(idsBySource[it.Source], it.SourceRecipeID)
		}
	}
	recipeIDs := map[sourceRecipeKey]string{}
	for _, source := range slices.Sorted(maps.Keys(idsBySource)) {
		found, err := s.store.FindRecipesBySourceIDs(ctx, householdID, source, idsBySource[source])
		if err != nil {
			return nil, fmt.Errorf("find recipes for review items: %w", err)
		}
		for _, r := range found {
			recipeIDs[sourceRecipeKey{source, r.SourceRecipeID}] = r.ID
			for _, alias := range r.SourceAliases {
				recipeIDs[sourceRecipeKey{source, alias}] = r.ID
			}
		}
	}
	for i := range items {
		items[i].RecipeID = recipeIDs[sourceRecipeKey{items[i].Source, items[i].SourceRecipeID}]
	}
	return items, nil
}

// reviewKey identifies a review item so re-imports don't duplicate it.
func reviewKey(it ReviewItem) string {
	h := sha256.Sum256([]byte(strings.Join([]string{it.Source, it.SourceRecipeID, it.Field, it.Value, it.Reason}, "\x00")))
	return hex.EncodeToString(h[:])
}

// List returns a page of the household's recipe summaries.
func (s *Service) List(ctx context.Context, householdID string, q ListQuery) (ListPage, error) {
	if householdID == "" {
		return ListPage{}, errHouseholdRequired
	}
	f, err := q.filter()
	if err != nil {
		return ListPage{}, err
	}
	pageSize := f.Limit
	f.Limit = pageSize + 1 // one extra tells us whether another page exists
	items, err := s.store.ListRecipes(ctx, householdID, f)
	if err != nil {
		return ListPage{}, err
	}
	page := ListPage{Items: items}
	if len(items) > pageSize {
		page.Items = items[:pageSize]
		page.NextCursor = encodeCursor(f.Sort, positionOf(items[pageSize-1]))
	}
	return page, nil
}

// Get returns one of the household's recipes with each ingredient's catalog
// category filled in. Recipes of other households are ErrNotFound.
func (s *Service) Get(ctx context.Context, householdID, id string) (Recipe, error) {
	if householdID == "" {
		return Recipe{}, errHouseholdRequired
	}
	r, err := s.store.GetRecipe(ctx, householdID, id)
	if err != nil {
		return Recipe{}, err
	}
	list := []Recipe{r}
	if err := s.fillCategories(ctx, list); err != nil {
		return Recipe{}, err
	}
	return list[0], nil
}

// GetMany returns the household's recipes with the given IDs, ordered by ID,
// with each ingredient's catalog category filled in. IDs that are malformed,
// missing, or another household's are skipped, so callers compare the result
// with what they asked for. It is two round trips however many IDs there are.
func (s *Service) GetMany(ctx context.Context, householdID string, ids []string) ([]Recipe, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	list, err := s.store.GetRecipes(ctx, householdID, ids)
	if err != nil {
		return nil, err
	}
	if err := s.fillCategories(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// MaxCatalogRecipes bounds how many recipes Catalog returns.
const MaxCatalogRecipes = 2000

// Catalog returns the household's recipes (main meals and add-ons) ordered by
// ID, for modules that look at the whole collection at once, such as the
// recommender. Steps, nutrition, descriptions, and ingredient categories are
// left out to keep the read small. At most MaxCatalogRecipes are returned.
func (s *Service) Catalog(ctx context.Context, householdID string) ([]Recipe, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	return s.store.ListCatalog(ctx, householdID, MaxCatalogRecipes)
}

// MenuCatalog returns the household's recipes (main meals and add-ons) ordered
// by ID with what menu cards need: names, images, times, cuisines, tags,
// utensils, allergens, nutrition, order history, and ingredient names (for
// recipe attributes). Steps, descriptions, and ingredient amounts are left
// out. At most MaxCatalogRecipes are returned.
func (s *Service) MenuCatalog(ctx context.Context, householdID string) ([]Recipe, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	return s.store.ListMenuCatalog(ctx, householdID, MaxCatalogRecipes)
}

// fillCategories sets each ingredient line's category and image from the
// catalog, with one catalog query for all recipes.
func (s *Service) fillCategories(ctx context.Context, list []Recipe) error {
	var ids []string
	for _, r := range list {
		for _, line := range r.Ingredients {
			if line.IngredientID != "" && !slices.Contains(ids, line.IngredientID) {
				ids = append(ids, line.IngredientID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	catalog, err := s.store.GetIngredients(ctx, ids)
	if err != nil {
		return fmt.Errorf("get ingredients: %w", err)
	}
	byID := make(map[string]Ingredient, len(catalog))
	for _, ing := range catalog {
		byID[ing.ID] = ing
	}
	for _, r := range list {
		for i := range r.Ingredients {
			ing := byID[r.Ingredients[i].IngredientID]
			r.Ingredients[i].Category, r.Ingredients[i].ImageURL = ing.Category, ing.ImageURL
		}
	}
	return nil
}

// ExistingRecipeIDs returns those of ids that are the household's recipes, in
// any order. Other modules (ratings, events) use it to check recipe
// references without reading whole recipes.
func (s *Service) ExistingRecipeIDs(ctx context.Context, householdID string, ids []string) ([]string, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return s.store.ExistingRecipeIDs(ctx, householdID, ids)
}

// --- small helpers ------------------------------------------------------------

// uniqueStrings trims values and drops empties, exclude, and duplicates,
// keeping the first occurrence. It returns nil when nothing remains.
func uniqueStrings(values []string, exclude string) []string {
	var out []string
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && v != exclude && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// sortedSet is uniqueStrings, sorted.
func sortedSet(values []string, exclude string) []string {
	out := uniqueStrings(values, exclude)
	slices.Sort(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
