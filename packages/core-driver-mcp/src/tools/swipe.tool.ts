import { z } from "zod";
import { defineTool } from "../tool-definition.js";

const SwipeArgs = z
  .union([
    z.object({
      direction: z.union([
        z.literal("up"),
        z.literal("down"),
        z.literal("left"),
        z.literal("right"),
      ]),
      durationMs: z.number().int().positive().optional(),
      fraction: z
        .number()
        .positive()
        .max(1)
        .optional()
        .describe(
          "Swipe distance as a fraction of screen size. Default 0.6.",
        ),
    }),
    z.object({
      fromX: z.number(),
      fromY: z.number(),
      toX: z.number(),
      toY: z.number(),
      durationMs: z.number().int().positive().optional(),
    }),
  ])
  .describe(
    "Either {direction} for centered directional swipe, or {fromX,fromY,toX,toY} for raw two-point swipe.",
  );

/**
 * `swipe` — directional or coordinate. The directional form is
 * the common case for scrolling lists. The coordinate form is
 * for precise gestures (drag-and-drop, partial drags from a
 * known anchor).
 */
export const swipeTool = defineTool({
  name: "swipe",
  description:
    "Swipe the screen. Pass {direction: 'up'|'down'|'left'|'right'} for a " +
    "centered directional scroll (default 60% of screen), or {fromX, fromY, " +
    "toX, toY} for a precise two-point swipe. durationMs default 200.",
  inputSchema: SwipeArgs,
  async execute(args, ctx) {
    if ("direction" in args) {
      await ctx.orchestra.swipeDirection(args.direction, {
        durationMs: args.durationMs,
        fraction: args.fraction,
      });
      return { ok: true, direction: args.direction };
    }
    await ctx.orchestra.swipeAt(
      { x: args.fromX, y: args.fromY },
      { x: args.toX, y: args.toY },
      args.durationMs,
    );
    return {
      ok: true,
      detail: `swiped (${args.fromX},${args.fromY}) → (${args.toX},${args.toY})`,
    };
  },
});
