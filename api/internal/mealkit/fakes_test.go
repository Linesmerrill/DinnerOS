package mealkit

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Synthetic IDs, valid ObjectID hex so the same fixtures work against MongoDB.
const (
	hhAda   = "66e5a1f2c3b4a5d6e7f80a01"
	hhBob   = "66e5a1f2c3b4a5d6e7f80b01"
	userAda = "66e5a1f2c3b4a5d6e7f80a21"
	userBob = "66e5a1f2c3b4a5d6e7f80a22"
)

// --- Store --------------------------------------------------------------------

// memoryStore is an in-memory Store following the MongoStore contract,
// including the atomic claim: every mutation holds the same lock, which is
// what MongoDB's single-document find-and-modify gives us in production.
type memoryStore struct {
	mu   sync.Mutex
	jobs []Job
	seq  int
}

var _ Store = (*memoryStore)(nil)

func (m *memoryStore) nextID(prefix string) string {
	m.seq++
	return fmt.Sprintf("%s%019d", prefix, m.seq)
}

func (m *memoryStore) InsertJob(_ context.Context, j Job) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j.ID = m.nextID("job")
	m.jobs = append(m.jobs, j)
	return j, nil
}

func (m *memoryStore) GetJob(_ context.Context, householdID, id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.jobs {
		if j.ID == id && j.HouseholdID == householdID {
			return j, nil
		}
	}
	return Job{}, ErrNotFound
}

func (m *memoryStore) LatestJob(_ context.Context, householdID, source string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.jobs) - 1; i >= 0; i-- {
		if m.jobs[i].HouseholdID == householdID && m.jobs[i].Source == source {
			return m.jobs[i], nil
		}
	}
	return Job{}, ErrNotFound
}

func (m *memoryStore) ActiveJob(_ context.Context, householdID, source string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.jobs) - 1; i >= 0; i-- {
		j := m.jobs[i]
		if j.HouseholdID == householdID && j.Source == source && j.Status.Active() {
			return j, nil
		}
	}
	return Job{}, ErrNotFound
}

func (m *memoryStore) ListJobs(_ context.Context, householdID string, limit int) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Job
	for i := len(m.jobs) - 1; i >= 0 && len(out) < max(limit, 1); i-- {
		if m.jobs[i].HouseholdID == householdID {
			out = append(out, m.jobs[i])
		}
	}
	return out, nil
}

func (m *memoryStore) ClaimJob(_ context.Context, owner string, now, leaseUntil time.Time) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	best := -1
	for i, j := range m.jobs {
		runnable := (j.Status == JobQueued && !j.AvailableAt.After(now)) ||
			(j.Status == JobRunning && !j.LeaseExpiresAt.After(now))
		if !runnable {
			continue
		}
		if best < 0 || j.AvailableAt.Before(m.jobs[best].AvailableAt) {
			best = i
		}
	}
	if best < 0 {
		return Job{}, ErrNotFound
	}
	m.jobs[best].Status = JobRunning
	m.jobs[best].LeaseOwner = owner
	m.jobs[best].LeaseExpiresAt = leaseUntil
	m.jobs[best].StartedAt = now
	m.jobs[best].UpdatedAt = now
	m.jobs[best].Attempts++
	return m.jobs[best], nil
}

func (m *memoryStore) claimedIndex(id, owner string) int {
	for i, j := range m.jobs {
		if j.ID == id && j.Status == JobRunning && j.LeaseOwner == owner {
			return i
		}
	}
	return -1
}

func (m *memoryStore) ExtendLease(_ context.Context, id, owner string, leaseUntil, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.claimedIndex(id, owner)
	if i < 0 {
		return ErrJobGone
	}
	m.jobs[i].LeaseExpiresAt = leaseUntil
	m.jobs[i].UpdatedAt = at
	return nil
}

func (m *memoryStore) SaveCheckpoint(_ context.Context, id, owner string, c Checkpoint, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.claimedIndex(id, owner)
	if i < 0 {
		return ErrJobGone
	}
	m.jobs[i].Checkpoint = c
	m.jobs[i].UpdatedAt = at
	return nil
}

func (m *memoryStore) FinishJob(_ context.Context, id, owner string, status JobStatus, c Checkpoint, jobErr *JobError, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.claimedIndex(id, owner)
	if i < 0 {
		return ErrJobGone
	}
	m.jobs[i].Status = status
	m.jobs[i].Checkpoint = c
	m.jobs[i].LeaseOwner = ""
	m.jobs[i].LeaseExpiresAt = time.Time{}
	m.jobs[i].UpdatedAt = at
	if jobErr != nil {
		m.jobs[i].LastError = jobErr
	}
	if status.Terminal() {
		m.jobs[i].FinishedAt = at
	}
	return nil
}

