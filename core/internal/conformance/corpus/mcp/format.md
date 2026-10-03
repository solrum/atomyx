# MCP corpus format

## Purpose

Who reads this: a contributor adding or reviewing a device-free MCP
tool-call differential case under `corpus/mcp/`. When to read it:
before adding a new `*.json` case file here, or before changing the
loader in `../../mcp_corpus.go` or the replay logic in
`../../mcp_differential_test.go`. What this does not cover: the sidecar's
own JSON-RPC corpus (`../sidecar/format.md`) and the schema-only
`TestMcpToolSchemaDifferential`, which needs no corpus file at all
because it diffs `tools/list` directly.

## Summary

An MCP case seeds one static screen on a scripted `MockDriver`, then
replays a sequence of `tools/call` requests against an in-process Go
server (`core/internal/mcp`, wired the same way `cmd/atomyx-mcp` wires
it). The differential test diffs the Go server's live responses
against a frozen golden fixture at `../../goldens/mcp/<name>.json`.

## Fields

Every case has:

- `name` (string, required): case identifier, matching the filename
  without the `.json` extension.
- `description` (string, required): one sentence stating what the
  case exercises.
- `driver` (object, optional): the `MockDriver` program every
  `select_device` step in this case resolves to: `hierarchy` (a
  single raw `TreeNode`, the same shape `corpus/driver/` and
  `corpus/runner/` use), `repeat` (how many times that one tree
  answers a `Hierarchy` read before mock.Driver would report "no tree
  staged"; omitted defaults to 200), `capabilities` (a partial
  override merged onto the mock's default capabilities), and `screen`
  (`{width, height}`). Omitted when no case step ever selects a
  device.
- `pinned` (object `{platform, id}`, optional): starts the server bound
  to that device, as `atomyx-mcp --platform <platform> --device <id>`
  does. A `select_device` step naming another device then reports
  `DEVICE_PINNED`.
- `steps` (array, required): an ordered list of `{tool, args}` calls,
  replayed sequentially, each response awaited before the next call is
  sent.
- `driver.apps` (array of strings, optional): the app ids the Go-side
  mock driver considers installed, so `launch_app` and `list_apps`
  resolve them as present. The recorded `list_apps` golden always
  answers empty regardless of this field, so a case that sets `apps`
  must not also call `list_apps` in the same case, or the Go side stops
  matching that golden.

Most cases stage exactly one screen, repeated. A case that needs a tap
to succeed and an appear-selector to already be satisfied puts both
facts on the SAME tree rather than staging a distinct "before" and
"after" screen: the point of a case is to reach a specific result
shape, not to reproduce a literal UI transition, and a repeated single
tree is immune to the Go server reading the hierarchy a different
number of times than the golden was recorded against.

A case whose result depends on something appearing that was not
present before, `tap_and_wait_transition`'s overlay-based timeout
classifications, instead sets `hierarchySequence` in place of
`hierarchy`/`repeat`: an ordered list of `{hierarchy, repeat}` stages,
each answering its own `repeat` count of `Hierarchy` reads (omitted
also defaults to 200) before the next stage takes over, with the last
stage sticky once its own queue is exhausted. Because the very first
`Hierarchy` read of any case is always the tool's own pre-tap read,
and every read past a sequence's last stage returns that same sticky
tree forever, a two-stage sequence (a small `repeat` on the first
stage, the default on the second) reaches a deterministic "before" and
"after" without depending on exactly how many times the Go server
reads the hierarchy: the first stage need only outlast every read up
to and including the tap's own resolution, and anything past that is
answered by the second, sticky stage regardless of exactly when the
switch happens.

## Argument placeholders

An arg value may be the literal string `"$lastRunId"` or
`"$lastBugId"`, substituted at replay time with the id the most
recent `start_run` or `report_bug` step in the SAME case produced.
This lets a case exercise `get_run`, `update_run_summary`,
`delete_run`, `get_bug`, and `delete_bug` without the corpus
hard-coding an id the Go server mints unpredictably at replay time.

## Normalization

`../../mcp_differential_test.go`'s `mcpNondeterministicFieldsByTool` maps
each tool to the wire fields, at any nesting depth, that get replaced
with a fixed placeholder before diffing: run and bug ids (they embed
`RunStore`'s own wall-clock timestamp), every `startedAt` /
`finishedAt` / `durationMs` / `timestamp` / `connectedAt` /
`waitedMs`, and `screenshot`'s `path` (it embeds the process's working
directory and a timestamp). Every other field, including
`add_case_study`'s `case-studies/YYYY-MM/<slug>` key, is compared
exactly, so a real slug or shape regression still surfaces.

## Out of scope

- `list_devices` is not covered by a tool-CALL case (though it is
  covered by `TestMcpToolSchemaDifferential`'s schema diff): the tool
  shells out to `adb` / `xcrun` directly rather than going through an
  injectable probe, so its result depends on whatever host toolchain
  happens to be installed, not on the `MockDriver` program.
- `tap_and_wait_transition`'s three paths are all covered:
  the success path (`tap-and-wait-transition-success.json`), the
  immediate-tap-failure path
  (`tap-and-wait-transition-immediate-failure.json`), and the timeout
  path's four classifications, one case each:
  `tap-and-wait-transition-timeout-still-loading.json`,
  `tap-and-wait-transition-timeout-partial-transition.json`,
  `tap-and-wait-transition-timeout-no-change.json`, and
  `tap-and-wait-transition-timeout-dialog-overlay.json` (the last
  using `hierarchySequence` per the `driver` field docs above, since
  `dialog_or_error_shown` requires an overlay that is new relative to
  the pre-tap read). `still_loading_with_cancel` and `overlay_unknown`
  are reachable through the same `analyzeOverlay` code path as
  `dialog_or_error_shown` (a different combination of inner loading
  role and inner label) and are exercised directly by
  `internal/orchestra`'s own unit tests rather than duplicated here.
- `run_script`'s own step semantics (branching, retries, assertions)
  are exhaustively covered at a lower layer by `corpus/script/` and
  `corpus/runner/`; `run-script.json` here only exercises the MCP
  tool's own argument parsing and result wiring.

## Adding a case

1. Save the case as `<name>.json` in this directory.
2. No registration step is needed: `LoadMcpCorpus` in
   `../../mcp_corpus.go` reads every `*.json` file here, sorted by
   filename.
3. If a new tool call produces a wall-clock-derived or randomly
   generated field, add it to `mcpNondeterministicFieldsByTool` in
   `../../mcp_differential_test.go` rather than hard-coding an expected
   value the Go server cannot reproduce byte for byte.
4. Run `go test ./internal/conformance/... -update` to write the Go
   server's response as the case's golden fixture, then review the
   generated file before committing it alongside the case.
