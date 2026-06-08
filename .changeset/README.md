# Changesets — Atomyx versioning guide

## When to add a changeset

Run `npx changeset add` when you change a public API or break a contract — for example:
- Removing or renaming a method on a domain port
- Changing a Zod schema that downstream scripts depend on
- Changing the wire protocol between the CLI/MCP and an agent driver

You do **not** need a changeset for internal refactors, test-only changes, or documentation edits.

## Tracked packages

These packages are versioned together (bumped in lock-step via `linked`):

| Package | Purpose |
|---|---|
| `@atomyx/shared` | Zod schemas, script schema, shared types |
| `@atomyx/core` | Core runtime, Orchestra, filters, selectors |
| `@atomyx/driver` | Driver port + testing kit |
| `@atomyx/driver-wire` | Wire protocol types |
| `@atomyx/android-driver` | Android Accessibility Service adapter |
| `@atomyx/ios-driver` | iOS XCUITest adapter |
| `@atomyx/ios-sim-driver` | iOS Simulator adapter |
| `@atomyx/mcp` | MCP server (27-tool surface) |
| `@atomyx/script` | Script parser and executor |
| `@atomyx/skills` | Bundled workflow skills |
| `@atomyx/sidecar` | Studio sidecar IPC server |

## Ignored packages (not published)

`@atomyx/studio` and `@atomyx/cli` are excluded from changeset versioning. They are private and released separately.

## Bump decision guide

| Change | Bump |
|---|---|
| New optional field, new tool, backward-compatible addition | `patch` |
| New capability that consumers must adopt to use | `minor` |
| Removed field, renamed method, tightened schema (breaking) | `major` |

## Release workflow

```
npx changeset add      # describe your change; pick affected packages + bump level
npx changeset version  # applies version bumps + updates CHANGELOGs (CI only)
npx changeset publish  # publishes to npm registry (CI only — do not run locally)
```
