package liveactivity

import "time"

// Limiter decides whether a progress update is worth a push.
//
// Apple budgets Live Activity pushes per activity and throttles an app that
// sends too many at high priority. Progress updates therefore go at
// apns-priority 5 (not counted against that budget, delivered when the system
// finds it convenient) and are spaced out here; only the final `end` goes at
// priority 10 and it is never limited. The rule, per import job — and a
// household has at most one import in flight, so per household:
//
//   - nothing new to show (same phase, same count): no push;
//   - the same phase with a new count: at most one push per MinInterval;
//   - a change of phase (importing ↔ waiting): at most one per
//     PhaseChangeInterval, so "Next batch soon" is not held back for half a
//     minute behind the batch that preceded it.
//
// With the defaults a household gets at most four updates in any minute and,
// in practice, two: a batch of ten recipes takes about thirty seconds.
type Limiter struct {
	MinInterval         time.Duration
	PhaseChangeInterval time.Duration
}

// DefaultLimiter is what production runs with.
var DefaultLimiter = Limiter{MinInterval: 30 * time.Second, PhaseChangeInterval: 15 * time.Second}

// Allow reports whether next may be pushed at now, given the last update that
// was pushed (zero lastAt: none yet).
func (l Limiter) Allow(last ContentState, lastAt time.Time, next ContentState, now time.Time) bool {
	if lastAt.IsZero() {
		return true
	}
	if next == last {
		return false
	}
	since := now.Sub(lastAt)
	if next.Phase != last.Phase {
		return since >= l.PhaseChangeInterval
	}
	return since >= l.MinInterval
}
