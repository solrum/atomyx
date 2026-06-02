import { z } from "zod";

/**
 * Single source of truth for the shape of every YAML test script
 * Atomyx accepts. Runtime-enforced via zod; TypeScript types are
 * derived with `z.infer` from the same schemas so the compile-
 * time contract and the runtime validator cannot drift.
 *
 * Consumed by:
 *   - `@atomyx/script` — parses YAML then validates with these
 *     schemas before handing the `ScriptDefinition` to the runner.
 *   - `@atomyx/mcp` (run_script tool) — pre-flight check before
 *     dispatching to the script engine.
 *   - `@atomyx/studio` (editor) — validation, autocomplete,
 *     hover documentation derived from `.describe()` calls.
 *
 * Every field carries a `.describe()` string. Those strings surface
 * as hover tooltips in the Studio editor via `zod-to-json-schema`
 * + `monaco-yaml`. Write them for a reader who has just typed the
 * field name and needs to know what to put next: purpose first,
 * then accepted shape, then a concrete example when the shape is
 * non-trivial.
 *
 * Adding a new step type =
 *   1. Author a per-command schema below with full `.describe()`
 *      text and YAML examples.
 *   2. Append it to `ScriptStepSchema`'s union.
 *   3. Export the inferred type.
 *   4. Ship a matching `CommandDefinition` in
 *      `@atomyx/script/commands` and an entry in the YAML-shorthand
 *      `NORMALIZERS` table in the parser.
 */

/* -------------------------------------------------------------- */
/*  Shared building blocks                                         */
/* -------------------------------------------------------------- */

/**
 * Script format identifier. Runner uses this to pick the correct
 * parser behaviour. `atomyx/v1` is the only stable version today;
 * `atomyx/v2` etc. will land when a breaking change to step
 * shape or selector semantics requires it.
 */
const ScriptFormatSchema = z
  .string()
  .regex(
    /^atomyx\/v\d+$/,
    "format must match 'atomyx/v<n>' (e.g. 'atomyx/v1')",
  )
  .describe(
    "Script format version. Format: 'atomyx/v<n>'. " +
      "Current stable: 'atomyx/v1'. Omit to default to the stable version.",
  );

export const ScriptSelectorSchema = z
  .object({
    text: z
      .string()
      .optional()
      .describe(
        "Match element by its visible text (the string the user sees). " +
          "Use for buttons, labels, menu items. " +
          "YAML shorthand: a bare string like `tap: \"Login\"` expands " +
          "to `{ text: \"Login\", label: \"Login\" }` for Flutter parity.",
      ),
    id: z
      .string()
      .optional()
      .describe(
        "Match element by its stable resource id (Android " +
          "`resource-id`) or accessibility id (iOS). " +
          "Preferred when the app exposes them — survives i18n and " +
          "copy changes. Example: `id: com.app:id/login_btn`.",
      ),
    label: z
      .string()
      .optional()
      .describe(
        "Match element by its accessibility label. Flutter exposes " +
          "visible text as `label`; native Android/iOS use `text`. " +
          "Setting both lets priority broadening try each platform's " +
          "convention without the author knowing which.",
      ),
    hint: z
      .string()
      .optional()
      .describe(
        "Match element by its input hint / placeholder. Use for " +
          "text fields whose only identifier is the placeholder text " +
          "(e.g. `hint: \"Search products\"`).",
      ),
    role: z
      .string()
      .optional()
      .describe(
        "Constrain the match to a semantic role. " +
          "Common values: 'button', 'text-field', 'image', 'link', " +
          "'checkbox', 'switch', 'container'. Narrow broad text " +
          "matches — e.g. `{ text: 'Home', role: 'button' }` avoids " +
          "matching a 'Home' menu item.",
      ),
    nth: z
      .number()
      .int()
      .nonnegative()
      .optional()
      .describe(
        "When multiple elements match, pick the nth one (0-indexed). " +
          "Use sparingly — `nth` is brittle under layout changes. " +
          "Prefer adding `role` or a more specific `text`.",
      ),
  })
  .strict()
  .describe(
    "Element selector. Every field is optional, but at least one " +
      "must be set for the selector to be useful. In YAML, a bare " +
      "string shorthand — e.g. `tap: \"Login\"` — is equivalent to " +
      "`tap: { text: \"Login\", label: \"Login\" }`.",
  );

