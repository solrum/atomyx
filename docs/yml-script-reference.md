# Atomyx YML Script Reference

> Format: `atomyx/v1`
>
> Write test scripts in YAML, run them on real devices, with no code required.

## Quick Start

```yaml
format: atomyx/v1
appId: com.example.app
name: Login test
---
- launchApp
- tap: "Sign in"
- type:
    into: "Email"
    text: user@test.com
- tap: "Login"
- waitFor: "Dashboard"
- screenshot: done
```

Run it:

```bash
atomyx run --file login.yml --platform ios --device <UDID>
```

---

## Script Structure

A script has two parts separated by `---`:

```yaml
# ── Config (above ---) ────────────────────────
format: atomyx/v1
appId: com.example.app
name: My test
# ... other config

---

# ── Steps (below ---) ────────────────────────
- launchApp
- tap: "Button"
- screenshot: result
```

### Config Fields

| Field | Required | Description |
|---|---|---|
| `format` | No | Script format. Default: `atomyx/v1` |
| `appId` | Yes | Bundle id (iOS) or package name (Android) |
| `name` | Yes | Test name for reporting |
| `description` | No | What this test verifies |
| `precondition` | No | Conditions that must be true before running |
| `tags` | No | List of tags for filtering |
| `env` | No | Variables available as `${name}` in steps |
| `proxy` | No | `required` or `optional` (default). `required` refuses the run before any step executes when no network capture adapter is configured. See Proxy Setup |
| `requires` | No | Flow files to run before this script |
| `preconditions` | No | Setup gates checked, and established when unmet, before steps run. See Preconditions |
| `stepDelay` | No | Delay in ms inserted between each step. Default: 500 |

### Example with all config fields

```yaml
format: atomyx/v1
appId: com.example.app
name: Purchase flow
description: Verify end-to-end purchase with payment
precondition: User account exists in staging
tags:
  - smoke
  - payment
  - P0
proxy: required
requires:
  - flows/login.yml
  - flows/add-to-cart.yml
env:
  email: user@test.com
  card: "4111111111111111"
---
- tap: "Checkout"
- waitFor: "Payment"
```

---

## Preconditions

`preconditions` declares run-once setup gates the runner checks
before a script's steps run. Each entry pairs a `check` with a
`setup` that runs when the check does not currently hold.

| Field | Required | Description |
|---|---|---|
| `check` | Yes | `visible` or `notVisible` condition, exactly one of the two |
| `setup` | Yes | Inline step list, or a string path to a flow fragment file |
| `policy` | No | `ensure` (default), `always`, or `skip` |

`policy` controls when `setup` runs:
- `ensure`: run `setup` only when `check` does not currently hold,
  then confirm `check` holds afterward
- `always`: run `setup` every time, then confirm `check` holds
  afterward
- `skip`: this precondition is not evaluated or enforced at all

```yaml
preconditions:
  - check:
      visible: "Home"
    setup: flows/login.yml
  - check:
      notVisible: "Onboarding banner"
    setup:
      - tap: "Dismiss"
    policy: always
---
- tap: "Checkout"
```

---

## Variables

Define in `env`, use with `${name}`:

```yaml
env:
  email: user@test.com
  password: secret123
---
- type:
    into: "Email"
    text: ${email}
- type:
    into: "Password"
    text: ${password}
```

Variables can also come from:
- CLI: `--env email=other@test.com` (overrides script env)
- `extract` command (runtime, from API responses)
- Parent script (when used as sub-flow)

The `read` command binds typed variables (string or boolean) into this
same set, and `assertExpr` reads them by bare name in expressions. The
`${name}` form above stays textual interpolation, applied once when
the script is parsed.

---

## Expression Syntax

The expression language used by `assertExpr` evaluates a boolean
expression against the script's runtime variables.

Values are string, number, or boolean. A variable keeps the type its
producer gave it. The `read` command binds an element's real type, so
the `value` field and other attribute fields are strings, and state
fields are booleans.

Variables are referenced by bare name and resolved when the step
runs. This differs from `${name}`, which is textual interpolation
applied once when the script is parsed.

Operators:
- Arithmetic: `+`, `-`, `*`, `/`, `%` on numbers
- Comparison: `<`, `<=`, `>`, `>=` on numbers
- Equality: `==`, `!=` between any two values
- Logical: `and`, `or`, `not` on booleans

Typing is strict, with no implicit coercion. Arithmetic requires
numbers, logical operators require booleans, and a type mismatch is a
clear error. Equality between two values of different types is
simply false rather than an error. There is no truthiness: a number
or a string is never treated as a boolean.

