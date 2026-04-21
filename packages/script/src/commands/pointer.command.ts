import { defineCommand } from "@atomyx/driver/script";
import type { Gesture, Point } from "@atomyx/driver/driver";
import type {
  PointerAction,
  PointerGroup,
  PointerStep,
  PointerTarget,
} from "@atomyx/shared/script";
import { compileScriptSelector } from "../parser/selector-compiler.js";

/**
 * Cap for the `wait: <ms>` action. Sane upper bound so a typo
 * can't stall a script for minutes silently.
 */
const MAX_WAIT_MS = 30_000;

/**
 * Cap for `moveDurationMs`. Same rationale as the wait cap.
 */
const MAX_MOVE_DURATION_MS = 10_000;

/**
 * Default drag duration when the author does not pass
 * `moveDurationMs`. Matches `Orchestra.swipeAt`'s own default so
 * behaviour stays consistent with existing coord-swipes.
 */
const DEFAULT_MOVE_DURATION_MS = 200;

/**
 * Default press-hold for the drag-without-wait pattern. The
 * Orchestra swipe primitive maps to
 * `press(forDuration:thenDragTo:)` on iOS, which requires a
 * nonzero press before the drag kicks in. 50ms is the minimum
 * we've seen XCUITest reliably accept as a "press then drag".
 */
const DRAG_MIN_PRESS_MS = 50;

/**
 * Structured error the pointer command throws for every
 * validator violation. `code` matches the `PointerErrorCode`
 * union below; `detail` is a human-readable message safe to
 * surface to the script author.
 */
export class PointerError extends Error {
  constructor(
    public readonly code: PointerErrorCode,
    public readonly detail: string,
  ) {
    super(`${code}: ${detail}`);
    this.name = "PointerError";
  }
}

export type PointerErrorCode =
  | "POINTER_EMPTY_SEQUENCE"
  | "POINTER_NO_OPENING_DOWN"
  | "POINTER_NO_CLOSING_UP"
  | "POINTER_NESTED_DOWN"
  | "POINTER_INVALID_WAIT"
  | "POINTER_INVALID_MOVE_DURATION"
  | "POINTER_FORM_CONFLICT"
  | "POINTER_MULTI_NOT_SUPPORTED"
  | "POINTER_PRESSURE_NOT_SUPPORTED"
  | "POINTER_PATTERN_NOT_EXPRESSIBLE"
  | "POINTER_SELECTOR_RESOLUTION_FAILED";

type Pattern =
  | { readonly kind: "tap"; readonly downPoint: Point }
  | {
      readonly kind: "longPress";
      readonly downPoint: Point;
      readonly holdMs: number;
    }
  | {
      readonly kind: "drag";
      readonly fromPoint: Point;
      readonly toPoint: Point;
      readonly pressMs: number;
    };

export const pointerCommand = defineCommand<PointerStep>({
  command: "pointer",
  async execute(args, ctx) {
    try {
      const moveDurationMs = validateMoveDuration(args.moveDurationMs);

      if (args.pointers !== undefined) {
        if (args.actions !== undefined) {
          throw new PointerError(
            "POINTER_FORM_CONFLICT",
            "pointer: exactly one of `actions` or `pointers` must be present",
          );
        }
        validateMultiPointer(args.pointers, ctx);
        validatePressureCapability(
          args.pointers.flatMap((p) => p.actions),
          ctx,
        );
        const gesture = await compileMultiPointer(args.pointers, moveDurationMs, ctx);
        await ctx.orchestra.dispatchGesture(gesture);
        return {
          ok: true,
          detail: `pointer ${gesture.pointers.length}-pointer gesture`,
        };
      }

      if (args.actions === undefined) {
        throw new PointerError(
          "POINTER_FORM_CONFLICT",
          "pointer: exactly one of `actions` or `pointers` must be present",
        );
      }

      validateSingleSequence(args.actions);
      validatePressureCapability(args.actions, ctx);

      // Single-pointer sequences with pressure cannot go through
      // the public `tapAt/longPressAt/swipeAt` primitives (they
      // carry no pressure). Route such sequences through
      // `dispatchGesture` as a one-pointer gesture; the private
      // synthesizer preserves pressure on the underlying path.
      const hasPressure = args.actions.some(
        (a) => (a.type === "down" || a.type === "move") && a.pressure !== undefined,
      );
      if (hasPressure) {
        const waypoints = await compilePointerWaypoints(
          args.actions, moveDurationMs, ctx,
        );
        await ctx.orchestra.dispatchGesture({
          pointers: [{ id: "single", waypoints }],
        });
        return { ok: true, detail: "pointer gesture (single pointer, pressure)" };
      }

      const pattern = await classifyPattern(args.actions, ctx);
      await dispatchPattern(pattern, moveDurationMs, ctx);

      return {
        ok: true,
        detail: `pointer ${pattern.kind}`,
      };
    } catch (err) {
      if (err instanceof PointerError) {
        return { ok: false, detail: err.message };
      }
      throw err;
    }
  },
});

