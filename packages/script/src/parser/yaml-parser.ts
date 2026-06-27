import { parseAllDocuments, isSeq } from "yaml";
import {
  ScriptDefinitionSchema,
  ScriptStepSchema,
  SCRIPT_DEFAULTS,
  type ScriptDefinition,
  type ScriptStep,
} from "@atomyx/shared/script";
import { z } from "zod";
import { normalizeStep } from "./step-normalizer.js";
import { resolveVariables } from "./variable-resolver.js";
import { ScriptParseError } from "./selector-compiler.js";

/**
 * Parse a YAML test script string into a validated
 * `ScriptDefinition`. Supports two formats:
 *
 * **Entry script** (full config + steps):
 * ```yaml
 * appId: com.example.app
 * name: Login flow
 * description: Verify login works
 * tags: [smoke, auth]
 * requires:
 *   - flows/setup.yml
 * env:
 *   email: user@test.com
 * ---
 * - launchApp
 * - tap: "Sign in"
 * ```
 *
 * **Flow fragment** (steps only, no config):
 * ```yaml
 * - tap: "Email"
 * - type: ${email}
 * - tap: "Login"
 * ```
 *
 * Flow fragments are used by `requires`, `runFlow`, and
 * `do: file.yml`. They execute in the parent's context
 * (appId, variables, captures).
 *
 * Validation happens in two layers:
 *   1. The parser normalizes YAML shorthand (bare strings, implicit
 *      keys) into canonical step objects via `step-normalizer.ts`.
 *   2. The canonical object is validated against `ScriptDefinitionSchema`
 *      from `@atomyx/shared/script`, which is the single source of
 *      truth for step/selector/config shape.
 */
export function parseScript(
  yamlContent: string,
  externalEnv?: Readonly<Record<string, string>>,
): ScriptDefinition {
  const docs = parseAllDocuments(yamlContent);

  if (docs.length === 0) {
    throw new ScriptParseError("Empty script — no YAML documents found");
  }

  const { config, rawSteps, stepLines } = splitDocs(docs, yamlContent);

  const scriptEnv = coerceStringRecord(config.env);
  const mergedEnv: Record<string, string> = {
    ...scriptEnv,
    ...(externalEnv ?? {}),
  };

  const resolved = resolveVariables(rawSteps, mergedEnv);

  const steps: ScriptStep[] = [];
  for (let i = 0; i < resolved.length; i++) {
    steps.push(normalizeStep(resolved[i], i));
  }

  if (steps.length === 0 && !config.appId) {
    throw new ScriptParseError("Script has no steps");
  }

  const candidate = {
    format: config.format ?? SCRIPT_DEFAULTS.format,
    appId: config.appId ?? config.app ?? "",
    name: config.name ?? SCRIPT_DEFAULTS.name,
    description: config.description,
    precondition: config.precondition,
    tags: config.tags,
    env: mergedEnv,
    proxy: config.proxy,
    requires: config.requires,
    stepDelay: config.stepDelay,
    steps,
    _stepLines: stepLines,
  };

  return validateDefinition(candidate);
}

/**
 * Standalone entry point for validating a fully-formed canonical
 * script object — used by callers that already have a parsed
 * JS shape (e.g. API bodies, programmatic authoring).
 */
export function validateDefinition(value: unknown): ScriptDefinition {
  const result = ScriptDefinitionSchema.safeParse(value);
  if (!result.success) {
    throw scriptParseErrorFromZod("script", result.error);
  }
  return result.data;
}

/**
 * Standalone entry point for validating a single canonical step.
 * Used by the normalizer's own unit tests and by any caller that
 * wants step-level validation without parsing a whole document.
 */
export function validateStep(value: unknown): ScriptStep {
  const result = ScriptStepSchema.safeParse(value);
  if (!result.success) {
    throw scriptParseErrorFromZod("step", result.error);
  }
  return result.data;
}

interface SplitDocs {
  readonly config: RawConfig;
  readonly rawSteps: unknown[];
  readonly stepLines: number[];
}

interface RawConfig {
  readonly format?: string;
  readonly appId?: string;
  readonly app?: string;
  readonly name?: string;
  readonly description?: string;
  readonly precondition?: string;
  readonly tags?: unknown;
  readonly env?: unknown;
  readonly proxy?: unknown;
  readonly requires?: unknown;
  readonly stepDelay?: unknown;
}

