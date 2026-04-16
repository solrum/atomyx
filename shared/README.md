# shared/ — cross-module type contracts

Type-only contracts shared between modules. **No business
logic, no runtime code** — just types, schema definitions,
and protocol specs.

The point of this directory is to give modules a place to
share type definitions WITHOUT introducing a runtime
dependency. If `@atomyx/test-mgmt` and `@atomyx/core-driver`
both need to agree on the shape of a `RunResult` envelope,
that shape lives here as a `.d.ts` or a Zod schema, and both
modules import it as a peer type.

This avoids two anti-patterns:

1. **Duplicating types in each module** — drift hazard, runtime
   shape mismatches.
2. **Having one module import another's types** — would create
   a hidden coupling that violates module independence.

## When to add something here

- Cross-module wire shapes (e.g. the `RunResult` produced by
  core-driver and consumed by test-mgmt + studio).
- Shared semver constants or capability negotiations.
- Protocol version tags.

## When NOT to add something here

- Module-internal types — keep those in the module's own
  `packages/<x>/src/types.ts`.
- Anything with runtime behavior — that's a package, not a
  type contract.
- Stuff only one module uses — speculative sharing creates
  drag without benefit.

Currently empty pending real cross-module contracts.