func (m *memoryStore) RequeueJob(_ context.Context, id, owner string, availableAt time.Time, countAttempt bool, jobErr *JobError, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.claimedIndex(id, owner)
	if i < 0 {
		return ErrJobGone
	}
	m.jobs[i].Status = JobQueued
	m.jobs[i].AvailableAt = availableAt
	m.jobs[i].LeaseOwner = ""
	m.jobs[i].LeaseExpiresAt = time.Time{}
	m.jobs[i].UpdatedAt = at
	if jobErr != nil {
		m.jobs[i].LastError = jobErr
	}
	if !countAttempt {
		m.jobs[i].Attempts--
	}
	return nil
}

func (m *memoryStore) CancelJobs(_ context.Context, householdID, source, reason string, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for i, j := range m.jobs {
		if j.HouseholdID == householdID && j.Source == source && j.Status.Active() {
			m.jobs[i].Status = JobCanceled
			m.jobs[i].LeaseOwner = ""
			m.jobs[i].FinishedAt = at
			m.jobs[i].LastError = &JobError{Code: ErrCodeInternal, Message: reason, At: at}
			n++
		}
	}
	return n, nil
}

// --- Source -------------------------------------------------------------------

// fakeSource is a scripted meal-kit service.
type fakeSource struct {
	mu sync.Mutex

	// recipeErr maps a source recipe ID to the error to return for it.
	recipeErr map[string]error
	// rejectID, when set, is the one id NormalizeOrder refuses.
	rejectID string

	fetched []string
	// beforeRecipe runs before each Recipe call, for racing a stop in.
	beforeRecipe func(o OrderedRecipe)
}

var _ Source = (*fakeSource)(nil)

func (f *fakeSource) Name() string { return SourceHelloFresh }

// NormalizeOrder keeps anything with an id, which is enough for the queue's
// tests; the real allow-list is the HelloFresh client's own.
func (f *fakeSource) NormalizeOrder(o OrderedRecipe) (OrderedRecipe, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o.SourceRecipeID == "" || (f.rejectID != "" && o.SourceRecipeID == f.rejectID) {
		return OrderedRecipe{}, false
	}
	if o.URL == "" {
		o.URL = "https://example.test/recipes/" + o.SourceRecipeID
	}
	return o, true
}

func (f *fakeSource) Recipe(_ context.Context, o OrderedRecipe) (recipes.ImportRecipe, []recipes.ImportReviewItem, error) {
	if f.beforeRecipe != nil {
		f.beforeRecipe(o)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.recipeErr[o.SourceRecipeID]; ok {
		return recipes.ImportRecipe{}, nil, err
	}
	f.fetched = append(f.fetched, o.SourceRecipeID)
	return recipes.ImportRecipe{
		Source: SourceHelloFresh, SourceRecipeID: o.SourceRecipeID, Name: o.Name,
		Servings: []int{2}, OrderWeeks: o.Weeks,
	}, nil, nil
}

func (f *fakeSource) fetchedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.fetched)
}

// --- Publisher ----------------------------------------------------------------

// fakePublisher records what reached the import pipeline.
type fakePublisher struct {
	mu    sync.Mutex
	files []recipes.ImportFile
	err   error
	// reject names source recipe IDs the pipeline refuses.
	reject map[string]string
}

func (p *fakePublisher) Import(_ context.Context, _ string, file recipes.ImportFile) (recipes.ImportResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return recipes.ImportResult{}, p.err
	}
	p.files = append(p.files, file)
	res := recipes.ImportResult{}
	for i, r := range file.Recipes {
		if reason, bad := p.reject[r.SourceRecipeID]; bad {
			res.Errors = append(res.Errors, recipes.RecipeError{
				Index: i, SourceRecipeID: r.SourceRecipeID, Name: r.Name, Problems: []string{reason},
			})
			continue
		}
		res.Created++
	}
	res.ReviewItems = len(file.Review)
	return res, nil
}

func (p *fakePublisher) imported() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, f := range p.files {
		for _, r := range f.Recipes {
			out = append(out, r.SourceRecipeID)
		}
	}
	return out
}

// --- Notifier -----------------------------------------------------------------

type fakeNotifier struct {
	mu   sync.Mutex
	sent []notifications.New
}

func (n *fakeNotifier) Create(_ context.Context, in notifications.New) (notifications.Notification, bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, in)
	return notifications.Notification{}, true, nil
}

func (n *fakeNotifier) types() []notifications.Type {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []notifications.Type
	for _, in := range n.sent {
		out = append(out, in.Type)
	}
	return out
}

// --- HTTP fakes ---------------------------------------------------------------

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants permissions per (household, user).
type fakeAuthorizer map[string][]households.Permission

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	perms, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
	if !slices.Contains(perms, perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

var testAuthorizer = fakeAuthorizer{
	hhAda + "/" + userAda: {households.PermHouseholdView, households.PermRecipesImport},
	hhBob + "/" + userBob: {households.PermHouseholdView},
}
