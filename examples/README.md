# examples/ — runnable demos

Each subdirectory is a self-contained example showing how to
compose Atomyx modules for a specific use case. Examples are
**not** workspace members — they have their own `package.json`
and `npm install` independently. This prevents npm from trying
to install example dependencies into the root `node_modules`.

## Current contents

- `atomyx-demo/` — Flutter cross-platform demo app used by the
  script examples. Bundle id `dev.atomyx.demo` on both
  platforms.
- `test-login-flow.yml` — runnable YAML script: login +
  OTP-branching flow against `atomyx-demo`.

## Planned examples

- `driver-only/` — minimal MCP client driving an Android device
  with just `@atomyx/cli` installed.
- `driver-ios/` — same for iOS.
- `mode-b-spec/` — running a YAML test spec end-to-end.
- `mode-c-explore/` — exploratory agent loop.
- `studio-quickstart/` — Studio power-user workflow (when the
  Studio module ships).

## Convention

Each future subdirectory-based example will carry:

- `README.md` — what it shows + how to run.
- `package.json` — independent deps (examples are NOT workspace
  members).
- Source files.
- Optional `.atomyx/` workspace dir with sample test cases.

The examples deliberately install Atomyx modules from npm (or
from the local workspace via `file:` paths in dev) — they do
NOT reach into package `src/` directly.
