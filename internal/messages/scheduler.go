package messages

import (
	"context"
	"log/slog"
	"time"

	"github.com/go-tangra/go-tangra-notification/v4/internal/authz"
	"github.com/go-tangra/go-tangra-notification/v4/internal/repo"
)

// SchedulerConfig bounds the worker.
type SchedulerConfig struct {
	Interval time.Duration
	Lease    time.Duration
	Batch    int
}

// Scheduler publishes due messages: a SQL lease claim hands every due
// message to exactly one instance; a crash mid-publish leaves the lease to
// expire and the idempotent fan-out finishes on the next claim (research R5).
type Scheduler struct {
	st   repo.Store
	svc  *Service
	cfg  SchedulerConfig
	log  *slog.Logger
	tick func()
	now  func() time.Time
}

// NewScheduler wires the worker.
func NewScheduler(st repo.Store, svc *Service, cfg SchedulerConfig, log *slog.Logger, tick func()) *Scheduler {
	if cfg.Interval <= 0 {
		cfg.Interval = 15 * time.Second
	}
	if cfg.Lease <= 0 {
		cfg.Lease = time.Minute
	}
	if cfg.Batch <= 0 {
		cfg.Batch = 50
	}
	if tick == nil {
		tick = func() {}
	}
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{st: st, svc: svc, cfg: cfg, log: log, tick: tick, now: time.Now}
}

// SetClock injects the clock (tests).
func (w *Scheduler) SetClock(now func() time.Time) { w.now = now }

// Once claims and publishes one batch; returns the number published.
func (w *Scheduler) Once(ctx context.Context) (int, error) {
	due, err := w.st.ClaimDueMessages(ctx, w.now(), w.cfg.Lease, w.cfg.Batch)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range due {
		if _, _, err := w.svc.Publish(ctx, m.TenantID, m.ID, authz.Subjects{TenantID: m.TenantID, Service: "system"}); err != nil {
			w.log.Warn("scheduled publish failed; will retry after the lease", "message", m.ID, "err", err)
			continue
		}
		n++
	}
	w.tick()
	return n, nil
}

// Run publishes every interval until ctx ends.
func (w *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(w.cfg.Interval)
	defer t.Stop()
	for {
		if _, err := w.Once(ctx); err != nil && ctx.Err() == nil {
			w.log.Warn("scheduler claim failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
