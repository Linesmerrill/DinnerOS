// Package baseline is the local Autopilot implementation: deterministic,
// explainable, and deliberately simple. It filters the catalog with hard
// constraints, scores each meal for each open day with weighted signals, and
// chooses the week with a beam search over days that penalizes repetition,
// unbalanced cook times, and repeated at-most-once rules. Ties break with a
// seed derived from the household, week, attempt, and model version, so the
// same request always produces the same week.
//
// It is a baseline, not the final algorithm: weights are hand-set defaults
// meant to be replaced by tuned models behind autopilot.RecommendationProvider.
package baseline

import (
	"context"
	"fmt"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// ModelVersion identifies this implementation and DefaultWeights. Bump it when
// features, weights, or the optimizer change.
const ModelVersion = "baseline-2026.2"

// Search defaults.
const (
	DefaultBeamWidth         = 16
	DefaultCandidatesPerSlot = 40
)

// Options configures a Provider. Zero values use the defaults.
type Options struct {
	Weights Weights
	// ModelVersion labels results. Set it when using non-default weights.
	ModelVersion string
	// BeamWidth is how many partial weeks the search keeps per day.
	BeamWidth int
	// CandidatesPerSlot is how many of a day's best meals each partial week
	// tries.
	CandidatesPerSlot int
}

// Provider is the baseline autopilot.RecommendationProvider. It is safe for
// concurrent use.
type Provider struct {
	weights Weights
	version string
	beam    int
	perSlot int
}

var _ autopilot.RecommendationProvider = (*Provider)(nil)

// New returns a Provider.
func New(opts Options) *Provider {
	p := &Provider{weights: opts.Weights, version: opts.ModelVersion, beam: opts.BeamWidth, perSlot: opts.CandidatesPerSlot}
	if p.weights == (Weights{}) {
		p.weights = DefaultWeights()
	}
	if p.version == "" {
		p.version = ModelVersion
	}
	if p.beam <= 0 {
		p.beam = DefaultBeamWidth
	}
	if p.perSlot <= 0 {
		p.perSlot = DefaultCandidatesPerSlot
	}
	return p
}

// ModelVersion returns the version results are labeled with.
func (p *Provider) ModelVersion() string { return p.version }

// GenerateWeek implements autopilot.RecommendationProvider.
func (p *Provider) GenerateWeek(ctx context.Context, req autopilot.WeekRequest) (autopilot.WeekResult, error) {
	if err := ctx.Err(); err != nil {
		return autopilot.WeekResult{}, err
	}
	m, err := p.prepare(req.Input, req.Attempt)
	if err != nil {
		return autopilot.WeekResult{}, err
	}
	return m.generate(), nil
}

// RankMeals implements autopilot.RecommendationProvider.
func (p *Provider) RankMeals(ctx context.Context, req autopilot.RankRequest) (autopilot.RankResult, error) {
	if err := ctx.Err(); err != nil {
		return autopilot.RankResult{}, err
	}
	if !req.Day.Valid() {
		return autopilot.RankResult{}, fmt.Errorf("%w: day must be one of mon–sun", autopilot.ErrInvalidRequest)
	}
	m, err := p.prepare(req.Input, 0)
	if err != nil {
		return autopilot.RankResult{}, err
	}
	return m.rank(req.Day, req.Exclude, req.Limit), nil
}
