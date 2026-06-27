# @atomyx/driver-wire

Shared wire types for Atomyx platform driver adapters. Provides the
canonical `TreeNodeWire` shape and primitive types (`PointWire`,
`SizeWire`, `BoundsWire`, etc.) that driver adapters emit and core logic
consumes.

## What's inside

- **`TreeNodeWire`** — the canonical UI tree node shape. Driver adapters
  translate platform-native accessibility trees into this type before
  the core framework consumes the result.
- **Primitive types** — `PointWire`, `SizeWire`, `BoundsWire`,
  `KeyCodeWire`, `KeyResultWire`, `CapabilitiesWire`, `DeviceInfoWire`,
  `ForegroundInfoWire`, `InstalledAppWire`, `WriteAckWire`, `HealthWire`.
- **Zod schemas** — `TreeNodeSchema` and companions for internal adapter
  validation. Consumers that only need types use the `type` import; those
  that need runtime validation import the schema directly.

## Shape invariants

`TreeNodeWire` carries:

- `attributes` — flat `string → string` map. Canonical keys: `id`,
  `text`, `label`, `hint`, `value`, `role`, `class`, `package`, `bounds`.
  Non-portable extension data uses an `ext:` prefix.
- `children` — ordered list; document order matches z-order.
- Optional state booleans: `clickable`, `enabled`, `focused`, `selected`,
  `checked`, `visible`. `undefined` means unknown, not false.

## Wire protocols

Each platform driver speaks its own on-wire format. This package provides
the shared **host-side types** that normalizers on each adapter produce.
The per-platform wire shapes are documented in
[`.claude/docs/architecture.md`](../../.claude/docs/architecture.md)
§ "Wire protocols".

## Dependencies

- `zod` — used by the Zod schemas exported alongside the plain TS types.

## See also

- [`@atomyx/driver`](../driver) — consumer of the types.
- [`@atomyx/ios-driver`](../ios-driver),
  [`@atomyx/android-driver`](../android-driver) — adapters that
  normalize into `TreeNodeWire`.
