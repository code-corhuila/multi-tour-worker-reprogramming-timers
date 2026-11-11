package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	runtimeconfig "github.com/code-corhuila/multi-tour-worker-runtime/config"
)

const defaultInterval = 5 * time.Minute // ADR-004 D10, HU-WRK-005 AC1

// Reprogramming is everything the worker reads from the environment: the shared runtime variables plus the interval, the health
// address and the internal base URL of the only service it calls. It carries no tenant list: the service lists across tenants.
type Reprogramming struct {
	Runtime                  runtimeconfig.Config
	Interval                 time.Duration
	HealthAddr               string
	CommercialReservationURL string
}

// LoadReprogramming reads the variables through getenv. Anything missing or malformed stops the worker at start; an error names
// the variable and never prints a value.
func LoadReprogramming(getenv func(string) string) (Reprogramming, error) {
	shared, err := runtimeconfig.Load(getenv)
	if err != nil {
		return Reprogramming{}, err
	}
	d := Reprogramming{Runtime: shared, Interval: defaultInterval, HealthAddr: ":8080"}
	if s := getenv("REPROGRAMMING_INTERVAL"); s != "" {
		if d.Interval, err = time.ParseDuration(s); err != nil || d.Interval <= 0 {
			return Reprogramming{}, fmt.Errorf("REPROGRAMMING_INTERVAL must be a positive duration")
		}
	}
	if s := getenv("HEALTH_ADDR"); s != "" {
		d.HealthAddr = s
	}
	if d.CommercialReservationURL, err = baseURL(getenv, "COMMERCIAL_RESERVATION_BASE_URL"); err != nil {
		return Reprogramming{}, err
	}
	return d, nil
}

func baseURL(getenv func(string) string, name string) (string, error) {
	raw := strings.TrimRight(getenv(name), "/")
	u, err := url.Parse(raw)
	// Only scheme://host[:port]: credentials, a query, a fragment or a path would reach every request.
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return "", fmt.Errorf("%s must be an http(s) url with only scheme, host and port", name)
	}
	return raw, nil
}
