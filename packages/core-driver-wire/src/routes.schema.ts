import { z } from "zod";
import {
  BoundsSchema,
  CapabilitiesSchema,
  DeviceInfoSchema,
  ForegroundInfoSchema,
  HealthSchema,
  InstalledAppSchema,
  KeyCodeSchema,
  KeyResultSchema,
  PointSchema,
  SizeSchema,
  TreeNodeSchema,
  WriteAckSchema,
} from "./tree-node.schema.js";

/**
 * Wire protocol routes — the canonical HTTP+JSON contract
 * between the Atomyx host (any language) and a platform driver
 * (Kotlin APK, Swift XCTest runner, future Web/desktop driver).
 *
 * Framework rule: this file is the SINGLE SOURCE OF TRUTH for
 * what bytes cross the wire. Drivers MUST validate incoming
 * requests against the request schemas and format outgoing
 * responses to match the response schemas. Host adapters MUST
 * validate driver responses against the response schemas and
 * reject payloads that fail.
 *
 * Adding a new route = add a `RouteX` object here with paired
 * request + response Zod schemas, then implement the driver
 * side (platform native) and host side (TypeScript adapter).
 * No route may exist without a schema entry.
 *
 * Naming convention:
 *
 *   - Read-only: `GET /<resource>` or `GET /<resource>/<sub>`.
 *   - Mutations: `POST /<resource>/<action>`. Mutations accept a
 *     JSON body with the action-specific arguments and return
 *     either `WriteAck` (fire-and-forget) or a resource-specific
 *     response when additional data is produced.
 *   - Every route is versioned by the `HealthSchema.version` field
 *     returned from `GET /health` — consumers check this once at
 *     connect time and reject drivers with an incompatible wire
 *     protocol version.
 *
 * Current wire protocol version: 1.0
 */

export const WIRE_PROTOCOL_VERSION = "1.0" as const;

// ── Meta ────────────────────────────────────────────────────────

export const HealthRoute = {
  method: "GET" as const,
  path: "/health" as const,
  request: z.undefined(),
  response: HealthSchema,
};

export const DeviceInfoRoute = {
  method: "GET" as const,
  path: "/device-info" as const,
  request: z.undefined(),
  response: DeviceInfoSchema,
};

export const ScreenSizeRoute = {
  method: "GET" as const,
  path: "/screen-size" as const,
  request: z.undefined(),
  response: SizeSchema,
};

export const CapabilitiesRoute = {
  method: "GET" as const,
  path: "/capabilities" as const,
  request: z.undefined(),
  response: CapabilitiesSchema,
};

// ── Hierarchy ───────────────────────────────────────────────────

export const HierarchyRoute = {
  method: "GET" as const,
  path: "/hierarchy" as const,
  request: z.undefined(),
  response: z.object({
    tree: TreeNodeSchema,
    capturedAt: z.number().optional(),
  }),
};

// ── Gesture primitives ──────────────────────────────────────────

export const TapRoute = {
  method: "POST" as const,
  path: "/gesture/tap" as const,
  request: PointSchema,
  response: WriteAckSchema,
};

export const LongPressRoute = {
  method: "POST" as const,
  path: "/gesture/long-press" as const,
  request: PointSchema.extend({
    durationMs: z.number().positive(),
  }),
  response: WriteAckSchema,
};

export const SwipeRoute = {
  method: "POST" as const,
  path: "/gesture/swipe" as const,
  request: z.object({
    from: PointSchema,
    to: PointSchema,
    durationMs: z.number().positive(),
  }),
  response: WriteAckSchema,
};

// ── Input (text + keys) ─────────────────────────────────────────

export const InputTextRoute = {
  method: "POST" as const,
  path: "/input/text" as const,
  request: z.object({
    text: z.string(),
  }),
  response: WriteAckSchema,
};

export const EraseTextRoute = {
  method: "POST" as const,
  path: "/input/erase" as const,
  request: z.object({
    count: z.number().int().nonnegative(),
  }),
  response: WriteAckSchema,
};

export const PressKeyRoute = {
  method: "POST" as const,
  path: "/input/key" as const,
  request: z.object({
    key: KeyCodeSchema,
  }),
  response: KeyResultSchema,
};

// ── App lifecycle ───────────────────────────────────────────────

