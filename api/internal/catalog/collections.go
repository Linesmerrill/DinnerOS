package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collections (decision 710) are the rows at the top of Shared Recipes. The
// server decides them: which rows, in what order, with what title, and which
// recipes each holds. A few are built in (DefaultCollections); operators can
// override one by storing a collection with its ID, or add more, through the
// operator routes. Nothing about a row is decided in the app.

// Collection types.
const (
	// TypeEditorial is a row someone picked: ordered RecipeIDs, falling back
	// to Rule while it has none.
	TypeEditorial = "editorial"
	// TypeAlgorithmic is a row computed by Rule.
	TypeAlgorithmic = "algorithmic"
	// TypeSponsored is a partner's paid row. It always shows its sponsor.
	TypeSponsored = "sponsored"
)

// Rules a collection can be computed by (home.go).
const (
	RuleBest      = "best"
	RuleTopRated  = "top-rated"
	RuleTrending  = "trending"
	RuleSeasonal  = "seasonal"
	RuleQuick     = "quick"
	RuleCrowd     = "crowd"
	maxCollection = 100
)

// Rules lists every rule a collection may name.
var Rules = []string{RuleBest, RuleTopRated, RuleTrending, RuleSeasonal, RuleQuick, RuleCrowd}

// Collection is one row of Shared Recipes.
type Collection struct {
	// ID is a slug ("chefs-picks"), the row's identity.
	ID       string
	Title    string
	Subtitle string
	Type     string
	// RecipeIDs are catalog entry IDs, in order. A copy of a recipe stands
	// for the whole recipe (decision 689).
	RecipeIDs []string
	// Rule computes the row when it has no RecipeIDs.
	Rule string
	// StartsAt and EndsAt bound when the row shows; zero means open.
	StartsAt time.Time
	EndsAt   time.Time
	// Position orders rows, lowest first.
	Position int
	// Sponsor is set only on a sponsored row.
	Sponsor *Sponsor
	// BuiltIn is true for a DefaultCollections row nobody has overridden.
	BuiltIn   bool
	UpdatedAt time.Time
}

// Sponsor is who paid for a sponsored row.
type Sponsor struct {
	Name string
	URL  string
	// Label is what the row says, "Sponsored by <Name>" when empty.
	Label string
}

// SponsorLabel is the words a sponsored row shows.
func (c Collection) SponsorLabel() string {
	if c.Type != TypeSponsored || c.Sponsor == nil {
		return ""
	}
	if c.Sponsor.Label != "" {
		return c.Sponsor.Label
	}
	return "Sponsored by " + c.Sponsor.Name
}

// Active reports whether the row shows at now.
func (c Collection) Active(now time.Time) bool {
	return (c.StartsAt.IsZero() || !now.Before(c.StartsAt)) && (c.EndsAt.IsZero() || now.Before(c.EndsAt))
}

// DefaultCollections are the rows Shared Recipes has with nothing stored.
// There is no sponsored row among them, and none ships.
func DefaultCollections() []Collection {
	return []Collection{
		{ID: "chefs-picks", Title: "Chef's Picks This Week", Subtitle: "Our favorites right now", Type: TypeEditorial, Rule: RuleBest, Position: 10},
		{ID: "top-rated", Title: "Top Rated", Subtitle: "Households loved these", Type: TypeAlgorithmic, Rule: RuleTopRated, Position: 20},
		{ID: "trending", Title: "Trending", Subtitle: "Saved most this week", Type: TypeAlgorithmic, Rule: RuleTrending, Position: 30},
		{ID: "seasonal", Title: "Seasonal Favorites", Type: TypeAlgorithmic, Rule: RuleSeasonal, Position: 40},
		{ID: "quick", Title: "Quick", Subtitle: "30 minutes or less", Type: TypeAlgorithmic, Rule: RuleQuick, Position: 50},
		{ID: "crowd", Title: "Feeds a Crowd", Subtitle: "Serves 6 or more", Type: TypeAlgorithmic, Rule: RuleCrowd, Position: 60},
	}
}

