// Package out holds the ports the job needs from the outside world.
package out

import "context"

// Due is one rescheduling episode that has a clock run out, as the list names it. The service re-checks it at the expiry.
type Due struct {
	TenantID       string
	ReschedulingID string
}

// Outcome is what the service decided when asked to expire an episode: applied, or skipped because nothing was due after all.
type Outcome struct {
	Expired bool
	// Skipped is the answer 422: an answer or a freeze arrived first. It is not a failure and it is not retried.
	Skipped bool
}

// Reschedulings is commercial-reservation as the job sees it: the application never learns that it is HTTP.
type Reschedulings interface {
	// ListDue reads the first limit due episodes across tenants.
	ListDue(ctx context.Context, limit int) ([]Due, error)
	// Expire asks the service to apply the expiry of one episode of a tenant. The service decides; it is safe to repeat with the same key.
	Expire(ctx context.Context, tenantID, reschedulingID, idempotencyKey string) (Outcome, error)
}