Casts are the only way to change a type: `number(x)`, `string(x)`,
`bool(x)`. `number` accepts a numeric string, `string` accepts any
value, and `bool` accepts the strings `"true"` or `"false"`. A cast
that cannot apply is a clear error, for example converting a boolean
to a number.

Precedence, loosest to tightest binding: `or`, then `and`, then
`not`, then equality (`==`, `!=`), then comparison (`<`, `<=`, `>`,
`>=`), then addition and subtraction, then multiplication, division,
and modulo. Use parentheses to group.

---

## Commands

### launchApp

Launch the app specified in `appId`.

```yaml
- launchApp
```

Three optional fields change how the app starts. A field a step leaves out
falls back to the project launch policy, so a bare `launchApp` behaves as
the policy says.

```yaml
- launchApp:
    clearState: true
- launchApp:
    stopApp: false
- launchApp:
    arguments:
      route: /focus
      retries: 3
      debug: true
```

| Field | Type | Effect |
| --- | --- | --- |
| `clearState` | boolean | `true` clears the app's data and then launches it fresh. `false` skips a data clear that the launch policy asks for. |
| `stopApp` | boolean | `true` force-stops a running instance, then launches again. `false` reuses a running instance. |
| `arguments` | mapping | Named values handed to the app at launch. Each value is a string, number or boolean and reaches the app as text. |

How `arguments` reach the app:

- Android: each entry is a string extra on the launch intent, so the app reads it from its launching intent.
- iOS: each entry `name: value` becomes the launch environment entry `name=value` and the launch argument pair `-name value`. The app reads it from its environment or from its standard user defaults.

Rules:

- `clearState: true` together with `stopApp: false` is rejected, because clearing data stops the app.
- `arguments` together with `stopApp: false` is rejected, because a reused instance never receives them.
- An arguments name must not be empty and must not contain `=`.
- Arguments take effect only when the launch starts the app. A project launch policy that attaches to a running instance ignores them.
- A launch that carries `arguments` always runs, even when it repeats an earlier launch of the same app.
- A `launchApp` that would only repeat an earlier launch of the same app in the flow is skipped. A launch with `stopApp: true`, `clearState: true` or `arguments` always runs.
- A `launchApp` after a `stopApp`, `killApp` or `clearState` of the same app runs again.
- On iOS, `clearState` reinstalls the app and needs the app's `artifact` in the project config. The step fails with the `INVALID_ARGUMENT` code when none is configured.

### stopApp

Stop an app. Without an app id, the step acts on the script's `appId`.

```yaml
- stopApp
- stopApp: com.example.other
- stopApp:
    appId: com.example.other
```

### killApp

Terminate an app. The argument forms are the same as `stopApp`. Every
platform adapter runs the same device operation for `killApp` as for
`stopApp`, so the two steps behave identically. Use whichever reads
better in the script.

```yaml
- killApp
- killApp: com.example.other
```

### clearState

Reset an app's data to a clean state. The argument forms are the same as
`stopApp`. The app stays installed on Android. On iOS the app is
reinstalled from the app's `artifact` in the project config, so that field
is required there. Without it the step fails with the `INVALID_ARGUMENT`
code and a message telling you to set the `artifact` field.

```yaml
- clearState
- clearState: com.example.other
```

### openLink

Open a URL, deep link or universal link in the device's handler for it. The
step succeeds once the device accepts the link, not once the destination
screen has finished appearing, so follow it with `waitFor` or an assertion on
an element of that screen.

```yaml
- openLink: myapp://orders/42
- openLink: https://example.com/orders/42?tab=all&sort=new
- openLink:
    link: myapp://orders/42
    appId: com.example.app
```

| Field | Type | Meaning |
| --- | --- | --- |
| `link` | string | Required. The link to open. It must start with a scheme such as `https:` or `myapp:`, and it can use `${var}` placeholders. |
| `appId` | string | Optional. The app that must open the link, as an Android package name or an iOS bundle id. Without it the device chooses the handler. |

Rules and behavior:

- A link without a scheme, an empty link, or a link with a control character is rejected when the script is parsed. A link that still holds a `${var}` placeholder is checked again when the step runs.
- Naming `appId` also avoids the confirmation prompt that a custom scheme can raise when the system picks the handler.
- A device that cannot open links fails the step with the `LINK_UNSUPPORTED` code. An iOS device older than iOS 16.4 is one of them.
- A link that no installed app handles fails the step with the `LINK_NOT_OPENED` code.
- Opening a link in an `appId` counts as launching that app in the flow, so a later `launchApp` of the same app is skipped as a repeat unless it is a fresh launch.

