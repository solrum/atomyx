# @atomyx/driver-wire

Canonical HTTP / JSON wire-protocol schemas for the Atomyx
driver module. Drivers (iOS TCP, Android HTTP, future Web)
implement the routes defined here; host adapters validate payloads
against these schemas at the wire boundary.

## What's inside

- **`TreeNodeSchema`** — Zod schema for the canonical `TreeNode`
  shape drivers emit on `/hierarchy`. Declared here independently
  from the framework core so this package stays at zero runtime
  dependency on core — the two are siblings in the dependency
  graph.
- **Primitive schemas** — `PointSchema`, `SizeSchema`,
  `BoundsSchema`, `KeyCodeSchema`, `KeyResultSchema`,
  `CapabilitiesSchema`, `DeviceInfoSchema`, `ForegroundInfoSchema`,
  `InstalledAppSchema`.
- **Route registry** (`src/routes.schema.ts`) — route
  definitions with `{method, path, request, response}` quads.
  The central `ROUTES` object enables codegen + iteration.
- **`parseRequest` / `parseResponse`** — helpers that validate a
  payload against a named route's schema and throw `ZodError` on
  mismatch.
- **`WIRE_PROTOCOL_VERSION`** — semver string (`"1.0"`) consumers
  check at connect time to detect driver version skew.

## Route surface

```
GET  /health, /device-info, /screen-size, /capabilities
GET  /hierarchy
POST /gesture/tap, /gesture/long-press, /gesture/swipe
POST /input/text, /input/erase, /input/key, /input/hide-keyboard
POST /app/launch, /app/stop, /app/kill
GET  /app/foreground, /app/list
GET  /media/screenshot
POST /idle/wait
POST /geometry/hit-test   (optional)
```

## Usage

`@atomyx/android-driver` and `@atomyx/ios-driver` import this
package for the payload types they produce and consume. Drivers
translate between their platform's native wire shape and the
canonical `TreeNodeWire` + primitive schemas here, so the core
framework operates on a uniform structure regardless of which
driver answered the request.

## Dependencies

- `zod` — runtime schema validation.

## See also

- [`@atomyx/driver`](../driver) — consumer of the types.
- [`@atomyx/ios-driver`](../ios-driver),
  [`@atomyx/android-driver`](../android-driver) — drivers that
  implement the wire.
- [`.claude/docs/architecture.md`](../../.claude/docs/architecture.md) §3
  — the interface layering contract.
