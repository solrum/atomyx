# @atomyx/core-driver-wire

Canonical HTTP/JSON wire protocol schemas for the Atomyx
core-driver module. Drivers (iOS TCP, Android HTTP, future Web)
implement the routes defined here; host adapters validate
payloads against these schemas at the wire boundary.

## What's inside

- **`TreeNodeSchema`** — Zod schema for the canonical `TreeNode`
  shape drivers emit on `/hierarchy`. Mirrors the `TreeNode`
  interface from `@atomyx/core-driver` (kept duplicated here so
  this package has zero runtime dependency on core — the two
  are siblings).
- **Primitive schemas** — `PointSchema`, `SizeSchema`,
  `BoundsSchema`, `KeyCodeSchema`, `KeyResultSchema`,
  `CapabilitiesSchema`, `DeviceInfoSchema`, `ForegroundInfoSchema`,
  `InstalledAppSchema`.
- **Route registry** (`src/routes.schema.ts`) — 19 route
  definitions with `{method, path, request, response}` quads.
  Central `ROUTES` object enables codegen + iteration.
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
POST /input/text, /input/erase, /input/key
POST /app/launch, /app/stop, /app/kill
GET  /app/foreground, /app/list
GET  /media/screenshot
POST /idle/wait
POST /geometry/hit-test   (optional)
```

## Current usage

`@atomyx/core-driver-android` + `@atomyx/core-driver-ios` import
this package for type definitions of the wire payloads they
produce and consume. Drivers currently translate between the
legacy Kotlin/Swift native wire shapes and the canonical
`TreeNodeWire` defined here — full adoption of the `ROUTES` surface
in the native drivers is planned for a future batch.

## Dependencies

- `zod` — runtime schema validation

## See also

- [`@atomyx/core-driver`](../core-driver) — consumer of the types
- [`@atomyx/core-driver-ios`](../core-driver-ios), [`@atomyx/core-driver-android`](../core-driver-android) — drivers that implement the wire
- [`.claude/docs/architecture.md`](../../.claude/docs/architecture.md) §3 — the interface layering contract
