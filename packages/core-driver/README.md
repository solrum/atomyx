# @atomyx/core-driver

Framework primitives for the Atomyx core-driver module. This package
is the cross-platform brain — it knows how to find elements, resolve
selectors with priority broadening, scroll elements into view, detect
visual obscurement, and compose everything through the `Orchestra`
command layer. It knows nothing about iOS or Android specifically;
both are plugged in via the `Driver` port.

## What's inside

- **`Driver`** (`src/driver/driver.port.ts`) — the primitive device
  interface every platform implements. Thin surface: `hierarchy`,
  `tap`, `swipe`, `inputText`, `launchApp`, `screenshot`, and a
  handful more. Selector, scroll, and obscurement concerns are
  explicitly NOT on this interface — they live host-side in the
  layers above.
- **`TreeNode`** (`src/tree/tree-node.ts`) — canonical cross-platform
  UI element shape. Attribute bag with standardized keys (`id`,
  `text`, `label`, `hint`, `value`, `role`, `class`, `package`,
  `bounds`). Drivers normalize native fields into this shape at the
  wire boundary.
- **Filter composition** (`src/filters/element-filter.ts`) — pure
  function primitives (`textMatches`, `roleIs`, `isEnabled`, `below`,
  `hasDescendant`, ...) composed via `intersect` / `union` / `not`.
  Replaces the old strategy-class selector pipeline with a
  functional model.
- **Priority broadening** (`src/selectors/priority-broadening.ts`) —
  `compileSelector(selector)` turns a typed `Selector` into an
  `ElementFilter` that tries content fields in priority order:
  `id > label > text > value > hint`.
- **`Finder`** (`src/finder/finder.ts`) — `find` / `findOne` /
  `waitFor` / `waitForOne` with clock-driven polling. Typed
  `FindTimeoutError`.
- **`ScrollController`** (`src/scroll/scroll-controller.ts`) —
  cross-platform `ensureVisible` with Phase 0 scroll-search
  (virtualized list recovery) + Phase 1 positional scroll with
  safe-area insets.
- **`detectObscurement`** (`src/obscurement/obscurement.ts`) —
  z-order walk on the canonical `TreeNode` with ancestor
  disambiguation + generic container suppression.
- **`Orchestra`** (`src/orchestra/orchestra.ts`) — the high-level
  command layer. Composes Driver + Finder + ScrollController +
  obscurement into tap/longPress/inputText/swipe/... methods.
  Returns `ActionResult` instead of throwing.
- **Infra** (`src/infra/`) — `Clock` (with `SystemClock` +
  `FakeClock`) and `Logger` (with `ConsoleLogger` + `NoopLogger`)
  ports for deterministic testing.
- **Testing kit** (`src/testing/`) — `MockDriver` (in-memory
  scripted Driver implementation) + fixture builders for use in
  unit tests of anything that consumes this package. Imported via
  `@atomyx/core-driver/testing`.

## Usage

```ts
import {
  Orchestra,
  SystemClock,
  ConsoleLogger,
  type Driver,
} from "@atomyx/core-driver";

// Any Driver — iOS, Android, or a MockDriver in tests.
const driver: Driver = ...;

const orchestra = new Orchestra({
  driver,
  clock: new SystemClock(),
  logger: new ConsoleLogger("info"),
});

await driver.connect();
await orchestra.launchApp("com.example.app");
const result = await orchestra.tap({ text: "Login", role: "button" });
```

## Dependencies

Zero runtime dependencies. Pure TypeScript on Node 20+.

## See also

- [`@atomyx/core-driver-ios`](../core-driver-ios) — iOS driver
- [`@atomyx/core-driver-android`](../core-driver-android) — Android driver
- [`@atomyx/core-driver-mcp`](../core-driver-mcp) — MCP server factory
- [`@atomyx/core-driver-cli`](../core-driver-cli) — CLI binary
- [`.claude/docs/architecture.md`](../../.claude/docs/architecture.md) — opt-in modular ecosystem contract
- [`.claude/docs/tools.md`](../../.claude/docs/tools.md) — tool implementation reference