### tap

Tap an element on screen.

```yaml
# By text (shorthand)
- tap: "Login"

# By id
- tap:
    id: "btn_login"

# By accessibility label
- tap:
    label: "Close dialog"

# By hint/placeholder
- tap:
    hint: "Search"

# With role constraint
- tap:
    text: "Submit"
    role: button

# Nth match (0-indexed)
- tap:
    text: "Item"
    nth: 2
```

A bare string shorthand sets both `text` and `label` to that value,
so it matches an element by either its visible text or its
accessibility label. Setting `text` explicitly without `label`
mirrors the same value into `label`. A `text` value is also tried
against the element's `hint` when no element matches by `label` or
`text`, so `text: "Email"` finds an input field whose name is exposed
as a hint on one platform and as a label on another. A selector that
sets `hint` itself gets no such fallback. Every selector field
(`text`, `id`, `label`, `hint`, `role`, `nth`) is optional, and every
command that takes a selector (`tap`, `waitFor`, `assertVisible`,
`assertNotVisible`, `assertExists`, `read`, and pointer targets)
accepts the same shorthand and field set.

A string selector field matches by exact string equality: the
element's entire attribute value must equal the field's value exactly,
with matching case and no trimming of surrounding or embedded
whitespace. A string that contains regex characters, such as
`Price (USD)`, stays a literal.

The `text`, `id`, `label` and `hint` fields also accept a
`{regex, flags}` object, the same shape the MCP selector takes, for a
partial, anchored or case-insensitive match:

```yaml
- tap:
    text:
      regex: "^Item [0-9]+$"
- assertVisible:
    id: { regex: "^row_", flags: "i" }
```

- `regex` is a JavaScript-syntax expression, required.
- `flags` is optional and accepts `i` (case-insensitive), `m`
  (multiline anchors) and `s` (dot matches newline). `g`, `y`, `u` and
  `d` are accepted and have no effect.
- A regex match is an unanchored search, so `Item` also matches
  "Item 42 extra". Write `^` and `$` to require a whole-value match.
- Lookahead, lookbehind and backreferences are not supported.
- An invalid expression, an unknown flag, or an unknown key in the
  object is rejected when the script is parsed.
- A regex `text` is mirrored onto `label` just like a string `text`.
- A regex field is never split into newline segments.

Segment rule: Flutter Semantics often merges a visible title and a
subtitle into one label, so an element showing "Products" with a
subtitle below it can carry the combined label
"Products\nBrowse our catalog", newline included. When an attribute
value (`label`, `hint`, `text`, or `value`) contains
newline-separated segments, a selector value that equals exactly one
whole segment also matches it. `text: "Products"` and
`label: "Products"` therefore find that element.

- Segments use the same exact equality as a whole value, with no case
  folding and no trimming.
- A partial segment never matches: `"Prod"` does not match.
- An element whose whole value equals the selector always wins over
  an element matched only by a segment.
- An `id` selector that matches no element id also tries the label
  segments of every element, after every other content field has
  failed, to reach an identifier the platform folded into its
  parent's label.

### type

Type text into an input field.

```yaml
# Into focused field
- type: "Hello world"

# Into specific field (by text)
- type:
    into: "Email"
    text: user@test.com

# Into specific field (by id)
- type:
    into:
      id: "input_email"
    text: user@test.com

# Into specific field (by hint)
- type:
    into:
      hint: "Enter your email"
    text: user@test.com
```

A type step reports only how many characters it entered, for example
`Typed 13 characters`, and its progress summary shows the same fixed
mask whatever the text. The typed text never appears in the step
result, the run log, the failure bundle or the report, so a value that
is a secret stays out of them. An `extract` step reports each variable
name with the length of its value, and a failed `assertApi` or
`assertExpr` describes a string it compared by length, for the same
reason.

### inputRandom

Type a generated value into the focused field. The kind is required.

```yaml
- inputRandom: text
- inputRandom: email
- inputRandom:
    kind: number
    length: 6
```

| Kind | Value | `length` |
| --- | --- | --- |
| `text` | Lowercase letters | Number of letters. Default 8. |
| `number` | Decimal digits | Number of digits. Default 8. |
| `email` | Lowercase letters, then `@example.com` | Number of letters before the `@`. Default 8. |
| `personName` | A first and a last name from a fixed list | Not accepted. |