const TimeoutMsSchema = z
  .number()
  .int()
  .nonnegative()
  .describe(
    "Polling timeout in milliseconds. 0 = instant single check " +
      "(no polling). Typical range: 1000–30000 ms.",
  );

/* -------------------------------------------------------------- */
/*  Step schemas — one per command                                 */
/* -------------------------------------------------------------- */

const LaunchAppSchema = z
  .object({
    command: z.literal("launchApp"),
  })
  .strict()
  .describe(
    "Launch (or relaunch) the app declared at the top of the script " +
      "via `appId`. First step of most entry scripts. " +
      "Example: `- launchApp`.",
  );

const TapSchema = z
  .object({
    command: z.literal("tap"),
    selector: ScriptSelectorSchema,
  })
  .strict()
  .describe(
    "Tap a UI element.\n\n" +
      "```yaml\n" +
      "- tap: \"Sign in\"\n" +
      "- tap: { text: \"Continue\", role: button }\n" +
      "- tap: { id: com.app:id/next_btn }\n" +
      "```",
  );

const TypeSchema = z
  .object({
    command: z.literal("type"),
    text: z
      .string()
      .describe(
        "Text to type. `${var}` placeholders are resolved against the " +
          "script env before execution.",
      ),
    into: ScriptSelectorSchema.optional().describe(
      "Optional selector for the target field. When omitted, `type` " +
        "targets the currently focused field (after the previous step).",
    ),
  })
  .strict()
  .describe(
    "Type text into a field.\n\n" +
      "```yaml\n" +
      "- type: \"hello\"                       # focused field\n" +
      "- type: { into: \"Email\", text: ${email} }\n" +
      "```",
  );

const WaitForSchema = z
  .object({
    command: z.literal("waitFor"),
    selector: ScriptSelectorSchema,
    timeoutMs: TimeoutMsSchema.optional().describe(
      "How long to wait for the element to appear, in ms. " +
        "Default: the driver's built-in wait (usually 5000 ms).",
    ),
  })
  .strict()
  .describe(
    "Wait for an element to become visible. Fails the script if the " +
      "element does not appear within `timeoutMs`.\n\n" +
      "```yaml\n" +
      "- waitFor: \"Welcome\"\n" +
      "- waitFor: { text: \"Done\", timeout: 10000 }\n" +
      "```",
  );

const AssertVisibleSchema = z
  .object({
    command: z.literal("assertVisible"),
    selector: ScriptSelectorSchema,
    timeoutMs: TimeoutMsSchema.optional().describe(
      "Polling timeout in ms. 0 or omitted = single instant check.",
    ),
  })
  .strict()
  .describe(
    "Assert an element is currently visible. Unlike `waitFor`, this " +
      "is an assertion — the step fails (not the step's precondition) " +
      "if the element is absent.",
  );

const AssertNotVisibleSchema = z
  .object({
    command: z.literal("assertNotVisible"),
    selector: ScriptSelectorSchema,
    timeoutMs: TimeoutMsSchema.optional().describe(
      "How long to wait for the element to disappear, in ms. " +
        "0 or omitted = single instant check.",
    ),
  })
  .strict()
  .describe(
    "Assert an element is NOT visible. Useful after dismissing a " +
      "dialog or navigating away.",
  );

const ScreenshotSchema = z
  .object({
    command: z.literal("screenshot"),
    label: z
      .string()
      .optional()
      .describe(
        "Filename-safe label attached to the screenshot in the run " +
          "artifacts. Omit to auto-name by step index.",
      ),
  })
  .strict()
  .describe(
    "Capture a screenshot and attach it to the run artifacts.\n\n" +
      "```yaml\n" +
      "- screenshot               # auto-named\n" +
      "- screenshot: home_screen  # labeled\n" +
      "```",
  );

