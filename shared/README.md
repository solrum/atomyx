# shared/ — cross-module type contracts

Type-only contracts shared between modules. **No business
logic, no runtime code** — just types, schema definitions,
and protocol specs.

The point of this directory is to give modules a place to share
type definitions WITHOUT introducing a runtime dependency. If
two modules need to agree on the shape of a wire payload or a
cross-module envelope, that shape lives here as a `.d.ts` or a
Zod schema, and both modules import it as a peer type.

This avoids two anti-patterns:

1. **Duplicating types in each module** — drift hazard, runtime
   shape mismatches.
2. **Having one module import another's types** — would create
   a hidden coupling that violates module independence.

## When to add something here

- Cross-module wire shapes consumed by more than one module.
- Shared semver constants or capability negotiations.
- Protocol version tags.

## When NOT to add something here

- Module-internal types — keep those in the module's own
  `packages/<x>/src/types.ts`.
- Anything with runtime behavior — that's a package, not a type
  contract.
- Stuff only one module uses — speculative sharing creates drag
  without benefit.

## Current contents

- `src/script/` — script-definition types that cross the
  `@atomyx/script` → `@atomyx/cli` / `@atomyx/mcp` boundary
  (`ScriptDefinition`, `ScriptStep`, `CaptureConfig`,
  `CapturedRequest`, `ScriptArtifacts`, …).