`length` is between 1 and 256. Every value comes from the run's random
source. A host that needs repeatable values seeds it, so two runs with
the same seed type the same text. The step result reports the kind and
the number of characters, not the value itself.

### hideKeyboard

Dismiss the on-screen keyboard. The step passes when no keyboard is
showing, and fails when the device cannot hide a keyboard.

```yaml
- hideKeyboard
```

### eraseText

Erase characters backwards from the focused field.

```yaml
- eraseText
- eraseText: 5
- eraseText:
    charactersToErase: 5
```

Without a count the step erases 50 characters. A count must be at least 1.
Erasing more characters than the field holds is not an error. The step
fails when the device cannot erase text directly, and then
`pressKey: delete` is the alternative. A `type` step already clears its
field first, so use `eraseText` to edit a field without retyping it.

### waitFor

Wait for an element to appear. Throws on timeout.

```yaml
# Default 5s timeout
- waitFor: "Dashboard"

# Custom timeout
- waitFor:
    text: "Loading complete"
    timeout: 10000
```

### assertVisible

Assert an element is visible on screen.

```yaml
# Instant check
- assertVisible: "Welcome"

# With polling timeout
- assertVisible:
    text: "Success"
    timeout: 5000

# By id
- assertVisible:
    id: "success_icon"
```

### assertNotVisible

Assert an element is NOT visible.

```yaml
# Instant check
- assertNotVisible: "Error"

# Wait for element to disappear
- assertNotVisible:
    text: "Loading"
    timeout: 5000
```

### assertExists

Assert a node matching the selector is present in the UI hierarchy.
Unlike `assertVisible`, presence does not require the element to
currently be visible on screen: an element scrolled out of the
viewport or covered by another view still counts as existing.

```yaml
# Instant check
- assertExists: "Row 12"

# With polling timeout
- assertExists:
    id: "row_12"
    timeout: 5000
```

### read

Read one field of an element into a variable, keeping the field's real type.

```yaml
# Default field (value)
- read:
    selector: { id: "Z01-01-02/2" }
    as: qtyBefore
    # field defaults to value

# Explicit field
- read:
    selector: { id: "row_1" }
    field: label
    as: rowLabel

# Boolean state field
- read:
    selector: { id: "terms_checkbox" }
    field: checked
    as: agreed
    # binds a real boolean, not the string "true"
```

Attribute fields (`id`, `text`, `label`, `hint`, `value`, `role`,
`class`, `package`, `bounds`) bind as strings. State fields
(`clickable`, `enabled`, `focused`, `selected`, `checked`, `visible`)
bind as booleans. `field` defaults to `value`. The command waits for
the element up to a fixed default timeout of 5000 ms, then fails the
step if the element has not appeared by then. An absent attribute or
an unreported state field fails the step. A present empty string
binds an empty string.

### assertExpr

Assert a boolean expression over variables read earlier.

```yaml
- read:
    selector: { id: "qty_field" }
    as: qtyBefore
- tap: "Plus 5"
- read:
    selector: { id: "qty_field" }
    as: qtyAfter
- assertExpr:
    expr: "number(qtyAfter) == number(qtyBefore) + 5"
    # asserts a runtime relation without hardcoding an absolute value
```

The expression references variables by their bare name, not the
`${...}` form. It must evaluate to a boolean, and a false result
fails the step. See the Expression Syntax section for the full
operator and casting rules.

### screenshot

Capture a screenshot. Saved in test artifacts.

```yaml
- screenshot: login_screen
- screenshot                    # auto-labeled by step number
```

### swipe

Swipe the screen in a direction.

```yaml
- swipe: up
- swipe: down
- swipe: left
- swipe: right
```

### pressKey

Press a device key.

```yaml
- pressKey: back
- pressKey: home
- pressKey: enter
```

