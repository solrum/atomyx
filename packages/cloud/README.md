# @atomyx/cloud — module (skeleton)

**Status**: not yet implemented. Placeholder for the cloud
orchestration module per `.claude/docs/architecture.md` §6.

## Persona

**Cloud / Scale Operator** who needs to run tests against
device farms, manage worker pools, distribute load across
remote machines, and aggregate results centrally. Installs
`@atomyx/cloud-cli` to manage the infrastructure side.

## Planned packages

- `packages/cloud/` — `@atomyx/cloud` — module main: device
  registry, worker pool abstractions, scheduling primitives.
- `packages/worker/` — `@atomyx/cloud-worker` — long-running
  worker process that hosts a `core-driver` instance and
  reports up to an orchestrator.
- `packages/orchestrator/` — `@atomyx/cloud-orchestrator` —
  centralized scheduler that distributes test runs to workers.
- `packages/cli/` — `@atomyx/cloud-cli` — `atomyx-cloud`
  binary for ops + admin.

## Why a separate module

Cloud orchestration has independent operational concerns
(authentication, multi-tenant isolation, queueing, retries,
worker health checks) that have nothing to do with device
interaction or test management. Keeping it as a separate
module means single-machine users never pay the cloud
infrastructure cost.

## Composition

Workers compose `@atomyx/core-driver` in-process; the
orchestrator only knows about workers + jobs, not about
specific drivers. Studio talks to cloud through its own
client API at runtime when present.
