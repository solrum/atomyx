import { parseAllDocuments } from "yaml";
import type { ScriptDefinition, ScriptStep } from "@atomyx/shared/script";
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
 */
export function parseScript(
  yamlContent: string,
  externalEnv?: Readonly<Record<string, string>>,
): ScriptDefinition {
  const docs = parseAllDocuments(yamlContent);

  if (docs.length === 0) {
    throw new ScriptParseError("Empty script — no YAML documents found");
  }

  let config: Record<string, unknown>;
  let rawSteps: unknown[];

  if (docs.length === 1) {
    const content = docs[0]!.toJSON();
    if (Array.isArray(content)) {
      // Flow fragment — steps only, no config
      config = {};
      rawSteps = content;
    } else if (typeof content === "object" && content !== null) {
      const { steps, ...rest } = content as Record<string, unknown>;
      config = rest;
      rawSteps = Array.isArray(steps) ? steps : [];
      if (!Array.isArray(steps)) {
        throw new ScriptParseError(
          "Single-document script must have a 'steps' array, or use " +
            "--- separator to split config from steps",
        );
      }
    } else {
      throw new ScriptParseError(
        "Script must be a YAML object with 'steps' key, or use --- separator",
      );
    }
  } else {
    // Two documents: config (first) + steps (second)
    config = (docs[0]!.toJSON() ?? {}) as Record<string, unknown>;
    const stepsDoc = docs[1]!.toJSON();
    if (!Array.isArray(stepsDoc)) {
      throw new ScriptParseError(
        "Second YAML document (after ---) must be a list of steps",
      );
    }
    rawSteps = stepsDoc;
  }

  // Extract config fields
  const format = config.format ? String(config.format) : "atomyx/v1";
  const appId = String(config.appId ?? config.app ?? "");
  const name = String(config.name ?? "Untitled script");
  const description = config.description ? String(config.description) : undefined;
  const precondition = config.precondition ? String(config.precondition) : undefined;
  const proxy = config.proxy === "required" ? "required" as const : undefined;
  const stepDelay = typeof config.stepDelay === "number" ? config.stepDelay : undefined;

  // Tags
  let tags: string[] | undefined;
  if (Array.isArray(config.tags)) {
    tags = config.tags.map(String);
  }

  // Requires
  let requires: string[] | undefined;
  if (Array.isArray(config.requires)) {
    requires = config.requires.map(String);
  }

  // Env
  const scriptEnv: Record<string, string> = {};
  if (typeof config.env === "object" && config.env !== null) {
    for (const [k, v] of Object.entries(config.env)) {
      scriptEnv[k] = String(v);
    }
  }

  // Merge external env (CLI --env flags) over script env
  const mergedEnv: Record<string, string> = {
    ...scriptEnv,
    ...(externalEnv ?? {}),
  };

  // Resolve variables in raw steps
  const resolved = resolveVariables(rawSteps, mergedEnv);

  // Normalize steps
  const steps: ScriptStep[] = [];
  for (let i = 0; i < resolved.length; i++) {
    steps.push(normalizeStep(resolved[i], i));
  }

  // Flow fragments can have 0 steps if loaded for metadata only
  if (steps.length === 0 && !config.appId) {
    throw new ScriptParseError("Script has no steps");
  }

  return {
    format,
    appId,
    name,
    description,
    precondition,
    tags,
    env: mergedEnv,
    proxy,
    stepDelay,
    requires,
    steps,
  };
}