function validateMoveDuration(raw: number | undefined): number {
  if (raw === undefined) return DEFAULT_MOVE_DURATION_MS;
  if (raw < 0 || raw > MAX_MOVE_DURATION_MS) {
    throw new PointerError(
      "POINTER_INVALID_MOVE_DURATION",
      `moveDurationMs must be between 0 and ${MAX_MOVE_DURATION_MS}, got ${raw}`,
    );
  }
  return raw;
}

function validatePressureCapability(
  actions: readonly PointerAction[],
  ctx: Parameters<typeof pointerCommand.execute>[1],
): void {
  const pressureCount = actions.filter(
    (a) => (a.type === "down" || a.type === "move") && a.pressure !== undefined,
  ).length;
  if (pressureCount === 0) return;
  if (!ctx.orchestra.capabilities.canPressure) {
    throw new PointerError(
      "POINTER_PRESSURE_NOT_SUPPORTED",
      "pressure-sensitive waypoints are not supported by the active driver. " +
        "On iOS, pressure requires a device with 3D Touch / Force Touch and " +
        "a runtime with gesture synthesis capability. On Android, pressure-" +
        "sensitive dispatch is not yet available.",
    );
  }
}

function validateMultiPointer(
  pointers: readonly PointerGroup[],
  ctx: Parameters<typeof pointerCommand.execute>[1],
): void {
  // Active driver must support multi-pointer; when capability
  // is false we reject before dispatch attempts.
  if (!ctx.orchestra.capabilities.canMultiPointer) {
    throw new PointerError(
      "POINTER_MULTI_NOT_SUPPORTED",
      "multi-pointer gestures are not supported by the active driver. " +
        "On iOS, upgrade Xcode to a version that supports multi-pointer " +
        "gesture dispatch. On Android, multi-pointer is not yet available.",
    );
  }

  // Minimum 2 pointers — one pointer belongs in the single-
  // pointer `actions:` form instead.
  if (pointers.length < 2) {
    throw new PointerError(
      "POINTER_FORM_CONFLICT",
      `multi-pointer form requires at least 2 pointers, got ${pointers.length}. ` +
        "Use the single-pointer `actions:` form for one pointer.",
    );
  }

  // Pointer ids must be unique within the gesture so error
  // messages and per-pointer telemetry are readable.
  const seen = new Set<string>();
  for (const p of pointers) {
    if (seen.has(p.id)) {
      throw new PointerError(
        "POINTER_FORM_CONFLICT",
        `duplicate pointer id "${p.id}". Each pointer in a multi-pointer gesture needs a unique id.`,
      );
    }
    seen.add(p.id);
  }

  // Shape validation per pointer: reuse the single-sequence
  // rules (opens with down, closes with up, no nested down,
  // valid wait range). This catches malformed individual
  // pointers before they reach the dispatcher.
  for (const p of pointers) {
    try {
      validateSingleSequence(p.actions);
    } catch (err) {
      if (err instanceof PointerError) {
        throw new PointerError(
          err.code,
          `pointer "${p.id}": ${err.detail}`,
        );
      }
      throw err;
    }
  }
}

async function compileMultiPointer(
  pointers: readonly PointerGroup[],
  moveDurationMs: number,
  ctx: Parameters<typeof pointerCommand.execute>[1],
): Promise<Gesture> {
  // Each pointer walks its own clock — the runner aligns them
  // on a shared wall clock anchored at the first `down` across
  // all pointers. Compile each to a waypoint list; the
  // synthesizer handles the actual time alignment downstream.
  const compiled = await Promise.all(
    pointers.map(async (p) => ({
      id: p.id,
      waypoints: await compilePointerWaypoints(p.actions, moveDurationMs, ctx),
    })),
  );
  return { pointers: compiled };
}