// ErrInvalidCollection means a collection an operator sent is not valid. Its
// message after the prefix is safe to show.
var ErrInvalidCollection = errors.New("catalog: invalid collection")

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Validate checks an operator's collection and tidies its text.
func (c *Collection) Validate() error {
	bad := func(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidCollection, msg) }
	c.ID = strings.TrimSpace(c.ID)
	c.Title = strings.TrimSpace(c.Title)
	c.Subtitle = strings.TrimSpace(c.Subtitle)
	c.Rule = strings.TrimSpace(c.Rule)
	switch {
	case !slugPattern.MatchString(c.ID) || len(c.ID) > 60:
		return bad("The ID must be short lowercase words joined by dashes, like chefs-picks.")
	case c.Title == "" || utf8.RuneCountInString(c.Title) > 60:
		return bad("Give the row a title of 60 characters or less.")
	case utf8.RuneCountInString(c.Subtitle) > 120:
		return bad("Keep the subtitle to 120 characters or less.")
	case !slices.Contains([]string{TypeEditorial, TypeAlgorithmic, TypeSponsored}, c.Type):
		return bad("The type must be editorial, algorithmic, or sponsored.")
	case c.Rule != "" && !slices.Contains(Rules, c.Rule):
		return bad("That rule doesn't exist. Use one of: " + strings.Join(Rules, ", ") + ".")
	case c.Type == TypeAlgorithmic && c.Rule == "":
		return bad("A computed row needs a rule.")
	case c.Type != TypeAlgorithmic && len(c.RecipeIDs) == 0 && c.Rule == "":
		return bad("Pick some recipes or a rule for the row.")
	case len(c.RecipeIDs) > maxCollection:
		return bad(fmt.Sprintf("A row can hold at most %d recipes.", maxCollection))
	case !c.StartsAt.IsZero() && !c.EndsAt.IsZero() && !c.EndsAt.After(c.StartsAt):
		return bad("The end date must be after the start date.")
	}
	var ids []string
	for _, id := range c.RecipeIDs {
		id = strings.TrimSpace(id)
		if _, err := mongodb.ParseID(id); err != nil {
			return bad("One of the recipe IDs isn't a shared recipe ID.")
		}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	c.RecipeIDs = ids
	if c.Type == TypeSponsored {
		if c.Sponsor == nil || strings.TrimSpace(c.Sponsor.Name) == "" {
			return bad("A sponsored row needs the sponsor's name.")
		}
		c.Sponsor.Name = strings.TrimSpace(c.Sponsor.Name)
		c.Sponsor.Label = strings.TrimSpace(c.Sponsor.Label)
		c.Sponsor.URL = strings.TrimSpace(c.Sponsor.URL)
		if c.Sponsor.URL != "" && !strings.HasPrefix(c.Sponsor.URL, "https://") {
			return bad("The sponsor's link must start with https://.")
		}
	} else {
		// Sponsor fields belong only to a sponsored row.
		c.Sponsor = nil
	}
	c.BuiltIn = false
	return nil
}

// CollectionStore keeps operators' collections.
type CollectionStore interface {
	List(ctx context.Context) ([]Collection, error)
	// Put creates or replaces the collection with c.ID.
	Put(ctx context.Context, c Collection) error
	// Delete removes one; ErrNotFound when there is none.
	Delete(ctx context.Context, id string) error
}

// WithCollections turns on stored collections and returns s.
func (s *Service) WithCollections(c CollectionStore) *Service {
	s.collections = c
	return s
}

