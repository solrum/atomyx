# adet — Known pitfalls

Traps the team has already hit once. Read before touching the corresponding
area. For the high-level map see [`architecture.md`](./architecture.md).

## Android control plane

- **Never cache `AccessibilityNodeInfo` references across tool calls.** They
  go stale. Capture `bounds: Rect` at dump time and use cached bounds for
  gestures; re-resolve via `findAccessibilityNodeInfosByViewId` for
  node-action fallbacks.
- **Never replace `serviceInfo` wholesale in `onServiceConnected`.** Augment
  it. Replacing clobbers XML-declared capabilities and causes
  `service.windows` to return empty.
- **Never dispatch gestures at coordinates behind the IME.**
  `GestureDispatcher.tap()` auto-dismisses the IME if the target intersects
  the keyboard bounds. Additionally, `tap_coordinates`-via-`tap({x,y})`
  rejects coords inside the IME window via the `coordInIme` geometric check.
- **Never call `typeViaKeyboard` without accounting for IME layout switches.**
  When focus moves from a numeric field to a text field, the keyboard
  re-renders; the handler polls via `waitForKeyboardReady` with a fast path.
  It also has a `typeViaOnScreenKeys` fallback for custom in-app keypads
  (Flutter banking apps).
- **`ResourceIdStrategy` has a walk fallback for Flutter / Compose / RN.**
  Android's `findAccessibilityNodeInfosByViewId` requires fully qualified
  `package:id/name` — Flutter exposes ids like `G01-05-01/2` without a
  prefix, so the strategy walks the tree and matches by suffix when the
  native lookup is empty.
- **`UiTreeService.dumpCompact` keeps elements with ANY stable signal**
  (resourceId / contentDesc / text / clickable). Do not revert this to
  "clickable or labeled only" — Flutter elements frequently have only a
  resourceId.
- **`clickable` flag is unreliable on Flutter / Compose / RN.** Those
  frameworks dispatch gestures in-engine via `GestureDetector` without
  setting the a11y clickable flag. The tool layer ignores the flag when
  deciding whether to tap; do not add a clickable filter.

## Tool layer (cross-platform)

- **Never import anything from `@synapse/*` or any parent-repo path.** adet
  is standalone.
- **Never branch on `ctx.controller.platform` inside tools.** If behavior
  must differ, it belongs in the adapter.
- **Never assume a device-side HTTP server exists** — that's Android-specific.
  iOS uses a host-side bridge. Design tools against `DeviceController`, not
  against the Android HTTP API.
- **Never add a tool that duplicates an existing tool's intent.** The
  consolidation from 40 → 19 fixed a measurable agent-confusion problem. If
  your new tool overlaps `input_text` / `tap` / `find_element`, extend the
  existing tool with a new param instead.
- **Do not render resourceId as a path-like short form** (e.g.
  `#G01-05-01/2`). Agents misparse the `/` as a path separator. Use explicit
  `resourceId="G01-05-01/2"` quoted form in any text output.
- **`get_ui_tree` is cached (2s)** and the handler blocks duplicate calls on
  an unchanged screen. Tools that need fresh tree data should use
  `ctx.invalidateUiCache()` first (`server.ts` dispatches this automatically
  for mutating tools).
- **Do not inline business logic in a tool handler** — extract to a strategy
  class in `src/tools/core/`. Example: when `TapTool` needed fuzzy resourceId
  matching, the logic went into `FuzzyResourceMatcher` (with unit tests) and
  was injected, not spliced into `execute()`.

## iOS (when you get there)

- **Never try to run an HTTP server inside an iOS app** — sandbox will kill
  it.
- **Never commit to a specific bridge approach** (Appium, WDA, idb, custom
  XCTest, …) without reading [`ios.md`](./ios.md), prototyping, and opening a
  discussion. The choice is open.
- **Always design iOS features to reuse the existing `DeviceController`
  interface.** If you find yourself adding iOS-specific methods, you're
  probably leaking platform details.
- **Rename `src/adapters/ios-xctest.adapter.ts`** once the real approach is
  chosen if XCTest isn't it. The current filename is a placeholder, not a
  commitment.