Which keys the current platform recognizes, and how each one behaves,
differs by platform. See the `press_key` platform support table in
[`mcp-tools.md`](./mcp-tools.md#press_key).

### back

Shorthand for `pressKey: back`.

```yaml
- back
```

### scrollUntilVisible

Scroll until an element sits inside the viewport. The selector takes the
same forms as `tap`.

```yaml
- scrollUntilVisible: "Terms of service"
- scrollUntilVisible:
    id: footer
    direction: down
```

| Field | Effect |
| --- | --- |
| `direction` | `down` (default) or `up`. The direction searched first. |

The step searches in both directions, so the element is found wherever it
sits, and `direction` only decides which one is tried first. The step
fails, with the element-not-found code, when scrolling never reveals the
element. It does not tap the element or check whether another element
covers it. A `tap` or `type` step scrolls its own target into view, so use
`scrollUntilVisible` when the scroll is the point of the step or when an
assertion follows it. There is no timeout or visibility percentage, because
the search is bounded by a fixed number of swipes.

### waitForAnimationToEnd

Wait until the screen stops changing. The step compares consecutive
snapshots of the screen and passes when two of them are identical.

```yaml
- waitForAnimationToEnd
- waitForAnimationToEnd: 3000
- waitForAnimationToEnd:
    timeout: 3000
```

The timeout is in milliseconds and defaults to 5000. A screen that is
still changing when the timeout elapses does not fail the step, because
a permanent animation such as a spinner never settles and the next step
still has to run. The step result says whether the screen settled. Use
`waitFor` or `assertVisible` when the script needs a specific element.

### sleep

Wait for a fixed duration (milliseconds).

```yaml
- sleep: 2000
```

### pointer

General-purpose pointer gesture. Expresses every W3C Actions
sequence: tap, long-press, drag, press-and-drag, flick. Use the
narrower commands (`tap`, `swipe`, `pressKey`) for the common
cases. Reach for `pointer` when you need timing control that
those don't expose (drag from A to B, press-and-hold before a
drag, etc.).

Five action types, executed in order:

| Action | Payload | Semantics |
|---|---|---|
| `down` | `"selector"` or `{ x, y }` | Touch down at the resolved point |
| `move` | `"selector"` or `{ x, y }` | Drag to the resolved point |
| `wait` | integer ms | Hold the current position |
| `up` | (no payload) | Release the pointer |
| `capture` | optional `label` | Take a screenshot while the pointer is held down, mid-gesture |

`capture` is valid only strictly between a sequence's opening `down`
and its closing `up`. Its optional label names the artifact the
capture produces in the run report.

```yaml
- pointer:
    touches:
      - actions:
          - down: "Item A"
          - wait: 300
          - capture: mid-drag
          - move: "Drop zone"
          - up
```

A pointer step carries a `touches` list, one entry per pointer.
Each entry holds an `actions` sequence and, for a multi-pointer
gesture, a unique `id`. A single entry is a single-pointer gesture.
Each sequence must open with `down` and close with `up`, with exactly
one `down` before each `up`.

**Tap**: `down` then `up` at the same point:

```yaml
- pointer:
    touches:
      - actions:
          - down: "Submit"
          - up
```

**Long-press**: `down`, `wait`, `up`:

```yaml
- pointer:
    touches:
      - actions:
          - down: "Item A"
          - wait: 800
          - up
```

**Drag**: `down`, `move`, `up`:

```yaml
- pointer:
    touches:
      - actions:
          - down: { x: 100, y: 500 }
          - move: { x: 300, y: 500 }
          - up
```

**Long-press then drag**: `down`, `wait`, `move`, `up`:

```yaml
- pointer:
    touches:
      - actions:
          - down: "Item A"
          - wait: 800
          - move: "Drop zone"
          - up
```

Selector targets in `down` resolve once when the pointer touches
down. Selector targets in `move` re-resolve at their own step, so
the drag can target an element that animated into view during
the gesture. Coordinate targets (`{ x, y }`) are always absolute.

**Multi-pointer gestures**: pinch, rotate, multi-finger
custom. Give `touches` more than one entry, each with a unique
`id` and its own sequence, and the runner aligns them on a shared wall
clock anchored at the first `down`.

```yaml
- pointer:
    touches:
      - id: finger1
        actions:
          - down: { x: 100, y: 300 }
          - move: { x: 50, y: 300 }
          - up
      - id: finger2
        actions:
          - down: { x: 100, y: 500 }
          - move: { x: 150, y: 500 }
          - up
    moveDurationMs: 300
```

Multi-pointer requires driver support:

| Platform | Requirement |
|---|---|
| iOS | runtime probe that is supported on Xcode 16+ sim and recent physical devices |
| Android | supported through simultaneous multi-pointer dispatch |

Scripts whose `touches` list has more than one entry, run on a
driver without multi-pointer capability, fail at the validator
with `POINTER_MULTI_NOT_SUPPORTED`.

**Pressure-sensitive gestures** (3D Touch / Force Touch): add
`pressure: 0.0..1.0` to `down` or `move`:

```yaml
- pointer:
    touches:
      - actions:
          - down: { x: 200, y: 400, pressure: 0.3 }
          - move: { x: 200, y: 400, pressure: 0.9 }
          - up
```