// Collections returns every row, built-in ones overridden by stored ones,
// in order, including rows outside their dates.
func (s *Service) Collections(ctx context.Context) ([]Collection, error) {
	out := DefaultCollections()
	for i := range out {
		out[i].BuiltIn = true
	}
	if s.collections != nil {
		stored, err := s.collections.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list collections: %w", err)
		}
		for _, c := range stored {
			if i := slices.IndexFunc(out, func(o Collection) bool { return o.ID == c.ID }); i >= 0 {
				out[i] = c
			} else {
				out = append(out, c)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Collection) int {
		if a.Position != b.Position {
			return a.Position - b.Position
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// PutCollection validates and stores an operator's collection.
func (s *Service) PutCollection(ctx context.Context, c Collection) (Collection, error) {
	if s.collections == nil {
		return Collection{}, errors.New("catalog: collections are not wired")
	}
	if err := c.Validate(); err != nil {
		return Collection{}, err
	}
	c.UpdatedAt = s.now().UTC()
	if err := s.collections.Put(ctx, c); err != nil {
		return Collection{}, err
	}
	return c, nil
}

// DeleteCollection removes a stored collection. Deleting a stored override
// of a built-in row brings the built-in back.
func (s *Service) DeleteCollection(ctx context.Context, id string) error {
	if s.collections == nil {
		return ErrNotFound
	}
	return s.collections.Delete(ctx, id)
}

// CollectionsCollection holds operators' collections.
const CollectionsCollection = "catalog_collections"

// MongoCollections is the MongoDB CollectionStore. The _id is the slug.
type MongoCollections struct{ coll *mongo.Collection }

var _ CollectionStore = (*MongoCollections)(nil)

// NewMongoCollections returns a store using db.
func NewMongoCollections(db *mongo.Database) *MongoCollections {
	return &MongoCollections{coll: db.Collection(CollectionsCollection)}
}

type collectionDoc struct {
	ID        string          `bson:"_id"`
	Title     string          `bson:"title"`
	Subtitle  string          `bson:"subtitle,omitempty"`
	Type      string          `bson:"type"`
	RecipeIDs []bson.ObjectID `bson:"recipeIds,omitempty"`
	Rule      string          `bson:"rule,omitempty"`
	StartsAt  *time.Time      `bson:"startsAt,omitempty"`
	EndsAt    *time.Time      `bson:"endsAt,omitempty"`
	Position  int             `bson:"position"`
	Sponsor   *sponsorDoc     `bson:"sponsor,omitempty"`
	UpdatedAt time.Time       `bson:"updatedAt"`
}

type sponsorDoc struct {
	Name  string `bson:"name"`
	URL   string `bson:"url,omitempty"`
	Label string `bson:"label,omitempty"`
}

// List implements CollectionStore.
func (s *MongoCollections) List(ctx context.Context) ([]Collection, error) {
	cur, err := s.coll.Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "position", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, translate(err)
	}
	var docs []collectionDoc
	if err := cur.All(ctx, &docs); err != nil {
		return nil, translate(err)
	}
	out := make([]Collection, 0, len(docs))
	for _, d := range docs {
		c := Collection{
			ID: d.ID, Title: d.Title, Subtitle: d.Subtitle, Type: d.Type, Rule: d.Rule,
			Position: d.Position, UpdatedAt: d.UpdatedAt.UTC(),
		}
		for _, id := range d.RecipeIDs {
			c.RecipeIDs = append(c.RecipeIDs, id.Hex())
		}
		if d.StartsAt != nil {
			c.StartsAt = d.StartsAt.UTC()
		}
		if d.EndsAt != nil {
			c.EndsAt = d.EndsAt.UTC()
		}
		if d.Sponsor != nil {
			c.Sponsor = &Sponsor{Name: d.Sponsor.Name, URL: d.Sponsor.URL, Label: d.Sponsor.Label}
		}
		out = append(out, c)
	}
	return out, nil
}

// Put implements CollectionStore.
func (s *MongoCollections) Put(ctx context.Context, c Collection) error {
	d := collectionDoc{
		ID: c.ID, Title: c.Title, Subtitle: c.Subtitle, Type: c.Type, Rule: c.Rule,
		Position: c.Position, UpdatedAt: c.UpdatedAt,
	}
	for _, id := range c.RecipeIDs {
		oid, err := mongodb.ParseID(id)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrInvalidCollection, "One of the recipe IDs isn't a shared recipe ID.")
		}
		d.RecipeIDs = append(d.RecipeIDs, oid)
	}
	if !c.StartsAt.IsZero() {
		t := c.StartsAt
		d.StartsAt = &t
	}
	if !c.EndsAt.IsZero() {
		t := c.EndsAt
		d.EndsAt = &t
	}
	if c.Sponsor != nil {
		d.Sponsor = &sponsorDoc{Name: c.Sponsor.Name, URL: c.Sponsor.URL, Label: c.Sponsor.Label}
	}
	_, err := s.coll.ReplaceOne(ctx, bson.D{{Key: "_id", Value: c.ID}}, d, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("put collection: %w", err)
	}
	return nil
}

// Delete implements CollectionStore.
func (s *MongoCollections) Delete(ctx context.Context, id string) error {
	res, err := s.coll.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