const SwipeSchema = z
  .object({
    command: z.literal("swipe"),
    direction: z
      .enum(["up", "down", "left", "right"])
      .describe(
        "Swipe direction. 'up' scrolls content up (content moves " +
          "up, the view reveals what was below). Mnemonic: direction " +
          "of finger travel.",
      ),
  })
  .strict()
  .describe(
    "Swipe the screen in a cardinal direction. Targets the centre " +
      "of the visible viewport. For swipes on a specific element or " +
      "with custom distance, use `pointer` instead.",
  );

const PressKeySchema = z
  .object({
    command: z.literal("pressKey"),
    key: z
      .string()
      .min(1)
      .describe(
        "Key identifier. Common values: 'enter', 'back', 'home', " +
          "'menu', 'search', 'volumeUp', 'volumeDown', 'power'. " +
          "Platform-specific keys are accepted — the driver maps them.",
      ),
  })
  .strict()
  .describe(
    "Press a hardware or system key. Example: `- pressKey: enter`.",
  );

const BackSchema = z
  .object({
    command: z.literal("back"),
  })
  .strict()
  .describe(
    "Navigate back one step. Equivalent to Android's back button / " +
      "iOS's swipe-back / view-stack pop. Example: `- back`.",
  );

const SleepSchema = z
  .object({
    command: z.literal("sleep"),
    ms: z
      .number()
      .int()
      .nonnegative()
      .describe(
        "Sleep duration in milliseconds. Avoid values above ~3000 — " +
          "long unconditional sleeps mask timing bugs. Prefer `waitFor`.",
      ),
  })
  .strict()
  .describe(
    "Unconditional wait. Use only when no observable signal exists " +
      "(rare). Prefer `waitFor` for element-bound waits.",
  );

const ApiPatternSchema = z
  .string()
  .min(1)
  .describe(
    "Method + path pattern to match against the captured traffic. " +
      "Format: `METHOD /path`. Supports `*` wildcard. " +
      "Examples: `POST /api/transfer`, `GET /users/*`.",
  );

const CaptureVarSchema = z
  .string()
  .min(1)
  .regex(
    /^[A-Za-z_][A-Za-z0-9_]*$/,
    "variable names must match /^[A-Za-z_][A-Za-z0-9_]*$/",
  )
  .describe(
    "Variable name to bind the captured request to. Referenceable " +
      "later via `${name}` and by `from:` in `assertApi`/`extract`/" +
      "`branch`.",
  );

const CaptureSchema = z
  .object({
    command: z.literal("capture"),
    pattern: ApiPatternSchema,
    as: CaptureVarSchema,
  })
  .strict()
  .describe(
    "Capture the next request that matches `pattern` via the network " +
      "proxy. Read-only (does not modify the request). Requires proxy.\n\n" +
      "```yaml\n" +
      "- capture: { pattern: \"POST /api/login\", as: loginResp }\n" +
      "- capture: \"POST /api/transfer as: transfer\"   # shorthand\n" +
      "```",
  );

const AssertApiSchema = z
  .object({
    command: z.literal("assertApi"),
    from: CaptureVarSchema.describe(
      "Variable name of a previously captured request.",
    ),
    status: z
      .number()
      .int()
      .min(100)
      .max(599)
      .optional()
      .describe("Expected HTTP status code (100–599)."),
    body: z
      .record(z.string(), z.unknown())
      .optional()
      .describe(
        "Body assertions keyed by dot-path. Each key is a JSONPath- " +
          "style selector against the response body; each value is " +
          "the expected match.\n\n" +
          "```yaml\n" +
          "body:\n" +
          "  \"$.status\": completed\n" +
          "  \"$.user.id\": ${userId}\n" +
          "```",
      ),
  })
  .strict()
  .describe(
    "Assert that a captured request's response matches the given " +
      "status and/or body shape. Fails the step on mismatch.",
  );

