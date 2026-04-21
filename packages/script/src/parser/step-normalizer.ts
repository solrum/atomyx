import type {
  PointerAction,
  PointerGroup,
  PointerTarget,
  ScriptStep,
} from "@atomyx/shared/script";
import {
  expandSelectorShorthand,
  ScriptParseError,
} from "./selector-compiler.js";

/**
 * Normalizer entry for a YAML shorthand key. Each entry maps
 * a key name (e.g. "tap") to a function that converts the
 * YAML value into a canonical `ScriptStep`.
 *
 * Adding a new shorthand = adding one entry to NORMALIZERS.
 */
type StepNormalizer = (value: unknown) => ScriptStep;

/**
 * Dispatch table: YAML key → ScriptStep. Table-driven, not
 * if/else chain. Extensible by appending entries.
 */
const NORMALIZERS: Record<string, StepNormalizer> = {
  launchApp: (): ScriptStep => ({ command: "launchApp" }),

  tap: (value): ScriptStep => ({
    command: "tap",
    selector: expandSelectorShorthand(value),
  }),

  type: (value): ScriptStep => {
    if (typeof value === "string") {
      return { command: "type", text: value };
    }
    const obj = value as Record<string, unknown>;
    return {
      command: "type",
      text: String(obj.text ?? ""),
      into: obj.into
        ? expandSelectorShorthand(obj.into)
        : undefined,
    };
  },

  waitFor: (value): ScriptStep => {
    if (typeof value === "string") {
      return {
        command: "waitFor",
        selector: expandSelectorShorthand(value),
      };
    }
    const obj = value as Record<string, unknown>;
    return {
      command: "waitFor",
      selector: expandSelectorShorthand(
        obj.text ?? obj.id ?? obj.label ?? obj,
      ),
      timeoutMs:
        typeof obj.timeout === "number" ? obj.timeout : undefined,
    };
  },

  assertVisible: (value): ScriptStep => {
    if (typeof value === "string") {
      return { command: "assertVisible", selector: expandSelectorShorthand(value) };
    }
    const obj = value as Record<string, unknown>;
    return {
      command: "assertVisible",
      selector: expandSelectorShorthand(obj.text ?? obj.id ?? obj.label ?? obj),
      timeoutMs: typeof obj.timeout === "number" ? obj.timeout : undefined,
    };
  },

  assertNotVisible: (value): ScriptStep => {
    if (typeof value === "string") {
      return { command: "assertNotVisible", selector: expandSelectorShorthand(value) };
    }
    const obj = value as Record<string, unknown>;
    return {
      command: "assertNotVisible",
      selector: expandSelectorShorthand(obj.text ?? obj.id ?? obj.label ?? obj),
      timeoutMs: typeof obj.timeout === "number" ? obj.timeout : undefined,
    };
  },

  screenshot: (value): ScriptStep => ({
    command: "screenshot",
    label: typeof value === "string" ? value : undefined,
  }),

  swipe: (value): ScriptStep => ({
    command: "swipe",
    direction: String(value) as ScriptStep & { command: "swipe" } extends { direction: infer D } ? D : never,
  }),

  pressKey: (value): ScriptStep => ({
    command: "pressKey",
    key: String(value),
  }),

  back: (): ScriptStep => ({ command: "back" }),

  sleep: (value): ScriptStep => ({
    command: "sleep",
    ms: typeof value === "number" ? value : parseInt(String(value), 10),
  }),

  capture: (value): ScriptStep => {
    if (typeof value === "string") {
      // Parse "POST /api/transfer as: transfer" format
      const match = value.match(
        /^(.+?)\s+as:\s*(\w+)$/,
      );
      if (!match) {
        throw new ScriptParseError(
          `Invalid capture format: "${value}". Expected: "METHOD /path as: varName"`,
        );
      }
      return {
        command: "capture",
        pattern: match[1]!.trim(),
        as: match[2]!,
      };
    }
    const obj = value as Record<string, unknown>;
    return {
      command: "capture",
      pattern: String(obj.url ?? obj.pattern ?? ""),
      as: String(obj.as ?? ""),
    };
  },

  assertApi: (value): ScriptStep => {
    const obj = value as Record<string, unknown>;
    return {
      command: "assertApi",
      from: String(obj.from ?? ""),
      status:
        typeof obj.status === "number" ? obj.status : undefined,
      body:
        typeof obj.body === "object" && obj.body !== null
          ? (obj.body as Record<string, unknown>)
          : undefined,
    };
  },

  extract: (value): ScriptStep => {
    const obj = value as Record<string, unknown>;
    return {
      command: "extract",
      from: String(obj.from ?? ""),
      values: (obj.values ?? {}) as Record<string, string>,
    };
  },

  handle: (value): ScriptStep => {
    const arr = value as Array<Record<string, unknown>>;
    const branches: Array<{
      when: { visible?: string; notVisible?: string };
      do: ScriptStep[] | string;
    }> = [];
    let otherwise: "fail" | "skip" | undefined;

    for (const entry of arr) {
      if ("otherwise" in entry) {
        otherwise = entry.otherwise === "skip" ? "skip" : "fail";
        continue;
      }
      if ("when" in entry && "do" in entry) {
        const when = entry.when as Record<string, unknown>;
        // do: string → file reference, array → inline steps
        const doValue = typeof entry.do === "string"
          ? entry.do
          : (entry.do as unknown[]).map((s, i) => normalizeStep(s, i));
        branches.push({
          when: {
            visible: when.visible as string | undefined,
            notVisible: when.notVisible as string | undefined,
          },
          do: doValue,
        });
      }
    }

    return { command: "handle", branches, otherwise };
  },

  branch: (value): ScriptStep => {
    const obj = value as Record<string, unknown>;
    const on = ((obj.on ?? []) as Array<Record<string, unknown>>).map(
      (c) => ({
        match: (c.match ?? {}) as {
          status?: number;
          body?: Record<string, unknown>;
        },
        do: typeof c.do === "string"
          ? c.do
          : ((c.do ?? []) as unknown[]).map((s, i) => normalizeStep(s, i)),
      }),
    );
    const defaultValue = obj.default
      ? typeof obj.default === "string"
        ? obj.default
        : (obj.default as unknown[]).map((s, i) => normalizeStep(s, i))
      : undefined;

    return {
      command: "branch",
      from: String(obj.from ?? ""),
      on,
      default: defaultValue,
    };
  },

  runFlow: (value): ScriptStep => {
    if (typeof value === "string") {
      return { command: "runFlow", file: value };
    }
    const obj = value as Record<string, unknown>;
    return {
      command: "runFlow",
      file: String(obj.file ?? ""),
      env: typeof obj.env === "object" && obj.env !== null
        ? (obj.env as Record<string, string>)
        : undefined,
    };
  },

  pointer: (value): ScriptStep => {
    if (typeof value !== "object" || value === null) {
      throw new ScriptParseError(
        "pointer: expected an object with `actions` or `pointers`",
      );
    }
    const obj = value as Record<string, unknown>;
    const hasActions = Array.isArray(obj.actions);
    const hasPointers = Array.isArray(obj.pointers);

    if (hasActions === hasPointers) {
      throw new ScriptParseError(
        hasActions
          ? "pointer: `actions` and `pointers` are mutually exclusive"
          : "pointer: must specify either `actions` or `pointers`",
      );
    }

    const moveDurationMs = normalizePointerMoveDuration(obj.moveDurationMs);

    if (hasActions) {
      return {
        command: "pointer",
        actions: normalizePointerActions(obj.actions as readonly unknown[]),
        ...(moveDurationMs !== undefined ? { moveDurationMs } : {}),
      };
    }

    return {
      command: "pointer",
      pointers: normalizePointerGroups(obj.pointers as readonly unknown[]),
      ...(moveDurationMs !== undefined ? { moveDurationMs } : {}),
    };
  },
};