function splitDocs(
  docs: ReturnType<typeof parseAllDocuments>,
  yamlText: string,
): SplitDocs {
  if (docs.length === 1) {
    const doc = docs[0]!;
    const content = doc.toJSON();
    if (Array.isArray(content)) {
      return {
        config: {},
        rawSteps: content,
        stepLines: extractStepLines(doc, "root", yamlText, content.length),
      };
    }
    if (typeof content === "object" && content !== null) {
      const { steps, ...rest } = content as Record<string, unknown>;
      if (!Array.isArray(steps)) {
        throw new ScriptParseError(
          "Single-document script must have a 'steps' array, or use " +
            "--- separator to split config from steps",
        );
      }
      return {
        config: rest as RawConfig,
        rawSteps: steps,
        stepLines: extractStepLines(doc, "key:steps", yamlText, steps.length),
      };
    }
    throw new ScriptParseError(
      "Script must be a YAML object with 'steps' key, or use --- separator",
    );
  }

  const config = (docs[0]!.toJSON() ?? {}) as RawConfig;
  const stepsDoc = docs[1]!.toJSON();
  if (!Array.isArray(stepsDoc)) {
    throw new ScriptParseError(
      "Second YAML document (after ---) must be a list of steps",
    );
  }
  return {
    config,
    rawSteps: stepsDoc,
    stepLines: extractStepLines(docs[1]!, "root", yamlText, stepsDoc.length),
  };
}

/**
 * Walk the YAML CST to find the source line of each step in the
 * top-level sequence. `mode` selects whether the steps live at
 * the root of the doc (flow fragment / second-doc form) or under
 * a `steps:` mapping key (single-doc inline form). Returns 1-based
 * line numbers parallel to the canonical `steps` array; falls back
 * to a zero-fill when the AST shape is not what the parser expects
 * so a malformed source can never crash the line lookup.
 */
interface MinimalDoc {
  readonly contents: unknown;
}

function extractStepLines(
  doc: MinimalDoc,
  mode: "root" | "key:steps",
  yamlText: string,
  expected: number,
): number[] {
  const seq = mode === "root" ? doc.contents : findStepsSeq(doc);
  if (!isSeq(seq)) return new Array(expected).fill(0);
  const lineStarts = computeLineStarts(yamlText);
  const out: number[] = [];
  for (const item of seq.items) {
    const range = (item as { range?: readonly [number, number, number] }).range;
    const offset = range?.[0] ?? -1;
    out.push(offset >= 0 ? offsetToLine(offset, lineStarts) : 0);
  }
  while (out.length < expected) out.push(0);
  return out;
}

function findStepsSeq(doc: MinimalDoc): unknown {
  const root = doc.contents as
    | { items?: { key?: { value?: unknown }; value?: unknown }[] }
    | null;
  if (!root || !Array.isArray(root.items)) return null;
  for (const pair of root.items) {
    if (pair.key && (pair.key as { value?: unknown }).value === "steps") {
      return pair.value;
    }
  }
  return null;
}

function computeLineStarts(text: string): number[] {
  const starts = [0];
  for (let i = 0; i < text.length; i++) {
    if (text.charCodeAt(i) === 10) starts.push(i + 1);
  }
  return starts;
}

function offsetToLine(offset: number, lineStarts: readonly number[]): number {
  let lo = 0;
  let hi = lineStarts.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >>> 1;
    if (lineStarts[mid]! <= offset) lo = mid;
    else hi = mid - 1;
  }
  return lo + 1;
}

function coerceStringRecord(raw: unknown): Record<string, string> {
  if (typeof raw !== "object" || raw === null) return {};
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(raw)) {
    out[k] = String(v);
  }
  return out;
}

function scriptParseErrorFromZod(
  subject: "script" | "step",
  error: z.ZodError,
): ScriptParseError {
  const lines = error.issues.map((issue) => {
    const path = issue.path.length > 0 ? issue.path.join(".") : "<root>";
    return `  at ${path}: ${issue.message}`;
  });
  return new ScriptParseError(
    `Invalid ${subject}:\n${lines.join("\n")}`,
  );
}
