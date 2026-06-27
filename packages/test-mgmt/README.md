# @atomyx/test-mgmt — module (skeleton)

**Status**: not yet implemented. Placeholder for the test-
management module per `.claude/docs/architecture.md` §6.

## Persona

**QC Manager** who organizes test cases, plans, and reports
WITHOUT necessarily driving devices directly. Installs
`@atomyx/test-mgmt` alone; gets case storage, YAML spec parsing,
report aggregation, and an MCP server exposing the case
repository to agents.

## Planned packages

- `@atomyx/test-mgmt` — module main: case models, YAML spec
  parser, report storage abstractions, query interface.
- `@atomyx/test-mgmt-mcp` — MCP server exposing case repository
  operations to agents.
- `@atomyx/test-mgmt-cli` — CLI binary (`atomyx-test`).
- `@atomyx/test-mgmt-storage-file` — default filesystem storage
  backend.
- Additional storage adapters may ship as separate packages if
  downstream integrations need them.

## Composition with the driver module

When both `@atomyx/test-mgmt` and `@atomyx/cli` are installed,
the CLI can compose them: load a case from test-mgmt, run it
through the driver, save the report back via test-mgmt. The
composition is in-process — no HTTP between modules.

When only test-mgmt is installed, the QC Manager uses it purely
for case organization without device interaction.
