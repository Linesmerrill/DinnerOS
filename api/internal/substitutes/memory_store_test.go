package substitutes

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// memoryStore is an in-memory Store for unit tests. It follows the same
// contract as MongoStore (runStoreContract).
type memoryStore struct {
	mu          sync.Mutex
	next        int
	specialties []Specialty
	options     []Option
	choices     []Choice
	settings    map[string]Settings
	upserts     int
}

var _ Store = (*memoryStore)(nil)

func newMemoryStore() *memoryStore { return &memoryStore{settings: map[string]Settings{}} }

func cloneSpecialty(sp Specialty) Specialty {
	sp.Aliases, sp.AliasKeys, sp.UnitSizes = slices.Clone(sp.Aliases), slices.Clone(sp.AliasKeys), slices.Clone(sp.UnitSizes)
	opts := make([]Option, 0, len(sp.Options))
	for _, o := range sp.Options {
		opts = append(opts, cloneOption(o))
	}
	sp.Options = opts
	return sp
}

func cloneOption(o Option) Option {
	o.Ingredients, o.Steps = slices.Clone(o.Ingredients), slices.Clone(o.Steps)
	if o.Per != nil {
		per := *o.Per
		o.Per = &per
	}
	if o.Yield != nil {
		y := *o.Yield
		o.Yield = &y
	}
	return o
}

func (m *memoryStore) ListSpecialties(_ context.Context, includeRetired bool) ([]Specialty, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Specialty
	for _, sp := range m.specialties {
		if includeRetired || !sp.Retired {
			out = append(out, cloneSpecialty(sp))
		}
	}
	slices.SortFunc(out, func(a, b Specialty) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (m *memoryStore) GetSpecialty(_ context.Context, id string) (Specialty, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sp := range m.specialties {
		if sp.ID == id {
			return cloneSpecialty(sp), nil
		}
	}
	return Specialty{}, ErrNotFound
}

func (m *memoryStore) UpsertSpecialty(_ context.Context, sp Specialty) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserts++
	sp = cloneSpecialty(sp)
	sp.Retired = false
	for i, e := range m.specialties {
		if e.ID == sp.ID {
			sp.CreatedAt = e.CreatedAt
			m.specialties[i] = sp
			return nil
		}
	}
	for _, e := range m.specialties {
		if e.Key == sp.Key {
			return fmt.Errorf("%w: key_unique", ErrDuplicate)
		}
	}
	m.specialties = append(m.specialties, sp)
	return nil
}

func (m *memoryStore) RetireSpecialties(_ context.Context, ids []string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, sp := range m.specialties {
		if slices.Contains(ids, sp.ID) {
			m.specialties[i].Retired, m.specialties[i].UpdatedAt = true, at
		}
	}
	return nil
}

func (m *memoryStore) ListOptions(_ context.Context, householdID, specialtyID string) ([]Option, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Option
	for _, o := range m.options {
		if o.HouseholdID == householdID && (specialtyID == "" || o.SpecialtyID == specialtyID) {
			out = append(out, cloneOption(o))
		}
	}
	slices.SortStableFunc(out, func(a, b Option) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out, nil
}

func (m *memoryStore) optionIndex(householdID, id string) int {
	return slices.IndexFunc(m.options, func(o Option) bool { return o.ID == id && o.HouseholdID == householdID })
}

func (m *memoryStore) GetOption(_ context.Context, householdID, id string) (Option, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.optionIndex(householdID, id); i >= 0 {
		return cloneOption(m.options[i]), nil
	}
	return Option{}, ErrNotFound
}

func (m *memoryStore) CountOptions(_ context.Context, householdID, specialtyID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, o := range m.options {
		if o.HouseholdID == householdID && o.SpecialtyID == specialtyID {
			n++
		}
	}
	return n, nil
}

func (m *memoryStore) InsertOption(_ context.Context, o Option) (Option, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	o = cloneOption(o)
	o.ID, o.Source = fmt.Sprintf("%024x", m.next), SourceHousehold
	m.options = append(m.options, o)
	return cloneOption(o), nil
}

func (m *memoryStore) ReplaceOption(_ context.Context, o Option) (Option, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.optionIndex(o.HouseholdID, o.ID)
	if i < 0 {
		return Option{}, ErrNotFound
	}
	e := m.options[i]
	o = cloneOption(o)
	o.SpecialtyID, o.Source, o.CreatedBy, o.CreatedAt = e.SpecialtyID, SourceHousehold, e.CreatedBy, e.CreatedAt
	m.options[i] = o
	return cloneOption(o), nil
}

func (m *memoryStore) DeleteOption(_ context.Context, householdID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.optionIndex(householdID, id)
	if i < 0 {
		return ErrNotFound
	}
	m.options = slices.Delete(m.options, i, i+1)
	return nil
}

func (m *memoryStore) ListChoices(_ context.Context, householdID string) ([]Choice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Choice
	for _, c := range m.choices {
		if c.HouseholdID == householdID {
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Choice) int { return cmp.Compare(a.SpecialtyID, b.SpecialtyID) })
	return out, nil
}

func (m *memoryStore) choiceIndex(householdID, specialtyID string) int {
	return slices.IndexFunc(m.choices, func(c Choice) bool { return c.HouseholdID == householdID && c.SpecialtyID == specialtyID })
}

func (m *memoryStore) PutChoice(_ context.Context, c Choice) (Choice, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.choiceIndex(c.HouseholdID, c.SpecialtyID); i >= 0 {
		m.choices[i] = c
	} else {
		m.choices = append(m.choices, c)
	}
	return c, nil
}

func (m *memoryStore) InsertChoice(_ context.Context, c Choice) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.choiceIndex(c.HouseholdID, c.SpecialtyID) >= 0 {
		return false, nil
	}
	m.choices = append(m.choices, c)
	return true, nil
}

func (m *memoryStore) DeleteChoice(_ context.Context, householdID, specialtyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.choiceIndex(householdID, specialtyID); i >= 0 {
		m.choices = slices.Delete(m.choices, i, i+1)
	}
	return nil
}

func (m *memoryStore) DeleteChoicesForOption(_ context.Context, householdID, optionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.choices = slices.DeleteFunc(m.choices, func(c Choice) bool { return c.HouseholdID == householdID && c.OptionID == optionID })
	return nil
}

func (m *memoryStore) GetSettings(_ context.Context, householdID string) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.settings[householdID]
	if !ok {
		return Settings{}, ErrNotFound
	}
	return s, nil
}

func (m *memoryStore) PutSettings(_ context.Context, s Settings) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.settings == nil {
		m.settings = map[string]Settings{}
	}
	m.settings[s.HouseholdID] = s
	return s, nil
}
