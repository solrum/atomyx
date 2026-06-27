# Atomyx YML Script Reference

> Format: `atomyx/v1`
>
> Write test scripts in YAML, run them on real devices — no code required.

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
| `proxy` | No | `required` or `optional` (default) |
| `requires` | No | Flow files to run before this script |

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

---

## Commands

### launchApp

Launch the app specified in `appId`.

```yaml
- launchApp
```

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

### back

Shorthand for `pressKey: back`.

```yaml
- back
```

### sleep

Wait for a fixed duration (milliseconds).

```yaml
- sleep: 2000
```

### pointer

General-purpose pointer gesture. Expresses every W3C Actions
sequence: tap, long-press, drag, press-and-drag, flick. Use the
narrower commands (`tap`, `swipe`, `pressKey`) for the common
cases; reach for `pointer` when you need timing control that
those don't expose (drag from A to B, press-and-hold before a
drag, etc.).

Four action types, executed in order:

| Action | Payload | Semantics |
|---|---|---|
| `down` | `"selector"` or `{ x, y }` | Touch down at the resolved point |
| `move` | `"selector"` or `{ x, y }` | Drag to the resolved point |
| `wait` | integer ms | Hold the current position |
| `up` | (no payload) | Release the pointer |

A sequence must open with `down` and close with `up`. Exactly
one `down` before each `up`.

**Tap** — `down` then `up` at the same point:

```yaml
- pointer:
    actions:
      - down: "Submit"
      - up
```

**Long-press** — `down`, `wait`, `up`:

```yaml
- pointer:
    actions:
      - down: "Item A"
      - wait: 800
      - up
```

**Drag** — `down`, `move`, `up`:

```yaml
- pointer:
    actions:
      - down: { x: 100, y: 500 }
      - move: { x: 300, y: 500 }
      - up
```

**Long-press then drag** — `down`, `wait`, `move`, `up`:

```yaml
- pointer:
    actions:
      - down: "Item A"
      - wait: 800
      - move: "Drop zone"
      - up
```

Selector targets in `down` resolve once when the pointer touches
down; selector targets in `move` re-resolve at their own step so
the drag can target an element that animated into view during
the gesture. Coordinate targets (`{ x, y }`) are always absolute.

**Multi-pointer gestures** — pinch, rotate, multi-finger
custom — use the `pointers:` form instead of `actions:`. Each
entry is one pointer with its own sequence; the runner aligns
them on a shared wall clock anchored at the first `down`.

```yaml
- pointer:
    pointers:
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
| iOS | runtime probe — supported on Xcode 16+ sim and recent physical devices |
| Android | multi-stroke APK route — not yet shipped |

Scripts that use the `pointers:` form on a driver without
multi-pointer capability fail at the validator with
`POINTER_MULTI_NOT_SUPPORTED`.

**Pressure-sensitive gestures** (3D Touch / Force Touch) — add
`pressure: 0.0..1.0` to `down` or `move`:

```yaml
- pointer:
    actions:
      - down: { x: 200, y: 400, pressure: 0.3 }
      - move: { x: 200, y: 400, pressure: 0.9 }
      - up
```

Pressure requires `canPressure=true` on the active driver;
scripts setting pressure on a driver without the capability
fail with `POINTER_PRESSURE_NOT_SUPPORTED`.

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

**`status`** — HTTP status code (number).

**`body`** — Dot-path assertions against the response body.
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

  # $not_empty — not null, not "", not []
  $.token: $not_empty

  # $exists — field is present (even if null)
  $.data: $exists

  # $not_exists — field is absent
  $.error: $not_exists

  # $contains — string includes substring
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

UI-based branching — detect which screen the app is showing and act accordingly.

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

API-based branching — route based on a captured API response.

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

---

## Flow Fragments

A flow fragment is a steps-only YML file — no config, no `---`:

```yaml
# flows/login.yml — reusable login flow
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

Flow fragments execute in the parent's context — they inherit
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
- If a required flow fails → main script is **skipped** (fail fast)
- **Dedup**: same flow runs once even if required by multiple scripts
- **Circular detection**: A requires B requires A → error
- **Shared state**: variables and captures persist through the chain

---

## Proxy Setup

Required for `capture` / `assertApi` / `extract` / `branch` commands.

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
(under `scripts/`, gitignored); run `make ready-ios-capture`
to start the proxy.

### Configure device

Settings → Wi-Fi → HTTP Proxy → Manual:
- Server: your Mac's IP
- Port: 8889

Trust mitmproxy CA cert (one-time):
1. Download: `http://<mac-ip>:9999/mitmproxy-ca-cert.pem`
2. Install: Settings → General → VPN & Device Management
3. Trust: Settings → General → About → Certificate Trust Settings

### Run with capture

```bash
atomyx run \
  --file script.yml \
  --platform ios --device <UDID> \
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
| `test-login-flow.yml` | login + OTP branching + handle + assertVisible — exercised end-to-end against `examples/atomyx-demo` |
