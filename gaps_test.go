package workerreprogrammingtimers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"
	"github.com/code-corhuila/multi-tour-worker-runtime/logging"
	"github.com/code-corhuila/multi-tour-worker-runtime/runner"

	reschedulingshttp "github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/adapter/out/http"
	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/application/usecase"
)

const (
	noProposal = "55555555-5555-4555-8555-555555555555" // 24 hours to propose ran out
	unanswered = "66666666-6666-4666-8666-666666666666" // 72 hours to answer a proposal ran out
	endless    = "77777777-7777-4777-8777-777777777777" // the 96 hours of the episode ran out
	raced      = "88888888-8888-4888-8888-888888888888" // the customer answered first: the service refuses with 422
)

// service is commercial-reservation as the worker meets it: one due list and the status it answers to the expiry of each episode.
type service struct {
	mu      sync.Mutex
	items   string
	status  map[string]int
	expiry  []string
	bodies  []string
	retries map[string]int
}

func (s *service) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		n := strings.Count(s.items, "{")
		_, _ = w.Write([]byte(`{"data":[` + s.items + `],"meta":{"page":1,"limit":100,"total":` + string(rune('0'+n)) + `,"totalPages":1}}`))
		return
	}
	b, _ := io.ReadAll(r.Body)
	s.expiry = append(s.expiry, strings.TrimPrefix(r.URL.Path, prefix))
	s.bodies = append(s.bodies, string(b))
	for id, status := range s.status {
		if strings.Contains(r.URL.Path, id) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"RESCHEDULING_NOT_DUE","message":"nothing is due","traceId":"t-1"}`))
			return
		}
	}
	_, _ = w.Write([]byte(`{}`))
}

func item(id, clock string) string {
	return `{"tenantId":"` + tenant + `","reschedulingId":"` + id + `","clock":"` + clock + `","dueAt":"2020-01-01T00:00:00Z"}`
}

func (s *service) runOnce(t *testing.T, logs io.Writer) runner.Result {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(api.Close)
	log := logging.New(logs)
	job := usecase.NewExpireDue(reschedulingshttp.NewReschedulings(httpclient.New(api.URL, runtimeconfig.Secret(token), httpclient.WithAttempts(3))), log)
	r, err := runner.New(job, runner.Options{Interval: time.Hour, BatchSize: 100, RunTimeout: 30 * time.Second, Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	return r.RunOnce(t.Context())
}

// HU-WRK-005 AC1 (24 hours to propose, 72 hours to answer, 96 hours of the episode; spec 018 AC-18.10, AC-18.11): the three clocks are the
// service's. The worker holds no clock: every episode the list names is expired in its tenant, whichever clock ran out, with no date sent.
func TestHU_WRK_005_AC1_every_clock_of_the_episode_is_expired_by_the_service_the_worker_sends_no_date(t *testing.T) {
	s := &service{items: item(noProposal, "PROPOSE_24H") + "," + item(unanswered, "RESPOND_72H") + "," + item(endless, "EPISODE_96H")}

	res := s.runOnce(t, io.Discard)

	want := []string{noProposal, unanswered, endless}
	if len(s.expiry) != 3 {
		t.Fatalf("expiry calls = %v, want one for each of the 24 h, 72 h and 96 h episodes", s.expiry)
	}
	for i, id := range want {
		if s.expiry[i] != "/tenants/"+tenant+"/reschedulings/"+id+"/expiry" {
			t.Errorf("call %d = %s, want the expiry of %s in its tenant", i, s.expiry[i], id)
		}
		if strings.TrimSpace(s.bodies[i]) != "" {
			t.Errorf("call %d carries a body %q: the worker must send no date or clock", i, s.bodies[i])
		}
	}
	if res.Processed != 3 || res.Failed != 0 {
		t.Errorf("processed %d failed %d, want 3 and 0", res.Processed, res.Failed)
	}
}

// AC-WK-05.3 (spec 018 AC-18.20), against the real refusal of the service (status 422 and its error body, not a stub of the port): the customer
// answered first, so the expiry of that episode is skipped, asked once and never retried, it is not a failure, and the next episode is still expired.
func TestHU_WRK_005_AC3_a_real_422_is_skipped_once_and_the_run_goes_on(t *testing.T) {
	s := &service{items: item(raced, "RESPOND_72H") + "," + item(endless, "EPISODE_96H"), status: map[string]int{raced: http.StatusUnprocessableEntity}}

	res := s.runOnce(t, io.Discard)

	if len(s.expiry) != 2 || !strings.Contains(s.expiry[0], raced) || !strings.Contains(s.expiry[1], endless) {
		t.Fatalf("expiry calls = %v, want the refused one asked once and then the next episode", s.expiry)
	}
	if res.Processed != 2 || res.Failed != 0 {
		t.Errorf("processed %d failed %d, want 2 and 0: a 422 is skipped, not failed", res.Processed, res.Failed)
	}
}

// AC-WK-05.5 against the real status: a 4xx other than 422 is not retried and counts as failed, the next episode is still expired.
func TestHU_WRK_005_AC5_a_real_404_is_not_retried_and_does_not_stop_the_run(t *testing.T) {
	s := &service{items: item(raced, "RESPOND_72H") + "," + item(endless, "EPISODE_96H"), status: map[string]int{raced: http.StatusNotFound}}

	res := s.runOnce(t, io.Discard)

	if len(s.expiry) != 2 || res.Processed != 1 || res.Failed != 1 {
		t.Errorf("expiry calls %v processed %d failed %d, want 2 calls (no retry), 1 processed and 1 failed", s.expiry, res.Processed, res.Failed)
	}
}