Pressure requires `canPressure=true` on the active driver;
scripts setting pressure on a driver without the capability
fail with `POINTER_PRESSURE_NOT_SUPPORTED`.

See [`pointer.md`](./pointer.md) for further gesture patterns,
capability gates, and platform notes.

### longPress

Press and hold at a target, then release. When `thenSwipeTo` is
set, drag to it before releasing (a hold-then-drag). `at` and
`thenSwipeTo` accept the same target as a pointer action: an
absolute `{ x, y }` coordinate or a selector, resolved by the
driver at dispatch time. `holdMs` defaults to 500.

```yaml
- longPress:
    at: "Item A"
    holdMs: 600
    thenSwipeTo: "Drop zone"
```

`longPress` compiles to the pointer gesture path: a hold-only
press dispatches as a long-press primitive, and a hold-then-drag
holds correctly on every platform.

### pinch

Two-finger pinch centered on `center`, moving the two contact
points from `startGap` apart to `endGap` apart. An `endGap`
greater than `startGap` spreads the fingers (a zoom-in). `center`
accepts a coordinate or a selector. A selector is resolved to the
element's center at dispatch time. `axis` is `vertical` (default)
or `horizontal`. `durationMs` defaults to 200.

```yaml
- pinch:
    center: { id: photo }
    startGap: 80
    endGap: 320
    axis: vertical
```

### rotate

Two-finger rotation of `degrees` around `center`, with the two
contact points placed `radius` apart on opposite sides and each
tracing an arc. `center` accepts a coordinate or a selector, like
`pinch`. `radius` defaults to 100 points, and `durationMs` defaults
to 200.

```yaml
- rotate:
    center: { id: dial }
    degrees: 90
    radius: 120
```

`pinch` and `rotate` are multi-pointer gestures, so they require
`canMultiPointer` on the active driver and fail with
`POINTER_MULTI_NOT_SUPPORTED` otherwise. The existing `tap` and
`swipe` commands are unaffected by these three steps.

See [`pointer.md`](./pointer.md) §"Gesture sugar" for `longPress`,
`pinch`, and `rotate` alongside the raw `pointer` primitives they
compile to.

---

## API Capture

Capture HTTP traffic from the app via a MITM proxy, then validate responses.

> Requires `proxy: required` in config and `--proxy` flag when running.

### capture

Wait for an API request matching a pattern and store it.

```yaml
# Method + path
- capture: "POST /api/login as: login"

# Path only (matches any method)
- capture: "/api/config as: config"

# GET with path
- capture: "GET /api/products as: products"
```

The captured response is stored under the variable name after `as:`.

### assertApi

Validate a captured API response.

```yaml
- assertApi:
    from: login
    status: 200
    body:
      $.token: $not_empty
      $.user.email: user@test.com
      $.user.active: true
```

**`status`**: HTTP status code (number).

**`body`**: Dot-path assertions against the response body.
Paths start with `$.` and support nested objects and arrays:

```yaml
body:
  $.name: "John"                    # top-level field
  $.user.email: "john@test.com"     # nested field
  $.items[0].id: 123                # array index
  $.data[0].tags[1]: "featured"     # nested array
```

### Assertion Operators

Operators start with `$` to distinguish from literal values.

```yaml
body:
  # Exact match (no $ prefix)
  $.status: "completed"
  $.count: 42
  $.active: true

  # $not_empty, that is, not null, not "", not []
  $.token: $not_empty

  # $exists, that is, field is present (even if null)
  $.data: $exists

  # $not_exists, that is, field is absent
  $.error: $not_exists

  # $contains, that is, string includes substring
  $.message: "$contains:success"

  # Numeric comparisons
  $.count: "$gt:0"
  $.count: "$gte:1"
  $.page: "$lt:100"
  $.page: "$lte:99"

  # Range (inclusive)
  $.ttl: "$between:1,86400"
```

> Without `$` prefix, values match literally.
> `"not_empty"` matches the string "not_empty".
> `$not_empty` checks the value is not empty.

### extract

Extract values from a captured response into variables for later use.

```yaml
- capture: "POST /api/login as: login"
- extract:
    from: login
    values:
      token: $.body.token
      userId: $.body.user.id

# Use extracted values in subsequent steps
- assertVisible: "${userId}"
```

---

## Flow Control

### handle

UI-based branching: detect which screen the app is showing and act accordingly.

