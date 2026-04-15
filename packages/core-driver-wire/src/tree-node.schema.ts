import { z } from "zod";

/**
 * Canonical wire-shape for a UI tree node. Mirrors the `TreeNode`
 * interface exported by `@atomyx/core-driver/tree` — intentionally
 * duplicated here as a Zod schema so the wire-schema package has
 * zero runtime dependency on core (dependency direction: drivers
 * depend on both; core and wire-schema are siblings).
 *
 * Shape invariants drivers MUST honor:
 *
 *   - `attributes` is a flat map of string→string. Keys should
 *     come from the canonical `AttrKeys` set (`id`, `text`,
 *     `label`, `hint`, `value`, `role`, `class`, `package`,
 *     `bounds`). Keys outside the canonical set MUST start with
 *     `ext:` to signal non-portable extension data.
 *
 *   - `children` is an ordered list. Document order matters for
 *     z-order detection — later siblings render on top of earlier
 *     ones.
 *
 *   - State booleans (`clickable`, `enabled`, `focused`, `selected`,
 *     `checked`) are optional. `undefined` means "unknown", NOT
 *     "false" — consumers must distinguish the two.
 *
 * The schema uses `z.lazy` for recursion because children are
 * themselves `TreeNodeWire` values. Zod type inference handles
 * this through `z.ZodType<TreeNodeWire>` declaration.
 */
export interface TreeNodeWire {
  attributes: Record<string, string>;
  children: TreeNodeWire[];
  clickable?: boolean;
  enabled?: boolean;
  focused?: boolean;
  selected?: boolean;
  checked?: boolean;
}

export const TreeNodeSchema: z.ZodType<TreeNodeWire> = z.lazy(() =>
  z.object({
    attributes: z.record(z.string(), z.string()),
    children: z.array(TreeNodeSchema),
    clickable: z.boolean().optional(),
    enabled: z.boolean().optional(),
    focused: z.boolean().optional(),
    selected: z.boolean().optional(),
    checked: z.boolean().optional(),
  }),
);

// ── Primitive shapes shared across routes ───────────────────────

export const PointSchema = z.object({
  x: z.number(),
  y: z.number(),
});
export type PointWire = z.infer<typeof PointSchema>;

export const SizeSchema = z.object({
  width: z.number(),
  height: z.number(),
});
export type SizeWire = z.infer<typeof SizeSchema>;

export const BoundsSchema = z.object({
  left: z.number(),
  top: z.number(),
  right: z.number(),
  bottom: z.number(),
});
export type BoundsWire = z.infer<typeof BoundsSchema>;

/**
 * Cross-platform key vocabulary. Drivers translate these names
 * into their native key codes internally. Strings outside this
 * enum are allowed for driver-specific keys (Android
 * KEYCODE_ constants, iOS hardware keys), but consumers who use
 * them sacrifice portability.
 */
export const KeyCodeSchema = z.union([
  z.literal("back"),
  z.literal("home"),
  z.literal("enter"),
  z.literal("tab"),
  z.literal("escape"),
  z.literal("delete"),
  z.literal("space"),
  z.literal("up"),
  z.literal("down"),
  z.literal("left"),
  z.literal("right"),
  z.string(), // escape hatch for driver-specific keys
]);
export type KeyCodeWire = z.infer<typeof KeyCodeSchema>;

export const KeyResultSchema = z.object({
  ok: z.boolean(),
  reason: z.string().optional(),
});
export type KeyResultWire = z.infer<typeof KeyResultSchema>;

// ── Driver metadata ─────────────────────────────────────────────

export const CapabilitiesSchema = z.object({
  canScreenshot: z.boolean(),
  canEraseText: z.boolean(),
  canWaitForIdle: z.boolean(),
  canSetLocation: z.boolean(),
  canSetOrientation: z.boolean(),
  supportedKeyCodes: z.array(z.string()),
});
export type CapabilitiesWire = z.infer<typeof CapabilitiesSchema>;

export const DeviceInfoSchema = z.object({
  platform: z.string(),
  platformVersion: z.string(),
  model: z.string(),
  udid: z.string(),
  kind: z.union([
    z.literal("simulator"),
    z.literal("emulator"),
    z.literal("device"),
  ]),
});
export type DeviceInfoWire = z.infer<typeof DeviceInfoSchema>;

export const ForegroundInfoSchema = z.object({
  bundleId: z.string().nullable(),
  activity: z.string().optional(),
});
export type ForegroundInfoWire = z.infer<typeof ForegroundInfoSchema>;

export const InstalledAppSchema = z.object({
  bundleId: z.string(),
  displayName: z.string(),
});
export type InstalledAppWire = z.infer<typeof InstalledAppSchema>;

// ── Common response envelopes ───────────────────────────────────

/**
 * All write-path routes (`POST /...`) return this envelope on
 * success. Drivers that cannot report meaningful `ok` semantics
 * (e.g. Android fire-and-forget intents) return `ok: true`
 * unconditionally — consumers should use `reason` as advisory
 * only.
 */
export const WriteAckSchema = z.object({
  ok: z.boolean(),
  reason: z.string().optional(),
});
export type WriteAckWire = z.infer<typeof WriteAckSchema>;

/**
 * Health probe response. `version` lets callers detect driver
 * version skew against the wire schema version they expect.
 */
export const HealthSchema = z.object({
  ok: z.boolean(),
  version: z.string(),
  capabilities: CapabilitiesSchema,
});
export type HealthWire = z.infer<typeof HealthSchema>;