export const LaunchAppRoute = {
  method: "POST" as const,
  path: "/app/launch" as const,
  request: z.object({
    appId: z.string(),
    args: z.array(z.string()).optional(),
    environment: z.record(z.string(), z.string()).optional(),
  }),
  response: WriteAckSchema,
};

export const StopAppRoute = {
  method: "POST" as const,
  path: "/app/stop" as const,
  request: z.object({ appId: z.string() }),
  response: WriteAckSchema,
};

export const KillAppRoute = {
  method: "POST" as const,
  path: "/app/kill" as const,
  request: z.object({ appId: z.string() }),
  response: WriteAckSchema,
};

export const ForegroundRoute = {
  method: "GET" as const,
  path: "/app/foreground" as const,
  request: z.undefined(),
  response: ForegroundInfoSchema,
};

export const ListAppsRoute = {
  method: "GET" as const,
  path: "/app/list" as const,
  request: z.undefined(),
  response: z.object({
    apps: z.array(InstalledAppSchema),
  }),
};

// ── Media ───────────────────────────────────────────────────────

export const ScreenshotRoute = {
  method: "GET" as const,
  path: "/media/screenshot" as const,
  request: z.undefined(),
  response: z.object({
    base64: z.string(),
    format: z.union([z.literal("png"), z.literal("jpeg")]),
  }),
};

// ── Idle detection ──────────────────────────────────────────────

export const WaitForIdleRoute = {
  method: "POST" as const,
  path: "/idle/wait" as const,
  request: z.object({
    timeoutMs: z.number().int().positive(),
  }),
  response: z.object({
    idle: z.boolean(),
    waitedMs: z.number().nonnegative(),
  }),
};

// ── Element geometry helper ─────────────────────────────────────

/**
 * Optional helper route — drivers that expose native hit-testing
 * can honor this to return the hittable bounds for a point.
 * Consumers should NOT depend on this route existing; it's a
 * performance optimization. Host-side bounds parsing is always
 * the fallback.
 */
export const HitTestRoute = {
  method: "POST" as const,
  path: "/geometry/hit-test" as const,
  request: PointSchema,
  response: z.object({
    hit: z.boolean(),
    bounds: BoundsSchema.optional(),
  }),
};

// ── Route registry ──────────────────────────────────────────────

/**
 * Central registry of every wire route. Consumers iterate this
 * map to validate driver coverage, generate docs, or build
 * typed HTTP clients without hand-listing routes.
 */
export const ROUTES = {
  health: HealthRoute,
  deviceInfo: DeviceInfoRoute,
  screenSize: ScreenSizeRoute,
  capabilities: CapabilitiesRoute,
  hierarchy: HierarchyRoute,
  tap: TapRoute,
  longPress: LongPressRoute,
  swipe: SwipeRoute,
  inputText: InputTextRoute,
  eraseText: EraseTextRoute,
  pressKey: PressKeyRoute,
  launchApp: LaunchAppRoute,
  stopApp: StopAppRoute,
  killApp: KillAppRoute,
  foreground: ForegroundRoute,
  listApps: ListAppsRoute,
  screenshot: ScreenshotRoute,
  waitForIdle: WaitForIdleRoute,
  hitTest: HitTestRoute,
} as const;

export type RouteId = keyof typeof ROUTES;

/**
 * Given a route id, infer its request body type. Use in host
 * adapter to type-check outgoing payloads at the call site:
 *
 *   const body: RouteRequest<"tap"> = { x: 100, y: 200 };
 *   await httpPost("/gesture/tap", body);
 */
export type RouteRequest<K extends RouteId> = z.infer<(typeof ROUTES)[K]["request"]>;

/** Infer response type for a route id. */
export type RouteResponse<K extends RouteId> = z.infer<(typeof ROUTES)[K]["response"]>;

/**
 * Validate a driver response against a route's schema. Throws
 * `ZodError` with full path info on mismatch — host adapter
 * catches this and surfaces as "driver returned invalid payload
 * for route X" with enough detail to debug the driver side.
 */
export function parseResponse<K extends RouteId>(
  route: K,
  payload: unknown,
): RouteResponse<K> {
  return ROUTES[route].response.parse(payload) as RouteResponse<K>;
}

/**
 * Validate a request payload against a route's schema. Used on
 * the driver side to reject malformed requests before they reach
 * the native gesture layer.
 */
export function parseRequest<K extends RouteId>(
  route: K,
  payload: unknown,
): RouteRequest<K> {
  return ROUTES[route].request.parse(payload) as RouteRequest<K>;
}