```yaml
- tap: "Submit"
- handle:
    - when:
        visible: "Enter OTP"
      do:
        - type: "123456"
        - tap: "Verify"
    - when:
        visible: "Success"
      do:
        - screenshot: success
    - otherwise: fail
```

**`when`** conditions:

```yaml
# Element is visible
- when:
    visible: "OTP Screen"

# Element is NOT visible
- when:
    notVisible: "Loading"

# Both conditions
- when:
    visible: "Dashboard"
    notVisible: "Error"
```

**`otherwise`**: `fail` (default) or `skip`.

**`do`** can be inline steps or a file reference:

```yaml
# Inline steps
- when:
    visible: "OTP"
  do:
    - type: "123456"
    - tap: "Verify"

# File reference
- when:
    visible: "OTP"
  do: flows/otp-verify.yml
```

### branch

API-based branching: route based on a captured API response.

```yaml
- capture: "POST /api/payment as: payment"
- branch:
    from: payment
    on:
      - match:
          body:
            $.requires_otp: true
        do:
          - waitFor: "Enter OTP"
          - type: "123456"
      - match:
          status: 400
        do:
          - screenshot: error
          - assertVisible: "Payment failed"
    default:
      - waitFor: "Success"
```

**`match`** conditions:

```yaml
# By status
- match:
    status: 200

# By body field
- match:
    body:
      $.type: "premium"

# Both
- match:
    status: 200
    body:
      $.requires_otp: true
```

**`default`** runs when no case matches. Can be inline steps or file reference.

### runFlow

Execute another YML file as a sub-flow.

```yaml
# Simple
- runFlow: flows/login.yml

# With env overrides
- runFlow:
    file: flows/login.yml
    env:
      email: other@test.com
```

Sub-flows inherit the parent's variables and captures. They can be
either full scripts (with config) or flow fragments (steps only).

### repeat

Run a block of steps in a loop, a fixed number of times, while a
condition holds, or both.

```yaml
# A fixed count
- repeat:
    times: 3
    commands:
      - swipe: up
      - assertVisible: "Next page"

# While an element is visible
- repeat:
    while:
      visible: "Load more"
    commands:
      - tap: "Load more"

# Whichever comes first
- repeat:
    times: 10
    while:
      notVisible: "Done"
    commands:
      - swipe: up
```

`commands` is required and holds at least one step. At least one of
`times` and `while` is required.

**`times`** is a whole number of at least 1.

**`while`** sets exactly one condition, which is checked before every
iteration:

```yaml
# The element is on screen
while:
  visible: "Load more"

# The element is not on screen
while:
  notVisible: "Done"

# A boolean expression over runtime variables
while:
  expr: attempts < 5
```