const ExtractSchema = z
  .object({
    command: z.literal("extract"),
    from: CaptureVarSchema.describe(
      "Variable name of a previously captured request.",
    ),
    values: z
      .record(z.string(), z.string())
      .describe(
        "Map of target variable name → dot-path to extract from the " +
          "captured response. Extracted values are usable as `${name}` " +
          "in subsequent steps.",
      ),
  })
  .strict()
  .describe(
    "Extract fields from a captured API response into runtime variables.\n\n" +
      "```yaml\n" +
      "- extract:\n" +
      "    from: login\n" +
      "    values:\n" +
      "      token: $.body.token\n" +
      "      userId: $.body.user.id\n" +
      "```",
  );

const HandleConditionSchema = z
  .object({
    visible: z
      .union([z.string(), ScriptSelectorSchema])
      .optional()
      .describe(
        "Branch matches when this element IS visible. " +
          "Shorthand: a bare string becomes `{ text, label }`.",
      ),
    notVisible: z
      .union([z.string(), ScriptSelectorSchema])
      .optional()
      .describe(
        "Branch matches when this element is NOT visible.",
      ),
  })
  .strict()
  .describe(
    "Condition a `handle` branch matches on. Exactly one of " +
      "`visible` / `notVisible` should be set.",
  );

const HandleBranchSchema = z
  .object({
    when: HandleConditionSchema,
    do: z
      .union([
        z.array(z.lazy(() => ScriptStepSchema)),
        z.string(),
      ])
      .describe(
        "Steps to execute when the `when` condition holds. Either " +
          "an inline list of steps, or a string path to a flow fragment " +
          "file (relative to the current script).",
      ),
  })
  .strict()
  .describe(
    "One branch of a `handle` step: a condition plus the steps to " +
      "run when that condition matches.",
  );

const HandleSchema = z
  .object({
    command: z.literal("handle"),
    branches: z
      .array(HandleBranchSchema)
      .min(1, "handle requires at least one branch")
      .describe(
        "Ordered list of branches. First matching `when` wins — " +
          "subsequent branches are skipped for this invocation.",
      ),
    otherwise: z
      .enum(["fail", "skip"])
      .optional()
      .describe(
        "Action when no branch matches within `timeout`. " +
          "'fail' (default) fails the step; 'skip' continues the script.",
      ),
    timeout: z
      .number()
      .int()
      .nonnegative()
      .optional()
      .describe(
        "Max time (ms) the runner waits for any branch's `when` " +
          "condition to hold. When the timeout expires without a match, " +
          "`otherwise` fires. Default is chosen to cover typical screen " +
          "transitions so simple scripts need no explicit `sleep` before " +
          "a `handle`.",
      ),
  })
  .strict()
  .describe(
    "UI-based branching — probe the current screen state and run " +
      "the matching branch.\n\n" +
      "```yaml\n" +
      "- handle:\n" +
      "    - when: { visible: \"Enter OTP\" }\n" +
      "      do:\n" +
      "        - type: \"123456\"\n" +
      "        - tap: \"Verify\"\n" +
      "    - when: { visible: \"Success\" }\n" +
      "      do:\n" +
      "        - screenshot: success\n" +
      "    - otherwise: fail\n" +
      "```",
  );

const BranchMatchConditionSchema = z
  .object({
    status: z
      .number()
      .int()
      .min(100)
      .max(599)
      .optional()
      .describe("Match when response status equals this code."),
    body: z
      .record(z.string(), z.unknown())
      .optional()
      .describe(
        "Match when every dot-path in this map equals its expected " +
          "value in the response body.",
      ),
  })
  .strict()
  .describe(
    "Case condition for an API-based `branch`. Conditions are ANDed " +
      "together (status AND body must both match).",
  );

const BranchCaseSchema = z
  .object({
    match: BranchMatchConditionSchema,
    do: z
      .union([
        z.array(z.lazy(() => ScriptStepSchema)),
        z.string(),
      ])
      .describe(
        "Steps to run when this case matches. Inline list or " +
          "path to a flow fragment.",
      ),
  })
  .strict()
  .describe(
    "One case of a `branch` step: a match condition plus the " +
      "steps to run when that case matches.",
  );

