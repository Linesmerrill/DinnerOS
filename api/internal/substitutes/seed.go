package substitutes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// SeedPath is the curated seed file inside the embedded seed directory.
const SeedPath = "seed/specialty-ingredients.json"

//go:embed seed/*.json
var seedFS embed.FS

// Seed is a parsed, validated curated set.
type Seed struct {
	Version     int
	Specialties []Specialty
}

type seedFile struct {
	Version     int             `json:"version"`
	Note        string          `json:"note"`
	Specialties []seedSpecialty `json:"specialties"`
}

type seedSpecialty struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Aliases         []string       `json:"aliases"`
	Category        string         `json:"category"`
	Note            string         `json:"note"`
	UnitSizes       []seedUnitSize `json:"unitSizes"`
	DefaultOptionID string         `json:"defaultOptionId"`
	Options         []seedOption   `json:"options"`
}

type seedUnitSize struct {
	Per      string `json:"per"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type seedMeasure struct {
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

type seedComponent struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
	Category string `json:"category"`
}

type seedOption struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Name          string          `json:"name"`
	Notes         string          `json:"notes"`
	Per           *seedMeasure    `json:"per"`
	Ingredients   []seedComponent `json:"ingredients"`
	Steps         []string        `json:"steps"`
	Yield         *seedMeasure    `json:"yield"`
	ShelfLifeDays int             `json:"shelfLifeDays"`
}

// LoadSeed parses the embedded curated seed.
func LoadSeed() (Seed, error) {
	data, err := seedFS.ReadFile(SeedPath)
	if err != nil {
		return Seed{}, fmt.Errorf("substitutes: read seed: %w", err)
	}
	return ParseSeed(data)
}

// ParseSeed parses and validates a seed file: stable slugs, unique names and
// aliases across specialties, known units and categories, exact positive
// amounts, and options that pass the same rules as household options.
func ParseSeed(data []byte) (Seed, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var f seedFile
	if err := dec.Decode(&f); err != nil {
		return Seed{}, fmt.Errorf("substitutes: decode seed: %w", err)
	}
	if f.Version < 1 {
		return Seed{}, fmt.Errorf("substitutes: seed version must be at least 1")
	}
	seed := Seed{Version: f.Version}
	owners := map[string]string{} // normalized name or alias → specialty ID
	for i, ss := range f.Specialties {
		sp, err := parseSeedSpecialty(ss, f.Version, owners)
		if err != nil {
			return Seed{}, fmt.Errorf("substitutes: seed specialties[%d] (%s): %w", i, ss.Name, err)
		}
		seed.Specialties = append(seed.Specialties, sp)
	}
	if len(seed.Specialties) == 0 {
		return Seed{}, fmt.Errorf("substitutes: seed has no specialties")
	}
	return seed, nil
}

func parseSeedSpecialty(ss seedSpecialty, version int, owners map[string]string) (Specialty, error) {
	sp := Specialty{ID: ss.ID, Name: strings.TrimSpace(ss.Name), Category: ss.Category, DefaultOptionID: ss.DefaultOptionID, SeedVersion: version}
	sp.Key = ingredients.NormalizeName(sp.Name)
	switch {
	case sp.Key == "":
		return Specialty{}, fmt.Errorf("name is required")
	case sp.ID != Slug(sp.Name):
		return Specialty{}, fmt.Errorf("id must be %q", Slug(sp.Name))
	case !validCategory(sp.Category):
		return Specialty{}, fmt.Errorf("unknown category %q", sp.Category)
	}
	note, err := cleanText("note", ss.Note, MaxNoteLength, false)
	if err != nil {
		return Specialty{}, err
	}
	sp.Note = note
	claim := func(key string) error {
		if owner, ok := owners[key]; ok {
			return fmt.Errorf("name or alias %q is already %s", key, owner)
		}
		owners[key] = sp.ID
		return nil
	}
	if err := claim(sp.Key); err != nil {
		return Specialty{}, err
	}
	for _, alias := range ss.Aliases {
		key := ingredients.NormalizeName(alias)
		if key == "" {
			return Specialty{}, fmt.Errorf("alias %q has no letters or digits", alias)
		}
		if err := claim(key); err != nil {
			return Specialty{}, err
		}
		sp.Aliases, sp.AliasKeys = append(sp.Aliases, strings.TrimSpace(alias)), append(sp.AliasKeys, key)
	}
	for _, us := range ss.UnitSizes {
		size, err := normalizeUnitSize(us)
		if err != nil {
			return Specialty{}, err
		}
		if slices.ContainsFunc(sp.UnitSizes, func(u UnitSize) bool { return u.Per == size.Per }) {
			return Specialty{}, fmt.Errorf("unitSizes repeats %q", size.Per)
		}
		sp.UnitSizes = append(sp.UnitSizes, size)
	}
	if len(ss.Options) == 0 {
		return Specialty{}, fmt.Errorf("options must not be empty")
	}
	for _, so := range ss.Options {
		o := Option{
			ID: so.ID, SpecialtyID: sp.ID, Source: SourceCurated, Type: OptionType(so.Type),
			Name: so.Name, Notes: so.Notes, Steps: so.Steps, ShelfLifeDays: so.ShelfLifeDays,
		}
		if so.Per != nil {
			o.Per = &Measure{Quantity: so.Per.Quantity, Unit: so.Per.Unit}
		}
		if so.Yield != nil {
			o.Yield = &Measure{Quantity: so.Yield.Quantity, Unit: so.Yield.Unit}
		}
		for _, c := range so.Ingredients {
			o.Ingredients = append(o.Ingredients, Component(c))
		}
		if !strings.HasPrefix(o.ID, sp.ID+".") || len(o.ID) == len(sp.ID)+1 || o.ID == OptionAsIs {
			return Specialty{}, fmt.Errorf("option id %q must start with %q", o.ID, sp.ID+".")
		}
		if _, dup := sp.option(o.ID); dup {
			return Specialty{}, fmt.Errorf("option id %q repeats", o.ID)
		}
		normalized, err := normalizeOption(o)
		if err != nil {
			return Specialty{}, fmt.Errorf("option %s: %w", o.ID, err)
		}
		sp.Options = append(sp.Options, normalized)
	}
	if _, ok := sp.option(sp.DefaultOptionID); !ok {
		return Specialty{}, fmt.Errorf("defaultOptionId %q is not one of its options", sp.DefaultOptionID)
	}
	sp.ContentHash = contentHash(sp)
	return sp, nil
}

func normalizeUnitSize(us seedUnitSize) (UnitSize, error) {
	if u, err := ingredients.LookupUnit(us.Per); err != nil || !u.Discrete() {
		return UnitSize{}, fmt.Errorf("unitSizes.per %q must be a unit such as count or package", us.Per)
	}
	q, unit, err := normalizeAmount("unitSizes", us.Quantity, us.Unit)
	if err != nil {
		return UnitSize{}, err
	}
	if u, _ := ingredients.LookupUnit(unit); q == "" || u.Discrete() {
		return UnitSize{}, fmt.Errorf("unitSizes for %q needs a positive volume or weight", us.Per)
	}
	return UnitSize{Per: us.Per, Quantity: q, Unit: unit}, nil
}

// contentHash identifies a specialty's curated content, so a sync writes only
// what changed.
func contentHash(sp Specialty) string {
	content := sp
	content.SeedVersion, content.ContentHash, content.Retired = 0, "", false
	content.CreatedAt, content.UpdatedAt = time.Time{}, time.Time{}
	data, err := json.Marshal(content)
	if err != nil {
		panic("substitutes: marshal specialty: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SyncResult reports what a seed sync did, or would do, by specialty ID.
type SyncResult struct {
	Created   []string
	Updated   []string
	Unchanged []string
	Retired   []string
}

// SyncSeed makes the stored curated set match seed. Specialties are created
// when missing and replaced when their content changed (or they were retired);
// stored specialties the seed no longer has are retired, never deleted, so
// household choices still resolve. With apply false nothing is written. It is
// idempotent: a second run reports everything unchanged.
func SyncSeed(ctx context.Context, store Store, seed Seed, apply bool, now time.Time) (SyncResult, error) {
	existing, err := store.ListSpecialties(ctx, true)
	if err != nil {
		return SyncResult{}, fmt.Errorf("list specialties: %w", err)
	}
	byID := make(map[string]Specialty, len(existing))
	for _, sp := range existing {
		byID[sp.ID] = sp
	}
	var res SyncResult
	inSeed := map[string]bool{}
	for _, sp := range seed.Specialties {
		inSeed[sp.ID] = true
		stored, ok := byID[sp.ID]
		switch {
		case !ok:
			res.Created = append(res.Created, sp.ID)
		case stored.ContentHash != sp.ContentHash || stored.Retired:
			res.Updated = append(res.Updated, sp.ID)
		default:
			res.Unchanged = append(res.Unchanged, sp.ID)
			continue
		}
		if apply {
			sp.SeedVersion, sp.CreatedAt, sp.UpdatedAt = seed.Version, now, now
			if err := store.UpsertSpecialty(ctx, sp); err != nil {
				return res, fmt.Errorf("upsert specialty %s: %w", sp.ID, err)
			}
		}
	}
	for _, sp := range existing {
		if !inSeed[sp.ID] && !sp.Retired {
			res.Retired = append(res.Retired, sp.ID)
		}
	}
	if apply && len(res.Retired) > 0 {
		if err := store.RetireSpecialties(ctx, res.Retired, now); err != nil {
			return res, fmt.Errorf("retire specialties: %w", err)
		}
	}
	return res, nil
}

// EnsureSeed syncs the embedded curated set on startup and logs what changed.
func EnsureSeed(ctx context.Context, store Store, logger *slog.Logger) error {
	seed, err := LoadSeed()
	if err != nil {
		return err
	}
	res, err := SyncSeed(ctx, store, seed, true, time.Now().UTC().Truncate(time.Millisecond))
	if err != nil {
		return fmt.Errorf("substitutes: sync seed: %w", err)
	}
	if logger != nil {
		logger.InfoContext(ctx, "specialty ingredient seed synced", "version", seed.Version,
			"created", len(res.Created), "updated", len(res.Updated), "unchanged", len(res.Unchanged), "retired", len(res.Retired))
	}
	return nil
}
