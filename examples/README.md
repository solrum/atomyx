# examples/ — runnable demos

Each subdirectory is a self-contained example showing how to
compose Atomyx modules for a specific use case. Examples are
**not** workspace members — they have their own `package.json`
and `npm install` independently. This prevents npm from trying
to install example dependencies into the root `node_modules`.

## Planned examples

- `core-driver-only/` — minimal MCP client driving an Android
  device with just `@atomyx/core-driver-cli` installed.
- `core-driver-ios/` — same for iOS.
- `mode-b-spec/` — running a YAML test spec end-to-end.
- `mode-c-explore/` — exploratory agent loop.
- `studio-quickstart/` — Studio + core-driver power-user
  workflow (when Studio ships).
- `synapse-integration/` — using core-driver with Synapse
  test management as an external storage backend.

## Convention

Each example has:

- `README.md` — what it shows + how to run
- `package.json` — independent deps
- Source files
- Optional `.atomyx/` workspace dir with sample test cases

The examples deliberately install Atomyx modules from npm (or
from the local workspace via `file:` paths in dev) — they do
NOT reach into `modules/*/packages/*/src/` directly.

Currently empty pending real examples.
