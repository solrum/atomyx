# examples/ — runnable demos

Each subdirectory is a self-contained example showing how to
compose Atomyx modules for a specific use case. Examples are
**not** workspace members — they have their own `package.json`
and `npm install` independently. This prevents npm from trying
to install example dependencies into the root `node_modules`.

## Planned examples

- `atomyx-demo/` — Flutter cross-platform demo app, wired for
  both iOS and Android. Used by the script and MCP examples.
- `mcp-quickstart/` — minimal MCP client driving a device with
  just `@atomyx/mcp` + a driver adapter installed.
- `script-login-flow/` — runnable YAML script demonstrating
  login + OTP-branching against the demo app.
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
