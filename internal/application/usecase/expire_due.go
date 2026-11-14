// Package usecase holds the jobs of the worker.
package usecase

import (
	"context"
	"fmt"
	"log/slog"

	workerruntime "github.com/code-corhuila/multi-tour-worker-runtime"
	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"

	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/application/port/out"
)

// maxBatches bounds a run: the rest of the due episodes waits for the next run (AC-WK-05.4, Annex D).
const maxBatches = 5

// ExpireDue is job 5: it asks the service for the rescheduling episodes that have a clock run out, across tenants, and asks it to
// apply the expiry of each one in its tenant. The service decides which clock ran out and cancels the reservation (HU-COM-018);
// the job holds no deadline rule.
type ExpireDue struct {
	episodes out.Reschedulings
	log      *slog.Logger
}

var _ workerruntime.Job = (*ExpireDue)(nil)

// NewExpireDue wires the job over the service.
func NewExpireDue(episodes out.Reschedulings, log *slog.Logger) *ExpireDue {
	return &ExpireDue{episodes: episodes, log: log}
}

// Name is how the runner logs the job.
func (*ExpireDue) Name() string { return "reprogramming-timers" }

// Run lists batches of at most batch episodes and goes on while a batch comes back full, up to five batches. Each item is counted as
// processed (expired or skipped) or failed; one failure never stops the run.
func (j *ExpireDue) Run(ctx context.Context, batch int) (processed, failed int) {
	seen := map[string]bool{}
	for range maxBatches {
		if ctx.Err() != nil {
			break
		}
		due, err := j.episodes.ListDue(ctx, batch)
		if err != nil {
			failed++
			j.log.WarnContext(ctx, "the due episodes could not be listed, the next run asks again", "error", err)
			break
		}
		listed := len(due)
		due = unseen(due, seen)
		if listed == 0 {
			j.log.InfoContext(ctx, "none due: no rescheduling episode has a clock run out")
		}
		if len(due) == 0 {
			break
		}
		done, bad := workerruntime.Each(ctx, j.log, due, j.expire)
		processed, failed = processed+done, failed+bad
		j.log.InfoContext(ctx, "rescheduling episodes swept", "listed", len(due), "handled", done, "failed", bad)
		if listed < batch {
			break
		}
	}
	return processed, failed
}

// expire asks for the expiry. The key names the episode and the run: a retry inside the run repeats it, while a later run asks again.
func (j *ExpireDue) expire(ctx context.Context, d out.Due) error {
	key := fmt.Sprintf("expiry-%s-%s", d.ReschedulingID, correlation.From(ctx))
	outcome, err := j.episodes.Expire(ctx, d.TenantID, d.ReschedulingID, key)
	if err != nil {
		return fmt.Errorf("expire episode %s of tenant %s: %w", d.ReschedulingID, d.TenantID, err)
	}
	if outcome.Skipped {
		j.log.InfoContext(ctx, "episode skipped: nothing was due after all", "tenantId", d.TenantID, "reschedulingId", d.ReschedulingID)
		return nil
	}
	j.log.InfoContext(ctx, "episode expired", "tenantId", d.TenantID, "reschedulingId", d.ReschedulingID)
	return nil
}

// unseen drops the episodes already handled in this run: the service keeps listing an episode it answered 422 for, so asking again
// inside the run would repeat the same call, and a full batch of them would fill every one of the batches of the run.
func unseen(due []out.Due, seen map[string]bool) []out.Due {
	var fresh []out.Due
	for _, d := range due {
		if !seen[d.ReschedulingID] {
			seen[d.ReschedulingID] = true
			fresh = append(fresh, d)
		}
	}
	return fresh
}
