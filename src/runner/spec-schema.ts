import { z } from "zod";

/**
 * YAML test spec schema for Atomyx Mode B (scripted).
 *
 * Shape:
 *   name: ...
 *   target: com.example.app
 *   data: { key: value }
 *   setup: [ <step>, ... ]
 *   steps: [ <step>, ... ]
 *   verify:
 *     mustContain: [...]
 *     mustNotContain: [...]
 *   bug_rules: [ <rule>, ... ]
 *   teardown: [ <step>, ... ]
 *
 * Step shape (one of):
 *   { launch: "package.name" }
 *   { tap: { ...selector } }
 *   { input: { find: { ...selector }, text: "value" } }
 *   { swipe: { fromX, fromY, toX, toY, durationMs? } }
 *   { press_key: "back" | "home" | "enter" }
 *   { wait_for_idle: { timeoutMs?, idleMs? } }
 *   { wait_for: { ...selector, timeoutMs? } }
 *   { assert: { mustContain?, mustNotContain? } }
 *   { sleep: 500 }
 *
 * `selector` shape: { resourceId?, contentDesc?, text?, textContains?, hint?, nth? }
 * — resolved on the device side via SelectorResolver strategies (fastest first).
 */

const selectorCriteria = z
  .object({
    resourceId: z.string().optional(),
    contentDesc: z.string().optional(),
    text: z.string().optional(),
    textContains: z.string().optional(),
    hint: z.string().optional(),
    nth: z.number().int().nonnegative().optional(),
  })
  .strict();

const stepLaunch = z.object({ launch: z.string() }).strict();
const stepTap = z.object({ tap: selectorCriteria, id: z.string().optional() }).strict();
const stepInput = z
  .object({
    input: z.object({ find: selectorCriteria, text: z.string() }).strict(),
    id: z.string().optional(),
  })
  .strict();
const stepSwipe = z
  .object({
    swipe: z
      .object({
        fromX: z.number(),
        fromY: z.number(),
        toX: z.number(),
        toY: z.number(),
        durationMs: z.number().optional(),
      })
      .strict(),
    id: z.string().optional(),
  })
  .strict();
const stepPressKey = z
  .object({ press_key: z.enum(["back", "home", "enter"]), id: z.string().optional() })
  .strict();
const stepWaitIdle = z
  .object({
    wait_for_idle: z
      .object({
        timeoutMs: z.number().optional(),
        idleMs: z.number().optional(),
      })
      .strict()
      .optional()
      .default({}),
    id: z.string().optional(),
  })
  .strict();
const stepWaitFor = z
  .object({
    wait_for: selectorCriteria.extend({ timeoutMs: z.number().optional() }),
    id: z.string().optional(),
  })
  .strict();
const stepAssert = z
  .object({
    assert: z
      .object({
        mustContain: z.array(z.string()).optional(),
        mustNotContain: z.array(z.string()).optional(),
      })
      .strict(),
    id: z.string().optional(),
  })
  .strict();
const stepSleep = z.object({ sleep: z.number(), id: z.string().optional() }).strict();

export const stepSchema = z.union([
  stepLaunch,
  stepTap,
  stepInput,
  stepSwipe,
  stepPressKey,
  stepWaitIdle,
  stepWaitFor,
  stepAssert,
  stepSleep,
]);

export const verifySchema = z
  .object({
    mustContain: z.array(z.string()).optional(),
    mustNotContain: z.array(z.string()).optional(),
  })
  .strict();

export const bugRuleSchema = z
  .object({
    if: z.enum(["step_failed", "timeout", "text_matches", "verify_failed"]),
    pattern: z.string().optional(),
    severity: z.enum(["low", "medium", "high", "critical"]),
  })
  .strict();

export const specSchema = z
  .object({
    name: z.string(),
    target: z.string().optional(),
    device: z.string().optional(),
    data: z.record(z.string(), z.any()).optional(),
    setup: z.array(stepSchema).optional(),
    steps: z.array(stepSchema),
    verify: verifySchema.optional(),
    bug_rules: z.array(bugRuleSchema).optional(),
    teardown: z.array(stepSchema).optional(),
  })
  .strict();

export type Spec = z.infer<typeof specSchema>;
export type Step = z.infer<typeof stepSchema>;
export type SelectorCriteriaSpec = z.infer<typeof selectorCriteria>;
export type BugRule = z.infer<typeof bugRuleSchema>;
