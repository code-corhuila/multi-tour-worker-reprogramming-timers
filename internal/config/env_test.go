package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/config"
)

func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

var required = []string{
	"SERVICE_TOKEN", "secret-token-value",
	"COMMERCIAL_RESERVATION_BASE_URL", "http://commercial-reservation-api:8083/",
}

func with(extra ...string) []string { return append(append([]string{}, required...), extra...) }

// DP-WRK5-2: every five minutes by default, and the shared runtime variables travel with it. The worker carries no tenant list.
func TestLoadReprogramming_applies_the_declared_defaults(t *testing.T) {
	got, err := config.LoadReprogramming(env(required...))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interval != 5*time.Minute || got.HealthAddr != ":8080" || got.Runtime.BatchSize != 100 || got.Runtime.RunTimeout != 30*time.Second {
		t.Errorf("defaults = %+v", got)
	}
	if got.CommercialReservationURL != "http://commercial-reservation-api:8083" {
		t.Errorf("url = %q, the trailing slash should go", got.CommercialReservationURL)
	}
}

func TestLoadReprogramming_reads_its_own_variables(t *testing.T) {
	got, err := config.LoadReprogramming(env(with("REPROGRAMMING_INTERVAL", "30s", "HEALTH_ADDR", ":9090")...))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interval != 30*time.Second || got.HealthAddr != ":9090" {
		t.Errorf("got %+v", got)
	}
}

// AC-WK-05.9: a missing or malformed variable stops the worker with its name, never its value.
func TestU_config_names_the_bad_variable(t *testing.T) {
	cases := map[string][]string{
		"SERVICE_TOKEN":                   {"COMMERCIAL_RESERVATION_BASE_URL", "http://x:8083"},
		"COMMERCIAL_RESERVATION_BASE_URL": {"SERVICE_TOKEN", "secret-token-value"},
		"REPROGRAMMING_INTERVAL":          with("REPROGRAMMING_INTERVAL", "soon"),
	}
	for name, pairs := range cases {
		_, err := config.LoadReprogramming(env(pairs...))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error = %v, want one that names the variable", name, err)
			continue
		}
		if strings.Contains(err.Error(), "secret-token-value") {
			t.Errorf("%s: the error prints a secret: %v", name, err)
		}
	}
}

func TestLoadReprogramming_refuses_a_base_url_with_a_path_or_credentials(t *testing.T) {
	for _, bad := range []string{"http://user:pw@host:8083", "http://host:8083/api", "ftp://host", "http://host:8083?x=1"} {
		if _, err := config.LoadReprogramming(env(with("COMMERCIAL_RESERVATION_BASE_URL", bad)...)); err == nil {
			t.Errorf("url %q was accepted", bad)
		}
	}
}
