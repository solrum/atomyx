# `pointer` command

End-user guide to the YAML `pointer` command, which is Atomyx's
general-purpose gesture primitive. Read this when:

- You need a gesture the narrower commands (`tap`, `swipe`,
  `pressKey`) don't express, for example press-and-drag, multi-finger,
  pressure.
- A `tap`/`swipe` script works on Android but is too imprecise on
  iOS, or vice versa, and you want explicit timing control.
- You're authoring a YAML test that should run on both
  the public and private iOS synthesizer paths.

This doc does NOT cover:

- The full YAML schema / parser rules: see
  [`yml-script-reference.md`](./yml-script-reference.md) §pointer.
- The Swift synthesizer protocol or runner internals: see
  [`../.claude/docs/ios.md`](../.claude/docs/ios.md) §"Gesture
  dispatch architecture".

## TL;DR

```yaml
- pointer:
    touches:
      - actions:
          - down: "Submit"      # touch down on the Submit element
          - wait: 700           # hold 700 ms
          - move: { x: 200, y: 400 }
          - up                  # release
```

The `pointer` command takes a `touches` list. Each entry is one
pointer path: a sequence of `down` / `move` / `wait` / `up` actions
modelled directly on the
[W3C WebDriver Actions](https://www.w3.org/TR/webdriver/#actions)
pointer input source. A single entry is a single-pointer gesture;
more than one entry is a multi-pointer gesture whose paths run in
parallel.

## When to reach for `pointer`

| You want to… | Use |
|---|---|
| Tap a button by id / text / coords | `tap` (simpler) |
| Swipe a screen up/down/left/right | `swipe` (simpler) |
| Press a system key (back, enter, …) | `pressKey` |
| Long-press an element ≥X ms | `pointer` |
| Drag from element A to element B | `pointer` |
| Press-and-drag (reorder a list row) | `pointer` |
| Flick a list with explicit timing | `pointer` (or `swipe`) |
| Pinch / rotate / multi-finger | `pointer` (multi-entry `touches`) |
| 3D-touch / pressure input | `pointer` with `pressure` |

If `tap` or `swipe` covers the need, prefer them, because they are
shorter and stay on the cheaper public synthesizer path on iOS.

## The five canonical patterns

Each pattern is a fixed shape of `down` / `move` / `wait` / `up`.
The host-side validator rejects out-of-shape sequences (open
without `down`, close without `up`, multiple `down`s without
intermediate `up`, etc.) at parse time so the runner never
dispatches an invalid script.

### Tap

`down` and `up` at the same point, no hold.

```yaml
- pointer:
    touches:
      - actions:
          - down: "Submit"
          - up
```

### Long-press

`down`, `wait`, `up` at the same point. The hold duration is
the `wait` value in milliseconds.

```yaml
- pointer:
    touches:
      - actions:
          - down: "Item A"
          - wait: 800
          - up
```

### Drag

`down` at the start, `move` to the end, `up`. Public path
maps this to `XCUICoordinate.press(forDuration:thenDragTo:)`
on iOS with a 50 ms minimum press to avoid the runtime
reinterpreting the gesture as a tap.

```yaml
- pointer:
    touches:
      - actions:
          - down: { x: 100, y: 500 }
          - move: { x: 300, y: 500 }
          - up
```

### Press-and-drag (reorder)

A `wait` between `down` and `move` gives the target widget
time to enter its drag-handle state, which is required for
`ReorderableListView`, drag-and-drop, and similar widgets
that distinguish a tap from a drag by a hold threshold.

```yaml
- pointer:
    touches:
      - actions:
          - down: "Alpha"        # row 1 in a reorder list
          - wait: 600            # enter reorder mode
          - move: { x: 220, y: 487 }   # below row 2
          - up
```

### Flick

Short `down` then `move` with a small time delta makes the
runtime treat the gesture as a flick (high velocity), which
in turn triggers inertia scrolling on most scrollable widgets.

```yaml
- pointer:
    touches:
      - actions:
          - down: { x: 220, y: 800 }
          - move: { x: 220, y: 200 }   # 600 px in ~50 ms
          - up
```

> The public path can't pass an explicit velocity to the
> XCUITest API. Practically: each flick scrolls roughly one
> container height. Lists with hundreds of rows need several
> flicks.

## Targets: selectors vs coordinates

`down` and `move` accept either:

- **A selector string or object**: `"Submit"`,
  `{ id: "submit-btn" }`, `{ text: "Sign in", role: "button" }`.
  Resolved at the moment the action runs, so the gesture can
  follow an element that animates into view mid-drag.
- **A coordinate**: `{ x: 200, y: 400 }`. Always absolute
  (app-frame origin, points not pixels). Use coordinates when
  no stable selector exists, or when you want pixel-precise
  control (drawing, drag offsets, gestures inside a canvas).

A single sequence can mix the two. For example, "press a
button, then drag to a fixed coordinate":

```yaml
- pointer:
    touches:
      - actions:
          - down: "Item A"
          - wait: 600
          - move: { x: 220, y: 800 }
          - up
```

## Multi-pointer: pinch, rotate, custom

For any gesture with more than one finger, give `touches` more than
one entry. Each entry is one pointer with a unique `id` and its own
action sequence, and the runner aligns them on a shared wall clock
anchored at the first `down` across all pointers.

```yaml
- pointer:
    touches:
      - id: f1
        actions:
          - down: { x: 200, y: 400 }
          - move: { x: 100, y: 400 }   # pinch in
          - up
      - id: f2
        actions:
          - down: { x: 200, y: 600 }
          - move: { x: 300, y: 600 }   # pinch in
          - up
    moveDurationMs: 250
```

`moveDurationMs` controls how long each `move` stretches
across all pointers. Without it the runner picks a default
sized to the largest path delta.

### Capability gates

Multi-pointer support is a runtime-resolved capability. Atomyx
probes the active iOS / Xcode combination at startup and reports
back via `driver.capabilities`. The validator checks before
dispatch:

| Capability | iOS | Android |
|---|---|---|
| `canMultiPointer` | resolved at runtime and supported on Xcode 16+ sim and recent physical devices | supported through simultaneous multi-pointer dispatch |
| `canPressure` | resolved at runtime and needs a device with 3D Touch / Force Touch | not on a touchscreen |

A script whose `touches` list has more than one entry, run on a
driver without `canMultiPointer`, fails with
`POINTER_MULTI_NOT_SUPPORTED`. No
silent downgrade. Pinch / rotate / pressure either work or
surface an actionable error naming the platform.

## Gesture sugar: `longPress`, `pinch`, `rotate`

Three high-level commands compile to the same pointer gesture path
as `pointer`, so you write the intent directly instead of hand-
building an action sequence. Their location fields (`at`, `center`,
`thenSwipeTo`) take the same target as a pointer action: an
absolute `{ x, y }` coordinate or a selector.

`longPress` presses and holds, then releases. With `thenSwipeTo`
it drags there first (a hold-then-drag that holds correctly on
every platform). `holdMs` defaults to 500.

```yaml
- longPress:
    at: "Item A"
    holdMs: 600
    thenSwipeTo: "Drop zone"
```

`pinch` moves two contacts from `startGap` apart to `endGap`
apart, symmetric about `center` along `axis` (`vertical` default,
or `horizontal`). An `endGap` greater than `startGap` is a
zoom-in. `durationMs` defaults to 200.

```yaml
- pinch:
    center: { id: photo }
    startGap: 80
    endGap: 320
```

`rotate` sweeps two contacts `degrees` around `center`, placed
`radius` apart on opposite sides, each tracing an arc. `radius`
defaults to 100 points and `durationMs` to 200.

```yaml
- rotate:
    center: { id: dial }
    degrees: 90
```

A selector `center` is resolved to the element's center
coordinate at dispatch time. `pinch` and `rotate` are two-finger
gestures, so they require `canMultiPointer` and fail with
`POINTER_MULTI_NOT_SUPPORTED` otherwise. These three steps are
additive: the `tap` and `swipe` commands are unchanged.

## Pressure: 3D Touch / Force Touch

Add `pressure: 0.0..1.0` to any `down` or `move`:

```yaml
- pointer:
    touches:
      - actions:
          - down: { x: 200, y: 400, pressure: 0.3 }
          - move: { x: 200, y: 400, pressure: 0.9 }
          - up
```

Pressure requires `canPressure=true` on the active driver.
Scripts setting pressure on a driver without the capability fail
with `POINTER_PRESSURE_NOT_SUPPORTED`.

## Pattern coverage

All five W3C single-pointer patterns plus pinch / rotate /
multi-finger custom and pressure-bearing dispatches are
supported when the runtime exposes the multi-pointer
capability. The narrow fallback case (older Xcode without the
multi-pointer runtime) restricts dispatch to the five
single-pointer patterns and rejects multi / pressure with an
actionable error.

| Pattern | Multi-pointer runtime | Single-pointer fallback |
|---|---|---|
| Tap, long-press, drag, press-and-drag, flick | ✓ | ✓ |
| Pinch, rotate, multi-finger custom | ✓ | ✗ (POINTER_MULTI_NOT_SUPPORTED) |
| Pressure (3D / Force Touch) | ✓ | ✗ (POINTER_PRESSURE_NOT_SUPPORTED) |

## Smoke verification: Flutter test app

The example Flutter fixture under `examples/atomyx-demo` exposes a
Gestures screen where every covered pattern has a visible
counter / status indicator (tap count, long-press count, drag
status + offset, reorder order, pinch scale / events, pressure
peak / events). Exercise each pattern against this fixture
after any change to the Swift synthesizer or the host-side
compiler and assert the indicator moved as expected. The
driver must be running (`make serve` under
`platforms/ios-agent/`) with the simulator booted and the app
on the Gestures card.

Expected output (iPhone 16 Pro Max sim, Xcode 16.2,
event-record backend):

```
== summary ==
[flutter-pointer-smoke] PASS  tap
[flutter-pointer-smoke] PASS  long-press
[flutter-pointer-smoke] PASS  drag
[flutter-pointer-smoke] PASS  reorder
[flutter-pointer-smoke] PASS  pinch
[flutter-pointer-smoke] PASS  pressure

== dispatch latency ==
pattern         calls      min      avg      max
tap                 1    213ms    213ms    213ms
long-press          1    918ms    918ms    918ms
drag                1    614ms    614ms    614ms
reorder             1   1501ms   1501ms   1501ms
pinch               1    799ms    799ms    799ms
pressure            1    333ms    333ms    333ms
```

Latency budget for CI planning: 6 patterns ≈ 4.4 s of pure
dispatch + ~70 s of orient / settle / scroll waits. End-to-
end smoke runtime ~75 s on a warm simulator.

The smoke ships unit-test coverage for the pointer step in
`core/internal/runner/pointer_command_test.go`, which runs under
`go test` without a device.

## Common pitfalls

**Selector targets that scroll out of view.** A `down: "Item A"`
fails if Item A is offscreen. Add a preceding `swipe` or scroll
gesture to bring it into view, or capture the row's coordinates
from `get_ui_tree` and pass them as `{ x, y }` instead.

**Drag too short.** Public-path drag enforces a 50 ms minimum
press before the move. Below that XCUITest reinterprets the
gesture as a tap and the drag silently doesn't happen. The
host compiler raises the press transparently.

**Press-and-drag without enough hold.** `ReorderableListView`,
draggable canvases, and most drag-and-drop widgets ignore a
move that starts before they enter drag-handle state. 600 ms
is a safe hold, but some widgets need 800 ms.

**Inner widgets eat page-level scroll.** When a card has its
own `GestureDetector` (drag, long-press), a swipe on top of
it triggers the card's pan handler instead of scrolling the
outer page. Workarounds: swipe in the gutter outside the card
bounds, or use the platform's accessibility scroll-to.

**Multi-pointer rejected by the validator.** Surfaced as
`POINTER_MULTI_NOT_SUPPORTED` before dispatch when the active
runtime doesn't expose the multi-pointer surface. On iOS this
typically means the XCSynthesizedEventRecord symbols are
missing, which is usually resolved by upgrading Xcode to a version
known to ship them.

## Error codes

| Code | Cause |
|---|---|
| `POINTER_EMPTY_SEQUENCE` | The `touches` list is empty, or a touch's action sequence has no actions |
| `POINTER_FORM_CONFLICT` | Duplicate pointer ids across the touches in a multi-pointer gesture |
| `POINTER_NO_OPENING_DOWN` | Sequence does not start with `down` |
| `POINTER_NO_CLOSING_UP` | Sequence does not end with `up` |
| `POINTER_NESTED_DOWN` | Two `down`s without an intermediate `up` |
| `POINTER_INVALID_WAIT` | `wait` value is not a positive number |
| `POINTER_INVALID_MOVE_DURATION` | `moveDurationMs` is not a non-negative number |
| `POINTER_MULTI_NOT_SUPPORTED` | Multi-pointer on a driver without `canMultiPointer` |
| `POINTER_PRESSURE_NOT_SUPPORTED` | Pressure on a driver without `canPressure` |
| `POINTER_PATTERN_NOT_EXPRESSIBLE` | The active backend cannot compile the requested shape (e.g. multi-waypoint paths on the coordinate fallback) |
| `POINTER_SELECTOR_RESOLUTION_FAILED` | A selector target in `down` / `move` did not resolve to an element at dispatch time |

## Verification status: iOS

Five of the six W3C single-pointer patterns plus pinch and
pressure are verified end-to-end on the iPhone 16 Pro Max
simulator running Xcode 16.2 / iOS 18.3 against the example
Flutter fixture. The pointer command surface is feature-
complete on iOS. The items below are limitations or
unverified scenarios, not missing implementation.

### Verified

- iPhone 16 Pro Max simulator, Xcode 16.2, iOS 18.3
- Event-record backend (multi-pointer + pressure capable)
- 6 patterns: tap, long-press, drag, press-and-drag,
  pinch, pressure
- Latency band: 213 ms (tap) to 1501 ms (reorder)

### Not yet verified

- **Physical iOS device**: every measurement above is from
  the simulator. Force Touch / 3D Touch hardware on a real
  device may surface non-zero `pressure` values that the sim
  reports as zero.
- **Other iOS / Xcode combinations**: the event-record
  backend probes for `XCSynthesizedEventRecord`,
  `XCPointerEventPath`, and `XCTRunnerDaemonSession` at
  startup. Apple has changed selector signatures between
  Xcode major versions before, so expect 0 to 3 days of selector
  fixes per major Xcode bump.

### Known limitations

- **Flick on nested vertical scrollables.** The example
  fixture used to include a 50-row inner ListView for flick-
  to-bottom testing. Flutter's nested-Scrollable gesture
  arbitration intermittently routed synthesized swipes
  intended for the inner list to the outer page, scrolling
  past the next test's fixture instead of advancing inner
  items. The fixture was removed, but drag semantics are still
  covered by the Drag card. If your test app has nested
  vertical scrollables, prefer driving them through the
  higher-level Atomyx orchestra layer (which handles
  scroll-into-view) rather than raw `dispatchGesture`.
- **Pressure on the iOS simulator reports zero.** The sim
  has no Force Touch sensor, so Flutter's
  `PointerEvent.pressure` resolves to 0 even when the
  synthesized event carries `pressure: 0.9`. The dispatch
  wire is verified (the pressure event fires the listener);
  the value verification awaits physical-device testing.
- **Reorder timing tuned for `ReorderableListView`.** The
  smoke uses 700 ms hold + 6 stepped move waypoints over
  500 ms, calibrated for Flutter's pan recognizer. SwiftUI
  reorder controls or non-Flutter scrollables may need
  different hold / interpolation parameters.
- **Pressure validation is host-side AND backend-side.**
  `[0.0, 1.0]` range checks happen in both the TypeScript
  validator (`POINTER_PRESSURE_NOT_SUPPORTED` /
  invalid-range error) and the Swift dispatcher. Direct
  HTTP POSTs to the runner that bypass the host are still
  rejected at the backend.

### CI coverage (pending)

A CI matrix + weekly cron against the latest Xcode beta is
planned to catch private-symbol drift before users. Until a CI
workflow is committed, run the Flutter pointer smoke manually
(local script: see `platforms/ios-agent/Makefile` targets)
after any change to the Swift bridge or before each release.

## References

- [`yml-script-reference.md`](./yml-script-reference.md) §pointer:
  schema and parser rules.
- [`../.claude/docs/ios.md`](../.claude/docs/ios.md) §"Gesture
  dispatch architecture": Swift synthesizer protocol.
