import { parse as parseYaml } from "yaml";
import {
  ScenarioDefinitionSchema,
  type ScenarioDefinition,
} from "@atomyx/shared/script";
import { z } from "zod";
import { ScriptParseError } from "./selector-compiler.js";

/**
 * Parse a YAML scenario string into a validated `ScenarioDefinition`.
 *
 * ```yaml
 * name: Checkout regression
 * description: Login + browse + buy happy path
 * scripts:
 *   - flows/login.yml
 *   - flows/browse-catalog.yml
 *   - flows/checkout.yml
 * onFailure: stop
 * env:
 *   BASE_URL: https://staging.example.com
 * ```
 *
 * Throws `ScriptParseError` on malformed YAML or schema mismatch.
 */
export function parseScenario(yamlContent: string): ScenarioDefinition {
  let raw: unknown;
  try {
    raw = parseYaml(yamlContent);
  } catch (err) {
    throw new ScriptParseError(
      `Invalid YAML in scenario: ${(err as Error).message}`,
    );
  }
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) {
    throw new ScriptParseError(
      "Scenario root must be a YAML mapping with `name` and `scripts` keys",
    );
  }
  try {
    return ScenarioDefinitionSchema.parse(raw);
  } catch (err) {
    if (err instanceof z.ZodError) {
      const issues = err.issues
        .map((i) => `  - ${i.path.join(".") || "<root>"}: ${i.message}`)
        .join("\n");
      throw new ScriptParseError(`Scenario validation failed:\n${issues}`);
    }
    throw err;
  }
}

/**
 * Cheap shape-detection used by the runtime layer to decide
 * between `parseScript` and `parseScenario` when a caller hands
 * over an untyped YAML string. Returns true when the parsed root
 * is an object containing a `scripts` array — the structural
 * fingerprint of a scenario document.
 */
export function isScenarioYaml(yamlContent: string): boolean {
  let raw: unknown;
  try {
    raw = parseYaml(yamlContent);
  } catch {
    return false;
  }
  if (raw === null || typeof raw !== "object" || Array.isArray(raw)) {
    return false;
  }
  const scripts = (raw as { scripts?: unknown }).scripts;
  return Array.isArray(scripts);
}