const BranchSchema = z
  .object({
    command: z.literal("branch"),
    from: CaptureVarSchema.describe(
      "Variable name of a previously captured request to route on.",
    ),
    on: z
      .array(BranchCaseSchema)
      .min(1, "branch requires at least one case")
      .describe(
        "Ordered list of cases. First matching `match` wins.",
      ),
    default: z
      .union([
        z.array(z.lazy(() => ScriptStepSchema)),
        z.string(),
      ])
      .optional()
      .describe(
        "Steps (inline) or flow-fragment path to run when no case " +
          "matches. Omit to make a non-match a step failure.",
      ),
  })
  .strict()
  .describe(
    "API-based branching — route based on a captured API response.\n\n" +
      "```yaml\n" +
      "- branch:\n" +
      "    from: payment\n" +
      "    on:\n" +
      "      - match: { body: { $.requires_otp: true } }\n" +
      "        do:\n" +
      "          - waitFor: \"Enter OTP\"\n" +
      "      - match: { status: 400 }\n" +
      "        do:\n" +
      "          - screenshot: error\n" +
      "    default:\n" +
      "      - waitFor: \"Success\"\n" +
      "```",
  );

const RunFlowSchema = z
  .object({
    command: z.literal("runFlow"),
    file: z
      .string()
      .min(1)
      .describe(
        "Path to the sub-flow YML file, relative to the current " +
          "script's directory. The sub-flow executes in this script's " +
          "context (shares appId, env, captures).",
      ),
    env: z
      .record(z.string(), z.string())
      .optional()
      .describe(
        "Extra variables to pass to the sub-flow. Merged over the " +
          "parent env — sub-flow values win on key collision.",
      ),
  })
  .strict()
  .describe(
    "Execute another YML script as a sub-flow.\n\n" +
      "```yaml\n" +
      "- runFlow: flows/login.yml\n" +
      "- runFlow:\n" +
      "    file: flows/login.yml\n" +
      "    env:\n" +
      "      email: other@test.com\n" +
      "```",
  );

/* -------------------------------------------------------------- */
/*  Pointer gesture primitives                                     */
/* -------------------------------------------------------------- */

const PressureSchema = z
  .number()
  .min(0)
  .max(1)
  .describe(
    "Pressure applied at this action, in the range [0.0, 1.0]. " +
      "Drivers without the `canPressure` capability reject scripts " +
      "that set this — iOS 3D Touch, Android API 26+ stroke pressure.",
  );

export const PointerTargetSchema = z
  .union([
    z
      .object({
        selector: ScriptSelectorSchema,
      })
      .strict()
      .describe(
        "Resolve to the centre of the matched element at dispatch time.",
      ),
    z
      .object({
        point: z
          .object({
            x: z.number().finite().describe("Screen x in points."),
            y: z.number().finite().describe("Screen y in points."),
          })
          .strict(),
      })
      .strict()
      .describe(
        "Absolute screen coordinates in points (the same unit the " +
          "`get_ui_tree` MCP tool emits as `@cx,cy`).",
      ),
  ])
  .describe(
    "Target of a `down` or `move` pointer action: a selector or an " +
      "absolute coordinate.",
  );

