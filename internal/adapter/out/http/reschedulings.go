// Package http is the output adapter that calls commercial-reservation on its internal routes.
package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/code-corhuila/multi-tour-worker-runtime/httpclient"

	"github.com/code-corhuila/multi-tour-worker-reprogramming-timers/internal/application/port/out"
)

const (
	listPath   = "/api/v1/commercial-reservation/internal/reschedulings?due=true&limit=%d"
	expiryPath = "/api/v1/commercial-reservation/internal/tenants/%s/reschedulings/%s/expiry"
)

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Reschedulings calls commercial-reservation through the client of the runtime.
type Reschedulings struct {
	client *httpclient.Client
}

var _ out.Reschedulings = (*Reschedulings)(nil)

// NewReschedulings returns the adapter over a client whose base URL is commercial-reservation.
func NewReschedulings(client *httpclient.Client) *Reschedulings {
	return &Reschedulings{client: client}
}

type dueItem struct {
	TenantID       string `json:"tenantId"`
	ReschedulingID string `json:"reschedulingId"`
}

// ListDue reads GET .../reschedulings?due=true across tenants (DP-WRK5-1).
func (r *Reschedulings) ListDue(ctx context.Context, limit int) ([]out.Due, error) {
	if limit < 1 {
		return nil, fmt.Errorf("a positive limit is required")
	}
	got, err := httpclient.List[dueItem](ctx, r.client, fmt.Sprintf(listPath, limit))
	if err != nil {
		return nil, fmt.Errorf("list the due episodes: %w", err)
	}
	due := make([]out.Due, 0, len(got.Data))
	for _, item := range got.Data {
		if !uuid.MatchString(item.TenantID) || !uuid.MatchString(item.ReschedulingID) {
			return nil, fmt.Errorf("list the due episodes: an item has no tenant or episode uuid")
		}
		due = append(due, out.Due{TenantID: item.TenantID, ReschedulingID: item.ReschedulingID})
	}
	return due, nil
}

// Expire is a POST with an Idempotency-Key, so the runtime may repeat it after a network failure, a 429 or a 5xx. An answer 422 is
// the outcome Skipped, never an error (DP-WRK5-3); any other final failure is an error that the job counts as failed.
func (r *Reschedulings) Expire(ctx context.Context, tenantID, reschedulingID, idempotencyKey string) (out.Outcome, error) {
	if !uuid.MatchString(tenantID) || !uuid.MatchString(reschedulingID) {
		return out.Outcome{}, fmt.Errorf("a tenant uuid and an episode uuid are required")
	}
	err := r.client.Do(ctx, httpclient.Request{
		Method:         http.MethodPost,
		Path:           fmt.Sprintf(expiryPath, tenantID, reschedulingID),
		IdempotencyKey: idempotencyKey,
	}, nil)
	var apiErr *httpclient.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnprocessableEntity {
		return out.Outcome{Skipped: true}, nil
	}
	if err != nil {
		return out.Outcome{}, fmt.Errorf("expire the episode: %w", err)
	}
	return out.Outcome{Expired: true}, nil
}