`visible` and `notVisible` take a string or a selector object, like
`handle` conditions. `expr` uses the syntax in
[Expression Syntax](#expression-syntax), and it must evaluate to a
boolean.

**Stopping.** With `times` alone the block runs exactly `times`
iterations. With `while` alone the loop ends when the condition stops
holding, and it fails if the condition still holds after 50
iterations. With both, the loop ends at whichever comes first, and the
50 iteration limit does not apply because `times` bounds the loop.

**Failures.** The first failing step ends the loop. The step detail
names the iteration, for example `repeat: iteration 2 of 4 failed:` and
the nested step's own detail. Every nested step appears in the run
events at one level deeper than the `repeat` step, in iteration order.

### retry

Run a block of steps and, when any step in it fails, run the whole
block again from its first step.

```yaml
# One retry, the default
- retry:
    commands:
      - tap: "Refresh"
      - assertVisible: "Results"

# Up to three retries
- retry:
    maxRetries: 3
    commands:
      - tap: "Submit"
      - assertVisible: "Confirmed"
```

`commands` is required and holds at least one step.

**`maxRetries`** is a whole number from 0 to 3, the number of extra
attempts after the first one fails. It defaults to 1 when omitted, so
a block runs at most twice. With 0 the block runs once and a failure
fails the step as usual.

**Attempts.** The block always restarts from its first step.
Variables and captures that an earlier attempt wrote stay set. The
step passes on the first attempt that passes, and its detail names the
attempt, for example `retry: passed on attempt 2 of 3`.

**Failures.** When every attempt fails, the step fails with the last
attempt's error, for example `retry: attempt 3 of 3 failed:` and the
nested step's own detail. Every attempt's steps appear in the run
events at one level deeper than the `retry` step, so a failed earlier
attempt is visible there.

---

## Flow Fragments

A flow fragment is a steps-only YML file, with no config, no `---`:

```yaml
# flows/login.yml, that is, a reusable login flow
- tap: "Email"
- type: ${email}
- tap: "Password"
- type: ${password}
- tap: "Login"
- waitFor: "Dashboard"
```

<!-- atomyx-allow-path: flows/login.yml -->
<!-- atomyx-allow-path: runFlow: flows/login.yml -->
<!-- atomyx-allow-path: do: flows/login.yml -->
Used by:
- `requires: [flows/login.yml]`
- `runFlow: flows/login.yml`
- `do: flows/login.yml` (in handle/branch)

Flow fragments execute in the parent's context. They inherit
variables, captures, and share state.

---

## Dependencies

Run other scripts before this one:

```yaml
requires:
  - flows/login.yml
  - flows/add-to-cart.yml
---
- tap: "Checkout"
```

Behavior:
- Required flows run in order, before the main steps
- If a required flow fails, the main script is **skipped** (fail fast)
- **Dedup**: same flow runs once even if required by multiple scripts
- **Circular detection**: A requires B requires A results in an error
- **Shared state**: variables and captures persist through the chain

---

## Proxy Setup

Required for `capture` / `assertApi` / `extract` / `branch` commands,
and for any script declaring `proxy: required` in its config fields.

### Without a proxy configured

A `proxy: required` script run with no network capture adapter
configured is refused before its first step: zero steps run, and the
result carries a top level abort naming the code `PROXY_REQUIRED`, a
message explaining that the script needs network capture, and a
remedy pointing at `--proxy` on the CLI or the `proxy` argument on the
MCP server's `run_script` tool. Nothing runs partway and no route,
host, or port detail leaks into the message. Configure a capture
adapter with the platform steps below, or drop `proxy: required` from
the script's config fields when the run genuinely needs no capture.

### Prerequisites

```bash
brew install mitmproxy
```

### Start proxy

```bash
make ready-ios-capture
```

The `ready-ios-capture` target wraps mitmdump with a local
capture add-on that streams captured flows into
`.atomyx/capture/*.jsonl`. The add-on ships per-contributor
(under `scripts/`, gitignored). Run `make ready-ios-capture`
to start the proxy.

### Configure device

Platform notes (the proxy and capture file format are the same on
both; only the device-side configuration differs):

- **iOS**: Settings, then Wi-Fi, then HTTP Proxy, then Manual:
  - Server: your Mac's IP
  - Port: 8889

  Trust mitmproxy CA cert (one-time):
  1. Download: `http://<mac-ip>:9999/mitmproxy-ca-cert.pem`
  2. Install: Settings, then General, then VPN & Device Management
  3. Trust: Settings, then General, then About, then Certificate Trust Settings

- **Android**: point the device at the host over `adb`, then start
  mitmdump directly (no Makefile target ships for Android yet):

  ```bash
  HOST_IP=$(ipconfig getifaddr en0 || ipconfig getifaddr en1)
  adb shell settings put global http_proxy ${HOST_IP}:8889
  adb shell settings put global global_http_proxy_host ${HOST_IP}
  adb shell settings put global global_http_proxy_port 8889

  mitmdump --listen-port 8889 --listen-host 0.0.0.0 -w /tmp/atomyx-capture.jsonl
  ```

  Trust mitmproxy CA cert (one-time): serve
  `~/.mitmproxy/mitmproxy-ca-cert.pem` from the host and install it on
  the device through Settings, then Security, then Install
  certificate, then CA certificate. A user-installed cert covers apps
  that respect the user trust store; an app pinning to the system
  trust store needs a rooted device or a debug build instead.

### Run with capture

```bash
atomyx run \
  --file script.yml \
  --platform ios --device <UDID> \
  --proxy mitmproxy:/tmp/atomyx-capture.jsonl

atomyx run \
  --file script.yml \
  --platform android --device <serial> \
  --proxy mitmproxy:/tmp/atomyx-capture.jsonl
```

---

## CLI Reference

```bash
# Run a script
atomyx run --file <path> --platform <ios|android> --device <id>

# With proxy capture
atomyx run --file <path> --platform ios --device <id> \
  --proxy mitmproxy:/tmp/atomyx-capture.jsonl

# JSON output (for CI)
atomyx run --file <path> --platform ios --device <id> --json

# List devices
atomyx driver list-devices
atomyx driver list-devices --json
```

---

## Examples

See `examples/` directory:

| File | Description |
|---|---|
| `test-login-flow.yml` | login + OTP branching + handle + assertVisible, which is exercised end-to-end against `examples/atomyx-demo` |
