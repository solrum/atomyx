# @atomyx/studio — module (skeleton)

**Status**: not yet implemented. Placeholder for the GUI client
module per `.claude/docs/architecture.md` §6.

## Persona

**Power User** who wants a visual interface for everything:
live screen preview, click-to-inspect element selection,
visual test recording, replay debugger, report dashboards.
Installs `@atomyx/studio` which auto-discovers whichever
sibling modules (`@atomyx/driver`, `@atomyx/test-mgmt`,
`@atomyx/cloud`) are present in the runtime.

## Planned packages

- `packages/studio/` — `@atomyx/studio` — module main:
  application shell, feature discovery, settings.
- `packages/core/` — `@atomyx/studio-core` — UI logic that's
  framework-agnostic (testable without rendering).
- `packages/ui/` — `@atomyx/studio-ui` — React (or chosen
  framework) component library: tree view, screenshot canvas,
  selector picker, action history.
- `packages/desktop/` — `@atomyx/studio-desktop` — Electron /
  Tauri shell binary.

## Composition pattern

Studio imports sibling modules via npm package boundaries
(`import { Orchestra } from "@atomyx/driver"`,
`import { IosDriver } from "@atomyx/ios-driver"`). At runtime,
missing siblings are detected via dynamic-import try/catch and
the corresponding UI sections are disabled gracefully. No HTTP
between Studio and other modules — they run in-process.
