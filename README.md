# multi-tour-worker-reprogramming-timers

> Worker job 5: reprogramming timers (commercial-reservation)

Part of the **Multi Tour** distributed system — team `multi-tour`, Grupo 2.
Governance and documentation live in [`multi-tour-docs`](https://github.com/code-corhuila/multi-tour-docs).

## What it is

Job 5 of ADR-004 D3 (HU-WRK-005). Every 5 minutes (`REPROGRAMMING_INTERVAL`), it asks `commercial-reservation` for the rescheduling episodes that have a clock run out, across tenants (`GET /api/v1/commercial-reservation/internal/reschedulings?due=true`, at most 5 batches per run), and asks it to apply the expiry of each one in its tenant (`POST .../internal/tenants/{tenantId}/reschedulings/{reschedulingId}/expiry`, with an `Idempotency-Key`). The service decides which clock ran out and cancels the reservation (HU-COM-018); the worker holds no deadline rule, has no database and exposes only `/health`.

## How to start it

Copy `.env.example` and fill `SERVICE_TOKEN` (never versioned; issued by `multi-tour-security`). The worker joins the external `platform` network and publishes no port, so `deploy/compose.yml` is run from the root composition of `multi-tour-infra-postgres`, which also provides commercial-reservation. The configuration is read at start and a bad variable stops the worker naming it.

## How to test it

`gofmt -l .` must print nothing, then `go vet ./...` and `go test ./...` (Go 1.26). The same run in `ci.yml` on pull requests to `develop` and `qa`. The unit tests use stubs of the service and need no database, no network and no Docker.

## Branching

Three permanent branches. **None of them accepts a direct commit** — you enter through a child
branch and leave through a Pull Request.

```
develop  <--PR--  feat/... fix/... chore/...
qa       <--PR--  qa/...
main     <--PR--  release/...  hotfix/...
```

Promotion happens **by re-application** (`git cherry-pick -x`), never by merging one permanent
branch into another: `merge develop -> qa` and `merge qa -> main` do not exist in this model.

`main` requires **1 approval from `ariel5253`**. On `develop` and `qa` the team sets its own review
rule.

Full policy: `00-governance/branching-policy.md` in `multi-tour-docs`.

## What it does

Job 5 of the platform, every five minutes (`REPROGRAMMING_INTERVAL`): it asks `commercial-reservation`, with its service credential,
for the rescheduling episodes that ran out of time across tenants and asks it to apply the expiry of each one. The service decides
which clock ran out and cancels the reservation; the job holds no deadline rule. It reads its configuration from the environment
(`.env.example`) and a bad value stops it at start.
