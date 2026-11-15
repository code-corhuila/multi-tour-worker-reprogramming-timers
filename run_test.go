package workerreprogrammingtimers_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"
	"github.com/code-corhuila/multi-tour-worker-runtime/runner"

	reschedulingshttp "github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/adapter/out/http"
	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/application/usecase"
)

const (
	tenant  = "11111111-1111-4111-8111-111111111111"
	episode = "44444444-4444-4444-8444-444444444444"
	token   = "service-token-value"
	prefix  = "/api/v1/commercial-reservation/internal"
)

// AC-WK-05.1: with a fake clock, each interval that passes is one run that lists the due episodes with the credential and a new
// correlation id; an interval that passes while a run is alive does not start another.
func TestI_lists_every_interval_with_the_fake_clock(t *testing.T) {
	var mu sync.Mutex
	var lists []string
	var expiries int
	listed := make(chan struct{}, 8)
	expired := make(chan struct{}, 8)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == prefix+"/reschedulings":
			if r.Header.Get("Authorization") != "Bearer "+token || r.URL.RawQuery != "due=true&limit=100" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			lists = append(lists, r.Header.Get("X-Correlation-Id"))
			listed <- struct{}{}
			_, _ = w.Write([]byte(`{"data":[{"tenantId":"` + tenant + `","reschedulingId":"` + episode + `"}],"meta":{"page":1,"limit":100,"total":1,"totalPages":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == prefix+"/tenants/"+tenant+"/reschedulings/"+episode+"/expiry":
			expiries++
			expired <- struct{}{}
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := httpclient.New(api.URL, runtimeconfig.Secret(token))
	job := usecase.NewExpireDue(reschedulingshttp.NewReschedulings(client), log)
	ticks := make(chan time.Time)
	r, err := runner.New(job, runner.Options{Interval: 5 * time.Minute, BatchSize: 100, RunTimeout: 10 * time.Second, Logger: log, Ticks: ticks})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	ctx, stop := context.WithCancel(t.Context())
	go func() { r.Start(ctx); close(done) }()

	// A tick that arrives while the previous run is alive is skipped by design, so each interval is offered until a run starts.
	for i := 0; i < 3; i++ {
		started := false
		for attempt := 0; attempt < 100 && !started; attempt++ {
			ticks <- time.Now()
			select {
			case <-listed:
				started = true
			case <-time.After(time.Second):
			}
		}
		select {
		case <-expired:
		case <-time.After(5 * time.Second):
			t.Fatalf("interval %d did not expire its episode", i+1)
		}
		if !started {
			t.Fatalf("interval %d did not list the due episodes", i+1)
		}
	}
	stop()
	<-done // Start returns only when the run in flight ended, so reading below is safe

	mu.Lock()
	defer mu.Unlock()
	if expiries != 3 {
		t.Errorf("expiries = %d, want one per listed episode", expiries)
	}
	if len(lists) != 3 {
		t.Fatalf("lists = %d, want one per interval", len(lists))
	}
	seen := map[string]bool{}
	for _, c := range lists {
		if c == "" || seen[c] {
			t.Errorf("correlation ids %v: each run needs its own, never empty", lists)
		}
		seen[c] = true
	}
}