export const PointerActionSchema = z
  .discriminatedUnion("type", [
    z
      .object({
        type: z.literal("down"),
        target: PointerTargetSchema,
        pressure: PressureSchema.optional(),
      })
      .strict()
      .describe(
        "Touch down at the resolved target. A pointer sequence must " +
          "open with `down`.",
      ),
    z
      .object({
        type: z.literal("move"),
        target: PointerTargetSchema,
        pressure: PressureSchema.optional(),
      })
      .strict()
      .describe(
        "Drag the active pointer to a new target. " +
          "Duration per move comes from `moveDurationMs` on the " +
          "enclosing `pointer` step.",
      ),
    z
      .object({
        type: z.literal("wait"),
        ms: z
          .number()
          .finite()
          .nonnegative()
          .describe("Hold duration in milliseconds."),
      })
      .strict()
      .describe(
        "Hold the current pointer position for `ms` milliseconds. " +
          "Used to produce long-press and dwell gestures.",
      ),
    z
      .object({
        type: z.literal("up"),
      })
      .strict()
      .describe(
        "Release the pointer. A pointer sequence must close with `up`.",
      ),
  ])
  .describe(
    "One primitive in a pointer sequence. W3C Actions semantics.",
  );

export const PointerGroupSchema = z
  .object({
    id: z
      .string()
      .min(1)
      .describe(
        "Author-chosen pointer id. Any string — used only to " +
          "distinguish parallel pointers in the multi-pointer form.",
      ),
    actions: z
      .array(PointerActionSchema)
      .min(1)
      .describe(
        "This pointer's time-ordered action list. Must open with " +
          "`down` and close with `up`.",
      ),
  })
  .strict()
  .describe(
    "One pointer's action list, used in the multi-pointer form " +
      "(`pointer.pointers`). Pointers align on a shared wall clock " +
      "anchored at the earliest `down`.",
  );

const PointerSchema = z
  .object({
    command: z.literal("pointer"),
    actions: z
      .array(PointerActionSchema)
      .optional()
      .describe(
        "Single-pointer form: one sequence the active pointer walks " +
          "through. Mutually exclusive with `pointers`.",
      ),
    pointers: z
      .array(PointerGroupSchema)
      .optional()
      .describe(
        "Multi-pointer form: parallel sequences keyed by author id. " +
          "Requires the driver's `canMultiPointer` capability. " +
          "Mutually exclusive with `actions`.",
      ),
    moveDurationMs: z
      .number()
      .finite()
      .nonnegative()
      .optional()
      .describe(
        "Duration (ms) of each `move` action in this gesture. " +
          "Defaults to 200 ms. Shorter = flick; longer = slow drag.",
      ),
  })
  .strict()
  .describe(
    "General-purpose pointer gesture. Expresses every W3C Actions " +
      "sequence: tap, long-press, drag, flick, pinch, rotate, multi-" +
      "finger. Use when `tap`/`swipe` don't fit.\n\n" +
      "```yaml\n" +
      "- pointer:\n" +
      "    actions:\n" +
      "      - down: \"Item A\"\n" +
      "      - wait: 800\n" +
      "      - move: { x: 300, y: 600 }\n" +
      "      - up\n" +
      "    moveDurationMs: 300\n" +
      "```",
  );

/* -------------------------------------------------------------- */
/*  Script step — discriminated-style union                        */
/* -------------------------------------------------------------- */

/**
 * Tagged union of every canonical step the runner accepts. YAML
 * shorthand (bare strings, omitted command keys) is expanded by
 * the parser's normalizer BEFORE this schema validates — keep this
 * list aligned with the `NORMALIZERS` table in
 * `packages/script/src/parser/step-normalizer.ts`.
 *
 * `z.discriminatedUnion` on `command` so tooling that consumes the
 * generated JSON Schema (most importantly `monaco-yaml` in Studio)
 * can narrow by `command` value and show only the matching
 * command's hover docs instead of OR-joining all 18.
 *
 * The outer `.superRefine` enforces pointer's mutual-exclusion
 * between `actions` / `pointers` — previously modelled as a
 * `.refine` on `PointerSchema` itself, which produced a `ZodEffects`
 * wrapper incompatible with `z.discriminatedUnion`.
 */
