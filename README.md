# multi-tour-worker-reprogramming-timers

> Worker job 5: reprogramming timers (commercial-reservation)

Part of the **Multi Tour** distributed system — team `multi-tour`, Grupo 2.
Governance and documentation live in [`multi-tour-docs`](https://github.com/code-corhuila/multi-tour-docs).

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
(`.env.example`) and a bad value stops it at start. This first piece is the module, the configuration and `/health`; the job comes next.