async function compilePointerWaypoints(
  actions: readonly PointerAction[],
  moveDurationMs: number,
  ctx: Parameters<typeof pointerCommand.execute>[1],
): Promise<Gesture["pointers"][number]["waypoints"]> {
  // Walk the action list accumulating a clock. Each action
  // advances time by its explicit wait (`wait: ms`) or by
  // `moveDurationMs` on a `move`. `down`/`up` are zero-cost.
  const waypoints: Array<{
    phase: "down" | "move" | "up";
    point: Point;
    atOffsetSeconds: number;
    pressure?: number;
  }> = [];
  let clockSeconds = 0;
  let lastPoint: Point | null = null;

  for (const action of actions) {
    if (action.type === "down") {
      const point = await resolveTarget(action.target, ctx);
      waypoints.push({
        phase: "down",
        point,
        atOffsetSeconds: clockSeconds,
        ...(action.pressure !== undefined ? { pressure: action.pressure } : {}),
      });
      lastPoint = point;
    } else if (action.type === "move") {
      const point = await resolveTarget(action.target, ctx);
      clockSeconds += moveDurationMs / 1000;
      waypoints.push({
        phase: "move",
        point,
        atOffsetSeconds: clockSeconds,
        ...(action.pressure !== undefined ? { pressure: action.pressure } : {}),
      });
      lastPoint = point;
    } else if (action.type === "wait") {
      clockSeconds += action.ms / 1000;
    } else {
      // "up"
      const point = lastPoint ?? { x: 0, y: 0 };
      waypoints.push({ phase: "up", point, atOffsetSeconds: clockSeconds });
    }
  }
  return waypoints;
}

function validateSingleSequence(
  actions: readonly PointerAction[],
): void {
  if (actions.length === 0) {
    throw new PointerError(
      "POINTER_EMPTY_SEQUENCE",
      "actions list is empty",
    );
  }
  if (actions[0]!.type !== "down") {
    throw new PointerError(
      "POINTER_NO_OPENING_DOWN",
      `first action must be \`down\`, got \`${actions[0]!.type}\``,
    );
  }
  if (actions[actions.length - 1]!.type !== "up") {
    throw new PointerError(
      "POINTER_NO_CLOSING_UP",
      `last action must be \`up\`, got \`${actions[actions.length - 1]!.type}\``,
    );
  }

  let openDown = false;
  for (let i = 0; i < actions.length; i++) {
    const a = actions[i]!;
    if (a.type === "down") {
      if (openDown) {
        throw new PointerError(
          "POINTER_NESTED_DOWN",
          `action[${i}] is \`down\` but the previous \`down\` has not yet been closed by \`up\``,
        );
      }
      openDown = true;
    } else if (a.type === "up") {
      if (!openDown) {
        throw new PointerError(
          "POINTER_NO_OPENING_DOWN",
          `action[${i}] is \`up\` without a matching preceding \`down\``,
        );
      }
      openDown = false;
    } else if (a.type === "wait") {
      if (!openDown) {
        throw new PointerError(
          "POINTER_NO_OPENING_DOWN",
          `action[${i}] is \`wait\` but no pointer is currently down`,
        );
      }
      if (a.ms < 0 || a.ms > MAX_WAIT_MS) {
        throw new PointerError(
          "POINTER_INVALID_WAIT",
          `action[${i}] wait(${a.ms}) out of range [0..${MAX_WAIT_MS}]`,
        );
      }
    } else {
      // "move"
      if (!openDown) {
        throw new PointerError(
          "POINTER_NO_OPENING_DOWN",
          `action[${i}] is \`move\` but no pointer is currently down`,
        );
      }
    }
  }
}

/**
 * Reduce a validated single-pointer sequence to one of the four
 * patterns the coordinate primitives can dispatch:
 * tap / long-press / drag / press-and-drag.
 *
 * Hybrid resolve semantics: `down: <selector>` resolves once now
 * (against the tree at dispatch time); `move: <selector>` re-
 * resolves at its own step, right before the gesture fires.
 * Coordinate targets are always absolute.
 */