export const ScriptStepSchema: z.ZodType<ScriptStep> = z.lazy(() =>
  z
    .discriminatedUnion("command", [
      LaunchAppSchema,
      TapSchema,
      TypeSchema,
      WaitForSchema,
      AssertVisibleSchema,
      AssertNotVisibleSchema,
      ScreenshotSchema,
      SwipeSchema,
      PressKeySchema,
      BackSchema,
      SleepSchema,
      CaptureSchema,
      AssertApiSchema,
      ExtractSchema,
      HandleSchema,
      BranchSchema,
      RunFlowSchema,
      PointerSchema,
    ])
    .superRefine((step, ctx) => {
      if (step.command !== "pointer") return;
      const hasActions = step.actions !== undefined;
      const hasPointers = step.pointers !== undefined;
      if (hasActions === hasPointers) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message:
            "pointer: exactly one of `actions` or `pointers` must be provided (they are mutually exclusive)",
          path: hasActions ? ["pointers"] : ["actions"],
        });
      }
    }),
);

/* -------------------------------------------------------------- */
/*  Top-level script definition                                    */
/* -------------------------------------------------------------- */

/**
 * Defaults that the parser and programmatic authors apply when
 * a field is absent. Kept at the schema level so every consumer
 * (parser, runner, Studio) uses the same fallbacks.
 */
export const SCRIPT_DEFAULTS = {
  format: "atomyx/v1",
  name: "Untitled script",
  stepDelayMs: 500,
  pointerMoveDurationMs: 200,
} as const;

export const ScriptDefinitionSchema = z
  .object({
    format: ScriptFormatSchema.optional().describe(
      "Script format version. Defaults to " +
        `'${SCRIPT_DEFAULTS.format}' when omitted.`,
    ),
    appId: z
      .string()
      .describe(
        "Bundle id (iOS) or package name (Android) of the app under " +
          "test. Example: `com.example.myapp` / `com.apple.mobilesafari`. " +
          "An empty string is reserved for flow fragments that run in " +
          "a parent script's context.",
      ),
    name: z
      .string()
      .min(1)
      .describe(
        "Human-readable script name. Shown in run reports and the " +
          "Studio run list.",
      ),
    description: z
      .string()
      .optional()
      .describe(
        "Longer description of what this script verifies. Surfaced in " +
          "reports and the Studio detail view.",
      ),
    precondition: z
      .string()
      .optional()
      .describe(
        "Human-readable preconditions that must hold before running " +
          "(e.g. 'user is logged out', 'device is in airplane mode'). " +
          "Informational only — not enforced by the runner.",
      ),
    tags: z
      .array(z.string().min(1))
      .optional()
      .describe(
        "Tags for filtering and organizing runs. Examples: 'smoke', " +
          "'regression', 'auth', 'ios-only'.",
      ),
    env: z
      .record(z.string(), z.string())
      .describe(
        "Variables available via `${varName}` in every step value. " +
          "External env (e.g. CLI `--env key=value`) is merged on top " +
          "of this — external wins on key collision.",
      ),
    proxy: z
      .enum(["required", "optional"])
      .optional()
      .describe(
        "Network-capture proxy policy. " +
          "'required' → runner validates proxy before executing any step. " +
          "'optional' (default) → capture commands fail individually if " +
          "no proxy is configured.",
      ),
    requires: z
      .array(z.string().min(1))
      .optional()
      .describe(
        "Flow files to run before this script (paths relative to CWD). " +
          "If any required flow fails, this script is skipped.",
      ),
    stepDelay: z
      .number()
      .nonnegative()
      .optional()
      .describe(
        `Delay in ms inserted between each step. Default: ${SCRIPT_DEFAULTS.stepDelayMs} ms — ` +
          "gives UI time to settle after actions (animations, keyboard, " +
          "network). Set to 0 for maximum speed (unit tests with MockDriver).",
      ),
    steps: z
      .array(ScriptStepSchema)
      .describe(
        "Ordered list of steps to execute. Each step runs after the " +
          "previous one completes; `stepDelay` applies between them.",
      ),
    _stepLines: z
      .array(z.number().int().nonnegative())
      .optional()
      .describe(
        "Parser metadata: 1-indexed source line for each entry in " +
          "`steps`, used by run telemetry to point a failure or a " +
          "live progress row at its exact position in the YAML. " +
          "Underscore prefix = not part of the user-authored surface; " +
          "callers do not set this — `parseScript` populates it.",
      ),
  })
  .strict()
  .describe(
    "Top-level Atomyx YAML script. Two physical forms share this " +
      "shape:\n\n" +
      "**Entry script** — full config + steps, separated by `---`:\n\n" +
      "```yaml\n" +
      "appId: com.example.app\n" +
      "name: Login flow\n" +
      "env:\n" +
      "  email: user@test.com\n" +
      "---\n" +
      "- launchApp\n" +
      "- tap: \"Sign in\"\n" +
      "```\n\n" +
      "**Flow fragment** — steps only, no config. Used by `requires`, " +
        "`runFlow`, and `do: file.yml`. Executes in the parent's " +
        "context (appId, env, captures).",
  );

