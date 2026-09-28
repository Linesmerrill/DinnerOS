package liveactivity

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
)

// sendTimeout bounds one push, so a slow gateway never holds a worker's
// checkpoint loop or a member's Stop request for long.
const sendTimeout = 10 * time.Second

// PusherOptions configures a Pusher.
type PusherOptions struct {
	Store  Store
	Sender push.LiveActivitySender
	// Limiter spaces out progress updates. Zero value: DefaultLimiter.
	Limiter Limiter
	Logger  *slog.Logger
	Now     func() time.Time
}

// Pusher mirrors import progress onto the member's Live Activity through
// APNs. It implements mealkit.ProgressObserver. Nothing it does can fail an
// import: every error is logged — without the token — and dropped.
type Pusher struct {
	opts PusherOptions
}

var _ mealkit.ProgressObserver = (*Pusher)(nil)

// NewPusher returns a Pusher with defaults filled in.
func NewPusher(opts PusherOptions) *Pusher {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Limiter == (Limiter{}) {
		opts.Limiter = DefaultLimiter
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Pusher{opts: opts}
}

// ImportProgressed implements mealkit.ProgressObserver: an `update`, if the
// job has an activity and the Limiter allows it.
func (p *Pusher) ImportProgressed(ctx context.Context, job mealkit.Job) {
	if job.Status.Terminal() {
		return
	}
	reg, err := p.opts.Store.Get(ctx, job.ID)
	if err != nil {
		p.logLookup(ctx, job, err)
		return
	}
	now := p.now()
	state := StateFor(job)
	if !p.opts.Limiter.Allow(reg.LastSent, reg.LastSentAt, state, now) {
		return
	}
	err = p.send(ctx, reg, push.LiveActivityMessage{
		Event: push.LiveActivityUpdate, ContentState: state,
		Timestamp: now, StaleDate: now.Add(StaleAfter), LowPriority: true,
	})
	if err != nil {
		p.dropped(ctx, job, reg, "update", err)
		return
	}
	if err := p.opts.Store.MarkSent(ctx, job.ID, state, now); err != nil {
		p.opts.Logger.WarnContext(ctx, "recording a live activity update failed", "jobId", job.ID, "error", err)
	}
}

// ImportEnded implements mealkit.ProgressObserver: an `end` with the final
// state — green for done — and the token is removed whatever happens, because
// the job it belonged to is over.
func (p *Pusher) ImportEnded(ctx context.Context, job mealkit.Job) {
	reg, err := p.opts.Store.Get(ctx, job.ID)
	if err != nil {
		p.logLookup(ctx, job, err)
		return
	}
	now := p.now()
	state := StateFor(job)
	err = p.send(ctx, reg, push.LiveActivityMessage{
		Event: push.LiveActivityEnd, ContentState: state,
		Timestamp: now, DismissalDate: now.Add(linger(state.Phase)),
	})
	if err != nil {
		p.dropped(ctx, job, reg, "end", err)
	}
	if err := p.opts.Store.Clear(ctx, job.ID); err != nil {
		p.opts.Logger.WarnContext(ctx, "removing a live activity token failed", "jobId", job.ID, "error", err)
	}
}

func linger(phase Phase) time.Duration {
	switch phase {
	case PhaseDone:
		return DoneLinger
	case PhaseFailed:
		return FailedLinger
	default:
		return CanceledLinger
	}
}

func (p *Pusher) send(ctx context.Context, reg Registration, m push.LiveActivityMessage) error {
	m.Token, m.Environment = reg.Token, reg.Environment
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
	defer cancel()
	return p.opts.Sender.SendLiveActivity(ctx, m)
}

// dropped logs a failed push. A token APNs will never accept again is
// removed, so the next checkpoint does not try it.
func (p *Pusher) dropped(ctx context.Context, job mealkit.Job, reg Registration, event string, err error) {
	if errors.Is(err, push.ErrDeviceTokenInvalid) {
		_ = p.opts.Store.Clear(ctx, job.ID)
		p.opts.Logger.InfoContext(ctx, "live activity token is no longer valid; removed it",
			"jobId", job.ID, "event", event, "environment", reg.Environment)
		return
	}
	p.opts.Logger.WarnContext(ctx, "live activity push failed",
		"jobId", job.ID, "event", event, "environment", reg.Environment, "error", err)
}

func (p *Pusher) logLookup(ctx context.Context, job mealkit.Job, err error) {
	if errors.Is(err, ErrNotFound) {
		return // No activity: the member has Live Activities off, or dismissed it.
	}
	p.opts.Logger.WarnContext(ctx, "reading a live activity token failed", "jobId", job.ID, "error", err)
}

func (p *Pusher) now() time.Time { return p.opts.Now().UTC().Truncate(time.Second) }