function normalizePointerMoveDuration(raw: unknown): number | undefined {
  if (raw === undefined || raw === null) return undefined;
  if (typeof raw !== "number" || !Number.isFinite(raw)) {
    throw new ScriptParseError(
      `pointer.moveDurationMs: expected number, got ${typeof raw}`,
    );
  }
  return raw;
}

function normalizePointerGroups(raw: readonly unknown[]): PointerGroup[] {
  return raw.map((entry, i) => {
    if (typeof entry !== "object" || entry === null) {
      throw new ScriptParseError(
        `pointer.pointers[${i}]: expected an object with \`id\` and \`actions\``,
      );
    }
    const obj = entry as Record<string, unknown>;
    if (typeof obj.id !== "string" || obj.id.length === 0) {
      throw new ScriptParseError(
        `pointer.pointers[${i}]: missing string \`id\``,
      );
    }
    if (!Array.isArray(obj.actions)) {
      throw new ScriptParseError(
        `pointer.pointers[${i}]: missing \`actions\` array`,
      );
    }
    return {
      id: obj.id,
      actions: normalizePointerActions(obj.actions as readonly unknown[]),
    };
  });
}

function normalizePointerActions(
  raw: readonly unknown[],
): PointerAction[] {
  return raw.map((entry, i) => normalizePointerAction(entry, i));
}

