package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-runtime/config"
	"github.com/code-corhuila/multi-tour-worker-runtime/correlation"
	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"

	reschedulingshttp "github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/adapter/out/http"
)

const (
	tenant       = "11111111-1111-4111-8111-111111111111"
	reservation  = "33333333-3333-4333-8333-333333333333"
	rescheduling = "44444444-4444-4444-8444-444444444444"
	token        = "service-token-value"
	internal     = "/api/v1/commercial-reservation/internal"
)

type seen struct {
	method, path, query, auth, correlation, key string
}

// stub is commercial-reservation: it records each request and answers with the next scripted reply.
type stub struct {
	mu      sync.Mutex
	seen    []seen
	replies []func(w http.ResponseWriter)
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.seen = append(s.seen, seen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization"),
		r.Header.Get(correlation.Header), r.Header.Get("Idempotency-Key")})
	reply := s.replies[min(len(s.seen), len(s.replies))-1]
	s.mu.Unlock()
	reply(w)
}

func write(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func adapter(t *testing.T, replies ...func(http.ResponseWriter)) (*reschedulingshttp.Reschedulings, *stub) {
	t.Helper()
	s := &stub{replies: replies}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	client := httpclient.New(srv.URL, config.Secret(token), httpclient.WithAttempts(3),
		httpclient.WithSleeper(func(context.Context, time.Duration) error { return nil }))
	return reschedulingshttp.NewReschedulings(client), s
}

func run() context.Context { return correlation.WithID(context.Background(), "run-0000-aaaa") }

const dueList = `{"data":[{"tenantId":"` + tenant + `","reservationId":"` + reservation + `","reschedulingId":"` + rescheduling + `"}],"meta":{"page":1,"limit":100,"total":1,"totalPages":1}}`

// AC-WK-05.1: it lists the due episodes across tenants with the service credential and the correlation id of the run.
func TestA_lists_the_due_episodes_with_credential_and_correlation_id(t *testing.T) {
	a, s := adapter(t, write(200, dueList))

	got, err := a.ListDue(run(), 50)

	if err != nil || len(got) != 1 || got[0].TenantID != tenant || got[0].ReschedulingID != rescheduling {
		t.Fatalf("got %+v, %v", got, err)
	}
	r := s.seen[0]
	if r.method != "GET" || r.path != internal+"/reschedulings" || r.query != "due=true&limit=50" {
		t.Errorf("request = %+v", r)
	}
	if r.auth != "Bearer "+token || r.correlation != "run-0000-aaaa" {
		t.Errorf("credentials = %q, correlation %q", r.auth, r.correlation)
	}
}

// AC-WK-05.2: the expiry is a POST with the tenant in the route and an Idempotency-Key, and a 200 means the service applied it.
func TestA_expires_each_due_episode_in_its_tenant(t *testing.T) {
	a, s := adapter(t, write(200, `{"reschedulingId":"`+rescheduling+`","episodeState":"CANCELLED"}`))

	got, err := a.Expire(run(), tenant, rescheduling, "expiry-key-0001")

	if err != nil || !got.Expired || got.Skipped {
		t.Fatalf("got %+v, %v", got, err)
	}
	r := s.seen[0]
	if r.method != "POST" || r.path != internal+"/tenants/"+tenant+"/reschedulings/"+rescheduling+"/expiry" || r.key != "expiry-key-0001" {
		t.Errorf("request = %+v", r)
	}
	if r.auth != "Bearer "+token || r.correlation != "run-0000-aaaa" {
		t.Errorf("credentials = %q, correlation %q", r.auth, r.correlation)
	}
}

// AC-WK-05.3: a 422 means nothing is due after all (an answer or a freeze came first): skipped, not an error, not retried.
func TestA_a_422_is_skipped_and_not_retried(t *testing.T) {
	a, s := adapter(t, write(422, `{"error":"BUSINESS_RULE_VIOLATION","message":"nothing is due","traceId":"t1"}`))

	got, err := a.Expire(run(), tenant, rescheduling, "expiry-key-0001")

	if err != nil || got.Expired || !got.Skipped || len(s.seen) != 1 {
		t.Fatalf("got %+v, %v after %d calls, want a skipped outcome after one call", got, err, len(s.seen))
	}
}

// AC-WK-05.5: network failures, 429 and 5xx are retried up to the limit with the same key; any other 4xx is not.
func TestI_retries_network_429_and_5xx_not_other_4xx(t *testing.T) {
	a, s := adapter(t, write(503, `{"error":"UNAVAILABLE","message":"x","traceId":"t1"}`),
		write(429, `{"error":"TOO_MANY","message":"x","traceId":"t2"}`), write(200, `{"reschedulingId":"`+rescheduling+`"}`))

	got, err := a.Expire(run(), tenant, rescheduling, "expiry-key-0001")

	if err != nil || !got.Expired || len(s.seen) != 3 || s.seen[0].key != s.seen[2].key {
		t.Fatalf("got %+v %v after %d calls", got, err, len(s.seen))
	}
	a, s = adapter(t, write(503, `{"error":"UNAVAILABLE","message":"x","traceId":"t1"}`))
	if _, err := a.Expire(run(), tenant, rescheduling, "expiry-key-0001"); err == nil || len(s.seen) != 3 {
		t.Errorf("error %v after %d calls, want a failure after the 3 attempts", err, len(s.seen))
	}
	for _, status := range []int{401, 403, 404} {
		a, s := adapter(t, write(status, `{"error":"X","message":"no","traceId":"t"}`))
		if _, err := a.Expire(run(), tenant, rescheduling, "expiry-key-0001"); err == nil || len(s.seen) != 1 {
			t.Errorf("%d: error %v after %d calls, want one call and a failure", status, err, len(s.seen))
		}
	}
}

// An id that is not a UUID never reaches a URL path.
func TestA_refuses_an_id_that_is_not_a_uuid_before_calling(t *testing.T) {
	a, s := adapter(t, write(200, dueList))

	_, err1 := a.Expire(run(), tenant, "../../x", "expiry-key-0001")
	_, err2 := a.Expire(run(), "acme", rescheduling, "expiry-key-0001")
	_, err3 := a.ListDue(run(), 0)

	if err1 == nil || err2 == nil || err3 == nil || len(s.seen) != 0 {
		t.Errorf("errors %v %v %v, calls %d, want three refusals and no call", err1, err2, err3, len(s.seen))
	}
}

// A malformed 2xx is a failure, and no body text is copied into the error.
func TestA_a_list_that_is_not_the_contract_is_an_error(t *testing.T) {
	a, _ := adapter(t, write(200, `{"data":[{"tenantId":"nope","reschedulingId":"`+rescheduling+`"}],"meta":{}}`))

	_, err := a.ListDue(run(), 10)

	if err == nil || strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want a refusal that does not echo the body", err)
	}
}
