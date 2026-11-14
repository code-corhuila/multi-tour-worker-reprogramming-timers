package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	workerruntime "github.com/code-corhuila/multi-tour-worker-runtime"
	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"

	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/application/port/out"
	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/application/usecase"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

type expiry struct{ tenant, rescheduling, key string }

// fake is commercial-reservation as the job sees it: each call of ListDue answers the next scripted batch.
type fake struct {
	mu       sync.Mutex
	batches  [][]out.Due
	listErr  error
	expireBy func(rescheduling string) (out.Outcome, error)
	limits   []int
	calls    []expiry
}

func (f *fake) ListDue(_ context.Context, limit int) ([]out.Due, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits = append(f.limits, limit)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(f.limits) > len(f.batches) {
		return nil, nil
	}
	return f.batches[len(f.limits)-1], nil
}

func (f *fake) Expire(_ context.Context, tenant, rescheduling, key string) (out.Outcome, error) {
	f.mu.Lock()
	f.calls = append(f.calls, expiry{tenant, rescheduling, key})
	f.mu.Unlock()
	if f.expireBy != nil {
		return f.expireBy(rescheduling)
	}
	return out.Outcome{Expired: true}, nil
}

func dues(tenant string, ids ...string) []out.Due {
	var d []out.Due
	for _, id := range ids {
		d = append(d, out.Due{TenantID: tenant, ReschedulingID: id})
	}
	return d
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)) }

func job(f *fake) *usecase.ExpireDue { return usecase.NewExpireDue(f, quiet()) }

var _ workerruntime.Job = (*usecase.ExpireDue)(nil)

// AC-WK-05.2: it expires each due episode in its own tenant.
func TestA_expires_each_due_episode_in_its_tenant(t *testing.T) {
	f := &fake{batches: [][]out.Due{append(dues(tenantA, "e1"), dues(tenantB, "e2")...)}}

	processed, failed := job(f).Run(context.Background(), 100)

	if processed != 2 || failed != 0 || len(f.calls) != 2 {
		t.Fatalf("processed %d failed %d calls %+v", processed, failed, f.calls)
	}
	if f.calls[0].tenant != tenantA || f.calls[0].rescheduling != "e1" || f.calls[1].tenant != tenantB || f.calls[1].rescheduling != "e2" {
		t.Errorf("calls = %+v", f.calls)
	}
}

// AC-WK-05.4: a full batch asks again and the run stops at five batches; the rest waits for the next run.
func TestA_repeats_while_the_batch_is_full_and_stops_at_five(t *testing.T) {
	// expired episodes leave the list, so every batch holds other episodes.
	f := &fake{batches: [][]out.Due{dues(tenantA, "a1", "a2"), dues(tenantA, "b1", "b2"), dues(tenantA, "c1", "c2"), dues(tenantA, "d1", "d2"), dues(tenantA, "e1", "e2"), dues(tenantA, "f1", "f2")}}

	job(f).Run(context.Background(), 2)

	if len(f.limits) != 5 || f.limits[0] != 2 {
		t.Errorf("limits = %v, want five lists of 2", f.limits)
	}
	g := &fake{batches: [][]out.Due{dues(tenantA, "e1", "e2"), dues(tenantA, "e3")}}
	job(g).Run(context.Background(), 2)
	if len(g.limits) != 2 {
		t.Errorf("a batch that comes back not full ends the run: lists = %v", g.limits)
	}
}

// AC-WK-05.3: a skipped episode (the service answered 422) is not a failure and the run goes on.
func TestA_a_skipped_episode_is_not_a_failure(t *testing.T) {
	f := &fake{batches: [][]out.Due{dues(tenantA, "e1", "e2")}}
	f.expireBy = func(r string) (out.Outcome, error) {
		if r == "e1" {
			return out.Outcome{Skipped: true}, nil
		}
		return out.Outcome{Expired: true}, nil
	}

	processed, failed := job(f).Run(context.Background(), 100)

	if processed != 2 || failed != 0 || len(f.calls) != 2 {
		t.Fatalf("processed %d failed %d calls %d, want 2, 0 and 2", processed, failed, len(f.calls))
	}
}