async function classifyPattern(
  actions: readonly PointerAction[],
  ctx: Parameters<typeof pointerCommand.execute>[1],
): Promise<Pattern> {
  const nonUp = actions.slice(0, -1); // drop trailing `up`

  // Minimum well-formed sequence is [down, up].
  if (nonUp.length === 1) {
    const downAction = nonUp[0]! as Extract<PointerAction, { type: "down" }>;
    const downPoint = await resolveTarget(downAction.target, ctx);
    return { kind: "tap", downPoint };
  }

  // Two actions before `up`: down + (wait | move).
  if (nonUp.length === 2) {
    const [down, second] = nonUp as [
      Extract<PointerAction, { type: "down" }>,
      PointerAction,
    ];
    const downPoint = await resolveTarget(down.target, ctx);
    if (second.type === "wait") {
      return { kind: "longPress", downPoint, holdMs: second.ms };
    }
    if (second.type === "move") {
      const toPoint = await resolveTarget(second.target, ctx);
      return {
        kind: "drag",
        fromPoint: downPoint,
        toPoint,
        pressMs: DRAG_MIN_PRESS_MS,
      };
    }
    throw new PointerError(
      "POINTER_PATTERN_NOT_EXPRESSIBLE",
      `cannot express sequence: down + ${second.type} + up`,
    );
  }

  // Three actions before `up`: down + wait + move (press-and-drag).
  if (nonUp.length === 3) {
    const [down, mid, last] = nonUp as [
      Extract<PointerAction, { type: "down" }>,
      PointerAction,
      PointerAction,
    ];
    if (mid.type === "wait" && last.type === "move") {
      const downPoint = await resolveTarget(down.target, ctx);
      const toPoint = await resolveTarget(last.target, ctx);
      return {
        kind: "drag",
        fromPoint: downPoint,
        toPoint,
        pressMs: mid.ms,
      };
    }
    throw new PointerError(
      "POINTER_PATTERN_NOT_EXPRESSIBLE",
      `cannot express sequence: down + ${mid.type} + ${last.type} + up on the single-pointer path`,
    );
  }

  // Everything else — multi-waypoint bezier, repeated moves, etc.
  // — needs the multi-waypoint EventSynthesizer path which lands
  // with multi-pointer support. Reject explicitly.
  throw new PointerError(
    "POINTER_PATTERN_NOT_EXPRESSIBLE",
    "sequences with more than one `move` or interleaved waits " +
      "require the multi-waypoint synthesizer (not yet available)",
  );
}

async function resolveTarget(
  target: PointerTarget,
  ctx: Parameters<typeof pointerCommand.execute>[1],
): Promise<Point> {
  if ("point" in target) {
    return { x: target.point.x, y: target.point.y };
  }
  try {
    const selector = compileScriptSelector(target.selector);
    return await ctx.orchestra.resolvePoint(selector);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw new PointerError(
      "POINTER_SELECTOR_RESOLUTION_FAILED",
      message,
    );
  }
}

async function dispatchPattern(
  pattern: Pattern,
  moveDurationMs: number,
  ctx: Parameters<typeof pointerCommand.execute>[1],
): Promise<void> {
  switch (pattern.kind) {
    case "tap":
      await ctx.orchestra.tapAt(pattern.downPoint);
      return;
    case "longPress":
      await ctx.orchestra.longPressAt(
        pattern.downPoint,
        pattern.holdMs,
      );
      return;
    case "drag":
      // Current public path: `swipeAt` maps to the driver's
      // native press+drag primitive, where the duration argument
      // controls the press-hold before the drag starts (not the
      // drag speed itself). For a naked `[down, move, up]` we
      // pass a minimum viable press (DRAG_MIN_PRESS_MS); for
      // `[down, wait(N), move, up]` we pass N. The
      // `moveDurationMs` hint is reserved for the multi-waypoint
      // synthesizer that will actually be able to control drag
      // speed end-to-end.
      void moveDurationMs;
      await ctx.orchestra.swipeAt(
        pattern.fromPoint,
        pattern.toPoint,
        pattern.pressMs,
      );
      return;
  }
}
