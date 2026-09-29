# Changelog

All notable changes to Atomyx will be documented in this file.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Atomyx is in development. Notable changes are recorded under Unreleased.

## [Unreleased]

### Added
- `atomyx` CLI: a `driver` module (`run`, `suite`, `list-devices`,
  `doctor`) and a `skills` module (`init`, `update-skills`), plus
  top-level shortcuts `atomyx run`, `atomyx suite`, `atomyx devices`,
  `atomyx doctor`, `atomyx init`, and `atomyx update-skills`.
- `atomyx-mcp`: a stdio MCP server exposing tools for device and app
  lifecycle, UI inspection, interaction, run and bug reporting, case
  studies, and script execution.
- Suite discovery: `atomyx suite` runs a named suite declared in
  project config, or an ad-hoc selection of flows by tag or glob,
  with each script executing in its own clean context.
- Project configuration: a config file with environment profiles,
  loaded via `--config` and selected via `--env`, resolving platform,
  device, proxy, and variables ahead of a run.
- Script engine: an expression engine with typed variables (string,
  number, boolean), a `read` command that binds an element field to a
  variable, `assertExpr` for boolean expressions over variables, and
  `assertExists` for an occlusion-independent presence check.
- Script engine flow control: `handle` polls a set of conditions
  until one becomes visible, `branch` routes deterministically on a
  captured API response, `runFlow` invokes a sub-flow inline, and
  `requires` runs setup flows before a script's own steps.
- Script engine preconditions: a script declares a precondition, and
  a run applies a skip or force policy with per-flow deduplication.
- API capture: `capture` and `assertApi` against network traffic
  recorded through a proxy, with `extract` binding a captured value
  into a variable.
- A unified `pointer` step expressing arbitrary multi-finger touch
  sequences through a single `touches` list, with `longPress`,
  `pinch`, and `rotate` as sugar steps for the common cases, and a
  `capture` primitive inside a pointer sequence for a mid-gesture
  screenshot on both Android and iOS.
- A per-run report bundle in HTML and JSON, and a per-command help
  reference for the CLI.
- `run_script` flags the failed step and the top-level result with
  `deviceLost` when a run aborts because the device or agent went
  unreachable.

### Changed
- Breaking: the `pointer` step takes a single `touches` list. A
  script written against the previous multi-shape pointer form must
  migrate its steps to the `touches` form described in
  `docs/yml-script-reference.md`.
- Report screenshots are named by their content type.

### Fixed
- Typing waits for the target field to hold focus before sending
  text, closing a race that could land input in the wrong field.
- A reset launch on Android fails when the app cannot actually be
  force-stopped, instead of reporting a successful reset.
- Android dispatches a multi-pointer gesture as per-segment
  continued strokes, so gestures with independent per-finger timing,
  such as pinch and rotate, land correctly.
- `select_device` reports the kind of the device actually bound,
  taken from the driver, instead of echoing back the caller's kind
  argument.