/* -------------------------------------------------------------- */
/*  TypeScript types — derived from schemas                        */
/* -------------------------------------------------------------- */

export type ScriptSelector = z.infer<typeof ScriptSelectorSchema>;
export type PointerTarget = z.infer<typeof PointerTargetSchema>;
export type PointerAction = z.infer<typeof PointerActionSchema>;
export type PointerGroup = z.infer<typeof PointerGroupSchema>;

export type LaunchAppStep = z.infer<typeof LaunchAppSchema>;
export type TapStep = z.infer<typeof TapSchema>;
export type TypeStep = z.infer<typeof TypeSchema>;
export type WaitForStep = z.infer<typeof WaitForSchema>;
export type AssertVisibleStep = z.infer<typeof AssertVisibleSchema>;
export type AssertNotVisibleStep = z.infer<typeof AssertNotVisibleSchema>;
export type ScreenshotStep = z.infer<typeof ScreenshotSchema>;
export type SwipeStep = z.infer<typeof SwipeSchema>;
export type PressKeyStep = z.infer<typeof PressKeySchema>;
export type BackStep = z.infer<typeof BackSchema>;
export type SleepStep = z.infer<typeof SleepSchema>;
export type CaptureStep = z.infer<typeof CaptureSchema>;
export type AssertApiStep = z.infer<typeof AssertApiSchema>;
export type ExtractStep = z.infer<typeof ExtractSchema>;
export type RunFlowStep = z.infer<typeof RunFlowSchema>;

export type HandleCondition = z.infer<typeof HandleConditionSchema>;

/**
 * Recursive types whose zod schema references `ScriptStep` via
 * `z.lazy()`. Declared explicitly instead of via `z.infer` because
 * `ZodType<T>` loses recursive inference.
 */
export interface HandleBranch {
  readonly when: HandleCondition;
  readonly do: readonly ScriptStep[] | string;
}

export interface HandleStep {
  readonly command: "handle";
  readonly branches: readonly HandleBranch[];
  readonly otherwise?: "fail" | "skip";
  readonly timeout?: number;
}

export type BranchMatchCondition = z.infer<typeof BranchMatchConditionSchema>;

export interface BranchCase {
  readonly match: BranchMatchCondition;
  readonly do: readonly ScriptStep[] | string;
}

export interface BranchStep {
  readonly command: "branch";
  readonly from: string;
  readonly on: readonly BranchCase[];
  readonly default?: readonly ScriptStep[] | string;
}

export interface PointerStep {
  readonly command: "pointer";
  readonly actions?: readonly PointerAction[];
  readonly pointers?: readonly PointerGroup[];
  readonly moveDurationMs?: number;
}

export type ScriptStep =
  | LaunchAppStep
  | TapStep
  | TypeStep
  | WaitForStep
  | AssertVisibleStep
  | AssertNotVisibleStep
  | ScreenshotStep
  | SwipeStep
  | PressKeyStep
  | BackStep
  | SleepStep
  | CaptureStep
  | AssertApiStep
  | ExtractStep
  | HandleStep
  | BranchStep
  | RunFlowStep
  | PointerStep;

export type ScriptDefinition = z.infer<typeof ScriptDefinitionSchema>;