function normalizePointerAction(raw: unknown, index: number): PointerAction {
  if (typeof raw === "string") {
    if (raw === "up") return { type: "up" };
    throw new ScriptParseError(
      `pointer.actions[${index}]: bare string must be "up", got "${raw}"`,
    );
  }
  if (typeof raw !== "object" || raw === null) {
    throw new ScriptParseError(
      `pointer.actions[${index}]: expected string or object, got ${typeof raw}`,
    );
  }

  const entry = raw as Record<string, unknown>;
  const keys = Object.keys(entry);
  if (keys.length !== 1) {
    throw new ScriptParseError(
      `pointer.actions[${index}]: expected a single-key object, got keys [${keys.join(", ")}]`,
    );
  }

  const key = keys[0]!;
  const value = entry[key];

  switch (key) {
    case "down":
      return {
        type: "down",
        target: normalizePointerTarget(value, index, "down"),
        ...extractPressure(value, index, "down"),
      };
    case "move":
      return {
        type: "move",
        target: normalizePointerTarget(value, index, "move"),
        ...extractPressure(value, index, "move"),
      };
    case "wait":
      if (typeof value !== "number" || !Number.isFinite(value)) {
        throw new ScriptParseError(
          `pointer.actions[${index}]: wait expects a number (ms), got ${typeof value}`,
        );
      }
      return { type: "wait", ms: value };
    case "up":
      // Object form `{ up: ... }` is accepted for YAML parser quirks but the
      // payload is discarded; canonical form is the bare string "up".
      return { type: "up" };
    default:
      throw new ScriptParseError(
        `pointer.actions[${index}]: unknown action "${key}" (expected down / move / wait / up)`,
      );
  }
}

/**
 * Pull the optional `pressure` field out of an action's payload.
 * Selectors (`"Item A"`) have no pressure; coordinate and
 * selector-object forms may specify it as
 * `down: { x, y, pressure: 0.5 }` or
 * `down: { id: "btn", pressure: 0.9 }`. Validated to 0.0–1.0.
 */
function extractPressure(
  raw: unknown,
  index: number,
  action: "down" | "move",
): { pressure?: number } {
  if (typeof raw !== "object" || raw === null) return {};
  const value = (raw as Record<string, unknown>).pressure;
  if (value === undefined) return {};
  if (typeof value !== "number" || !Number.isFinite(value) || value < 0 || value > 1) {
    throw new ScriptParseError(
      `pointer.actions[${index}].${action}: pressure must be a number in [0.0, 1.0], got ${String(value)}`,
    );
  }
  return { pressure: value };
}

function normalizePointerTarget(
  raw: unknown,
  index: number,
  action: "down" | "move",
): PointerTarget {
  if (typeof raw === "string") {
    return { selector: expandSelectorShorthand(raw) };
  }
  if (typeof raw !== "object" || raw === null) {
    throw new ScriptParseError(
      `pointer.actions[${index}].${action}: expected selector or { x, y }, got ${typeof raw}`,
    );
  }
  const obj = raw as Record<string, unknown>;

  // Coordinate form — `x` + `y` as numbers.
  if (typeof obj.x === "number" && typeof obj.y === "number") {
    if (!Number.isFinite(obj.x) || !Number.isFinite(obj.y)) {
      throw new ScriptParseError(
        `pointer.actions[${index}].${action}: x and y must be finite numbers`,
      );
    }
    return { point: { x: obj.x, y: obj.y } };
  }

  // Selector object — delegate to the shared expander.
  return { selector: expandSelectorShorthand(obj) };
}

/**
 * Normalize a raw YAML step (object or bare string) into a
 * canonical `ScriptStep`. Handles both forms:
 *
 *   - Bare string: `"launchApp"` or `"back"`
 *   - Key-value: `{ tap: "Login" }` or `{ type: { into: "Email", text: "hi" } }`
 */
export function normalizeStep(
  raw: unknown,
  stepIndex: number,
): ScriptStep {
  // Bare string → command with no args
  if (typeof raw === "string") {
    const normalizer = NORMALIZERS[raw];
    if (!normalizer) {
      throw new ScriptParseError(
        `Step ${stepIndex + 1}: unknown command "${raw}"`,
      );
    }
    return normalizer(undefined);
  }

  if (typeof raw !== "object" || raw === null) {
    throw new ScriptParseError(
      `Step ${stepIndex + 1}: expected string or object, got ${typeof raw}`,
    );
  }

  const keys = Object.keys(raw);
  if (keys.length === 0) {
    throw new ScriptParseError(
      `Step ${stepIndex + 1}: empty step object`,
    );
  }

  const commandKey = keys[0]!;
  const normalizer = NORMALIZERS[commandKey];
  if (!normalizer) {
    throw new ScriptParseError(
      `Step ${stepIndex + 1}: unknown command "${commandKey}"`,
    );
  }

  return normalizer((raw as Record<string, unknown>)[commandKey]);
}