// AC-WK-05.5: a failed item is counted and the run goes on; the next run lists it again.
func TestA_a_failed_item_does_not_stop_the_run(t *testing.T) {
	f := &fake{batches: [][]out.Due{dues(tenantA, "e1", "e2", "e3")}}
	f.expireBy = func(r string) (out.Outcome, error) {
		if r == "e2" {
			return out.Outcome{}, errors.New("boom")
		}
		return out.Outcome{Expired: true}, nil
	}

	processed, failed := job(f).Run(context.Background(), 100)

	if processed != 2 || failed != 1 || len(f.calls) != 3 {
		t.Fatalf("processed %d failed %d calls %d, want 2, 1 and 3", processed, failed, len(f.calls))
	}
}

// AC-WK-05.6: an empty list is a quiet run: nothing is asked of the service and it is logged that none was due.
func TestA_an_empty_list_is_a_quiet_run(t *testing.T) {
	var logs bytes.Buffer
	f := &fake{batches: [][]out.Due{nil}}

	processed, failed := usecase.NewExpireDue(f, slog.New(slog.NewTextHandler(&logs, nil))).Run(context.Background(), 100)

	if processed != 0 || failed != 0 || len(f.calls) != 0 || len(f.limits) != 1 {
		t.Fatalf("processed %d failed %d calls %d lists %d", processed, failed, len(f.calls), len(f.limits))
	}
	if !strings.Contains(logs.String(), "none due") {
		t.Errorf("log = %q, want a line saying none was due", logs.String())
	}
}

// A list that fails is one failure and the next run asks again.
func TestA_a_list_that_fails_is_one_failure(t *testing.T) {
	f := &fake{listErr: errors.New("down")}

	processed, failed := job(f).Run(context.Background(), 100)

	if processed != 0 || failed != 1 || len(f.limits) != 1 {
		t.Fatalf("processed %d failed %d lists %d", processed, failed, len(f.limits))
	}
}

// AC-WK-05.7: every line carries the ids of tenant and episode, and no line carries the credential; the key is stable inside a run and new in the next.
func TestA_log_lines_carry_the_correlation_id_and_never_the_token(t *testing.T) {
	var logs bytes.Buffer
	f := &fake{batches: [][]out.Due{dues(tenantA, "e1"), dues(tenantA, "e1")}}
	j := usecase.NewExpireDue(f, slog.New(slog.NewTextHandler(&logs, nil)))

	j.Run(correlation.WithID(context.Background(), "run-one-0000"), 100)
	j.Run(correlation.WithID(context.Background(), "run-two-0000"), 100)

	if !strings.Contains(logs.String(), tenantA) || !strings.Contains(logs.String(), "e1") {
		t.Errorf("log = %q, want the ids of tenant and episode", logs.String())
	}
	if strings.Contains(strings.ToLower(logs.String()), "bearer") || strings.Contains(strings.ToLower(logs.String()), "token") {
		t.Errorf("log = %q, the credential never appears", logs.String())
	}
	if f.calls[0].key == f.calls[1].key {
		t.Errorf("another run reused the key %q", f.calls[0].key)
	}
	if k := f.calls[0].key; len(k) < 8 || len(k) > 128 || !strings.Contains(k, "e1") {
		t.Errorf("key %q must be 8 to 128 characters and name the episode", k)
	}
}

// Annex D: a run stops when RUN_TIMEOUT expires.
func TestA_a_done_context_stops_the_run(t *testing.T) {
	f := &fake{batches: [][]out.Due{dues(tenantA, "e1", "e2")}}
	ctx, cancel := context.WithCancel(context.Background())
	f.expireBy = func(string) (out.Outcome, error) { cancel(); return out.Outcome{Expired: true}, nil }

	processed, _ := job(f).Run(ctx, 100)

	if processed != 1 || len(f.calls) != 1 {
		t.Fatalf("processed %d calls %d, want the run to stop after the first item", processed, len(f.calls))
	}
}

// S3: an episode the service answers 422 for stays listed; a full batch of them must not be asked again inside the same run.
func TestA_a_full_batch_of_skipped_episodes_is_not_repeated_inside_the_run(t *testing.T) {
	full := dues(tenantA, "e1", "e2")
	f := &fake{
		batches:  [][]out.Due{full, full, full, full, full},
		expireBy: func(string) (out.Outcome, error) { return out.Outcome{Skipped: true}, nil },
	}

	processed, failed := job(f).Run(context.Background(), 2)

	if len(f.calls) != 2 || processed != 2 || failed != 0 {
		t.Fatalf("each skipped episode must be asked once per run: %d calls, processed %d, failed %d, lists %d", len(f.calls), processed, failed, len(f.limits))
	}
}
